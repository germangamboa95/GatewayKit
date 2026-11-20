// Package auth is the auth middleware. Stub: pass-through until its F package lands.
package auth

import "gatewaykit/internal/contract"

// New returns the auth middleware for route.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	return func(next contract.Handler) contract.Handler { return next }
}
