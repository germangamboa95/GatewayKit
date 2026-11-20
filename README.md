# GatewayKit

An HTTP API gateway written in Go. You describe your routes in one YAML file. GatewayKit matches each request to a route by longest path prefix and forwards it to that route's upstream.

## Features

Each route can turn on any of these:

- **Routing**: longest-prefix match, a method allowlist, and optional `strip_prefix`
- **Load balancing**: round-robin or weighted round-robin across several targets
- **Health checks**: background probes that take failing targets out of rotation
- **Rate limiting**: fixed or sliding window, keyed per client IP or global. Set it once globally and override it per route, or turn it off with `rate_limit: false`
- **API key auth**: a header-based key check that returns 401 when the key is missing or wrong
- **Retries**: fixed or exponential backoff on chosen status codes
- **Circuit breaker**: after N failures in a window, returns 503 until a cooldown ends
- **Timeouts**: one global timeout, overridable per route
- **Transforms**: add or remove request and response headers, remap JSON bodies with dot paths, wrap responses in an envelope. Values can use variables such as `$request_time` and `$route_path`
- **Health endpoint**: `GET /health` returns `{"status":"healthy","uptime_seconds":<int>}`

`configs/gateway.yaml` is a commented example that uses every feature.

## Run it

Requires Go 1.27.4. Docker with Compose v2 is needed for the full stack and the e2e tests.

**Full stack (recommended):** the gateway on `:8080` plus six mock upstreams on `:3001` to `:3006`:

```sh
docker compose up --build
curl localhost:8080/health
curl localhost:8080/api/users
```

To use a different file from `configs/`, set `CONFIG`:

```sh
CONFIG=variant-b.yaml docker compose up --build   # variant B listens on :9090
```

Stop the stack with `docker compose --profile test down`.

**Gateway only:**

```sh
go run . configs/gateway.yaml
```

The config path comes from the first of these that is set: the first argument, `--config <path>`, `$GATEWAY_CONFIG`, or `./gateway.yaml`. An invalid config prints each error with `file:line:col` and exits 1.

## Test

```sh
go test ./...                      # gateway unit tests
scripts/e2e.sh variant-b.yaml      # e2e run against one config in Docker
scripts/ci.sh                      # everything: fmt, vet, unit tests, e2e on all configs
```

The e2e stacks all publish the same ports, so only one can run at a time. The scripts take a lock to enforce this.

## Where things live

| Path | What |
| --- | --- |
| `main.go` | Loads the config and wires each route's middleware chain |
| `internal/config` | Loads and validates `gateway.yaml` into the route model |
| `internal/gateway` | The pipeline types: request context, response, handler, middleware |
| `internal/server`, `internal/router` | The root `http.Handler` and longest-prefix routing |
| `internal/auth`, `internal/ratelimit`, `internal/transform` | Per-route middleware |
| `internal/upstream`, `internal/proxy`, `internal/balance`, `internal/breaker`, `internal/retry`, `internal/healthcheck` | Forwarding to targets |
| `configs/` | The sample config and three test variants |
| `mock-upstream/` | The mock service used by Compose. Its README covers the control headers |
| `tests/` | The e2e suite |
| `specs/`, `openspec/` | The original brief, the config spec, and the design decisions |
