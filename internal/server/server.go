// Package server is the gateway's root http.Handler: it answers the health
// endpoint, routes each request, runs the route's chain and writes the
// response.
package server

import (
	"io"
	"log/slog"
	"net"
	"net/http"
	"strconv"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
	"github.com/germangamboa95/gatewaykit/internal/router"
)

// Server answers GET /health, routes, runs the matched route's chain, writes
// the response, then logs one access line.
type Server struct {
	router *router.Router
	chains map[*config.Route]gateway.Handler
	clock  clock.Clock
	logger *slog.Logger
	start  time.Time
}

// New builds the root handler. chains maps each route to its assembled
// handler. Uptime counts from the moment New is called. A nil clk means the
// system clock; a nil logger means slog.Default().
func New(cfg *config.Gateway, chains map[*config.Route]gateway.Handler, clk clock.Clock, logger *slog.Logger) *Server {
	if clk == nil {
		clk = clock.System{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	var routes []*config.Route
	if cfg != nil {
		routes = cfg.Routes
	}
	return &Server{
		router: router.New(routes),
		chains: chains,
		clock:  clk,
		logger: logger,
		start:  clk.Now(),
	}
}

// ServeHTTP handles one request end to end.
func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := s.clock.Now()
	route, resp := s.handle(r, now)
	Write(w, r, resp)
	s.logAccess(r, route, resp.Status, s.clock.Now().Sub(now))
}

func (s *Server) handle(r *http.Request, now time.Time) (*config.Route, *gateway.Response) {
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		return nil, s.health(now)
	}
	route, errResp := s.router.Resolve(r.Method, r.URL.Path)
	if errResp != nil {
		return route, errResp
	}
	chain := s.chains[route]
	if chain == nil {
		s.logger.Error("no handler for route", "route", route.Path)
		return route, gateway.BadGateway()
	}
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return route, &gateway.Response{Status: gateway.StatusClientClosed}
		}
		body = b
	}
	rc := &gateway.RequestContext{
		Req:         r,
		ClientIP:    ClientIP(r.RemoteAddr),
		Route:       route,
		RequestTime: now,
		Body:        body,
	}
	resp := s.run(chain, rc)
	if resp == nil {
		return route, gateway.BadGateway()
	}
	return route, resp
}

func (s *Server) run(chain gateway.Handler, rc *gateway.RequestContext) (resp *gateway.Response) {
	defer func() {
		if v := recover(); v != nil {
			s.logger.Error("handler panic", "route", rc.Route.Path, "panic", v)
			resp = gateway.BadGateway()
		}
	}()
	return chain(rc)
}

func (s *Server) health(now time.Time) *gateway.Response {
	up := int64(now.Sub(s.start) / time.Second)
	if up < 0 {
		up = 0
	}
	return &gateway.Response{
		Status: http.StatusOK,
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   []byte(`{"status":"healthy","uptime_seconds":` + strconv.FormatInt(up, 10) + `}`),
	}
}

// ClientIP returns the host part of a socket address, or addr unchanged.
func ClientIP(addr string) string {
	host, _, err := net.SplitHostPort(addr)
	if err != nil {
		return addr
	}
	return host
}

// Write sends resp to the client. It sets Content-Length from the body,
// except on HEAD, 1xx, 204 and 304 responses, which carry no body.
func Write(w http.ResponseWriter, r *http.Request, resp *gateway.Response) {
	h := w.Header()
	for k, v := range resp.Header {
		h[k] = append([]string(nil), v...)
	}
	h.Del("Transfer-Encoding")
	status := resp.Status
	if status == 0 {
		status = http.StatusOK
	}
	noBody := status < 200 || status == http.StatusNoContent || status == http.StatusNotModified
	if status < 200 || status == http.StatusNoContent {
		h.Del("Content-Length")
	}
	if !noBody && r.Method != http.MethodHead {
		h.Set("Content-Length", strconv.Itoa(len(resp.Body)))
	}
	w.WriteHeader(status)
	if !noBody && r.Method != http.MethodHead && len(resp.Body) > 0 {
		_, _ = w.Write(resp.Body)
	}
}
