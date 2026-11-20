package balance

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

func target(t *testing.T, url string, w int) *config.Target {
	t.Helper()
	tg, err := config.NewTarget(url, w)
	if err != nil {
		t.Fatal(err)
	}
	return tg
}

func route(targets []*config.Target, hc bool) *config.Route {
	r := &config.Route{Targets: targets}
	if hc {
		r.HealthCheck = &config.HealthCheck{Path: "/healthz", Interval: time.Second, UnhealthyThreshold: 1}
	}
	return r
}

func names(t *testing.T, b *Balancer, p TargetPool, n int, m map[*config.Target]string) []string {
	t.Helper()
	var out []string
	for i := 0; i < n; i++ {
		tg, ok := b.Pick(p)
		if !ok {
			t.Fatalf("pick %d: no target", i)
		}
		out = append(out, m[tg])
	}
	return out
}

func eq(t *testing.T, got []string, want ...string) {
	t.Helper()
	if len(got) != len(want) {
		t.Fatalf("got %v want %v", got, want)
	}
	for i := range got {
		if got[i] != want[i] {
			t.Fatalf("got %v want %v", got, want)
		}
	}
}

func TestWeighted3to1(t *testing.T) {
	a, b := target(t, "http://a", 3), target(t, "http://b", 1)
	m := map[*config.Target]string{a: "A", b: "B"}
	bal := New(config.BalanceWeightedRoundRobin)
	p := NewPool(route([]*config.Target{a, b}, false))
	got := names(t, bal, p, 40, m)
	eq(t, got[:8], "A", "A", "B", "A", "A", "A", "B", "A")
	for i := 0; i+4 <= len(got); i++ {
		n := 0
		for _, s := range got[i : i+4] {
			if s == "A" {
				n++
			}
		}
		if n != 3 {
			t.Fatalf("window %d: %v", i, got[i:i+4])
		}
	}
}

func TestRoundRobinIgnoresWeights(t *testing.T) {
	a, b := target(t, "http://a", 3), target(t, "http://b", 1)
	m := map[*config.Target]string{a: "A", b: "B"}
	p := NewPool(route([]*config.Target{a, b}, false))
	eq(t, names(t, New(config.BalanceRoundRobin), p, 4, m), "A", "B", "A", "B")
}

func TestRoundRobinSkipsUnhealthy(t *testing.T) {
	a, b, c := target(t, "http://a", 1), target(t, "http://b", 1), target(t, "http://c", 1)
	m := map[*config.Target]string{a: "A", b: "B", c: "C"}
	p := NewPool(route([]*config.Target{a, b, c}, true))
	bal := New(config.BalanceRoundRobin)
	eq(t, names(t, bal, p, 1, m), "A")
	p.MarkProbe(b, false)
	eq(t, names(t, bal, p, 4, m), "C", "A", "C", "A")
	p.MarkProbe(b, true)
	eq(t, names(t, bal, p, 3, m), "B", "C", "A")
}

func TestWeightedSkipsUnhealthy(t *testing.T) {
	a, b := target(t, "http://a", 3), target(t, "http://b", 1)
	m := map[*config.Target]string{a: "A", b: "B"}
	p := NewPool(route([]*config.Target{a, b}, true))
	bal := New(config.BalanceWeightedRoundRobin)
	p.MarkProbe(a, false)
	eq(t, names(t, bal, p, 3, m), "B", "B", "B")
	p.MarkProbe(a, true)
	eq(t, names(t, bal, p, 4, m), "A", "A", "B", "A")
}

func TestNoHealthyTarget(t *testing.T) {
	a, b := target(t, "http://a", 1), target(t, "http://b", 1)
	p := NewPool(route([]*config.Target{a, b}, true))
	p.MarkProbe(a, false)
	p.MarkProbe(b, false)
	for _, kind := range []config.Balance{config.BalanceRoundRobin, config.BalanceWeightedRoundRobin} {
		if tg, ok := New(kind).Pick(p); ok || tg != nil {
			t.Fatalf("%s: picked %v", kind, tg)
		}
	}
	if _, ok := New("").Pick(NewPool(route(nil, false))); ok {
		t.Fatal("empty pool picked a target")
	}
}

func TestSeparatePools(t *testing.T) {
	a1, b1 := target(t, "http://a", 1), target(t, "http://b", 1)
	a2, b2 := target(t, "http://a", 1), target(t, "http://b", 1)
	m := map[*config.Target]string{a1: "A", b1: "B", a2: "A", b2: "B"}
	p1 := NewPool(route([]*config.Target{a1, b1}, false))
	p2 := NewPool(route([]*config.Target{a2, b2}, false))

	shared := New(config.BalanceRoundRobin)
	eq(t, names(t, shared, p1, 1, m), "A")
	eq(t, names(t, shared, p2, 1, m), "A")

	r1, r2 := New(config.BalanceWeightedRoundRobin), New(config.BalanceWeightedRoundRobin)
	eq(t, names(t, r1, p1, 1, m), "A")
	eq(t, names(t, r2, p2, 1, m), "A")
}

func TestPoolWithoutHealthCheckAlwaysHealthy(t *testing.T) {
	a := target(t, "http://a", 1)
	a.SetHealthy(false)
	p := NewPool(route([]*config.Target{a}, false))
	if !p.Healthy(a) {
		t.Fatal("target should count as healthy without a health check")
	}
}

func TestPoolThreshold(t *testing.T) {
	a := target(t, "http://a", 1)
	r := route([]*config.Target{a}, true)
	r.HealthCheck.UnhealthyThreshold = 3
	p := NewPool(r)
	p.MarkProbe(a, false)
	p.MarkProbe(a, false)
	if !p.Healthy(a) {
		t.Fatal("unhealthy after 2 failures")
	}
	p.MarkProbe(a, false)
	if p.Healthy(a) {
		t.Fatal("healthy after 3 failures")
	}
	p.MarkProbe(a, true)
	if !p.Healthy(a) {
		t.Fatal("not recovered")
	}
}

func TestConcurrentPicksKeepRatio(t *testing.T) {
	a, b := target(t, "http://a", 3), target(t, "http://b", 1)
	p := NewPool(route([]*config.Target{a, b}, false))
	bal := New(config.BalanceWeightedRoundRobin)
	var na, nb atomic.Int64
	var wg sync.WaitGroup
	for g := 0; g < 8; g++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			for i := 0; i < 100; i++ {
				tg, _ := bal.Pick(p)
				if tg == a {
					na.Add(1)
				} else {
					nb.Add(1)
				}
			}
		}()
	}
	wg.Wait()
	if na.Load() != 600 || nb.Load() != 200 {
		t.Fatalf("a=%d b=%d", na.Load(), nb.Load())
	}
}
