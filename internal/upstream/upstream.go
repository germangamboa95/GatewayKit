// Package upstream is the terminal handler: breaker, pick, proxy, record, retry (design D6).
// Stub until F5 lands.
package upstream

import "gatewaykit/internal/contract"

// New returns the terminal handler for route.
func New(route *contract.Route, deps contract.Deps) contract.Handler {
	return func(ctx *contract.RequestContext) *contract.Response {
		return contract.BadGateway()
	}
}
