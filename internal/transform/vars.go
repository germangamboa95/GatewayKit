// Package transform resolves variables, applies header, body-mapping and
// envelope transforms, and provides the request and response transform
// middleware (config spec §9).
package transform

import (
	"time"

	"github.com/germangamboa95/gatewaykit/internal/config"
)

// TimeLayout is the ISO-8601 UTC format with milliseconds used by
// $request_time and $response_time.
const TimeLayout = "2006-01-02T15:04:05.000Z"

// FormatTime formats t in UTC using TimeLayout.
func FormatTime(t time.Time) string { return t.UTC().Format(TimeLayout) }

// Env holds the per-request values variables resolve to.
type Env struct {
	RequestTime  time.Time
	ResponseTime time.Time
	RoutePath    string
}

// Resolve returns the string value of v. $body is not a string variable;
// it resolves to "" here and is handled by Envelope.
func Resolve(v config.Var, env Env) string {
	switch v.Kind {
	case config.VarLiteral:
		return v.Text
	case config.VarRequestTime:
		return FormatTime(env.RequestTime)
	case config.VarResponseTime:
		return FormatTime(env.ResponseTime)
	case config.VarRoutePath:
		return env.RoutePath
	}
	return ""
}
