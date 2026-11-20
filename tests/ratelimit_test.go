package e2e

import (
	"math"
	"net/http"
	"slices"
	"strconv"
	"testing"
	"time"
)

var rateLimitHeaders = []string{"X-RateLimit-Limit", "X-RateLimit-Remaining", "X-RateLimit-Reset"}

// headerInt parses a required integer header.
func headerInt(t testing.TB, r *Resp, name string) int64 {
	t.Helper()
	v := r.Header.Get(name)
	if v == "" {
		t.Fatalf("%s missing; status %d, headers %v", name, r.Status, r.Header)
	}
	n, err := strconv.ParseInt(v, 10, 64)
	if err != nil {
		t.Fatalf("%s = %q, not an integer", name, v)
	}
	return n
}

// requireAllowed asserts a 200 carrying X-RateLimit-Limit and X-RateLimit-Remaining; it returns X-RateLimit-Reset.
func requireAllowed(t testing.TB, r *Resp, limit, remaining int) int64 {
	t.Helper()
	if r.Status != http.StatusOK {
		t.Fatalf("status = %d, want 200; body %s", r.Status, r.Body)
	}
	if got := headerInt(t, r, "X-RateLimit-Limit"); got != int64(limit) {
		t.Errorf("X-RateLimit-Limit = %d, want %d", got, limit)
	}
	if got := headerInt(t, r, "X-RateLimit-Remaining"); got != int64(remaining) {
		t.Errorf("X-RateLimit-Remaining = %d, want %d", got, remaining)
	}
	return requireUnixReset(t, r)
}

// requireUnixReset asserts X-RateLimit-Reset is Unix seconds near the request time and returns it.
func requireUnixReset(t testing.TB, r *Resp) int64 {
	t.Helper()
	reset := headerInt(t, r, "X-RateLimit-Reset")
	now := r.Start.Unix()
	if reset < now-1 || reset > now+24*3600 {
		t.Errorf("X-RateLimit-Reset = %d, not Unix seconds near %d", reset, now)
	}
	return reset
}

// requireRateLimited asserts the 429 contract and returns retry_after.
func requireRateLimited(t testing.TB, r *Resp, limit int) int {
	t.Helper()
	if r.Status != http.StatusTooManyRequests {
		t.Fatalf("status = %d, want 429; body %s", r.Status, r.Body)
	}
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	m := r.Map(t)
	if len(m) != 3 {
		t.Errorf("body keys = %v, want exactly error, message, retry_after; body %s", mapKeys(m), r.Body)
	}
	if m["error"] != "rate_limit_exceeded" {
		t.Errorf("error = %v, want rate_limit_exceeded; body %s", m["error"], r.Body)
	}
	if msg, ok := m["message"].(string); !ok || msg == "" {
		t.Errorf("message missing or not a non-empty string; body %s", r.Body)
	}
	f, ok := m["retry_after"].(float64)
	if !ok || f != math.Trunc(f) {
		t.Fatalf("retry_after = %v, want an integer; body %s", m["retry_after"], r.Body)
	}
	ra := int(f)
	if ra < 1 {
		t.Errorf("retry_after = %d, want at least 1", ra)
	}
	if got := headerInt(t, r, "Retry-After"); got != int64(ra) {
		t.Errorf("Retry-After = %d, want retry_after %d", got, ra)
	}
	if got := headerInt(t, r, "X-RateLimit-Limit"); got != int64(limit) {
		t.Errorf("X-RateLimit-Limit = %d, want %d", got, limit)
	}
	if got := r.Header.Get("X-RateLimit-Remaining"); got != "0" {
		t.Errorf("X-RateLimit-Remaining = %q, want 0", got)
	}
	requireUnixReset(t, r)
	return ra
}

func requireNoRateLimitHeaders(t testing.TB, r *Resp) {
	t.Helper()
	for _, h := range rateLimitHeaders {
		if v, ok := r.Header[h]; ok {
			t.Errorf("%s = %v, want absent", h, v)
		}
	}
}

func mapKeys(m map[string]any) []string {
	var ks []string
	for k := range m {
		ks = append(ks, k)
	}
	slices.Sort(ks)
	return ks
}

// fixedReset returns the window boundaries a fixed-window reset may equal for r.
func fixedReset(r *Resp, window time.Duration) (int64, int64) {
	return WindowBoundary(r.Start, window).Unix(), WindowBoundary(r.Start.Add(r.Elapsed), window).Unix()
}

func requireFixedReset(t testing.TB, r *Resp, window time.Duration) {
	t.Helper()
	got := headerInt(t, r, "X-RateLimit-Reset")
	lo, hi := fixedReset(r, window)
	if got != lo && got != hi {
		t.Errorf("X-RateLimit-Reset = %d, want next %v boundary %d", got, window, lo)
	}
}

// requireRetryWithin asserts retry_after is the whole seconds until until, give or take a second.
func requireRetryWithin(t testing.TB, r *Resp, ra int, until time.Time) {
	t.Helper()
	lo := int(until.Sub(r.Start.Add(r.Elapsed)).Seconds()) - 1
	hi := int(math.Ceil(until.Sub(r.Start).Seconds())) + 1
	if ra < max(lo, 1) || ra > hi {
		t.Errorf("retry_after = %d, want between %d and %d", ra, max(lo, 1), hi)
	}
}

func TestRateLimitUsersSliding(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3001)
	c := NewClient(t)
	const limit, window = 30, 60 * time.Second

	first := c.Get("/api/users")
	reset := requireAllowed(t, first, limit, limit-1)
	leave := first.Start.Add(window)
	if reset < leave.Unix()-1 || reset > first.Start.Add(first.Elapsed).Add(window).Unix()+1 {
		t.Errorf("sliding X-RateLimit-Reset = %d, want about %d (first request + 60s)", reset, leave.Unix())
	}
	for i := 2; i <= limit; i++ {
		requireAllowed(t, c.Get("/api/users"), limit, limit-i)
	}
	r := c.Get("/api/users")
	ra := requireRateLimited(t, r, limit)
	requireRetryWithin(t, r, ra, leave)
	if got := headerInt(t, r, "X-RateLimit-Reset"); got < leave.Unix()-1 || got > leave.Unix()+2 {
		t.Errorf("429 X-RateLimit-Reset = %d, want about %d", got, leave.Unix())
	}

	if testing.Short() {
		return
	}
	// A fixed window would reset at the next 60s boundary; a sliding log must not.
	if b := WindowBoundary(first.Start, window).Add(500 * time.Millisecond); b.Before(leave.Add(-2 * time.Second)) {
		time.Sleep(time.Until(b))
		requireRateLimited(t, c.Get("/api/users"), limit)
	}
	time.Sleep(time.Until(leave.Add(-1500 * time.Millisecond)))
	requireRateLimited(t, c.Get("/api/users"), limit)

	time.Sleep(time.Until(first.Start.Add(first.Elapsed).Add(window + 300*time.Millisecond)))
	r = c.Get("/api/users")
	if r.Status != http.StatusOK {
		t.Fatalf("after the first request left the window: status %d, want 200; body %s", r.Status, r.Body)
	}
	if got := headerInt(t, r, "X-RateLimit-Limit"); got != limit {
		t.Errorf("X-RateLimit-Limit = %d, want %d", got, limit)
	}
}

func TestRateLimitOrdersFixed(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3002)
	c := NewClient(t)
	const limit, window = 10, 10 * time.Second

	WaitWindowStart(t, window)
	var r *Resp
	for i := 1; i <= limit; i++ {
		r = c.Get("/api/orders")
		requireAllowed(t, r, limit, limit-i)
		requireFixedReset(t, r, window)
	}
	boundary := WindowBoundary(r.Start, window)
	for range 3 {
		r = c.Get("/api/orders")
		if !r.Start.Add(r.Elapsed).Before(boundary) {
			t.Fatalf("burst crossed the %v boundary; host too slow", window)
		}
		ra := requireRateLimited(t, r, limit)
		requireFixedReset(t, r, window)
		requireRetryWithin(t, r, ra, boundary)
	}
	if n := len(MockRequests(t, 3002)); n != limit {
		t.Errorf("orders mock got %d requests, want %d (429s must not be forwarded)", n, limit)
	}

	// Rejected requests do not extend the block: the next window starts fresh.
	time.Sleep(time.Until(boundary) + 50*time.Millisecond)
	r = c.Get("/api/orders")
	requireAllowed(t, r, limit, limit-1)
	requireFixedReset(t, r, window)
}

func TestRateLimitInheritedSeparateBuckets(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3003, 3004, 3005, 3006)
	c := NewClient(t)
	const limit, window = 100, 60 * time.Second

	WaitWindowRoom(t, window, 15*time.Second)
	var r *Resp
	for i := 1; i <= limit; i++ {
		r = c.Get("/api/products/1")
		requireAllowed(t, r, limit, limit-i)
	}
	requireFixedReset(t, r, window)
	requireRateLimited(t, c.Get("/api/products/1"), limit)

	r = c.Get("/api/legacy/v1/data")
	requireAllowed(t, r, limit, limit-1)
	requireFixedReset(t, r, window)
	r = c.Get("/api/internal/x", "X-API-Key", "sk_live_abc123")
	requireAllowed(t, r, limit, limit-1)
	requireFixedReset(t, r, window)
	requireRateLimited(t, c.Get("/api/products/1"), limit)
}

func TestRateLimitUncountedRequests(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3002)
	c := NewClient(t)
	const limit, window = 10, 10 * time.Second

	WaitWindowRoom(t, window, 5*time.Second)
	start := time.Now()
	for range limit + 1 {
		if r := c.Get("/nope/orders"); r.Status != http.StatusNotFound {
			t.Fatalf("GET /nope/orders: status %d, want 404", r.Status)
		}
		if r := c.Req(http.MethodDelete, "/api/orders", nil); r.Status != http.StatusMethodNotAllowed {
			t.Fatalf("DELETE /api/orders: status %d, want 405", r.Status)
		}
		if r := c.Get("/health"); r.Status != http.StatusOK {
			t.Fatalf("GET /health: status %d, want 200", r.Status)
		}
	}
	r := c.Get("/api/orders")
	if !WindowBoundary(start, window).After(r.Start.Add(r.Elapsed)) {
		t.Fatalf("requests crossed the %v boundary; host too slow", window)
	}
	requireAllowed(t, r, limit, limit-1)
	if n := len(MockRequests(t, 3002)); n != 1 {
		t.Errorf("orders mock got %d requests, want 1", n)
	}
}

func TestRateLimitIgnoresForwardedFor(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3002)
	a, b := NewClient(t), NewClient(t)
	const limit, window = 10, 10 * time.Second

	WaitWindowRoom(t, window, 5*time.Second)
	start := time.Now()
	for i := 1; i <= limit; i++ {
		requireAllowed(t, a.Get("/api/orders", "X-Forwarded-For", "10.1.1.1"), limit, limit-i)
	}
	for _, xff := range []string{"10.9.9.9", b.IP, "203.0.113.7, 10.2.2.2"} {
		requireRateLimited(t, a.Get("/api/orders", "X-Forwarded-For", xff), limit)
	}
	requireRateLimited(t, a.Get("/api/orders"), limit)

	// The bucket is the socket address: another source IP still has full quota.
	r := b.Get("/api/orders", "X-Forwarded-For", a.IP)
	if !WindowBoundary(start, window).After(r.Start.Add(r.Elapsed)) {
		t.Fatalf("requests crossed the %v boundary; host too slow", window)
	}
	requireAllowed(t, r, limit, limit-1)
}

func TestRateLimitGlobalBucketVariantB(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3002)
	a, b := NewClient(t), NewClient(t)
	const limit, window = 2, time.Second

	WaitWindowStart(t, window)
	r1 := a.Get("/v2/a")
	requireAllowed(t, r1, limit, 1)
	requireFixedReset(t, r1, window)
	requireAllowed(t, b.Get("/v2/a"), limit, 0)
	r := b.Get("/v2/a")
	if !WindowBoundary(r1.Start, window).After(r.Start.Add(r.Elapsed)) {
		t.Fatalf("burst crossed the %v boundary; host too slow", window)
	}
	if ra := requireRateLimited(t, r, limit); ra != 1 {
		t.Errorf("retry_after = %d, want 1 for a 1s window", ra)
	}
	requireFixedReset(t, r, window)
	requireRateLimited(t, a.Get("/v2/a"), limit)
	if n := len(MockRequests(t, 3002)); n != 2 {
		t.Errorf("mock 3002 got %d requests, want 2", n)
	}

	WaitWindowStart(t, window)
	requireAllowed(t, b.Get("/v2/a"), limit, 1)
}

func TestRateLimitVariantCLimited(t *testing.T) {
	RequireConfig(t, "variant-c.yaml")
	RequireDistinctIPs(t)
	ResetMocks(t, 3001)
	a, b := NewClient(t), NewClient(t)
	const limit, window = 5, 60 * time.Second

	first := a.Get("/c/limited")
	requireAllowed(t, first, limit, limit-1)
	for i := 2; i <= limit; i++ {
		requireAllowed(t, a.Get("/c/limited"), limit, limit-i)
	}
	r := a.Get("/c/limited")
	ra := requireRateLimited(t, r, limit)
	requireRetryWithin(t, r, ra, first.Start.Add(window))

	for i := 1; i <= limit; i++ {
		requireAllowed(t, b.Get("/c/limited"), limit, limit-i)
	}
	requireRateLimited(t, b.Get("/c/limited"), limit)
	if n := len(MockRequests(t, 3001)); n != 2*limit {
		t.Errorf("mock 3001 got %d requests, want %d", n, 2*limit)
	}
}

func TestRateLimitVariantCUnlimited(t *testing.T) {
	RequireConfig(t, "variant-c.yaml")
	ResetMocks(t, 3001)
	c := NewClient(t)
	const n = 12

	for i := range n {
		r := c.Get("/c/unlimited")
		if r.Status != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200; body %s", i+1, r.Status, r.Body)
		}
		requireNoRateLimitHeaders(t, r)
	}
	if got := len(MockRequests(t, 3001)); got != n {
		t.Errorf("mock 3001 got %d requests, want %d", got, n)
	}
}

// Routes with no limit after the merge are never limited and carry no X-RateLimit-* headers.
func TestRateLimitNoLimitConfigured(t *testing.T) {
	RequireConfig(t, "variant-a.yaml", "variant-b.yaml")
	path, port, hdr := "/anything", 3001, []string(nil)
	if Config() == "variant-b.yaml" {
		path, port, hdr = "/v2/admin/x", 3003, []string{"Authorization", "k1"}
		ResetMocks(t, 3004)
	}
	ResetMocks(t, port)
	c := NewClient(t)

	for i := range 12 {
		r := c.Get(path, hdr...)
		if r.Status != http.StatusOK {
			t.Fatalf("request %d: status %d, want 200; body %s", i+1, r.Status, r.Body)
		}
		requireNoRateLimitHeaders(t, r)
	}
}
