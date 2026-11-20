package auth

import (
	"errors"
	"net/http"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

func TestDefaultHeader(t *testing.T) {
	a := NewAPIKey("", []string{"sk_live_abc123", "sk_live_def456"})
	if a.Header() != "X-API-Key" {
		t.Fatalf("header %q", a.Header())
	}
	h := http.Header{}
	h.Set("x-api-key", "sk_live_abc123")
	if err := a.Check(h); err != nil {
		t.Fatalf("valid key: %v", err)
	}
	h.Set("X-API-Key", "sk_live_def456")
	if err := a.Check(h); err != nil {
		t.Fatalf("second key: %v", err)
	}
}

func TestNonCanonicalHeaderMap(t *testing.T) {
	a := NewAPIKey("X-API-Key", []string{"k1"})
	h := http.Header{"x-api-key": {"k1"}}
	if err := a.Check(h); err != nil {
		t.Fatalf("lowercase map key: %v", err)
	}
}

func TestCustomHeader(t *testing.T) {
	a := NewAPIKey("authorization", []string{"k1"})
	h := http.Header{}
	h.Set("Authorization", "k1")
	if err := a.Check(h); err != nil {
		t.Fatalf("custom header: %v", err)
	}
	if err := a.Check(http.Header{"X-Api-Key": {"k1"}}); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("missing custom header: %v", err)
	}
}

func TestMissing(t *testing.T) {
	a := NewAPIKey("", []string{"k1"})
	if err := a.Check(http.Header{}); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("got %v", err)
	}
	if err := a.Check(nil); !errors.Is(err, ErrMissingAPIKey) {
		t.Fatalf("nil header: %v", err)
	}
}

func TestInvalid(t *testing.T) {
	a := NewAPIKey("", []string{"sk_live_abc123"})
	for _, v := range []string{"sk_live_wrong", "sk_live_abc12", "sk_live_abc1234", "SK_LIVE_ABC123", ""} {
		h := http.Header{"X-Api-Key": {v}}
		if err := a.Check(h); !errors.Is(err, ErrInvalidAPIKey) {
			t.Fatalf("%q: got %v", v, err)
		}
	}
}

func TestFromConfig(t *testing.T) {
	if FromConfig(nil) != nil {
		t.Fatal("nil config should give nil")
	}
	a := FromConfig(&config.Auth{Type: config.AuthTypeAPIKey, Header: "x-api-key", Keys: []string{"k"}})
	if err := a.Check(http.Header{"X-Api-Key": {"k"}}); err != nil {
		t.Fatalf("valid: %v", err)
	}
	deny := FromConfig(&config.Auth{Type: "bearer", Keys: []string{"k"}})
	if err := deny.Check(http.Header{"X-Api-Key": {"k"}}); err == nil {
		t.Fatal("unknown type must reject")
	}
}
