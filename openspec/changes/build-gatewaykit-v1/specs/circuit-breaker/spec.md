# Spec Delta

## Purpose

Stops sending traffic to a failing upstream after repeated failures, rejects requests fast while open, and probes recovery with a single trial request.

## ADDED Requirements

### Requirement: One breaker per route
The gateway SHALL keep one circuit breaker for each route that sets `circuit_breaker`. Routes without it MUST never return the breaker's 503.

#### Scenario: Breakers are independent
- **WHEN** the breaker on one route opens
- **THEN** requests to other routes still reach their upstreams

### Requirement: Failure definition
The breaker SHALL count an attempt as a failure when its upstream status is 500 or above, including the 502 stand-in for connection failures and the 504 stand-in for timeouts. 4xx responses MUST NOT count as failures. Every attempt counts, including retries.

#### Scenario: 4xx is not a failure
- **WHEN** a route's upstream returns 404 to 10 requests and the breaker threshold is 5
- **THEN** the breaker stays closed

#### Scenario: Timeouts and connection errors count
- **WHEN** an attempt times out or the target refuses the connection
- **THEN** the breaker counts one failure

#### Scenario: Retries count
- **WHEN** one client request makes 3 attempts that each return 503
- **THEN** the breaker counts 3 failures

### Requirement: Closed to open
While closed, the breaker SHALL open when the failures within the last `window` reach `threshold`. Failures older than `window` MUST drop out of the count.

#### Scenario: Threshold reached on the sample internal route
- **WHEN** `/api/internal` on the sample config (threshold 5, window 60s) gets 5 upstream 503s within 60s
- **THEN** the breaker opens
- **AND** the next request receives 503 without reaching the upstream

#### Scenario: Old failures expire
- **WHEN** a breaker with threshold 5 and window 60s records 4 failures, then a fifth failure more than 60s after the first
- **THEN** the breaker stays closed

### Requirement: Open rejection response
While open, the breaker SHALL reject every request with 503, body exactly `{"error":"service_unavailable","retry_after":<int>}` and a `Retry-After` header with the same value. `retry_after` MUST be the cooldown seconds left, rounded up, and at least 1. The body MUST contain no other keys.

#### Scenario: Rejection while open
- **WHEN** a breaker with cooldown 30s opened 10.5s ago
- **THEN** a request receives 503 with `{"error":"service_unavailable","retry_after":20}`
- **AND** `Retry-After: 20`
- **AND** the upstream receives no request

#### Scenario: Minimum retry_after
- **WHEN** 0.2s of cooldown remain
- **THEN** `retry_after` is 1

### Requirement: Open to half-open
When `cooldown` has passed since the breaker opened, the breaker SHALL move to half-open.

#### Scenario: Cooldown elapsed
- **WHEN** a breaker with cooldown 30s opened 30s ago
- **THEN** the next request is sent to the upstream as a trial

### Requirement: Half-open trial
In half-open, the breaker SHALL send only the first request to the upstream as a trial. Other requests that arrive before the trial finishes MUST receive 503 with `{"error":"service_unavailable","retry_after":1}` and `Retry-After: 1`.

#### Scenario: Concurrent requests during the trial
- **WHEN** the breaker is half-open and a trial request is in flight
- **AND** another request arrives
- **THEN** the second request receives 503 with `retry_after` 1

### Requirement: Trial outcome
If the trial succeeds, the breaker SHALL close and reset its failure count. If the trial fails, the breaker SHALL open again and restart the cooldown.

#### Scenario: Trial succeeds
- **WHEN** the half-open trial gets a status below 500
- **THEN** the breaker closes with a failure count of 0
- **AND** later requests reach the upstream

#### Scenario: Trial fails
- **WHEN** the half-open trial gets 503
- **THEN** the breaker opens again
- **AND** requests receive 503 with `retry_after` equal to the full cooldown in seconds

### Requirement: Trial without an upstream attempt
If the half-open trial request ends before reaching the upstream, because no target is healthy or the client disconnects, the breaker SHALL stay half-open and admit the next request as the trial.

#### Scenario: No healthy target during the trial
- **WHEN** the breaker is half-open and the trial request finds every target unhealthy
- **THEN** that request receives 503 `no_healthy_upstream`
- **AND** the next request is sent to the upstream as the trial once a target is healthy

### Requirement: Breaker check before each attempt
The gateway SHALL check the breaker before every upstream attempt, after auth and the request transform and before target selection, so retries stop as soon as the breaker opens.

#### Scenario: Rejected before target selection
- **WHEN** the breaker is open on a route whose targets are all unhealthy
- **THEN** the gateway returns the breaker's 503 `service_unavailable`, not `no_healthy_upstream`
