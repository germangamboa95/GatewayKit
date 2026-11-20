package ratelimit

import (
	"net/http"
	"strconv"

	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

// Middleware returns the rate-limit stage for cfg. A nil or unusable cfg
// passes every request through. A nil clk means the system clock.
func Middleware(cfg *config.RateLimit, clk clock.Clock) gateway.Middleware {
	lim := New(cfg)
	if lim == nil {
		return gateway.PassThrough
	}
	if clk == nil {
		clk = clock.System{}
	}
	return func(next gateway.Handler) gateway.Handler {
		return func(rc *gateway.RequestContext) *gateway.Response {
			d := lim.Allow(lim.Key(clientIP(rc)), clk.Now())
			if !d.Allowed {
				resp := RateLimited(d.RetryAfter)
				setHeaders(resp, d)
				return resp
			}
			resp := next(rc)
			if resp != nil {
				setHeaders(resp, d)
			}
			return resp
		}
	}
}

// RateLimited is the 429 body with Retry-After; the caller adds X-RateLimit-*.
func RateLimited(retryAfter int) *gateway.Response {
	r := gateway.ErrorResponse(http.StatusTooManyRequests, gateway.CodeRateLimitExceeded, "rate limit exceeded",
		map[string]any{"retry_after": retryAfter})
	r.Header.Set("Retry-After", strconv.Itoa(retryAfter))
	return r
}

func clientIP(rc *gateway.RequestContext) string {
	if rc.Req != nil && rc.Req.RemoteAddr != "" {
		return ClientIP(rc.Req.RemoteAddr)
	}
	return rc.ClientIP
}

func setHeaders(resp *gateway.Response, d Decision) {
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	resp.Header.Set("X-RateLimit-Limit", strconv.Itoa(d.Limit))
	resp.Header.Set("X-RateLimit-Remaining", strconv.Itoa(d.Remaining))
	resp.Header.Set("X-RateLimit-Reset", strconv.FormatInt(d.Reset, 10))
}
