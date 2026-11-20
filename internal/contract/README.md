# internal/contract

Shared Wave 0 types. Do not edit; request changes through the orchestrator.

## Model (config spec §10)

| Type | Fields |
| --- | --- |
| `GatewayConfig` | `Port int`, `Routes []*Route` |
| `Route` | `Path`, `Methods map[string]bool` (uppercase), `MethodList []string` (config order), `StripPrefix`, `Targets []*Target`, `Balance`, `Timeout time.Duration`, `Retry *Retry`, `HealthCheck *HealthCheck`, `RateLimit *RateLimit`, `Auth *Auth`, `CircuitBreaker *CircuitBreakerConfig`, `ReqTransform *ReqTransform`, `RespTransform *RespTransform` |
| `Target` | `URL *url.URL`, `Weight int`, atomic health state |
| `Retry` | `Attempts` (total tries), `Backoff`, `InitialDelay`, `On map[int]bool` |
| `HealthCheck` | `Path`, `Interval`, `UnhealthyThreshold` |
| `RateLimit` | `Requests`, `Window`, `Strategy`, `Per`. Nil means no limit (including `rate_limit: false`) |
| `Auth` | `Type`, `Header` (lowercase), `Keys []string` |
| `CircuitBreakerConfig` | `Threshold`, `Window`, `Cooldown` |

Constants: `BalanceRoundRobin`, `BalanceWeightedRoundRobin`, `StrategyFixedWindow`, `StrategySlidingWindow`, `PerIP`, `PerGlobal`, `BackoffFixed`, `BackoffExponential`, `AuthTypeAPIKey`, `DefaultMethods`.

Target methods:

- `NewTarget(rawURL string, weight int) (*Target, error)`
- `(*Target) Healthy() bool`, `SetHealthy(ok bool)`, `Failures() int`
- `(*Target) RecordProbe(ok bool, threshold int) bool`: success resets and marks healthy; `threshold` consecutive failures mark unhealthy. Returns health after the probe.

## Compiled transforms

| Type | Fields |
| --- | --- |
| `Var` | `Kind VarKind`, `Text string` (literal value) |
| `VarKind` | `VarLiteral`, `VarRequestTime`, `VarResponseTime`, `VarRoutePath`, `VarBody` |
| `HeaderAdd` | `Name` (lowercase), `Value Var` |
| `ReqTransform` | `AddHeaders []HeaderAdd` (YAML order), `RemoveHeaders []string` (lowercase), `Mapping *Mapping` |
| `RespTransform` | `AddHeaders`, `RemoveHeaders`, `Envelope *Template` |
| `Mapping` | `Entries []MappingEntry` (YAML order) |
| `MappingEntry` | `Dest []string`, `Source []string` (dot-path segments) or `Var *Var` |
| `Template` | `Kind TemplateKind`, `Fields []TemplateField`, `Items []*Template`, `Var Var`, `Raw string` |
| `TemplateKind` | `TemplateObject`, `TemplateArray`, `TemplateVar` (string leaf), `TemplateScalar` (number/bool/null JSON text in `Raw`) |
| `TemplateField` | `Key string`, `Value *Template` |

- `Literal(s string) Var`
- `ParseVar(s string) (Var, error)`: whole-value tokens only; no scope checks
- `TimeLayout = "2006-01-02T15:04:05.000Z"`, `FormatTime(t time.Time) string`

## Request flow

| Type | Fields |
| --- | --- |
| `RequestContext` | `Req *http.Request`, `ClientIP`, `Route *Route`, `RequestTime`, `Body []byte`, `Upstream *Response`, `ResponseTime` |
| `Handler` | `func(ctx *RequestContext) *Response` |
| `Middleware` | `func(next Handler) Handler` |
| `Response` | `Status`, `Header http.Header`, `Body []byte`, `FromUpstream bool` |
| `OutRequest` | `Method`, `Path` (path plus query), `Header`, `Body []byte` |
| `Decision` | `Allowed`, `Limit`, `Remaining`, `Reset int64` (Unix s), `RetryAfter int` (s) |

## Interfaces

```go
type Balancer interface       { Pick(pool TargetPool) (*Target, bool) }
type TargetPool interface     { Targets() []*Target; Healthy(t *Target) bool; MarkProbe(t *Target, ok bool) }
type RateLimiter interface    { Allow(key string, now time.Time) Decision }
type Authenticator interface  { Check(h http.Header) error } // nil, ErrMissingAPIKey, ErrInvalidAPIKey
type CircuitBreaker interface { TryPass(now time.Time) (ok bool, retryAfter int); Record(ok bool, now time.Time); Release() }
type RetryPolicy interface    { ShouldRetry(status, attempt int) bool; Delay(attempt int) time.Duration } // attempt from 1
type Clock interface          { Now() time.Time }
type Sleeper interface        { Sleep(ctx context.Context, d time.Duration) error }
type UpstreamClient interface { Send(ctx context.Context, t *Target, r *OutRequest) (*Response, error) }
```

- `SystemClock{}`: real `Clock` and `Sleeper`
- `Deps{Clock, Sleeper, Client UpstreamClient, Logger *slog.Logger}`; `(Deps) WithDefaults() Deps` fills Clock, Sleeper, Logger

## Errors (config spec §12)

- `ErrorResponse(status int, code, msg string, extra map[string]any) *Response`: JSON, keys `error`, `message` (omitted if empty), then `extra` sorted; `Content-Type: application/json`
- `Classify(resp *Response, err error) int`: response status; 504 on timeout; `StatusClientClosed` (499) on client cancel; 502 otherwise
- Helpers: `NotFound()`, `MethodNotAllowed(allow []string)` (sets `Allow`), `RateLimited(retryAfter int)` (sets `Retry-After`; caller adds `X-RateLimit-*`), `Unauthorized(err error)`, `InvalidJSONBody()`, `BadGateway()`, `GatewayTimeout()`, `NoHealthyUpstream()`, `BreakerOpen(retryAfter int)` (sets `Retry-After`)
- Codes: `CodeInvalidJSONBody`, `CodeUnauthorized`, `CodeNotFound`, `CodeMethodNotAllowed`, `CodeRateLimitExceeded`, `CodeBadGateway`, `CodeServiceUnavailable`, `CodeNoHealthyUpstream`, `CodeGatewayTimeout`

## Constructors (pass-through stubs until each F package lands)

```go
ratelimit.New(route *contract.Route, deps contract.Deps) contract.Middleware     // internal/middleware/ratelimit
auth.New(route *contract.Route, deps contract.Deps) contract.Middleware          // internal/middleware/auth
reqtransform.New(route *contract.Route, deps contract.Deps) contract.Middleware  // internal/middleware/reqtransform
resptransform.New(route *contract.Route, deps contract.Deps) contract.Middleware // internal/middleware/resptransform
upstream.New(route *contract.Route, deps contract.Deps) contract.Handler         // internal/upstream, terminal; stub returns 502
healthcheck.Start(ctx context.Context, routes []*contract.Route, deps contract.Deps) // internal/healthcheck, no-op
```

Chain order (design D6): `ratelimit -> auth -> reqtransform -> resptransform -> terminal`.

Fixtures: `internal/fixtures` (`Sample()`, one func per sample route).
