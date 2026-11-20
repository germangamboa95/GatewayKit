// The normalized model of configs/gateway.yaml, which load and normalize
// tests compare against.
package config

import (
	"time"
)

const (
	samplePort          = 8080
	sampleGlobalTimeout = 30 * time.Second
)

// sampleGlobalRateLimit is the sample's global_rate_limit.
func sampleGlobalRateLimit() *RateLimit {
	return &RateLimit{Requests: 100, Window: 60 * time.Second, Strategy: StrategyFixedWindow, Per: PerIP}
}

// sample returns the whole normalized sample config, routes in file order.
func sample() *Gateway {
	return &Gateway{
		Port:   samplePort,
		Routes: []*Route{sampleUsers(), sampleOrders(), sampleProducts(), sampleLegacy(), sampleInternal()},
	}
}

// sampleRoute returns the sample route with the given path, or nil.
func sampleRoute(path string) *Route {
	for _, r := range sample().Routes {
		if r.Path == path {
			return r
		}
	}
	return nil
}

func sampleUsers() *Route {
	return &Route{
		Path:       "/api/users",
		Methods:    methods("GET", "POST"),
		MethodList: []string{"GET", "POST"},
		Targets:    []*Target{target("http://localhost:3001", 1)},
		Balance:    BalanceRoundRobin,
		Timeout:    sampleGlobalTimeout,
		RateLimit:  &RateLimit{Requests: 30, Window: 60 * time.Second, Strategy: StrategySlidingWindow, Per: PerIP},
	}
}

func sampleOrders() *Route {
	return &Route{
		Path:       "/api/orders",
		Methods:    methods("GET", "POST", "PUT"),
		MethodList: []string{"GET", "POST", "PUT"},
		Targets:    []*Target{target("http://localhost:3002", 1)},
		Balance:    BalanceRoundRobin,
		Timeout:    5 * time.Second,
		Retry: &Retry{
			Attempts:     3,
			Backoff:      BackoffExponential,
			InitialDelay: time.Second,
			On:           map[int]bool{502: true, 503: true, 504: true},
		},
		RateLimit: &RateLimit{Requests: 10, Window: 10 * time.Second, Strategy: StrategyFixedWindow, Per: PerIP},
	}
}

func sampleProducts() *Route {
	return &Route{
		Path:        "/api/products",
		Methods:     methods("GET"),
		MethodList:  []string{"GET"},
		StripPrefix: true,
		Targets: []*Target{
			target("http://localhost:3003", 3),
			target("http://localhost:3004", 1),
		},
		Balance:     BalanceWeightedRoundRobin,
		Timeout:     10 * time.Second,
		HealthCheck: &HealthCheck{Path: "/healthz", Interval: 30 * time.Second, UnhealthyThreshold: 3},
		RateLimit:   sampleGlobalRateLimit(),
	}
}

func sampleLegacy() *Route {
	reqTime := Var{Kind: VarRequestTime}
	return &Route{
		Path:        "/api/legacy",
		Methods:     methods("GET", "POST"),
		MethodList:  []string{"GET", "POST"},
		StripPrefix: true,
		Targets:     []*Target{target("http://localhost:3005", 1)},
		Balance:     BalanceRoundRobin,
		Timeout:     sampleGlobalTimeout,
		RateLimit:   sampleGlobalRateLimit(),
		ReqTransform: &ReqTransform{
			AddHeaders: []HeaderAdd{
				{Name: "x-gateway", Value: Literal("gatewaykit")},
				{Name: "x-request-start", Value: reqTime},
			},
			RemoveHeaders: []string{"x-debug", "x-internal"},
			Mapping: &Mapping{Entries: []MappingEntry{
				{Dest: []string{"user", "id"}, Source: []string{"userId"}},
				{Dest: []string{"user", "name"}, Source: []string{"userName"}},
				{Dest: []string{"meta", "source"}, Var: &Var{Kind: VarLiteral, Text: "gateway"}},
				{Dest: []string{"meta", "timestamp"}, Var: &Var{Kind: VarRequestTime}},
			}},
		},
		RespTransform: &RespTransform{
			AddHeaders:    []HeaderAdd{{Name: "x-served-by", Value: Literal("gatewaykit")}},
			RemoveHeaders: []string{"server", "x-powered-by"},
			Envelope: object(
				field("data", leaf(Var{Kind: VarBody})),
				field("gateway_metadata", object(
					field("served_at", leaf(Var{Kind: VarResponseTime})),
					field("route", leaf(Var{Kind: VarRoutePath})),
				)),
			),
		},
	}
}

func sampleInternal() *Route {
	return &Route{
		Path:       "/api/internal",
		Methods:    methods("GET", "POST"),
		MethodList: []string{"GET", "POST"},
		Targets:    []*Target{target("http://localhost:3006", 1)},
		Balance:    BalanceRoundRobin,
		Timeout:    sampleGlobalTimeout,
		RateLimit:  sampleGlobalRateLimit(),
		Auth: &Auth{
			Type:   AuthTypeAPIKey,
			Header: "x-api-key",
			Keys:   []string{"sk_live_abc123", "sk_live_def456"},
		},
		CircuitBreaker: &CircuitBreaker{Threshold: 5, Window: 60 * time.Second, Cooldown: 30 * time.Second},
	}
}

func methods(ms ...string) map[string]bool {
	m := make(map[string]bool, len(ms))
	for _, s := range ms {
		m[s] = true
	}
	return m
}

func target(raw string, weight int) *Target {
	t, err := NewTarget(raw, weight)
	if err != nil {
		panic(err)
	}
	return t
}

func object(fields ...TemplateField) *Template {
	return &Template{Kind: TemplateObject, Fields: fields}
}

func field(key string, v *Template) TemplateField {
	return TemplateField{Key: key, Value: v}
}

func leaf(v Var) *Template {
	return &Template{Kind: TemplateVar, Var: v}
}
