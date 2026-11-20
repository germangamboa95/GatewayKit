# GatewayKit Config Schema Spec (gateway.yaml)

Nov 19, 2026 · @German

## 1. Purpose and conventions

This file is the only input that decides how GatewayKit behaves. Any config that follows this spec must work, not just the sample, so the loader follows these rules and never hard-codes the sample's values.

**Loading.** The gateway looks for the config path in this order:

1. The first CLI argument (`gatewaykit ./gateway.yaml`)
2. The `--config <path>` flag
3. The `GATEWAY_CONFIG` env var
4. `./gateway.yaml`

If no file is found, the process exits with code 1 and liasts the locations it tried.

**Format.** One YAML document. Keys are snake\_case and case-sensitive. Enum values are case-insensitive and stored in lowercase. HTTP methods are stored in uppercase.

| Type | Meaning | Example |
| --- | --- | --- |
| `int` | Whole number; each field gives its allowed range | `100` |
| `bool` | `true` or `false` | `false` |
| `duration` | A number plus a unit: `ms`, `s`, `m` or `h`. A bare integer means seconds | `"500ms"`, `"30s"`, `"1.5m"` |
| `url` | Absolute `http://` or `https://` URL with a host. May include a base path; no query string | `http://localhost:3001` |
| `path` | String starting with `/` | `/api/users` |
| `status` | Integer from 100 to 599 | `503` |
| `method` | One of GET, POST, PUT, PATCH, DELETE, HEAD, OPTIONS | `GET` |

Duration pattern: `^\d+(\.\d+)?(ms|s|m|h)$`. A duration must be greater than zero unless its field says otherwise.

**Unknown keys** log a warning but don't fail startup, so a config with newer fields still loads. **Known keys with invalid values** stop startup (see §11).

**The Req column** in the tables below means: **yes** = must be present; **cond** = required only in certain cases, explained in the row; **no** = optional, and the default shown is used if it's left out.

### Supported YAML

GatewayKit parses YAML with its own parser, because Go's standard library has none and the build allows no outside modules. The parser handles the features below, which cover the sample config and any config written in the same style. Anything else stops startup with an error that gives the line number; it is never quietly misread.

| Supported | Notes |
| --- | --- |
| Block mappings `key: value` | Indent with spaces; any width works as long as it's consistent. A tab in the indentation is an error |
| Block sequences `- item` | Items can be scalars or mappings (`- path: "/x"` starts a mapping item). The `-` can sit at its parent key's indentation or deeper |
| Flow sequences `[a, "b", 3]` and flow mappings `{ k: v }` | Can be nested, but must open and close on the same line |
| Scalars | Plain; double-quoted with the escapes `\"`, `\\`, `\n`, `\t` and `\uXXXX`; single-quoted, where `''` means one `'` |
| Comments | `#` at the start of a line or after whitespace, outside quotes |
| Empty values | `key:` with nothing on the line or nested under it means null |
| Keys | Any plain or quoted scalar, including dots and dashes (`user.id`, `X-Gateway`). A key repeated within one mapping is an error |
| Document markers | A leading `---` and a trailing `...` are ignored |

**Not supported** (startup fails, naming the line): anchors and aliases (`&`, `*`, `<<`), tags (`!!str`), block scalars (`|`, `>`), more than one document in a file, complex keys (`?`), and plain or flow values that span several lines.

**Typing.** The parser returns only strings, maps and lists, and records whether each string was quoted. Types come from the schema when the config is decoded:

- An `int` field accepts `8080` or `"8080"`.
- A `bool` field accepts only `true` or `false`. YAML 1.1 words like `yes`, `no`, `on` and `off` are rejected.
- `null`, `~` or an empty value means the field is absent.

This avoids classic YAML surprises such as `no` turning into false. Every error in §11 is prefixed with `file:line:col`, for example `gateway.yaml:23:5: routes[2].upstream.timeout: invalid duration "5 s"`.

## 2. Top-level structure

The schema has two top-level keys: `gateway` for global settings and `routes` for an ordered list of routes. On a route, only `path` and `upstream` are required; every other feature is optional.

```yaml
gateway:                          # required map
  port: 8080                      # int 1-65535, default 8080
  global_timeout: "30s"           # duration, default "30s"
  global_rate_limit:              # RateLimit, optional (omitted = no default limit)
    requests: 100
    window: "60s"
    strategy: fixed_window        # fixed_window | sliding_window
    per: ip                       # ip | global

routes:                           # list of Route, required (may be empty)
  - path: "/api/example"          # required, unique
    methods: ["GET", "POST"]      # optional, default: all methods
    strip_prefix: false           # optional, default false

    upstream:                     # required
      url: "http://localhost:3001" # exactly one of url / targets
      # targets:
      #   - url: "http://localhost:3003"
      #     weight: 3             # default 1
      balance: round_robin        # round_robin | weighted_round_robin
      timeout: "5s"               # default: gateway.global_timeout
      retry:                      # Retry, optional
        attempts: 3
        backoff: exponential      # fixed | exponential
        initial_delay: "1s"
        on: [502, 503, 504]
      health_check:               # HealthCheck, optional
        path: "/healthz"
        interval: "30s"
        unhealthy_threshold: 3

    rate_limit:                   # RateLimit, optional; merged over global_rate_limit
      requests: 30
      window: "60s"
    auth:                         # Auth, optional
      type: api_key
      header: "X-API-Key"
      keys: ["sk_live_abc123"]
    circuit_breaker:              # CircuitBreaker, optional
      threshold: 5
      window: "60s"
      cooldown: "30s"
    request_transform:            # optional
      headers: { add: {}, remove: [] }
      body: { mapping: {} }
    response_transform:           # optional
      headers: { add: {}, remove: [] }
      body: { envelope: {} }
```

**Placement leniency.** The sample config lost its indentation, so two placements are ambiguous. The loader accepts either form of each:

- `routes` can sit at the top level (canonical) or under `gateway.routes`. If both are present, startup fails.
- `timeout`, `retry` and `health_check` can sit under `upstream` (canonical) or directly on the route. `circuit_breaker` can sit on the route (canonical) or under `upstream`. If the same key appears in both places, startup fails.

## 3. `gateway`

`gateway` holds the listen port and the defaults every route inherits. It is required, but each of its fields is optional, so `gateway: {}` is valid.

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `port` | int | no | `8080` | 1–65535 |
| `global_timeout` | duration | no | `"30s"` | Applies to every upstream call that doesn't set its own `timeout` |
| `global_rate_limit` | RateLimit (§6) | no | none (no limit) | The default for every route that doesn't override it |

**`GET /health` is reserved.** It always returns `200 {"status":"healthy","uptime_seconds":<int>}`. It is checked before routing, rate limiting and auth. A route whose path is `/health` loads, but the gateway logs a warning that the route will never receive `/health` itself; subpaths like `/health/x` still reach the route. `uptime_seconds` is whole seconds since the process started, rounded down.

## 4. `routes[]`

Each route matches a literal path prefix, filters by method, and sends the request to one upstream. If several routes match, the longest prefix wins. The order of routes in the file has no effect.

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `path` | path | yes | — | Starts with `/`. A trailing `/` is removed (except on `/` itself). No `*`, `:param`, query string or fragment. Must be unique after the trailing slash is removed |
| `methods` | list of method | no | all methods | Non-empty if present. Case-insensitive. Duplicates are removed |
| `strip_prefix` | bool | no | `false` | See the table below |
| `upstream` | Upstream (§5) | yes | — |  |
| `rate_limit` | RateLimit (§6) | no | inherits global |  |
| `auth` | Auth (§8) | no | none |  |
| `circuit_breaker` | CircuitBreaker (§7) | no | none |  |
| `request_transform` | RequestTransform (§9) | no | none |  |
| `response_transform` | ResponseTransform (§9) | no | none |  |

**Matching algorithm** (the request path excludes the query string; matching is case-sensitive):

1. `GET /health` is answered first, before any route is checked.
2. A route matches if the request path equals `route.path`, or starts with `route.path + "/"`. So `/api/users` matches `/api/users`, `/api/users/` and `/api/users/42`, but not `/api/usersX`. A route with path `/` matches every request.
3. Of the matching routes, the one with the longest `path` wins.
4. If no route matches, return **404**.
5. If the winning route doesn't list the request's method, return **405** with an `Allow` header listing the route's methods. The gateway doesn't fall back to a shorter route that would accept the method.
6. `HEAD` is not implied by `GET`. Only the methods listed are allowed.

**Path rewriting.** If `strip_prefix` is true, `route.path` is removed from the start of the request path, and an empty result becomes `/`. The query string is always kept. If the upstream URL has a base path, it's joined in front of the forwarded path without doubling the `/`.

| Route path | Upstream URL | Request | `strip_prefix: false` | `strip_prefix: true` |
| --- | --- | --- | --- | --- |
| `/api/products` | `http://h:3003` | `/api/products/123?x=1` | `/api/products/123?x=1` | `/123?x=1` |
| `/api/products` | `http://h:3003` | `/api/products` | `/api/products` | `/` |
| `/api/legacy` | `http://h:3005` | `/api/legacy/v1/data` | `/api/legacy/v1/data` | `/v1/data` |
| `/svc` | `http://h:4000/base` | `/svc/a` | `/base/svc/a` | `/base/a` |

## 5. `upstream`

An upstream is one `url` or a list of weighted `targets`. Both forms are converted to a list of targets at load time, so the proxy only ever deals with targets.

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `url` | url | cond | — | Exactly one of `url` or `targets` must be present |
| `targets` | list of Target | cond | — | At least one entry. No duplicate URLs |
| `targets[].url` | url | yes | — |  |
| `targets[].weight` | int | no | `1` | 1 or more |
| `balance` | enum | no | `round_robin` | `round_robin` or `weighted_round_robin`. Logs a warning, and has no effect, if there's only one target |
| `timeout` | duration | no | `gateway.global_timeout` | Limit for each attempt, covering connect through the full response body |
| `retry` | Retry (§7) | no | none |  |
| `health_check` | HealthCheck (§7) | no | none | Without it, targets are always treated as healthy |

**How the load is spread.**

- `round_robin` cycles through the healthy targets in the order they're listed, ignoring weights.
- `weighted_round_robin` uses smooth weighted round-robin, the same method as nginx. Weights 3:1 give the order A, A, B, A, repeating. Every 4 consecutive picks are exactly 3:1, and B is never kept waiting for long.
- Unhealthy targets are skipped. The rotation state is kept per route, in memory.
- If every target is unhealthy, return **503** `{"error":"no_healthy_upstream"}`.

**Forwarding.**

- The method, body and all end-to-end headers are forwarded.
- Hop-by-hop headers are removed: `Connection`, `Keep-Alive`, `Proxy-*`, `TE`, `Trailer`, `Transfer-Encoding`, `Upgrade`.
- `Host` is set to the target's host.
- The client IP is added to `X-Forwarded-For`, and `X-Forwarded-Proto` and `X-Forwarded-Host` are set.
- If the target can't be reached, return **502**. If the timeout runs out, return **504**.

## 6. RateLimit (`global_rate_limit`, `rate_limit`)

A RateLimit allows `requests` requests per `window` for each bucket. Each route has its own buckets, even when every route inherits the same global settings, so traffic on `/api/users` never uses up `/api/orders`'s quota.

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `requests` | int | yes\* | — | 1 or more |
| `window` | duration | yes\* | — | Greater than 0 |
| `strategy` | enum | no | `fixed_window` | `fixed_window` or `sliding_window` |
| `per` | enum | no | `ip` | `ip`: one bucket per client IP. `global`: one bucket shared by all clients of the route |

\* A route's `rate_limit` is merged field by field over `global_rate_limit`, so a route can set only `requests` and inherit the other fields. After the merge, `requests` and `window` must both have values.

**Strategies.**

- `fixed_window`: windows are aligned to the clock (`floor(now / window)`), and the count resets when a new window starts.
- `sliding_window`: an exact sliding log. A request is allowed if fewer than `requests` were allowed in the last `window`. Memory per bucket never exceeds `requests` timestamps.

**What counts.**

- Only requests that matched a route and passed the method check count. 404s, 405s and `/health` never count.
- Rate limiting runs before auth, so failed auth attempts use up quota. This makes brute-forcing API keys harder.
- Rejected requests don't count.
- The client IP is the socket's remote address. `X-Forwarded-For` is ignored because clients can fake it.

**When a request is rejected:** return **429** with body `{"error":"rate_limit_exceeded","retry_after":<int seconds>}` and these headers: `Retry-After`, `X-RateLimit-Limit`, `X-RateLimit-Remaining: 0`, `X-RateLimit-Reset` (Unix seconds). Allowed responses also carry the `X-RateLimit-*` headers.

Buckets that haven't been used for 2× `window` are deleted, so memory doesn't keep growing.

## 7. Resilience: `retry`, `health_check`, `circuit_breaker`

All three use the same definition of a failed upstream call. Connection refused or reset is treated as status **502**, and a timeout as **504**. Each feature then judges the call by that status.

### Retry

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `attempts` | int | yes | — | 1 or more. This is the **total** number of tries, including the first. `1` means no retries |
| `backoff` | enum | no | `fixed` | `fixed` or `exponential` |
| `initial_delay` | duration | no | `"1s"` | `"0s"` is allowed here |
| `on` | list of status | no | `[502, 503, 504]` | Statuses that trigger a retry. Must not be empty |

- The wait before retry *n* (counting from 1) is `initial_delay` for `fixed`, or `initial_delay × 2^(n-1)` for `exponential`. With 1s: 1s, 2s, 4s.
- A connection failure or timeout is retried only if its stand-in status (502 or 504) is in `on`.
- Each attempt asks the load balancer for a target again, so a retry can go to a different target. Each attempt gets the full `timeout`.
- The client receives the response from the last attempt.
- The request body is held in memory so it can be resent.
- Retries apply to every method the route allows, including POST. That's the config author's decision.

### HealthCheck

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `path` | path | yes | — | Added to each target's URL |
| `interval` | duration | no | `"30s"` |  |
| `unhealthy_threshold` | int | no | `3` | 1 or more |

- Each target is probed with `GET`. The probe's timeout is the shorter of `interval` and the upstream `timeout`.
- A 2xx response is a success. Anything else, a connection error or a timeout is a failure.
- Targets start healthy, and the first round of probes runs at startup.
- After `unhealthy_threshold` failures in a row, the target is marked unhealthy and the load balancer skips it. One success marks it healthy again.
- Health checks don't count toward the circuit breaker or rate limits.

### CircuitBreaker

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `threshold` | int | yes | — | 1 or more failures |
| `window` | duration | no | `"60s"` | Failures older than this are dropped from the count |
| `cooldown` | duration | no | `"30s"` |  |

- There's one breaker per route.
- A failure is any upstream status of 500 or above, including the 502 and 504 stand-ins. 4xx responses are not failures.
- Every attempt counts, retries included. The breaker is checked before each attempt, so retries stop as soon as it opens.

| State | Event | Next state |
| --- | --- | --- |
| closed | Failures within `window` reach `threshold` | open |
| open | Any request | Rejected with **503** `{"error":"service_unavailable","retry_after":<int>}` plus a `Retry-After` header. Stays open |
| open | `cooldown` has passed since it opened | half-open |
| half-open | First request | Sent to the upstream as a trial. Other requests in the meantime get 503 with `retry_after: 1` |
| half-open | The trial succeeds | closed, failure count reset |
| half-open | The trial fails | open, `cooldown` starts again |

`retry_after` is the number of cooldown seconds left, rounded up, and never less than 1. The 503 body contains only those two keys, exactly as the brief specifies.

## 8. `auth`

`auth` is tagged by its `type` field so more schemes can be added later. Today only `api_key` exists. An unknown `type` stops startup, so a typo can never quietly turn auth off.

| Field | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `type` | enum | yes | — | `api_key` |
| `header` | string | no | `"X-API-Key"` | Header name, matched case-insensitively |
| `keys` | list of string | yes | — | At least one non-empty key |

- If the header is missing, return **401** `{"error":"unauthorized","message":"missing API key"}`.
- If the key isn't in `keys`, return **401** with `"message":"invalid API key"`. Both cases use 401, not 403, so the response doesn't reveal which keys exist.
- Keys are compared in constant time.
- On success, the header is forwarded to the upstream unchanged (see §14).
- Future types would sit beside this one, for example `bearer` with `tokens`, or `basic` with `users`. Each type validates its own fields.

## 9. `request_transform` and `response_transform`

Transforms change headers and JSON bodies as they pass through the gateway. Any string value in them can be a literal or a `$variable`, and both directions use the same variable resolver.

| Path | Type | Req | Default | Rules |
| --- | --- | --- | --- | --- |
| `*.headers.add` | map of string to string | no | `{}` | Sets each header, replacing any existing value |
| `*.headers.remove` | list of string | no | `[]` | Header names, case-insensitive. Removing a header that isn't there is fine |
| `request_transform.body.mapping` | map of dot-path to source | no | none | See “Body mapping” |
| `response_transform.body.envelope` | nested map | no | none | See “Envelope” |

**Order of operations.**

- Request: remove headers, then add headers, then map the body.
- Response: wrap the body in the envelope, then remove headers, then add headers.

Transforms can't change the headers the gateway manages itself: `Host`, `Content-Length`, `Transfer-Encoding` and the hop-by-hop headers. If a config tries, the gateway logs a warning and ignores that entry.

### Variables

| Token | Resolves to | Allowed in |
| --- | --- | --- |
| `$request_time` | When the gateway received the request, as an ISO-8601 UTC string with milliseconds, e.g. `2026-11-19T19:43:05.123Z` | Both directions |
| `$response_time` | When the upstream response arrived, in the same format | Response only |
| `$route_path` | The matched route's `path` from the config, e.g. `/api/legacy` | Both directions |
| `$body` | The upstream response body: parsed JSON if it parses, otherwise the raw string; `null` if empty | `response_transform.body.envelope` only |
| `$literal:<text>` | `<text>` as a string, e.g. `$literal:gateway` gives `gateway` | Both directions |

- A value is resolved only if the **whole** string is a token. Anything else is used as a literal, so `"v=$request_time"` is sent exactly as written. There's no interpolation inside strings.
- An unknown `$token`, or a token used outside its “Allowed in” scope, stops startup. To send a literal string that starts with `$`, write `$literal:$foo`.

### Body mapping (request)

The mapping **builds a new JSON object**. Each key is a destination dot-path, and each value is a source dot-path or a variable. Fields of the original body that aren't mapped are dropped.

```yaml
mapping:
  user.id: "userId"               # copy body.userId to out.user.id
  user.name: "userName"
  meta.source: "$literal:gateway" # literal string
  meta.timestamp: "$request_time"
```

```json
// in:  {"userId": 7, "userName": "Ada", "debug": true}
// out: {"user": {"id": 7, "name": "Ada"},
//       "meta": {"source": "gateway", "timestamp": "2026-11-19T19:43:05.123Z"}}
```

- Mapping applies only when the request's `Content-Type` is `application/json` or `*+json`, and the body parses to an object. Other bodies, and empty bodies, are forwarded unchanged.
- If the content type says JSON but the body doesn't parse, return **400** `{"error":"invalid_json_body"}`.
- Source paths can index into arrays with numeric segments, e.g. `items.0.sku`. In destination paths, every segment is an object key.
- If a source path doesn't exist, the destination key is left out. A source value of `null` is copied as `null`.
- Two destinations that conflict, such as `a: x` and `a.b: y`, stop startup. Keys containing a dot can't be addressed.
- `Content-Length` is recalculated after mapping.

### Envelope (response)

The envelope is a template that can be nested to any depth. String leaves go through the variable resolver; other scalars (numbers, bools, null) are copied as they are.

```yaml
envelope:
  data: "$body"
  gateway_metadata:
    served_at: "$response_time"
    route: "$route_path"
```

- The envelope applies to every response that came from the upstream, including its 4xx and 5xx responses, and keeps the upstream's status code. It doesn't apply to errors the gateway produces itself (§12).
- On routes with an envelope, the gateway asks the upstream for an uncompressed body (`Accept-Encoding: identity`) so it can parse it.
- The output is JSON: `Content-Type: application/json`, with `Content-Length` recalculated.

## 10. Defaults and precedence

The loader fills in every default and override once, at startup, and produces a normalized model. Code that runs per request reads only that model and never looks at raw YAML or checks for missing fields.

| Setting | Where the value comes from, highest priority first |
| --- | --- |
| Per-attempt timeout | `upstream.timeout`, then `route.timeout` (lenient placement), then `gateway.global_timeout`, then `30s` |
| Rate limit | `route.rate_limit` merged field by field over `gateway.global_rate_limit`. If both are absent, the route has no limit |
| Methods | `route.methods`, then all 7 methods |
| Targets | `upstream.targets`, otherwise `[{url: upstream.url, weight: 1}]` |
| Balance | `upstream.balance`, then `round_robin` |
| Retry fields | `backoff: fixed`, `initial_delay: 1s`, `on: [502, 503, 504]` |
| Health check fields | `interval: 30s`, `unhealthy_threshold: 3` |
| Circuit breaker fields | `window: 60s`, `cooldown: 30s` |
| Auth header | `X-API-Key` |
| Port | `gateway.port`, then `8080` |

The normalized model, sketched without committing to a language:

```
GatewayConfig { port, routes: Route[] }   // routes indexed by path for longest-prefix lookup

Route {
  path, methods: Set<Method>, stripPrefix: bool,
  targets: [{ url, weight }], balance, timeoutMs,
  retry?:          { attempts, backoff, initialDelayMs, on: Set<int> },
  healthCheck?:    { path, intervalMs, unhealthyThreshold },
  rateLimit?:      { requests, windowMs, strategy, per },
  auth?:           { type, ...typeSpecific },
  circuitBreaker?: { threshold, windowMs, cooldownMs },
  requestTransform?:  { addHeaders, removeHeaders, mapping?: CompiledMapping },
  responseTransform?: { addHeaders, removeHeaders, envelope?: CompiledTemplate }
}
```

Durations become milliseconds, header names become lowercase, and variables are parsed into tokens at load time. A `$typo` therefore fails at startup instead of surfacing on the first request.

## 11. Validation and startup behavior

An invalid config never starts. The loader collects **every** error instead of stopping at the first, prints each one with its location in the file, and exits with code 1. A partial config is never served.

```
config error: 3 problems in gateway.yaml
  routes[1].upstream: exactly one of "url" or "targets" is required
  routes[2].upstream.targets[0].weight: must be an integer >= 1 (got 0)
  routes[3].response_transform.headers.add.X-Time: "$requst_time" is not a known variable
```

**Fatal errors.** Rules already listed in the field tables aren't repeated here. Checks that involve more than one field:

- The YAML can't be parsed, or the top-level value isn't a map.
- Both `routes` and `gateway.routes` are present, or one key is set at both the route level and the upstream level (§2).
- Two routes have the same `path` after the trailing slash is removed.
- `upstream` has neither or both of `url` and `targets`.
- A route's rate limit, after merging with the global one, has no `requests` or no `window`.
- An enum has a value that isn't allowed. This includes an unknown `auth.type`, `strategy`, `per`, `balance` or `backoff`.
- A variable is unknown or used outside its scope, or two mapping destinations conflict (§9).
- A field has the wrong type, for example `port: "abc"` or `methods: GET` instead of a list.

**Warnings** (logged, startup continues):

- Unknown keys, reported with their location.
- A route has path `/health` (§3).
- `balance` or `weight` is set on an upstream with only one target.
- A transform touches a header the gateway manages (§9).

`routes: []` is valid. The gateway then serves `/health` and answers everything else with 404.

## 12. Runtime contracts

Every error the gateway produces itself is JSON (`Content-Type: application/json`), in the form `{"error":"<code>","message":"<text>"}`. The circuit-breaker 503 is the one exception: it has exactly the keys the brief gives. Responses relayed from the upstream are passed through unchanged, apart from any configured transforms.

| Status | `error` code | When | Extra |
| --- | --- | --- | --- |
| 200 | — | `GET /health` | `{"status":"healthy","uptime_seconds":<int>}` |
| 400 | `invalid_json_body` | A route with body mapping receives a JSON content type with a body that doesn't parse |  |
| 401 | `unauthorized` | API key is missing or invalid |  |
| 404 | `not_found` | No route matches |  |
| 405 | `method_not_allowed` | A route matches but the method isn't allowed | `Allow` header |
| 429 | `rate_limit_exceeded` | The bucket is used up | `retry_after` in the body, `Retry-After` header, `X-RateLimit-*` headers |
| 502 | `bad_gateway` | Connection refused or reset, or an invalid upstream response, after any retries |  |
| 503 | `service_unavailable` | The circuit breaker is open or half-open | Body is only `{"error","retry_after"}`, plus a `Retry-After` header |
| 503 | `no_healthy_upstream` | Every target is unhealthy |  |
| 504 | `gateway_timeout` | The upstream timeout ran out, after any retries |  |

The stages each request passes through, in order. Each one can end the request early:

1. Health check (`GET /health`)
2. Route match (404) and method check (405)
3. Rate limit (429)
4. Auth (401)
5. Request transform (400)
6. Circuit breaker check (503)
7. Load balancer picks a target (503 if none are healthy)
8. Proxy the request, with the timeout (502 or 504). If a retry is allowed, go back to step 6
9. Response transform
10. Respond and write the access log line

## 13. Variation test configs

Graders will run a second config with the same schema and different values, so CI should run at least these two configs alongside the sample. Together they cover defaults, the lenient placements and edge cases that the sample never hits.

**A. The smallest valid config.** Every default applies.

```yaml
gateway: {}
routes:
  - path: "/"
    upstream:
      url: "http://localhost:4000"
```

**B. A config unlike the sample.** It uses the lenient placements, `ms` durations and a base path.

```yaml
gateway:
  port: 9090
  global_timeout: "750ms"
  routes:                           # nested under gateway
    - path: "/v2/"                  # trailing slash is removed
      methods: ["get", "delete"]    # lowercase methods
      strip_prefix: true
      timeout: "2s"                 # route-level timeout
      upstream:
        url: "http://localhost:4001/base"
      rate_limit: { requests: 2, window: "1s", per: global }
    - path: "/v2/admin"             # longer prefix than /v2
      upstream:
        targets:
          - { url: "http://localhost:4002" }
          - { url: "http://localhost:4003" }
        retry: { attempts: 2, backoff: fixed, initial_delay: "100ms", on: [500] }
      auth: { type: api_key, header: "Authorization", keys: ["k1"] }
      response_transform:
        body:
          envelope:
            result: { payload: "$body", ok: true }
            route: "$route_path"
```

| Case | Request | Expected |
| --- | --- | --- |
| Smallest config, defaults | `GET :8080/anything` on config A | Proxied to `:4000/anything` |
| Port from config | Config B | Listens on 9090; `/health` returns 200 there |
| Longest prefix wins | `GET /v2/admin/x` | Matches `/v2/admin`, not `/v2` |
| Segment boundary | `GET /v2x` | 404 |
| Lowercase methods accepted | `DELETE /v2/a` | Proxied to `/base/a` |
| Method not listed | `POST /v2/a` | 405 with `Allow: GET, DELETE` |
| Global bucket | 3 requests to `/v2/a` within 1s from 2 different IPs | Third gets 429 |
| Route timeout beats global | Upstream on `/v2` delays 1s | Proxied (limit is 2s, not 750ms) |
| Custom retry status | `:4002` returns 500 once | Retried on `:4003` after 100ms; 200 returned |
| Custom auth header | `/v2/admin` with no `Authorization` header | 401 |
| Nested envelope | `/v2/admin` upstream returns `[1,2]` | `{"result":{"payload":[1,2],"ok":true},"route":"/v2/admin"}` |
| Invalid config | Config with `balance: random` | Exit code 1 with a message giving the location |

## 14. Decisions and open questions

The brief leaves the points below unspecified. Each row records the choice this spec makes so every part of the build handles them the same way. Revisit any row by commenting on it.

| Question | Decision | Why |
| --- | --- | --- |
| Where `routes` sits | Accept top level or `gateway.routes` | The sample lost its indentation; accepting both is safe against either grader config |
| Where `timeout` and `retry` sit | Accept under `upstream` or on the route | Same reason |
| Meaning of `retry.attempts` | Total tries, including the first | `attempts: 1` = no retry reads naturally |
| Missing vs wrong API key | 401 for both | Doesn't reveal which keys exist |
| Forward the API-key header upstream | Yes, unchanged | Keeps the gateway transparent. Stripping it is a one-line change if the team prefers |
| Trust `X-Forwarded-For` for the client IP | No, use the socket address | Clients can fake the header. A `trusted_proxies` field could be added later |
| Sliding window method | Exact sliding log | Exact, and memory per bucket never exceeds `requests` timestamps |
| Fixed window alignment | Aligned to the clock | Simple and predictable in tests |
| Envelope on upstream 4xx and 5xx | Yes, status code kept | The upstream's errors get the same shape as its successes |
| `HEAD` allowed when `GET` is | No | Only the listed methods are allowed |
| `$var` inside a longer string | Not supported; whole value only | Nothing like `"v=$x"` appears in the sample |
| Turning off the global rate limit on one route | **Open.** Proposal: accept `rate_limit: false` | The schema has no way to do this yet |
| Reloading the config without a restart | **Open.** Out of scope for v1 | The brief doesn't ask for it |
