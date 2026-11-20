package e2e

import (
	"encoding/json"
	"fmt"
	"net/http"
	"reflect"
	"strings"
	"testing"
	"time"
)

// resilHdr tags requests so mock log entries can be matched to one test.
const resilHdr = "X-Resil-Id"

func resilID(t testing.TB) string {
	return fmt.Sprintf("%s-%d", strings.ReplaceAll(t.Name(), "/", "-"), time.Now().UnixNano())
}

// resilFilter keeps the mock log entries tagged with id.
func resilFilter(reqs []MockRequest, id string) []MockRequest {
	var out []MockRequest
	for _, r := range reqs {
		if r.Headers[strings.ToLower(resilHdr)] == id {
			out = append(out, r)
		}
	}
	return out
}

func resilLogged(t testing.TB, port int, id string) []MockRequest {
	t.Helper()
	return resilFilter(MockRequests(t, port), id)
}

func resilService(r *Resp) string { return r.Header.Get("X-Mock-Service") }

// resilCheckWaits asserts len(waits)+1 attempts and each gap between w-100ms and 1.8w.
func resilCheckWaits(t testing.TB, reqs []MockRequest, waits ...time.Duration) {
	t.Helper()
	if len(reqs) != len(waits)+1 {
		t.Fatalf("upstream got %d attempts, want %d", len(reqs), len(waits)+1)
	}
	for i, g := range Gaps(reqs) {
		lo, hi := waits[i]-100*time.Millisecond, waits[i]*9/5
		if g < lo || g > hi {
			t.Errorf("wait before attempt %d = %v, want %v (%v to %v)", i+2, g, waits[i], lo, hi)
		}
	}
}

// resilUpstreamBody asserts r is the mock's own response, not a gateway error.
func resilUpstreamBody(t testing.TB, r *Resp, status int, service string) {
	t.Helper()
	if r.Status != status {
		t.Fatalf("status = %d, want %d; body %s", r.Status, status, r.Body)
	}
	if got := resilService(r); got != service {
		t.Errorf("X-Mock-Service = %q, want %q", got, service)
	}
	var e Echo
	r.JSON(t, &e)
	if e.Service != service {
		t.Errorf("body service = %q, want %q; body %s", e.Service, service, r.Body)
	}
}

func TestRetrySampleOrders(t *testing.T) {
	RequireConfig(t, "gateway.yaml")
	const port = 3002
	ResetMocks(t, port)
	t.Cleanup(func() { ResetMocks(t, port) })

	t.Run("fails twice then 200", func(t *testing.T) {
		c, id := NewClient(t), resilID(t)
		r := c.Get("/api/orders/"+id, resilHdr, id, "X-Mock-Fail-Times", "2", "X-Mock-Key", id)
		resilUpstreamBody(t, r, http.StatusOK, "orders")
		resilCheckWaits(t, resilLogged(t, port, id), time.Second, 2*time.Second)
		if r.Elapsed < 2900*time.Millisecond || r.Elapsed > 6*time.Second {
			t.Errorf("elapsed %v, want about 3s", r.Elapsed)
		}
	})

	t.Run("every attempt 503 returns the third", func(t *testing.T) {
		c, id := NewClient(t), resilID(t)
		r := c.Get("/api/orders/"+id, resilHdr, id, "X-Mock-Status", "503")
		resilUpstreamBody(t, r, http.StatusServiceUnavailable, "orders")
		resilCheckWaits(t, resilLogged(t, port, id), time.Second, 2*time.Second)
	})

	t.Run("500 not in on list", func(t *testing.T) {
		c, id := NewClient(t), resilID(t)
		r := c.Get("/api/orders/"+id, resilHdr, id, "X-Mock-Status", "500")
		resilUpstreamBody(t, r, http.StatusInternalServerError, "orders")
		if n := len(resilLogged(t, port, id)); n != 1 {
			t.Errorf("upstream got %d attempts, want 1", n)
		}
	})

	t.Run("POST body resent", func(t *testing.T) {
		c, id := NewClient(t), resilID(t)
		body := map[string]any{"order": float64(7), "items": []any{"a", "b"}}
		r := c.Req(http.MethodPost, "/api/orders/"+id, body, resilHdr, id, "X-Mock-Fail-Times", "1", "X-Mock-Key", id)
		resilUpstreamBody(t, r, http.StatusOK, "orders")
		reqs := resilLogged(t, port, id)
		resilCheckWaits(t, reqs, time.Second)
		for i, q := range reqs {
			if q.Method != http.MethodPost {
				t.Errorf("attempt %d method %s", i+1, q.Method)
			}
			if !reflect.DeepEqual(q.Body, body) {
				got, _ := json.Marshal(q.Body)
				t.Errorf("attempt %d body %s, want %v", i+1, got, body)
			}
		}
	})
}

func TestRetryVariantCExponential(t *testing.T) {
	RequireConfig(t, "variant-c.yaml")
	const port = 3002
	ResetMocks(t, port)
	t.Cleanup(func() { ResetMocks(t, port) })
	SetMockDefault(t, port, http.StatusServiceUnavailable, 0)

	c, id := NewClient(t), resilID(t)
	r := c.Get("/c/retry/"+id, resilHdr, id)
	resilUpstreamBody(t, r, http.StatusServiceUnavailable, "orders")
	resilCheckWaits(t, resilLogged(t, port, id), time.Second, 2*time.Second, 4*time.Second)
	if r.Elapsed < 6900*time.Millisecond || r.Elapsed > 13*time.Second {
		t.Errorf("elapsed %v, want about 7s", r.Elapsed)
	}
}

func TestRetryVariantBOtherTarget(t *testing.T) {
	RequireConfig(t, "variant-b.yaml")
	ResetMocks(t, 3003, 3004)
	t.Cleanup(func() { ResetMocks(t, 3003, 3004) })
	SetMockDefault(t, 3003, http.StatusInternalServerError, 0)

	c, id := NewClient(t), resilID(t)
	for i := range 4 {
		tag := fmt.Sprintf("%s-%d", id, i)
		r := c.Get("/v2/admin/"+tag, "Authorization", "k1", resilHdr, tag)
		a, b := resilLogged(t, 3003, tag), resilLogged(t, 3004, tag)
		if r.Status != http.StatusOK {
			t.Fatalf("try %d: status %d, want 200; body %s", i, r.Status, r.Body)
		}
		if got := resilService(r); got != "products-b" {
			t.Errorf("try %d: X-Mock-Service = %q, want products-b", i, got)
		}
		if len(a) == 0 {
			if len(b) != 1 {
				t.Fatalf("try %d: started on 3004, got %d requests there, want 1", i, len(b))
			}
			continue
		}
		if len(a) != 1 || len(b) != 1 {
			t.Fatalf("try %d: 3003 got %d, 3004 got %d; want 1 each", i, len(a), len(b))
		}
		gap := b[0].Time.Sub(a[0].Time)
		if gap < 100*time.Millisecond || gap > time.Second {
			t.Errorf("retry on 3004 after %v, want at least 100ms (under 1s)", gap)
		}
		return
	}
	t.Fatal("no request started on 3003 in 4 tries")
}
