package retry

import (
	"net/http"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

func on(codes ...int) map[int]bool {
	m := map[int]bool{}
	for _, c := range codes {
		m[c] = true
	}
	return m
}

func TestExponentialDelays(t *testing.T) {
	p := New(&contract.Retry{Attempts: 4, Backoff: contract.BackoffExponential, InitialDelay: time.Second, On: on(503)})
	want := []time.Duration{time.Second, 2 * time.Second, 4 * time.Second}
	for i, w := range want {
		if got := p.Delay(i + 1); got != w {
			t.Fatalf("Delay(%d) = %v want %v", i+1, got, w)
		}
	}
	if p.Delay(0) != 0 {
		t.Fatal("Delay(0) should be 0")
	}
	if d := p.Delay(200); d <= 0 {
		t.Fatalf("overflow: %v", d)
	}
}

func TestFixedDelays(t *testing.T) {
	p := New(&contract.Retry{Attempts: 3, Backoff: contract.BackoffFixed, InitialDelay: 100 * time.Millisecond, On: on(500)})
	for a := 1; a <= 3; a++ {
		if got := p.Delay(a); got != 100*time.Millisecond {
			t.Fatalf("Delay(%d) = %v", a, got)
		}
	}
	z := New(&contract.Retry{Attempts: 3, Backoff: contract.BackoffExponential, On: on(500)})
	if z.Delay(2) != 0 {
		t.Fatal("zero initial delay should give 0")
	}
}

func TestAttemptsBound(t *testing.T) {
	p := New(&contract.Retry{Attempts: 3, On: on(503)})
	tries := 1
	for p.ShouldRetry(503, tries) {
		tries++
	}
	if tries != 3 {
		t.Fatalf("tries %d", tries)
	}
	one := New(&contract.Retry{Attempts: 1, On: on(503)})
	if one.ShouldRetry(503, 1) {
		t.Fatal("attempts 1 must not retry")
	}
}

func TestOnFiltering(t *testing.T) {
	p := New(&contract.Retry{Attempts: 3, On: on(502, 503, 504)})
	if p.ShouldRetry(500, 1) {
		t.Fatal("500 not in on")
	}
	if !p.ShouldRetry(contract.Classify(nil, errRefused{}), 1) {
		t.Fatal("connection error stand-in 502 should retry")
	}
	if !p.ShouldRetry(http.StatusGatewayTimeout, 1) {
		t.Fatal("timeout stand-in 504 should retry")
	}
	if p.ShouldRetry(200, 1) || p.ShouldRetry(404, 1) {
		t.Fatal("success and 4xx must not retry")
	}

	only500 := New(&contract.Retry{Attempts: 3, On: on(500)})
	if only500.ShouldRetry(http.StatusBadGateway, 1) {
		t.Fatal("502 stand-in not in on")
	}
	if only500.ShouldRetry(http.StatusGatewayTimeout, 1) {
		t.Fatal("504 stand-in not in on")
	}
}

func TestNilConfig(t *testing.T) {
	p := New(nil)
	if p.Attempts() != 1 || p.ShouldRetry(503, 1) || p.Delay(1) != 0 {
		t.Fatal("nil config must make one attempt")
	}
	var zero Policy
	if zero.ShouldRetry(503, 1) || zero.Attempts() != 1 {
		t.Fatal("zero policy must not retry")
	}
}

type errRefused struct{}

func (errRefused) Error() string { return "connection refused" }
