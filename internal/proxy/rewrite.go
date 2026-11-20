// Package proxy forwards one request to an upstream target: path rewriting,
// the upstream HTTP client, the per-attempt timeout and error classification.
package proxy

import "strings"

// RewritePath returns the forwarded path and query (without any target base
// path). With strip, routePath is removed and an empty result becomes "/".
func RewritePath(routePath string, strip bool, reqPath, rawQuery string) string {
	p := reqPath
	if strip && routePath != "/" {
		if rest, ok := strings.CutPrefix(p, routePath); ok {
			p = rest
		}
	}
	if p == "" || p[0] != '/' {
		p = "/" + p
	}
	if rawQuery != "" {
		p += "?" + rawQuery
	}
	return p
}

// JoinPath puts the target base path in front of p without doubling "/".
// p may carry a query string.
func JoinPath(base, p string) string {
	base = strings.TrimRight(base, "/")
	if base == "" {
		if p == "" {
			return "/"
		}
		return p
	}
	if p == "" || p == "/" {
		return base + "/"
	}
	if p[0] == '?' {
		return base + "/" + p
	}
	return base + "/" + strings.TrimLeft(p, "/")
}
