// Package ratelimit is the ratelimit middleware. Stub: pass-through until its F package lands.
package ratelimit

import "gatewaykit/internal/contract"

// New returns the ratelimit middleware for route.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	return func(next contract.Handler) contract.Handler { return next }
}
