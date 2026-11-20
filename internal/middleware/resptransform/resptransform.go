// Package resptransform is the response-transform middleware: envelope, then
// remove headers, then add headers, on upstream responses only (config spec §9).
package resptransform

import (
	"net/http"
	"strconv"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/transform"
)

// New returns the resptransform middleware for route.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	if route == nil || route.RespTransform == nil {
		return func(next contract.Handler) contract.Handler { return next }
	}
	rt := route.RespTransform
	deps = deps.WithDefaults()
	return func(next contract.Handler) contract.Handler {
		return func(ctx *contract.RequestContext) *contract.Response {
			if rt.Envelope != nil && ctx.Req != nil {
				if ctx.Req.Header == nil {
					ctx.Req.Header = http.Header{}
				}
				ctx.Req.Header.Set("Accept-Encoding", "identity")
			}
			resp := next(ctx)
			if resp == nil || !resp.FromUpstream {
				return resp
			}
			env := transform.Env{
				RequestTime:  ctx.RequestTime,
				ResponseTime: ctx.ResponseTime,
				RoutePath:    route.Path,
			}
			if env.ResponseTime.IsZero() {
				env.ResponseTime = deps.Clock.Now()
			}
			if env.RequestTime.IsZero() {
				env.RequestTime = env.ResponseTime
			}
			if resp.Header == nil {
				resp.Header = http.Header{}
			}
			if rt.Envelope != nil {
				resp.Body = transform.Envelope(rt.Envelope, resp.Body, env)
				resp.Header.Del("Content-Encoding")
				resp.Header.Set("Content-Type", "application/json")
				resp.Header.Set("Content-Length", strconv.Itoa(len(resp.Body)))
			}
			transform.ApplyHeaders(resp.Header, rt.RemoveHeaders, rt.AddHeaders, env)
			return resp
		}
	}
}
