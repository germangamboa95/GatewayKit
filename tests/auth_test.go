package e2e

import (
	"context"
	"net/http"
	"reflect"
	"testing"
	"time"
)

// requireAuthError asserts 401 with exactly {"error":"unauthorized","message":msg}.
func requireAuthError(t testing.TB, r *Resp, msg string) {
	t.Helper()
	if r.Status != http.StatusUnauthorized {
		t.Fatalf("status = %d, want 401; body %s", r.Status, r.Body)
	}
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	want := map[string]any{"error": "unauthorized", "message": msg}
	if got := r.Map(t); !reflect.DeepEqual(got, want) {
		t.Errorf("body = %s, want {\"error\":\"unauthorized\",\"message\":%q}", r.Body, msg)
	}
}

// rawHeaderGet sends GET with header name written to the wire as given, without canonicalization.
func rawHeaderGet(t testing.TB, c *Client, path, name, value string) *Resp {
	t.Helper()
	req, err := http.NewRequestWithContext(context.Background(), http.MethodGet, c.Base+path, nil)
	if err != nil {
		t.Fatal(err)
	}
	req.Header[name] = []string{value}
	r, err := c.Do(req)
	if err != nil {
		t.Fatalf("GET %s: %v", path, err)
	}
	return r
}

func requireNoMockRequests(t testing.TB, ports ...int) {
	t.Helper()
	for _, p := range ports {
		if reqs := MockRequests(t, p); len(reqs) != 0 {
			t.Errorf("mock %d got %d requests, want none: %+v", p, len(reqs), reqs)
		}
	}
}

func TestAuthMissingKey(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, 3006)
	c := NewClient(t)

	requireAuthError(t, c.Get("/api/internal"), "missing API key")
	requireAuthError(t, c.Req(http.MethodPost, "/api/internal/jobs", map[string]int{"n": 1}), "missing API key")
	requireAuthError(t, c.Get("/api/internal/x", "Authorization", "sk_live_abc123"), "missing API key")
	requireNoMockRequests(t, 3006)
}

func TestAuthWrongKey(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, 3006)
	c := NewClient(t)

	requireAuthError(t, c.Get("/api/internal/x", "X-API-Key", "sk_live_wrong"), "invalid API key")
	// Equal-length keys, one sharing a long prefix with sk_live_abc123 and one sharing none.
	requireAuthError(t, c.Get("/api/internal/x", "X-API-Key", "sk_live_abc124"), "invalid API key")
	requireAuthError(t, c.Get("/api/internal/x", "X-API-Key", "zzzzzzzzzzzzzz"), "invalid API key")
	requireAuthError(t, c.Get("/api/internal/x", "X-API-Key", "SK_LIVE_ABC123"), "invalid API key")
	requireAuthError(t, c.Get("/api/internal/x", "X-API-Key", "sk_live_abc123x"), "invalid API key")
	requireNoMockRequests(t, 3006)
}

func TestAuthValidKey(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	ResetMocks(t, 3006)
	c := NewClient(t)

	r := rawHeaderGet(t, c, "/api/internal/x", "x-api-key", "sk_live_abc123")
	if r.Status != http.StatusOK {
		t.Fatalf("lowercase x-api-key: status %d, want 200; body %s", r.Status, r.Body)
	}
	if got := r.Header.Get("X-Mock-Service"); got != "internal" {
		t.Errorf("X-Mock-Service = %q, want internal", got)
	}
	if got := LastMockRequest(t, 3006).Headers["x-api-key"]; got != "sk_live_abc123" {
		t.Errorf("upstream x-api-key = %q, want sk_live_abc123", got)
	}

	r = c.Req(http.MethodPost, "/api/internal/jobs", map[string]int{"n": 1}, "X-API-Key", "sk_live_def456")
	if r.Status != http.StatusOK {
		t.Fatalf("POST with sk_live_def456: status %d, want 200; body %s", r.Status, r.Body)
	}
	var e Echo
	r.JSON(t, &e)
	if e.Service != "internal" || e.Method != http.MethodPost || e.Path != "/api/internal/jobs" {
		t.Errorf("echo = %+v", e)
	}
	last := LastMockRequest(t, 3006)
	if got := last.Headers["x-api-key"]; got != "sk_live_def456" {
		t.Errorf("upstream x-api-key = %q, want sk_live_def456", got)
	}
	if n := len(MockRequests(t, 3006)); n != 2 {
		t.Errorf("mock 3006 got %d requests, want 2", n)
	}
}

// Rate limiting runs before auth: failed auth uses quota, and a used-up bucket gives 429 even without a key.
func TestAuthFailedAuthUsesQuota(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3006)
	c := NewClient(t)
	const limit, window = 100, 60 * time.Second

	WaitWindowRoom(t, window, 15*time.Second)
	for i := 1; i <= limit; i++ {
		var r *Resp
		if i%2 == 0 {
			r = c.Get("/api/internal/x", "X-API-Key", "sk_live_wrong")
			requireAuthError(t, r, "invalid API key")
		} else {
			r = c.Get("/api/internal/x")
			requireAuthError(t, r, "missing API key")
		}
		if got := headerInt(t, r, "X-RateLimit-Remaining"); got != int64(limit-i) {
			t.Errorf("401 #%d X-RateLimit-Remaining = %d, want %d", i, got, limit-i)
		}
	}
	requireRateLimited(t, c.Get("/api/internal/x", "X-API-Key", "sk_live_abc123"), limit)
	requireRateLimited(t, c.Get("/api/internal/x"), limit)
	requireRateLimited(t, c.Get("/api/internal/x", "X-API-Key", "sk_live_wrong"), limit)
	requireNoMockRequests(t, 3006)
}

func TestAuthCustomHeaderVariantB(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	ResetMocks(t, 3003, 3004)
	c := NewClient(t)

	requireAuthError(t, c.Get("/v2/admin/x"), "missing API key")
	requireAuthError(t, c.Get("/v2/admin/x", "X-API-Key", "k1"), "missing API key")
	requireAuthError(t, c.Get("/v2/admin/x", "Authorization", "k2"), "invalid API key")
	requireAuthError(t, c.Get("/v2/admin/x", "Authorization", "Bearer k1"), "invalid API key")
	requireNoMockRequests(t, 3003, 3004)

	r := rawHeaderGet(t, c, "/v2/admin/x", "authorization", "k1")
	if r.Status != http.StatusOK {
		t.Fatalf("Authorization: k1: status %d, want 200; body %s", r.Status, r.Body)
	}
	var got []MockRequest
	for _, p := range []int{3003, 3004} {
		got = append(got, MockRequests(t, p)...)
	}
	if len(got) != 1 {
		t.Fatalf("upstreams got %d requests, want 1", len(got))
	}
	if h := got[0].Headers["authorization"]; h != "k1" {
		t.Errorf("upstream authorization = %q, want k1", h)
	}
	if got[0].Path != "/v2/admin/x" {
		t.Errorf("upstream path = %q, want /v2/admin/x", got[0].Path)
	}
}
