// Package transform resolves variables and applies header, body-mapping and
// envelope transforms (config spec §9, design D8).
package transform

import (
	"time"

	"gatewaykit/internal/contract"
)

// Env holds the per-request values variables resolve to.
type Env struct {
	RequestTime  time.Time
	ResponseTime time.Time
	RoutePath    string
}

// Resolve returns the string value of v. $body is not a string variable;
// it resolves to "" here and is handled by Envelope.
func Resolve(v contract.Var, env Env) string {
	switch v.Kind {
	case contract.VarLiteral:
		return v.Text
	case contract.VarRequestTime:
		return contract.FormatTime(env.RequestTime)
	case contract.VarResponseTime:
		return contract.FormatTime(env.ResponseTime)
	case contract.VarRoutePath:
		return env.RoutePath
	}
	return ""
}
