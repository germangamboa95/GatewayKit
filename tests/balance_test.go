package e2e

import (
	"fmt"
	"net/http"
	"strings"
	"testing"
)

// resilPicks sends n tagged GETs and returns the serving mock per response as a string of
// letters ("a" for products-a, "b" for products-b).
func resilPicks(t testing.TB, c *Client, path, id string, n int, hdr ...string) string {
	t.Helper()
	var sb strings.Builder
	for i := range n {
		r := c.Get(fmt.Sprintf("%s%s-%d", path, id, i), append([]string{resilHdr, id}, hdr...)...)
		if r.Status != http.StatusOK {
			t.Fatalf("request %d: status %d; body %s", i, r.Status, r.Body)
		}
		switch resilService(r) {
		case "products-a":
			sb.WriteByte('a')
		case "products-b":
			sb.WriteByte('b')
		default:
			t.Fatalf("request %d: X-Mock-Service %q", i, resilService(r))
		}
	}
	return sb.String()
}

// resilCyclic reports whether got is period repeated, starting at any phase.
func resilCyclic(got, period string) bool {
	for p := range len(period) {
		want := strings.Repeat(period[p:]+period[:p], len(got)/len(period)+1)[:len(got)]
		if got == want {
			return true
		}
	}
	return false
}

func TestBalanceSampleWeighted(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	c, id := NewClient(t), resilID(t)
	seq := resilPicks(t, c, "/api/products/", id, 8)
	if !resilCyclic(seq, "aaba") {
		t.Errorf("pick order %q, want a,a,b,a repeating", seq)
	}
	na, nb := len(resilLogged(t, 3003, id)), len(resilLogged(t, 3004, id))
	if na != 6 || nb != 2 {
		t.Errorf("mock logs: products-a %d, products-b %d; want 6 and 2", na, nb)
	}
}

func TestBalanceRoundRobin(t *testing.T) {
	RequireConfig(t, "variant-b.yaml", "variant-c.yaml")
	path, hdr := "/c/health/", []string(nil)
	if Config() == "variant-b.yaml" {
		path, hdr = "/v2/admin/", []string{"Authorization", "k1"}
	}
	c, id := NewClient(t), resilID(t)
	seq := resilPicks(t, c, path, id, 4, hdr...)
	if !resilCyclic(seq, "ab") {
		t.Errorf("pick order %q, want alternating", seq)
	}
	na, nb := len(resilLogged(t, 3003, id)), len(resilLogged(t, 3004, id))
	if na != 2 || nb != 2 {
		t.Errorf("mock logs: products-a %d, products-b %d; want 2 and 2", na, nb)
	}
}

func TestResilHelpers(t *testing.T) {
	for _, tc := range []struct {
		got, period string
		want        bool
	}{
		{"aabaaaba", "aaba", true}, {"abaaabaa", "aaba", true}, {"baaabaaa", "aaba", true},
		{"aaabaaab", "aaba", true}, {"aabbaaba", "aaba", false}, {"abab", "ab", true},
		{"baba", "ab", true}, {"aabb", "ab", false},
	} {
		if got := resilCyclic(tc.got, tc.period); got != tc.want {
			t.Errorf("resilCyclic(%q, %q) = %v, want %v", tc.got, tc.period, got, tc.want)
		}
	}
}
