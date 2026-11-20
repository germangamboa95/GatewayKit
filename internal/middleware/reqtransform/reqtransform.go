// Package reqtransform is the request-transform middleware: remove headers,
// add headers, then map a JSON object body (config spec §9).
package reqtransform

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/transform"
)

// New returns the reqtransform middleware for route.
func New(route *contract.Route, deps contract.Deps) contract.Middleware {
	if route == nil || route.ReqTransform == nil {
		return func(next contract.Handler) contract.Handler { return next }
	}
	rt := route.ReqTransform
	deps = deps.WithDefaults()
	return func(next contract.Handler) contract.Handler {
		return func(ctx *contract.RequestContext) *contract.Response {
			env := transform.Env{RequestTime: ctx.RequestTime, RoutePath: route.Path}
			if env.RequestTime.IsZero() {
				env.RequestTime = deps.Clock.Now()
			}
			if ctx.Req == nil {
				return next(ctx)
			}
			if ctx.Req.Header == nil {
				ctx.Req.Header = http.Header{}
			}
			transform.ApplyHeaders(ctx.Req.Header, rt.RemoveHeaders, rt.AddHeaders, env)
			if rt.Mapping != nil && IsJSON(ctx.Req.Header.Get("Content-Type")) {
				body, err := requestBody(ctx)
				if err != nil {
					return contract.InvalidJSONBody()
				}
				out, mapped, err := transform.MapBody(body, rt.Mapping, env)
				if err != nil {
					return contract.InvalidJSONBody()
				}
				if mapped {
					setBody(ctx, out)
				}
			}
			return next(ctx)
		}
	}
}

// IsJSON reports whether a Content-Type is application/json or ends in +json.
func IsJSON(ct string) bool {
	mt, _, _ := strings.Cut(ct, ";")
	mt = strings.ToLower(strings.TrimSpace(mt))
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

func requestBody(ctx *contract.RequestContext) ([]byte, error) {
	if ctx.Body != nil || ctx.Req.Body == nil || ctx.Req.Body == http.NoBody {
		return ctx.Body, nil
	}
	b, err := io.ReadAll(ctx.Req.Body)
	ctx.Req.Body.Close()
	if err != nil {
		return nil, err
	}
	ctx.Body = b
	ctx.Req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

func setBody(ctx *contract.RequestContext, b []byte) {
	ctx.Body = b
	ctx.Req.Body = io.NopCloser(bytes.NewReader(b))
	ctx.Req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
	ctx.Req.ContentLength = int64(len(b))
	ctx.Req.TransferEncoding = nil
	ctx.Req.Header.Set("Content-Length", strconv.Itoa(len(b)))
}
