package e2e

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/url"
	"reflect"
	"testing"
	"time"
)

var allConfigs = []string{"gateway.yaml", "variant-a.yaml", "variant-b.yaml", "variant-c.yaml"}

// paceB waits for a fresh 1s window on variant B, whose /v2 route allows 2 requests per 1s globally.
func paceB(t *testing.T) {
	t.Helper()
	if Config() == "variant-b.yaml" {
		WaitWindowStart(t, time.Second)
	}
}

func requestsOn(t *testing.T, ports ...int) []MockRequest {
	t.Helper()
	var all []MockRequest
	for _, p := range ports {
		all = append(all, MockRequests(t, p)...)
	}
	return all
}

func requireUpstream(t *testing.T, port int, method, uri string) MockRequest {
	t.Helper()
	reqs := MockRequests(t, port)
	if len(reqs) != 1 {
		t.Fatalf("mock %d got %d requests, want 1: %+v", port, len(reqs), reqs)
	}
	if reqs[0].Method != method || reqs[0].URI != uri {
		t.Fatalf("mock %d got %s %s, want %s %s", port, reqs[0].Method, reqs[0].URI, method, uri)
	}
	return reqs[0]
}

func TestCoreHealth(t *testing.T) {
	RequireConfig(t, allConfigs...)
	ResetMocks(t)
	c := NewClient(t)
	r := c.Get("/health")
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, body %s", r.Status, r.Body)
	}
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode %s: %v", r.Body, err)
	}
	if len(m) != 2 || m["status"] != "healthy" {
		t.Errorf("body = %s, want exactly status and uptime_seconds", r.Body)
	}
	n, ok := m["uptime_seconds"].(json.Number)
	if !ok {
		t.Fatalf("uptime_seconds = %v, want an integer", m["uptime_seconds"])
	}
	if up, err := n.Int64(); err != nil || up < 0 {
		t.Errorf("uptime_seconds = %s, want a non-negative integer", n)
	}
	for _, p := range MockPorts {
		if reqs := MockRequests(t, p); len(reqs) != 0 {
			t.Errorf("GET /health reached mock %d: %+v", p, reqs)
		}
	}
}

func TestCoreHeadHealthRouted(t *testing.T) {
	RequireConfig(t, allConfigs...)
	ResetMocks(t)
	c := NewClient(t)
	r := c.Req(http.MethodHead, "/health", nil)
	if Config() == "variant-a.yaml" {
		if r.Status != http.StatusOK || r.Header.Get("X-Mock-Service") != "users" {
			t.Fatalf("HEAD /health = %d (X-Mock-Service %q), want 200 proxied to users", r.Status, r.Header.Get("X-Mock-Service"))
		}
		requireUpstream(t, 3001, http.MethodHead, "/health")
		return
	}
	if r.Status != http.StatusNotFound {
		t.Fatalf("HEAD /health = %d, want 404 from routing", r.Status)
	}
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
}

type passCase struct {
	prefix, upstreamPrefix, service string
	port                            int
}

func passThroughCase() passCase {
	switch Config() {
	case "variant-a.yaml":
		return passCase{"", "", "users", 3001}
	case "variant-c.yaml":
		return passCase{"/c/unlimited", "/c/unlimited", "users", 3001}
	}
	return passCase{"/api/users", "/api/users", "users", 3001}
}

func TestCoreProxyPassThrough(t *testing.T) {
	RequireConfig(t, "gateway.yaml", "variant-a.yaml", "variant-c.yaml")
	pc := passThroughCase()
	c := NewClient(t)

	t.Run("status headers body", func(t *testing.T) {
		ResetMocks(t, pc.port)
		body := map[string]any{"a": float64(1), "b": "two"}
		r := c.Req(http.MethodPost, pc.prefix+"/echo?x=1", body, "X-Custom", "1", "X-Mock-Status", "201")
		if r.Status != http.StatusCreated {
			t.Fatalf("status = %d, want 201; body %s", r.Status, r.Body)
		}
		if got := r.Header.Get("X-Mock-Service"); got != pc.service {
			t.Errorf("X-Mock-Service = %q, want %q", got, pc.service)
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/json" {
			t.Errorf("Content-Type = %q, want application/json", ct)
		}
		var e Echo
		r.JSON(t, &e)
		if e.Method != http.MethodPost || e.Path != pc.upstreamPrefix+"/echo" || e.Query["x"] != "1" {
			t.Errorf("echo = %s %s %v", e.Method, e.Path, e.Query)
		}
		if e.Headers["x-custom"] != "1" {
			t.Errorf("x-custom = %q, want 1", e.Headers["x-custom"])
		}
		if !reflect.DeepEqual(e.Body, body) {
			t.Errorf("body = %v, want %v", e.Body, body)
		}
		got := requireUpstream(t, pc.port, http.MethodPost, pc.upstreamPrefix+"/echo?x=1")
		if !reflect.DeepEqual(got.Body, body) {
			t.Errorf("upstream body = %v, want %v", got.Body, body)
		}
	})

	t.Run("upstream error relayed", func(t *testing.T) {
		ResetMocks(t, pc.port)
		r := c.Get(pc.prefix+"/broken", "X-Mock-Status", "500", "X-Mock-Body", "upstream broke")
		if r.Status != http.StatusInternalServerError {
			t.Fatalf("status = %d, want 500", r.Status)
		}
		if string(r.Body) != "upstream broke" {
			t.Errorf("body = %q, want the upstream text", r.Body)
		}
		if ct := r.Header.Get("Content-Type"); ct != "text/plain; charset=utf-8" {
			t.Errorf("Content-Type = %q, want the upstream's", ct)
		}
		if r.Header.Get("X-Mock-Service") != pc.service {
			t.Errorf("X-Mock-Service = %q, want %q", r.Header.Get("X-Mock-Service"), pc.service)
		}
		requireUpstream(t, pc.port, http.MethodGet, pc.upstreamPrefix+"/broken")
	})

	t.Run("forwarded headers", func(t *testing.T) {
		ResetMocks(t, pc.port)
		r := c.Get(pc.prefix+"/hdr",
			"Host", "gateway.example:8080",
			"X-Forwarded-For", "1.2.3.4",
			"Keep-Alive", "timeout=5",
			"Proxy-Authorization", "x",
			"Upgrade", "websocket")
		if r.Status != http.StatusOK {
			t.Fatalf("status = %d; body %s", r.Status, r.Body)
		}
		h := requireUpstream(t, pc.port, http.MethodGet, pc.upstreamPrefix+"/hdr").Headers
		if h["host"] != "localhost:3001" {
			t.Errorf("host = %q, want localhost:3001", h["host"])
		}
		if h["x-forwarded-proto"] != "http" {
			t.Errorf("x-forwarded-proto = %q, want http", h["x-forwarded-proto"])
		}
		if h["x-forwarded-host"] != "gateway.example:8080" {
			t.Errorf("x-forwarded-host = %q, want gateway.example:8080", h["x-forwarded-host"])
		}
		if SourceIPsUsable() {
			if want := "1.2.3.4, " + c.IP; h["x-forwarded-for"] != want {
				t.Errorf("x-forwarded-for = %q, want %q", h["x-forwarded-for"], want)
			}
		}
		for _, k := range []string{"keep-alive", "proxy-authorization", "upgrade"} {
			if v, ok := h[k]; ok {
				t.Errorf("hop-by-hop %s = %q forwarded", k, v)
			}
		}
	})
}

func TestCoreStripPrefix(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	c := NewClient(t)
	cases := []struct {
		name, path string
		ports      []int
		uri        string
	}{
		{"strip subpath", "/api/products/123?x=1", []int{3003, 3004}, "/123?x=1"},
		{"strip exact", "/api/products", []int{3003, 3004}, "/"},
		{"strip exact with query", "/api/products?x=1", []int{3003, 3004}, "/?x=1"},
		{"legacy strip", "/api/legacy/v1/data", []int{3005}, "/v1/data"},
		{"no strip subpath", "/api/users/42?x=1", []int{3001}, "/api/users/42?x=1"},
		{"no strip exact", "/api/users", []int{3001}, "/api/users"},
		{"no strip trailing slash", "/api/users/", []int{3001}, "/api/users/"},
		{"no strip orders", "/api/orders/7", []int{3002}, "/api/orders/7"},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			ResetMocks(t, tc.ports...)
			r := c.Get(tc.path)
			if r.Status != http.StatusOK {
				t.Fatalf("GET %s = %d; body %s", tc.path, r.Status, r.Body)
			}
			reqs := requestsOn(t, tc.ports...)
			if len(reqs) != 1 || reqs[0].URI != tc.uri {
				t.Fatalf("GET %s reached %+v, want one request to %s", tc.path, reqs, tc.uri)
			}
		})
	}
}

func TestCoreBasePath(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	c := NewClient(t)
	cases := []struct {
		method, path, uri string
	}{
		{http.MethodDelete, "/v2/a", "/base/a"},
		{http.MethodGet, "/v2/a/b?q=1", "/base/a/b?q=1"},
	}
	for _, tc := range cases {
		t.Run(tc.method+" "+tc.path, func(t *testing.T) {
			ResetMocks(t, 3002)
			paceB(t)
			r := c.Req(tc.method, tc.path, nil)
			if r.Status != http.StatusOK || r.Header.Get("X-Mock-Service") != "orders" {
				t.Fatalf("%s %s = %d (X-Mock-Service %q); body %s", tc.method, tc.path, r.Status, r.Header.Get("X-Mock-Service"), r.Body)
			}
			requireUpstream(t, 3002, tc.method, tc.uri)
		})
	}
}

func TestCoreNotFound(t *testing.T) {
	RequireConfig(t, "gateway.yaml", "variant-b.yaml", "variant-c.yaml")
	ResetMocks(t)
	c := NewClient(t)
	paths := map[string][]string{
		"gateway.yaml":   {"/api/usersX", "/API/users", "/api", "/nope", "/health/x"},
		"variant-b.yaml": {"/v2x", "/V2/a", "/v", "/"},
		"variant-c.yaml": {"/c/limitedX", "/c", "/c/unlimitedfoo"},
	}[Config()]
	for _, p := range paths {
		RequireError(t, c.Get(p), http.StatusNotFound, "not_found")
	}
	for _, port := range MockPorts {
		if reqs := MockRequests(t, port); len(reqs) != 0 {
			t.Errorf("404 requests reached mock %d: %+v", port, reqs)
		}
	}
}

func TestCoreMethodNotAllowed(t *testing.T) {
	RequireConfig(t, "gateway.yaml", "variant-b.yaml")
	ResetMocks(t)
	c := NewClient(t)
	type tc struct{ method, path, allow string }
	cases := map[string][]tc{
		"gateway.yaml": {
			{http.MethodDelete, "/api/users", "GET, POST"},
			{http.MethodPatch, "/api/orders/1", "GET, POST, PUT"},
			{http.MethodPost, "/api/products/1", "GET"},
			{http.MethodPut, "/api/internal", "GET, POST"},
			{http.MethodHead, "/api/products", "GET"},
		},
		"variant-b.yaml": {
			{http.MethodPost, "/v2/a", "GET, DELETE"},
			{http.MethodHead, "/v2/a", "GET, DELETE"},
		},
	}[Config()]
	for _, k := range cases {
		r := c.Req(k.method, k.path, nil)
		if got := r.Header.Get("Allow"); got != k.allow {
			t.Errorf("%s %s Allow = %q, want %q", k.method, k.path, got, k.allow)
		}
		if k.method == http.MethodHead {
			if r.Status != http.StatusMethodNotAllowed {
				t.Errorf("HEAD %s = %d, want 405", k.path, r.Status)
			}
			continue
		}
		RequireError(t, r, http.StatusMethodNotAllowed, "method_not_allowed")
	}
	for _, port := range MockPorts {
		if reqs := MockRequests(t, port); len(reqs) != 0 {
			t.Errorf("405 requests reached mock %d: %+v", port, reqs)
		}
	}
}

func TestCoreLongestPrefix(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	ResetMocks(t, 3002, 3003, 3004)
	c := NewClient(t)
	r := c.Get("/v2/admin/x", "Authorization", "k1")
	if r.Status != http.StatusOK {
		t.Fatalf("GET /v2/admin/x = %d; body %s", r.Status, r.Body)
	}
	if reqs := MockRequests(t, 3002); len(reqs) != 0 {
		t.Errorf("/v2 upstream got %+v; /v2/admin should win", reqs)
	}
	reqs := requestsOn(t, 3003, 3004)
	if len(reqs) != 1 || reqs[0].URI != "/v2/admin/x" {
		t.Fatalf("/v2/admin upstreams got %+v, want one GET /v2/admin/x", reqs)
	}

	ResetMocks(t, 3002, 3003, 3004)
	paceB(t)
	if r := c.Get("/v2/adminX"); r.Status != http.StatusOK {
		t.Fatalf("GET /v2/adminX = %d; body %s", r.Status, r.Body)
	}
	requireUpstream(t, 3002, http.MethodGet, "/base/adminX")
	if n := len(requestsOn(t, 3003, 3004)); n != 0 {
		t.Errorf("/v2/adminX reached /v2/admin upstreams %d times", n)
	}
}

func TestCoreVariantADefaults(t *testing.T) {
	RequireConfig(t, "variant-a.yaml")
	c := NewClient(t)
	cases := []struct{ method, path string }{
		{http.MethodGet, "/anything"},
		{http.MethodGet, "/anything/at/all?k=v"},
		{http.MethodPost, "/p"},
		{http.MethodPut, "/p"},
		{http.MethodPatch, "/p"},
		{http.MethodDelete, "/p"},
		{http.MethodOptions, "/p"},
		{http.MethodHead, "/p"},
	}
	for _, tc := range cases {
		ResetMocks(t, 3001)
		r := c.Req(tc.method, tc.path, nil)
		if r.Status != http.StatusOK || r.Header.Get("X-Mock-Service") != "users" {
			t.Errorf("%s %s = %d (X-Mock-Service %q), want 200 from users", tc.method, tc.path, r.Status, r.Header.Get("X-Mock-Service"))
			continue
		}
		for _, h := range []string{"X-Ratelimit-Limit", "X-Ratelimit-Remaining", "X-Ratelimit-Reset"} {
			if v := r.Header.Get(h); v != "" {
				t.Errorf("%s %s has %s: %s with no rate limit configured", tc.method, tc.path, h, v)
			}
		}
		requireUpstream(t, 3001, tc.method, tc.path)
	}
}

func TestCoreVariantBPort(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	u, err := url.Parse(GatewayURL())
	if err != nil {
		t.Fatal(err)
	}
	if u.Port() != "9090" {
		t.Fatalf("GATEWAY_URL = %s, want port 9090 for variant B", GatewayURL())
	}
	if r := NewClient(t).Get("/health"); r.Status != http.StatusOK {
		t.Fatalf("GET :9090/health = %d", r.Status)
	}
}

func TestCoreVariantBRouteTimeout(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	c := NewClient(t)

	ResetMocks(t, 3002)
	paceB(t)
	r := c.Get("/v2/slow", "X-Mock-Delay-Ms", "1000")
	if r.Status != http.StatusOK || r.Header.Get("X-Mock-Service") != "orders" {
		t.Fatalf("1s upstream = %d (X-Mock-Service %q), want 200 proxied: 2s route timeout beats 750ms global; body %s",
			r.Status, r.Header.Get("X-Mock-Service"), r.Body)
	}
	if r.Elapsed < time.Second {
		t.Errorf("elapsed %v, want at least the 1s upstream delay", r.Elapsed)
	}
	requireUpstream(t, 3002, http.MethodGet, "/base/slow")

	ResetMocks(t, 3002)
	paceB(t)
	r = c.Get("/v2/slower", "X-Mock-Delay-Ms", "2600")
	RequireError(t, r, http.StatusGatewayTimeout, "gateway_timeout")
	Within(t, "504 elapsed", r.Elapsed, 2*time.Second, 400*time.Millisecond)
}

func TestCoreVariantCTimeout(t *testing.T) {
	RequireConfig(t, "variant-c.yaml")
	ResetMocks(t, 3001)
	c := NewClient(t)
	r := c.Get("/c/unlimited/slow", "X-Mock-Delay-Ms", "2500")
	RequireError(t, r, http.StatusGatewayTimeout, "gateway_timeout")
	Within(t, "504 elapsed", r.Elapsed, 2*time.Second, 400*time.Millisecond)
	requireUpstream(t, 3001, http.MethodGet, "/c/unlimited/slow")
}
