# Spec Delta

## Purpose

Selects the route for each request by longest literal path prefix on segment boundaries, enforces each route's allowed methods with 404 and 405, and computes the path forwarded to the upstream.

## ADDED Requirements

### Requirement: Prefix match on segment boundaries
A route SHALL match a request when the request path equals the route's `path` or starts with the route's `path` followed by `/`. Matching MUST compare the path without the query string and MUST be case-sensitive.

#### Scenario: Exact, trailing slash and subpath match
- **WHEN** a route has `path: "/api/users"`
- **THEN** requests to `/api/users`, `/api/users/` and `/api/users/42` match it

#### Scenario: No match across a segment boundary
- **WHEN** a route has `path: "/api/users"` and a client requests `/api/usersX`
- **THEN** the route does not match

#### Scenario: Query string ignored for matching
- **WHEN** a route has `path: "/api/users"` and a client requests `/api/users?id=1`
- **THEN** the route matches

#### Scenario: Case-sensitive matching
- **WHEN** a route has `path: "/api/users"` and a client requests `/API/users`
- **THEN** the route does not match

#### Scenario: Root route matches everything
- **WHEN** a route has `path: "/"` and a client requests `/anything/at/all`
- **THEN** the route matches

### Requirement: Longest prefix wins
When several routes match a request, the gateway SHALL select the route with the longest `path`. The order of routes in the config file MUST NOT affect selection.

#### Scenario: Longer prefix selected
- **WHEN** routes `/v2` and `/v2/admin` both exist and a client sends `GET /v2/admin/x`
- **THEN** the gateway selects `/v2/admin`

#### Scenario: File order has no effect
- **WHEN** route `/v2/admin` appears before route `/v2` in the file and a client sends `GET /v2/a`
- **THEN** the gateway selects `/v2`

### Requirement: No matching route
When no route matches the request path, the gateway SHALL return 404 with `"error":"not_found"`.

#### Scenario: Segment boundary miss returns 404
- **WHEN** variant config B is loaded and a client sends `GET /v2x`
- **THEN** the gateway returns 404 with `"error":"not_found"`

### Requirement: Method not allowed
When the selected route does not list the request's method, the gateway SHALL return 405 with `"error":"method_not_allowed"` and an `Allow` header that lists the route's methods in config order after duplicates are removed, joined by `, `.

#### Scenario: Allow header lists route methods
- **WHEN** variant config B is loaded and a client sends `POST /v2/a`
- **THEN** the gateway returns 405 with `"error":"method_not_allowed"`
- **AND** the response has the header `Allow: GET, DELETE`

### Requirement: No fallback to a shorter route
When the longest matching route rejects the method, the gateway MUST return 405 and MUST NOT try a shorter matching route that would accept the method.

#### Scenario: Shorter route accepting the method is ignored
- **WHEN** route `/api` allows POST, route `/api/items` allows only GET, and a client sends `POST /api/items`
- **THEN** the gateway returns 405 with `Allow: GET`

### Requirement: Only listed methods are allowed
The gateway SHALL allow only the methods a route lists. `HEAD` MUST NOT be implied by `GET`.

#### Scenario: HEAD rejected on a GET-only route
- **WHEN** a route lists only `GET` and a client sends `HEAD` to its path
- **THEN** the gateway returns 405 with `Allow: GET`

### Requirement: All methods when methods is omitted
A route without `methods` SHALL allow all seven methods: GET, POST, PUT, PATCH, DELETE, HEAD and OPTIONS.

#### Scenario: Omitted methods allow DELETE
- **WHEN** a route omits `methods` and a client sends `DELETE` to its path
- **THEN** the gateway does not return 405

### Requirement: Strip prefix rewriting
When a route sets `strip_prefix: true`, the gateway SHALL remove the route's `path` from the start of the request path before forwarding, and MUST forward `/` when the result is empty. With `strip_prefix: false` the gateway SHALL forward the request path unchanged.

#### Scenario: Prefix stripped from a subpath
- **WHEN** route `/api/products` has `strip_prefix: true` and upstream `http://h:3003`, and a client requests `/api/products/123?x=1`
- **THEN** the upstream receives `/123?x=1`

#### Scenario: Prefix not stripped
- **WHEN** route `/api/products` has `strip_prefix: false` and upstream `http://h:3003`, and a client requests `/api/products/123?x=1`
- **THEN** the upstream receives `/api/products/123?x=1`

#### Scenario: Empty result becomes root
- **WHEN** route `/api/products` has `strip_prefix: true` and a client requests `/api/products`
- **THEN** the upstream receives `/`

#### Scenario: Exact path without strip
- **WHEN** route `/api/products` has `strip_prefix: false` and a client requests `/api/products`
- **THEN** the upstream receives `/api/products`

#### Scenario: Legacy route strip
- **WHEN** route `/api/legacy` has upstream `http://h:3005` and a client requests `/api/legacy/v1/data`
- **THEN** the upstream receives `/api/legacy/v1/data` with `strip_prefix: false`, and `/v1/data` with `strip_prefix: true`

### Requirement: Query string preserved
The gateway SHALL forward the request's query string unchanged, whether or not the prefix is stripped.

#### Scenario: Query kept after strip
- **WHEN** route `/api/products` has `strip_prefix: true` and a client requests `/api/products/123?x=1`
- **THEN** the forwarded request carries the query string `x=1`

### Requirement: Upstream base path joined
When the upstream URL has a base path, the gateway SHALL join it in front of the forwarded path without doubling the `/`.

#### Scenario: Base path with strip_prefix false
- **WHEN** route `/svc` has upstream `http://h:4000/base`, `strip_prefix: false`, and a client requests `/svc/a`
- **THEN** the upstream receives `/base/svc/a`

#### Scenario: Base path with strip_prefix true
- **WHEN** route `/svc` has upstream `http://h:4000/base`, `strip_prefix: true`, and a client requests `/svc/a`
- **THEN** the upstream receives `/base/a`

#### Scenario: Variant B lowercase method and base path
- **WHEN** variant config B is loaded and a client sends `DELETE /v2/a`
- **THEN** the gateway proxies it to the upstream path `/base/a`
