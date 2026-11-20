package fixtures

import (
	"reflect"
	"testing"
	"time"

	"gatewaykit/internal/contract"
)

func TestSampleShape(t *testing.T) {
	cfg := Sample()
	if cfg.Port != 8080 {
		t.Errorf("port = %d", cfg.Port)
	}
	var paths []string
	for _, r := range cfg.Routes {
		paths = append(paths, r.Path)
	}
	want := []string{"/api/users", "/api/orders", "/api/products", "/api/legacy", "/api/internal"}
	if !reflect.DeepEqual(paths, want) {
		t.Fatalf("paths = %v", paths)
	}
	for _, r := range cfg.Routes {
		if len(r.Methods) != len(r.MethodList) {
			t.Errorf("%s: Methods and MethodList differ", r.Path)
		}
		for _, m := range r.MethodList {
			if !r.Methods[m] {
				t.Errorf("%s: %s in MethodList but not Methods", r.Path, m)
			}
		}
		if r.RateLimit == nil {
			t.Errorf("%s: no rate limit", r.Path)
		}
		for _, tg := range r.Targets {
			if !tg.Healthy() {
				t.Errorf("%s: target %s starts unhealthy", r.Path, tg.URL)
			}
		}
	}
	if Route("/api/legacy") == nil || Route("/nope") != nil {
		t.Errorf("Route lookup broken")
	}
}

func TestMethodListOrder(t *testing.T) {
	cases := map[string][]string{
		"/api/users":    {"GET", "POST"},
		"/api/orders":   {"GET", "POST", "PUT"},
		"/api/products": {"GET"},
		"/api/legacy":   {"GET", "POST"},
		"/api/internal": {"GET", "POST"},
	}
	for path, want := range cases {
		if got := Route(path).MethodList; !reflect.DeepEqual(got, want) {
			t.Errorf("%s: MethodList = %v, want %v", path, got, want)
		}
	}
}

func TestTargetsAndTimeouts(t *testing.T) {
	cases := []struct {
		path    string
		urls    []string
		weights []int
		balance string
		timeout time.Duration
		strip   bool
	}{
		{"/api/users", []string{"http://localhost:3001"}, []int{1}, contract.BalanceRoundRobin, 30 * time.Second, false},
		{"/api/orders", []string{"http://localhost:3002"}, []int{1}, contract.BalanceRoundRobin, 5 * time.Second, false},
		{"/api/products", []string{"http://localhost:3003", "http://localhost:3004"}, []int{3, 1}, contract.BalanceWeightedRoundRobin, 10 * time.Second, true},
		{"/api/legacy", []string{"http://localhost:3005"}, []int{1}, contract.BalanceRoundRobin, 30 * time.Second, true},
		{"/api/internal", []string{"http://localhost:3006"}, []int{1}, contract.BalanceRoundRobin, 30 * time.Second, false},
	}
	for _, c := range cases {
		r := Route(c.path)
		var urls []string
		var weights []int
		for _, tg := range r.Targets {
			urls = append(urls, tg.URL.String())
			weights = append(weights, tg.Weight)
		}
		if !reflect.DeepEqual(urls, c.urls) || !reflect.DeepEqual(weights, c.weights) {
			t.Errorf("%s: targets %v weights %v", c.path, urls, weights)
		}
		if r.Balance != c.balance || r.Timeout != c.timeout || r.StripPrefix != c.strip {
			t.Errorf("%s: balance %q timeout %v strip %v", c.path, r.Balance, r.Timeout, r.StripPrefix)
		}
	}
}

func TestRateLimits(t *testing.T) {
	global := contract.RateLimit{Requests: 100, Window: 60 * time.Second, Strategy: "fixed_window", Per: "ip"}
	cases := map[string]contract.RateLimit{
		"/api/users":    {Requests: 30, Window: 60 * time.Second, Strategy: "sliding_window", Per: "ip"},
		"/api/orders":   {Requests: 10, Window: 10 * time.Second, Strategy: "fixed_window", Per: "ip"},
		"/api/products": global,
		"/api/legacy":   global,
		"/api/internal": global,
	}
	for path, want := range cases {
		if got := *Route(path).RateLimit; got != want {
			t.Errorf("%s: rate limit %+v, want %+v", path, got, want)
		}
	}
	cfg := Sample()
	if cfg.Routes[2].RateLimit == cfg.Routes[3].RateLimit {
		t.Errorf("inherited limits share one struct")
	}
}

func TestOrdersRetry(t *testing.T) {
	want := &contract.Retry{Attempts: 3, Backoff: "exponential", InitialDelay: time.Second, On: map[int]bool{502: true, 503: true, 504: true}}
	if got := Orders().Retry; !reflect.DeepEqual(got, want) {
		t.Errorf("retry = %+v", got)
	}
	for _, r := range []*contract.Route{Users(), Products(), Legacy(), Internal()} {
		if r.Retry != nil {
			t.Errorf("%s: unexpected retry", r.Path)
		}
	}
}

func TestProductsHealthCheck(t *testing.T) {
	want := &contract.HealthCheck{Path: "/healthz", Interval: 30 * time.Second, UnhealthyThreshold: 3}
	if got := Products().HealthCheck; !reflect.DeepEqual(got, want) {
		t.Errorf("health check = %+v", got)
	}
	for _, r := range []*contract.Route{Users(), Orders(), Legacy(), Internal()} {
		if r.HealthCheck != nil {
			t.Errorf("%s: unexpected health check", r.Path)
		}
	}
}

func TestInternalAuthAndBreaker(t *testing.T) {
	r := Internal()
	wantAuth := &contract.Auth{Type: "api_key", Header: "x-api-key", Keys: []string{"sk_live_abc123", "sk_live_def456"}}
	if !reflect.DeepEqual(r.Auth, wantAuth) {
		t.Errorf("auth = %+v", r.Auth)
	}
	wantCB := &contract.CircuitBreakerConfig{Threshold: 5, Window: 60 * time.Second, Cooldown: 30 * time.Second}
	if !reflect.DeepEqual(r.CircuitBreaker, wantCB) {
		t.Errorf("breaker = %+v", r.CircuitBreaker)
	}
	for _, o := range []*contract.Route{Users(), Orders(), Products(), Legacy()} {
		if o.Auth != nil || o.CircuitBreaker != nil {
			t.Errorf("%s: unexpected auth or breaker", o.Path)
		}
	}
}

func TestLegacyTransforms(t *testing.T) {
	r := Legacy()
	req := r.ReqTransform
	if req == nil || r.RespTransform == nil {
		t.Fatal("legacy transforms missing")
	}
	wantAdd := []contract.HeaderAdd{
		{Name: "x-gateway", Value: contract.Var{Kind: contract.VarLiteral, Text: "gatewaykit"}},
		{Name: "x-request-start", Value: contract.Var{Kind: contract.VarRequestTime}},
	}
	if !reflect.DeepEqual(req.AddHeaders, wantAdd) {
		t.Errorf("request add = %+v", req.AddHeaders)
	}
	if !reflect.DeepEqual(req.RemoveHeaders, []string{"x-debug", "x-internal"}) {
		t.Errorf("request remove = %v", req.RemoveHeaders)
	}
	gw := contract.Var{Kind: contract.VarLiteral, Text: "gateway"}
	rt := contract.Var{Kind: contract.VarRequestTime}
	wantMap := []contract.MappingEntry{
		{Dest: []string{"user", "id"}, Source: []string{"userId"}},
		{Dest: []string{"user", "name"}, Source: []string{"userName"}},
		{Dest: []string{"meta", "source"}, Var: &gw},
		{Dest: []string{"meta", "timestamp"}, Var: &rt},
	}
	if req.Mapping == nil || !reflect.DeepEqual(req.Mapping.Entries, wantMap) {
		t.Errorf("mapping = %+v", req.Mapping)
	}

	resp := r.RespTransform
	if !reflect.DeepEqual(resp.AddHeaders, []contract.HeaderAdd{{Name: "x-served-by", Value: contract.Literal("gatewaykit")}}) {
		t.Errorf("response add = %+v", resp.AddHeaders)
	}
	if !reflect.DeepEqual(resp.RemoveHeaders, []string{"server", "x-powered-by"}) {
		t.Errorf("response remove = %v", resp.RemoveHeaders)
	}
	env := resp.Envelope
	if env == nil || env.Kind != contract.TemplateObject || len(env.Fields) != 2 {
		t.Fatalf("envelope = %+v", env)
	}
	if env.Fields[0].Key != "data" || env.Fields[0].Value.Kind != contract.TemplateVar || env.Fields[0].Value.Var.Kind != contract.VarBody {
		t.Errorf("envelope data = %+v", env.Fields[0])
	}
	meta := env.Fields[1]
	if meta.Key != "gateway_metadata" || meta.Value.Kind != contract.TemplateObject || len(meta.Value.Fields) != 2 {
		t.Fatalf("envelope gateway_metadata = %+v", meta)
	}
	got := []contract.TemplateField{meta.Value.Fields[0], meta.Value.Fields[1]}
	if got[0].Key != "served_at" || got[0].Value.Var.Kind != contract.VarResponseTime ||
		got[1].Key != "route" || got[1].Value.Var.Kind != contract.VarRoutePath {
		t.Errorf("gateway_metadata fields = %+v %+v", got[0].Value, got[1].Value)
	}
	for _, o := range []*contract.Route{Users(), Orders(), Products(), Internal()} {
		if o.ReqTransform != nil || o.RespTransform != nil {
			t.Errorf("%s: unexpected transform", o.Path)
		}
	}
}

func TestFreshTargets(t *testing.T) {
	a, b := Products(), Products()
	a.Targets[0].SetHealthy(false)
	if !b.Targets[0].Healthy() {
		t.Errorf("fixtures share target state")
	}
}
