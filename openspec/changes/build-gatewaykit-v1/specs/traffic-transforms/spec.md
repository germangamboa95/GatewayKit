# Spec Delta

## Purpose

Changes headers and JSON bodies as requests and responses pass through the gateway, using one variable resolver for both directions.

## ADDED Requirements

### Requirement: Variable resolution
The gateway SHALL resolve these tokens: `$request_time` (when the gateway received the request, ISO-8601 UTC with milliseconds), `$response_time` (when the upstream response arrived, same format, response only), `$route_path` (the matched route's configured `path`), `$body` (envelope only) and `$literal:<text>` (`<text>` as a string). Tokens come from load-time parsing; the gateway never parses them per request.

#### Scenario: Request time format
- **WHEN** a request arrives at 2026-11-19 19:43:05.123 UTC on a route whose request transform adds `X-Request-Start: "$request_time"`
- **THEN** the upstream receives `X-Request-Start: 2026-11-19T19:43:05.123Z`

#### Scenario: Route path
- **WHEN** a request to `/api/legacy/v1/data` matches route `/api/legacy` and the envelope sets `route: "$route_path"`
- **THEN** the response body contains `"route": "/api/legacy"`

#### Scenario: Literal token
- **WHEN** a value is `$literal:gateway`
- **THEN** the gateway uses the string `gateway`

#### Scenario: Literal that starts with a dollar sign
- **WHEN** a value is `$literal:$foo`
- **THEN** the gateway uses the string `$foo`

### Requirement: Whole-value tokens only
The gateway SHALL resolve a value only when the whole string is a token. Any other string SHALL be used as a literal. The gateway MUST NOT interpolate tokens inside longer strings.

#### Scenario: Token inside a longer string
- **WHEN** a header add value is `"v=$request_time"`
- **THEN** the gateway sends the header value `v=$request_time` exactly as written

### Requirement: Body variable resolution
In a response envelope, `$body` SHALL resolve to the upstream response body parsed as JSON when it parses, otherwise to the raw body as a string, and to `null` when the body is empty.

#### Scenario: JSON body
- **WHEN** the upstream returns `{"id": 7}` and the envelope sets `data: "$body"`
- **THEN** the response contains `"data": {"id": 7}`

#### Scenario: Plain-text body
- **WHEN** the upstream returns the plain-text body `hello` and the envelope sets `data: "$body"`
- **THEN** the response contains `"data": "hello"`

#### Scenario: Empty body
- **WHEN** the upstream returns an empty body and the envelope sets `data: "$body"`
- **THEN** the response contains `"data": null`

### Requirement: Header add
`headers.add` SHALL set each listed header to its resolved value, replacing any existing value of that header.

#### Scenario: Request header added
- **WHEN** a request matches `/api/legacy` in the sample config
- **THEN** the upstream receives `X-Gateway: gatewaykit`

#### Scenario: Existing value replaced
- **WHEN** a client sends `X-Gateway: client` to `/api/legacy`
- **THEN** the upstream receives only `X-Gateway: gatewaykit`

#### Scenario: Response header added
- **WHEN** the upstream for `/api/legacy` responds
- **THEN** the client receives `X-Served-By: gatewaykit`

### Requirement: Header remove
`headers.remove` SHALL delete each listed header, matching names case-insensitively. Removing a header that is absent SHALL NOT be an error.

#### Scenario: Request headers removed
- **WHEN** a client sends `x-debug: 1` and `X-Internal: yes` to `/api/legacy`
- **THEN** the upstream receives neither header

#### Scenario: Response headers removed
- **WHEN** the `/api/legacy` upstream responds with `Server: mock` and `X-Powered-By: mock`
- **THEN** the client receives neither header

#### Scenario: Absent header
- **WHEN** a client sends no `X-Debug` header to `/api/legacy`
- **THEN** the gateway forwards the request without error

### Requirement: Transform order
The request transform SHALL remove headers, then add headers, then map the body. The response transform SHALL wrap the body in the envelope, then remove headers, then add headers.

#### Scenario: Remove then add on the same header
- **WHEN** a request transform both removes and adds `X-Gateway` and the client sends `X-Gateway: client`
- **THEN** the upstream receives the configured value from `add`

### Requirement: Managed headers protected
Transforms MUST NOT change `Host`, `Content-Length`, `Transfer-Encoding` or the hop-by-hop headers. The gateway SHALL ignore any transform entry that names one of them; the warning is logged at load time.

#### Scenario: Transform names a managed header
- **WHEN** a request transform adds `Host: evil.example`
- **THEN** the upstream receives `Host` set to the target's host

### Requirement: Request body mapping
When a route has `request_transform.body.mapping`, the gateway SHALL build a new JSON object in which each key is a destination dot-path and each value comes from a source dot-path or a variable. Fields of the original body that are not mapped SHALL be dropped.

#### Scenario: Sample mapping
- **WHEN** a client POSTs `{"userId": 7, "userName": "Ada", "debug": true}` with `Content-Type: application/json` to `/api/legacy` at 2026-11-19T19:43:05.123Z
- **THEN** the upstream receives `{"user": {"id": 7, "name": "Ada"}, "meta": {"source": "gateway", "timestamp": "2026-11-19T19:43:05.123Z"}}`

### Requirement: Mapping applies only to JSON objects
The gateway SHALL apply body mapping only when the request `Content-Type` is `application/json` or ends in `+json` and the body parses to a JSON object. Other bodies and empty bodies SHALL be forwarded unchanged.

#### Scenario: Non-JSON body
- **WHEN** a client POSTs `a=1` with `Content-Type: application/x-www-form-urlencoded` to `/api/legacy`
- **THEN** the upstream receives the body `a=1` unchanged

#### Scenario: Suffix JSON content type
- **WHEN** a client POSTs `{"userId": 7}` with `Content-Type: application/vnd.api+json` to `/api/legacy`
- **THEN** the upstream receives the mapped object

#### Scenario: JSON array body
- **WHEN** a client POSTs `[1, 2]` with `Content-Type: application/json` to `/api/legacy`
- **THEN** the upstream receives `[1, 2]` unchanged

#### Scenario: Empty body
- **WHEN** a client POSTs an empty body with `Content-Type: application/json` to `/api/legacy`
- **THEN** the upstream receives an empty body

### Requirement: Invalid JSON body rejected
When a route has body mapping and the request declares a JSON content type but the body does not parse, the gateway SHALL return 400 with `{"error":"invalid_json_body","message":"<text>"}` and SHALL NOT call the upstream.

#### Scenario: Malformed JSON
- **WHEN** a client POSTs `{"userId":` with `Content-Type: application/json` to `/api/legacy`
- **THEN** the gateway returns 400 with `"error": "invalid_json_body"`
- **AND** the upstream receives no request

### Requirement: Mapping path semantics
Numeric segments in a source path SHALL index into arrays. Every destination segment SHALL be an object key. A source path that does not exist SHALL leave its destination key out. A source value of `null` SHALL be copied as `null`.

#### Scenario: Array index in source
- **WHEN** a mapping sets `first.sku: "items.0.sku"` and the body is `{"items": [{"sku": "A1"}]}`
- **THEN** the upstream receives `{"first": {"sku": "A1"}}`

#### Scenario: Missing source
- **WHEN** the body for `/api/legacy` is `{"userId": 7}` with no `userName`
- **THEN** the upstream body contains `"user": {"id": 7}` with no `name` key

#### Scenario: Null source
- **WHEN** the body for `/api/legacy` is `{"userId": null}`
- **THEN** the upstream body contains `"user": {"id": null}`

### Requirement: Content-Length after body changes
After body mapping or envelope wrapping, the gateway SHALL set `Content-Length` to the length of the new body.

#### Scenario: Mapped request length
- **WHEN** the gateway maps a request body
- **THEN** the upstream receives a `Content-Length` equal to the mapped body's byte length

### Requirement: Response envelope
When a route has `response_transform.body.envelope`, the gateway SHALL build the response body from the template, nested to any depth. String leaves SHALL go through the variable resolver; numbers, booleans and null SHALL be copied as they are. The output SHALL be JSON with `Content-Type: application/json`.

#### Scenario: Sample envelope
- **WHEN** the `/api/legacy` upstream returns `{"ok": 1}` at 2026-11-19T19:43:06.000Z
- **THEN** the client receives `{"data": {"ok": 1}, "gateway_metadata": {"served_at": "2026-11-19T19:43:06.000Z", "route": "/api/legacy"}}` with `Content-Type: application/json`

#### Scenario: Nested envelope with literal scalar
- **WHEN** variant B's `/v2/admin` upstream returns `[1,2]`
- **THEN** the client receives `{"result":{"payload":[1,2],"ok":true},"route":"/v2/admin"}`

### Requirement: Envelope scope
The envelope SHALL apply to every response that came from the upstream, including 4xx and 5xx, and SHALL keep the upstream's status code. It MUST NOT apply to errors the gateway produces itself.

#### Scenario: Upstream error wrapped
- **WHEN** the `/api/legacy` upstream returns 503 with body `{"e": 1}`
- **THEN** the client receives 503 with `{"data": {"e": 1}, "gateway_metadata": {...}}`

#### Scenario: Gateway error not wrapped
- **WHEN** a request to `/api/legacy` fails with a gateway-generated 504
- **THEN** the client receives the standard error body `{"error":"gateway_timeout","message":"<text>"}` without an envelope

### Requirement: Uncompressed upstream body on envelope routes
On routes with an envelope, the gateway SHALL send `Accept-Encoding: identity` to the upstream so it can parse the body.

#### Scenario: Client asks for gzip
- **WHEN** a client sends `Accept-Encoding: gzip` to `/api/legacy`
- **THEN** the upstream receives `Accept-Encoding: identity`
