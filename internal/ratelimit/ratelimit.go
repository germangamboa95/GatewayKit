// Package ratelimit implements per-route fixed and sliding window limiters
// and the rate-limit middleware (config spec §6).
package ratelimit

import (
	"container/list"
	"net"
	"sync"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// Decision is a limiter verdict. Reset is Unix seconds; RetryAfter is
// whole seconds (at least 1 when !Allowed).
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	Reset      int64
	RetryAfter int
}

// Limiter is one route's bucket store.
type Limiter struct {
	requests int
	window   int64
	sliding  bool
	global   bool

	mu      sync.Mutex
	buckets map[string]*bucket
	lru     *list.List
}

type bucket struct {
	key      string
	elem     *list.Element
	lastUsed int64

	start int64
	count int

	buf  []int64
	head int
	n    int
}

// New returns a limiter for cfg, or nil when cfg is nil or unusable.
func New(cfg *config.RateLimit) *Limiter {
	if cfg == nil || cfg.Requests < 1 || cfg.Window <= 0 {
		return nil
	}
	return &Limiter{
		requests: cfg.Requests,
		window:   int64(cfg.Window),
		sliding:  cfg.Strategy == config.StrategySlidingWindow,
		global:   cfg.Per == config.PerGlobal,
		buckets:  make(map[string]*bucket),
		lru:      list.New(),
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
func (l *Limiter) Allow(key string, now time.Time) Decision {
	t := now.UnixNano()
	l.mu.Lock()
	defer l.mu.Unlock()
	l.evictIdle(t)
	b := l.buckets[key]
	if b == nil {
		b = &bucket{key: key}
		b.elem = l.lru.PushFront(b)
		l.buckets[key] = b
	} else {
		l.lru.MoveToFront(b.elem)
	}
	b.lastUsed = t
	if l.sliding {
		return l.allowSliding(b, t)
	}
	return l.allowFixed(b, t)
}

func (l *Limiter) allowFixed(b *bucket, t int64) Decision {
	start := floorDiv(t, l.window) * l.window
	if b.start != start {
		b.start = start
		b.count = 0
	}
	end := start + l.window
	d := Decision{Limit: l.requests, Reset: ceilSeconds(end)}
	if b.count < l.requests {
		b.count++
		d.Allowed = true
		d.Remaining = l.requests - b.count
		return d
	}
	d.RetryAfter = retryAfter(end - t)
	return d
}

func (l *Limiter) allowSliding(b *bucket, t int64) Decision {
	for b.n > 0 && b.buf[b.head]+l.window <= t {
		b.head = (b.head + 1) % len(b.buf)
		b.n--
	}
	d := Decision{Limit: l.requests}
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

// evictIdle deletes buckets idle for at least 2x window, oldest first.
func (l *Limiter) evictIdle(t int64) {
	for e := l.lru.Back(); e != nil; e = l.lru.Back() {
		b := e.Value.(*bucket)
		if t-b.lastUsed < 2*l.window {
			return
		}
		l.lru.Remove(e)
		delete(l.buckets, b.key)
	}
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
			l.lru.Remove(b.elem)
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
