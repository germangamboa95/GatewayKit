package config

import (
	"fmt"
	"net/url"
	"sync/atomic"
	"time"
)

// Balance is a load-balancing strategy.
type Balance string

const (
	BalanceRoundRobin         Balance = "round_robin"
	BalanceWeightedRoundRobin Balance = "weighted_round_robin"
)

// Strategy is a rate-limit window strategy.
type Strategy string

const (
	StrategyFixedWindow   Strategy = "fixed_window"
	StrategySlidingWindow Strategy = "sliding_window"
)

// Per is what a rate limit counts against.
type Per string

const (
	PerIP     Per = "ip"
	PerGlobal Per = "global"
)

// Backoff is a retry backoff strategy.
type Backoff string

const (
	BackoffFixed       Backoff = "fixed"
	BackoffExponential Backoff = "exponential"
)

// AuthType is an authentication scheme.
type AuthType string

const AuthTypeAPIKey AuthType = "api_key"

// DefaultMethods is the method list a route gets when it sets none, in Allow order.
var DefaultMethods = []string{"GET", "POST", "PUT", "PATCH", "DELETE", "HEAD", "OPTIONS"}

// Gateway is the normalized config (config spec §10).
type Gateway struct {
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
	Balance Balance
	Timeout time.Duration

	Retry          *Retry
	HealthCheck    *HealthCheck
	RateLimit      *RateLimit
	Auth           *Auth
	CircuitBreaker *CircuitBreaker
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

// Healthy reports whether the target is in rotation.
func (t *Target) Healthy() bool { return !t.unhealthy.Load() }

// SetHealthy sets the health state; marking a target healthy resets its failure count.
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

// Retry re-sends failed requests. Attempts is the total number of tries, including the first.
type Retry struct {
	Attempts     int
	Backoff      Backoff
	InitialDelay time.Duration
	On           map[int]bool
}

// HealthCheck probes Path every Interval; UnhealthyThreshold consecutive failures take a target out.
type HealthCheck struct {
	Path               string
	Interval           time.Duration
	UnhealthyThreshold int
}

// RateLimit allows Requests per Window, counted per client IP or globally.
type RateLimit struct {
	Requests int
	Window   time.Duration
	Strategy Strategy
	Per      Per
}

// Auth is API-key authentication. Header is lowercase; Keys keep config order.
type Auth struct {
	Type   AuthType
	Header string
	Keys   []string
}

// CircuitBreaker opens after Threshold failures within Window and stays open for Cooldown.
type CircuitBreaker struct {
	Threshold int
	Window    time.Duration
	Cooldown  time.Duration
}
