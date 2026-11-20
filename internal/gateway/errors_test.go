package gateway_test

import (
	"encoding/json"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/auth"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
	"github.com/germangamboa95/gatewaykit/internal/ratelimit"
	"github.com/germangamboa95/gatewaykit/internal/router"
	"github.com/germangamboa95/gatewaykit/internal/transform"
	"github.com/germangamboa95/gatewaykit/internal/upstream"
)

// TestErrorBodies covers every gateway error body, including the ones built
// by the packages that own them, in one table.
func TestErrorBodies(t *testing.T) {
	cases := []struct {
		name   string
		resp   *gateway.Response
		status int
		body   string
		header map[string]string
	}{
		{"invalid json", transform.InvalidJSONBody(), 400,
			`{"error":"invalid_json_body","message":"request body is not valid JSON"}`, nil},
		{"missing key", auth.Unauthorized(auth.ErrMissingAPIKey), 401,
			`{"error":"unauthorized","message":"missing API key"}`, nil},
		{"invalid key", auth.Unauthorized(auth.ErrInvalidAPIKey), 401,
			`{"error":"unauthorized","message":"invalid API key"}`, nil},
		{"not found", router.NotFound(), 404,
			`{"error":"not_found","message":"no route matches the request path"}`, nil},
		{"method not allowed", router.MethodNotAllowed([]string{"GET", "DELETE"}), 405,
			`{"error":"method_not_allowed","message":"method not allowed on this route"}`,
			map[string]string{"Allow": "GET, DELETE"}},
		{"rate limited", ratelimit.RateLimited(7), 429,
			`{"error":"rate_limit_exceeded","message":"rate limit exceeded","retry_after":7}`,
			map[string]string{"Retry-After": "7"}},
		{"bad gateway", gateway.BadGateway(), 502,
			`{"error":"bad_gateway","message":"upstream unreachable or sent an invalid response"}`, nil},
		{"breaker open", upstream.BreakerOpen(12), 503,
			`{"error":"service_unavailable","retry_after":12}`,
			map[string]string{"Retry-After": "12"}},
		{"no healthy upstream", gateway.NoHealthyUpstream(), 503,
			`{"error":"no_healthy_upstream","message":"no healthy upstream target"}`, nil},
		{"gateway timeout", gateway.GatewayTimeout(), 504,
			`{"error":"gateway_timeout","message":"upstream timed out"}`, nil},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if c.resp.Status != c.status {
				t.Errorf("status = %d, want %d", c.resp.Status, c.status)
			}
			if got := string(c.resp.Body); got != c.body {
				t.Errorf("body = %s, want %s", got, c.body)
			}
			if !json.Valid(c.resp.Body) {
				t.Errorf("body is not valid JSON")
			}
			if ct := c.resp.Header.Get("Content-Type"); ct != "application/json" {
				t.Errorf("Content-Type = %q", ct)
			}
			if c.resp.FromUpstream {
				t.Errorf("FromUpstream = true for a gateway error")
			}
			for k, v := range c.header {
				if got := c.resp.Header.Get(k); got != v {
					t.Errorf("%s = %q, want %q", k, got, v)
				}
			}
		})
	}
}

func TestErrorResponseExtraOrderAndEscaping(t *testing.T) {
	r := gateway.ErrorResponse(418, "teapot", "a <b> & c", map[string]any{"z": 1, "a": "x"})
	want := `{"error":"teapot","message":"a <b> & c","a":"x","z":1}`
	if string(r.Body) != want {
		t.Fatalf("body = %s, want %s", r.Body, want)
	}
}
