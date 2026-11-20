// Package upstream is a route's terminal handler: it checks the circuit
// breaker, picks a target, proxies the request, records the outcome and
// retries when the policy allows.
package upstream

import (
	"net/http"
	"strconv"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/balance"
	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
	"github.com/germangamboa95/gatewaykit/internal/proxy"
)

// Balancer picks the next target from a pool. *balance.Balancer implements it.
type Balancer interface {
	Pick(pool balance.TargetPool) (*config.Target, bool)
}

// CircuitBreaker gates requests to the route's upstream. Release ends a
// half-open trial that never reached the upstream. *breaker.Breaker
// implements it.
type CircuitBreaker interface {
	TryPass(now time.Time) (ok bool, retryAfter int)
	Record(ok bool, now time.Time)
	Release()
}

// RetryPolicy decides whether to retry. attempt counts from 1 (the first
// try). *retry.Policy implements it.
type RetryPolicy interface {
	ShouldRetry(status, attempt int) bool
	Delay(attempt int) time.Duration
}

// ticketed is a CircuitBreaker that tells the half-open trial apart from
// other requests. *breaker.Breaker implements it; the handler prefers it
// when available.
type ticketed interface {
	Admit(now time.Time) (ok bool, retryAfter int, ticket uint64)
	Settle(ticket uint64, ok bool, now time.Time)
	Abandon(ticket uint64)
}

// Option configures the handler. Unset options fall back to: no breaker, no
// retry, the first healthy target, the route's targets as the pool, and the
// system clock.
type Option func(*handler)

// WithBalancer sets how targets are picked.
func WithBalancer(b Balancer) Option { return func(h *handler) { h.balancer = b } }

// WithBreaker sets the circuit breaker.
func WithBreaker(cb CircuitBreaker) Option { return func(h *handler) { h.breaker = cb } }

// WithRetry sets the retry policy.
func WithRetry(p RetryPolicy) Option { return func(h *handler) { h.retry = p } }

// WithPool sets the targets to pick from.
func WithPool(p balance.TargetPool) Option { return func(h *handler) { h.pool = p } }

// WithClock sets the clock used for breaker time and response timestamps.
func WithClock(c clock.Clock) Option { return func(h *handler) { h.clock = c } }

// WithSleeper sets what waits between retries.
func WithSleeper(s clock.Sleeper) Option { return func(h *handler) { h.sleeper = s } }

type handler struct {
	route    *config.Route
	client   proxy.Sender
	clock    clock.Clock
	sleeper  clock.Sleeper
	balancer Balancer
	breaker  CircuitBreaker
	retry    RetryPolicy
	pool     balance.TargetPool
}

// New returns the terminal handler for route. A nil client means
// proxy.NewClient().
func New(route *config.Route, client proxy.Sender, opts ...Option) gateway.Handler {
	h := &handler{route: route, client: client}
	for _, o := range opts {
		o(h)
	}
	if h.client == nil {
		h.client = proxy.NewClient()
	}
	if h.clock == nil {
		h.clock = clock.System{}
	}
	if h.sleeper == nil {
		h.sleeper = clock.System{}
	}
	if h.pool == nil {
		h.pool = targets(route.Targets)
	}
	if h.balancer == nil {
		h.balancer = firstHealthy{}
	}
	return h.serve
}

func (h *handler) serve(rc *gateway.RequestContext) *gateway.Response {
	if rc.Route == nil {
		rc.Route = h.route
	}
	ctx := rc.Req.Context()
	out := proxy.BuildRequest(rc)
	maxAttempts := 0
	if h.route.Retry != nil && h.route.Retry.Attempts > 0 {
		maxAttempts = h.route.Retry.Attempts
	}

	for attempt := 1; ; attempt++ {
		var ticket uint64
		if h.breaker != nil {
			ok, retryAfter, tk := h.admit()
			if !ok {
				return BreakerOpen(retryAfter)
			}
			ticket = tk
		}
		t, ok := h.balancer.Pick(h.pool)
		if !ok || t == nil {
			h.release(ticket)
			return gateway.NoHealthyUpstream()
		}
		resp, err := proxy.Attempt(ctx, h.client, t, out, h.route.Timeout)
		res, status := proxy.Outcome(resp, err)
		if status == gateway.StatusClientClosed && !res.FromUpstream {
			h.release(ticket)
			return res
		}
		h.record(ticket, status < 500)

		if h.retry == nil || (maxAttempts > 0 && attempt >= maxAttempts) || !h.retry.ShouldRetry(status, attempt) {
			if res.FromUpstream {
				rc.Upstream = res
				rc.ResponseTime = h.clock.Now()
			}
			return res
		}
		if err := h.sleeper.Sleep(ctx, h.retry.Delay(attempt)); err != nil {
			return proxy.ClientClosed()
		}
	}
}

func (h *handler) admit() (bool, int, uint64) {
	now := h.clock.Now()
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
	now := h.clock.Now()
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

// BreakerOpen is the circuit-breaker 503: only "error" and "retry_after",
// plus Retry-After.
func BreakerOpen(retryAfter int) *gateway.Response {
	r := gateway.ErrorResponse(http.StatusServiceUnavailable, gateway.CodeServiceUnavailable, "",
		map[string]any{"retry_after": retryAfter})
	r.Header.Set("Retry-After", strconv.Itoa(retryAfter))
	return r
}

type firstHealthy struct{}

func (firstHealthy) Pick(p balance.TargetPool) (*config.Target, bool) {
	for _, t := range p.Targets() {
		if p.Healthy(t) {
			return t, true
		}
	}
	return nil, false
}

// targets is the default pool: the route's targets, each reporting its own
// health state.
type targets []*config.Target

func (ts targets) Targets() []*config.Target { return ts }

func (targets) Healthy(t *config.Target) bool { return t.Healthy() }
