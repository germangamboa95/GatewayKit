# Spec Delta

## Purpose

Limits how many requests each route accepts per time window, per client IP or across all clients, and tells clients when they may retry.

## ADDED Requirements

### Requirement: Per-route buckets
The gateway SHALL keep separate rate-limit buckets for each route. A route that inherits `global_rate_limit` MUST NOT share buckets with any other route, so traffic on one route never uses up another route's quota.

#### Scenario: Inherited limits stay separate
- **WHEN** two routes both inherit a global limit of 100 requests per 60s per IP
- **AND** one client sends 100 requests to the first route within the window
- **THEN** the same client's next request to the second route is allowed

#### Scenario: Sample route limits
- **WHEN** the gateway runs the sample config
- **THEN** `/api/users` allows 30 requests per 60s per IP using a sliding window
- **AND** `/api/orders` allows 10 requests per 10s per IP using a fixed window
- **AND** `/api/products`, `/api/legacy` and `/api/internal` each allow 100 requests per 60s per IP using a fixed window

### Requirement: Bucket key
The gateway SHALL key buckets by client IP when `per` is `ip`, and use one bucket shared by all clients of the route when `per` is `global`. The client IP MUST be the socket's remote address; the gateway MUST ignore `X-Forwarded-For` when choosing a bucket.

#### Scenario: Per-IP buckets
- **WHEN** a route allows 2 requests per 1s per IP
- **AND** clients A and B each send 2 requests within 1s
- **THEN** all 4 requests are allowed

#### Scenario: Global bucket across clients
- **WHEN** a route allows 2 requests per 1s with `per: global`
- **AND** 3 requests arrive within 1s from 2 different IPs
- **THEN** the third request receives 429

#### Scenario: Forged X-Forwarded-For
- **WHEN** a client that has used up its per-IP bucket sends a request with a different `X-Forwarded-For` value
- **THEN** the gateway rejects the request with 429

### Requirement: Fixed window strategy
With `strategy: fixed_window`, the gateway SHALL align windows to the clock as `floor(now / window)` and reset the count when a new window starts.

#### Scenario: Count resets at the window boundary
- **WHEN** a route allows 10 requests per 10s with a fixed window
- **AND** a client sends 10 requests between 12:00:00 and 12:00:09
- **THEN** an eleventh request at 12:00:09 receives 429
- **AND** a request at 12:00:10 is allowed

### Requirement: Sliding window strategy
With `strategy: sliding_window`, the gateway SHALL allow a request only if fewer than `requests` requests were allowed in the preceding `window`. Memory per bucket MUST NOT exceed `requests` timestamps.

#### Scenario: Old requests leave the window
- **WHEN** a route allows 30 requests per 60s with a sliding window
- **AND** a client sends 30 requests at 12:00:00
- **THEN** a request at 12:00:59 receives 429
- **AND** a request at 12:01:00 or later is allowed

### Requirement: What counts toward the limit
Only requests that matched a route and passed the method check SHALL count. 404 responses, 405 responses and `GET /health` MUST NOT count. Rejected requests MUST NOT count.

#### Scenario: Unrouted and rejected requests do not count
- **WHEN** a client sends requests that get 404, 405 or hit `GET /health`
- **THEN** none of them reduce any route's quota

#### Scenario: Rejected requests do not extend the block
- **WHEN** a client keeps sending requests after receiving 429 in a fixed window
- **THEN** the client's first request in the next window is allowed

### Requirement: Rate limiting runs before auth
The gateway SHALL apply rate limiting before API-key auth, so requests that fail auth still use quota.

#### Scenario: Failed auth uses quota
- **WHEN** a route with auth allows 2 requests per window
- **AND** a client sends 2 requests with an invalid API key
- **THEN** both receive 401
- **AND** a third request in the same window receives 429, even with a valid key

### Requirement: Rejection response
When a bucket is used up, the gateway SHALL return 429 with body `{"error":"rate_limit_exceeded","message":"<text>","retry_after":<int>}` and headers `Retry-After`, `X-RateLimit-Limit`, `X-RateLimit-Remaining: 0` and `X-RateLimit-Reset`. `retry_after` and `Retry-After` MUST be the whole seconds until the bucket admits a request, at least 1.

#### Scenario: 429 body and headers
- **WHEN** a client exceeds a route limit of 10 requests per 10s
- **THEN** the gateway returns 429 with `Content-Type: application/json`
- **AND** the body has `error` set to `rate_limit_exceeded`, a `message` string and an integer `retry_after` of at least 1
- **AND** `Retry-After` equals `retry_after`
- **AND** `X-RateLimit-Limit` is `10` and `X-RateLimit-Remaining` is `0`

### Requirement: Rate-limit headers on allowed responses
For routes with a rate limit, the gateway SHALL add `X-RateLimit-Limit`, `X-RateLimit-Remaining` and `X-RateLimit-Reset` to allowed responses. `X-RateLimit-Reset` MUST be Unix seconds: the next window boundary for fixed windows, and the time the oldest counted request leaves the window for sliding windows.

#### Scenario: Headers on an allowed request
- **WHEN** a client sends its first request to a route allowing 30 requests per 60s
- **THEN** the response carries `X-RateLimit-Limit: 30` and `X-RateLimit-Remaining: 29`
- **AND** `X-RateLimit-Reset` is a Unix timestamp in seconds

#### Scenario: Fixed window reset time
- **WHEN** a request hits a fixed window of 10s at Unix time 1000003
- **THEN** `X-RateLimit-Reset` is `1000010`

### Requirement: Routes without a limit
A route with `rate_limit: false`, or with no rate limit after merging with `global_rate_limit`, SHALL never be rate limited, and its responses MUST NOT carry `X-RateLimit-*` headers.

#### Scenario: rate_limit false disables the global limit
- **WHEN** `global_rate_limit` allows 2 requests per 60s and a route sets `rate_limit: false`
- **AND** a client sends 5 requests to that route within 60s
- **THEN** all 5 requests are forwarded
- **AND** no response carries `X-RateLimit-*` headers

#### Scenario: No limit configured anywhere
- **WHEN** neither `global_rate_limit` nor the route's `rate_limit` is set
- **THEN** the route never returns 429

### Requirement: Idle bucket cleanup
The gateway SHALL delete buckets that have not been used for 2 times their `window`, so memory does not keep growing.

#### Scenario: Idle bucket removed
- **WHEN** a client's bucket on a route with a 60s window receives no request for 120s
- **THEN** the gateway deletes the bucket
- **AND** the client's next request starts a fresh bucket with full quota
