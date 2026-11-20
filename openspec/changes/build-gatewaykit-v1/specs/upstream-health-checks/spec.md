# Spec Delta

## Purpose

Probes upstream targets in the background and takes unhealthy targets out of load-balancer rotation until they recover.

## ADDED Requirements

### Requirement: Probe request
For a route with `health_check`, the gateway SHALL probe each target with a `GET` to the target's URL joined with `health_check.path`. The probe timeout MUST be the shorter of `interval` and the route's upstream `timeout`.

#### Scenario: Probe path on the sample products route
- **WHEN** the gateway runs the sample config
- **THEN** it sends `GET /healthz` to `http://localhost:3003` and `http://localhost:3004`

#### Scenario: Probe timeout
- **WHEN** a route sets `interval: "30s"` and `timeout: "10s"`
- **AND** a target takes longer than 10s to answer a probe
- **THEN** the gateway counts that probe as a failure

### Requirement: Probe result
A 2xx probe response SHALL count as a success. Any other status, a connection error or a timeout MUST count as a failure.

#### Scenario: Non-2xx probe fails
- **WHEN** a target's health path returns 503
- **THEN** the gateway counts the probe as a failure

#### Scenario: Stopped target fails
- **WHEN** a target's container is stopped and the probe gets connection refused
- **THEN** the gateway counts the probe as a failure

### Requirement: Probe schedule
Targets SHALL start healthy. The gateway MUST run the first round of probes at startup and then one round every `interval`.

#### Scenario: First round at startup
- **WHEN** the gateway starts with a route that has `health_check`
- **THEN** each target receives a probe without waiting for the first `interval` to pass
- **AND** each target is treated as healthy until it reaches the failure threshold

### Requirement: Unhealthy and recovered transitions
After `unhealthy_threshold` consecutive probe failures, the gateway SHALL mark the target unhealthy and the load balancer MUST skip it. One successful probe SHALL mark the target healthy again.

#### Scenario: Target marked unhealthy
- **WHEN** a target on `/api/products` fails 3 probes in a row
- **THEN** the gateway stops sending client requests to that target
- **AND** sends them to the remaining healthy target

#### Scenario: Fewer failures keep the target healthy
- **WHEN** a target with `unhealthy_threshold: 3` fails 2 probes and then passes one
- **THEN** the target stays healthy and its failure count resets

#### Scenario: Target recovers
- **WHEN** an unhealthy target passes one probe
- **THEN** the load balancer includes it again

### Requirement: Routes without health checks
A route without `health_check` SHALL treat all its targets as always healthy and send no probes.

#### Scenario: No probes without config
- **WHEN** a route has no `health_check`
- **THEN** its upstream receives no probe requests
- **AND** every target stays eligible for selection

### Requirement: Probes are isolated from other features
Health-check probes MUST NOT count toward any circuit breaker or rate limit.

#### Scenario: Failed probes do not trip the breaker
- **WHEN** a route has both `health_check` and `circuit_breaker` with threshold 5
- **AND** 5 probes fail
- **THEN** the breaker stays closed

#### Scenario: Probes do not use quota
- **WHEN** probes run on a rate-limited route
- **THEN** no client bucket on that route loses quota
