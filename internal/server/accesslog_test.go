package server

import (
	"bytes"
	"encoding/json"
	"log/slog"
	"net/http"
	"strings"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/auth"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

func stage(name string, calls *[]string, stop *gateway.Response) gateway.Middleware {
	return func(next gateway.Handler) gateway.Handler {
		return func(rc *gateway.RequestContext) *gateway.Response {
			*calls = append(*calls, name)
			if stop != nil {
				return stop
			}
			return next(rc)
		}
	}
}

func TestServerEarlyExitSkipsUpstream(t *testing.T) {
	var calls []string
	r := route("/api/internal", "GET")
	terminal := func(*gateway.RequestContext) *gateway.Response {
		calls = append(calls, "upstream")
		return &gateway.Response{Status: 200}
	}
	chain := gateway.Chain(terminal, stage("auth", &calls, auth.Unauthorized(auth.ErrInvalidAPIKey)), stage("reqtransform", &calls, nil))
	s := New(&config.Gateway{Routes: []*config.Route{r}}, map[*config.Route]gateway.Handler{r: chain}, newClock(), quiet())
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
	chain := func(*gateway.RequestContext) *gateway.Response {
		called = true
		return &gateway.Response{Status: 200}
	}
	s := New(&config.Gateway{Routes: []*config.Route{r}}, map[*config.Route]gateway.Handler{r: chain}, newClock(), quiet())
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
	logger := slog.New(slog.NewJSONHandler(&buf, nil))
	users := route("/api/users", "GET", "POST")
	chain := func(*gateway.RequestContext) *gateway.Response {
		clock.Advance(150 * time.Millisecond)
		return &gateway.Response{Status: 200, Header: http.Header{}, Body: []byte("{}"), FromUpstream: true}
	}
	s := New(&config.Gateway{Routes: []*config.Route{users}}, map[*config.Route]gateway.Handler{users: chain}, clock, logger)

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
