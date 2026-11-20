// Package breaker is the per-route circuit breaker (config spec §7).
package breaker

import (
	"sync"
	"time"

	"gatewaykit/internal/contract"
)

type State int

const (
	Closed State = iota
	Open
	HalfOpen
)

func (s State) String() string {
	switch s {
	case Open:
		return "open"
	case HalfOpen:
		return "half-open"
	}
	return "closed"
}

var _ contract.CircuitBreaker = (*Breaker)(nil)

// Breaker counts failures in a sliding window and admits one trial after cooldown.
type Breaker struct {
	threshold int
	window    time.Duration
	cooldown  time.Duration

	mu       sync.Mutex
	state    State
	failures []time.Time
	openedAt time.Time
	trial    bool
	seq      uint64
	trialID  uint64
}

// New returns the breaker for cfg, or a pass-through when cfg is nil.
func New(cfg *contract.CircuitBreakerConfig) contract.CircuitBreaker {
	if cfg == nil {
		return passThrough{}
	}
	return NewBreaker(cfg)
}

// NewBreaker returns a closed Breaker. cfg must not be nil.
func NewBreaker(cfg *contract.CircuitBreakerConfig) *Breaker {
	th := cfg.Threshold
	if th < 1 {
		th = 1
	}
	return &Breaker{threshold: th, window: cfg.Window, cooldown: cfg.Cooldown}
}

// TryPass reports whether a request may reach the upstream. On rejection
// retryAfter is the whole seconds to wait, at least 1.
func (b *Breaker) TryPass(now time.Time) (bool, int) {
	ok, ra, _ := b.Admit(now)
	return ok, ra
}

// Admit is TryPass that also returns a ticket for Settle and Abandon. Only
// the half-open trial's ticket can decide or release the trial.
func (b *Breaker) Admit(now time.Time) (bool, int, uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.seq++
	switch b.state {
	case Open:
		elapsed := now.Sub(b.openedAt)
		if elapsed < b.cooldown {
			return false, ceilSeconds(b.cooldown - elapsed), 0
		}
		b.state = HalfOpen
		b.trial = true
		b.trialID = b.seq
		return true, 0, b.seq
	case HalfOpen:
		if b.trial {
			return false, 1, 0
		}
		b.trial = true
		b.trialID = b.seq
		return true, 0, b.seq
	}
	return true, 0, b.seq
}

// Record applies one attempt outcome; ok is false for status 500 and above.
// In half-open it is taken as the trial's outcome.
func (b *Breaker) Record(ok bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.record(b.trialID, ok, now)
}

// Settle is Record for the request holding ticket.
func (b *Breaker) Settle(ticket uint64, ok bool, now time.Time) {
	b.mu.Lock()
	defer b.mu.Unlock()
	b.record(ticket, ok, now)
}

func (b *Breaker) record(ticket uint64, ok bool, now time.Time) {
	switch b.state {
	case Closed:
		if ok {
			return
		}
		b.prune(now)
		b.failures = append(b.failures, now)
		if len(b.failures) >= b.threshold {
			b.open(now)
		}
	case HalfOpen:
		if !b.trial || ticket != b.trialID {
			return
		}
		b.trial = false
		if ok {
			b.state = Closed
			b.failures = nil
		} else {
			b.open(now)
		}
	}
}

// Release ends a half-open trial that never reached the upstream, so the
// next request becomes the trial.
func (b *Breaker) Release() {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == HalfOpen {
		b.trial = false
	}
}

// Abandon is Release for the request holding ticket.
func (b *Breaker) Abandon(ticket uint64) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state == HalfOpen && ticket == b.trialID {
		b.trial = false
	}
}

// State returns the current state without advancing it.
func (b *Breaker) State() State {
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.state
}

// Failures returns the failures counted in the window ending at now.
func (b *Breaker) Failures(now time.Time) int {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.state != Closed {
		return 0
	}
	b.prune(now)
	return len(b.failures)
}

func (b *Breaker) open(now time.Time) {
	b.state = Open
	b.openedAt = now
	b.failures = nil
	b.trial = false
}

func (b *Breaker) prune(now time.Time) {
	i := 0
	for i < len(b.failures) && now.Sub(b.failures[i]) > b.window {
		i++
	}
	if i > 0 {
		b.failures = append(b.failures[:0], b.failures[i:]...)
	}
}

func ceilSeconds(d time.Duration) int {
	s := int((d + time.Second - 1) / time.Second)
	if s < 1 {
		s = 1
	}
	return s
}

type passThrough struct{}

func (passThrough) TryPass(time.Time) (bool, int) { return true, 0 }
func (passThrough) Record(bool, time.Time)        {}
func (passThrough) Release()                      {}
