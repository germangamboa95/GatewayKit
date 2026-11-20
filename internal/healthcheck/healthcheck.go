// Package healthcheck runs the background probe loop (F7).
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

	"gatewaykit/internal/contract"
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

// Start probes every route with a HealthCheck until ctx is done. It returns immediately.
func Start(ctx context.Context, routes []*contract.Route, deps contract.Deps) {
	deps = deps.WithDefaults()
	for _, r := range routes {
		if r == nil || r.HealthCheck == nil || len(r.Targets) == 0 {
			continue
		}
		go run(ctx, r, deps)
	}
}

func run(ctx context.Context, r *contract.Route, deps contract.Deps) {
	interval := r.HealthCheck.Interval
	if interval <= 0 {
		interval = defaultInterval
	}
	for {
		start := deps.Clock.Now()
		round(ctx, r, deps.Logger)
		if ctx.Err() != nil {
			return
		}
		wait := interval - deps.Clock.Now().Sub(start)
		if wait < 0 {
			wait = 0
		}
		if err := deps.Sleeper.Sleep(ctx, wait); err != nil || ctx.Err() != nil {
			return
		}
	}
}

func round(ctx context.Context, r *contract.Route, log *slog.Logger) {
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
		go func(t *contract.Target) {
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
