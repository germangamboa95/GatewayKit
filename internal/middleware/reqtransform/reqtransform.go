// Package reqtransform is the reqtransform middleware. Stub: pass-through until its F package lands.
package reqtransform

import "gatewaykit/internal/contract"

// New returns the reqtransform middleware for route.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	return func(next contract.Handler) contract.Handler { return next }
}
