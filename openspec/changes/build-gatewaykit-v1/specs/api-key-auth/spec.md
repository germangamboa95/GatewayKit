# Spec Delta

## Purpose

Protects routes with an API key sent in a request header, rejecting requests that lack a valid key without revealing which keys exist.

## ADDED Requirements

### Requirement: API key header lookup
For a route with `auth.type: api_key`, the gateway SHALL read the key from the configured `header`, matched case-insensitively. The header defaults to `X-API-Key`.

#### Scenario: Default header
- **WHEN** a request to `/api/internal` on the sample config sends `x-api-key: sk_live_abc123`
- **THEN** the gateway accepts the key

#### Scenario: Custom header
- **WHEN** a route sets `header: "Authorization"` and `keys: ["k1"]`
- **AND** a request sends `Authorization: k1`
- **THEN** the gateway accepts the key

### Requirement: Missing API key
If the configured header is absent, the gateway SHALL return 401 with body `{"error":"unauthorized","message":"missing API key"}` and not contact the upstream.

#### Scenario: No key sent
- **WHEN** a request to `/api/internal` has no `X-API-Key` header
- **THEN** the gateway returns 401 with `{"error":"unauthorized","message":"missing API key"}`
- **AND** the upstream receives no request

#### Scenario: Custom header missing
- **WHEN** a route uses `header: "Authorization"`
- **AND** a request has no `Authorization` header
- **THEN** the gateway returns 401

### Requirement: Invalid API key
If the header value is not one of the route's `keys`, the gateway SHALL return 401 with body `{"error":"unauthorized","message":"invalid API key"}` and not contact the upstream. The gateway MUST use 401, never 403, for both missing and invalid keys.

#### Scenario: Wrong key sent
- **WHEN** a request to `/api/internal` sends `X-API-Key: sk_live_wrong`
- **THEN** the gateway returns 401 with `{"error":"unauthorized","message":"invalid API key"}`
- **AND** the upstream receives no request

### Requirement: Constant-time key comparison
The gateway SHALL compare the presented key against each configured key in constant time, so response timing does not reveal how much of a key matched.

#### Scenario: Timing does not depend on matching prefix
- **WHEN** two requests send invalid keys of equal length, one sharing a long prefix with a valid key and one sharing none
- **THEN** the gateway compares both with a constant-time comparison and returns 401 for both

### Requirement: Key header forwarded on success
When the key is valid, the gateway SHALL forward the request to the upstream with the API key header unchanged.

#### Scenario: Upstream sees the key
- **WHEN** a request to `/api/internal` sends `X-API-Key: sk_live_def456`
- **THEN** the request reaches the upstream with `X-API-Key: sk_live_def456`

### Requirement: Auth runs after rate limiting
The gateway SHALL check the API key after the rate-limit stage and before the request transform, circuit breaker and proxy stages.

#### Scenario: Rate limit applies before auth
- **WHEN** a client has used up its bucket on a route with auth
- **AND** it sends a request with no API key
- **THEN** the gateway returns 429, not 401
