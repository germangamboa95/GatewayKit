package config

import (
	"reflect"
	"strings"
	"testing"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/yamlparse"
)

func sampleTree() *yamlparse.Node {
	return m(
		"gateway", m("port", "8080", "global_timeout", q("30s"),
			"global_rate_limit", m("requests", "100", "window", q("60s"), "strategy", q("fixed_window"), "per", q("ip"))),
		"routes", seq(
			m("path", q("/api/users"), "methods", seq(q("GET"), q("POST")), "strip_prefix", "false",
				"upstream", m("url", q("http://localhost:3001")),
				"rate_limit", m("requests", "30", "window", q("60s"), "strategy", q("sliding_window"), "per", q("ip"))),
			m("path", q("/api/orders"), "methods", seq(q("GET"), q("POST"), q("PUT")), "strip_prefix", "false",
				"upstream", m("url", q("http://localhost:3002"), "timeout", q("5s"),
					"retry", m("attempts", "3", "backoff", q("exponential"), "initial_delay", q("1s"),
						"on", seq("502", "503", "504"))),
				"rate_limit", m("requests", "10", "window", q("10s"), "strategy", q("fixed_window"), "per", q("ip"))),
			m("path", q("/api/products"), "methods", seq(q("GET")), "strip_prefix", "true",
				"upstream", m(
					"targets", seq(m("url", q("http://localhost:3003"), "weight", "3"),
						m("url", q("http://localhost:3004"), "weight", "1")),
					"balance", q("weighted_round_robin"), "timeout", q("10s"),
					"health_check", m("path", q("/healthz"), "interval", q("30s"), "unhealthy_threshold", "3"))),
			m("path", q("/api/legacy"), "methods", seq(q("GET"), q("POST")), "strip_prefix", "true",
				"upstream", m("url", q("http://localhost:3005")),
				"request_transform", m(
					"headers", m("add", m("X-Gateway", q("gatewaykit"), "X-Request-Start", q("$request_time")),
						"remove", seq(q("X-Debug"), q("X-Internal"))),
					"body", m("mapping", m("user.id", q("userId"), "user.name", q("userName"),
						"meta.source", q("$literal:gateway"), "meta.timestamp", q("$request_time")))),
				"response_transform", m(
					"headers", m("add", m("X-Served-By", q("gatewaykit")), "remove", seq(q("Server"), q("X-Powered-By"))),
					"body", m("envelope", m("data", q("$body"),
						"gateway_metadata", m("served_at", q("$response_time"), "route", q("$route_path")))))),
			m("path", q("/api/internal"), "methods", seq(q("GET"), q("POST")), "strip_prefix", "false",
				"upstream", m("url", q("http://localhost:3006")),
				"auth", m("type", q("api_key"), "header", q("X-API-Key"), "keys", seq(q("sk_live_abc123"), q("sk_live_def456"))),
				"circuit_breaker", m("threshold", "5", "window", q("60s"), "cooldown", q("30s"))),
		),
	)
}

func TestNormalizeSample(t *testing.T) {
	cfg, warns, err := loadNode("gateway.yaml", sampleTree())
	if err != nil || len(warns) != 0 {
		t.Fatalf("err %v warns %v", err, warns)
	}
	want := sample()
	if cfg.Port != want.Port || len(cfg.Routes) != len(want.Routes) {
		t.Fatalf("port %d routes %d", cfg.Port, len(cfg.Routes))
	}
	for i := range want.Routes {
		if !reflect.DeepEqual(cfg.Routes[i], want.Routes[i]) {
			t.Errorf("route %d:\n got %+v\nwant %+v", i, cfg.Routes[i], want.Routes[i])
		}
	}
}

func load(t *testing.T, root *yamlparse.Node) *Gateway {
	t.Helper()
	cfg, _, err := loadNode("gateway.yaml", root)
	if err != nil {
		t.Fatal(err)
	}
	return cfg
}

func TestNormalizeConfigADefaults(t *testing.T) {
	cfg := load(t, doc(m(), route("/", up("url", q("http://localhost:3001")))))
	r := cfg.Routes[0]
	if cfg.Port != 8080 || r.Path != "/" || r.Timeout != 30*time.Second || r.RateLimit != nil ||
		r.Balance != BalanceRoundRobin || r.StripPrefix ||
		!reflect.DeepEqual(r.MethodList, DefaultMethods) || len(r.Methods) != 7 ||
		len(r.Targets) != 1 || r.Targets[0].URL.String() != "http://localhost:3001" || r.Targets[0].Weight != 1 ||
		r.Retry != nil || r.HealthCheck != nil || r.Auth != nil || r.CircuitBreaker != nil ||
		r.ReqTransform != nil || r.RespTransform != nil {
		t.Fatalf("route %+v", r)
	}

	cfg = load(t, m("gateway", m(), "routes", seq()))
	if cfg.Port != 8080 || len(cfg.Routes) != 0 {
		t.Fatalf("cfg %+v", cfg)
	}
}

func TestNormalizeFeatureDefaults(t *testing.T) {
	u := up("url", q("http://h:1"))
	cfg := load(t, doc(m(), route("/", u,
		"retry", m("attempts", "2"),
		"health_check", m("path", q("/healthz")),
		"circuit_breaker", m("threshold", "5"),
		"auth", m("type", "API_KEY", "keys", seq(q("k1"))))))
	r := cfg.Routes[0]
	want := &Retry{Attempts: 2, Backoff: "fixed", InitialDelay: time.Second, On: map[int]bool{502: true, 503: true, 504: true}}
	if !reflect.DeepEqual(r.Retry, want) {
		t.Errorf("retry %+v", r.Retry)
	}
	if *r.HealthCheck != (HealthCheck{Path: "/healthz", Interval: 30 * time.Second, UnhealthyThreshold: 3}) {
		t.Errorf("hc %+v", r.HealthCheck)
	}
	if *r.CircuitBreaker != (CircuitBreaker{Threshold: 5, Window: 60 * time.Second, Cooldown: 30 * time.Second}) {
		t.Errorf("cb %+v", r.CircuitBreaker)
	}
	if r.Auth.Header != "x-api-key" || r.Auth.Type != "api_key" {
		t.Errorf("auth %+v", r.Auth)
	}
}

func TestNormalizeRateLimitMerge(t *testing.T) {
	g := func() *yamlparse.Node {
		return m("global_rate_limit", m("requests", "100", "window", q("60s"), "per", "global"))
	}
	u := func() *yamlparse.Node { return up("url", q("http://h:1")) }
	cfg := load(t, doc(g(),
		route("/a", u(), "rate_limit", m("requests", "30")),
		route("/b", u()),
		route("/c", u(), "rate_limit", "false")))
	want := RateLimit{Requests: 30, Window: time.Minute, Strategy: "fixed_window", Per: "global"}
	if *cfg.Routes[0].RateLimit != want {
		t.Errorf("partial %+v", cfg.Routes[0].RateLimit)
	}
	want.Requests = 100
	if *cfg.Routes[1].RateLimit != want {
		t.Errorf("inherited %+v", cfg.Routes[1].RateLimit)
	}
	if cfg.Routes[2].RateLimit != nil {
		t.Errorf("disabled %+v", cfg.Routes[2].RateLimit)
	}
}

func TestNormalizeTimeoutPrecedence(t *testing.T) {
	cfg := load(t, doc(m("global_timeout", q("750ms")),
		route("/a", up("url", q("http://h:1"), "timeout", q("5s"))),
		route("/b", up("url", q("http://h:1")), "timeout", q("2s")),
		route("/c", up("url", q("http://h:1")))))
	for i, want := range []time.Duration{5 * time.Second, 2 * time.Second, 750 * time.Millisecond} {
		if cfg.Routes[i].Timeout != want {
			t.Errorf("route %d timeout %v want %v", i, cfg.Routes[i].Timeout, want)
		}
	}
}

func TestNormalizeMethodsAndPaths(t *testing.T) {
	cfg := load(t, doc(m(),
		route("/v2/", up("url", q("http://h:1/base")), "methods", seq(q("get"), q("GET"), q("delete")))))
	r := cfg.Routes[0]
	if r.Path != "/v2" || !reflect.DeepEqual(r.MethodList, []string{"GET", "DELETE"}) || len(r.Methods) != 2 {
		t.Fatalf("route %+v", r)
	}
	if r.Targets[0].URL.Path != "/base" {
		t.Errorf("base path %q", r.Targets[0].URL.Path)
	}
}

func TestNormalizeTransforms(t *testing.T) {
	u := func() *yamlparse.Node { return up("url", q("http://h:1")) }
	cfg := load(t, doc(m(), route("/", u(),
		"request_transform", m("headers", m("add", m("X-V", q("v=$request_time")), "remove", seq(q("X-Debug")))),
		"response_transform", m("body", m("envelope", m("n", "3", "ok", "true", "z", nil, "s", "plain", "list", seq(q("$body"), "1")))))))
	r := cfg.Routes[0]
	if got := r.ReqTransform.AddHeaders[0]; got.Name != "x-v" || got.Value != Literal("v=$request_time") {
		t.Errorf("add %+v", got)
	}
	if !reflect.DeepEqual(r.ReqTransform.RemoveHeaders, []string{"x-debug"}) {
		t.Errorf("remove %v", r.ReqTransform.RemoveHeaders)
	}
	f := r.RespTransform.Envelope.Fields
	if f[0].Value.Raw != "3" || f[1].Value.Raw != "true" || f[2].Value.Raw != "null" ||
		f[3].Value.Var != Literal("plain") || f[4].Value.Items[0].Var.Kind != VarBody ||
		f[4].Value.Items[1].Raw != "1" {
		t.Errorf("envelope %+v", f)
	}

	cases := []struct {
		name string
		rt   *yamlparse.Node
		want string
	}{
		{"response var in request", m("request_transform", m("headers", m("add", m("X", q("$response_time"))))), "not allowed in a request transform"},
		{"body in header", m("response_transform", m("headers", m("add", m("X", q("$body"))))), "only allowed in response_transform.body.envelope"},
		{"body in mapping", m("request_transform", m("body", m("mapping", m("a", q("$body"))))), "only allowed"},
		{"envelope typo", m("response_transform", m("body", m("envelope", m("t", q("$respnse_time"))))), "is not a known variable"},
		{"mapping conflict", m("request_transform", m("body", m("mapping", m("a", q("x"), "a.b", q("y"))))), `destination "a.b" conflicts with "a"`},
	}
	for _, c := range cases {
		kv := []any{}
		for _, k := range c.rt.Keys {
			kv = append(kv, k, c.rt.Map[k])
		}
		d := run(doc(m(), route("/", u(), kv...)))
		if len(d.Errors) != 1 || !strings.Contains(d.Errors[0], c.want) {
			t.Errorf("%s: errors %v", c.name, d.Errors)
		}
	}
}
