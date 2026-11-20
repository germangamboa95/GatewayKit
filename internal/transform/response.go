package transform

import (
	"net/http"
	"strconv"

	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

// ResponseMiddleware returns the response-transform stage for route:
// envelope, then remove headers, then add headers, on upstream responses
// only (config spec §9). Routes without a response transform pass through.
// A nil clk means the system clock.
func ResponseMiddleware(route *config.Route, clk clock.Clock) gateway.Middleware {
	if route == nil || route.RespTransform == nil {
		return gateway.PassThrough
	}
	if clk == nil {
		clk = clock.System{}
	}
	rt := route.RespTransform
	return func(next gateway.Handler) gateway.Handler {
		return func(rc *gateway.RequestContext) *gateway.Response {
			if rt.Envelope != nil && rc.Req != nil {
				if rc.Req.Header == nil {
					rc.Req.Header = http.Header{}
				}
				rc.Req.Header.Set("Accept-Encoding", "identity")
			}
			resp := next(rc)
			if resp == nil || !resp.FromUpstream {
				return resp
			}
			env := Env{
				RequestTime:  rc.RequestTime,
				ResponseTime: rc.ResponseTime,
				RoutePath:    route.Path,
			}
			if env.ResponseTime.IsZero() {
				env.ResponseTime = clk.Now()
			}
			if env.RequestTime.IsZero() {
				env.RequestTime = env.ResponseTime
			}
			if resp.Header == nil {
				resp.Header = http.Header{}
			}
			if rt.Envelope != nil {
				resp.Body = Envelope(rt.Envelope, resp.Body, env)
				resp.Header.Del("Content-Encoding")
				resp.Header.Set("Content-Type", "application/json")
				resp.Header.Set("Content-Length", strconv.Itoa(len(resp.Body)))
			}
			ApplyHeaders(resp.Header, rt.RemoveHeaders, rt.AddHeaders, env)
			return resp
		}
	}
}
