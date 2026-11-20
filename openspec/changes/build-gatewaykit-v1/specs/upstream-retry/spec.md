# Spec Delta

## Purpose

Retries failed upstream attempts on configured status codes with fixed or exponential backoff, so transient upstream failures do not reach the client.

## ADDED Requirements

### Requirement: Total attempts
The gateway SHALL make at most `retry.attempts` tries per request, counting the first. `attempts: 1` MUST mean no retries. A route without `retry` SHALL make exactly one attempt.

#### Scenario: Attempts bound the tries
- **WHEN** a route sets `retry.attempts: 3` and `on: [503]`
- **AND** the upstream returns 503 to every attempt
- **THEN** the upstream receives exactly 3 requests

#### Scenario: One attempt means no retry
- **WHEN** a route sets `retry.attempts: 1`
- **AND** the upstream returns 503
- **THEN** the upstream receives exactly 1 request

### Requirement: Retry trigger statuses
The gateway SHALL retry an attempt only if its status is in `retry.on`. A connection failure MUST count as status 502 and a timeout as 504, and the gateway retries them only if that stand-in status is in `on`.

#### Scenario: Status not in on list
- **WHEN** a route sets `on: [502, 503, 504]`
- **AND** the upstream returns 500
- **THEN** the gateway makes no retry and returns the 500

#### Scenario: Timeout retried as 504
- **WHEN** a route sets `on: [504]` and `timeout: "5s"`
- **AND** the first attempt exceeds 5s
- **THEN** the gateway retries the request

#### Scenario: Connection failure not retried
- **WHEN** a route sets `on: [500]`
- **AND** the target refuses the connection
- **THEN** the gateway makes no retry and returns 502

### Requirement: Backoff delay
Before retry n (counting from 1), the gateway SHALL wait `initial_delay` for `backoff: fixed`, or `initial_delay` times 2^(n-1) for `backoff: exponential`.

#### Scenario: Exponential backoff on the sample orders route
- **WHEN** `/api/orders` on the sample config (attempts 3, exponential, initial_delay 1s, on [502, 503, 504]) gets 503 from every attempt
- **THEN** the gateway waits 1s before the second attempt and 2s before the third
- **AND** returns the third attempt's 503

#### Scenario: Exponential backoff with four attempts
- **WHEN** a route sets attempts 4, exponential backoff and initial_delay 1s
- **AND** every attempt fails with a status in `on`
- **THEN** the gateway waits 1s, 2s and 4s before the second, third and fourth attempts

#### Scenario: Fixed backoff
- **WHEN** a route sets attempts 3, fixed backoff and initial_delay 100ms
- **AND** every attempt fails with a status in `on`
- **THEN** the gateway waits 100ms before each retry

### Requirement: Fresh target and timeout per attempt
Each attempt SHALL ask the load balancer for a target again, so a retry can reach a different target. Each attempt MUST get the full per-attempt `timeout`.

#### Scenario: Retry lands on another target
- **WHEN** a route has two targets, round-robin balance, and retry with attempts 2, fixed backoff, initial_delay 100ms and `on: [500]`
- **AND** the first target returns 500 once
- **THEN** the gateway retries on the second target after 100ms
- **AND** the client receives the second target's 200

#### Scenario: Full timeout per attempt
- **WHEN** a route sets `timeout: "5s"` and the first attempt times out
- **THEN** the retry also gets 5s

### Requirement: Last attempt response
The client SHALL receive the response from the last attempt made, whether it succeeded or failed.

#### Scenario: Retry succeeds after failures
- **WHEN** the upstream fails the first 2 requests with 503 and then returns 200
- **AND** the route allows 3 attempts on 503
- **THEN** the client receives 200

#### Scenario: All attempts fail with a connection error
- **WHEN** every attempt fails because the target refuses the connection and 502 is in `on`
- **THEN** the client receives 502 with `{"error":"bad_gateway","message":"<text>"}`

### Requirement: Request body resent
The gateway SHALL hold the request body in memory and send the same body on every attempt.

#### Scenario: POST body on retry
- **WHEN** a POST with a JSON body is retried
- **THEN** the upstream receives an identical body on each attempt

### Requirement: Retries for every allowed method
Retries SHALL apply to every method the route allows, including POST, PUT, PATCH and DELETE.

#### Scenario: POST is retried
- **WHEN** a POST to a route with retry gets a status in `on`
- **THEN** the gateway retries the POST

### Requirement: Breaker checked before each attempt
On routes with a circuit breaker, the gateway SHALL check the breaker before every attempt, including retries, and stop retrying as soon as the breaker rejects an attempt.

#### Scenario: Breaker opens mid-retry
- **WHEN** a route has a circuit breaker with threshold 2 and retry with attempts 5 on 503
- **AND** every attempt returns 503
- **THEN** the upstream receives 2 requests
- **AND** the client receives the breaker's 503 with `retry_after`
