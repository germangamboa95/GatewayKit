package config

import (
	"testing"

	"github.com/germangamboa95/gatewaykit/internal/yamlparse"
)

func TestDecodeTyping(t *testing.T) {
	d := newDiag()
	raw := decode(doc(m("port", q("9090")), minimalRoute("strip_prefix", "false")), d)
	if len(d.Errors) != 0 {
		t.Fatal(d.Errors)
	}
	if raw.Port.V != 9090 || raw.Routes[0].StripPrefix.V {
		t.Fatalf("port %v strip %v", raw.Port.V, raw.Routes[0].StripPrefix.V)
	}

	abc := q("abc")
	yes := s("yes")
	get := s("GET")
	d = newDiag()
	decode(doc(m("port", abc), minimalRoute("strip_prefix", yes, "methods", get)), d)
	hasLine(t, d.Errors, loc(abc)+"gateway.port: expected an integer")
	hasLine(t, d.Errors, loc(yes)+"routes[0].strip_prefix: expected true or false")
	hasLine(t, d.Errors, loc(get)+"routes[0].methods: expected a list")
}

func TestDecodeNullIsAbsent(t *testing.T) {
	src := "gateway: {}\nroutes:\n  - path: \"/\"\n    upstream: { url: \"http://localhost:3001\" }\n    strip_prefix: ~\n    auth: null\n    retry:\n"
	root, err := yamlparse.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	d := newDiag()
	raw := decode(root, d)
	r := raw.Routes[0]
	if len(d.Errors) != 0 || r.StripPrefix != nil || r.Auth != nil || r.Retry != nil {
		t.Fatalf("errors %v route %+v", d.Errors, r)
	}
}

func TestDecodeTopLevel(t *testing.T) {
	d := newDiag()
	top := seq("a")
	decode(top, d)
	hasLine(t, d.Errors, loc(top)+"top-level value must be a map")

	d = newDiag()
	decode(m("routes", seq()), d)
	hasLine(t, d.Errors, "gateway: required field is missing")

	d = newDiag()
	decode(m("gateway", m()), d)
	hasLine(t, d.Errors, "routes: required field is missing")
}

func TestDecodeUnknownKeyWarning(t *testing.T) {
	v := q("10s")
	d := newDiag()
	decode(doc(m(), minimalRoute("cache_ttl", v)), d)
	if len(d.Errors) != 0 {
		t.Fatal(d.Errors)
	}
	hasLine(t, d.Warnings, loc(v)+"routes[0].cache_ttl: unknown key")
}

func TestDecodeRoutesPlacement(t *testing.T) {
	d := newDiag()
	raw := decode(m("gateway", m("routes", seq(minimalRoute()))), d)
	if len(d.Errors) != 0 || len(raw.Routes) != 1 || raw.Routes[0].Path != "gateway.routes[0]" {
		t.Fatalf("errors %v raw %+v", d.Errors, raw)
	}

	d = newDiag()
	decode(m("gateway", m("routes", seq(minimalRoute())), "routes", seq(minimalRoute())), d)
	hasLine(t, d.Errors, "routes set both at top level", "under gateway")
}

func TestDecodeUpstreamPlacement(t *testing.T) {
	d := newDiag()
	r := m("path", q("/"), "timeout", q("2s"),
		"upstream", m("url", q("http://h:1"), "circuit_breaker", m("threshold", "5")))
	raw := decode(doc(m(), r), d)
	if len(d.Errors) != 0 {
		t.Fatal(d.Errors)
	}
	rt := raw.Routes[0]
	if rt.Timeout == nil || rt.Timeout.Path != "routes[0].timeout" || rt.CircuitBreaker == nil ||
		rt.CircuitBreaker.Path != "routes[0].upstream.circuit_breaker" {
		t.Fatalf("route %+v", rt)
	}

	d = newDiag()
	r = m("path", q("/"), "retry", m("attempts", "2"),
		"upstream", m("url", q("http://h:1"), "retry", m("attempts", "3")))
	decode(doc(m(), r), d)
	hasLine(t, d.Errors, "routes[0].retry: set both at routes[0].retry", "routes[0].upstream.retry")
}

func TestDecodeRateLimitFalse(t *testing.T) {
	d := newDiag()
	raw := decode(doc(m(), minimalRoute("rate_limit", "false")), d)
	if len(d.Errors) != 0 || raw.Routes[0].RateLimit == nil || !raw.Routes[0].RateLimit.Disabled {
		t.Fatalf("errors %v", d.Errors)
	}

	tr := s("true")
	gf := s("false")
	d = newDiag()
	decode(doc(m("global_rate_limit", gf), minimalRoute("rate_limit", tr)), d)
	hasLine(t, d.Errors, loc(tr)+"routes[0].rate_limit: expected a map or false")
	hasLine(t, d.Errors, loc(gf)+"gateway.global_rate_limit: expected a map")
}

func TestDecodeEnumsAndMethods(t *testing.T) {
	d := newDiag()
	raw := decode(doc(m(), minimalRoute(
		"methods", seq(q("get"), q("delete")),
		"rate_limit", m("strategy", "Sliding_Window"))), d)
	if len(d.Errors) != 0 {
		t.Fatal(d.Errors)
	}
	r := raw.Routes[0]
	if r.Methods[0].V != "GET" || r.Methods[1].V != "DELETE" || r.RateLimit.Strategy.V != "sliding_window" {
		t.Fatalf("route %+v", r)
	}
}

func TestDecodeDurationSyntax(t *testing.T) {
	v := q("5 s")
	r := m("path", q("/"), "upstream", m("url", q("http://h:1"), "timeout", v))
	d := newDiag()
	decode(doc(m(), minimalRoute(), minimalRoute(), r), d)
	hasLine(t, d.Errors, loc(v)+`routes[2].upstream.timeout: invalid duration "5 s"`)
}
