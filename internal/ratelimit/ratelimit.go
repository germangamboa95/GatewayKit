// Package ratelimit implements per-route fixed and sliding window limiters.
package ratelimit

import (
	"net"
	"sync"
	"time"

	"gatewaykit/internal/contract"
)

// Limiter is one route's bucket store. It implements contract.RateLimiter.
type Limiter struct {
	requests int
	window   int64
	sliding  bool
	global   bool

	mu        sync.Mutex
	buckets   map[string]*bucket
	lastSweep int64
}

type bucket struct {
	lastUsed int64

	start int64
	count int

	buf  []int64
	head int
	n    int
}

var _ contract.RateLimiter = (*Limiter)(nil)

// New returns a limiter for cfg, or nil when cfg is nil or unusable.
func New(cfg *contract.RateLimit) *Limiter {
	if cfg == nil || cfg.Requests < 1 || cfg.Window <= 0 {
		return nil
	}
	return &Limiter{
		requests: cfg.Requests,
		window:   int64(cfg.Window),
		sliding:  cfg.Strategy == contract.StrategySlidingWindow,
		global:   cfg.Per == contract.PerGlobal,
		buckets:  make(map[string]*bucket),
	}
}

// Key returns the bucket key for a client IP: the IP, or "" for per: global.
func (l *Limiter) Key(clientIP string) string {
	if l.global {
		return ""
	}
	return clientIP
}

// Limit returns the configured request count.
func (l *Limiter) Limit() int { return l.requests }

// Len returns the number of live buckets.
func (l *Limiter) Len() int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return len(l.buckets)
}

// Allow records one request for key at now and returns the verdict.
// Rejected requests do not count.
func (l *Limiter) Allow(key string, now time.Time) contract.Decision {
	t := now.UnixNano()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.maybeSweep(t)
	b := l.buckets[key]
	if b == nil {
		b = &bucket{}
		l.buckets[key] = b
	}
	b.lastUsed = t
	if l.sliding {
		return l.allowSliding(b, t)
	}
	return l.allowFixed(b, t)
}

func (l *Limiter) allowFixed(b *bucket, t int64) contract.Decision {
	start := floorDiv(t, l.window) * l.window
	if b.start != start {
		b.start = start
		b.count = 0
	}
	end := start + l.window
	d := contract.Decision{Limit: l.requests, Reset: ceilSeconds(end)}
	if b.count < l.requests {
		b.count++
		d.Allowed = true
		d.Remaining = l.requests - b.count
		return d
	}
	d.RetryAfter = retryAfter(end - t)
	return d
}

func (l *Limiter) allowSliding(b *bucket, t int64) contract.Decision {
	for b.n > 0 && b.buf[b.head]+l.window <= t {
		b.head = (b.head + 1) % len(b.buf)
		b.n--
	}
	d := contract.Decision{Limit: l.requests}
	if b.n < l.requests {
		b.push(t, l.requests)
		d.Allowed = true
		d.Remaining = l.requests - b.n
		d.Reset = ceilSeconds(b.buf[b.head] + l.window)
		return d
	}
	leave := b.buf[b.head] + l.window
	d.Reset = ceilSeconds(leave)
	d.RetryAfter = retryAfter(leave - t)
	return d
}

func (b *bucket) push(t int64, limit int) {
	if b.n == len(b.buf) {
		size := min(max(2*len(b.buf), 8), limit)
		nb := make([]int64, size)
		for i := 0; i < b.n; i++ {
			nb[i] = b.buf[(b.head+i)%len(b.buf)]
		}
		b.buf, b.head = nb, 0
	}
	b.buf[(b.head+b.n)%len(b.buf)] = t
	b.n++
}

func (l *Limiter) maybeSweep(t int64) {
	if l.lastSweep != 0 && t-l.lastSweep < l.window {
		return
	}
	l.lastSweep = t
	l.sweep(t)
}

// Sweep deletes buckets idle for at least 2x window and returns how many it removed.
func (l *Limiter) Sweep(now time.Time) int {
	l.mu.Lock()
	defer l.mu.Unlock()
	return l.sweep(now.UnixNano())
}

func (l *Limiter) sweep(t int64) int {
	removed := 0
	for k, b := range l.buckets {
		if t-b.lastUsed >= 2*l.window {
			delete(l.buckets, k)
			removed++
		}
	}
	return removed
}

// ClientIP returns the host part of a socket address such as "1.2.3.4:5678".
func ClientIP(remoteAddr string) string {
	host, _, err := net.SplitHostPort(remoteAddr)
	if err != nil {
		return remoteAddr
	}
	return host
}

func floorDiv(a, b int64) int64 {
	q := a / b
	if a%b != 0 && (a < 0) != (b < 0) {
		q--
	}
	return q
}

func ceilSeconds(ns int64) int64 {
	return -floorDiv(-ns, int64(time.Second))
}

func retryAfter(ns int64) int {
	s := int(-floorDiv(-ns, int64(time.Second)))
	if s < 1 {
		return 1
	}
	return s
}
