// Package server is the gateway's root http.Handler (design D6).
package server

import (
	"io"
	"net"
	"net/http"
	"strconv"
	"time"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/router"
)

// Server answers GET /health, routes, then runs the matched route's chain.
type Server struct {
	router *router.Router
	chains map[*contract.Route]contract.Handler
	deps   contract.Deps
	start  time.Time
}

// New builds the root handler. chains maps each route to its assembled
// handler. Uptime counts from the moment New is called.
func New(cfg *contract.GatewayConfig, chains map[*contract.Route]contract.Handler, deps contract.Deps) *Server {
	deps = deps.WithDefaults()
	var routes []*contract.Route
	if cfg != nil {
		routes = cfg.Routes
	}
	return &Server{
		router: router.New(routes),
		chains: chains,
		deps:   deps,
		start:  deps.Clock.Now(),
	}
}

func (s *Server) ServeHTTP(w http.ResponseWriter, r *http.Request) {
	now := s.deps.Clock.Now()
	_, resp := s.handle(r, now)
	Write(w, r, resp)
}

func (s *Server) handle(r *http.Request, now time.Time) (*contract.Route, *contract.Response) {
	if r.Method == http.MethodGet && r.URL.Path == "/health" {
		return nil, s.health(now)
	}
	route, errResp := s.router.Resolve(r.Method, r.URL.Path)
	if errResp != nil {
		return route, errResp
	}
	chain := s.chains[route]
	if chain == nil {
		s.deps.Logger.Error("no handler for route", "route", route.Path)
		return route, contract.BadGateway()
	}
	var body []byte
	if r.Body != nil {
		b, err := io.ReadAll(r.Body)
		if err != nil {
			return route, &contract.Response{Status: contract.StatusClientClosed}
		}
		body = b
	}
	ctx := &contract.RequestContext{
		Req:         r,
		ClientIP:    ClientIP(r.RemoteAddr),
		Route:       route,
		RequestTime: now,
		Body:        body,
	}
	resp := s.run(chain, ctx)
	if resp == nil {
		return route, contract.BadGateway()
	}
	return route, resp
}

func (s *Server) run(chain contract.Handler, ctx *contract.RequestContext) (resp *contract.Response) {
	defer func() {
		if v := recover(); v != nil {
			s.deps.Logger.Error("handler panic", "route", ctx.Route.Path, "panic", v)
			resp = contract.BadGateway()
		}
	}()
	return chain(ctx)
}

func (s *Server) health(now time.Time) *contract.Response {
	up := int64(now.Sub(s.start) / time.Second)
	if up < 0 {
		up = 0
	}
	return &contract.Response{
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
func Write(w http.ResponseWriter, r *http.Request, resp *contract.Response) {
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
