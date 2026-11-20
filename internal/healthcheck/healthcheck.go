// Package healthcheck runs the background probe loop (F7). Stub: no-op.
package healthcheck

import (
	"context"

	"gatewaykit/internal/contract"
)

// Start probes every route with a HealthCheck until ctx is done. It returns immediately.
func Start(ctx context.Context, routes []*contract.Route, deps contract.Deps) {}
