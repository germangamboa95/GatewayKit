package main

import (
	"context"
	"encoding/json"
	"io"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"gatewaykit/internal/config"
	"gatewaykit/internal/contract"
	"gatewaykit/internal/server"
)

type seen struct {
	Method string
	URI    string
	Header http.Header
	Body   string
}

type mock struct {
	srv    *httptest.Server
	mu     sync.Mutex
	reqs   []seen
	status int
	header map[string]string
	body   string
}

func newMock(t *testing.T) *mock {
	m := &mock{status: http.StatusOK, body: `{"ok":true}`}
	m.srv = httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		b, _ := io.ReadAll(r.Body)
		m.mu.Lock()
		m.reqs = append(m.reqs, seen{r.Method, r.RequestURI, r.Header.Clone(), string(b)})
		status, header, body := m.status, m.header, m.body
		m.mu.Unlock()
		for k, v := range header {
			w.Header().Set(k, v)
		}
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(status)
		_, _ = io.WriteString(w, body)
	}))
	t.Cleanup(m.srv.Close)
	return m
}

func (m *mock) hits() []seen {
	m.mu.Lock()
	defer m.mu.Unlock()
	return append([]seen(nil), m.reqs...)
}

type sleeps struct {
	mu sync.Mutex
	d  []time.Duration
}

func (s *sleeps) Sleep(ctx context.Context, d time.Duration) error {
	s.mu.Lock()
	s.d = append(s.d, d)
	s.mu.Unlock()
	return ctx.Err()
}

type sample struct {
	h      *server.Server
	mocks  map[string]*mock
	sleeps *sleeps
}

// newSample loads configs/gateway.yaml with each localhost:300N upstream
// replaced by an httptest server.
func newSample(t *testing.T) *sample {
	t.Helper()
	src, err := os.ReadFile("configs/gateway.yaml")
	if err != nil {
		t.Fatal(err)
	}
	s := &sample{mocks: map[string]*mock{}, sleeps: &sleeps{}}
	text := string(src)
	for _, port := range []string{"3001", "3002", "3003", "3004", "3005", "3006"} {
		m := newMock(t)
		s.mocks[port] = m
		text = strings.ReplaceAll(text, "http://localhost:"+port, m.srv.URL)
	}
	cfg := loadText(t, text)
	if len(cfg.Routes) != 5 {
		t.Fatalf("routes = %d, want 5", len(cfg.Routes))
	}
	s.h = buildHandler(cfg, contract.Deps{
		Sleeper: s.sleeps,
		Logger:  discardLogger(),
	})
	return s
}

func loadText(t *testing.T, text string) *contract.GatewayConfig {
	t.Helper()
	p := filepath.Join(t.TempDir(), "gateway.yaml")
	if err := os.WriteFile(p, []byte(text), 0o644); err != nil {
		t.Fatal(err)
	}
	cfg, _, err := config.Load(p)
	if err != nil {
		t.Fatalf("load: %v", err)
	}
	return cfg
}

func do(h http.Handler, method, target, body string, header map[string]string) *httptest.ResponseRecorder {
	var r io.Reader
	if body != "" {
		r = strings.NewReader(body)
	}
	req := httptest.NewRequest(method, target, r)
	for k, v := range header {
		req.Header.Set(k, v)
	}
	rec := httptest.NewRecorder()
	h.ServeHTTP(rec, req)
	return rec
}

func decode(t *testing.T, rec *httptest.ResponseRecorder) map[string]any {
	t.Helper()
	var m map[string]any
	if err := json.Unmarshal(rec.Body.Bytes(), &m); err != nil {
		t.Fatalf("body %q: %v", rec.Body.String(), err)
	}
	return m
}

func TestSampleHealth(t *testing.T) {
	s := newSample(t)
	rec := do(s.h, "GET", "/health", "", nil)
	if rec.Code != 200 {
		t.Fatalf("status = %d", rec.Code)
	}
	if m := decode(t, rec); m["status"] != "healthy" {
		t.Fatalf("body = %v", m)
	}
}

func TestSampleUsersProxy(t *testing.T) {
	s := newSample(t)
	rec := do(s.h, "GET", "/api/users/1?x=1", "", nil)
	if rec.Code != 200 || rec.Body.String() != `{"ok":true}` {
		t.Fatalf("got %d %q", rec.Code, rec.Body.String())
	}
	if got := rec.Header().Get("X-RateLimit-Limit"); got != "30" {
		t.Fatalf("X-RateLimit-Limit = %q", got)
	}
	hits := s.mocks["3001"].hits()
	if len(hits) != 1 || hits[0].URI != "/api/users/1?x=1" {
		t.Fatalf("upstream saw %+v", hits)
	}
}

func TestSampleRouting(t *testing.T) {
	s := newSample(t)
	if rec := do(s.h, "GET", "/nope", "", nil); rec.Code != 404 {
		t.Fatalf("/nope = %d", rec.Code)
	}
	if rec := do(s.h, "GET", "/api/usersx", "", nil); rec.Code != 404 {
		t.Fatalf("/api/usersx = %d", rec.Code)
	}
	rec := do(s.h, "DELETE", "/api/users", "", nil)
	if rec.Code != 405 {
		t.Fatalf("DELETE = %d", rec.Code)
	}
	if allow := rec.Header().Get("Allow"); !strings.Contains(allow, "GET") || !strings.Contains(allow, "POST") {
		t.Fatalf("Allow = %q", allow)
	}
}

func TestSampleProductsWeightedStrip(t *testing.T) {
	s := newSample(t)
	for i := 0; i < 8; i++ {
		if rec := do(s.h, "GET", "/api/products/123", "", nil); rec.Code != 200 {
			t.Fatalf("status = %d", rec.Code)
		}
	}
	a, b := s.mocks["3003"].hits(), s.mocks["3004"].hits()
	if len(a) != 6 || len(b) != 2 {
		t.Fatalf("split = %d/%d, want 6/2", len(a), len(b))
	}
	if a[0].URI != "/123" {
		t.Fatalf("forwarded path = %q", a[0].URI)
	}
}

func TestSampleOrdersRetry(t *testing.T) {
	s := newSample(t)
	s.mocks["3002"].status = 503
	rec := do(s.h, "GET", "/api/orders", "", nil)
	if rec.Code != 503 {
		t.Fatalf("status = %d", rec.Code)
	}
	if n := len(s.mocks["3002"].hits()); n != 3 {
		t.Fatalf("attempts = %d, want 3", n)
	}
	want := []time.Duration{time.Second, 2 * time.Second}
	if len(s.sleeps.d) != 2 || s.sleeps.d[0] != want[0] || s.sleeps.d[1] != want[1] {
		t.Fatalf("delays = %v, want %v", s.sleeps.d, want)
	}
}

func TestSampleLegacyTransforms(t *testing.T) {
	s := newSample(t)
	up := s.mocks["3005"]
	up.header = map[string]string{"Server": "mock", "X-Powered-By": "x"}
	rec := do(s.h, "POST", "/api/legacy/v1/data", `{"userId":7,"userName":"ana"}`, map[string]string{
		"Content-Type": "application/json",
		"X-Debug":      "1",
		"X-Internal":   "1",
	})
	if rec.Code != 200 {
		t.Fatalf("status = %d body %q", rec.Code, rec.Body.String())
	}
	hits := up.hits()
	if len(hits) != 1 {
		t.Fatalf("upstream hits = %d", len(hits))
	}
	in := hits[0]
	if in.URI != "/v1/data" {
		t.Fatalf("forwarded path = %q", in.URI)
	}
	if in.Header.Get("X-Gateway") != "gatewaykit" || in.Header.Get("X-Request-Start") == "" {
		t.Fatalf("added headers missing: %v", in.Header)
	}
	if in.Header.Get("X-Debug") != "" || in.Header.Get("X-Internal") != "" {
		t.Fatalf("removed headers forwarded: %v", in.Header)
	}
	var body struct {
		User map[string]any `json:"user"`
		Meta map[string]any `json:"meta"`
	}
	if err := json.Unmarshal([]byte(in.Body), &body); err != nil {
		t.Fatalf("forwarded body %q: %v", in.Body, err)
	}
	if body.User["id"] != float64(7) || body.User["name"] != "ana" || body.Meta["source"] != "gateway" || body.Meta["timestamp"] == nil {
		t.Fatalf("forwarded body = %s", in.Body)
	}

	if rec.Header().Get("X-Served-By") != "gatewaykit" || rec.Header().Get("Server") != "" || rec.Header().Get("X-Powered-By") != "" {
		t.Fatalf("response headers = %v", rec.Header())
	}
	out := decode(t, rec)
	data, _ := out["data"].(map[string]any)
	meta, _ := out["gateway_metadata"].(map[string]any)
	if data["ok"] != true || meta["route"] != "/api/legacy" || meta["served_at"] == nil {
		t.Fatalf("envelope = %s", rec.Body.String())
	}
}

func TestSampleInternalAuth(t *testing.T) {
	s := newSample(t)
	if rec := do(s.h, "GET", "/api/internal", "", nil); rec.Code != 401 {
		t.Fatalf("no key = %d", rec.Code)
	}
	if rec := do(s.h, "GET", "/api/internal", "", map[string]string{"X-API-Key": "bad"}); rec.Code != 401 {
		t.Fatalf("bad key = %d", rec.Code)
	}
	rec := do(s.h, "GET", "/api/internal/x", "", map[string]string{"X-API-Key": "sk_live_def456"})
	if rec.Code != 200 {
		t.Fatalf("good key = %d", rec.Code)
	}
	if n := len(s.mocks["3006"].hits()); n != 1 {
		t.Fatalf("upstream hits = %d", n)
	}
}

func TestSampleInternalBreaker(t *testing.T) {
	s := newSample(t)
	s.mocks["3006"].status = 500
	key := map[string]string{"X-API-Key": "sk_live_abc123"}
	for i := 0; i < 5; i++ {
		if rec := do(s.h, "GET", "/api/internal", "", key); rec.Code != 500 {
			t.Fatalf("request %d = %d", i, rec.Code)
		}
	}
	rec := do(s.h, "GET", "/api/internal", "", key)
	if rec.Code != 503 {
		t.Fatalf("tripped = %d", rec.Code)
	}
	m := decode(t, rec)
	if m["error"] != "service_unavailable" || m["retry_after"] == nil {
		t.Fatalf("body = %v", m)
	}
	if n := len(s.mocks["3006"].hits()); n != 5 {
		t.Fatalf("upstream hits = %d, want 5", n)
	}
}

func TestEmptyRoutes(t *testing.T) {
	cfg := loadText(t, "gateway:\n  port: 8080\nroutes: []\n")
	h := buildHandler(cfg, contract.Deps{Logger: discardLogger()})
	if rec := do(h, "GET", "/health", "", nil); rec.Code != 200 {
		t.Fatalf("/health = %d", rec.Code)
	}
	for _, p := range []string{"/", "/api/users", "/health/x"} {
		if rec := do(h, "GET", p, "", nil); rec.Code != 404 {
			t.Fatalf("%s = %d", p, rec.Code)
		}
	}
	if rec := do(h, "POST", "/health", "", nil); rec.Code != 404 {
		t.Fatalf("POST /health = %d", rec.Code)
	}
}

func TestMiddlewareNames(t *testing.T) {
	src, err := os.ReadFile("configs/gateway.yaml")
	if err != nil {
		t.Fatal(err)
	}
	cfg := loadText(t, string(src))
	want := map[string]string{
		"/api/users":    "rate_limit",
		"/api/orders":   "rate_limit,retry",
		"/api/products": "rate_limit,health_check",
		"/api/legacy":   "rate_limit,request_transform,response_transform",
		"/api/internal": "rate_limit,auth,circuit_breaker",
	}
	for _, r := range cfg.Routes {
		if got := strings.Join(middlewareNames(r), ","); got != want[r.Path] {
			t.Errorf("%s: %q, want %q", r.Path, got, want[r.Path])
		}
	}
}
