# Spec Delta

## Purpose

Sends each routed request to one healthy upstream target, chosen by round-robin or smooth weighted round-robin, forwards it with correct header handling and a per-attempt timeout, and maps upstream failures to 502, 503 or 504.

## ADDED Requirements

### Requirement: Upstream targets list
The gateway SHALL treat every upstream as a list of targets. An upstream with a single `url` MUST behave as one target with weight 1.

#### Scenario: Single url proxies to one target
- **WHEN** a route has `upstream.url: "http://localhost:3001"` and a client sends a request it allows
- **THEN** the gateway forwards the request to `localhost:3001`

### Requirement: Round-robin balancing
With `balance: round_robin`, the gateway SHALL cycle through the route's healthy targets in the order they are listed and MUST ignore weights.

#### Scenario: Even rotation in listed order
- **WHEN** a route has targets A and B with `balance: round_robin` and both are healthy
- **THEN** four consecutive requests go to A, B, A, B

#### Scenario: Weights ignored
- **WHEN** a route has targets A (weight 3) and B (weight 1) with `balance: round_robin`
- **THEN** four consecutive requests go to A, B, A, B

### Requirement: Smooth weighted round-robin balancing
With `balance: weighted_round_robin`, the gateway SHALL pick targets by smooth weighted round-robin, so that every run of consecutive picks spanning the sum of the weights matches the weight ratio exactly.

#### Scenario: 3:1 pick order
- **WHEN** a route has targets A (weight 3) and B (weight 1) with `balance: weighted_round_robin` and both are healthy
- **THEN** consecutive requests go to A, A, B, A, repeating

#### Scenario: Exact ratio over the sample route
- **WHEN** the sample config is loaded and a client sends 8 requests to `/api/products`
- **THEN** `products-a` receives exactly 6 and `products-b` exactly 2, counted from the mock request logs

### Requirement: Unhealthy targets skipped
The gateway SHALL skip targets marked unhealthy when picking a target.

#### Scenario: Unhealthy target receives no traffic
- **WHEN** a route has targets A and B and B is marked unhealthy
- **THEN** every request goes to A until B is healthy again

### Requirement: Per-route rotation state
The gateway SHALL keep balancing state per route, in memory, so traffic on one route does not shift the rotation of another.

#### Scenario: Routes rotate independently
- **WHEN** two routes each have targets A and B with `balance: round_robin`
- **AND** a client sends one request to the first route, then one to the second
- **THEN** both requests go to A

### Requirement: No healthy upstream
When every target of a route is unhealthy, the gateway SHALL return 503 with `"error":"no_healthy_upstream"` and a `message`, without contacting any target.

#### Scenario: All targets unhealthy
- **WHEN** every target of a route is marked unhealthy and a client sends a request it allows
- **THEN** the gateway returns 503 with `"error":"no_healthy_upstream"` and a `message` string

### Requirement: Forward method, body and end-to-end headers
The gateway SHALL forward the request method, the request body and all end-to-end request headers to the chosen target. The gateway SHALL hold the request body in memory before forwarding.

#### Scenario: POST forwarded intact
- **WHEN** a client sends `POST /api/users` with a JSON body and header `X-Custom: 1`
- **THEN** the upstream receives method POST, the same body and the header `X-Custom: 1`

### Requirement: Remove hop-by-hop headers
The gateway MUST remove the hop-by-hop headers `Connection`, `Keep-Alive`, every `Proxy-*` header, `TE`, `Trailer`, `Transfer-Encoding` and `Upgrade` from forwarded requests.

#### Scenario: Hop-by-hop headers stripped
- **WHEN** a client sends a request with `Keep-Alive: timeout=5`, `Proxy-Authorization: x` and `Upgrade: websocket`
- **THEN** the upstream receives none of those headers

### Requirement: Host header set to target
The gateway SHALL set the forwarded `Host` header to the chosen target's host.

#### Scenario: Host rewritten
- **WHEN** a client sends `Host: gateway.example` to a route whose target is `http://localhost:3001`
- **THEN** the upstream receives `Host: localhost:3001`

### Requirement: Forwarded headers
The gateway SHALL append the client IP, taken from the socket's remote address, to `X-Forwarded-For`, and SHALL set `X-Forwarded-Proto` and `X-Forwarded-Host` on the forwarded request.

#### Scenario: Client IP appended
- **WHEN** a client at 10.0.0.5 sends a request with `X-Forwarded-For: 1.2.3.4`
- **THEN** the upstream receives `X-Forwarded-For: 1.2.3.4, 10.0.0.5`

#### Scenario: Proto and host set
- **WHEN** a client sends a plain HTTP request with `Host: gateway.example:8080`
- **THEN** the upstream receives `X-Forwarded-Proto: http` and `X-Forwarded-Host: gateway.example:8080`

### Requirement: Per-attempt timeout
The gateway SHALL limit each upstream attempt to the route's normalized timeout, covering connection through the full response body.

#### Scenario: Route timeout beats global
- **WHEN** variant config B is loaded, `/v2` has a 2s route timeout with a 750ms global timeout, and its upstream responds after 1s
- **THEN** the gateway proxies the upstream response

#### Scenario: Slow body counts toward the timeout
- **WHEN** an upstream sends headers at once but finishes the body after the route's timeout
- **THEN** the gateway returns 504

### Requirement: Unreachable upstream returns 502
When the target refuses or resets the connection, or returns an invalid response, the gateway SHALL treat the attempt as status 502 and, once no retry follows, return 502 with `"error":"bad_gateway"`.

#### Scenario: Connection refused
- **WHEN** a route's only target is stopped and a client sends a request it allows
- **THEN** the gateway returns 502 with `"error":"bad_gateway"`

#### Scenario: Socket closed without a response
- **WHEN** the upstream closes the socket without responding
- **THEN** the gateway returns 502 with `"error":"bad_gateway"`

### Requirement: Timed-out upstream returns 504
When an attempt exceeds the timeout, the gateway SHALL treat the attempt as status 504 and, once no retry follows, return 504 with `"error":"gateway_timeout"`.

#### Scenario: Upstream slower than the timeout
- **WHEN** `/api/orders` has a 5s timeout and its upstream delays 6s on every attempt
- **THEN** the gateway returns 504 with `"error":"gateway_timeout"`
