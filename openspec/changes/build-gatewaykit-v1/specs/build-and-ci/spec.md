# Spec Delta

## Purpose

Keeps GatewayKit buildable from the Go standard library alone, packages the gateway and mocks as containers, and proves every behavior in CI against the sample and variant configs.

## ADDED Requirements

### Requirement: Standard library only
The gateway, the mock upstream and the tests SHALL use the Go standard library only. `go.mod` SHALL contain no `require` lines.

#### Scenario: No requires
- **WHEN** a reviewer reads `go.mod`
- **THEN** it declares the module `gatewaykit` and contains no `require` line

### Requirement: Dependency check in CI
In each of the gateway, mock and test modules, CI SHALL fail if `go.mod` has a `require` line or if `go list -m all` lists any module besides the module itself.

#### Scenario: Third-party module added
- **WHEN** a change adds any outside module
- **THEN** the CI command exits non-zero and names the extra module

### Requirement: Scratch images
The gateway, mock and test images SHALL compile with the official `golang` image and copy a static binary into a `scratch` image.

#### Scenario: Gateway image
- **WHEN** `docker compose build` runs
- **THEN** the gateway image contains a static binary on a `scratch` base

### Requirement: Compose topology
Compose SHALL run the gateway plus six mock services: `users` on 3001, `orders` on 3002, `products-a` on 3003, `products-b` on 3004, `legacy` on 3005 with `EXTRA_RESPONSE_HEADERS=Server=mock;X-Powered-By=mock`, and `internal` on 3006. Each mock SHALL join the gateway's network namespace so `http://localhost:300x` URLs work unchanged.

#### Scenario: Stack starts
- **WHEN** a user runs `docker compose up --build`
- **THEN** seven containers run and the gateway reaches each mock at `http://localhost:3001` through `http://localhost:3006`

### Requirement: Config selection
Compose SHALL pass `GATEWAY_CONFIG=/config/${CONFIG}` to the gateway, mounting `./configs` read-only at `/config`, with `CONFIG` defaulting to `gateway.yaml`.

#### Scenario: Variant config
- **WHEN** a user runs `CONFIG=variant-b.yaml docker compose up`
- **THEN** the gateway loads `/config/variant-b.yaml`

### Requirement: Published ports
The gateway service SHALL publish port 8080, ports 3001 to 3006 for the mock admin APIs, and every listen port a variant config uses, including 9090 for variant B.

#### Scenario: Mock admin from the host
- **WHEN** the stack runs and a host process calls `POST http://localhost:3002/__mock/reset`
- **THEN** the `orders` mock answers

#### Scenario: Variant B port
- **WHEN** the stack runs with variant B
- **THEN** `GET http://localhost:9090/health` from the host returns 200

### Requirement: E2E test container
`docker compose run --rm tests` SHALL run the e2e suite inside the gateway's network namespace with `GATEWAY_URL` pointing at the gateway.

#### Scenario: Run e2e
- **WHEN** a user runs `docker compose run --rm tests` with the stack up
- **THEN** the suite runs against `http://localhost:8080` and exits non-zero on any failure

### Requirement: Single CI command
One CI command SHALL run the unit tests, the dependency check and the e2e suite against the sample config and every variant config, and SHALL exit non-zero if any step fails.

#### Scenario: Full matrix
- **WHEN** the CI command runs on a clean checkout
- **THEN** it runs unit tests, the dependency check, and e2e on `gateway.yaml` and each variant config in turn

### Requirement: Sample config
`configs/gateway.yaml` SHALL hold the brief's sample config re-indented into canonical placement: `routes` at the top level, `timeout`, `retry` and `health_check` under `upstream`, and `circuit_breaker` on the route. It SHALL keep every value from the brief.

#### Scenario: Sample loads
- **WHEN** the gateway starts with `configs/gateway.yaml`
- **THEN** it loads five routes: `/api/users`, `/api/orders`, `/api/products`, `/api/legacy` and `/api/internal`

### Requirement: Variant configs
`configs/` SHALL hold variant configs A and B from config spec §13, with upstream ports 4000 to 4003 remapped onto 3001 to 3004, plus a third variant whose route sets `retry: {attempts: 4, backoff: exponential, initial_delay: "1s"}`. Every variant SHALL stay within the supported YAML subset.

#### Scenario: Smallest config
- **WHEN** the gateway runs variant A and a client sends `GET :8080/anything`
- **THEN** the request reaches the mock on port 3001 at `/anything`

#### Scenario: Retry timing variant
- **WHEN** the e2e suite runs the third variant against an upstream that always returns 503
- **THEN** it observes waits of 1s, 2s and 4s between the four attempts

### Requirement: README
`README.md` SHALL document how to build, run and test the gateway, and SHALL list the decisions from config spec §14, recording that a route's `rate_limit: false` turns off the inherited global limit.

#### Scenario: Reader follows the README
- **WHEN** a reader runs the commands in the README on a clean checkout
- **THEN** the stack starts and the tests run as described

#### Scenario: Decisions listed
- **WHEN** a reader opens the decisions section
- **THEN** it covers every §14 row, with `rate_limit: false` marked as supported

### Requirement: Gate reports
For each milestone M0 to M5, the build SHALL produce a gate report listing what merged, the test results, a transcript of real requests and responses, and any difference from the spec. Gate reports SHALL NOT block the next milestone.

#### Scenario: Milestone finished
- **WHEN** the work for a milestone is done
- **THEN** its gate report exists and the next milestone starts without an approval pause
