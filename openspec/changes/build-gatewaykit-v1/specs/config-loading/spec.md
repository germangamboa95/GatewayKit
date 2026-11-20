# Spec Delta

## Purpose

Finds `gateway.yaml`, decodes it against the schema, rejects invalid configs with every error located in the file, and produces one normalized model with all defaults and precedence applied before the gateway serves traffic.

## ADDED Requirements

### Requirement: Resolve the config path
The gateway SHALL take the config path from the first source present, in this order: the first CLI argument, the `--config <path>` flag, the `GATEWAY_CONFIG` env var, then `./gateway.yaml`.

#### Scenario: CLI argument wins
- **WHEN** the gateway starts as `gatewaykit ./a.yaml --config ./b.yaml` with `GATEWAY_CONFIG=./c.yaml`
- **THEN** it loads `./a.yaml`

#### Scenario: Flag beats env var
- **WHEN** the gateway starts with `--config ./b.yaml`, no positional argument, and `GATEWAY_CONFIG=./c.yaml`
- **THEN** it loads `./b.yaml`

#### Scenario: Env var beats default
- **WHEN** the gateway starts with no argument or flag and `GATEWAY_CONFIG=./c.yaml`
- **THEN** it loads `./c.yaml`

#### Scenario: Default path
- **WHEN** the gateway starts with no argument, flag or env var
- **THEN** it loads `./gateway.yaml`

### Requirement: Exit when no config file exists
The gateway MUST exit with code 1 when the resolved config file does not exist, and MUST print every location it tried.

#### Scenario: Missing file
- **WHEN** the gateway starts with no argument, flag or env var and `./gateway.yaml` does not exist
- **THEN** the process exits with code 1 and prints the paths it tried

### Requirement: Require the top-level structure
The config MUST be one YAML document whose top-level value is a map. `gateway` MUST be present and MUST be a map; `gateway: {}` is valid. `routes` MUST be present as a list and MAY be empty.

#### Scenario: Empty gateway map
- **WHEN** the config is `gateway: {}` with `routes: []`
- **THEN** the config loads with port 8080 and no routes

#### Scenario: Top-level list
- **WHEN** the file's top-level value is a list
- **THEN** startup fails with an error at that location

#### Scenario: Missing gateway
- **WHEN** the config has `routes` but no `gateway` key
- **THEN** startup fails with an error that `gateway` is required

### Requirement: Decode integer fields
An `int` field SHALL accept a whole number written plain or quoted, such as `8080` or `"8080"`. Any other value MUST be a type error.

#### Scenario: Quoted integer
- **WHEN** the config sets `port: "9090"`
- **THEN** the gateway port is 9090

#### Scenario: Non-numeric integer
- **WHEN** the config sets `port: "abc"`
- **THEN** startup fails with a type error located at that value

### Requirement: Decode boolean fields strictly
A `bool` field SHALL accept only `true` or `false`. YAML 1.1 words such as `yes`, `no`, `on` and `off` MUST be rejected.

#### Scenario: Literal false
- **WHEN** a route sets `strip_prefix: false`
- **THEN** the route does not strip its prefix

#### Scenario: YAML 1.1 word
- **WHEN** a route sets `strip_prefix: yes`
- **THEN** startup fails with a type error located at that value

### Requirement: Treat null as absent
A field set to `null`, `~` or an empty value SHALL be treated as absent, so its default applies or, if it is required, a missing-field error is reported.

#### Scenario: Null optional field
- **WHEN** a route sets `strip_prefix: ~`
- **THEN** the route uses the default `false`

#### Scenario: Null required field
- **WHEN** a route sets `path:` with no value
- **THEN** startup fails with an error that `path` is required

### Requirement: Normalize enums and methods
Enum values SHALL be matched case-insensitively and stored in lowercase. HTTP methods SHALL be matched case-insensitively and stored in uppercase. A value outside the allowed set MUST be an error, including an unknown `auth.type`, `strategy`, `per`, `balance`, `backoff` or method.

#### Scenario: Uppercase enum
- **WHEN** a rate limit sets `strategy: Sliding_Window`
- **THEN** the stored strategy is `sliding_window`

#### Scenario: Lowercase methods
- **WHEN** a route sets `methods: ["get", "delete"]`
- **THEN** the route allows `GET` and `DELETE`

#### Scenario: Unknown balance
- **WHEN** an upstream sets `balance: random`
- **THEN** startup fails with an error located at that value

#### Scenario: Unknown method
- **WHEN** a route sets `methods: ["FETCH"]`
- **THEN** startup fails with an error located at that value

### Requirement: Parse durations
A `duration` field SHALL accept a number plus a unit `ms`, `s`, `m` or `h` matching `^\d+(\.\d+)?(ms|s|m|h)$`, or a bare integer meaning seconds. A duration MUST be greater than zero unless its field allows zero.

#### Scenario: Fractional minutes
- **WHEN** a field is `"1.5m"`
- **THEN** the stored duration is 90 seconds

#### Scenario: Milliseconds
- **WHEN** `global_timeout` is `"750ms"`
- **THEN** the stored timeout is 750 milliseconds

#### Scenario: Bare integer
- **WHEN** a field is `30`
- **THEN** the stored duration is 30 seconds

#### Scenario: Space before unit
- **WHEN** `routes[2].upstream.timeout` is `"5 s"`
- **THEN** startup fails with `gateway.yaml:<line>:<col>: routes[2].upstream.timeout: invalid duration "5 s"`

#### Scenario: Zero where not allowed
- **WHEN** `global_timeout` is `"0s"`
- **THEN** startup fails with an error located at that value

### Requirement: Validate URLs
A `url` field MUST be an absolute `http://` or `https://` URL with a host. It MAY include a base path and MUST NOT include a query string.

#### Scenario: Base path
- **WHEN** an upstream sets `url: "http://localhost:4001/base"`
- **THEN** the config loads and the target keeps the base path `/base`

#### Scenario: Wrong scheme
- **WHEN** an upstream sets `url: "ftp://localhost:3001"`
- **THEN** startup fails with an error located at that value

#### Scenario: Query string
- **WHEN** an upstream sets `url: "http://localhost:3001?x=1"`
- **THEN** startup fails with an error located at that value

### Requirement: Validate route paths
A route `path` MUST start with `/` and MUST NOT contain `*`, a `:param` segment, a query string or a fragment. The loader SHALL remove a trailing `/` except on `/` itself.

#### Scenario: Trailing slash removed
- **WHEN** a route sets `path: "/v2/"`
- **THEN** the stored route path is `/v2`

#### Scenario: Root path kept
- **WHEN** a route sets `path: "/"`
- **THEN** the stored route path is `/`

#### Scenario: Wildcard
- **WHEN** a route sets `path: "/api/*"`
- **THEN** startup fails with an error located at that value

#### Scenario: Missing leading slash
- **WHEN** a route sets `path: "api/users"`
- **THEN** startup fails with an error located at that value

### Requirement: Reject duplicate route paths
Two routes MUST NOT share a `path` after trailing-slash removal.

#### Scenario: Duplicate after normalization
- **WHEN** one route sets `path: "/api"` and another sets `path: "/api/"`
- **THEN** startup fails with an error naming the second route

### Requirement: Validate route methods
A route's `methods` MUST be a non-empty list when present. The loader SHALL remove duplicates. When `methods` is absent the route SHALL allow all seven methods: GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS.

#### Scenario: Scalar instead of list
- **WHEN** a route sets `methods: GET`
- **THEN** startup fails with a type error located at that value

#### Scenario: Empty list
- **WHEN** a route sets `methods: []`
- **THEN** startup fails with an error located at that value

#### Scenario: Duplicates removed
- **WHEN** a route sets `methods: ["GET", "get"]`
- **THEN** the route allows only `GET`

#### Scenario: Default methods
- **WHEN** a route omits `methods`
- **THEN** the route allows all seven methods

### Requirement: Validate the gateway port
`gateway.port` MUST be an integer from 1 to 65535 and SHALL default to 8080.

#### Scenario: Out of range
- **WHEN** the config sets `port: 70000`
- **THEN** startup fails with an error located at that value

#### Scenario: Default port
- **WHEN** `gateway.port` is absent
- **THEN** the gateway port is 8080

### Requirement: Validate upstream targets
An `upstream` MUST have exactly one of `url` or `targets`. `targets` MUST hold at least one entry, each with a `url`, and MUST NOT repeat a URL. `targets[].weight` MUST be 1 or more and SHALL default to 1.

#### Scenario: Neither url nor targets
- **WHEN** `routes[1].upstream` has neither `url` nor `targets`
- **THEN** startup fails with `routes[1].upstream: exactly one of "url" or "targets" is required`

#### Scenario: Both url and targets
- **WHEN** an upstream sets both `url` and `targets`
- **THEN** startup fails with the same error

#### Scenario: Zero weight
- **WHEN** `routes[2].upstream.targets[0].weight` is `0`
- **THEN** startup fails with `routes[2].upstream.targets[0].weight: must be an integer >= 1 (got 0)`

#### Scenario: Duplicate target URL
- **WHEN** two targets of one upstream share a URL
- **THEN** startup fails with an error naming the second target

### Requirement: Validate retry settings
`retry.attempts` MUST be present and be 1 or more. `backoff` MUST be `fixed` or `exponential`. `initial_delay` MUST be a duration of zero or more. `on` MUST be a non-empty list of statuses from 100 to 599.

#### Scenario: Missing attempts
- **WHEN** a retry block omits `attempts`
- **THEN** startup fails with an error that `attempts` is required

#### Scenario: Zero initial delay
- **WHEN** a retry block sets `initial_delay: "0s"`
- **THEN** the config loads

#### Scenario: Empty on list
- **WHEN** a retry block sets `on: []`
- **THEN** startup fails with an error located at that value

#### Scenario: Status out of range
- **WHEN** a retry block sets `on: [600]`
- **THEN** startup fails with an error located at that value

### Requirement: Validate health check settings
`health_check.path` MUST be present and start with `/`. `interval` MUST be a positive duration. `unhealthy_threshold` MUST be 1 or more.

#### Scenario: Missing path
- **WHEN** a health check omits `path`
- **THEN** startup fails with an error that `path` is required

#### Scenario: Zero threshold
- **WHEN** a health check sets `unhealthy_threshold: 0`
- **THEN** startup fails with an error located at that value

### Requirement: Validate circuit breaker settings
`circuit_breaker.threshold` MUST be present and be 1 or more. `window` and `cooldown` MUST be positive durations.

#### Scenario: Missing threshold
- **WHEN** a circuit breaker omits `threshold`
- **THEN** startup fails with an error that `threshold` is required

### Requirement: Validate rate limit fields
In `global_rate_limit` and a route's `rate_limit`, `requests` MUST be 1 or more, `window` MUST be a positive duration, `strategy` MUST be `fixed_window` or `sliding_window`, and `per` MUST be `ip` or `global`.

#### Scenario: Zero requests
- **WHEN** a rate limit sets `requests: 0`
- **THEN** startup fails with an error located at that value

#### Scenario: Unknown per
- **WHEN** a rate limit sets `per: user`
- **THEN** startup fails with an error located at that value

### Requirement: Merge route rate limits over the global limit
The loader SHALL merge a route's `rate_limit` field by field over `gateway.global_rate_limit`. After the merge, `requests` and `window` MUST both have values. `strategy` SHALL default to `fixed_window` and `per` to `ip`. A route with neither SHALL have no limit.

#### Scenario: Partial override
- **WHEN** `global_rate_limit` is `{requests: 100, window: "60s", per: global}` and a route sets `rate_limit: {requests: 30}`
- **THEN** the route limit is 30 requests per 60 seconds, per global, fixed window

#### Scenario: Incomplete after merge
- **WHEN** there is no `global_rate_limit` and a route sets `rate_limit: {requests: 30}`
- **THEN** startup fails with an error that the route's rate limit has no `window`

#### Scenario: Inherited global limit
- **WHEN** `global_rate_limit` is set and a route has no `rate_limit`
- **THEN** the route gets the global settings

#### Scenario: No limit anywhere
- **WHEN** neither `global_rate_limit` nor the route's `rate_limit` is set
- **THEN** the route has no rate limit

### Requirement: Disable the inherited limit with false
A route's `rate_limit` SHALL accept the literal `false`, which gives the route no limit even when `global_rate_limit` is set. `rate_limit: true` MUST be a type error. `global_rate_limit: false` MUST be a type error.

#### Scenario: Route opts out
- **WHEN** `global_rate_limit` is set and a route sets `rate_limit: false`
- **THEN** that route has no rate limit and other routes keep the global limit

#### Scenario: True is rejected
- **WHEN** a route sets `rate_limit: true`
- **THEN** startup fails with a type error located at that value

#### Scenario: Global false is rejected
- **WHEN** the config sets `global_rate_limit: false`
- **THEN** startup fails with a type error located at that value

### Requirement: Validate auth settings
`auth.type` MUST be present and MUST be `api_key`. For `api_key`, `keys` MUST be a list holding at least one key, and every key MUST be non-empty. `header` SHALL default to `X-API-Key`.

#### Scenario: Unknown auth type
- **WHEN** a route sets `auth: {type: bearer, keys: ["k"]}`
- **THEN** startup fails with an error located at that value

#### Scenario: Empty key
- **WHEN** a route sets `auth: {type: api_key, keys: [""]}`
- **THEN** startup fails with an error located at that value

#### Scenario: Default header
- **WHEN** a route sets `auth: {type: api_key, keys: ["k1"]}`
- **THEN** the route reads its key from `X-API-Key`

### Requirement: Validate transform variables
The loader MUST reject a transform value that starts with `$` but is not a known token: `$request_time`, `$response_time`, `$route_path`, `$body` or `$literal:<text>`. It MUST reject `$response_time` in a request transform and `$body` anywhere except `response_transform.body.envelope`.

#### Scenario: Misspelled variable
- **WHEN** `routes[3].response_transform.headers.add.X-Time` is `"$requst_time"`
- **THEN** startup fails with `routes[3].response_transform.headers.add.X-Time: "$requst_time" is not a known variable`

#### Scenario: Response variable in a request transform
- **WHEN** a request transform header value is `"$response_time"`
- **THEN** startup fails with an error located at that value

#### Scenario: Body outside the envelope
- **WHEN** a response header value is `"$body"`
- **THEN** startup fails with an error located at that value

#### Scenario: Token inside a longer string
- **WHEN** a header value is `"v=$request_time"`
- **THEN** the config loads and the value is the literal string `v=$request_time`

### Requirement: Reject conflicting mapping destinations
The loader MUST reject a request body mapping where one destination path is a prefix of another, such as `a` and `a.b`.

#### Scenario: Prefix conflict
- **WHEN** a mapping sets `a: "x"` and `a.b: "y"`
- **THEN** startup fails with an error located at the conflicting key

### Requirement: Accept lenient placement of routes
The loader SHALL accept `routes` at the top level or under `gateway.routes`. If both are present, startup MUST fail.

#### Scenario: Routes under gateway
- **WHEN** the config nests `routes` under `gateway`
- **THEN** the routes load as if they were at the top level

#### Scenario: Routes in both places
- **WHEN** the config has both top-level `routes` and `gateway.routes`
- **THEN** startup fails with an error naming both locations

### Requirement: Accept lenient placement of upstream settings
The loader SHALL accept `timeout`, `retry` and `health_check` under `upstream` or directly on the route, and `circuit_breaker` on the route or under `upstream`. If the same key appears in both places on one route, startup MUST fail.

#### Scenario: Route-level timeout
- **WHEN** a route sets `timeout: "2s"` on the route and none under `upstream`
- **THEN** the route's per-attempt timeout is 2 seconds

#### Scenario: Breaker under upstream
- **WHEN** a route sets `circuit_breaker` under `upstream`
- **THEN** the route gets that circuit breaker

#### Scenario: Retry in both places
- **WHEN** a route sets `retry` on the route and under `upstream`
- **THEN** startup fails with an error naming both locations

### Requirement: Warn on unknown keys
The loader SHALL log a warning with the file location for every key the schema does not define, and MUST continue startup.

#### Scenario: Unknown route key
- **WHEN** a route contains `cache_ttl: "10s"`
- **THEN** the gateway logs a warning with that key's `file:line:col` and starts

### Requirement: Log non-fatal warnings
The loader SHALL log a warning and continue when a route has path `/health`, when `balance` or `weight` is set on an upstream with one target, or when a transform adds or removes a gateway-managed header.

#### Scenario: Health route
- **WHEN** a route sets `path: "/health"`
- **THEN** the gateway logs a warning that the route never receives `GET /health` and starts

#### Scenario: Balance on one target
- **WHEN** an upstream with only `url` sets `balance: weighted_round_robin`
- **THEN** the gateway logs a warning and starts

#### Scenario: Managed header in a transform
- **WHEN** a transform adds `Host`
- **THEN** the gateway logs a warning, ignores that entry and starts

### Requirement: Report every error and refuse to start
The loader MUST collect every error instead of stopping at the first. It MUST print a summary line with the count and file name, then each error prefixed with `file:line:col` and the field path, and MUST exit with code 1. It MUST NOT serve any part of an invalid config.

#### Scenario: Three errors
- **WHEN** a config has three invalid fields
- **THEN** the gateway prints `config error: 3 problems in gateway.yaml`, then three lines each starting with `gateway.yaml:<line>:<col>:` and a field path, and exits with code 1

#### Scenario: Parse error
- **WHEN** the YAML cannot be parsed
- **THEN** the gateway prints the error with its `file:line:col` and exits with code 1

#### Scenario: No partial serving
- **WHEN** one route is invalid and the others are valid
- **THEN** the gateway does not listen on any port

### Requirement: Resolve the per-attempt timeout
The loader SHALL set each route's per-attempt timeout from `upstream.timeout`, then `route.timeout`, then `gateway.global_timeout`, then 30 seconds.

#### Scenario: Upstream timeout
- **WHEN** a route sets `upstream.timeout: "5s"` and `global_timeout` is `"30s"`
- **THEN** the route's timeout is 5 seconds

#### Scenario: Global fallback
- **WHEN** a route sets no timeout and `global_timeout` is `"750ms"`
- **THEN** the route's timeout is 750 milliseconds

#### Scenario: Built-in default
- **WHEN** neither the route nor `gateway` sets a timeout
- **THEN** the route's timeout is 30 seconds

### Requirement: Normalize upstreams to targets
The loader SHALL convert `upstream.url` to a single target with weight 1, so every route holds a list of targets. `balance` SHALL default to `round_robin`.

#### Scenario: Single url
- **WHEN** an upstream sets `url: "http://localhost:3001"`
- **THEN** the route has one target `http://localhost:3001` with weight 1 and balance `round_robin`

### Requirement: Fill feature defaults
The loader SHALL fill these defaults: retry `backoff: fixed`, `initial_delay: 1s`, `on: [502, 503, 504]`; health check `interval: 30s`, `unhealthy_threshold: 3`; circuit breaker `window: 60s`, `cooldown: 30s`; `strip_prefix: false`.

#### Scenario: Minimal retry
- **WHEN** a route sets `retry: {attempts: 2}`
- **THEN** the route retries with fixed backoff, a 1 second delay, on 502, 503 and 504

#### Scenario: Minimal health check
- **WHEN** a route sets `health_check: {path: "/healthz"}`
- **THEN** the route probes every 30 seconds with an unhealthy threshold of 3

#### Scenario: Minimal circuit breaker
- **WHEN** a route sets `circuit_breaker: {threshold: 5}`
- **THEN** the breaker uses a 60 second window and a 30 second cooldown

### Requirement: Compile the model at load time
The loader SHALL produce the normalized model once at startup: durations as exact values, header names in lowercase, and transform variables parsed into tokens. Request handling MUST read only this model.

#### Scenario: Header names lowercased
- **WHEN** a transform removes `X-Debug`
- **THEN** the model stores the header name `x-debug`

#### Scenario: Variable typo fails at startup
- **WHEN** an envelope leaf is `"$respnse_time"`
- **THEN** startup fails before the gateway listens, not on the first request

### Requirement: Load the reference configs
The loader SHALL load the re-indented sample config `configs/gateway.yaml` with no errors and no warnings, and SHALL load the two variation configs from config spec §13.

#### Scenario: Sample config
- **WHEN** the gateway starts with `configs/gateway.yaml`
- **THEN** it loads five routes and logs no warnings

#### Scenario: Smallest config
- **WHEN** the gateway starts with `gateway: {}` and one route with `path: "/"` and an upstream `url`
- **THEN** it loads with port 8080, all seven methods, a 30 second timeout and no rate limit

#### Scenario: Lenient variant
- **WHEN** the gateway starts with §13 config B, which nests `routes` under `gateway`, uses a route-level `timeout` and lowercase methods
- **THEN** it loads with port 9090, routes `/v2` and `/v2/admin`, and a 2 second timeout on `/v2`
