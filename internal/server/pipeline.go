package server

import (
	"context"
	"log/slog"
	"net/http"
	"time"

	"gatewaykit/internal/contract"
)

// Chain composes stages around terminal. The first middleware runs first; a
// stage that returns without calling next ends the request. Nil entries are skipped.
func Chain(terminal contract.Handler, mws ...contract.Middleware) contract.Handler {
	h := terminal
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			h = mws[i](h)
		}
	}
	return h
}

func (s *Server) logAccess(r *http.Request, route *contract.Route, status int, d time.Duration) {
	routePath := ""
	if route != nil {
		routePath = route.Path
	}
	s.deps.Logger.LogAttrs(context.Background(), slog.LevelInfo, "request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.String("route", routePath),
		slog.Duration("duration", d),
		slog.String("client_ip", ClientIP(r.RemoteAddr)),
	)
}
