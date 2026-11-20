// Package resptransform is the resptransform middleware. Stub: pass-through until its F package lands.
package resptransform

import "gatewaykit/internal/contract"

// New returns the resptransform middleware for route.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	return func(next contract.Handler) contract.Handler { return next }
}
