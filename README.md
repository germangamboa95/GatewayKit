# GatewayKit

GatewayKit is an HTTP API gateway written in Go with the standard library only. It reads one YAML config, matches each request to a route by longest path prefix, and forwards it to that route's upstream. Per route it supports rate limiting, API key auth, request and response transforms, retries, a circuit breaker, and round-robin or weighted round-robin balancing across health-checked targets. `GET /health` returns `{"status":"healthy","uptime_seconds":<int>}`.

## Requirements

- Go 1.27.4
- Docker with Compose v2 (`docker compose`)
- `bash` and `curl` for the e2e scripts. `scripts/e2e.sh` also uses `lsof` to check for busy ports, and skips the check if `lsof` is missing.

The three Go modules (`.`, `mock-upstream`, `tests`) have no `require` lines.

## Run

Run the gateway directly:

```sh
go run . configs/gateway.yaml
```

The gateway takes the config path from the first source present:

1. The first CLI argument
2. The `--config <path>` flag
3. The `GATEWAY_CONFIG` env var
4. `./gateway.yaml`

If no file is found, the gateway exits 1 and lists the locations it tried. An invalid config makes it print every error with `file:line:col` and exit 1. Unknown keys log a warning and startup continues.

The sample routes point at `http://localhost:3001` to `3006`, which are the Compose mock services. Run the full stack with Compose:

```sh
docker compose up --build
```

This starts the gateway on port 8080 and six mocks on 3001 to 3006 in the gateway's network namespace. Compose mounts `./configs` read-only at `/config` and passes `GATEWAY_CONFIG=/config/${CONFIG}`, with `CONFIG` defaulting to `gateway.yaml`. Pick another config with `CONFIG`:

```sh
CONFIG=variant-b.yaml docker compose up --build
```

Variant B listens on port 9090; the others listen on 8080. Stop the stack with `docker compose --profile test down`.

| Service | Port |
| --- | --- |
| `gateway` | 8080, 9090 |
| `users` | 3001 |
| `orders` | 3002 |
| `products-a` | 3003 |
| `products-b` | 3004 |
| `legacy` | 3005 |
| `internal` | 3006 |

See `mock-upstream/README.md` for the mock control headers and admin API.

## Configs

| File | Covers | Port | Upstreams |
| --- | --- | --- | --- |
| `configs/gateway.yaml` | The brief's sample, re-indented into canonical placement | 8080 | 3001 to 3006 |
| `configs/variant-a.yaml` | Config spec §13 A: one route, every default | 8080 | 3001 |
| `configs/variant-b.yaml` | Config spec §13 B: lenient placement, `ms` durations, base path | 9090 | 3002 to 3004 |
| `configs/variant-c.yaml` | Fast resilience timings for e2e: retry `attempts: 4`, breaker, health checks, `rate_limit: false`, a 504 case | 8080 | 3001 to 3004, 3006 |

The config spec's variants use ports 4000 to 4003. The repo remaps them as `400N` to `300(N+1)` so they hit the Compose mocks.

## Test

Unit tests, in each of the three modules:

```sh
go test ./...
(cd mock-upstream && go test ./...)
(cd tests && go test ./...)
```

E2E against one config:

```sh
scripts/e2e.sh variant-b.yaml
```

`scripts/e2e.sh <config>` takes a file name from `configs/`. It builds and starts the stack with that config, waits for `/health` and the six mocks, runs the `tests` container, prints the gateway log tail on failure, and tears the stack down. `E2E_RUN=<regex>` limits the tests run. `LOCK_WAIT=<seconds>` waits for a busy lock.

Only one stack can run at a time, because every stack publishes the same host ports. `scripts/e2e.sh` takes a lock at `${TMPDIR:-/tmp}/gatewaykit-compose.lock` and stops if ports 8080, 9090 or 3001 to 3006 are already in use.

With a stack already up, run the suite directly:

```sh
docker compose run --rm --no-deps tests
```

`--no-deps` keeps Compose from recreating the gateway, which would drop the mocks out of its network namespace.

For variant B, set `CONFIG=variant-b.yaml GATEWAY_PORT=9090` on that command.

Full CI:

```sh
scripts/ci.sh
```

`scripts/ci.sh` runs, in order: `gofmt`, the standard-library check (no `require` line, and `go list -m all` prints only the module itself), `go vet` and `go test` in all three modules, then `scripts/e2e.sh` for each of the four configs, then a host-side dead-target check on `variant-c.yaml`. The check stops `products-b` with `docker compose stop`, expects a 502 `bad_gateway` on `/c/health`, then all `/c/health` traffic on `products-a` within 3s, then both targets serving again after `docker compose start products-b`. `scripts/ci.sh` holds the Compose lock for the whole run. It exits non-zero if any step fails.

`.github/workflows/ci.yml` runs `scripts/ci.sh` on every push and pull request, on `ubuntu-latest` with Go 1.27.4.

## Decisions

The brief leaves these points open. Config spec §14 settles them:

| Question | Decision | Why |
| --- | --- | --- |
| Where `routes` sits | Top level or `gateway.routes` | The sample lost its indentation; both forms load |
| Where `timeout` and `retry` sit | Under `upstream` or on the route | Same reason |
| Meaning of `retry.attempts` | Total tries, including the first | `attempts: 1` means no retry |
| Missing vs wrong API key | 401 for both | Does not reveal which keys exist |
| Forward the API key header upstream | Yes, unchanged | Keeps the gateway transparent |
| Trust `X-Forwarded-For` for the client IP | No, use the socket address | Clients can fake the header |
| Sliding window method | Exact sliding log | Exact; memory per bucket never exceeds `requests` timestamps |
| Fixed window alignment | Aligned to the clock | Simple and predictable in tests |
| Envelope on upstream 4xx and 5xx | Yes, status code kept | Upstream errors get the same shape as successes |
| `HEAD` allowed when `GET` is | No | Only the listed methods are allowed |
| `$var` inside a longer string | Not supported; whole value only | Nothing like `"v=$x"` appears in the sample |
| Turning off the global rate limit on one route | Supported: route-level `rate_limit: false` | The schema had no way to do this |
| Reloading the config without a restart | Out of scope for v1 | The brief does not ask for it |

Where the brief contradicts itself, the build resolves it as follows (design D12):

| Conflict | Resolution |
| --- | --- |
| §6 shows the 429 body without `message`; §12 says every gateway error has one except the breaker 503 | The 429 body is `{"error","message","retry_after"}` |
| §5 shows `{"error":"no_healthy_upstream"}` alone | Add `message`, per the same §12 rule |
| The base spec expects retry waits of 1s, 2s, 4s; the sample has `attempts: 3`, which is total tries | The sample shows 1s, 2s. `variant-c.yaml` with `attempts: 4` shows 1s, 2s, 4s |
| Variant B uses ports 4000 to 4003; the mocks run on 3001 to 3006 | Remap the ports and publish 9090 |
| The base spec gates wait for owner approval | Write each gate report to `openspec/changes/build-gatewaykit-v1/gates/M<n>.md` and continue |
| The base spec freezes the Wave 0 contract block as written | Apply seven fixes (design D2) and implement `ErrorResponse` and `Classify` in Wave 0, since the block does not compile as written |
| The base spec gives the variant configs and e2e scenarios to one agent | Variant configs ship in Wave 0; e2e scenarios are spread across agents |

## Route-level `rate_limit: false`

A route can opt out of `global_rate_limit`:

```yaml
gateway:
  global_rate_limit:
    requests: 5
    window: "60s"
routes:
  - path: "/c/unlimited"
    upstream:
      url: "http://localhost:3001"
    rate_limit: false
```

That route gets no limiter and its responses carry no `X-RateLimit-*` headers. Other routes keep the global limit. A route-level mapping such as `rate_limit: { requests: 1000 }` overrides the global limit field by field and inherits the rest. `rate_limit: true`, any other scalar, and `global_rate_limit: false` are type errors, so the config fails to load.

## Layout

| Path | Contents |
| --- | --- |
| `main.go` | Load, normalize, build pipelines, serve |
| `internal/contract` | Shared model, request flow types, strategy interfaces, `Clock`, `UpstreamClient`, `Classify`, `ErrorResponse` |
| `internal/yamlparse` | `Node` type and the YAML subset parser |
| `internal/config` | Path resolution, durations, decode, validate, normalize |
| `internal/fixtures` | One normalized `Route` per sample route |
| `internal/server` | HTTP server, `GET /health`, pipeline runner, error responder, access log |
| `internal/router` | Longest-prefix router, 404 and 405 |
| `internal/proxy` | Path rewrite, upstream client, per-attempt timeout |
| `internal/ratelimit`, `internal/balance`, `internal/breaker`, `internal/retry`, `internal/auth` | Pure-logic libraries |
| `internal/transform` | Variable resolver, header transformer, dot paths, body mapper, envelope |
| `internal/middleware/{ratelimit,auth,reqtransform,resptransform}` | One package per middleware |
| `internal/upstream` | Orchestrator: breaker, pick, proxy, record, retry |
| `internal/healthcheck` | Background probe loop |
| `testdata/` | Broken configs for the exit-code test |
| `mock-upstream/` | Mock service, own `go.mod` and `Dockerfile` |
| `tests/` | E2E suite, own `go.mod` and `Dockerfile` |
| `configs/` | The four configs above |
| `Dockerfile`, `docker-compose.yml` | Static binaries in `scratch` images, the Compose stack |
| `scripts/e2e.sh`, `scripts/ci.sh`, `.github/workflows/ci.yml` | One-config e2e run, full CI |

The mocks and the e2e tests are separate modules because Compose builds each from its own directory. They share no code with the gateway.
