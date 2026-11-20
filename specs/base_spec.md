# GatewayKit Parallel Build Plan

Nov 19, 2026 · @German

**Constraint: Go, standard library only.** This applies to the gateway, the mock upstreams and the tests. It includes YAML: Go's standard library has no YAML parser, so track A starts with a hand-written parser for the subset of YAML the config uses (A0; spec §1, “Supported YAML”).

## Waves at a glance

Wave 0 is the only point where anyone waits on someone else. Within each of Waves 1–3, every package depends only on work from earlier waves or earlier in its own track, never on another track in progress.

| Wave         | Goal                                                                                                 | Parallelism                                                            | Exit gate                                                                    |
| ------------ | ---------------------------------------------------------------------------------------------------- | ---------------------------------------------------------------------- | ---------------------------------------------------------------------------- |
| 0. Contracts | Agree the types and interfaces every track codes against, and stand up the repo and Compose skeleton | None: Claude drafts, you approve                                       | Shared types merged; `docker compose up` starts a stub gateway and six mocks |
| 1. Build     | Build each component on its own                                                                      | 5 tracks, 23 packages, no waiting between tracks                       | Each package passes unit tests against fakes and fixtures                    |
| 2. Wire      | Wrap each library as middleware and assemble `main()`                                                | 7 packages; each needs only its own libraries plus the pipeline runner | Sample config boots and every middleware is registered                       |
| 3. Verify    | End-to-end tests in Compose, variant configs, README                                                 | One scenario file per feature, written independently                   | CI passes on the sample config plus 2 variant configs                        |

Building happens in parallel, but merging follows the milestones in the critical path section, so `main` always runs.

## Wave 0: shared contracts

Once these interfaces and fixtures are merged, every track can compile and test without another track's code. Keep Wave 0 to exactly this list. No implementation goes in except stubs.

```go
// YAML node tree: A0 produces it, A2 decodes it. Agreeing it now lets both start at once.
type Node struct {
    Kind   Kind              // Map | Seq | Scalar | Null
    Value  string            // scalars only
    Quoted bool
    Keys   []string          // map keys in file order
    Map    map[string]*Node
    Items  []*Node
    Line, Col int
}
func ParseYAML(src []byte) (*Node, error) // errors carry line:col

// Normalized model: exactly spec §10. Track A produces it; every other track consumes it.
type GatewayConfig struct { Port int; Routes []*Route }
type Route struct { Path string; Methods map[string]bool; StripPrefix bool
    Targets []Target; Balance string; Timeout time.Duration
    Retry *Retry; HealthCheck *HealthCheck; RateLimit *RateLimit; Auth *Auth
    CircuitBreaker *CircuitBreaker; ReqTransform *ReqTransform; RespTransform *RespTransform }

// Request flow
type RequestContext struct { Req *http.Request; ClientIP string; Route *Route
    RequestTime time.Time; Body []byte; Upstream *http.Response; ResponseTime time.Time }
type Handler func(ctx *RequestContext) *Response
type Middleware func(next Handler) Handler

// Strategy interfaces: Track E implements them, Wave 2 wires them in
type Balancer interface       { Pick(pool TargetPool) (*Target, bool) }
type TargetPool interface     { Targets() []*Target; Healthy(t *Target) bool; MarkProbe(t *Target, ok bool) }
type RateLimiter interface    { Allow(key string, now time.Time) Decision }
type Authenticator interface  { Check(h http.Header) error }
type CircuitBreaker interface { TryPass(now time.Time) (ok bool, retryAfter int); Record(ok bool, now time.Time) }
type RetryPolicy interface    { ShouldRetry(status, attempt int) bool; Delay(attempt int) time.Duration }

// Shared helpers
type Clock interface { Now() time.Time }        // injected, so time logic can be unit-tested
type UpstreamClient interface { Send(ctx context.Context, t *Target, r *OutRequest) (*Response, error) }
func Classify(resp *Response, err error) int    // connection error -> 502, timeout -> 504
func ErrorResponse(status int, code, msg string, extra map[string]any) *Response // spec §12
```

Wave 0 also delivers:

- **W0.1:** the repo layout, gateway `Dockerfile`, the Compose skeleton (gateway plus six mock services running a stub image), and one CI command that runs unit and e2e tests.
- **W0.2:** the interfaces above, plus `fixtures/`: hand-written normalized `Route` objects, one per sample route. With these, Tracks B, C and F can run before the loader exists.

**Which standard library package covers each need**

| Need                                                    | Package                                                                                                                                                        |
| ------------------------------------------------------- | -------------------------------------------------------------------------------------------------------------------------------------------------------------- |
| Server, client, connection pooling                      | `net/http`. Upstream calls use `http.Transport` directly rather than `httputil.ReverseProxy`, because retries and body transforms need the body held in memory |
| Timeouts, cancellation                                  | `context`, `time`                                                                                                                                              |
| JSON bodies and error responses                         | `encoding/json`                                                                                                                                                |
| Constant-time API-key comparison                        | `crypto/subtle`                                                                                                                                                |
| Shared state (rate-limit buckets, breaker, target pool) | `sync`, `sync/atomic`                                                                                                                                          |
| CLI flag, env var, config file                          | `flag`, `os`                                                                                                                                                   |
| Logging                                                 | `log/slog`                                                                                                                                                     |
| Unit tests with in-process fake upstreams               | `testing`, `net/http/httptest`                                                                                                                                 |
| YAML                                                    | Not in the standard library, so built as A0                                                                                                                    |

**Enforcing the standard-library rule.** `go.mod` has no `require` lines. CI runs `go list -m all` and fails if it lists anything besides the module itself. The Docker build compiles with the official `golang` image and copies the static binary into a `scratch` image. The mocks and the tests are built the same way.

## Work packages

Any package can start once its dependencies are done. Claude builds everything. "Main" is Claude's critical-path stream, and "agent N" is a parallel Claude agent working through one track on its own branch. Size: S is a few hours or less, M is half a day or more. Tracks: **A** config, **B** HTTP core, **C** proxy, **D** mocks and test harness, **E** pure-logic libraries, **F** wiring, **G** verification.

| ID   | Package                                                                                                                               | Wave | Depends on                                   | Size | Built by          |
| ---- | ------------------------------------------------------------------------------------------------------------------------------------- | ---- | -------------------------------------------- | ---- | ----------------- |
| W0.1 | Repo, `go.mod` with no requires, Dockerfiles, Compose skeleton, CI command with the standard-library check                            | 0    | —                                            | S    | Main              |
| W0.2 | Shared interfaces, including the YAML `Node` type, and model fixtures                                                                 | 0    | —                                            | S    | Main; you approve |
| A0   | YAML parser for the supported subset (spec §1): turns the file into a node tree with line and column on every node; conformance tests | 1    | W0.2                                         | M    | Agent 1           |
| A1   | Duration parser                                                                                                                       | 1    | W0.2                                         | S    | Agent 5           |
| A2   | Config path resolution (CLI, flag, env); decode the node tree into raw config structs, with `file:line:col` errors                    | 1    | W0.2 (develop against hand-built node trees) | S    | Agent 5           |
| A3   | Validator that collects every error with its location (spec §11)                                                                      | 1    | W0.2                                         | M    | Agent 5           |
| A4   | Normalizer: defaults, rate-limit merge, lenient placement                                                                             | 1    | A1                                           | M    | Agent 5           |
| B1   | HTTP server and `GET /health`                                                                                                         | 1    | W0.2                                         | S    | Main              |
| B2   | Router: longest prefix, segment boundaries, 404/405 + `Allow`                                                                         | 1    | W0.2                                         | S    | Main              |
| B3   | Pipeline runner and error responder                                                                                                   | 1    | W0.2                                         | S    | Main              |
| C1   | Path rewrite: strip_prefix, base path, query string (pure function)                                                                   | 1    | W0.2                                         | S    | Main              |
| C2   | Upstream client on `http.Transport`: forwarding, hop-by-hop headers, `X-Forwarded-*`, body buffering                                  | 1    | W0.2                                         | M    | Main              |
| C3   | Per-attempt timeout via `context` and 502/504 classification                                                                          | 1    | C2                                           | S    | Main              |
| D1   | Mock upstream service in Go (next sections)                                                                                           | 1    | W0.1                                         | M    | Agent 2           |
| D2   | Compose: six mocks, switchable config                                                                                                 | 1    | D1                                           | S    | Agent 2           |
| D3   | E2E harness in Go `testing`: mock reset, request-log assertions, helpers                                                              | 1    | D1                                           | M    | Agent 2           |
| D4   | Two or more variant configs (spec §13), kept within the supported YAML                                                                | 1    | spec only                                    | S    | Agent 2           |
| E1   | Rate limiter: fixed and sliding windows, bucket store, key extraction, cleanup                                                        | 1    | W0.2                                         | M    | Agent 3           |
| E2   | Balancers: round robin, smooth weighted round robin; target pool health state                                                         | 1    | W0.2                                         | S    | Agent 3           |
| E3   | Circuit breaker state machine                                                                                                         | 1    | W0.2                                         | S    | Agent 3           |
| E4   | Retry policy and backoff                                                                                                              | 1    | W0.2                                         | S    | Agent 3           |
| E5   | Variable resolver and header transformer                                                                                              | 1    | W0.2                                         | S    | Agent 4           |
| E6   | Dot-path get/set and body mapper                                                                                                      | 1    | W0.2                                         | M    | Agent 4           |
| E7   | Envelope builder                                                                                                                      | 1    | E5                                           | S    | Agent 4           |
| E8   | API-key authenticator (`crypto/subtle`)                                                                                               | 1    | W0.2                                         | S    | Agent 4           |
| F1   | `main()`: load, normalize, build each route's pipeline, serve                                                                         | 2    | A0–A4, B1–B3                                 | S    | Main              |
| F2   | Rate-limit middleware with 429 and `X-RateLimit-*` headers                                                                            | 2    | B3, E1                                       | S    | Agent 3           |
| F3   | Auth middleware                                                                                                                       | 2    | B3, E8                                       | S    | Agent 4           |
| F4   | Request-transform middleware, including 400 for invalid JSON                                                                          | 2    | B3, E5, E6                                   | S    | Agent 4           |
| F5   | Upstream orchestrator: breaker check, pick target, proxy, record result, retry loop                                                   | 2    | C1–C3, E2, E3, E4                            | M    | Main              |
| F6   | Response-transform middleware: buffer body, request identity encoding                                                                 | 2    | C2, E5, E7                                   | S    | Agent 4           |
| F7   | Health-check background loop                                                                                                          | 2    | C2, E2                                       | S    | Agent 3           |
| G1   | E2E scenarios, one file per feature                                                                                                   | 3    | D3 + that feature's F package                | M    | Agent 2           |
| G2   | CI matrix: sample config plus variant configs                                                                                         | 3    | D2, D4                                       | S    | Agent 2           |
| G3   | README: how to run, decisions (spec §14)                                                                                              | 3    | —                                            | S    | Main; you approve |

Each package merges with its own unit tests. Each F package also merges with at least one G1 scenario.

## Critical path and verification gates

Claude's main stream is the critical path: **W0.2 → B1–B3 and C1–C3 → F1 → F5 → G1**. Agents 1–5 build tracks A, D and E alongside it. The YAML parser (A0) gets its own agent, so the config track doesn't become the longest stream. Nothing from the next stage merges until you approve the current gate. That keeps `main` working, so there is always a version you could submit.

| Gate               | What merges            | Evidence Claude hands you                                                                                                                                                                               | What you check                                                                                        |
| ------------------ | ---------------------- | ------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------------- | ----------------------------------------------------------------------------------------------------- |
| M0 Contracts       | W0.1, W0.2             | The interface file, fixtures, a go.mod with no requires (the CI check passes), and a `docker compose up` log showing all 7 containers running                                                           | The interfaces match spec §10, and nothing in them looks wrong to you                                 |
| M1 Core            | A1–A4, B, C, D1–D3, F1 | An e2e report on the 5 baseline requirements, run against the sample config and one variant config; a curl transcript of health, proxy, 404 and 405; the error output from a deliberately broken config | Every baseline requirement passes. Bad configs fail with errors that point to the problem in the file |
| M2 Global features | F2, F3                 | A transcript of 429s once each route's limit is hit, with their headers; 401s for a missing key and for a wrong key                                                                                     | Per-route limits match the config; behavior matches spec §6 and §8                                    |
| M3 Resilience      | F5, F7                 | Retry timings (1s, 2s, 4s); the circuit breaker tripping to 503 with `retry_after`, then recovering; the 3:1 split counted from mock logs; a target going unhealthy and recovering                      | Numbers and states match spec §5 and §7                                                               |
| M4 Transforms      | F4, F6                 | Each request as the client sent it and as the mock received it, side by side; the same for responses                                                                                                    | Mapping and envelope match spec §9 exactly                                                            |
| M5 Finish          | G1, G2, G3             | CI passing on every config; the README                                                                                                                                                                  | The README's decisions match spec §14                                                                 |

If time runs out, the submission is whatever passed the last approved gate.

## Dummy targets: the mock upstream

One small mock-upstream image runs as six containers on ports 3001–3006, one per sample route. Tests control its behavior per request with headers, and check what it received through an admin API. It shares no code with the gateway, so D1 can start straight after W0.1.

| Env var                  | Default    | Purpose                                                                    |
| ------------------------ | ---------- | -------------------------------------------------------------------------- |
| `SERVICE_NAME`           | `mock`     | Included in every response, so load-balancing tests can tell targets apart |
| `PORT`                   | `3000`     | Listen port                                                                |
| `HEALTH_PATH`            | `/healthz` | Health endpoint; returns 200 unless changed through the admin API          |
| `EXTRA_RESPONSE_HEADERS` | empty      | e.g. `Server=mock;X-Powered-By=mock`, for testing header removal           |

**Default behavior:** any method on any path returns 200 with an `X-Mock-Service` header, and echoes the request back as JSON:

```json
{
  "service": "legacy",
  "method": "POST",
  "path": "/v1/data",
  "query": { "x": "1" },
  "headers": { "x-gateway": "gatewaykit" },
  "body": { "user": { "id": 7 } }
}
```

**Per-request controls.** Tests send these headers through the gateway:

| Request header                             | Effect                                                                                                 | Tests                                                |
| ------------------------------------------ | ------------------------------------------------------------------------------------------------------ | ---------------------------------------------------- |
| `X-Mock-Status: 503`                       | Respond with this status                                                                               | Retry `on` list, circuit breaker, envelope on errors |
| `X-Mock-Delay-Ms: 6000`                    | Wait this long before responding                                                                       | 504s, timeout overrides                              |
| `X-Mock-Fail-Times: 2` + `X-Mock-Key: abc` | The first 2 requests with key `abc` fail (using `X-Mock-Status`, default 503), then later ones succeed | Retry succeeding after N failures                    |
| `X-Mock-Body: text`                        | Return a plain-text body instead of JSON                                                               | Envelope with a raw-string `$body`                   |
| `X-Mock-Drop: 1`                           | Close the socket without responding                                                                    | 502 classification                                   |

**Admin API.** Tests call each mock's port directly, not through the gateway:

| Endpoint                                                | Effect                                                                          | Tests                                                                                    |
| ------------------------------------------------------- | ------------------------------------------------------------------------------- | ---------------------------------------------------------------------------------------- |
| `GET /__mock/requests`                                  | Every request received since the last reset (method, path, headers, body, time) | strip_prefix, header and body transforms, number of retry attempts, load-balancing ratio |
| `POST /__mock/reset`                                    | Clears the request log, counters and overrides                                  | Run before each test                                                                     |
| `POST /__mock/health` `{"status": 503}`                 | Makes `HEALTH_PATH` return that status                                          | Health checks marking a target unhealthy, then recovering                                |
| `POST /__mock/default` `{"status": 503, "delay_ms": 0}` | Applies that response to every request until reset                              | Tripping the circuit breaker, all targets failing                                        |

To simulate a dead target (connection refused), stop its container: `docker compose stop products-b`.

## Docker Compose

The gateway and all six mocks share one network namespace, so the sample config's `http://localhost:300x` URLs work unchanged inside Docker. With normal Compose networking, `localhost` inside the gateway container is the gateway itself, so the sample config would only work after rewriting its URLs to service names like `http://users:3001`. Each mock uses `network_mode: "service:gateway"` to join the gateway's network namespace instead.

```yaml
services:
  gateway:
    build: .
    environment:
      GATEWAY_CONFIG: /config/${CONFIG:-gateway.yaml}
    volumes:
      - ./configs:/config:ro
    ports: # ports must be published here; mocks have no network of their own
      - "8080:8080" # gateway; add each port your variant configs use
      - "3001-3006:3001-3006" # mock admin APIs, reachable from the host

  users: &mock
    build: ./mock-upstream
    network_mode: "service:gateway"
    environment: { SERVICE_NAME: users, PORT: "3001" }
  orders:
    <<: *mock
    environment: { SERVICE_NAME: orders, PORT: "3002" }
  products-a:
    <<: *mock
    environment: { SERVICE_NAME: products-a, PORT: "3003" }
  products-b:
    <<: *mock
    environment: { SERVICE_NAME: products-b, PORT: "3004" }
  legacy:
    <<: *mock
    environment:
      SERVICE_NAME: legacy
      PORT: "3005"
      EXTRA_RESPONSE_HEADERS: "Server=mock;X-Powered-By=mock"
  internal:
    <<: *mock
    environment: { SERVICE_NAME: internal, PORT: "3006" }

  tests:
    build: ./tests
    network_mode: "service:gateway"
    profiles: ["test"]
    environment: { GATEWAY_URL: "http://localhost:8080" }
```

| Command                                   | What it does                                                        |
| ----------------------------------------- | ------------------------------------------------------------------- |
| `docker compose up --build`               | Starts the gateway with the sample `gateway.yaml` and all six mocks |
| `CONFIG=variant-b.yaml docker compose up` | Same, with a variant config from `./configs`                        |
| `docker compose run --rm tests`           | Runs the e2e suite inside the shared namespace                      |
| `docker compose stop products-b`          | Simulates a dead target for health-check and 502 tests              |
| `docker compose up -d --force-recreate`   | Restarts everything together (see below)                            |

**Gotchas.**

- If you restart only the gateway, the mocks lose their network, because they live in its namespace. Always recreate everything together.
- Variant configs should reuse ports 3001–3006 with different paths and settings. If a config needs another port, add a mock service and publish that port on `gateway`. Spec §13's example B uses ports 4000–4003, so remap it.
- For quick runs while developing, you can also run the gateway binary on the host. Run the mocks with `network_mode: host` (Linux) or publish their ports, and the same `localhost` URLs still work.

## The control loop

You direct and Claude builds. In Wiener's terms, the spec is the reference signal, the gate evidence is the feedback, and your corrections are the control input. Claude never declares a gate passed on its own.

1. **Goal.** The spec doc defines correct behavior. Any change of direction goes into the spec first and the code second, so the two never disagree.
2. **Act.** Claude's main stream works the critical path while up to five agents work in parallel, each in its own worktree and branch:
   - agent 1: A0, the YAML parser. Agent 5: A1–A4, built against hand-made node trees until A0 lands
   - agent 2: D1–D4, then G1–G2
   - agent 3: E1–E4, then F2 and F7
   - agent 4: E5–E8, then F3, F4 and F6

   Agents code only against the approved W0.2 interfaces.

3. **Measure.** At each gate, Claude runs everything in Compose itself and hands you a short gate report:
   - what merged
   - the test results
   - a transcript of real requests and responses
   - anything that differs from the spec
4. **Correct.** You approve, or send corrections in plain language or as comments on either doc. Claude applies them, reruns everything and reports again. Only then does work for the next gate merge.

Claude stops and waits for you when:

- a W0.2 interface would have to change
- the spec is ambiguous in a way spec §14 doesn't settle
- a test would have to be weakened to pass
