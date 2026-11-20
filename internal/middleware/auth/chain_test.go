package auth

import (
	"testing"
	"time"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/middleware/ratelimit"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestFailedAuthUsesQuota(t *testing.T) {
	r := authRoute("x-api-key", "good")
	r.RateLimit = &contract.RateLimit{Requests: 2, Window: time.Minute, Strategy: contract.StrategyFixedWindow, Per: contract.PerIP}
	deps := contract.Deps{Clock: fixedClock{time.Unix(1000000, 0)}}
	u := &upstream{}
	h := ratelimit.New(r, deps)(New(r, deps)(u.handler))
	for range 2 {
		if resp := call(h, map[string]string{"X-Api-Key": "bad"}); resp.Status != 401 {
			t.Fatalf("got %d, want 401", resp.Status)
		}
	}
	if resp := call(h, map[string]string{"X-Api-Key": "good"}); resp.Status != 429 {
		t.Fatalf("got %d, want 429", resp.Status)
	}
	if resp := call(h, nil); resp.Status != 429 {
		t.Fatalf("missing key got %d, want 429", resp.Status)
	}
	if u.hits != 0 {
		t.Fatalf("upstream hits %d", u.hits)
	}
}
