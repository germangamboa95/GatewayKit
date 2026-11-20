# server

Root `http.Handler` for the gateway (design D6).

- `New(cfg *contract.GatewayConfig, chains map[*contract.Route]contract.Handler, deps contract.Deps) *Server`: `*Server` is an `http.Handler`. It answers exactly `GET /health`, routes via `internal/router` (404, 405 with `Allow`), reads the body, builds `RequestContext` (ClientIP from `RemoteAddr`, RequestTime from `deps.Clock`), runs the route's chain and writes the `*contract.Response`. A missing chain, nil response or panic yields 502 `bad_gateway`.
- `Chain(terminal contract.Handler, mws ...contract.Middleware) contract.Handler`: the first middleware runs first; nil entries are skipped.
- `Write(w, r, resp)`: writes a `*contract.Response`, setting `Content-Length` except on HEAD, 1xx, 204 and 304.
- `ClientIP(addr string) string`: host part of a socket address.
- Access log: one `deps.Logger` Info line `request` per request, with `method`, `path`, `status`, `route` (empty when unmatched), `duration`, `client_ip`.
- Uptime counts from the call to `New`.

Router: `router.New(routes)`, `Resolve(method, path) (*contract.Route, *contract.Response)`, `Match`, `Matches`, `Allowed`, `AllowList`.
