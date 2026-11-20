// Package auth is the API-key auth middleware (config spec §8).
package auth

import (
	apikey "gatewaykit/internal/auth"
	"gatewaykit/internal/contract"
)

// New returns the auth middleware for route. Routes without auth get a pass-through.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	if route == nil || route.Auth == nil {
		return func(next contract.Handler) contract.Handler { return next }
	}
	a := apikey.FromConfig(route.Auth)
	return func(next contract.Handler) contract.Handler {
		return func(ctx *contract.RequestContext) *contract.Response {
			var err error
			if ctx.Req == nil {
				err = contract.ErrMissingAPIKey
			} else {
				err = a.Check(ctx.Req.Header)
			}
			if err != nil {
				return contract.Unauthorized(err)
			}
			return next(ctx)
		}
	}
}
