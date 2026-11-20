package server

import (
	"log/slog"
	"net/http"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

func (s *Server) logAccess(r *http.Request, route *config.Route, status int, d time.Duration) {
	routePath := ""
	if route != nil {
		routePath = route.Path
	}
	s.logger.LogAttrs(r.Context(), slog.LevelInfo, "request",
		slog.String("method", r.Method),
		slog.String("path", r.URL.Path),
		slog.Int("status", status),
		slog.String("route", routePath),
		slog.Duration("duration", d),
		slog.String("client_ip", ClientIP(r.RemoteAddr)),
	)
}
