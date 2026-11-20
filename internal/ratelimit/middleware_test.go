package ratelimit

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/auth"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

type fakeClock struct{ t time.Time }

func (c *fakeClock) Now() time.Time { return c.t }

type counter struct{ n int }

func (c *counter) handler(rc *gateway.RequestContext) *gateway.Response {
	c.n++
	return &gateway.Response{Status: 200, Header: http.Header{"X-Up": {"1"}}, Body: []byte("ok"), FromUpstream: true}
}

func reqFrom(addr string, xff string) *gateway.RequestContext {
	r := httptest.NewRequest("GET", "/api", nil)
	r.RemoteAddr = addr
	if xff != "" {
		r.Header.Set("X-Forwarded-For", xff)
	}
	return &gateway.RequestContext{Req: r, ClientIP: "9.9.9.9"}
}

func build(rlc *config.RateLimit, clk *fakeClock) (gateway.Handler, *counter) {
	c := &counter{}
	h := Middleware(rlc, clk)(c.handler)
	return h, c
}

func TestAllowedHeaders(t *testing.T) {
	clk := &fakeClock{time.Unix(1000003, 0)}
	h, c := build(&config.RateLimit{Requests: 30, Window: 60 * time.Second, Strategy: config.StrategySlidingWindow, Per: config.PerIP}, clk)
	resp := h(reqFrom("1.2.3.4:5555", ""))
	if resp.Status != 200 || c.n != 1 || !resp.FromUpstream {
		t.Fatalf("status %d hits %d", resp.Status, c.n)
	}
	if resp.Header.Get("X-RateLimit-Limit") != "30" || resp.Header.Get("X-RateLimit-Remaining") != "29" {
		t.Fatalf("headers %v", resp.Header)
	}
	if resp.Header.Get("X-RateLimit-Reset") != "1000063" {
		t.Fatalf("reset %q", resp.Header.Get("X-RateLimit-Reset"))
	}
	if resp.Header.Get("X-Up") != "1" {
		t.Fatal("upstream header lost")
	}
}

func TestFixedResetHeader(t *testing.T) {
	clk := &fakeClock{time.Unix(1000003, 0)}
	h, _ := build(&config.RateLimit{Requests: 10, Window: 10 * time.Second, Strategy: config.StrategyFixedWindow, Per: config.PerIP}, clk)
	resp := h(reqFrom("1.2.3.4:5555", ""))
	if got := resp.Header.Get("X-RateLimit-Reset"); got != "1000010" {
		t.Fatalf("reset %q", got)
	}
}

func TestRejection(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	h, c := build(&config.RateLimit{Requests: 10, Window: 10 * time.Second, Strategy: config.StrategyFixedWindow, Per: config.PerIP}, clk)
	for range 10 {
		h(reqFrom("1.2.3.4:5555", ""))
	}
	clk.t = time.Unix(1000006, 0)
	resp := h(reqFrom("1.2.3.4:6666", ""))
	if resp.Status != 429 || c.n != 10 {
		t.Fatalf("status %d hits %d", resp.Status, c.n)
	}
	if resp.Header.Get("Content-Type") != "application/json" {
		t.Fatalf("content type %q", resp.Header.Get("Content-Type"))
	}
	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatal(err)
	}
	if body["error"] != "rate_limit_exceeded" || body["message"] == "" || body["retry_after"] != float64(4) {
		t.Fatalf("body %s", resp.Body)
	}
	if resp.Header.Get("Retry-After") != "4" {
		t.Fatalf("Retry-After %q", resp.Header.Get("Retry-After"))
	}
	if resp.Header.Get("X-RateLimit-Limit") != "10" || resp.Header.Get("X-RateLimit-Remaining") != "0" || resp.Header.Get("X-RateLimit-Reset") != "1000010" {
		t.Fatalf("headers %v", resp.Header)
	}
	if resp.FromUpstream {
		t.Fatal("429 must not be marked as upstream")
	}
	clk.t = time.Unix(1000010, 0)
	if resp := h(reqFrom("1.2.3.4:5555", "")); resp.Status != 200 {
		t.Fatalf("next window status %d", resp.Status)
	}
}

func TestXForwardedForIgnored(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	h, _ := build(&config.RateLimit{Requests: 2, Window: time.Second, Per: config.PerIP}, clk)
	h(reqFrom("1.2.3.4:1", ""))
	h(reqFrom("1.2.3.4:2", ""))
	if resp := h(reqFrom("1.2.3.4:3", "5.6.7.8")); resp.Status != 429 {
		t.Fatalf("forged XFF got %d", resp.Status)
	}
	if resp := h(reqFrom("5.6.7.8:3", "1.2.3.4")); resp.Status != 200 {
		t.Fatalf("other IP got %d", resp.Status)
	}
}

func TestMiddlewareGlobalBucket(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	h, _ := build(&config.RateLimit{Requests: 2, Window: time.Second, Per: config.PerGlobal}, clk)
	h(reqFrom("1.1.1.1:1", ""))
	h(reqFrom("2.2.2.2:1", ""))
	if resp := h(reqFrom("1.1.1.1:1", "")); resp.Status != 429 {
		t.Fatalf("got %d", resp.Status)
	}
}

func TestPerRouteBuckets(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	cfg := &config.RateLimit{Requests: 100, Window: time.Minute, Per: config.PerIP}
	a, _ := build(cfg, clk)
	b, _ := build(cfg, clk)
	for range 100 {
		a(reqFrom("1.1.1.1:1", ""))
	}
	if a(reqFrom("1.1.1.1:1", "")).Status != 429 {
		t.Fatal("route a should be exhausted")
	}
	if b(reqFrom("1.1.1.1:1", "")).Status != 200 {
		t.Fatal("route b should be independent")
	}
}

func TestDisabled(t *testing.T) {
	for _, rl := range []*config.RateLimit{nil, {Requests: 0, Window: time.Minute}} {
		c := &counter{}
		h := Middleware(rl, nil)(c.handler)
		for range 5 {
			resp := h(reqFrom("1.1.1.1:1", ""))
			if resp.Status != 200 {
				t.Fatalf("status %d", resp.Status)
			}
			for k := range resp.Header {
				if strings.HasPrefix(k, "X-Ratelimit") {
					t.Fatalf("unexpected header %s", k)
				}
			}
		}
		if c.n != 5 {
			t.Fatalf("hits %d", c.n)
		}
	}
}

func TestHeadersOnGatewayErrors(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	h := Middleware(&config.RateLimit{Requests: 2, Window: time.Minute}, clk)(
		func(*gateway.RequestContext) *gateway.Response {
			return auth.Unauthorized(auth.ErrInvalidAPIKey)
		})
	r1 := h(reqFrom("1.1.1.1:1", ""))
	r2 := h(reqFrom("1.1.1.1:1", ""))
	r3 := h(reqFrom("1.1.1.1:1", ""))
	if r1.Status != 401 || r2.Status != 401 || r3.Status != 429 {
		t.Fatalf("statuses %d %d %d", r1.Status, r2.Status, r3.Status)
	}
	if r2.Header.Get("X-RateLimit-Remaining") != "0" {
		t.Fatalf("remaining %q", r2.Header.Get("X-RateLimit-Remaining"))
	}
}

func TestNilHeaderAndNilResponse(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	mw := Middleware(&config.RateLimit{Requests: 5, Window: time.Minute}, clk)
	resp := mw(func(*gateway.RequestContext) *gateway.Response { return &gateway.Response{Status: 204} })(reqFrom("1.1.1.1:1", ""))
	if resp.Header.Get("X-RateLimit-Remaining") != "4" {
		t.Fatalf("headers %v", resp.Header)
	}
	if mw(func(*gateway.RequestContext) *gateway.Response { return nil })(reqFrom("1.1.1.1:1", "")) != nil {
		t.Fatal("nil response should pass through")
	}
}

func TestClientIPFallback(t *testing.T) {
	clk := &fakeClock{time.Unix(1000000, 0)}
	h, _ := build(&config.RateLimit{Requests: 1, Window: time.Minute}, clk)
	rc := &gateway.RequestContext{ClientIP: "3.3.3.3"}
	if h(rc).Status != 200 {
		t.Fatal("first should pass")
	}
	if got := h(&gateway.RequestContext{ClientIP: "3.3.3.3"}); got.Status != 429 {
		t.Fatalf("second got %d", got.Status)
	}
	if got := h(reqFrom("4.4.4.4:1", "")); got.Status != 200 {
		t.Fatalf("other ip got %d", got.Status)
	}
}
