package healthcheck

import (
	"context"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

// stepSleeper reports each Sleep call and blocks until the test releases it.
type stepSleeper struct {
	calls   chan time.Duration
	release chan struct{}
}

func newStepSleeper() *stepSleeper {
	return &stepSleeper{calls: make(chan time.Duration), release: make(chan struct{})}
}

func (s *stepSleeper) Sleep(ctx context.Context, d time.Duration) error {
	select {
	case s.calls <- d:
	case <-ctx.Done():
		return ctx.Err()
	}
	select {
	case <-s.release:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *stepSleeper) waitRound(t *testing.T) time.Duration {
	t.Helper()
	select {
	case d := <-s.calls:
		return d
	case <-time.After(5 * time.Second):
		t.Fatal("probe round did not finish")
		return 0
	}
}

func (s *stepSleeper) next(t *testing.T) time.Duration {
	t.Helper()
	s.release <- struct{}{}
	return s.waitRound(t)
}

type upstream struct {
	srv    *httptest.Server
	status atomic.Int64
	hits   atomic.Int64
	mu     sync.Mutex
	paths  []string
	hang   bool
}

func newUpstream(t *testing.T, status int) *upstream {
	u := &upstream{}
	u.status.Store(int64(status))
	u.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		u.hits.Add(1)
		u.mu.Lock()
		u.paths = append(u.paths, r.Method+" "+r.URL.RequestURI())
		u.mu.Unlock()
		if u.hang {
			select {
			case <-r.Context().Done():
			case <-time.After(5 * time.Second):
			}
			return
		}
		w.WriteHeader(int(u.status.Load()))
	}))
	t.Cleanup(u.srv.Close)
	return u
}

func (u *upstream) target(t *testing.T) *contract.Target {
	tg, err := contract.NewTarget(u.srv.URL, 1)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func deps(s *stepSleeper) contract.Deps {
	return contract.Deps{Clock: fixedClock{time.Unix(1000, 0)}, Sleeper: s}
}

func hcRoute(threshold int, targets ...*contract.Target) *contract.Route {
	return &contract.Route{
		Path:        "/api/products",
		Targets:     targets,
		Timeout:     2 * time.Second,
		HealthCheck: &contract.HealthCheck{Path: "/healthz", Interval: 30 * time.Second, UnhealthyThreshold: threshold},
	}
}

func TestStartupRoundProbesEveryTarget(t *testing.T) {
	a, b := newUpstream(t, 200), newUpstream(t, 200)
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, []*contract.Route{hcRoute(3, a.target(t), b.target(t))}, deps(s))
	if d := s.waitRound(t); d != 30*time.Second {
		t.Fatalf("sleep %v want 30s", d)
	}
	for _, u := range []*upstream{a, b} {
		if u.hits.Load() != 1 || u.paths[0] != "GET /healthz" {
			t.Fatalf("hits %d paths %v", u.hits.Load(), u.paths)
		}
	}
	s.next(t)
	if a.hits.Load() != 2 || b.hits.Load() != 2 {
		t.Fatalf("second round hits %d %d", a.hits.Load(), b.hits.Load())
	}
}

func TestThresholdAndRecovery(t *testing.T) {
	a := newUpstream(t, 503)
	tg := a.target(t)
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, []*contract.Route{hcRoute(3, tg)}, deps(s))

	s.waitRound(t)
	s.next(t)
	if !tg.Healthy() || tg.Failures() != 2 {
		t.Fatalf("after 2 failures: healthy %v failures %d", tg.Healthy(), tg.Failures())
	}
	a.status.Store(200)
	s.next(t)
	if !tg.Healthy() || tg.Failures() != 0 {
		t.Fatalf("after success: healthy %v failures %d", tg.Healthy(), tg.Failures())
	}
	a.status.Store(500)
	s.next(t)
	s.next(t)
	if !tg.Healthy() {
		t.Fatal("unhealthy before threshold")
	}
	s.next(t)
	if tg.Healthy() {
		t.Fatal("healthy after 3 failures")
	}
	a.status.Store(204)
	s.next(t)
	if !tg.Healthy() {
		t.Fatal("not recovered after one success")
	}
}

func TestNon2xxAndRedirectFail(t *testing.T) {
	for _, code := range []int{302, 404, 503} {
		a := newUpstream(t, code)
		tg := a.target(t)
		s := newStepSleeper()
		ctx, cancel := context.WithCancel(context.Background())
		Start(ctx, []*contract.Route{hcRoute(1, tg)}, deps(s))
		s.waitRound(t)
		if tg.Healthy() {
			t.Fatalf("status %d counted as success", code)
		}
		cancel()
	}
}

func TestConnectionRefusedFails(t *testing.T) {
	a := newUpstream(t, 200)
	tg := a.target(t)
	a.srv.Close()
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, []*contract.Route{hcRoute(1, tg)}, deps(s))
	s.waitRound(t)
	if tg.Healthy() {
		t.Fatal("refused probe counted as success")
	}
}

func TestProbeTimeout(t *testing.T) {
	a := newUpstream(t, 200)
	a.hang = true
	tg := a.target(t)
	r := hcRoute(1, tg)
	r.Timeout = 50 * time.Millisecond
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	begin := time.Now()
	Start(ctx, []*contract.Route{r}, deps(s))
	s.waitRound(t)
	if tg.Healthy() {
		t.Fatal("timed out probe counted as success")
	}
	if el := time.Since(begin); el > 2*time.Second {
		t.Fatalf("probe took %v", el)
	}
}

func TestProbeTimeoutIsMin(t *testing.T) {
	cases := []struct{ interval, upstream, want time.Duration }{
		{30 * time.Second, 10 * time.Second, 10 * time.Second},
		{2 * time.Second, 10 * time.Second, 2 * time.Second},
		{5 * time.Second, 0, 5 * time.Second},
		{0, 0, defaultInterval},
	}
	for _, c := range cases {
		if got := probeTimeout(c.interval, c.upstream); got != c.want {
			t.Errorf("probeTimeout(%v, %v) = %v want %v", c.interval, c.upstream, got, c.want)
		}
	}
}

func TestProbeURL(t *testing.T) {
	cases := map[[2]string]string{
		{"http://localhost:3003", "/healthz"}:     "http://localhost:3003/healthz",
		{"http://localhost:3003/", "/healthz"}:    "http://localhost:3003/healthz",
		{"http://h:1/base", "/healthz"}:           "http://h:1/base/healthz",
		{"http://h:1/base/", "healthz?deep=1"}:    "http://h:1/base/healthz?deep=1",
		{"http://h:1/base?x=1", "/__mock/health"}: "http://h:1/base/__mock/health?x=1",
	}
	for in, want := range cases {
		u, _ := url.Parse(in[0])
		if got := probeURL(u, in[1]); got != want {
			t.Errorf("probeURL(%q, %q) = %q want %q", in[0], in[1], got, want)
		}
	}
}

func TestRouteWithoutHealthCheckIsNotProbed(t *testing.T) {
	a := newUpstream(t, 500)
	tg := a.target(t)
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	Start(ctx, []*contract.Route{{Path: "/plain", Targets: []*contract.Target{tg}}, nil}, deps(s))
	select {
	case <-s.calls:
		t.Fatal("route without health_check was probed")
	case <-time.After(100 * time.Millisecond):
	}
	if a.hits.Load() != 0 || !tg.Healthy() {
		t.Fatalf("hits %d healthy %v", a.hits.Load(), tg.Healthy())
	}
}

func TestCancelStopsLoop(t *testing.T) {
	a := newUpstream(t, 200)
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	Start(ctx, []*contract.Route{hcRoute(3, a.target(t))}, deps(s))
	s.waitRound(t)
	cancel()
	time.Sleep(50 * time.Millisecond)
	if n := a.hits.Load(); n != 1 {
		t.Fatalf("hits %d after cancel", n)
	}
}

func TestWaitSubtractsRoundDuration(t *testing.T) {
	a := newUpstream(t, 200)
	s := newStepSleeper()
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	clk := &advClock{t: time.Unix(1000, 0), step: 3 * time.Second}
	Start(ctx, []*contract.Route{hcRoute(3, a.target(t))}, contract.Deps{Clock: clk, Sleeper: s})
	if d := s.waitRound(t); d != 27*time.Second {
		t.Fatalf("sleep %v want 27s", d)
	}
}

// advClock advances by step on every Now call.
type advClock struct {
	mu   sync.Mutex
	t    time.Time
	step time.Duration
}

func (c *advClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	now := c.t
	c.t = c.t.Add(c.step)
	return now
}
