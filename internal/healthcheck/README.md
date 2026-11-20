# healthcheck

Background probe loop for routes with `health_check` (config spec §7).

- `Start(ctx, routes []*contract.Route, deps contract.Deps)`: returns immediately; one goroutine per route with `HealthCheck` and targets. Stops when `ctx` is done.
- First round runs at startup, then one round per `interval` (wait is `interval` minus round time, via `deps.Sleeper` and `deps.Clock`).
- Each target gets `GET <target URL>/<path>`; probes in a round run concurrently.
- Probe timeout is `min(interval, route.Timeout)`. 2xx is success; other statuses (redirects not followed), connection errors and timeouts are failures.
- Results go to `Target.RecordProbe(ok, unhealthy_threshold)`: threshold failures in a row mark unhealthy, one success recovers.
- Uses its own `http.Client`, so probes never touch breakers, rate limits or the upstream client. Transitions are logged via `deps.Logger`.
