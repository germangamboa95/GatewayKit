package server

import (
	"bytes"
	"encoding/json"
	"io"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

type fakeClock struct {
	mu sync.Mutex
	t  time.Time
}

func (c *fakeClock) Now() time.Time {
	c.mu.Lock()
	defer c.mu.Unlock()
	return c.t
}

func (c *fakeClock) Advance(d time.Duration) {
	c.mu.Lock()
	c.t = c.t.Add(d)
	c.mu.Unlock()
}

func newClock() *fakeClock {
	return &fakeClock{t: time.Date(2026, 11, 19, 12, 0, 0, 0, time.UTC)}
}

func route(path string, methods ...string) *config.Route {
	r := &config.Route{Path: path, Methods: map[string]bool{}}
	if len(methods) == 0 {
		methods = config.DefaultMethods
	}
	for _, m := range methods {
		r.Methods[m] = true
		r.MethodList = append(r.MethodList, m)
	}
	return r
}

func reply(status int, body string) gateway.Handler {
	return func(*gateway.RequestContext) *gateway.Response {
		return &gateway.Response{Status: status, Header: http.Header{"X-From": {"route"}}, Body: []byte(body), FromUpstream: true}
	}
}

func quiet() *slog.Logger {
	return slog.New(slog.NewTextHandler(io.Discard, nil))
}

func serve(h http.Handler, method, target string, body io.Reader) *httptest.ResponseRecorder {
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, httptest.NewRequest(method, target, body))
	return rec
}

func TestHealth(t *testing.T) {
	clock := newClock()
	s := New(&config.Gateway{}, nil, clock, quiet())
	clock.Advance(5700 * time.Millisecond)
	rec := serve(s, "GET", "/health", nil)
	if rec.Code != 200 {
		t.Fatalf("status %d", rec.Code)
	}
	if ct := rec.Header().Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type %q", ct)
	}
	if got := rec.Body.String(); got != `{"status":"healthy","uptime_seconds":5}` {
		t.Fatalf("body %s", got)
	}
}

func TestHealthEmptyRoutes(t *testing.T) {
	s := New(&config.Gateway{Routes: []*config.Route{}}, nil, newClock(), quiet())
	if rec := serve(s, "GET", "/health", nil); rec.Code != 200 {
		t.Fatalf("health status %d", rec.Code)
	}
	for _, tc := range []struct{ method, path string }{{"GET", "/anything"}, {"HEAD", "/health"}, {"POST", "/health"}} {
		rec := serve(s, tc.method, tc.path, nil)
		if rec.Code != 404 {
			t.Fatalf("%s %s: status %d", tc.method, tc.path, rec.Code)
		}
		if tc.method != "HEAD" && !strings.Contains(rec.Body.String(), `"error":"not_found"`) {
			t.Fatalf("%s %s: body %s", tc.method, tc.path, rec.Body)
		}
	}
}

func TestHealthRouteInteraction(t *testing.T) {
	health := route("/health", "GET", "POST", "HEAD")
	root := route("/")
	cfg := &config.Gateway{Routes: []*config.Route{health, root}}
	chains := map[*config.Route]gateway.Handler{
		health: reply(201, "health-route"),
		root:   reply(202, "root-route"),
	}
	s := New(cfg, chains, newClock(), quiet())
	cases := []struct {
		method, path string
		status       int
		body         string
	}{
		{"GET", "/health", 200, `{"status":"healthy","uptime_seconds":0}`},
		{"GET", "/health?x=1", 200, `{"status":"healthy","uptime_seconds":0}`},
		{"GET", "/health/x", 201, "health-route"},
		{"POST", "/health", 201, "health-route"},
		{"HEAD", "/health", 201, ""},
		{"GET", "/healthz", 202, "root-route"},
		{"GET", "//health", 202, "root-route"},
	}
	for _, c := range cases {
		rec := serve(s, c.method, c.path, nil)
		if rec.Code != c.status || rec.Body.String() != c.body {
			t.Errorf("%s %s: %d %q, want %d %q", c.method, c.path, rec.Code, rec.Body, c.status, c.body)
		}
	}
}

func TestHeadHealthRoutedToRootRoute(t *testing.T) {
	root := route("/", "GET")
	s := New(&config.Gateway{Routes: []*config.Route{root}}, map[*config.Route]gateway.Handler{root: reply(200, "x")}, newClock(), quiet())
	rec := serve(s, "HEAD", "/health", nil)
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET" {
		t.Fatalf("HEAD /health: %d Allow=%q", rec.Code, rec.Header().Get("Allow"))
	}
}

func TestRequestContext(t *testing.T) {
	clock := newClock()
	r := route("/api/users", "POST")
	var got *gateway.RequestContext
	chain := func(rc *gateway.RequestContext) *gateway.Response {
		got = rc
		return reply(200, "ok")(rc)
	}
	s := New(&config.Gateway{Routes: []*config.Route{r}}, map[*config.Route]gateway.Handler{r: chain}, clock, quiet())
	clock.Advance(time.Minute)
	req := httptest.NewRequest("POST", "/api/users/1?x=1", strings.NewReader(`{"a":1}`))
	req.RemoteAddr = "[2001:db8::1]:5555"
	rec := httptest.NewRecorder()
	s.ServeHTTP(rec, req)
	if got == nil {
		t.Fatal("chain not called")
	}
	if got.ClientIP != "2001:db8::1" || got.Route != r || string(got.Body) != `{"a":1}` || got.Req != req {
		t.Fatalf("rc = %+v", got)
	}
	if !got.RequestTime.Equal(clock.Now()) {
		t.Fatalf("RequestTime %v", got.RequestTime)
	}
}

func TestUpstreamPassThrough(t *testing.T) {
	r := route("/api/users")
	chain := func(*gateway.RequestContext) *gateway.Response {
		return &gateway.Response{
			Status:       500,
			Header:       http.Header{"Content-Type": {"text/plain"}, "X-Upstream": {"a", "b"}, "Content-Length": {"999"}},
			Body:         []byte("boom"),
			FromUpstream: true,
		}
	}
	s := New(&config.Gateway{Routes: []*config.Route{r}}, map[*config.Route]gateway.Handler{r: chain}, newClock(), quiet())
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/api/users")
	if err != nil {
		t.Fatal(err)
	}
	defer resp.Body.Close()
	b, _ := io.ReadAll(resp.Body)
	if resp.StatusCode != 500 || string(b) != "boom" || resp.Header.Get("Content-Type") != "text/plain" {
		t.Fatalf("%d %q %v", resp.StatusCode, b, resp.Header)
	}
	if v := resp.Header.Values("X-Upstream"); len(v) != 2 {
		t.Fatalf("X-Upstream %v", v)
	}
	if resp.ContentLength != 4 {
		t.Fatalf("Content-Length %d", resp.ContentLength)
	}
}

func TestLargeBodyAndHead(t *testing.T) {
	r := route("/big")
	big := bytes.Repeat([]byte("x"), 100000)
	chain := func(rc *gateway.RequestContext) *gateway.Response {
		resp := &gateway.Response{Status: 200, Header: http.Header{"Content-Length": {"100000"}}}
		if rc.Req.Method != "HEAD" {
			resp.Body = big
		}
		return resp
	}
	s := New(&config.Gateway{Routes: []*config.Route{r}}, map[*config.Route]gateway.Handler{r: chain}, newClock(), quiet())
	srv := httptest.NewServer(s)
	defer srv.Close()
	resp, err := http.Get(srv.URL + "/big")
	if err != nil {
		t.Fatal(err)
	}
	b, _ := io.ReadAll(resp.Body)
	resp.Body.Close()
	if len(b) != len(big) || resp.ContentLength != int64(len(big)) {
		t.Fatalf("GET len %d cl %d", len(b), resp.ContentLength)
	}
	resp, err = http.Head(srv.URL + "/big")
	if err != nil {
		t.Fatal(err)
	}
	resp.Body.Close()
	if resp.ContentLength != 100000 {
		t.Fatalf("HEAD Content-Length %d", resp.ContentLength)
	}
}

func TestGatewayErrors(t *testing.T) {
	r := route("/nil")
	p := route("/panic")
	missing := route("/missing")
	chains := map[*config.Route]gateway.Handler{
		r: func(*gateway.RequestContext) *gateway.Response { return nil },
		p: func(*gateway.RequestContext) *gateway.Response { panic("bug") },
	}
	s := New(&config.Gateway{Routes: []*config.Route{r, p, missing}}, chains, newClock(), quiet())
	for _, path := range []string{"/nil", "/panic", "/missing"} {
		rec := serve(s, "GET", path, nil)
		var body map[string]any
		if err := json.Unmarshal(rec.Body.Bytes(), &body); err != nil || rec.Code != 502 || body["error"] != "bad_gateway" {
			t.Fatalf("%s: %d %s", path, rec.Code, rec.Body)
		}
	}
}

func TestClientIP(t *testing.T) {
	for in, want := range map[string]string{"10.0.0.1:80": "10.0.0.1", "[::1]:80": "::1", "bogus": "bogus"} {
		if got := ClientIP(in); got != want {
			t.Errorf("ClientIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestNilConfig(t *testing.T) {
	s := New(nil, nil, nil, quiet())
	if rec := serve(s, "GET", "/x", nil); rec.Code != 404 {
		t.Fatalf("status %d", rec.Code)
	}
}
