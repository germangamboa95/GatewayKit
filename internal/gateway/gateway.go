// Package gateway defines the request pipeline every route runs: the
// per-request state, the buffered response, and the handler and middleware
// types that stages are built from.
package gateway

import (
	"net/http"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// RequestContext carries one request through the pipeline.
type RequestContext struct {
	Req          *http.Request
	ClientIP     string
	Route        *config.Route
	RequestTime  time.Time
	Body         []byte
	Upstream     *Response
	ResponseTime time.Time
}

// Handler serves one request and returns the response to send.
type Handler func(rc *RequestContext) *Response

// Middleware wraps a Handler. A stage that returns without calling next
// ends the request.
type Middleware func(next Handler) Handler

// Response is a fully buffered response. FromUpstream is true only for
// responses relayed from an upstream, never for gateway errors.
type Response struct {
	Status       int
	Header       http.Header
	Body         []byte
	FromUpstream bool
}

// Chain composes stages around terminal. The first middleware runs first.
// Nil entries are skipped.
func Chain(terminal Handler, mws ...Middleware) Handler {
	h := terminal
	for i := len(mws) - 1; i >= 0; i-- {
		if mws[i] != nil {
			h = mws[i](h)
		}
	}
	return h
}

// PassThrough is the identity middleware, for routes without a stage.
func PassThrough(next Handler) Handler { return next }
