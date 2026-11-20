package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

func stage(name string, calls *[]string, stop *contract.Response) contract.Middleware {
	return func(next contract.Handler) contract.Handler {
		return func(ctx *contract.RequestContext) *contract.Response {
			*calls = append(*calls, name)
			if stop != nil {
				return stop
			}
			return next(ctx)
		}
	}
}

func TestChainOrder(t *testing.T) {
	var calls []string
	terminal := func(*contract.RequestContext) *contract.Response {
		calls = append(calls, "terminal")
		return &contract.Response{Status: 200}
	}
	h := Chain(terminal, stage("ratelimit", &calls, nil), nil, stage("auth", &calls, nil), stage("reqtransform", &calls, nil))
	if resp := h(&contract.RequestContext{}); resp.Status != 200 {
		t.Fatalf("status %d", resp.Status)
	}
	if got := strings.Join(calls, ","); got != "ratelimit,auth,reqtransform,terminal" {
		t.Fatalf("calls %s", got)
	}
}

func TestChainEarlyExit(t *testing.T) {
	var calls []string
	terminal := func(*contract.RequestContext) *contract.Response {
		calls = append(calls, "terminal")
		return &contract.Response{Status: 200}
	}
	h := Chain(terminal,
		stage("ratelimit", &calls, nil),
		stage("auth", &calls, contract.Unauthorized(contract.ErrMissingAPIKey)),
		stage("reqtransform", &calls, nil),
	)
	if resp := h(&contract.RequestContext{}); resp.Status != 401 {
		t.Fatalf("status %d", resp.Status)
	}
	if got := strings.Join(calls, ","); got != "ratelimit,auth" {
		t.Fatalf("calls %s", got)
	}
}

func TestChainNoMiddleware(t *testing.T) {
	h := Chain(reply(204, ""))
	if resp := h(&contract.RequestContext{}); resp.Status != 204 {
		t.Fatalf("status %d", resp.Status)
	}
}

func TestServerEarlyExitSkipsUpstream(t *testing.T) {
	var calls []string
	r := route("/api/internal", "GET")
	terminal := func(*contract.RequestContext) *contract.Response {
		calls = append(calls, "upstream")
		return &contract.Response{Status: 200}
	}
	chain := Chain(terminal, stage("auth", &calls, contract.Unauthorized(contract.ErrInvalidAPIKey)), stage("reqtransform", &calls, nil))
	s := New(&contract.GatewayConfig{Routes: []*contract.Route{r}}, map[*contract.Route]contract.Handler{r: chain}, quietDeps(newClock()))
	rec := serve(s, "GET", "/api/internal", nil)
	if rec.Code != 401 || !strings.Contains(rec.Body.String(), `"error":"unauthorized"`) {
		t.Fatalf("%d %s", rec.Code, rec.Body)
	}
	if got := strings.Join(calls, ","); got != "auth" {
		t.Fatalf("calls %s", got)
	}
}

func TestMethodCheckBeforeChain(t *testing.T) {
	called := false
	r := route("/api/products", "GET")
	chain := func(*contract.RequestContext) *contract.Response {
		called = true
		return &contract.Response{Status: 200}
	}
	s := New(&contract.GatewayConfig{Routes: []*contract.Route{r}}, map[*contract.Route]contract.Handler{r: chain}, quietDeps(newClock()))
	rec := serve(s, "POST", "/api/products", nil)
	if rec.Code != 405 || rec.Header().Get("Allow") != "GET" || called {
		t.Fatalf("%d Allow=%q called=%v", rec.Code, rec.Header().Get("Allow"), called)
	}
}

func logLines(t *testing.T, buf *bytes.Buffer) []map[string]any {
	t.Helper()
	var out []map[string]any
	for _, line := range strings.Split(strings.TrimSpace(buf.String()), "\n") {
		if line == "" {
			continue
		}
		var m map[string]any
		if err := json.Unmarshal([]byte(line), &m); err != nil {
			t.Fatalf("log line %q: %v", line, err)
		}
		out = append(out, m)
	}
	return out
}

func TestAccessLog(t *testing.T) {
	clock := newClock()
	var buf bytes.Buffer
	deps := contract.Deps{Clock: clock, Logger: slog.New(slog.NewJSONHandler(&buf, nil))}
	users := route("/api/users", "GET", "POST")
	chain := func(*contract.RequestContext) *contract.Response {
		clock.Advance(150 * time.Millisecond)
		return &contract.Response{Status: 200, Header: http.Header{}, Body: []byte("{}"), FromUpstream: true}
	}
	s := New(&contract.GatewayConfig{Routes: []*contract.Route{users}}, map[*contract.Route]contract.Handler{users: chain}, deps)

	cases := []struct {
		method, path string
		status       int
		route        string
		duration     time.Duration
	}{
		{"GET", "/api/users/1?x=1", 200, "/api/users", 150 * time.Millisecond},
		{"GET", "/nope", 404, "", 0},
		{"DELETE", "/api/users", 405, "/api/users", 0},
		{"GET", "/health", 200, "", 0},
	}
	for _, c := range cases {
		buf.Reset()
		rec := serve(s, c.method, c.path, nil)
		if rec.Code != c.status {
			t.Fatalf("%s %s: status %d", c.method, c.path, rec.Code)
		}
		lines := logLines(t, &buf)
		if len(lines) != 1 {
			t.Fatalf("%s %s: %d log lines", c.method, c.path, len(lines))
		}
		l := lines[0]
		wantPath, _, _ := strings.Cut(c.path, "?")
		if l["msg"] != "request" || l["method"] != c.method || l["path"] != wantPath ||
			l["status"] != float64(c.status) || l["route"] != c.route ||
			l["duration"] != float64(c.duration) || l["client_ip"] != "192.0.2.1" {
			t.Fatalf("%s %s: log %v", c.method, c.path, l)
		}
	}
}
