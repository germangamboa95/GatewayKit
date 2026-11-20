// Package contract holds the shared types every GatewayKit track codes against.
package contract

import (
	"fmt"
	"net/url"
	"sync/atomic"
	"time"
)

const (
	BalanceRoundRobin         = "round_robin"
	BalanceWeightedRoundRobin = "weighted_round_robin"

	StrategyFixedWindow   = "fixed_window"
	StrategySlidingWindow = "sliding_window"

	PerIP     = "ip"
	PerGlobal = "global"

	BackoffFixed       = "fixed"
	BackoffExponential = "exponential"

	AuthTypeAPIKey = "api_key"
)

// DefaultMethods is the method list a route gets when it sets none, in Allow order.
var DefaultMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// GatewayConfig is the normalized model (config spec §10).
type GatewayConfig struct {
	Port   int
	Routes []*Route
}

// Route is one normalized route. Optional features are nil when absent.
// Methods and MethodList hold uppercase names; MethodList keeps config order.
type Route struct {
	Path        string
	Methods     map[string]bool
	MethodList  []string
	StripPrefix bool

	Targets []*Target
	Balance string
	Timeout time.Duration

	Retry          *Retry
	HealthCheck    *HealthCheck
	RateLimit      *RateLimit
	Auth           *Auth
	CircuitBreaker *CircuitBreakerConfig
	ReqTransform   *ReqTransform
	RespTransform  *RespTransform
}

// Target is one upstream target. Health state is shared by the balancer
// (reads) and the health-check loop (writes). Targets start healthy.
type Target struct {
	URL    *url.URL
	Weight int

	unhealthy atomic.Bool
	failures  atomic.Int64
}

// NewTarget parses rawURL and returns a healthy target.
func NewTarget(rawURL string, weight int) (*Target, error) {
	u, err := url.Parse(rawURL)
	if err != nil {
		return nil, err
	}
	if u.Scheme == "" || u.Host == "" {
		return nil, fmt.Errorf("target url %q needs a scheme and host", rawURL)
	}
	return &Target{URL: u, Weight: weight}, nil
}

func (t *Target) Healthy() bool { return !t.unhealthy.Load() }

func (t *Target) SetHealthy(ok bool) {
	t.unhealthy.Store(!ok)
	if ok {
		t.failures.Store(0)
	}
}

// Failures returns the current count of consecutive failed probes.
func (t *Target) Failures() int { return int(t.failures.Load()) }

// RecordProbe applies one probe result: a success marks the target healthy
// and resets the count; threshold consecutive failures mark it unhealthy.
// It returns the health state after the probe.
func (t *Target) RecordProbe(ok bool, threshold int) bool {
	if ok {
		t.SetHealthy(true)
		return true
	}
	if n := t.failures.Add(1); threshold > 0 && n >= int64(threshold) {
		t.unhealthy.Store(true)
	}
	return t.Healthy()
}

// Retry: Attempts is the total number of tries, including the first.
type Retry struct {
	Attempts     int
	Backoff      string
	InitialDelay time.Duration
	On           map[int]bool
}

type HealthCheck struct {
	Path               string
	Interval           time.Duration
	UnhealthyThreshold int
}

type RateLimit struct {
	Requests int
	Window   time.Duration
	Strategy string
	Per      string
}

// Auth: Header is lowercase; Keys keep config order.
type Auth struct {
	Type   string
	Header string
	Keys   []string
}

type CircuitBreakerConfig struct {
	Threshold int
	Window    time.Duration
	Cooldown  time.Duration
}
