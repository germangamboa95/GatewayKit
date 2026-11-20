package ratelimit

import (
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

func cfg(n int, w time.Duration, strategy, per string) *contract.RateLimit {
	return &contract.RateLimit{Requests: n, Window: w, Strategy: strategy, Per: per}
}

func at(sec int64, ms ...int64) time.Time {
	t := time.Unix(sec, 0)
	if len(ms) > 0 {
		t = t.Add(time.Duration(ms[0]) * time.Millisecond)
	}
	return t
}

func TestNewNil(t *testing.T) {
	if New(nil) != nil {
		t.Fatal("nil config should give nil limiter")
	}
	if New(cfg(0, time.Second, "", "")) != nil {
		t.Fatal("zero requests should give nil limiter")
	}
}

func TestFixedWindowResetsAtBoundary(t *testing.T) {
	l := New(cfg(10, 10*time.Second, contract.StrategyFixedWindow, contract.PerIP))
	base := int64(1_700_000_000) // multiple of 10
	for i := range 10 {
		d := l.Allow("a", at(base+int64(i)))
		if !d.Allowed || d.Remaining != 9-i || d.Limit != 10 {
			t.Fatalf("req %d: %+v", i, d)
		}
		if d.Reset != base+10 {
			t.Fatalf("reset %d, want %d", d.Reset, base+10)
		}
	}
	d := l.Allow("a", at(base+9))
	if d.Allowed || d.Remaining != 0 || d.RetryAfter != 1 || d.Reset != base+10 {
		t.Fatalf("11th: %+v", d)
	}
	d = l.Allow("a", at(base+10))
	if !d.Allowed || d.Remaining != 9 || d.Reset != base+20 {
		t.Fatalf("next window: %+v", d)
	}
}

func TestFixedWindowResetValue(t *testing.T) {
	l := New(cfg(5, 10*time.Second, "", ""))
	d := l.Allow("a", at(1000003))
	if d.Reset != 1000010 {
		t.Fatalf("reset %d, want 1000010", d.Reset)
	}
}

func TestFixedRetryAfter(t *testing.T) {
	l := New(cfg(1, 10*time.Second, contract.StrategyFixedWindow, ""))
	l.Allow("a", at(1000000))
	d := l.Allow("a", at(1000002, 500))
	if d.Allowed || d.RetryAfter != 8 {
		t.Fatalf("got %+v, want retry 8", d)
	}
}

func TestRejectedDoNotExtendBlock(t *testing.T) {
	l := New(cfg(2, 10*time.Second, contract.StrategyFixedWindow, ""))
	for i := range 2 {
		l.Allow("a", at(1000000+int64(i)))
	}
	for i := range 20 {
		if l.Allow("a", at(1000002, int64(i*300))).Allowed {
			t.Fatal("should be rejected")
		}
	}
	if !l.Allow("a", at(1000010)).Allowed {
		t.Fatal("first request in next window should pass")
	}
}

func TestSlidingWindow(t *testing.T) {
	l := New(cfg(30, 60*time.Second, contract.StrategySlidingWindow, contract.PerIP))
	base := int64(1_700_000_000)
	for i := range 30 {
		d := l.Allow("a", at(base))
		if !d.Allowed || d.Remaining != 29-i || d.Reset != base+60 {
			t.Fatalf("req %d: %+v", i, d)
		}
	}
	d := l.Allow("a", at(base+59))
	if d.Allowed || d.RetryAfter != 1 || d.Remaining != 0 || d.Reset != base+60 {
		t.Fatalf("at +59: %+v", d)
	}
	d = l.Allow("a", at(base+60))
	if !d.Allowed || d.Remaining != 29 || d.Reset != base+120 {
		t.Fatalf("at +60: %+v", d)
	}
}

func TestSlidingResetIsOldest(t *testing.T) {
	l := New(cfg(3, 10*time.Second, contract.StrategySlidingWindow, ""))
	l.Allow("a", at(100))
	l.Allow("a", at(104))
	d := l.Allow("a", at(107))
	if d.Reset != 110 || d.Remaining != 0 {
		t.Fatalf("%+v", d)
	}
	d = l.Allow("a", at(108))
	if d.Allowed || d.RetryAfter != 2 {
		t.Fatalf("%+v", d)
	}
	d = l.Allow("a", at(110))
	if !d.Allowed || d.Reset != 114 || d.Remaining != 0 {
		t.Fatalf("%+v", d)
	}
}

func TestSlidingMemoryBounded(t *testing.T) {
	l := New(cfg(5, time.Minute, contract.StrategySlidingWindow, ""))
	for i := range 100 {
		l.Allow("a", at(1000, int64(i)))
	}
	b := l.buckets["a"]
	if len(b.buf) > 5 || b.n != 5 {
		t.Fatalf("buf len %d n %d", len(b.buf), b.n)
	}
	big := New(cfg(1000, time.Minute, contract.StrategySlidingWindow, ""))
	big.Allow("a", at(1000))
	if n := len(big.buckets["a"].buf); n > 8 {
		t.Fatalf("eager allocation: %d", n)
	}
}

func TestPerIPBuckets(t *testing.T) {
	l := New(cfg(2, time.Second, "", contract.PerIP))
	for _, ip := range []string{"1.1.1.1", "2.2.2.2", "1.1.1.1", "2.2.2.2"} {
		if !l.Allow(l.Key(ip), at(1000)).Allowed {
			t.Fatalf("%s rejected", ip)
		}
	}
}

func TestGlobalBucket(t *testing.T) {
	l := New(cfg(2, time.Second, "", contract.PerGlobal))
	l.Allow(l.Key("1.1.1.1"), at(1000))
	l.Allow(l.Key("2.2.2.2"), at(1000))
	if l.Allow(l.Key("1.1.1.1"), at(1000)).Allowed {
		t.Fatal("third request should be rejected")
	}
}

func TestSeparateLimiters(t *testing.T) {
	c := cfg(100, time.Minute, "", "")
	a, b := New(c), New(c)
	for range 100 {
		a.Allow("x", at(1000))
	}
	if a.Allow("x", at(1000)).Allowed {
		t.Fatal("route a should be exhausted")
	}
	if !b.Allow("x", at(1000)).Allowed {
		t.Fatal("route b should be independent")
	}
}

func TestIdleSweep(t *testing.T) {
	for _, s := range []string{contract.StrategyFixedWindow, contract.StrategySlidingWindow} {
		l := New(cfg(2, time.Minute, s, ""))
		l.Allow("a", at(1000))
		l.Allow("a", at(1000))
		l.Allow("b", at(1100))
		if n := l.Sweep(at(1119)); n != 0 || l.Len() != 2 {
			t.Fatalf("%s: early sweep removed %d", s, n)
		}
		if n := l.Sweep(at(1120)); n != 1 || l.Len() != 1 {
			t.Fatalf("%s: sweep removed %d, len %d", s, n, l.Len())
		}
		l.Allow("c", at(1300))
		if l.Len() != 1 {
			t.Fatalf("%s: lazy sweep left %d buckets", s, l.Len())
		}
		d := l.Allow("a", at(1300))
		if !d.Allowed || d.Remaining != 1 {
			t.Fatalf("%s: fresh bucket %+v", s, d)
		}
	}
}

func TestClientIP(t *testing.T) {
	cases := map[string]string{
		"1.2.3.4:5678":  "1.2.3.4",
		"[::1]:80":      "::1",
		"no-port":       "no-port",
		"10.0.0.1:1234": "10.0.0.1",
	}
	for in, want := range cases {
		if got := ClientIP(in); got != want {
			t.Errorf("ClientIP(%q) = %q, want %q", in, got, want)
		}
	}
}

func TestConcurrent(t *testing.T) {
	l := New(cfg(1000, time.Minute, contract.StrategySlidingWindow, ""))
	done := make(chan int)
	for range 10 {
		go func() {
			ok := 0
			for range 200 {
				if l.Allow("a", at(1000)).Allowed {
					ok++
				}
			}
			done <- ok
		}()
	}
	total := 0
	for range 10 {
		total += <-done
	}
	if total != 1000 {
		t.Fatalf("allowed %d, want 1000", total)
	}
}
