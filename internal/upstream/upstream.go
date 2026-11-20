// Package upstream is the terminal handler: breaker, pick, proxy, record, retry (design D6).
package upstream

import (
	"gatewaykit/internal/contract"
	"gatewaykit/internal/proxy"
)

// Option sets a strategy on the handler. Unset strategies fall back to:
// no breaker, no retry, first healthy target, a pool over route.Targets.
type Option func(*handler)

func WithBalancer(b contract.Balancer) Option { return func(h *handler) { h.balancer = b } }

func WithBreaker(cb contract.CircuitBreaker) Option { return func(h *handler) { h.breaker = cb } }

func WithRetry(p contract.RetryPolicy) Option { return func(h *handler) { h.retry = p } }

func WithPool(p contract.TargetPool) Option { return func(h *handler) { h.pool = p } }

type handler struct {
	route    *contract.Route
	deps     contract.Deps
	balancer contract.Balancer
	breaker  contract.CircuitBreaker
	retry    contract.RetryPolicy
	pool     contract.TargetPool
}

// New returns the terminal handler for route. deps.Client defaults to
// proxy.NewClient(); deps.Clock and deps.Sleeper drive breaker time and retry waits.
func New(route *contract.Route, deps contract.Deps, opts ...Option) contract.Handler {
	h := &handler{route: route, deps: deps.WithDefaults()}
	for _, o := range opts {
		o(h)
	}
	if h.deps.Client == nil {
		h.deps.Client = proxy.NewClient()
	}
	if h.pool == nil {
		h.pool = NewPool(route)
	}
	if h.balancer == nil {
		h.balancer = firstHealthy{}
	}
	return h.serve
}

func (h *handler) serve(ctx *contract.RequestContext) *contract.Response {
	if ctx.Route == nil {
		ctx.Route = h.route
	}
	reqCtx := ctx.Req.Context()
	out := proxy.BuildRequest(ctx)
	maxAttempts := 0
	if h.route.Retry != nil && h.route.Retry.Attempts > 0 {
		maxAttempts = h.route.Retry.Attempts
	}

	for attempt := 1; ; attempt++ {
		if h.breaker != nil {
			if ok, retryAfter := h.breaker.TryPass(h.deps.Clock.Now()); !ok {
				return contract.BreakerOpen(retryAfter)
			}
		}
		t, ok := h.balancer.Pick(h.pool)
		if !ok || t == nil {
			h.release()
			return contract.NoHealthyUpstream()
		}
		resp, err := proxy.Attempt(reqCtx, h.deps.Client, t, out, h.route.Timeout)
		res, status := proxy.Outcome(resp, err)
		if status == contract.StatusClientClosed {
			h.release()
			return res
		}
		if h.breaker != nil {
			h.breaker.Record(status < 500, h.deps.Clock.Now())
		}

		if h.retry == nil || (maxAttempts > 0 && attempt >= maxAttempts) || !h.retry.ShouldRetry(status, attempt) {
			if res.FromUpstream {
				ctx.Upstream = res
				ctx.ResponseTime = h.deps.Clock.Now()
			}
			return res
		}
		if err := h.deps.Sleeper.Sleep(reqCtx, h.retry.Delay(attempt)); err != nil {
			return proxy.ClientClosed()
		}
	}
}

func (h *handler) release() {
	if h.breaker != nil {
		h.breaker.Release()
	}
}

type firstHealthy struct{}

func (firstHealthy) Pick(p contract.TargetPool) (*contract.Target, bool) {
	for _, t := range p.Targets() {
		if p.Healthy(t) {
			return t, true
		}
	}
	return nil, false
}

// Pool is a contract.TargetPool over a route's targets using each target's
// own health state.
type Pool struct {
	targets   []*contract.Target
	threshold int
}

// NewPool returns a Pool over route.Targets; MarkProbe uses the route's
// health_check unhealthy_threshold.
func NewPool(route *contract.Route) *Pool {
	p := &Pool{targets: route.Targets, threshold: 1}
	if route.HealthCheck != nil && route.HealthCheck.UnhealthyThreshold > 0 {
		p.threshold = route.HealthCheck.UnhealthyThreshold
	}
	return p
}

func (p *Pool) Targets() []*contract.Target { return p.targets }

func (p *Pool) Healthy(t *contract.Target) bool { return t.Healthy() }

func (p *Pool) MarkProbe(t *contract.Target, ok bool) { t.RecordProbe(ok, p.threshold) }
