package e2e

import (
	"net/http"
	"testing"
	"time"
)

func TestHealthVariantC(t *testing.T) {
	RequireConfig(t, "variant-c.yaml")
	const a, b = 3003, 3004
	ResetMocks(t, a, b)

	c, id := NewClient(t), resilID(t)
	hit := func() *Resp { return c.Get("/c/health/"+id, resilHdr, id) }
	only := func(service string, n int) bool {
		for range n {
			r := hit()
			if r.Status != http.StatusOK || resilService(r) != service {
				return false
			}
		}
		return true
	}
	bothServe := func() bool {
		r1, r2 := hit(), hit()
		return r1.Status == http.StatusOK && r2.Status == http.StatusOK && resilService(r1) != resilService(r2)
	}
	t.Cleanup(func() {
		SetMockHealth(t, a, http.StatusOK)
		RestoreMockHealth(t, b, 5*time.Second, bothServe)
		ResetMocks(t, a, b)
	})
	Eventually(t, 5*time.Second, 100*time.Millisecond, "both targets in rotation", bothServe)

	t.Log("products-b goes unhealthy, then recovers")
	SetMockHealth(t, b, http.StatusServiceUnavailable)
	start := time.Now()
	Eventually(t, 4*time.Second, 50*time.Millisecond, "products-b out of rotation", func() bool { return only("products-a", 2) })
	if d := time.Since(start); d < 800*time.Millisecond {
		t.Errorf("products-b removed after %v; threshold 2 at 1s interval needs about 1s", d)
	}
	before := len(resilLogged(t, b, id))
	if !only("products-a", 6) {
		t.Fatal("traffic reached products-b while unhealthy")
	}
	if n := len(resilLogged(t, b, id)); n != before {
		t.Fatalf("products-b log grew from %d to %d while unhealthy", before, n)
	}

	SetMockHealth(t, b, http.StatusOK)
	start = time.Now()
	Eventually(t, 3*time.Second, 50*time.Millisecond, "products-b back in rotation", func() bool {
		r := hit()
		return r.Status == http.StatusOK && resilService(r) == "products-b"
	})
	t.Logf("products-b recovered after %v", time.Since(start))
	got := map[string]int{}
	for range 4 {
		r := hit()
		if r.Status != http.StatusOK {
			t.Fatalf("status %d; body %s", r.Status, r.Body)
		}
		got[resilService(r)]++
	}
	if got["products-a"] != 2 || got["products-b"] != 2 {
		t.Errorf("4 requests after recovery = %v, want 2 each", got)
	}

	t.Log("every target unhealthy")
	SetMockHealth(t, a, http.StatusServiceUnavailable)
	SetMockHealth(t, b, http.StatusServiceUnavailable)
	var last *Resp
	Eventually(t, 4*time.Second, 50*time.Millisecond, "no healthy target", func() bool {
		last = hit()
		return last.Status == http.StatusServiceUnavailable && resilService(last) == ""
	})
	RequireError(t, last, http.StatusServiceUnavailable, "no_healthy_upstream")
	na, nb := len(resilLogged(t, a, id)), len(resilLogged(t, b, id))
	RequireError(t, hit(), http.StatusServiceUnavailable, "no_healthy_upstream")
	if len(resilLogged(t, a, id)) != na || len(resilLogged(t, b, id)) != nb {
		t.Error("request reached a target while all were unhealthy")
	}

	SetMockHealth(t, a, http.StatusOK)
	SetMockHealth(t, b, http.StatusOK)
	Eventually(t, 3*time.Second, 50*time.Millisecond, "both targets back", bothServe)
}
