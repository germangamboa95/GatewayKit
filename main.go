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

	"gatewaykit/internal/balance"
	"gatewaykit/internal/breaker"
	"gatewaykit/internal/config"
	"gatewaykit/internal/contract"
	"gatewaykit/internal/healthcheck"
	"gatewaykit/internal/middleware/auth"
	"gatewaykit/internal/middleware/ratelimit"
	"gatewaykit/internal/middleware/reqtransform"
	"gatewaykit/internal/middleware/resptransform"
	"gatewaykit/internal/proxy"
	"gatewaykit/internal/retry"
	"gatewaykit/internal/server"
	"gatewaykit/internal/upstream"
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
	deps := contract.Deps{
		Clock:   contract.SystemClock{},
		Sleeper: contract.SystemClock{},
		Client:  proxy.NewClient(),
		Logger:  logger,
	}
	handler := buildHandler(cfg, deps)
	logRoutes(logger, cfg)

	ln, err := net.Listen("tcp", ":"+strconv.Itoa(cfg.Port))
	if err != nil {
		logger.Error("listen failed", "port", cfg.Port, "err", err)
		return 1
	}
	return serve(ctx, ln, handler, cfg, deps)
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

func serve(ctx context.Context, ln net.Listener, h http.Handler, cfg *contract.GatewayConfig, deps contract.Deps) int {
	hcCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	healthcheck.Start(hcCtx, cfg.Routes, deps)

	srv := &http.Server{Handler: h, ReadHeaderTimeout: 10 * time.Second}
	errc := make(chan error, 1)
	go func() { errc <- srv.Serve(ln) }()
	deps.Logger.Info("listening", "addr", ln.Addr().String(), "routes", len(cfg.Routes))

	select {
	case err := <-errc:
		deps.Logger.Error("server stopped", "err", err)
		return 1
	case <-ctx.Done():
	}
	deps.Logger.Info("shutting down")
	cancel()
	sctx, scancel := context.WithTimeout(context.Background(), shutdownTimeout)
	defer scancel()
	if err := srv.Shutdown(sctx); err != nil {
		deps.Logger.Error("shutdown", "err", err)
		return 1
	}
	return 0
}

// buildHandler assembles every route's chain and returns the root handler.
func buildHandler(cfg *contract.GatewayConfig, deps contract.Deps) *server.Server {
	deps = deps.WithDefaults()
	if deps.Client == nil {
		deps.Client = proxy.NewClient()
	}
	chains := make(map[*contract.Route]contract.Handler, len(cfg.Routes))
	for _, r := range cfg.Routes {
		chains[r] = server.Chain(terminal(r, deps),
			ratelimit.New(r, deps),
			auth.New(r, deps),
			reqtransform.New(r, deps),
			resptransform.New(r, deps),
		)
	}
	return server.New(cfg, chains, deps)
}

func terminal(r *contract.Route, deps contract.Deps) contract.Handler {
	opts := []upstream.Option{
		upstream.WithBalancer(balance.New(r.Balance)),
		upstream.WithPool(balance.NewPool(r)),
	}
	if r.CircuitBreaker != nil {
		opts = append(opts, upstream.WithBreaker(breaker.New(r.CircuitBreaker)))
	}
	if r.Retry != nil {
		opts = append(opts, upstream.WithRetry(retry.New(r.Retry)))
	}
	return upstream.New(r, deps, opts...)
}

// middlewareNames lists the stages active on r, in chain order.
func middlewareNames(r *contract.Route) []string {
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

func logRoutes(logger *slog.Logger, cfg *contract.GatewayConfig) {
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
