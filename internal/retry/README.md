# retry

Retry decision and backoff for upstream attempts (config spec §7).

- `New(cfg *contract.Retry) *Policy`: implements `contract.RetryPolicy`. A nil `cfg` makes exactly one attempt.
- `(*Policy).Attempts() int`: total tries allowed, at least 1.
- `ShouldRetry(status, attempt int) bool`: true when `attempt < attempts` and `status` is in `on`. `attempt` counts from 1. Pass `contract.Classify(resp, err)`, so 502 and 504 stand in for connection errors and timeouts.
- `Delay(attempt int) time.Duration`: wait after attempt `attempt` fails, before attempt `attempt+1`. Fixed: `initial_delay`. Exponential: `initial_delay * 2^(attempt-1)`, so 1s, 2s, 4s.
- Usage: `if p.ShouldRetry(status, n) { sleeper.Sleep(ctx, p.Delay(n)); n++ }`.
- Stateless and safe for concurrent use.
