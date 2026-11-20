package proxy

import (
	"gatewaykit/internal/contract"
)

// New returns the single-attempt terminal handler: first target, one
// attempt, route timeout. deps.Client defaults to NewClient().
func New(route *contract.Route, deps contract.Deps) contract.Handler {
	deps = deps.WithDefaults()
	client := deps.Client
	if client == nil {
		client = NewClient()
	}
	return func(ctx *contract.RequestContext) *contract.Response {
		if len(route.Targets) == 0 {
			return contract.NoHealthyUpstream()
		}
		if ctx.Route == nil {
			ctx.Route = route
		}
		out := BuildRequest(ctx)
		resp, err := Attempt(ctx.Req.Context(), client, route.Targets[0], out, route.Timeout)
		res, _ := Outcome(resp, err)
		if res.FromUpstream {
			ctx.Upstream = res
			ctx.ResponseTime = deps.Clock.Now()
		}
		return res
	}
}
