// Package ratelimit is the rate-limit middleware (config spec §6).
package ratelimit

import (
	"net/http"
	"strconv"

	"gatewaykit/internal/contract"
	rl "gatewaykit/internal/ratelimit"
)

// New returns the ratelimit middleware for route. Routes without a limit get a pass-through.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	if route == nil {
		return passThrough
	}
	lim := rl.New(route.RateLimit)
	if lim == nil {
		return passThrough
	}
	deps = deps.WithDefaults()
	return func(next contract.Handler) contract.Handler {
		return func(ctx *contract.RequestContext) *contract.Response {
			d := lim.Allow(lim.Key(clientIP(ctx)), deps.Clock.Now())
			if !d.Allowed {
				resp := contract.RateLimited(d.RetryAfter)
				setHeaders(resp, d)
				return resp
			}
			resp := next(ctx)
			if resp != nil {
				setHeaders(resp, d)
			}
			return resp
		}
	}
}

func passThrough(next contract.Handler) contract.Handler { return next }

func clientIP(ctx *contract.RequestContext) string {
	if ctx.Req != nil && ctx.Req.RemoteAddr != "" {
		return rl.ClientIP(ctx.Req.RemoteAddr)
	}
	return ctx.ClientIP
}

func setHeaders(resp *contract.Response, d contract.Decision) {
	if resp.Header == nil {
		resp.Header = http.Header{}
	}
	resp.Header.Set("X-RateLimit-Limit", strconv.Itoa(d.Limit))
	resp.Header.Set("X-RateLimit-Remaining", strconv.Itoa(d.Remaining))
	resp.Header.Set("X-RateLimit-Reset", strconv.FormatInt(d.Reset, 10))
}
