// Package balance picks upstream targets by round robin or smooth weighted
// round robin over the healthy subset of a pool.
package balance

import (
	"sync"

	"gatewaykit/internal/contract"
)

var _ contract.Balancer = (*Balancer)(nil)

// Balancer keeps rotation state per target, so pools with distinct targets
// rotate independently even when they share one Balancer.
type Balancer struct {
	weighted bool

	mu    sync.Mutex
	seq   uint64
	state map[*contract.Target]*targetState
}

type targetState struct {
	current int
	lastSeq uint64
	healthy bool
}

// New returns a Balancer for kind (contract.BalanceRoundRobin or
// contract.BalanceWeightedRoundRobin). Any other value means round robin.
func New(kind string) *Balancer {
	return &Balancer{
		weighted: kind == contract.BalanceWeightedRoundRobin,
		state:    map[*contract.Target]*targetState{},
	}
}

// Pick returns the next healthy target, or false when none is healthy.
func (b *Balancer) Pick(pool contract.TargetPool) (*contract.Target, bool) {
	if pool == nil {
		return nil, false
	}
	targets := pool.Targets()
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.weighted {
		return b.pickWeighted(pool, targets)
	}
	return b.pickRoundRobin(pool, targets)
}

func (b *Balancer) get(t *contract.Target) *targetState {
	s, ok := b.state[t]
	if !ok {
		s = &targetState{healthy: true}
		b.state[t] = s
	}
	return s
}

func (b *Balancer) pickRoundRobin(pool contract.TargetPool, targets []*contract.Target) (*contract.Target, bool) {
	n := len(targets)
	start := 0
	var last uint64
	for i, t := range targets {
		if t == nil {
			continue
		}
		if s := b.get(t); s.lastSeq > last {
			last = s.lastSeq
			start = i + 1
		}
	}
	for k := 0; k < n; k++ {
		t := targets[(start+k)%n]
		if t != nil && pool.Healthy(t) {
			b.seq++
			b.get(t).lastSeq = b.seq
			return t, true
		}
	}
	return nil, false
}

func (b *Balancer) pickWeighted(pool contract.TargetPool, targets []*contract.Target) (*contract.Target, bool) {
	healthy := make([]bool, len(targets))
	changed := false
	for i, t := range targets {
		if t == nil {
			continue
		}
		healthy[i] = pool.Healthy(t)
		if s := b.get(t); s.healthy != healthy[i] {
			changed = true
			s.healthy = healthy[i]
		}
	}
	if changed {
		for _, t := range targets {
			if t != nil {
				b.get(t).current = 0
			}
		}
	}
	var best *targetState
	var bestT *contract.Target
	total := 0
	for i, t := range targets {
		if t == nil || !healthy[i] {
			continue
		}
		w := t.Weight
		if w < 1 {
			w = 1
		}
		total += w
		s := b.get(t)
		s.current += w
		if best == nil || s.current > best.current {
			best, bestT = s, t
		}
	}
	if best == nil {
		return nil, false
	}
	best.current -= total
	return bestT, true
}
