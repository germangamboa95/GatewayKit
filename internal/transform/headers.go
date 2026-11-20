package transform

import (
	"net/http"
	"strings"

	"gatewaykit/internal/contract"
)

var managed = map[string]bool{
	"host":              true,
	"content-length":    true,
	"transfer-encoding": true,
	"connection":        true,
	"keep-alive":        true,
	"te":                true,
	"trailer":           true,
	"upgrade":           true,
}

// IsManaged reports whether a transform must leave the header alone: Host,
// Content-Length, Transfer-Encoding and the hop-by-hop headers.
func IsManaged(name string) bool {
	n := strings.ToLower(name)
	return managed[n] || strings.HasPrefix(n, "proxy-")
}

// ApplyHeaders removes, then adds, skipping managed headers. Each add
// replaces any existing value.
func ApplyHeaders(h http.Header, remove []string, add []contract.HeaderAdd, env Env) {
	for _, name := range remove {
		if IsManaged(name) {
			continue
		}
		h.Del(name)
	}
	for _, a := range add {
		if IsManaged(a.Name) {
			continue
		}
		h.Set(a.Name, Resolve(a.Value, env))
	}
}
