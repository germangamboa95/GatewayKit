# upstream

Terminal handler per design D6: breaker check, pick, attempt, record, retry.

- `New(route, deps, opts ...Option) contract.Handler`: `deps.Client` defaults to `proxy.NewClient()`; `deps.Clock` feeds the breaker, `deps.Sleeper` the retry waits.
- `WithBalancer(contract.Balancer)`: default picks the first healthy target.
- `WithBreaker(contract.CircuitBreaker)`: default none. Released on no healthy target or client cancel; never recorded on cancel.
- `WithRetry(contract.RetryPolicy)`: default single attempt. Calls `ShouldRetry(status, n)` and `Delay(n)` with n the attempt just finished, so `Delay(1)` is the wait before the second try. Capped at `route.Retry.Attempts`.
- `WithPool(contract.TargetPool)`: default `NewPool(route)`.
- `NewPool(route) *Pool`: pool over `route.Targets`; `MarkProbe` uses `health_check.unhealthy_threshold`.
- Gateway errors (502, 504, 503) have `FromUpstream` false; `ctx.Upstream` and `ctx.ResponseTime` are set only for relayed responses.
