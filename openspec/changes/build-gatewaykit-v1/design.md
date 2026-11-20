# Design

## Context

See proposal.md for motivation. The repo is a Go 1.27.4 module named `gatewaykit` whose `main.go` prints a greeting, with no dependencies and no tests. `specs/base_spec.md` fixes the constraints this design works within:

- Go standard library only, for the gateway, the mocks and the tests.
- A frozen set of shared contracts (Wave 0) so five tracks can build in parallel without waiting on each other.
- Upstream calls use `http.Transport` directly, because retries and body transforms need the body in memory.
- Docker images are static binaries in `scratch`. Mocks join the gateway's network namespace so the sample's `localhost:300x` URLs work unchanged.

The brief's sample config arrived with its indentation lost and its comments hard-wrapped (`reference/sample-gateway.pasted.txt`).

## Goals / Non-Goals

**Goals:**

- Freeze the contracts first, then let every track build and unit-test against fakes and fixtures.
- Run all per-request code from one normalized model. No request path reads raw YAML or checks for missing fields.
- Inject time through a `Clock`, so unit tests cover rate limits, breaker states and backoff without sleeping.
- Keep every slow e2e scenario under a few seconds by giving it its own fast-timing config.

**Non-Goals:**

- Config reload without a restart (config spec §14: out of scope for v1).
- `trusted_proxies`, auth schemes beyond `api_key`, TLS termination, metrics endpoints.
- Streaming bodies. Every request and response body is buffered.

## Decisions

### D1. Repository layout

The root package stays the entrypoint, so the `.gitignore` entry `/gatewaykit` keeps matching the binary. The mocks and the e2e tests are separate Go modules, because Compose builds them from their own directories (`build: ./mock-upstream`, `build: ./tests`). That keeps them sharing no code with the gateway.

| Path | Contents | Base spec IDs |
| --- | --- | --- |
| `main.go` | Load, normalize, build pipelines, serve | F1 |
| `internal/contract` | Shared model, request flow types, strategy interfaces, `Clock`, `UpstreamClient`, `Classify`, `ErrorResponse` | W0.2 |
| `internal/yamlparse` | `Node` type and the subset parser | W0.2 (type), A0 |
| `internal/config` | Path resolution, durations, decode, validate, normalize | A1 to A4 |
| `internal/fixtures` | One normalized `Route` per sample route | W0.2 |
| `internal/server` | HTTP server, `GET /health`, pipeline runner, error responder, access log | B1, B3 |
| `internal/router` | Longest-prefix router, 404/405 | B2 |
| `internal/proxy` | Path rewrite, upstream client, per-attempt timeout, and a single-attempt terminal handler used until F5 lands | C1 to C3 |
| `internal/ratelimit`, `internal/balance`, `internal/breaker`, `internal/retry`, `internal/auth` | Pure-logic libraries | E1 to E4, E8 |
| `internal/transform` | Variable resolver, header transformer, dot paths, body mapper, envelope | E5 to E7 |
| `internal/middleware/ratelimit`, `.../auth`, `.../reqtransform`, `.../resptransform` | One package per middleware, each with its own owner | F2, F3, F4, F6 |
| `internal/upstream` | Orchestrator: breaker, pick, proxy, record, retry | F5 |
| `internal/healthcheck` | Background probe loop | F7 |
| `testdata/` | Broken configs for the root-module exit-code test | F1 |
| `mock-upstream/` | Own `go.mod`, mock service, `Dockerfile` | D1 |
| `tests/` | Own `go.mod`, e2e harness and one scenario file per feature, `Dockerfile` | D3, G1 |
| `configs/` | `gateway.yaml`, `variant-a.yaml`, `variant-b.yaml`, `variant-c.yaml` | D4 |
| `Dockerfile`, `docker-compose.yml`, `scripts/e2e.sh`, `scripts/ci.sh`, `.github/workflows/ci.yml` | Build, Compose, one-config e2e run, full CI | W0.1, D2, G2 |

Alternative considered: one module with `cmd/` binaries. Rejected because Compose's per-directory build contexts would not see the root `go.mod`.

### D2. Contracts: the base spec's Wave 0 block, with seven fixes

`internal/contract` holds the Go block from base_spec.md "Wave 0" plus the model types it implies (`Target`, `Retry`, `HealthCheck`, `RateLimit`, `Auth`, `ReqTransform`, `RespTransform`, `Response`, `OutRequest`, `Decision`). It also holds the compiled transform forms (variable token, compiled mapping, envelope template), which the config track produces and the transform track consumes. `Node` lives in `internal/yamlparse`, so the parser has no dependency on the model.

The block cannot ship verbatim. These fixes land in W0.2, before any track starts:

| # | Change | Why |
| --- | --- | --- |
| 1 | The config type becomes `CircuitBreakerConfig`; the `Route` field keeps the name `CircuitBreaker` | The block declares `CircuitBreaker` as both an interface and a struct, which does not compile |
| 2 | Add `Route.MethodList []string` in config order (default `GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS`) | `Allow` lists methods in config order; `Methods map[string]bool` has no order |
| 3 | `RequestContext.Upstream` becomes `*Response` | `UpstreamClient.Send` returns a buffered `*Response`, not `*http.Response` |
| 4 | Add `Response.FromUpstream bool` | The envelope applies only to relayed responses (D6) |
| 5 | Add `CircuitBreaker.Release()` | Ends a half-open trial that never reached the upstream (circuit-breaker spec "Trial without an upstream attempt") |
| 6 | `ErrorResponse` and `Classify` ship implemented, not stubbed | Two Wave 1 tracks call them, and each is about 20 lines |
| 7 | Each feature package gets a `New(route, deps) Middleware` constructor that returns a pass-through stub | F1 wires the full chain once, and each F agent then edits only its own package |

Any later W0.2 change is recorded in the next gate report and applied by the orchestrator in one commit that updates every caller.

### D3. YAML: line-oriented parser with an indentation stack

The parser tokenizes each line into indent, optional `- `, key, and value. Then it builds nodes against a stack of open containers keyed by indent. Flow collections parse with a small recursive-descent scanner limited to one line. Each unsupported construct is detected by an explicit check that names the line, never by a generic parse error. This matches the "never quietly misread" rule in config spec §1.

Alternative considered: a general YAML 1.2 parser. Rejected because it is far larger than the subset needs, and each extra feature becomes a way to misread a config.

### D4. Loading runs in three passes, and only the first may stop early

1. **Decode** walks the node tree against a schema table (field name, type, allowed placements). It records each value with the node's `line:col`, warns on unknown keys, and resolves lenient placement.
2. **Validate** applies every per-field and cross-field rule, appending to an error list.
3. **Normalize** fills defaults, merges rate limits, builds target lists and compiles variables, mappings and envelopes.

The loader prints all errors with `file:line:col`, then exits 1. Only a YAML syntax error stops before validation, because no tree exists to validate.

### D5. Route-level `rate_limit: false`

The decoder accepts a mapping or the bare scalar `false` at `routes[].rate_limit`. `false` sets an explicit "disabled" marker that normalization honours before the merge: the route gets no limiter and sends no `X-RateLimit-*` headers. `true`, any other scalar, and `global_rate_limit: false` are type errors. This settles the open row in config spec §14 as the user chose.

### D6. Pipeline assembly

The server is a single root `http.Handler`, not `ServeMux` patterns. Go's `GET /health` pattern also matches `HEAD`, and `ServeMux` redirects unclean paths such as `//a`. The handler answers exactly `GET /health`, then routes, then calls the route's chain. F1 wires the full chain once from the D2 constructors:

```
ratelimit.New -> auth.New -> reqtransform.New -> resptransform.New -> terminal
```

The terminal handler is `proxy`'s single-attempt handler (first target, one attempt) until F5 lands. F5 then swaps in `upstream.New` with a one-line change to `main.go`. A constructor whose feature the route lacks returns a pass-through.

The orchestrator loop:

1. `TryPass`. A rejection returns the breaker 503.
2. Pick a target. If none is healthy, call `Release` and return `no_healthy_upstream`.
3. Send with the per-attempt timeout.
4. If the client disconnected, call `Release` and stop without recording or retrying.
5. Classify, then `Record`.
6. Ask the retry policy. Sleep through the injected clock, and stop if the client disconnects.

Response transform acts only when the returned `Response.FromUpstream` is true. A gateway 504 that follows an upstream 503 on an earlier attempt is therefore never wrapped.

### D7. Upstream client

- Use one shared `http.Transport` (connection pooling) with `DisableCompression: true`. The gateway forwards the client's `Accept-Encoding`, or sends `identity` on envelope routes.
- Set no Transport timeout (dial, TLS, response header) shorter than the route timeout. The attempt context governs all of them.
- Derive each attempt's context from the client request's context with `context.WithTimeout`. Read the whole response body inside it, so the timeout covers connect through the full body.
- Classify `errors.Is(err, context.DeadlineExceeded)`, or a `net.Error` whose `Timeout()` is true, as 504. If the client's context is canceled, abort (D6 step 4). Every other transport or read error (refused, reset, EOF, malformed response) is 502.
- Disable redirect following. The client returns upstream 3xx as-is.
- `http.Transport` silently re-sends a replayable request (GET, HEAD, OPTIONS) once when a reused connection closes without a response. So e2e tests that use `X-Mock-Drop` assert the status, not the mock's hit count.

### D8. JSON fidelity

- Decode with `json.Decoder.UseNumber()`, so numbers pass through without float rounding (`7` stays `7`).
- Reject trailing data: after the first value, a second `Decode` must return `io.EOF`. Otherwise `{"a":1}x` would count as valid JSON for both the 400 check and the `$body` check.
- Embed `$body` as compacted `json.RawMessage`, so the upstream's key order and number text survive.
- Write mapping output and envelope objects with an ordered writer in YAML key order, with no HTML escaping and no trailing newline. `map[string]any` would sort the keys: variant B's `{"result":{"payload":[1,2],"ok":true},...}` would come out as `{"ok":true,"payload":[1,2]}`.

### D9. Concurrency and memory

| State | Scope | Guard |
| --- | --- | --- |
| Rate-limit buckets | Per route, keyed by IP or `""` | Mutex per route store. A sweeper deletes buckets idle for 2x `window` |
| Sliding log | Per bucket | Ring buffer of at most `requests` timestamps |
| Balancer rotation | Per route | Mutex. Smooth WRR runs over the healthy subset only |
| Target health | Per target | Atomic healthy flag and failure counter, written only by the health loop |
| Breaker | Per route | Mutex. Failure timestamps pruned by `window`. A half-open trial flag admits exactly one request |

### D10. Canonical sample config

The repo ships the sample as `configs/gateway.yaml`, re-indented with each key at its canonical placement from config spec §2. Wrapped comment fragments are rejoined onto one line. The header comment keeps the em dash from the user-supplied original.

```yaml
# gateway.yaml — GatewayKit Configuration
gateway:
  port: 8080
  # Admin/health endpoint is always available at GET /health regardless of config.
  # It should return 200 OK with JSON: { "status": "healthy", "uptime_seconds": <int> }
  global_timeout: "30s" # Default timeout for all upstream requests
  global_rate_limit: # Default rate limit applied to all routes unless overridden
    requests: 100
    window: "60s"
    strategy: "fixed_window" # "fixed_window" or "sliding_window"
    per: "ip" # Rate limit bucket key: "ip" or "global"

routes:
  - path: "/api/users"
    methods: ["GET", "POST"]
    strip_prefix: false
    upstream:
      url: "http://localhost:3001"
    rate_limit:
      requests: 30
      window: "60s"
      strategy: "sliding_window"
      per: "ip"

  - path: "/api/orders"
    methods: ["GET", "POST", "PUT"]
    strip_prefix: false
    upstream:
      url: "http://localhost:3002"
      timeout: "5s" # Override global timeout for this route
      retry:
        attempts: 3
        backoff: "exponential" # "fixed" or "exponential"
        initial_delay: "1s"
        on: [502, 503, 504] # HTTP status codes that trigger a retry
    rate_limit:
      requests: 10
      window: "10s"
      strategy: "fixed_window"
      per: "ip"

  - path: "/api/products"
    methods: ["GET"]
    strip_prefix: true # /api/products/123 -> /123 when forwarded to upstream
    upstream:
      targets: # Multiple upstream targets for load balancing
        - url: "http://localhost:3003"
          weight: 3
        - url: "http://localhost:3004"
          weight: 1
      balance: "weighted_round_robin" # "round_robin" or "weighted_round_robin"
      timeout: "10s"
      health_check:
        path: "/healthz"
        interval: "30s"
        unhealthy_threshold: 3 # Mark upstream unhealthy after 3 consecutive failures

  - path: "/api/legacy"
    methods: ["GET", "POST"]
    strip_prefix: true # /api/legacy/v1/data -> /v1/data when forwarded
    upstream:
      url: "http://localhost:3005"
    request_transform:
      headers:
        add:
          X-Gateway: "gatewaykit"
          X-Request-Start: "$request_time" # Dynamic value: inject request timestamp
        remove: ["X-Debug", "X-Internal"]
      body:
        # Restructure the request body before forwarding
        # Maps "destination path" <- "source path" using dot notation
        mapping:
          user.id: "userId"
          user.name: "userName"
          meta.source: "$literal:gateway" # Inject a literal value
          meta.timestamp: "$request_time"
    response_transform:
      headers:
        add:
          X-Served-By: "gatewaykit"
        remove: ["Server", "X-Powered-By"]
      body:
        # Wrap the upstream response in an envelope
        envelope:
          data: "$body" # The original response body goes here
          gateway_metadata:
            served_at: "$response_time"
            route: "$route_path"

  - path: "/api/internal"
    methods: ["GET", "POST"]
    strip_prefix: false
    upstream:
      url: "http://localhost:3006"
    auth:
      type: "api_key"
      header: "X-API-Key"
      keys: ["sk_live_abc123", "sk_live_def456"]
    circuit_breaker:
      threshold: 5 # Trip after 5 failures
      window: "60s" # Within this time window
      cooldown: "30s" # Wait this long before trying upstream again
      # When tripped, return 503 with: { "error": "service_unavailable", "retry_after": <seconds_remaining> }
```

Placement choices where the flat paste is ambiguous: `routes` at top level, `rate_limit` on the route, and `timeout`, `retry` and `health_check` under `upstream`. Each matches config spec §2's canonical form, so the file loads with no placement warnings.

### D11. Test configs and e2e isolation

All four configs ship in Wave 0, because e2e scenarios on several agents depend on their exact paths and timings.

| File | Purpose | Port | Upstreams |
| --- | --- | --- | --- |
| `gateway.yaml` | The sample (D10) | 8080 | 3001 to 3006 |
| `variant-a.yaml` | Config spec §13 A: every default | 8080 | `4000` remapped to 3001 |
| `variant-b.yaml` | Config spec §13 B: lenient placement, `ms`, base path | 9090 | `4001` to `4003` remapped to 3002 to 3004 (one mapping, `400N` to `300(N+1)`, across all variants) |
| `variant-c.yaml` | Fast resilience timings, retry `attempts: 4`, `rate_limit: false`, a 504 case | 8080 | 3001 to 3004, 3006 |

The sample's 30s health interval and 30s breaker cooldown would make e2e runs take minutes, so `variant-c.yaml` exists:

```yaml
# variant-c.yaml: fast resilience timings for e2e
gateway:
  port: 8080
  global_timeout: "2s"
  global_rate_limit:
    requests: 5
    window: "60s"
    strategy: sliding_window
    per: ip

routes:
  - path: "/c/retry"
    upstream:
      url: "http://localhost:3002"
      retry:
        attempts: 4
        backoff: exponential
        initial_delay: "1s"
        on: [503]
    rate_limit: { requests: 1000 }

  - path: "/c/breaker"
    upstream:
      url: "http://localhost:3006"
    circuit_breaker:
      threshold: 3
      window: "10s"
      cooldown: "2s"
    rate_limit: { requests: 1000 }

  - path: "/c/health"
    upstream:
      targets:
        - url: "http://localhost:3003"
        - url: "http://localhost:3004"
      health_check:
        path: "/healthz"
        interval: "1s"
        unhealthy_threshold: 2
    rate_limit: { requests: 1000 }

  - path: "/c/limited"
    upstream:
      url: "http://localhost:3001"

  - path: "/c/unlimited"
    upstream:
      url: "http://localhost:3001"
    rate_limit: false
```

What each route covers:

- `/c/retry` with the mock's default set to 503: waits of 1s, 2s and 4s.
- `/c/limited`: the sixth request from one IP gets 429.
- `/c/unlimited`: never limited and sends no `X-RateLimit-*` headers. A 2500ms mock delay against the 2s global timeout gives 504.
- The `{ requests: 1000 }` overrides inherit `window`, `strategy` and `per` from the global limit, which also exercises the field-by-field merge.

**Running e2e.** `scripts/e2e.sh <config>` runs one config:

1. Take the Compose lock (D13).
2. Set `GATEWAY_PORT`: 9090 for `variant-b.yaml`, 8080 otherwise.
3. `docker compose up -d --build --force-recreate`, then poll `/health`.
4. `docker compose run --rm tests`, then `docker compose down`.

`scripts/ci.sh` runs the unit gates, then `e2e.sh` for each of the four configs. Compose passes `CONFIG` and `GATEWAY_URL=http://localhost:${GATEWAY_PORT:-8080}` to the tests container. Each scenario declares the configs it applies to and skips the others.

**Isolation inside one gateway run.** Mock reset does not clear gateway state (buckets, breaker, health, rotation), so the harness enforces these rules:

- **Own source IP per test.** Each test dials from its own `127.0.0.N` (`net.Dialer.LocalAddr`; Linux loopback accepts all of 127/8). Per-IP buckets therefore never leak between tests, and "two different IPs" is real.
- **Window helper.** Before a fixed-window assertion, or a burst on a shared global bucket (variant B `/v2`), the test waits for the next window boundary.
- **Restore state.** A test that opens a breaker or marks a target unhealthy restores it (waits out the cooldown, resets mock health) before it returns.

**Dead target and exit code.** The tests container has no Docker socket. `scripts/ci.sh` therefore runs the dead-target check from the host on `variant-c.yaml`: `docker compose stop products-b`, assert 502 handling and removal from `/c/health` rotation, then `docker compose start products-b`. The exit-1 check for an invalid config is a root-module test: it builds the binary, runs it with `testdata/broken.yaml`, and asserts exit code 1 and `broken.yaml:<line>:<col>` in stderr.

### D12. Settling PRD conflicts

| Conflict | Resolution | Why |
| --- | --- | --- |
| §6 shows the 429 body without `message`; §12 says every gateway error has one except the breaker 503 | 429 body is `{"error","message","retry_after"}` | §12 is the general contract and names a single exception |
| §5 shows `{"error":"no_healthy_upstream"}` alone | Add `message` | Same §12 rule |
| Base spec M3 expects retry waits 1s, 2s, 4s; the sample has `attempts: 3`, which is total tries (§7) | Sample shows 1s, 2s. `variant-c.yaml` with `attempts: 4` shows 1s, 2s, 4s | Keeps §7's definition and still produces the M3 evidence |
| Variant B uses ports 4000 to 4003; Compose has mocks on 3001 to 3006 | Remap as in D11 and publish 9090 | Base spec "Gotchas" asks for the remap |
| Base spec gates wait for owner approval; the user chose to run straight through | Write each gate report to `openspec/changes/build-gatewaykit-v1/gates/M<n>.md` and continue | User decision |
| Base spec says Wave 0 holds stubs only and freezes the block as written | Apply the D2 fixes and implement `ErrorResponse` and `Classify` in Wave 0 | The block does not compile as written, and two tracks need the helpers |
| Base spec gives the variant configs (D4) and the e2e scenarios (G1) to agent 2 | Variant configs ship in Wave 0; e2e scenario files are spread across idle agents (tasks.md) | Several agents depend on the configs; agent 2 would otherwise be the bottleneck |

### D13. Verification gates

Every package merges with its unit tests, and each F package merges with at least one e2e scenario. Before any merge to the integration branch, run from the worktree root:

- `gofmt -l .`: prints nothing
- `go vet ./...`, in all three modules
- `go test ./...`, in all three modules
- The standard-library check: no `require` line in any `go.mod`, and `go list -m all` prints only the module itself, in all three modules

`scripts/ci.sh` runs those gates plus the e2e matrix. They replace the npm commands in `openspec/config.yaml`'s apply guidance, which do not apply to this repo.

**Compose lock.** Every worktree's stack publishes the same host ports, so only one stack may run at a time. `scripts/e2e.sh` creates `${TMPDIR:-/tmp}/gatewaykit-compose.lock` with `mkdir` and removes it on exit through a trap. If it cannot take the lock, or `lsof -nP -iTCP:8080 -sTCP:LISTEN` prints anything, it stops. Agents run e2e only through `scripts/e2e.sh` or `scripts/ci.sh`.

## Risks / Trade-offs

- [Hand-written YAML misreads an edge case] → Run conformance tests per supported row, a rejection test per unsupported row, and round-trip the sample and variants against expected node trees.
- [Mocks share the gateway's network namespace, so restarting only the gateway orphans them] → `scripts/e2e.sh` always recreates the whole stack.
- [Parallel worktrees fight over host ports 8080, 9090 and 3001 to 3006] → The Compose lock in D13.
- [Gateway state leaks between e2e tests in one run] → The isolation rules in D11.
- [Timing-sensitive e2e tests flake on slow CI] → Unit tests prove exact timing with a fake clock. E2E asserts lower bounds plus generous upper bounds (for example, a 1s wait passes between 0.9s and 1.8s).
- [Wave 0 interfaces turn out wrong mid-build] → Tracks code only against W0.2. A signature change is logged in the gate report and applied by the main stream in one commit that updates every caller.
- [Buffering whole bodies costs memory on large payloads] → Accepted for v1. Retries and transforms need the bytes, and the brief sets no size limits.
- [The sample paste could hide a placement the grader reads differently] → Lenient placement accepts either form of every ambiguous key, so a grader's own indentation still loads.

## Migration Plan

Greenfield. Rollback means reverting the merge. Deploy with `docker compose up --build`.

## Open Questions

- Which base branch apply should cut from: `openspec/config.yaml` names `origin/dev`, but the remote lists no branches. Apply asks the owner before cutting, as its guidance says.
- Worktree location: `openspec/config.yaml` says `.claude/worktrees/<branch>`; `Task.md` says `./.agents/worktrees`. Apply follows `openspec/config.yaml` unless the owner says otherwise.
