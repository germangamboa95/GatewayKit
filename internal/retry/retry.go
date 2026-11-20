// Package retry decides whether to retry an upstream attempt and how long to wait.
package retry

import (
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// Policy decides retries for one route. The zero value never retries.
type Policy struct {
	attempts    int
	exponential bool
	initial     time.Duration
	on          map[int]bool
}

// New returns the policy for cfg. A nil cfg makes exactly one attempt.
func New(cfg *config.Retry) *Policy {
	if cfg == nil {
		return &Policy{attempts: 1}
	}
	return &Policy{
		attempts:    cfg.Attempts,
		exponential: cfg.Backoff == config.BackoffExponential,
		initial:     cfg.InitialDelay,
		on:          cfg.On,
	}
}

// Attempts is the total number of tries allowed, at least 1.
func (p *Policy) Attempts() int {
	if p.attempts < 1 {
		return 1
	}
	return p.attempts
}

// ShouldRetry reports whether attempt (from 1) that ended with status may be
// followed by another. Use 502 for connection errors and 504 for timeouts.
func (p *Policy) ShouldRetry(status, attempt int) bool {
	return attempt >= 1 && attempt < p.Attempts() && p.on[status]
}

// Delay is the wait after attempt (from 1) fails, before attempt+1:
// initial_delay for fixed, initial_delay*2^(attempt-1) for exponential.
func (p *Policy) Delay(attempt int) time.Duration {
	if attempt < 1 || p.initial <= 0 {
		return 0
	}
	if !p.exponential {
		return p.initial
	}
	d := p.initial
	for i := 1; i < attempt; i++ {
		if d > time.Duration(1<<62)/2 {
			return time.Duration(1<<63 - 1)
		}
		d *= 2
	}
	return d
}
