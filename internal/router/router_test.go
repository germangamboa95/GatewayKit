package router

import (
	"encoding/json"
	"net/http"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/config"
	"github.com/germangamboa95/gatewaykit/internal/gateway"
)

func route(path string, methods ...string) *config.Route {
	r := &config.Route{Path: path}
	if len(methods) == 0 {
		methods = config.DefaultMethods
	}
	r.Methods = map[string]bool{}
	for _, m := range methods {
		if !r.Methods[m] {
			r.Methods[m] = true
			r.MethodList = append(r.MethodList, m)
		}
	}
	return r
}

func errCode(t *testing.T, resp *gateway.Response) string {
	t.Helper()
	var body map[string]any
	if err := json.Unmarshal(resp.Body, &body); err != nil {
		t.Fatalf("body %q: %v", resp.Body, err)
	}
	if ct := resp.Header.Get("Content-Type"); ct != "application/json" {
		t.Fatalf("Content-Type = %q", ct)
	}
	if _, ok := body["message"].(string); !ok {
		t.Fatalf("body %q has no message", resp.Body)
	}
	code, _ := body["error"].(string)
	return code
}

func TestMatches(t *testing.T) {
	cases := []struct {
		route, path string
		want        bool
	}{
		{"/api/users", "/api/users", true},
		{"/api/users", "/api/users/", true},
		{"/api/users", "/api/users/42", true},
		{"/api/users", "/api/usersX", false},
		{"/api/users", "/API/users", false},
		{"/api/users", "/api", false},
		{"/", "/anything/at/all", true},
		{"/", "/", true},
		{"/", "//a", true},
		{"/v2", "/v2x", false},
		{"/v2/", "/v2", false},
		{"/v2/", "/v2/a", true},
	}
	for _, c := range cases {
		if got := Matches(c.route, c.path); got != c.want {
			t.Errorf("Matches(%q, %q) = %v, want %v", c.route, c.path, got, c.want)
		}
	}
}

func TestResolve(t *testing.T) {
	sample := []*config.Route{
		route("/api/users", "GET", "POST"),
		route("/api/orders", "GET", "POST", "PUT"),
		route("/api/products", "GET"),
		route("/api/legacy", "GET", "POST"),
		route("/api/internal", "GET", "POST"),
	}
	variantB := []*config.Route{
		route("/v2/admin", "GET"),
		route("/v2", "GET", "DELETE"),
	}
	nested := []*config.Route{
		route("/api", "GET", "POST"),
		route("/api/items", "GET"),
	}
	cases := []struct {
		name         string
		routes       []*config.Route
		method, path string
		wantRoute    string
		wantStatus   int
		wantAllow    string
	}{
		{"exact", sample, "GET", "/api/users", "/api/users", 0, ""},
		{"trailing slash", sample, "GET", "/api/users/", "/api/users", 0, ""},
		{"subpath", sample, "GET", "/api/users/42", "/api/users", 0, ""},
		{"segment boundary miss", sample, "GET", "/api/usersX", "", 404, ""},
		{"case sensitive", sample, "GET", "/API/users", "", 404, ""},
		{"root catch-all", []*config.Route{route("/")}, "GET", "/anything/at/all", "/", 0, ""},
		{"longer prefix", variantB, "GET", "/v2/admin/x", "/v2/admin", 0, ""},
		{"file order", variantB, "GET", "/v2/a", "/v2", 0, ""},
		{"variant B boundary", variantB, "GET", "/v2x", "", 404, ""},
		{"variant B 405", variantB, "POST", "/v2/a", "/v2", 405, "GET, DELETE"},
		{"no fallback", nested, "POST", "/api/items", "/api/items", 405, "GET"},
		{"head not implied", []*config.Route{route("/a", "GET")}, "HEAD", "/a", "/a", 405, "GET"},
		{"omitted methods", []*config.Route{route("/a")}, "DELETE", "/a", "/a", 0, ""},
		{"empty routes", nil, "GET", "/anything", "", 404, ""},
		{"lowercase request method", sample, "get", "/api/users", "/api/users", 405, "GET, POST"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			r, resp := New(c.routes).Resolve(c.method, c.path)
			got := ""
			if r != nil {
				got = r.Path
			}
			if got != c.wantRoute {
				t.Fatalf("route = %q, want %q", got, c.wantRoute)
			}
			if c.wantStatus == 0 {
				if resp != nil {
					t.Fatalf("unexpected response %d %s", resp.Status, resp.Body)
				}
				return
			}
			if resp == nil || resp.Status != c.wantStatus {
				t.Fatalf("response = %+v, want status %d", resp, c.wantStatus)
			}
			wantCode := map[int]string{404: gateway.CodeNotFound, 405: gateway.CodeMethodNotAllowed}[c.wantStatus]
			if code := errCode(t, resp); code != wantCode {
				t.Fatalf("error = %q, want %q", code, wantCode)
			}
			if allow := resp.Header.Get("Allow"); allow != c.wantAllow {
				t.Fatalf("Allow = %q, want %q", allow, c.wantAllow)
			}
		})
	}
}

func TestAllowListFallbacks(t *testing.T) {
	r := &config.Route{Path: "/x"}
	if got := AllowList(r); len(got) != 7 || !Allowed(r, http.MethodOptions) {
		t.Fatalf("no methods: %v", got)
	}
	r.Methods = map[string]bool{"DELETE": true, "GET": true, "PURGE": true}
	got := AllowList(r)
	if len(got) != 3 || got[0] != "GET" || got[1] != "DELETE" || got[2] != "PURGE" {
		t.Fatalf("map only: %v", got)
	}
}
