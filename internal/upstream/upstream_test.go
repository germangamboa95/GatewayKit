package upstream

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

type fakeClock struct{ now time.Time }

func (c *fakeClock) Now() time.Time { return c.now }

type fakeSleeper struct {
	clock  *fakeClock
	slept  []time.Duration
	cancel bool
}

func (s *fakeSleeper) Sleep(ctx context.Context, d time.Duration) error {
	if s.cancel {
		return context.Canceled
	}
	s.slept = append(s.slept, d)
	s.clock.now = s.clock.now.Add(d)
	return ctx.Err()
}

type result struct {
	status int
	err    error
	block  bool
}

type fakeClient struct {
	mu      sync.Mutex
	results []result
	targets []*contract.Target
	bodies  []string
}

func (c *fakeClient) Send(ctx context.Context, t *contract.Target, r *contract.OutRequest) (*contract.Response, error) {
	c.mu.Lock()
	i := len(c.targets)
	c.targets = append(c.targets, t)
	c.bodies = append(c.bodies, string(r.Body))
	res := c.results[min(i, len(c.results)-1)]
	c.mu.Unlock()
	if res.block {
		<-ctx.Done()
		return nil, ctx.Err()
	}
	if res.err != nil {
		return nil, res.err
	}
	return &contract.Response{Status: res.status, Header: http.Header{"X-Attempt": {string(rune('0' + i + 1))}}, FromUpstream: true}, nil
}

type rrBalancer struct{ next int }

func (b *rrBalancer) Pick(p contract.TargetPool) (*contract.Target, bool) {
	ts := p.Targets()
	for range ts {
		t := ts[b.next%len(ts)]
		b.next++
		if p.Healthy(t) {
			return t, true
		}
	}
	return nil, false
}

type fakeBreaker struct {
	passes     int
	allow      int
	retryAfter int
	records    []bool
	releases   int
	now        []time.Time
}

func (b *fakeBreaker) TryPass(now time.Time) (bool, int) {
	b.now = append(b.now, now)
	if b.passes >= b.allow {
		return false, b.retryAfter
	}
	b.passes++
	return true, 0
}

func (b *fakeBreaker) Record(ok bool, now time.Time) { b.records = append(b.records, ok) }

func (b *fakeBreaker) Release() { b.releases++ }

type fakeRetry struct {
	on      map[int]bool
	initial time.Duration
	always  bool
}

func (p fakeRetry) ShouldRetry(status, attempt int) bool { return p.always || p.on[status] }

func (p fakeRetry) Delay(attempt int) time.Duration { return p.initial << (attempt - 1) }

func mkRoute(t *testing.T, attempts int, urls ...string) *contract.Route {
	r := &contract.Route{Path: "/api", Timeout: time.Second}
	for _, u := range urls {
		tg, err := contract.NewTarget(u, 1)
		if err != nil {
			t.Fatal(err)
		}
		r.Targets = append(r.Targets, tg)
	}
	if attempts > 0 {
		r.Retry = &contract.Retry{Attempts: attempts}
	}
	return r
}

func mkCtx(body string) *contract.RequestContext {
	r := httptest.NewRequest("POST", "http://g/api/x", strings.NewReader(body))
	return &contract.RequestContext{Req: r, Body: []byte(body)}
}

func deps(c contract.UpstreamClient) (contract.Deps, *fakeClock, *fakeSleeper) {
	clk := &fakeClock{now: time.Unix(1000, 0)}
	sl := &fakeSleeper{clock: clk}
	return contract.Deps{Clock: clk, Sleeper: sl, Client: c}, clk, sl
}

var on5xx = map[int]bool{502: true, 503: true, 504: true}

func TestRetryWaitsRepicksAndReturnsLast(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1", "http://b:1")
	c := &fakeClient{results: []result{{status: 503}, {status: 503}, {status: 200}}}
	d, _, sl := deps(c)
	h := New(route, d, WithBalancer(&rrBalancer{}), WithRetry(fakeRetry{on: on5xx, initial: time.Second}))
	ctx := mkCtx(`{"a":1}`)
	res := h(ctx)
	if res.Status != 200 || !res.FromUpstream || res.Header.Get("X-Attempt") != "3" {
		t.Fatalf("res %+v", res)
	}
	if ctx.Upstream != res || ctx.ResponseTime != time.Unix(1003, 0) {
		t.Fatalf("upstream %v time %v", ctx.Upstream, ctx.ResponseTime)
	}
	want := []string{"a:1", "b:1", "a:1"}
	for i, tg := range c.targets {
		if tg.URL.Host != want[i] || c.bodies[i] != `{"a":1}` {
			t.Fatalf("attempt %d target %s body %q", i+1, tg.URL.Host, c.bodies[i])
		}
	}
	if len(sl.slept) != 2 || sl.slept[0] != time.Second || sl.slept[1] != 2*time.Second {
		t.Fatalf("slept %v", sl.slept)
	}
}

func TestRetryExhaustedReturnsLastUpstreamResponse(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1")
	c := &fakeClient{results: []result{{status: 503}}}
	d, _, sl := deps(c)
	res := New(route, d, WithRetry(fakeRetry{always: true, initial: time.Second}))(mkCtx(""))
	if len(c.targets) != 3 || res.Status != 503 || !res.FromUpstream || res.Header.Get("X-Attempt") != "3" {
		t.Fatalf("attempts %d res %+v", len(c.targets), res)
	}
	if len(sl.slept) != 2 {
		t.Fatalf("slept %v", sl.slept)
	}
}

func TestNoRetryOnUnlistedStatus(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1")
	c := &fakeClient{results: []result{{status: 500}}}
	d, _, _ := deps(c)
	res := New(route, d, WithRetry(fakeRetry{on: on5xx, initial: time.Second}))(mkCtx(""))
	if len(c.targets) != 1 || res.Status != 500 {
		t.Fatalf("attempts %d status %d", len(c.targets), res.Status)
	}
}

func TestConnectionErrorRetriedAs502(t *testing.T) {
	route := mkRoute(t, 2, "http://a:1")
	c := &fakeClient{results: []result{{err: errors.New("connection refused")}}}
	d, _, _ := deps(c)
	res := New(route, d, WithRetry(fakeRetry{on: map[int]bool{502: true}}))(mkCtx(""))
	if len(c.targets) != 2 || res.Status != 502 || res.FromUpstream || !strings.Contains(string(res.Body), "bad_gateway") {
		t.Fatalf("attempts %d res %+v", len(c.targets), res)
	}
	c = &fakeClient{results: []result{{err: errors.New("connection refused")}}}
	d, _, _ = deps(c)
	New(route, d, WithRetry(fakeRetry{on: map[int]bool{500: true}}))(mkCtx(""))
	if len(c.targets) != 1 {
		t.Fatalf("502 retried with on [500]: %d attempts", len(c.targets))
	}
}

func TestBreakerCheckedBeforeEachAttempt(t *testing.T) {
	route := mkRoute(t, 5, "http://a:1")
	c := &fakeClient{results: []result{{status: 503}}}
	d, clk, _ := deps(c)
	cb := &fakeBreaker{allow: 2, retryAfter: 30}
	res := New(route, d, WithBreaker(cb), WithRetry(fakeRetry{on: on5xx, initial: time.Second}))(mkCtx(""))
	if len(c.targets) != 2 {
		t.Fatalf("upstream got %d requests", len(c.targets))
	}
	if res.Status != 503 || res.FromUpstream || res.Header.Get("Retry-After") != "30" || string(res.Body) != `{"error":"service_unavailable","retry_after":30}` {
		t.Fatalf("res %+v %s", res, res.Body)
	}
	if len(cb.now) != 3 || len(cb.records) != 2 || cb.records[0] || cb.records[1] {
		t.Fatalf("tryPass %d records %v", len(cb.now), cb.records)
	}
	if cb.now[2] != clk.now {
		t.Fatalf("breaker not given the fake clock time")
	}
}

func TestBreakerRecordsSuccessBelow500(t *testing.T) {
	route := mkRoute(t, 0, "http://a:1")
	c := &fakeClient{results: []result{{status: 404}}}
	d, _, _ := deps(c)
	cb := &fakeBreaker{allow: 10}
	New(route, d, WithBreaker(cb))(mkCtx(""))
	if len(cb.records) != 1 || !cb.records[0] || cb.releases != 0 {
		t.Fatalf("records %v releases %d", cb.records, cb.releases)
	}
}

func TestBreakerOpenBeatsNoHealthyUpstream(t *testing.T) {
	route := mkRoute(t, 0, "http://a:1")
	route.Targets[0].SetHealthy(false)
	c := &fakeClient{results: []result{{status: 200}}}
	d, _, _ := deps(c)
	res := New(route, d, WithBreaker(&fakeBreaker{allow: 0, retryAfter: 5}))(mkCtx(""))
	if res.Status != 503 || !strings.Contains(string(res.Body), "service_unavailable") {
		t.Fatalf("res %s", res.Body)
	}
}

func TestHalfOpenAllUnhealthyReleasesTrial(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1", "http://b:1")
	for _, tg := range route.Targets {
		tg.SetHealthy(false)
	}
	c := &fakeClient{results: []result{{status: 200}}}
	d, _, _ := deps(c)
	cb := &fakeBreaker{allow: 1}
	res := New(route, d, WithBreaker(cb), WithBalancer(&rrBalancer{}), WithRetry(fakeRetry{always: true}))(mkCtx(""))
	if res.Status != 503 || res.FromUpstream || !strings.Contains(string(res.Body), `"no_healthy_upstream"`) || !strings.Contains(string(res.Body), `"message"`) {
		t.Fatalf("res %+v %s", res, res.Body)
	}
	if cb.releases != 1 || len(cb.records) != 0 || len(c.targets) != 0 {
		t.Fatalf("releases %d records %v sends %d", cb.releases, cb.records, len(c.targets))
	}
}

func TestClientCancelNotRecorded(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1")
	c := &fakeClient{results: []result{{block: true}}}
	d, _, sl := deps(c)
	cb := &fakeBreaker{allow: 10}
	ctx := mkCtx("")
	rctx, cancel := context.WithCancel(context.Background())
	ctx.Req = ctx.Req.WithContext(rctx)
	time.AfterFunc(20*time.Millisecond, cancel)
	res := New(route, d, WithBreaker(cb), WithRetry(fakeRetry{always: true}))(ctx)
	if res.Status != contract.StatusClientClosed || res.FromUpstream {
		t.Fatalf("res %+v", res)
	}
	if len(cb.records) != 0 || cb.releases != 1 || len(c.targets) != 1 || len(sl.slept) != 0 {
		t.Fatalf("records %v releases %d sends %d slept %v", cb.records, cb.releases, len(c.targets), sl.slept)
	}
	if ctx.Upstream != nil {
		t.Fatal("upstream set on cancel")
	}
}

func TestCancelDuringRetryWaitStops(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1")
	c := &fakeClient{results: []result{{status: 503}}}
	d, _, sl := deps(c)
	sl.cancel = true
	res := New(route, d, WithRetry(fakeRetry{always: true}))(mkCtx(""))
	if len(c.targets) != 1 || res.Status != contract.StatusClientClosed {
		t.Fatalf("sends %d status %d", len(c.targets), res.Status)
	}
}

func TestTimeoutIsGateway504NotFromUpstream(t *testing.T) {
	route := mkRoute(t, 2, "http://a:1")
	route.Timeout = 10 * time.Millisecond
	c := &fakeClient{results: []result{{block: true}}}
	d, _, _ := deps(c)
	cb := &fakeBreaker{allow: 10}
	ctx := mkCtx("")
	res := New(route, d, WithBreaker(cb), WithRetry(fakeRetry{on: on5xx}))(ctx)
	if res.Status != 504 || res.FromUpstream || !strings.Contains(string(res.Body), "gateway_timeout") {
		t.Fatalf("res %+v %s", res, res.Body)
	}
	if len(c.targets) != 2 || len(cb.records) != 2 || cb.records[0] || ctx.Upstream != nil {
		t.Fatalf("sends %d records %v upstream %v", len(c.targets), cb.records, ctx.Upstream)
	}
}

func TestDefaultsSingleAttemptFirstHealthy(t *testing.T) {
	route := mkRoute(t, 3, "http://a:1", "http://b:1")
	route.Targets[0].SetHealthy(false)
	c := &fakeClient{results: []result{{status: 503}}}
	d, _, _ := deps(c)
	res := New(route, d)(mkCtx(""))
	if len(c.targets) != 1 || c.targets[0].URL.Host != "b:1" || res.Status != 503 {
		t.Fatalf("sends %d status %d", len(c.targets), res.Status)
	}
}

func TestRealClientRoundTrip(t *testing.T) {
	var mu sync.Mutex
	hits := 0
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		hits++
		n := hits
		mu.Unlock()
		if n == 1 {
			w.WriteHeader(503)
			return
		}
		w.Write([]byte(r.URL.Path))
	}))
	defer srv.Close()
	route := mkRoute(t, 2, srv.URL)
	route.StripPrefix = true
	d, _, _ := deps(nil)
	d.Client = nil
	res := New(route, d, WithRetry(fakeRetry{on: on5xx}))(mkCtx("x"))
	if res.Status != 200 || string(res.Body) != "/x" || !res.FromUpstream || hits != 2 {
		t.Fatalf("res %+v %q hits %d", res, res.Body, hits)
	}
}

func TestPoolMarkProbe(t *testing.T) {
	route := mkRoute(t, 0, "http://a:1")
	route.HealthCheck = &contract.HealthCheck{UnhealthyThreshold: 2}
	p := NewPool(route)
	tg := route.Targets[0]
	p.MarkProbe(tg, false)
	if !p.Healthy(tg) {
		t.Fatal("unhealthy after 1 failure")
	}
	p.MarkProbe(tg, false)
	if p.Healthy(tg) {
		t.Fatal("healthy after 2 failures")
	}
	p.MarkProbe(tg, true)
	if !p.Healthy(tg) {
		t.Fatal("not recovered")
	}
}
