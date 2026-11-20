# proxy

Path rewrite, upstream client, per-attempt timeout, single-attempt terminal handler.

- `RewritePath(routePath, strip, reqPath, rawQuery) string`: forwarded path plus query, no base path.
- `JoinPath(base, p) string`: target base path joined without doubling `/`.
- `BuildRequest(*contract.RequestContext) *contract.OutRequest`: hop-by-hop removal, `X-Forwarded-*`, `Accept-Encoding: identity` on envelope routes.
- `NewClient() *Client`: `contract.UpstreamClient` on a shared transport, no redirects, no compression, buffers the full body.
- `Attempt(parent, client, target, out, timeout) (*Response, error)`: one attempt; timeout wraps `context.DeadlineExceeded`, client cancel wraps `context.Canceled`.
- `Outcome(resp, err) (*Response, int)`: upstream response as-is, or gateway 502/504; status 499 on client cancel.
- `New(route, deps) contract.Handler`: first target, one attempt.
