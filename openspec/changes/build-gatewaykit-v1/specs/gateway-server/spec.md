# Spec Delta

## Purpose

Runs the gateway's HTTP listener, answers the reserved health endpoint, drives each request through the fixed stage order, and returns one JSON error contract for every error the gateway produces itself.

## ADDED Requirements

### Requirement: Listen on the configured port
The gateway SHALL listen for HTTP requests on the normalized `port`, which is `gateway.port` when set and `8080` otherwise.

#### Scenario: Default port
- **WHEN** the gateway starts with a config that omits `gateway.port`
- **THEN** it listens on port 8080

#### Scenario: Port from config
- **WHEN** the gateway starts with variant config B, which sets `port: 9090`
- **THEN** it listens on port 9090
- **AND** `GET /health` on port 9090 returns 200

### Requirement: Reserved health endpoint
The gateway SHALL answer `GET /health` with status 200 and the JSON body `{"status":"healthy","uptime_seconds":<int>}`, where `uptime_seconds` is the whole number of seconds since the process started, rounded down.

#### Scenario: Health response body
- **WHEN** a client sends `GET /health` 5.7 seconds after the process started
- **THEN** the gateway returns 200 with `Content-Type: application/json`
- **AND** the body is `{"status":"healthy","uptime_seconds":5}`

### Requirement: Health endpoint precedes all other stages
The gateway MUST answer `GET /health` before routing, rate limiting and auth, and MUST NOT count it toward any rate limit.

#### Scenario: Health bypasses rate limiting
- **WHEN** the global rate limit is exhausted for a client IP
- **AND** that client sends `GET /health`
- **THEN** the gateway returns 200 with the health body
- **AND** the request does not change any rate-limit bucket

#### Scenario: Health with an empty route list
- **WHEN** the config has `routes: []`
- **THEN** `GET /health` returns 200 with the health body

### Requirement: Route with path /health
The gateway SHALL load a route whose path is `/health`, log a warning that the route never receives `GET /health`, and still route `/health` subpaths and non-GET methods on `/health` to that route.

#### Scenario: Warning at startup
- **WHEN** the config defines a route with `path: "/health"`
- **THEN** startup succeeds and logs a warning that the route will never receive `GET /health`

#### Scenario: GET /health stays reserved
- **WHEN** a route with `path: "/health"` exists and a client sends `GET /health`
- **THEN** the gateway returns the reserved health response and does not proxy the request

#### Scenario: Subpath reaches the route
- **WHEN** a route with `path: "/health"` exists and a client sends `GET /health/x`
- **THEN** the gateway routes the request to that route

#### Scenario: Other methods reach the route
- **WHEN** a route with `path: "/health"` allows POST and a client sends `POST /health`
- **THEN** the gateway routes the request to that route

### Requirement: JSON error contract
Every error the gateway produces itself SHALL be JSON with `Content-Type: application/json` and the body `{"error":"<code>","message":"<text>"}`, using the status and code pairs from config spec §12. Extra keys appear only where §12 lists them.

#### Scenario: Not found error shape
- **WHEN** no route matches a request
- **THEN** the gateway returns 404 with `Content-Type: application/json`
- **AND** the body has `"error":"not_found"` and a `message` string

#### Scenario: Status and code pairs
- **WHEN** the gateway produces an error itself
- **THEN** the status and `error` code match one row of: 400 `invalid_json_body`, 401 `unauthorized`, 404 `not_found`, 405 `method_not_allowed`, 429 `rate_limit_exceeded`, 502 `bad_gateway`, 503 `service_unavailable`, 503 `no_healthy_upstream`, 504 `gateway_timeout`

### Requirement: Circuit-breaker body exception
The circuit-breaker 503 SHALL be the only gateway error whose body omits `message`; its body MUST contain exactly the keys `error` and `retry_after`.

#### Scenario: Breaker 503 keys
- **WHEN** the gateway rejects a request because a route's circuit breaker is open
- **THEN** the body contains exactly `error` (`"service_unavailable"`) and `retry_after`, and no `message`

### Requirement: Upstream responses pass through
The gateway SHALL relay responses received from an upstream unchanged, apart from any transforms configured on the route. The gateway's error contract MUST NOT apply to upstream responses.

#### Scenario: Upstream error relayed as is
- **WHEN** an upstream on a route without transforms or retry returns 500 with a plain-text body
- **THEN** the client receives 500 with that plain-text body and the upstream's end-to-end headers

### Requirement: Request stage order
The gateway SHALL run each request through these stages in order, and each stage can end the request early: health check, route match (404) and method check (405), rate limit (429), auth (401), request transform (400), circuit-breaker check (503), target pick (503), proxy with timeout (502 or 504, returning to the breaker check when a retry is allowed), response transform, then respond and write the access log line.

#### Scenario: Rate limit runs before auth
- **WHEN** a route has both a rate limit and auth, its bucket is exhausted, and a request arrives without an API key
- **THEN** the gateway returns 429, not 401

#### Scenario: Method check runs before rate limit
- **WHEN** a request matches a route but uses a method the route does not allow
- **THEN** the gateway returns 405 and the request does not count toward the route's rate limit

#### Scenario: Early stage ends the request
- **WHEN** auth rejects a request with 401
- **THEN** the gateway does not run the request transform or contact any upstream

### Requirement: Access log line
The gateway SHALL write one access log line per request after the response is sent, containing the method, path, status, matched route (if any) and duration.

#### Scenario: Log line for a proxied request
- **WHEN** a client sends `GET /api/users/1` and the gateway returns 200
- **THEN** the gateway writes one log line with method `GET`, path `/api/users/1`, status 200, route `/api/users` and the request duration

#### Scenario: Log line for an unmatched request
- **WHEN** a client sends a request that matches no route
- **THEN** the gateway writes one log line with status 404 and no matched route

### Requirement: Empty route list
With `routes: []`, the gateway SHALL start, serve `GET /health`, and answer every other request with 404 `not_found`.

#### Scenario: Any path returns 404
- **WHEN** the config has `routes: []` and a client sends `GET /anything`
- **THEN** the gateway returns 404 with `"error":"not_found"`
