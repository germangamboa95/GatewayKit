package auth

import (
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/ratelimit"
)

type fixedClock struct{ t time.Time }

func (c fixedClock) Now() time.Time { return c.t }

func TestFailedAuthUsesQuota(t *testing.T) {
	r := authRoute("x-api-key", "good")
	r.RateLimit = &config.RateLimit{Requests: 2, Window: time.Minute, Strategy: config.StrategyFixedWindow, Per: config.PerIP}
	clk := fixedClock{time.Unix(1000000, 0)}
	u := &upstream{}
	h := ratelimit.Middleware(r.RateLimit, clk)(Middleware(r.Auth)(u.handler))
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
