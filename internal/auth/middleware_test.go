package auth

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

type upstream struct {
	hits int
	seen http.Header
}

func (u *upstream) handler(rc *gateway.RequestContext) *gateway.Response {
	u.hits++
	u.seen = rc.Req.Header.Clone()
	return &gateway.Response{Status: 200, Header: http.Header{}, FromUpstream: true}
}

func authRoute(header string, keys ...string) *config.Route {
	return &config.Route{Path: "/api/internal", Auth: &config.Auth{Type: config.AuthTypeAPIKey, Header: header, Keys: keys}}
}

func call(h gateway.Handler, hdr map[string]string) *gateway.Response {
	r := httptest.NewRequest("GET", "/api/internal", nil)
	for k, v := range hdr {
		r.Header[k] = []string{v}
	}
	return h(&gateway.RequestContext{Req: r})
}

func TestMissingKey(t *testing.T) {
	u := &upstream{}
	h := Middleware(authRoute("x-api-key", "sk_live_abc123").Auth)(u.handler)
	resp := call(h, nil)
	if resp.Status != 401 || u.hits != 0 {
		t.Fatalf("status %d hits %d", resp.Status, u.hits)
	}
	if string(resp.Body) != `{"error":"unauthorized","message":"missing API key"}` {
		t.Fatalf("body %s", resp.Body)
	}
	if resp.Header.Get("Content-Type") != "application/json" || resp.FromUpstream {
		t.Fatalf("headers %v upstream %v", resp.Header, resp.FromUpstream)
	}
}

func TestInvalidKey(t *testing.T) {
	u := &upstream{}
	h := Middleware(authRoute("x-api-key", "sk_live_abc123").Auth)(u.handler)
	resp := call(h, map[string]string{"X-Api-Key": "sk_live_wrong"})
	if resp.Status != 401 || u.hits != 0 {
		t.Fatalf("status %d hits %d", resp.Status, u.hits)
	}
	if string(resp.Body) != `{"error":"unauthorized","message":"invalid API key"}` {
		t.Fatalf("body %s", resp.Body)
	}
}

func TestValidKeyForwardedUnchanged(t *testing.T) {
	u := &upstream{}
	h := Middleware(authRoute("x-api-key", "sk_live_abc123", "sk_live_def456").Auth)(u.handler)
	resp := call(h, map[string]string{"X-Api-Key": "sk_live_def456"})
	if resp.Status != 200 || u.hits != 1 {
		t.Fatalf("status %d hits %d", resp.Status, u.hits)
	}
	if got := u.seen.Get("X-API-Key"); got != "sk_live_def456" {
		t.Fatalf("upstream saw %q", got)
	}
}

func TestCaseInsensitiveHeader(t *testing.T) {
	for _, cfgHeader := range []string{"X-API-Key", "x-api-key", "X-Api-Key"} {
		for _, reqHeader := range []string{"x-api-key", "X-API-KEY", "X-Api-Key"} {
			u := &upstream{}
			h := Middleware(authRoute(cfgHeader, "k1").Auth)(u.handler)
			if resp := call(h, map[string]string{reqHeader: "k1"}); resp.Status != 200 {
				t.Fatalf("cfg %s req %s: %d", cfgHeader, reqHeader, resp.Status)
			}
		}
	}
}

func TestMiddlewareDefaultHeader(t *testing.T) {
	u := &upstream{}
	h := Middleware(authRoute("", "k1").Auth)(u.handler)
	if resp := call(h, map[string]string{"X-Api-Key": "k1"}); resp.Status != 200 {
		t.Fatalf("status %d", resp.Status)
	}
}

func TestMiddlewareCustomHeader(t *testing.T) {
	u := &upstream{}
	h := Middleware(authRoute("authorization", "k1").Auth)(u.handler)
	if resp := call(h, map[string]string{"Authorization": "k1"}); resp.Status != 200 {
		t.Fatalf("valid: %d", resp.Status)
	}
	if got := u.seen.Get("Authorization"); got != "k1" {
		t.Fatalf("upstream saw %q", got)
	}
	resp := call(h, map[string]string{"X-Api-Key": "k1"})
	if resp.Status != 401 || string(resp.Body) != `{"error":"unauthorized","message":"missing API key"}` {
		t.Fatalf("missing custom header: %d %s", resp.Status, resp.Body)
	}
}

func TestNoAuthPassThrough(t *testing.T) {
	u := &upstream{}
	h := Middleware(nil)(u.handler)
	if resp := call(h, nil); resp.Status != 200 || u.hits != 1 {
		t.Fatalf("status %d hits %d", resp.Status, u.hits)
	}
}
