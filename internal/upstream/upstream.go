// Package upstream is the terminal handler: breaker, pick, proxy, record, retry (design D6).
package upstream

import (
	"time"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/proxy"
)

// ticketed is a breaker that tells the half-open trial apart from other
// requests (breaker.Breaker implements it).
type ticketed interface {
	Admit(now time.Time) (ok bool, retryAfter int, ticket uint64)
	Settle(ticket uint64, ok bool, now time.Time)
	Abandon(ticket uint64)
}

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
		var ticket uint64
		if h.breaker != nil {
			ok, retryAfter, tk := h.admit()
			if !ok {
				return contract.BreakerOpen(retryAfter)
			}
			ticket = tk
		}
		t, ok := h.balancer.Pick(h.pool)
		if !ok || t == nil {
			h.release(ticket)
			return contract.NoHealthyUpstream()
		}
		resp, err := proxy.Attempt(reqCtx, h.deps.Client, t, out, h.route.Timeout)
		res, status := proxy.Outcome(resp, err)
		if status == contract.StatusClientClosed && !res.FromUpstream {
			h.release(ticket)
			return res
		}
		h.record(ticket, status < 500)

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

func (h *handler) admit() (bool, int, uint64) {
	now := h.deps.Clock.Now()
	if tb, ok := h.breaker.(ticketed); ok {
		return tb.Admit(now)
	}
	ok, ra := h.breaker.TryPass(now)
	return ok, ra, 0
}

func (h *handler) record(ticket uint64, ok bool) {
	if h.breaker == nil {
		return
	}
	now := h.deps.Clock.Now()
	if tb, isT := h.breaker.(ticketed); isT {
		tb.Settle(ticket, ok, now)
		return
	}
	h.breaker.Record(ok, now)
}

func (h *handler) release(ticket uint64) {
	if h.breaker == nil {
		return
	}
	if tb, ok := h.breaker.(ticketed); ok {
		tb.Abandon(ticket)
		return
	}
	h.breaker.Release()
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
