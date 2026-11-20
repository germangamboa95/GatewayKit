// Package router selects a route by longest path prefix on segment boundaries.
package router

import (
	"net/http"
	"slices"
	"sort"
	"strings"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

// Router matches request paths against routes. It is safe for concurrent use.
type Router struct {
	routes []*config.Route
}

// New returns a router over routes. Config order does not affect selection.
func New(routes []*config.Route) *Router {
	rs := make([]*config.Route, 0, len(routes))
	for _, r := range routes {
		if r != nil {
			rs = append(rs, r)
		}
	}
	sort.SliceStable(rs, func(i, j int) bool { return len(rs[i].Path) > len(rs[j].Path) })
	return &Router{routes: rs}
}

// Matches reports whether reqPath equals routePath or continues it after a "/".
func Matches(routePath, reqPath string) bool {
	if reqPath == routePath {
		return true
	}
	prefix := strings.TrimSuffix(routePath, "/")
	return strings.HasPrefix(reqPath, prefix+"/")
}

// Match returns the route with the longest matching path, or nil.
func (rt *Router) Match(path string) *config.Route {
	for _, r := range rt.routes {
		if Matches(r.Path, path) {
			return r
		}
	}
	return nil
}

// Resolve matches path and checks method. It returns the route and nil, the
// route and a 405, or nil and a 404. It never falls back to a shorter route.
func (rt *Router) Resolve(method, path string) (*config.Route, *gateway.Response) {
	r := rt.Match(path)
	if r == nil {
		return nil, NotFound()
	}
	if !Allowed(r, method) {
		return r, MethodNotAllowed(AllowList(r))
	}
	return r, nil
}

// Allowed reports whether r accepts method. A route with no methods allows
// config.DefaultMethods.
func Allowed(r *config.Route, method string) bool {
	if len(r.Methods) > 0 {
		return r.Methods[method]
	}
	return slices.Contains(AllowList(r), method)
}

// AllowList returns the Allow header methods: MethodList in config order.
func AllowList(r *config.Route) []string {
	if len(r.MethodList) > 0 {
		return r.MethodList
	}
	if len(r.Methods) == 0 {
		return config.DefaultMethods
	}
	var out []string
	for _, m := range config.DefaultMethods {
		if r.Methods[m] {
			out = append(out, m)
		}
	}
	var rest []string
	for m, ok := range r.Methods {
		if ok && !slices.Contains(config.DefaultMethods, m) {
			rest = append(rest, m)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}

// NotFound is the 404 for a path no route matches.
func NotFound() *gateway.Response {
	return gateway.ErrorResponse(http.StatusNotFound, gateway.CodeNotFound, "no route matches the request path", nil)
}

// MethodNotAllowed is the 405 for a method the route does not accept. It
// sets Allow to allow, in config order.
func MethodNotAllowed(allow []string) *gateway.Response {
	r := gateway.ErrorResponse(http.StatusMethodNotAllowed, gateway.CodeMethodNotAllowed, "method not allowed on this route", nil)
	r.Header.Set("Allow", strings.Join(allow, ", "))
	return r
}
