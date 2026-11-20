package breaker

import (
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

var t0 = time.Date(2026, 1, 1, 0, 0, 0, 0, time.UTC)

func at(d time.Duration) time.Time { return t0.Add(d) }

func newB(th int, window, cooldown time.Duration) *Breaker {
	return New(&config.CircuitBreaker{Threshold: th, Window: window, Cooldown: cooldown})
}

func mustPass(t *testing.T, b *Breaker, now time.Time) {
	t.Helper()
	if ok, ra := b.TryPass(now); !ok {
		t.Fatalf("rejected with retry_after %d, state %s", ra, b.State())
	}
}

func mustReject(t *testing.T, b *Breaker, now time.Time, want int) {
	t.Helper()
	ok, ra := b.TryPass(now)
	if ok {
		t.Fatalf("passed, state %s", b.State())
	}
	if ra != want {
		t.Fatalf("retry_after %d want %d", ra, want)
	}
}

func trip(t *testing.T, b *Breaker, n int, now time.Time) {
	t.Helper()
	for i := 0; i < n; i++ {
		mustPass(t, b, now)
		b.Record(false, now)
	}
}

func TestClosedToOpenAtThreshold(t *testing.T) {
	b := newB(5, 60*time.Second, 30*time.Second)
	trip(t, b, 4, at(0))
	if b.State() != Closed || b.Failures(at(0)) != 4 {
		t.Fatalf("state %s failures %d", b.State(), b.Failures(at(0)))
	}
	trip(t, b, 1, at(time.Second))
	if b.State() != Open {
		t.Fatalf("state %s", b.State())
	}
	mustReject(t, b, at(time.Second), 30)
}

func TestOldFailuresExpire(t *testing.T) {
	b := newB(5, 60*time.Second, 30*time.Second)
	trip(t, b, 1, at(0))
	trip(t, b, 3, at(10*time.Second))
	trip(t, b, 1, at(61*time.Second))
	if b.State() != Closed {
		t.Fatalf("state %s", b.State())
	}
	if n := b.Failures(at(61 * time.Second)); n != 4 {
		t.Fatalf("failures %d", n)
	}
}

func TestSuccessAndClientErrorsDoNotCount(t *testing.T) {
	b := newB(5, 60*time.Second, 30*time.Second)
	for i := 0; i < 10; i++ {
		mustPass(t, b, at(0))
		b.Record(true, at(0))
	}
	if b.State() != Closed || b.Failures(at(0)) != 0 {
		t.Fatalf("state %s", b.State())
	}
}

func TestOpenRejectsWithRemainingCooldown(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	trip(t, b, 1, at(0))
	mustReject(t, b, at(10500*time.Millisecond), 20)
	mustReject(t, b, at(29800*time.Millisecond), 1)
	mustReject(t, b, at(29999*time.Millisecond), 1)
	if b.State() != Open {
		t.Fatalf("state %s", b.State())
	}
}

func TestOpenToHalfOpenAfterCooldown(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	trip(t, b, 1, at(0))
	mustPass(t, b, at(30*time.Second))
	if b.State() != HalfOpen {
		t.Fatalf("state %s", b.State())
	}
}

func TestHalfOpenSecondRequestRejected(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	trip(t, b, 1, at(0))
	mustPass(t, b, at(30*time.Second))
	mustReject(t, b, at(31*time.Second), 1)
	mustReject(t, b, at(90*time.Second), 1)
}

func TestTrialSucceedsCloses(t *testing.T) {
	b := newB(2, time.Minute, 30*time.Second)
	trip(t, b, 2, at(0))
	mustPass(t, b, at(30*time.Second))
	b.Record(true, at(31*time.Second))
	if b.State() != Closed || b.Failures(at(31*time.Second)) != 0 {
		t.Fatalf("state %s", b.State())
	}
	mustPass(t, b, at(31*time.Second))
	trip(t, b, 1, at(32*time.Second))
	if b.State() != Closed {
		t.Fatal("count was not reset")
	}
}

func TestTrialFailsReopensWithFullCooldown(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	trip(t, b, 1, at(0))
	mustPass(t, b, at(40*time.Second))
	b.Record(false, at(41*time.Second))
	if b.State() != Open {
		t.Fatalf("state %s", b.State())
	}
	mustReject(t, b, at(41*time.Second), 30)
	mustReject(t, b, at(70*time.Second), 1)
	mustPass(t, b, at(71*time.Second))
}

func TestReleasedTrialAdmitsNext(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	trip(t, b, 1, at(0))
	mustPass(t, b, at(30*time.Second))
	b.Release()
	if b.State() != HalfOpen {
		t.Fatalf("state %s", b.State())
	}
	mustPass(t, b, at(31*time.Second))
	mustReject(t, b, at(31*time.Second), 1)
	b.Record(true, at(32*time.Second))
	if b.State() != Closed {
		t.Fatalf("state %s", b.State())
	}
}

func TestReleaseOutsideHalfOpenIsNoop(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	b.Release()
	mustPass(t, b, at(0))
	b.Record(false, at(0))
	b.Release()
	mustReject(t, b, at(time.Second), 29)
}

func TestRecordWhileOpenIgnored(t *testing.T) {
	b := newB(1, time.Minute, 30*time.Second)
	trip(t, b, 1, at(0))
	b.Record(true, at(5*time.Second))
	b.Record(false, at(5*time.Second))
	mustReject(t, b, at(10*time.Second), 20)
}

func TestCeilSeconds(t *testing.T) {
	cases := map[time.Duration]int{
		0:                        1,
		200 * time.Millisecond:   1,
		time.Second:              1,
		1001 * time.Millisecond:  2,
		19500 * time.Millisecond: 20,
	}
	for d, want := range cases {
		if got := ceilSeconds(d); got != want {
			t.Errorf("ceilSeconds(%v) = %d want %d", d, got, want)
		}
	}
}

func TestHalfOpenAdmitsExactlyOneConcurrently(t *testing.T) {
	b := newB(1, time.Minute, time.Second)
	trip(t, b, 1, at(0))
	var passed atomic.Int64
	var wg sync.WaitGroup
	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			if ok, _ := b.TryPass(at(2 * time.Second)); ok {
				passed.Add(1)
			}
		}()
	}
	wg.Wait()
	if passed.Load() != 1 {
		t.Fatalf("%d trials admitted", passed.Load())
	}
}

func TestLateResultDoesNotDecideTrial(t *testing.T) {
	b := newB(1, time.Minute, time.Second)
	okA, _, a := b.Admit(at(0))
	if !okA {
		t.Fatal("A rejected")
	}
	b.Record(false, at(0))
	okC, _, c := b.Admit(at(time.Second))
	if !okC || b.State() != HalfOpen {
		t.Fatalf("trial not admitted, state %s", b.State())
	}
	b.Settle(a, true, at(1500*time.Millisecond))
	if b.State() != HalfOpen {
		t.Fatalf("late result changed state to %s", b.State())
	}
	mustReject(t, b, at(1500*time.Millisecond), 1)
	b.Abandon(a)
	mustReject(t, b, at(1600*time.Millisecond), 1)
	b.Settle(c, true, at(2*time.Second))
	if b.State() != Closed {
		t.Fatalf("trial result ignored, state %s", b.State())
	}
}

func TestAbandonTrialAdmitsNext(t *testing.T) {
	b := newB(1, time.Minute, time.Second)
	b.Record(false, at(0))
	_, _, c := b.Admit(at(time.Second))
	b.Abandon(c)
	if ok, _, _ := b.Admit(at(time.Second)); !ok {
		t.Fatal("next request not admitted as trial")
	}
}
