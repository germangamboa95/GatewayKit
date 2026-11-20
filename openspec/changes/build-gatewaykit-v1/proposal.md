# Proposal

## Why

GatewayKit has no gateway yet: the repo holds a `main.go` that prints a greeting. This change builds the v1 API gateway defined by two PRDs:

- `specs/gateway_config_spec.md`: the `gateway.yaml` schema and every runtime behavior it drives.
- `specs/base_spec.md`: the build constraints (Go standard library only), the mock upstream, the Compose setup and the parallel build plan.

The gateway must run the brief's sample config (`reference/sample-gateway.pasted.txt`) and any other config that follows the schema.

## What Changes

- Add a hand-written parser for the YAML subset in config spec §1, since the build allows no outside modules.
- Add config loading: path resolution, typed decoding, lenient placement, validation that reports every error with `file:line:col`, and normalization to one model with all defaults filled in.
- Add the HTTP gateway: reserved `GET /health`, longest-prefix routing with 404/405, path rewriting, forwarding with hop-by-hop and `X-Forwarded-*` handling, and the JSON error contract (§12).
- Add per-route features: rate limiting (fixed and sliding window), API-key auth, retry with backoff, active health checks, round-robin and smooth weighted round-robin balancing, a circuit breaker, and request/response transforms (headers, body mapping, envelope).
- Add `rate_limit: false` on a route, which turns off the inherited `global_rate_limit`. This settles the open row in config spec §14.
- Add a Go mock upstream with per-request controls and an admin API, a Docker Compose setup with six mocks, an e2e suite, variant configs and a CI command that also enforces the standard-library rule.
- Add `configs/gateway.yaml`: the brief's sample, re-indented into the canonical layout.
- Add a README with run instructions and the decisions from config spec §14.

## Capabilities

### New Capabilities

- `yaml-config-parser`: parses the supported YAML subset into a positioned node tree and rejects everything else with a line number.
- `config-loading`: finds the config file, decodes and validates it, reports errors and warnings, and produces the normalized model with defaults and precedence applied.
- `gateway-server`: listens on the configured port, serves `GET /health`, runs the request stages in order, and returns the JSON error contract.
- `request-routing`: matches routes by longest literal prefix on segment boundaries, enforces methods with 404/405, and rewrites the forwarded path.
- `upstream-proxy`: picks a healthy target with round-robin or smooth weighted round-robin, forwards the request, applies the per-attempt timeout, and maps failures to 502/503/504.
- `rate-limiting`: enforces per-route fixed or sliding window limits per IP or globally, with 429 and `X-RateLimit-*` headers.
- `api-key-auth`: checks an API key header in constant time and returns 401 for missing or invalid keys.
- `upstream-retry`: retries failed attempts on configured statuses with fixed or exponential backoff.
- `upstream-health-checks`: probes targets in the background and removes unhealthy ones from rotation.
- `circuit-breaker`: opens a per-route breaker after repeated upstream failures and recovers through a half-open trial.
- `traffic-transforms`: resolves `$variables` and applies header changes, request body mapping and response envelopes.
- `mock-upstream`: a controllable echo service that e2e tests drive through request headers and an admin API.
- `build-and-ci`: enforces the standard-library-only build, packages the gateway and mocks as Docker images, and runs unit and e2e tests on the sample and variant configs.

### Modified Capabilities

None. `openspec/specs/` is empty.

## Impact

- Code: replaces `main.go`; adds Go packages for config, routing, proxy and middleware, plus `mock-upstream/`, `tests/`, `configs/`, `Dockerfile`, `docker-compose.yml` and `README.md`.
- Dependencies: none. `go.mod` keeps zero `require` lines, and CI fails if `go list -m all` lists any module besides `gatewaykit`.
- Runtime: the gateway listens on `gateway.port` (default 8080); Compose publishes 8080, 9090 (variant B) and mock ports 3001 to 3006.
- Compatibility: greenfield, nothing to migrate.
