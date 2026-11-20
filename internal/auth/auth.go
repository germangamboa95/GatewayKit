// Package auth implements API-key authentication and its middleware
// (config spec §8).
package auth

import (
	"crypto/subtle"
	"errors"
	"net/http"
	"strings"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// DefaultHeader is the header used when the config sets none.
const DefaultHeader = "X-API-Key"

// Errors returned by Check.
var (
	ErrMissingAPIKey = errors.New("missing API key")
	ErrInvalidAPIKey = errors.New("invalid API key")
)

// APIKey checks a header against a fixed key set.
type APIKey struct {
	header string
	keys   [][]byte
}

// NewAPIKey returns an authenticator reading header (DefaultHeader when empty).
func NewAPIKey(header string, keys []string) *APIKey {
	if header == "" {
		header = DefaultHeader
	}
	a := &APIKey{header: header}
	for _, k := range keys {
		if k != "" {
			a.keys = append(a.keys, []byte(k))
		}
	}
	return a
}

// FromConfig builds an authenticator for cfg. An unknown type yields one
// that rejects every request, so auth never silently turns off.
func FromConfig(cfg *config.Auth) *APIKey {
	if cfg == nil {
		return nil
	}
	if cfg.Type != config.AuthTypeAPIKey && cfg.Type != "" {
		return NewAPIKey(cfg.Header, nil)
	}
	return NewAPIKey(cfg.Header, cfg.Keys)
}

// Header returns the configured header name.
func (a *APIKey) Header() string { return a.header }

// Check returns nil, ErrMissingAPIKey or ErrInvalidAPIKey.
func (a *APIKey) Check(h http.Header) error {
	v, ok := lookup(h, a.header)
	if !ok {
		return ErrMissingAPIKey
	}
	got := []byte(v)
	match := 0
	for _, k := range a.keys {
		match |= subtle.ConstantTimeCompare(got, k)
	}
	if match != 1 {
		return ErrInvalidAPIKey
	}
	return nil
}

func lookup(h http.Header, name string) (string, bool) {
	if vs, ok := h[http.CanonicalHeaderKey(name)]; ok && len(vs) > 0 {
		return vs[0], true
	}
	for k, vs := range h {
		if strings.EqualFold(k, name) && len(vs) > 0 {
			return vs[0], true
		}
	}
	return "", false
}
