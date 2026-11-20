package transform

import (
	"bytes"
	"io"
	"net/http"
	"strconv"
	"strings"

	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

// RequestMiddleware returns the request-transform stage for route: remove
// headers, add headers, then map a JSON object body (config spec §9).
// Routes without a request transform pass through. A nil clk means the
// system clock.
func RequestMiddleware(route *config.Route, clk clock.Clock) gateway.Middleware {
	if route == nil || route.ReqTransform == nil {
		return gateway.PassThrough
	}
	if clk == nil {
		clk = clock.System{}
	}
	rt := route.ReqTransform
	return func(next gateway.Handler) gateway.Handler {
		return func(rc *gateway.RequestContext) *gateway.Response {
			env := Env{RequestTime: rc.RequestTime, RoutePath: route.Path}
			if env.RequestTime.IsZero() {
				env.RequestTime = clk.Now()
			}
			if rc.Req == nil {
				return next(rc)
			}
			if rc.Req.Header == nil {
				rc.Req.Header = http.Header{}
			}
			ApplyHeaders(rc.Req.Header, rt.RemoveHeaders, rt.AddHeaders, env)
			if rt.Mapping != nil && IsJSON(rc.Req.Header.Get("Content-Type")) {
				body, err := requestBody(rc)
				if err != nil {
					return InvalidJSONBody()
				}
				out, mapped, err := MapBody(body, rt.Mapping, env)
				if err != nil {
					return InvalidJSONBody()
				}
				if mapped {
					setBody(rc, out)
				}
			}
			return next(rc)
		}
	}
}

// IsJSON reports whether a Content-Type is application/json or ends in +json.
func IsJSON(ct string) bool {
	mt, _, _ := strings.Cut(ct, ";")
	mt = strings.ToLower(strings.TrimSpace(mt))
	return mt == "application/json" || strings.HasSuffix(mt, "+json")
}

func requestBody(rc *gateway.RequestContext) ([]byte, error) {
	if rc.Body != nil || rc.Req.Body == nil || rc.Req.Body == http.NoBody {
		return rc.Body, nil
	}
	b, err := io.ReadAll(rc.Req.Body)
	rc.Req.Body.Close()
	if err != nil {
		return nil, err
	}
	rc.Body = b
	rc.Req.Body = io.NopCloser(bytes.NewReader(b))
	return b, nil
}

func setBody(rc *gateway.RequestContext, b []byte) {
	rc.Body = b
	rc.Req.Body = io.NopCloser(bytes.NewReader(b))
	rc.Req.GetBody = func() (io.ReadCloser, error) { return io.NopCloser(bytes.NewReader(b)), nil }
	rc.Req.ContentLength = int64(len(b))
	rc.Req.TransferEncoding = nil
	rc.Req.Header.Set("Content-Length", strconv.Itoa(len(b)))
}

// InvalidJSONBody is the 400 for a JSON request body that does not parse.
func InvalidJSONBody() *gateway.Response {
	return gateway.ErrorResponse(http.StatusBadRequest, gateway.CodeInvalidJSONBody, "request body is not valid JSON", nil)
}
