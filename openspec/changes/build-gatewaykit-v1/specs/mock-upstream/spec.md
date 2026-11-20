# Spec Delta

## Purpose

Provides a controllable echo service that stands in for every upstream, so e2e tests can steer upstream behavior per request and inspect what the gateway sent.

## ADDED Requirements

### Requirement: Environment configuration
The mock SHALL read `SERVICE_NAME` (default `mock`), `PORT` (default `3000`), `HEALTH_PATH` (default `/healthz`) and `EXTRA_RESPONSE_HEADERS` (default empty, format `Name=value;Name=value`) from the environment.

#### Scenario: Defaults
- **WHEN** the mock starts with none of the variables set
- **THEN** it listens on port 3000, reports service `mock` and serves health on `/healthz`

#### Scenario: Custom port and name
- **WHEN** the mock starts with `SERVICE_NAME=orders` and `PORT=3002`
- **THEN** it listens on port 3002 and reports service `orders`

### Requirement: Default echo response
For any method on any path, the mock SHALL return 200 with an `X-Mock-Service` header set to its service name and a JSON body echoing the request: `service`, `method`, `path`, `query`, `headers` and `body`.

#### Scenario: Echo a POST
- **WHEN** the `legacy` mock receives `POST /v1/data?x=1` with header `X-Gateway: gatewaykit` and body `{"user":{"id":7}}`
- **THEN** it returns 200 with `X-Mock-Service: legacy`
- **AND** the body is `{"service":"legacy","method":"POST","path":"/v1/data","query":{"x":"1"},"headers":{"x-gateway":"gatewaykit",...},"body":{"user":{"id":7}}}`

### Requirement: Extra response headers
The mock SHALL add every header listed in `EXTRA_RESPONSE_HEADERS` to each response.

#### Scenario: Server headers for removal tests
- **WHEN** the mock runs with `EXTRA_RESPONSE_HEADERS=Server=mock;X-Powered-By=mock` and receives any request
- **THEN** the response carries `Server: mock` and `X-Powered-By: mock`

### Requirement: Status control
When a request carries `X-Mock-Status: <code>`, the mock SHALL respond with that status.

#### Scenario: Forced 503
- **WHEN** a request carries `X-Mock-Status: 503`
- **THEN** the mock responds 503

### Requirement: Delay control
When a request carries `X-Mock-Delay-Ms: <n>`, the mock SHALL wait `n` milliseconds before responding.

#### Scenario: Slow upstream
- **WHEN** a request carries `X-Mock-Delay-Ms: 6000`
- **THEN** the mock responds no sooner than 6 seconds after receiving it

### Requirement: Fail-N-times control
When requests carry `X-Mock-Fail-Times: <n>` and `X-Mock-Key: <key>`, the mock SHALL fail the first `n` requests with that key, using the `X-Mock-Status` value or 503 by default, and SHALL let later requests with that key succeed.

#### Scenario: Two failures then success
- **WHEN** three requests carry `X-Mock-Fail-Times: 2` and `X-Mock-Key: abc`
- **THEN** the first two receive 503 and the third receives 200

#### Scenario: Keys counted separately
- **WHEN** one request carries key `abc` and another carries key `xyz`, both with `X-Mock-Fail-Times: 1`
- **THEN** each receives 503

### Requirement: Plain-text body control
When a request carries `X-Mock-Body: <text>`, the mock SHALL return `<text>` as a plain-text body instead of the JSON echo.

#### Scenario: Raw string body
- **WHEN** a request carries `X-Mock-Body: hello`
- **THEN** the mock returns the body `hello` with a plain-text content type

### Requirement: Drop control
When a request carries `X-Mock-Drop: 1`, the mock SHALL close the connection without sending a response.

#### Scenario: Dropped connection
- **WHEN** a request carries `X-Mock-Drop: 1`
- **THEN** the client of the mock sees the connection closed with no response

### Requirement: Request log
`GET /__mock/requests` SHALL return every non-admin request received since the last reset, each with method, path, headers, body and receive time. Admin API requests and `HEALTH_PATH` requests SHALL NOT appear in the log, so tests can count proxied calls exactly while health probes run.

#### Scenario: Count retry attempts
- **WHEN** the mock is reset and then receives three proxied requests
- **THEN** `GET /__mock/requests` returns exactly three entries

#### Scenario: Admin calls not logged
- **WHEN** a test calls `POST /__mock/reset` and then `GET /__mock/requests`
- **THEN** the log is empty

### Requirement: Reset
`POST /__mock/reset` SHALL clear the request log, the fail-N-times counters, the health override and the default-response override.

#### Scenario: Reset clears overrides
- **WHEN** a test sets `POST /__mock/default {"status":503}` and then calls `POST /__mock/reset`
- **THEN** the next request receives 200

### Requirement: Health override
The mock SHALL answer `HEALTH_PATH` with 200 by default. `POST /__mock/health` with `{"status": <code>}` SHALL make `HEALTH_PATH` return that status until changed or reset. Only this endpoint controls `HEALTH_PATH`: per-request control headers and `POST /__mock/default` MUST NOT affect it.

#### Scenario: Unhealthy target
- **WHEN** a test calls `POST /__mock/health {"status":503}`
- **THEN** `GET /healthz` on that mock returns 503

#### Scenario: Default override leaves health alone
- **WHEN** a test calls `POST /__mock/default {"status":503,"delay_ms":0}`
- **THEN** `GET /healthz` on that mock still returns 200
- **AND** the probe does not appear in `GET /__mock/requests`

### Requirement: Default response override
`POST /__mock/default` with `{"status": <code>, "delay_ms": <n>}` SHALL apply that status and delay to every non-admin request until reset.

#### Scenario: All requests fail
- **WHEN** a test calls `POST /__mock/default {"status":503,"delay_ms":0}`
- **THEN** every following proxied request receives 503 until `POST /__mock/reset`

### Requirement: Independent from the gateway
The mock SHALL share no code with the gateway and SHALL use the Go standard library only.

#### Scenario: Separate build
- **WHEN** the mock image builds
- **THEN** it compiles without importing any gateway package or third-party module
