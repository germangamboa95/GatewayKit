// Package healthcheck runs the background loop that probes each route's
// targets and takes failing ones out of rotation.
package healthcheck

import (
	"context"
	"io"
	"log/slog"
	"net/http"
	"net/url"
	"strings"
	"sync"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/clock"
	"github.com/germangamboa95/gatewaykit/internal/config"
)

const (
	defaultInterval  = 30 * time.Second
	defaultThreshold = 3
)

var client = &http.Client{
	Transport: &http.Transport{
		DisableCompression:  true,
		MaxIdleConnsPerHost: 4,
		IdleConnTimeout:     90 * time.Second,
	},
	CheckRedirect: func(*http.Request, []*http.Request) error { return http.ErrUseLastResponse },
}

// Start probes every route with a HealthCheck until ctx is done. It returns
// immediately. A nil clk or sleeper means the system clock; a nil logger
// means slog.Default().
func Start(ctx context.Context, routes []*config.Route, clk clock.Clock, sleeper clock.Sleeper, logger *slog.Logger) {
	if clk == nil {
		clk = clock.System{}
	}
	if sleeper == nil {
		sleeper = clock.System{}
	}
	if logger == nil {
		logger = slog.Default()
	}
	for _, r := range routes {
		if r == nil || r.HealthCheck == nil || len(r.Targets) == 0 {
			continue
		}
		go run(ctx, r, clk, sleeper, logger)
	}
}

func run(ctx context.Context, r *config.Route, clk clock.Clock, sleeper clock.Sleeper, logger *slog.Logger) {
	interval := r.HealthCheck.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	for {
		start := clk.Now()
		round(ctx, r, logger)
		if ctx.Err() != nil {
			return
		}
		wait := interval - clk.Now().Sub(start)
		if wait < 0 {
			wait = 0
		}
		if err := sleeper.Sleep(ctx, wait); err != nil || ctx.Err() != nil {
			return
		}
	}
}

func round(ctx context.Context, r *config.Route, log *slog.Logger) {
	hc := r.HealthCheck
	timeout := probeTimeout(hc.Interval, r.Timeout)
	threshold := hc.UnhealthyThreshold
	if threshold < 1 {
		threshold = defaultThreshold
	}
	var wg sync.WaitGroup
	for _, t := range r.Targets {
		if t == nil || t.URL == nil {
			continue
		}
		wg.Add(1)
		go func(t *config.Target) {
			defer wg.Done()
			u := probeURL(t.URL, hc.Path)
			ok := probe(ctx, u, timeout)
			if ctx.Err() != nil {
				return
			}
			before := t.Healthy()
			after := t.RecordProbe(ok, threshold)
			switch {
			case before && !after:
				log.Warn("target unhealthy", "route", r.Path, "target", t.URL.String(), "failures", t.Failures())
			case !before && after:
				log.Info("target recovered", "route", r.Path, "target", t.URL.String())
			}
		}(t)
	}
	wg.Wait()
}

// probeTimeout is min(interval, upstream timeout); a zero value is ignored.
func probeTimeout(interval, upstream time.Duration) time.Duration {
	if interval <= 0 {
		interval = defaultInterval
	}
	if upstream > 0 && upstream < interval {
		return upstream
	}
	return interval
}

func probeURL(base *url.URL, path string) string {
	u := *base
	p, q, hasQ := strings.Cut(path, "?")
	u.Path = strings.TrimRight(base.Path, "/") + "/" + strings.TrimLeft(p, "/")
	u.RawPath = ""
	if hasQ {
		u.RawQuery = q
	}
	u.Fragment = ""
	return u.String()
}

// probe sends one GET and reports whether it got a 2xx within timeout.
func probe(ctx context.Context, u string, timeout time.Duration) bool {
	ctx, cancel := context.WithTimeout(ctx, timeout)
	defer cancel()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, u, nil)
	if err != nil {
		return false
	}
	resp, err := client.Do(req)
	if err != nil {
		return false
	}
	defer resp.Body.Close()
	if _, err := io.Copy(io.Discard, io.LimitReader(resp.Body, 64<<10)); err != nil {
		return false
	}
	return resp.StatusCode >= 200 && resp.StatusCode < 300
}
