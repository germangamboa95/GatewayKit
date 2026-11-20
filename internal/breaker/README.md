# breaker

Per-route circuit breaker (config spec §7). Time is passed in, so tests use a fake clock.

- `New(cfg *contract.CircuitBreakerConfig) contract.CircuitBreaker`: a `*Breaker`, or a pass-through when `cfg` is nil.
- `NewBreaker(cfg) *Breaker`: closed breaker; `cfg` must not be nil.
- `TryPass(now) (ok bool, retryAfter int)`: call before every attempt. On rejection send `contract.BreakerOpen(retryAfter)`. `retryAfter` is the cooldown left rounded up, at least 1; 1 while a half-open trial is in flight.
- `Record(ok bool, now)`: call after every attempt that reached the upstream; `ok` is `status < 500`.
- `Release()`: call when an admitted attempt never reached the upstream (no healthy target, client gone). Frees the half-open trial slot.
- `State() State`, `Failures(now) int`: inspection for tests and logs. States: `Closed`, `Open`, `HalfOpen`.
- Failures older than `window` drop out. Records while open are ignored. Safe for concurrent use.
