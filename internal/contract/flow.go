package contract

import (
	"context"
	"errors"
	"log/slog"
	"net/http"
	"time"
)

// RequestContext carries one request through the pipeline.
type RequestContext struct {
	Req          *http.Request
	ClientIP     string
	Route        *Route
	RequestTime  time.Time
	Body         []byte
	Upstream     *Response
	ResponseTime time.Time
}

type Handler func(ctx *RequestContext) *Response

type Middleware func(next Handler) Handler

// Response is a fully buffered response. FromUpstream is true only for
// responses relayed from an upstream, never for gateway errors.
type Response struct {
	Status       int
	Header       http.Header
	Body         []byte
	FromUpstream bool
}

// OutRequest is what the upstream client sends. Path includes the query string.
type OutRequest struct {
	Method string
	Path   string
	Header http.Header
	Body   []byte
}

// Decision is a rate limiter verdict. Reset is Unix seconds; RetryAfter is
// whole seconds (at least 1 when !Allowed).
type Decision struct {
	Allowed    bool
	Limit      int
	Remaining  int
	Reset      int64
	RetryAfter int
}

type Balancer interface {
	Pick(pool TargetPool) (*Target, bool)
}

type TargetPool interface {
	Targets() []*Target
	Healthy(t *Target) bool
	MarkProbe(t *Target, ok bool)
}

type RateLimiter interface {
	Allow(key string, now time.Time) Decision
}

// Authenticator returns nil, ErrMissingAPIKey or ErrInvalidAPIKey.
type Authenticator interface {
	Check(h http.Header) error
}

var (
	ErrMissingAPIKey = errors.New("missing API key")
	ErrInvalidAPIKey = errors.New("invalid API key")
)

// CircuitBreaker: Release ends a half-open trial that never reached the upstream.
type CircuitBreaker interface {
	TryPass(now time.Time) (ok bool, retryAfter int)
	Record(ok bool, now time.Time)
	Release()
}

// RetryPolicy: attempt counts from 1 (the first try).
type RetryPolicy interface {
	ShouldRetry(status, attempt int) bool
	Delay(attempt int) time.Duration
}

type Clock interface {
	Now() time.Time
}

// Sleeper waits for d or until ctx is done, returning ctx.Err() in that case.
type Sleeper interface {
	Sleep(ctx context.Context, d time.Duration) error
}

type UpstreamClient interface {
	Send(ctx context.Context, t *Target, r *OutRequest) (*Response, error)
}

// SystemClock is the real Clock and Sleeper.
type SystemClock struct{}

func (SystemClock) Now() time.Time { return time.Now() }

func (SystemClock) Sleep(ctx context.Context, d time.Duration) error {
	if d <= 0 {
		return ctx.Err()
	}
	t := time.NewTimer(d)
	defer t.Stop()
	select {
	case <-t.C:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

// Deps is everything a constructor may need. Use WithDefaults to fill gaps.
type Deps struct {
	Clock   Clock
	Sleeper Sleeper
	Client  UpstreamClient
	Logger  *slog.Logger
}

// WithDefaults fills a nil Clock, Sleeper or Logger. Client has no default.
func (d Deps) WithDefaults() Deps {
	if d.Clock == nil {
		d.Clock = SystemClock{}
	}
	if d.Sleeper == nil {
		d.Sleeper = SystemClock{}
	}
	if d.Logger == nil {
		d.Logger = slog.Default()
	}
	return d
}
