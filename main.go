// Command gatewaykit runs the API gateway described by a YAML config file.
package main

import (
	"context"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"net"
	"net/http"
	"os"
	"os/signal"
	"strconv"
	"strings"
	"syscall"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/auth"
	"github.com/germangamboa95/gatewaykit/internal/balance"
	"github.com/germangamboa95/gatewaykit/internal/breaker"
	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
	"github.com/germangamboa95/gatewaykit/internal/healthcheck"
	"github.com/germangamboa95/gatewaykit/internal/proxy"
	"github.com/germangamboa95/gatewaykit/internal/ratelimit"
	"github.com/germangamboa95/gatewaykit/internal/retry"
	"github.com/germangamboa95/gatewaykit/internal/server"
	"github.com/germangamboa95/gatewaykit/internal/transform"
	"github.com/germangamboa95/gatewaykit/internal/upstream"
)

const shutdownTimeout = 10 * time.Second

func main() {
	ctx, stop := signal.NotifyContext(context.Background(), os.Interrupt, syscall.SIGTERM)
	defer stop()
	os.Exit(run(ctx, os.Args[1:], os.Getenv, os.Stderr))
}

// run loads the config, serves until ctx is done, and returns the exit code.
func run(ctx context.Context, args []string, getenv func(string) string, stderr io.Writer) int {
	path, err := config.ResolvePath(args, getenv)
	if err != nil {
		fmt.Fprintf(stderr, "config error: %v\n", err)
		return 1
	}
	cfg, warnings, err := config.Load(path)
	for _, w := range warnings {
		fmt.Fprintf(stderr, "warning: %s\n", w)
	}
	if err != nil {
		printLoadError(stderr, err)
		return 1
	}

	logger := slog.New(slog.NewTextHandler(stderr, nil))
	handler := buildHandler(cfg, clock.System{}, logger)
	logRoutes(logger, cfg)

	ln, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		logger.Error("listen failed", "port", cfg.Port, "err", err)
		return 1
	}
	return serve(ctx, ln, handler, cfg, logger)
}

func printLoadError(w io.Writer, err error) {
	var le *config.LoadError
	if errors.As(err, &le) {
		fmt.Fprintln(w, le.Summary())
		for _, p := range le.Problems {
			fmt.Fprintf(w, "  %s\n", p)
		}
		return
	}
	fmt.Fprintf(w, "config error: %v\n", err)
}

func serve(ctx context.Context, ln net.Listener, h http.Handler, cfg *config.Gateway, logger *slog.Logger) int {
	hcCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	healthcheck.Start(hcCtx, cfg.Routes, clock.System{}, clock.System{}, logger)

	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	logger.Info("listening", "addr", ln.Addr().String(), "routes", len(cfg.Routes))

	select {
	case err := <-errc:
		logger.Error("server stopped", "err", err)
		return 1
	case <-ctx.Done():
	}
	logger.Info("shutting down")
	cancel()
	sctx, scancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer scancel()
	if err := srv.Shutdown(sctx); err != nil {
		logger.Error("shutdown", "err", err)
		return 1
	}
	return 0
}

// buildHandler assembles every route's chain and returns the root handler.
// sleeper waits between retries; tests pass a fake one.
func buildHandler(cfg *config.Gateway, sleeper clock.Sleeper, logger *slog.Logger) *server.Server {
	clk := clock.System{}
	client := proxy.NewClient()
	chains := make(map[*config.Route]gateway.Handler, len(cfg.Routes))
	for _, r := range cfg.Routes {
		chains[r] = gateway.Chain(terminal(r, client, clk, sleeper),
			ratelimit.Middleware(r.RateLimit, clk),
			auth.Middleware(r.Auth),
			transform.RequestMiddleware(r, clk),
			transform.ResponseMiddleware(r, clk),
		)
	}
	return server.New(cfg, chains, clk, logger)
}

func terminal(r *config.Route, client proxy.Sender, clk clock.Clock, sleeper clock.Sleeper) gateway.Handler {
	opts := []upstream.Option{
		upstream.WithClock(clk),
		upstream.WithSleeper(sleeper),
		upstream.WithBalancer(balance.New(r.Balance)),
		upstream.WithPool(balance.NewPool(r)),
	}
	if r.CircuitBreaker != nil {
		opts = append(opts, upstream.WithBreaker(breaker.New(r.CircuitBreaker)))
	}
	if r.Retry != nil {
		opts = append(opts, upstream.WithRetry(retry.New(r.Retry)))
	}
	return upstream.New(r, client, opts...)
}

// middlewareNames lists the stages active on r, in chain order.
func middlewareNames(r *config.Route) []string {
	var names []string
	if r.RateLimit != nil {
		names = append(names, "rate_limit")
	}
	if r.Auth != nil {
		names = append(names, "auth")
	}
	if r.ReqTransform != nil {
		names = append(names, "request_transform")
	}
	if r.RespTransform != nil {
		names = append(names, "response_transform")
	}
	if r.CircuitBreaker != nil {
		names = append(names, "circuit_breaker")
	}
	if r.Retry != nil {
		names = append(names, "retry")
	}
	if r.HealthCheck != nil {
		names = append(names, "health_check")
	}
	return names
}

func logRoutes(logger *slog.Logger, cfg *config.Gateway) {
	for _, r := range cfg.Routes {
		targets := make([]string, len(r.Targets))
		for i, t := range r.Targets {
			targets[i] = t.URL.String()
		}
		mw := middlewareNames(r)
		if len(mw) == 0 {
			mw = []string{"none"}
		}
		logger.Info("route",
			"path", r.Path,
			"methods", strings.Join(r.MethodList, ","),
			"targets", strings.Join(targets, ","),
			"balance", r.Balance,
			"timeout", r.Timeout,
			"middleware", strings.Join(mw, ","),
		)
	}
}
