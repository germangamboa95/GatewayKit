# balance

Target selection over the healthy subset of a route's targets.

- `New(kind string) *Balancer`: `round_robin` (default for unknown values) or `weighted_round_robin` (smooth WRR). Implements `contract.Balancer`.
- `(*Balancer).Pick(pool) (*contract.Target, bool)`: next healthy target; `false` when none is healthy.
- `NewPool(route *contract.Route) *Pool`: implements `contract.TargetPool` over `route.Targets`.
- `Pool.Healthy(t)`: always true when the route has no `health_check`, else `t.Healthy()`.
- `Pool.MarkProbe(t, ok)`: applies a probe result with the route's `unhealthy_threshold`.
- Rotation state is keyed by target, so one Balancer per route is enough and pools never share rotation.
- Safe for concurrent use.
