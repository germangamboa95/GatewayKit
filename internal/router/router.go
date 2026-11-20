// Package router selects a route by longest path prefix on segment boundaries.
package router

import (
	"slices"
	"sort"
	"strings"

	"gatewaykit/internal/contract"
)

// Router matches request paths against routes. It is safe for concurrent use.
type Router struct {
	routes []*contract.Route
}

// New returns a router over routes. Config order does not affect selection.
func New(routes []*contract.Route) *Router {
	rs := make([]*contract.Route, 0, len(routes))
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
func (rt *Router) Match(path string) *contract.Route {
	for _, r := range rt.routes {
		if Matches(r.Path, path) {
			return r
		}
	}
	return nil
}

// Resolve matches path and checks method. It returns the route and nil, the
// route and a 405, or nil and a 404. It never falls back to a shorter route.
func (rt *Router) Resolve(method, path string) (*contract.Route, *contract.Response) {
	r := rt.Match(path)
	if r == nil {
		return nil, contract.NotFound()
	}
	if !Allowed(r, method) {
		return r, contract.MethodNotAllowed(AllowList(r))
	}
	return r, nil
}

// Allowed reports whether r accepts method. A route with no methods allows
// contract.DefaultMethods.
func Allowed(r *contract.Route, method string) bool {
	if len(r.Methods) > 0 {
		return r.Methods[method]
	}
	return slices.Contains(AllowList(r), method)
}

// AllowList returns the Allow header methods: MethodList in config order.
func AllowList(r *contract.Route) []string {
	if len(r.MethodList) > 0 {
		return r.MethodList
	}
	if len(r.Methods) == 0 {
		return contract.DefaultMethods
	}
	var out []string
	for _, m := range contract.DefaultMethods {
		if r.Methods[m] {
			out = append(out, m)
		}
	}
	var rest []string
	for m, ok := range r.Methods {
		if ok && !slices.Contains(contract.DefaultMethods, m) {
			rest = append(rest, m)
		}
	}
	sort.Strings(rest)
	return append(out, rest...)
}
