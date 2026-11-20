package e2e

import (
	"bytes"
	"encoding/json"
	"math"
	"net/http"
	"strconv"
	"testing"
	"time"
)

// resilRetryAfterRange returns the retry_after bounds for a breaker that opened during
// opened (a response) and was checked during r: ceil of the cooldown left, at least 1.
func resilRetryAfterRange(opened, r *Resp, cooldown time.Duration) (lo, hi int) {
	minSince := r.Start.Sub(opened.Start.Add(opened.Elapsed))
	maxSince := r.Start.Add(r.Elapsed).Sub(opened.Start)
	ceil := func(d time.Duration) int { return max(1, int(math.Ceil(d.Seconds()))) }
	return ceil(cooldown - maxSince), ceil(cooldown - minSince)
}

// resilRequireOpen asserts the breaker's 503: body exactly
// {"error":"service_unavailable","retry_after":N} with lo <= N <= hi and Retry-After: N.
func resilRequireOpen(t testing.TB, r *Resp, lo, hi int) {
	t.Helper()
	if r.Status != http.StatusServiceUnavailable {
		t.Fatalf("status = %d, want 503; body %s", r.Status, r.Body)
	}
	if ct := r.Header.Get("Content-Type"); !isJSONType(ct) {
		t.Errorf("Content-Type = %q, want application/json", ct)
	}
	if s := resilService(r); s != "" {
		t.Errorf("breaker response came from upstream %q", s)
	}
	dec := json.NewDecoder(bytes.NewReader(r.Body))
	dec.UseNumber()
	var m map[string]any
	if err := dec.Decode(&m); err != nil {
		t.Fatalf("decode %q: %v", r.Body, err)
	}
	if len(m) != 2 || m["error"] != "service_unavailable" {
		t.Fatalf("body = %s, want exactly {\"error\":\"service_unavailable\",\"retry_after\":<int>}", r.Body)
	}
	num, ok := m["retry_after"].(json.Number)
	if !ok {
		t.Fatalf("retry_after missing or not a number; body %s", r.Body)
	}
	n, err := strconv.Atoi(num.String())
	if err != nil {
		t.Fatalf("retry_after %q is not an integer", num)
	}
	if n < lo || n > hi {
		t.Errorf("retry_after = %d, want %d to %d", n, lo, hi)
	}
	if h := r.Header.Get("Retry-After"); h != strconv.Itoa(n) {
		t.Errorf("Retry-After = %q, want %q", h, strconv.Itoa(n))
	}
}

func TestBreakerVariantC(t *testing.T) {
	RequireConfig(t, "variant-c.yaml")
	const port = 3006
	const cooldown = 2 * time.Second
	ResetMocks(t, port)

	c, id := NewClient(t), resilID(t)
	path := "/c/breaker/" + id
	get := func(hdr ...string) *Resp { return c.Get(path, append([]string{resilHdr, id}, hdr...)...) }
	upstream := func() int { return len(resilLogged(t, port, id)) }
	t.Cleanup(func() {
		if t.Failed() {
			WaitBreakerCooldown(t, port, cooldown, func() *Resp { return get() })
		}
		ResetMocks(t, port)
	})
	sleepPast := func(r *Resp) { time.Sleep(time.Until(r.Start.Add(r.Elapsed).Add(cooldown + 150*time.Millisecond))) }

	for i := range 5 {
		if r := get("X-Mock-Status", "404"); r.Status != http.StatusNotFound || resilService(r) != "internal" {
			t.Fatalf("404 #%d: status %d service %q; 4xx must not trip the breaker", i+1, r.Status, resilService(r))
		}
	}

	var trip *Resp
	for range 3 {
		trip = get("X-Mock-Status", "503")
		resilUpstreamBody(t, trip, http.StatusServiceUnavailable, "internal")
	}
	sent := upstream()
	if sent != 8 {
		t.Fatalf("upstream got %d requests before open, want 8", sent)
	}

	r := get()
	lo, hi := resilRetryAfterRange(trip, r, cooldown)
	resilRequireOpen(t, r, lo, hi)
	if n := upstream(); n != sent {
		t.Fatalf("open breaker forwarded a request (upstream %d, want %d)", n, sent)
	}

	other := NewClient(t)
	if r := other.Get("/c/unlimited/" + id); r.Status != http.StatusOK || resilService(r) != "users" {
		t.Errorf("/c/unlimited while breaker open: status %d service %q", r.Status, resilService(r))
	}

	sleepPast(trip)
	failed := get("X-Mock-Status", "503")
	resilUpstreamBody(t, failed, http.StatusServiceUnavailable, "internal")
	if n := upstream(); n != sent+1 {
		t.Fatalf("half-open trial: upstream %d, want %d", n, sent+1)
	}
	r = get()
	lo, hi = resilRetryAfterRange(failed, r, cooldown)
	resilRequireOpen(t, r, lo, hi)
	if n := upstream(); n != sent+1 {
		t.Fatalf("reopened breaker forwarded a request (upstream %d)", n)
	}

	sleepPast(failed)
	type res struct {
		r   *Resp
		err error
	}
	done := make(chan res, 1)
	go func() {
		r, err := c.Try(http.MethodGet, path, nil, resilHdr, id, "X-Mock-Delay-Ms", "1000")
		done <- res{r, err}
	}()
	Eventually(t, 900*time.Millisecond, 20*time.Millisecond, "trial at upstream", func() bool { return upstream() == sent+2 })
	resilRequireOpen(t, get(), 1, 1)
	trial := <-done
	if trial.err != nil {
		t.Fatalf("trial: %v", trial.err)
	}
	resilUpstreamBody(t, trial.r, http.StatusOK, "internal")
	if trial.r.Elapsed < time.Second {
		t.Errorf("trial elapsed %v, want at least the 1s mock delay", trial.r.Elapsed)
	}
	if n := upstream(); n != sent+2 {
		t.Fatalf("request during trial reached upstream (upstream %d, want %d)", n, sent+2)
	}

	resilUpstreamBody(t, get("X-Mock-Status", "503"), http.StatusServiceUnavailable, "internal")
	resilUpstreamBody(t, get(), http.StatusOK, "internal")
	if n := upstream(); n != sent+4 {
		t.Fatalf("closed breaker: upstream %d, want %d", n, sent+4)
	}
}

func TestResilRetryAfterRange(t *testing.T) {
	t0 := time.Now()
	opened := &Resp{Start: t0, Elapsed: 10 * time.Millisecond}
	at := func(ms int) *Resp {
		return &Resp{Start: t0.Add(time.Duration(ms) * time.Millisecond), Elapsed: 5 * time.Millisecond}
	}
	for _, tc := range []struct{ ms, lo, hi int }{{20, 2, 2}, {990, 2, 2}, {1000, 1, 2}, {1500, 1, 1}, {2500, 1, 1}} {
		if lo, hi := resilRetryAfterRange(opened, at(tc.ms), 2*time.Second); lo != tc.lo || hi != tc.hi {
			t.Errorf("checked at +%dms: range %d..%d, want %d..%d", tc.ms, lo, hi, tc.lo, tc.hi)
		}
	}
}
