package contract

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"net"
	"net/http"
	"sort"
	"strconv"
	"strings"
)

// Error codes from config spec §12.
const (
	CodeInvalidJSONBody    = "invalid_json_body"
	CodeUnauthorized       = "unauthorized"
	CodeNotFound           = "not_found"
	CodeMethodNotAllowed   = "method_not_allowed"
	CodeRateLimitExceeded  = "rate_limit_exceeded"
	CodeBadGateway         = "bad_gateway"
	CodeServiceUnavailable = "service_unavailable"
	CodeNoHealthyUpstream  = "no_healthy_upstream"
	CodeGatewayTimeout     = "gateway_timeout"
)

// StatusClientClosed is what Classify returns when the client canceled the
// request. Callers abort without recording or retrying (design D6 step 4).
const StatusClientClosed = 499

// ErrorResponse builds a gateway error: a JSON object with "error", then
// "message" (omitted when msg is empty), then extra keys in sorted order.
func ErrorResponse(status int, code, msg string, extra map[string]any) *Response {
	var b bytes.Buffer
	b.WriteString(`{"error":`)
	b.Write(jsonValue(code))
	if msg != "" {
		b.WriteString(`,"message":`)
		b.Write(jsonValue(msg))
	}
	keys := make([]string, 0, len(extra))
	for k := range extra {
		keys = append(keys, k)
	}
	sort.Strings(keys)
	for _, k := range keys {
		b.WriteByte(',')
		b.Write(jsonValue(k))
		b.WriteByte(':')
		b.Write(jsonValue(extra[k]))
	}
	b.WriteByte('}')
	return &Response{
		Status: status,
		Header: http.Header{"Content-Type": {"application/json"}},
		Body:   b.Bytes(),
	}
}

func jsonValue(v any) []byte {
	var b bytes.Buffer
	enc := json.NewEncoder(&b)
	enc.SetEscapeHTML(false)
	if err := enc.Encode(v); err != nil {
		return []byte("null")
	}
	return bytes.TrimRight(b.Bytes(), "\n")
}

func NotFound() *Response {
	return ErrorResponse(http.StatusNotFound, CodeNotFound, "no route matches the request path", nil)
}

// MethodNotAllowed sets Allow to the route's methods in config order.
func MethodNotAllowed(allow []string) *Response {
	r := ErrorResponse(http.StatusMethodNotAllowed, CodeMethodNotAllowed, "method not allowed on this route", nil)
	r.Header.Set("Allow", strings.Join(allow, ", "))
	return r
}

// RateLimited sets the body and Retry-After; the caller adds X-RateLimit-*.
func RateLimited(retryAfter int) *Response {
	r := ErrorResponse(http.StatusTooManyRequests, CodeRateLimitExceeded, "rate limit exceeded",
		map[string]any{"retry_after": retryAfter})
	r.Header.Set("Retry-After", strconv.Itoa(retryAfter))
	return r
}

// Unauthorized maps ErrMissingAPIKey or ErrInvalidAPIKey to its 401 body.
func Unauthorized(err error) *Response {
	msg := ErrInvalidAPIKey.Error()
	if errors.Is(err, ErrMissingAPIKey) {
		msg = ErrMissingAPIKey.Error()
	}
	return ErrorResponse(http.StatusUnauthorized, CodeUnauthorized, msg, nil)
}

func InvalidJSONBody() *Response {
	return ErrorResponse(http.StatusBadRequest, CodeInvalidJSONBody, "request body is not valid JSON", nil)
}

func BadGateway() *Response {
	return ErrorResponse(http.StatusBadGateway, CodeBadGateway, "upstream unreachable or sent an invalid response", nil)
}

func GatewayTimeout() *Response {
	return ErrorResponse(http.StatusGatewayTimeout, CodeGatewayTimeout, "upstream timed out", nil)
}

func NoHealthyUpstream() *Response {
	return ErrorResponse(http.StatusServiceUnavailable, CodeNoHealthyUpstream, "no healthy upstream target", nil)
}

// BreakerOpen is the circuit-breaker 503: only "error" and "retry_after", plus Retry-After.
func BreakerOpen(retryAfter int) *Response {
	r := ErrorResponse(http.StatusServiceUnavailable, CodeServiceUnavailable, "",
		map[string]any{"retry_after": retryAfter})
	r.Header.Set("Retry-After", strconv.Itoa(retryAfter))
	return r
}

// Classify maps an upstream attempt to a status: the response status, 504 for
// a timeout, StatusClientClosed for a client cancel, 502 for any other error.
func Classify(resp *Response, err error) int {
	if err == nil {
		if resp == nil {
			return http.StatusBadGateway
		}
		return resp.Status
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return http.StatusGatewayTimeout
	}
	var ne net.Error
	if errors.As(err, &ne) && ne.Timeout() {
		return http.StatusGatewayTimeout
	}
	if errors.Is(err, context.Canceled) {
		return StatusClientClosed
	}
	return http.StatusBadGateway
}
