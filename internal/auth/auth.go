// Package auth implements API-key authentication.
package auth

import (
	"crypto/subtle"
	"net/http"
	"strings"

	"gatewaykit/internal/contract"
)

// DefaultHeader is the header used when the config sets none.
const DefaultHeader = "X-API-Key"

// APIKey checks a header against a fixed key set. It implements contract.Authenticator.
type APIKey struct {
	header string
	keys   [][]byte
}

var _ contract.Authenticator = (*APIKey)(nil)

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
func FromConfig(cfg *contract.Auth) contract.Authenticator {
	if cfg == nil {
		return nil
	}
	if cfg.Type != contract.AuthTypeAPIKey && cfg.Type != "" {
		return NewAPIKey(cfg.Header, nil)
	}
	return NewAPIKey(cfg.Header, cfg.Keys)
}

// Header returns the configured header name.
func (a *APIKey) Header() string { return a.header }

// Check returns nil, contract.ErrMissingAPIKey or contract.ErrInvalidAPIKey.
func (a *APIKey) Check(h http.Header) error {
	v, ok := lookup(h, a.header)
	if !ok {
		return contract.ErrMissingAPIKey
	}
	got := []byte(v)
	match := 0
	for _, k := range a.keys {
		match |= subtle.ConstantTimeCompare(got, k)
	}
	if match != 1 {
		return contract.ErrInvalidAPIKey
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
