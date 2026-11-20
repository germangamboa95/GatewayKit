package gateway

import (
	"strings"
	"testing"
)

func stage(name string, calls *[]string, stop *Response) Middleware {
	return func(next Handler) Handler {
		return func(rc *RequestContext) *Response {
			*calls = append(*calls, name)
			if stop != nil {
				return stop
			}
			return next(rc)
		}
	}
}

func terminal(calls *[]string, status int) Handler {
	return func(*RequestContext) *Response {
		*calls = append(*calls, "terminal")
		return &Response{Status: status}
	}
}

func TestChainOrder(t *testing.T) {
	var calls []string
	h := Chain(terminal(&calls, 200), stage("ratelimit", &calls, nil), nil, stage("auth", &calls, nil), stage("reqtransform", &calls, nil))
	if resp := h(&RequestContext{}); resp.Status != 200 {
		t.Fatalf("status %d", resp.Status)
	}
	if got := strings.Join(calls, ","); got != "ratelimit,auth,reqtransform,terminal" {
		t.Fatalf("calls %s", got)
	}
}

func TestChainEarlyExit(t *testing.T) {
	var calls []string
	h := Chain(terminal(&calls, 200),
		stage("ratelimit", &calls, nil),
		stage("auth", &calls, &Response{Status: 401}),
		stage("reqtransform", &calls, nil),
	)
	if resp := h(&RequestContext{}); resp.Status != 401 {
		t.Fatalf("status %d", resp.Status)
	}
	if got := strings.Join(calls, ","); got != "ratelimit,auth" {
		t.Fatalf("calls %s", got)
	}
}

func TestChainNoMiddleware(t *testing.T) {
	var calls []string
	h := Chain(terminal(&calls, 204))
	if resp := h(&RequestContext{}); resp.Status != 204 {
		t.Fatalf("status %d", resp.Status)
	}
}

func TestPassThrough(t *testing.T) {
	var calls []string
	h := Chain(terminal(&calls, 200), PassThrough)
	if resp := h(&RequestContext{}); resp.Status != 200 || len(calls) != 1 {
		t.Fatalf("status %d calls %v", resp.Status, calls)
	}
}
