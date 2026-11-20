package proxy

import "testing"

func TestRewritePath(t *testing.T) {
	cases := []struct {
		route, base, path, query string
		strip                    bool
		want                     string
	}{
		{"/api/products", "", "/api/products/123", "x=1", false, "/api/products/123?x=1"},
		{"/api/products", "", "/api/products/123", "x=1", true, "/123?x=1"},
		{"/api/products", "", "/api/products", "", false, "/api/products"},
		{"/api/products", "", "/api/products", "", true, "/"},
		{"/api/products", "", "/api/products/", "", true, "/"},
		{"/api/products", "", "/api/products", "a=b&c=d", true, "/?a=b&c=d"},
		{"/api/legacy", "", "/api/legacy/v1/data", "", false, "/api/legacy/v1/data"},
		{"/api/legacy", "", "/api/legacy/v1/data", "", true, "/v1/data"},
		{"/svc", "/base", "/svc/a", "", false, "/base/svc/a"},
		{"/svc", "/base", "/svc/a", "", true, "/base/a"},
		{"/svc", "/base/", "/svc/a", "", true, "/base/a"},
		{"/svc", "/base", "/svc", "q=1", true, "/base/?q=1"},
		{"/svc", "/base", "/svc//a", "", true, "/base/a"},
		{"/", "", "/x/y", "", true, "/x/y"},
		{"/", "/b", "/", "", false, "/b/"},
		{"/a", "", "/a//b", "", false, "/a//b"},
	}
	for _, c := range cases {
		got := JoinPath(c.base, RewritePath(c.route, c.strip, c.path, c.query))
		if got != c.want {
			t.Errorf("route %q base %q path %q strip %v: got %q want %q", c.route, c.base, c.path, c.strip, got, c.want)
		}
	}
}
