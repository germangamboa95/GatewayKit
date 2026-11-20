package config

import (
	"strings"
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/yamlparse"
)

func run(root *yamlparse.Node) *diag {
	d := newDiag()
	raw := decode(root, d)
	validate(raw, d)
	normalize(raw, d)
	return d
}

func expectErr(t *testing.T, name string, root *yamlparse.Node, at *yamlparse.Node, want ...string) {
	t.Helper()
	d := run(root)
	if at != nil {
		want = append([]string{loc(at)}, want...)
	}
	if len(d.Errors) == 0 {
		t.Errorf("%s: no errors", name)
		return
	}
	for _, l := range d.Errors {
		ok := true
		for _, w := range want {
			if !strings.Contains(l, w) {
				ok = false
			}
		}
		if ok {
			return
		}
	}
	t.Errorf("%s: want %q in:\n%s", name, want, strings.Join(d.Errors, "\n"))
}

func expectOK(t *testing.T, name string, root *yamlparse.Node) {
	t.Helper()
	if d := run(root); len(d.Errors) != 0 {
		t.Errorf("%s: unexpected errors:\n%s", name, strings.Join(d.Errors, "\n"))
	}
}

func up(kv ...any) *yamlparse.Node { return m(kv...) }

func route(path string, upstream *yamlparse.Node, extra ...any) *yamlparse.Node {
	return m(append([]any{"path", q(path), "upstream", upstream}, extra...)...)
}

func TestValidateRules(t *testing.T) {
	u := func() *yamlparse.Node { return up("url", q("http://localhost:3001")) }

	n := s("70000")
	expectErr(t, "port range", doc(m("port", n)), n, "gateway.port: must be an integer from 1 to 65535 (got 70000)")
	n = q("0s")
	expectErr(t, "zero global timeout", doc(m("global_timeout", n)), n, "gateway.global_timeout")

	n = q("ftp://localhost:3001")
	expectErr(t, "url scheme", doc(m(), route("/", up("url", n))), n, "routes[0].upstream.url")
	n = q("http://localhost:3001?x=1")
	expectErr(t, "url query", doc(m(), route("/", up("url", n))), n, "query string")
	expectOK(t, "url base path", doc(m(), route("/", up("url", q("http://localhost:4001/base")))))

	n = q("/api/*")
	expectErr(t, "wildcard path", doc(m(), m("path", n, "upstream", u())), n, "routes[0].path")
	n = q("api/users")
	expectErr(t, "leading slash", doc(m(), m("path", n, "upstream", u())), n, "routes[0].path")
	n = q("/users/:id")
	expectErr(t, "param path", doc(m(), m("path", n, "upstream", u())), n, "routes[0].path")
	expectErr(t, "null path", doc(m(), m("path", nil, "upstream", u())), nil, "routes[0].path: required field is missing")
	expectErr(t, "missing upstream", doc(m(), m("path", q("/"))), nil, "routes[0].upstream: required field is missing")

	n = q("/api/")
	expectErr(t, "duplicate path", doc(m(), route("/api", u()), m("path", n, "upstream", u())), n,
		"routes[1].path: duplicate route path \"/api\"")

	n = seq()
	expectErr(t, "empty methods", doc(m(), route("/", u(), "methods", n)), n, "routes[0].methods: must be a non-empty list")
	n = q("FETCH")
	expectErr(t, "unknown method", doc(m(), route("/", u(), "methods", seq(n))), n, "routes[0].methods[0]")

	expectErr(t, "neither url nor targets", doc(m(), route("/", u()), route("/b", up("balance", "round_robin"))), nil,
		`routes[1].upstream: exactly one of "url" or "targets" is required`)
	expectErr(t, "both url and targets", doc(m(), route("/", up("url", q("http://h:1"), "targets", seq(m("url", q("http://h:2")))))), nil,
		`routes[0].upstream: exactly one of "url" or "targets" is required`)
	n = s("0")
	expectErr(t, "zero weight", doc(m(), route("/a", u()), route("/b", u()),
		route("/c", up("targets", seq(m("url", q("http://h:1"), "weight", n), m("url", q("http://h:2")))))), n,
		"routes[2].upstream.targets[0].weight: must be an integer >= 1 (got 0)")
	n = q("http://h:1")
	expectErr(t, "duplicate target", doc(m(), route("/", up("targets", seq(m("url", q("http://h:1")), m("url", n))))), n,
		"routes[0].upstream.targets[1].url: duplicate target URL")
	n = seq()
	expectErr(t, "empty targets", doc(m(), route("/", up("targets", n))), n, "routes[0].upstream.targets")
	n = q("random")
	expectErr(t, "unknown balance", doc(m(), route("/", up("url", q("http://h:1"), "balance", n))), n, "routes[0].upstream.balance")

	expectErr(t, "retry attempts", doc(m(), route("/", u(), "retry", m("backoff", "fixed"))), nil, "routes[0].retry.attempts: required")
	expectOK(t, "zero initial delay", doc(m(), route("/", u(), "retry", m("attempts", "2", "initial_delay", q("0s")))))
	n = seq()
	expectErr(t, "empty on", doc(m(), route("/", u(), "retry", m("attempts", "2", "on", n))), n, "routes[0].retry.on")
	n = s("600")
	expectErr(t, "status range", doc(m(), route("/", u(), "retry", m("attempts", "2", "on", seq(n)))), n, "routes[0].retry.on[0]")
	n = q("linear")
	expectErr(t, "unknown backoff", doc(m(), route("/", u(), "retry", m("attempts", "2", "backoff", n))), n, "routes[0].retry.backoff")

	expectErr(t, "hc path", doc(m(), route("/", u(), "health_check", m("interval", q("5s")))), nil, "routes[0].health_check.path: required")
	n = s("0")
	expectErr(t, "hc threshold", doc(m(), route("/", u(), "health_check", m("path", q("/h"), "unhealthy_threshold", n))), n, "unhealthy_threshold")

	expectErr(t, "cb threshold", doc(m(), route("/", u(), "circuit_breaker", m("window", q("5s")))), nil, "routes[0].circuit_breaker.threshold: required")

	n = s("0")
	expectErr(t, "zero requests", doc(m(), route("/", u(), "rate_limit", m("requests", n, "window", q("1s")))), n, "routes[0].rate_limit.requests")
	n = s("user")
	expectErr(t, "unknown per", doc(m(), route("/", u(), "rate_limit", m("requests", "1", "window", q("1s"), "per", n))), n, "routes[0].rate_limit.per")
	n = s("leaky")
	expectErr(t, "unknown strategy", doc(m("global_rate_limit", m("requests", "1", "window", q("1s"), "strategy", n))), n, "gateway.global_rate_limit.strategy")
	expectErr(t, "incomplete merge", doc(m(), route("/", u(), "rate_limit", m("requests", "30"))), nil,
		`routes[0].rate_limit: no "window" after merging`)

	n = s("bearer")
	expectErr(t, "auth type", doc(m(), route("/", u(), "auth", m("type", n, "keys", seq(q("k"))))), n, "routes[0].auth.type")
	n = q("")
	expectErr(t, "empty key", doc(m(), route("/", u(), "auth", m("type", "api_key", "keys", seq(n)))), n, "routes[0].auth.keys[0]")
	expectErr(t, "missing keys", doc(m(), route("/", u(), "auth", m("type", "api_key"))), nil, "routes[0].auth.keys: required")
}

func TestValidateMultiError(t *testing.T) {
	timeX := q("$requst_time")
	w := s("0")
	root := doc(m(),
		route("/a", up("url", q("http://h:1"))),
		route("/b", up("balance", "round_robin")),
		route("/c", up("targets", seq(m("url", q("http://h:1"), "weight", w), m("url", q("http://h:2"))))),
		route("/d", up("url", q("http://h:1")), "response_transform", m("headers", m("add", m("X-Time", timeX)))),
	)
	_, _, err := loadNode("gateway.yaml", root)
	le, ok := err.(*LoadError)
	if !ok {
		t.Fatalf("err = %v", err)
	}
	if len(le.Problems) != 3 || le.Summary() != "config error: 3 problems in gateway.yaml" {
		t.Fatalf("summary %q problems:\n%s", le.Summary(), le.Error())
	}
	lines := strings.Split(le.Error(), "\n")
	hasLine(t, lines, `routes[1].upstream: exactly one of "url" or "targets" is required`)
	hasLine(t, lines, loc(w)+"routes[2].upstream.targets[0].weight: must be an integer >= 1 (got 0)")
	hasLine(t, lines, loc(timeX)+`routes[3].response_transform.headers.add.X-Time: "$requst_time" is not a known variable`)
	for _, l := range lines {
		if !strings.HasPrefix(l, "gateway.yaml:") {
			t.Errorf("line %q lacks location", l)
		}
	}
}

func TestWarnings(t *testing.T) {
	u := func() *yamlparse.Node { return up("url", q("http://localhost:3001")) }

	p := q("/health")
	d := run(doc(m(), m("path", p, "upstream", u())))
	if len(d.Errors) != 0 {
		t.Fatal(d.Errors)
	}
	hasLine(t, d.Warnings, loc(p)+"routes[0].path: route never receives GET /health")

	b := s("weighted_round_robin")
	d = run(doc(m(), route("/", up("url", q("http://h:1"), "balance", b))))
	hasLine(t, d.Warnings, loc(b)+"routes[0].upstream.balance: has no effect with only one target")

	w := s("3")
	d = run(doc(m(), route("/", up("targets", seq(m("url", q("http://h:1"), "weight", w))))))
	hasLine(t, d.Warnings, loc(w)+"routes[0].upstream.targets[0].weight: has no effect")

	h := q("x")
	rm := q("Content-Length")
	d = run(doc(m(), route("/", u(), "request_transform", m("headers", m("add", m("Host", h), "remove", seq(rm))))))
	if len(d.Errors) != 0 {
		t.Fatal(d.Errors)
	}
	hasLine(t, d.Warnings, loc(h)+"routes[0].request_transform.headers.add.Host: header \"Host\" is managed")
	hasLine(t, d.Warnings, loc(rm), "header \"Content-Length\" is managed")

	k := q("10s")
	d = run(doc(m(), route("/", u(), "cache_ttl", k)))
	hasLine(t, d.Warnings, loc(k)+"routes[0].cache_ttl: unknown key")
}
