package gateway

import (
	"bytes"
	"encoding/json"
	"net/http"
	"sort"
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

// StatusClientClosed marks a request the client canceled. Callers abort
// without recording or retrying.
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

// BadGateway is the 502 for an unreachable or invalid upstream.
func BadGateway() *Response {
	return ErrorResponse(http.StatusBadGateway, CodeBadGateway, "upstream unreachable or sent an invalid response", nil)
}

// GatewayTimeout is the 504 for an upstream that timed out.
func GatewayTimeout() *Response {
	return ErrorResponse(http.StatusGatewayTimeout, CodeGatewayTimeout, "upstream timed out", nil)
}

// NoHealthyUpstream is the 503 for a route with no healthy target.
func NoHealthyUpstream() *Response {
	return ErrorResponse(http.StatusServiceUnavailable, CodeNoHealthyUpstream, "no healthy upstream target", nil)
}
