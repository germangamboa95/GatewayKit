package e2e

import (
	"net/http"
	"testing"
	"time"
)

// TestHarnessMock checks the helpers against mock 3001. Without GATEWAY_URL it skips when the mock is down.
func TestHarnessMock(t *testing.T) {
	if _, err := adminClient.Get(MockURL(3001) + "/__mock/requests"); err != nil {
		if GatewayURL() == "" {
			t.Skipf("mock 3001 not reachable: %v", err)
		}
		t.Fatalf("mock 3001 not reachable: %v", err)
	}
	ResetMocks(t, 3001)
	defer ResetMocks(t, 3001)

	a, b := NewClientFor(t, MockURL(3001)), NewClientFor(t, MockURL(3001))
	if SourceIPsUsable() && a.IP == b.IP {
		t.Fatalf("clients share source IP %s", a.IP)
	}
	t.Logf("source IPs %s %s (usable=%v)", a.IP, b.IP, SourceIPsUsable())

	r := a.Req(http.MethodPost, "/x/y?q=1", map[string]int{"n": 1}, "X-Test", "v")
	if r.Status != http.StatusOK {
		t.Fatalf("status %d", r.Status)
	}
	var e Echo
	r.JSON(t, &e)
	if e.Path != "/x/y" || e.Query["q"] != "1" || e.Headers["x-test"] != "v" || e.Method != "POST" {
		t.Errorf("echo = %+v", e)
	}
	if got := r.Header.Get("X-Mock-Service"); got == "" {
		t.Error("X-Mock-Service missing")
	}

	SetMockDefault(t, 3001, 503, 100)
	r = b.Get("/slow")
	if r.Status != 503 {
		t.Errorf("default override status %d", r.Status)
	}
	if r.Elapsed < 100*time.Millisecond {
		t.Errorf("default delay elapsed %v", r.Elapsed)
	}
	SetMockHealth(t, 3001, 503)
	if r := b.Get("/healthz"); r.Status != 503 {
		t.Errorf("health status %d", r.Status)
	}
	RestoreMockHealth(t, 3001, time.Second, func() bool { return b.Get("/healthz").Status == 200 })

	reqs := MockRequests(t, 3001)
	if len(reqs) != 2 || reqs[0].URI != "/x/y?q=1" || reqs[1].Path != "/slow" {
		t.Fatalf("log = %+v", reqs)
	}
	if g := Gaps(reqs); len(g) != 1 || g[0] < 0 {
		t.Errorf("gaps %v", g)
	}
	if LastMockRequest(t, 3001).Path != "/slow" {
		t.Error("last request")
	}

	now := time.Date(2026, 1, 1, 12, 0, 9, 500e6, time.UTC)
	if got := WindowBoundary(now, 10*time.Second); !got.Equal(time.Date(2026, 1, 1, 12, 0, 10, 0, time.UTC)) {
		t.Errorf("boundary %v", got)
	}
	d := Timed(func() { WaitWindowStart(t, 200*time.Millisecond) })
	if d > 300*time.Millisecond {
		t.Errorf("window wait %v", d)
	}
	if ms := time.Now().UnixMilli() % 200; ms < 40 || ms > 120 {
		t.Errorf("after wait, %dms into window", ms)
	}
}

func TestAllocIP(t *testing.T) {
	seen := map[string]bool{}
	for range 300 {
		ip := allocIP()
		if seen[ip] {
			t.Fatalf("duplicate %s", ip)
		}
		seen[ip] = true
	}
}
