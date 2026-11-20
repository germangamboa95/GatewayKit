// Package fixtures holds the normalized model of configs/gateway.yaml (design D10).
// Every call returns fresh values, so tests never share target health state.
package fixtures

import (
	"time"

	"gatewaykit/internal/contract"
)

const (
	Port          = 8080
	GlobalTimeout = 30 * time.Second
)

// GlobalRateLimit is the sample's global_rate_limit.
func GlobalRateLimit() *contract.RateLimit {
	return &contract.RateLimit{Requests: 100, Window: 60 * time.Second, Strategy: contract.StrategyFixedWindow, Per: contract.PerIP}
}

// Sample returns the whole normalized sample config, routes in file order.
func Sample() *contract.GatewayConfig {
	return &contract.GatewayConfig{
		Port:   Port,
		Routes: []*contract.Route{Users(), Orders(), Products(), Legacy(), Internal()},
	}
}

// Route returns the sample route with the given path, or nil.
func Route(path string) *contract.Route {
	for _, r := range Sample().Routes {
		if r.Path == path {
			return r
		}
	}
	return nil
}

func Users() *contract.Route {
	return &contract.Route{
		Path:       "/api/users",
		Methods:    methods("GET", "POST"),
		MethodList: []string{"GET", "POST"},
		Targets:    []*contract.Target{target("http://localhost:3001", 1)},
		Balance:    contract.BalanceRoundRobin,
		Timeout:    GlobalTimeout,
		RateLimit:  &contract.RateLimit{Requests: 30, Window: 60 * time.Second, Strategy: contract.StrategySlidingWindow, Per: contract.PerIP},
	}
}

func Orders() *contract.Route {
	return &contract.Route{
		Path:       "/api/orders",
		Methods:    methods("GET", "POST", "PUT"),
		MethodList: []string{"GET", "POST", "PUT"},
		Targets:    []*contract.Target{target("http://localhost:3002", 1)},
		Balance:    contract.BalanceRoundRobin,
		Timeout:    5 * time.Second,
		Retry: &contract.Retry{
			Attempts:     3,
			Backoff:      contract.BackoffExponential,
			InitialDelay: time.Second,
			On:           map[int]bool{502: true, 503: true, 504: true},
		},
		RateLimit: &contract.RateLimit{Requests: 10, Window: 10 * time.Second, Strategy: contract.StrategyFixedWindow, Per: contract.PerIP},
	}
}

func Products() *contract.Route {
	return &contract.Route{
		Path:        "/api/products",
		Methods:     methods("GET"),
		MethodList:  []string{"GET"},
		StripPrefix: true,
		Targets: []*contract.Target{
			target("http://localhost:3003", 3),
			target("http://localhost:3004", 1),
		},
		Balance:     contract.BalanceWeightedRoundRobin,
		Timeout:     10 * time.Second,
		HealthCheck: &contract.HealthCheck{Path: "/healthz", Interval: 30 * time.Second, UnhealthyThreshold: 3},
		RateLimit:   GlobalRateLimit(),
	}
}

func Legacy() *contract.Route {
	reqTime := contract.Var{Kind: contract.VarRequestTime}
	return &contract.Route{
		Path:        "/api/legacy",
		Methods:     methods("GET", "POST"),
		MethodList:  []string{"GET", "POST"},
		StripPrefix: true,
		Targets:     []*contract.Target{target("http://localhost:3005", 1)},
		Balance:     contract.BalanceRoundRobin,
		Timeout:     GlobalTimeout,
		RateLimit:   GlobalRateLimit(),
		ReqTransform: &contract.ReqTransform{
			AddHeaders: []contract.HeaderAdd{
				{Name: "x-gateway", Value: contract.Literal("gatewaykit")},
				{Name: "x-request-start", Value: reqTime},
			},
			RemoveHeaders: []string{"x-debug", "x-internal"},
			Mapping: &contract.Mapping{Entries: []contract.MappingEntry{
				{Dest: []string{"user", "id"}, Source: []string{"userId"}},
				{Dest: []string{"user", "name"}, Source: []string{"userName"}},
				{Dest: []string{"meta", "source"}, Var: &contract.Var{Kind: contract.VarLiteral, Text: "gateway"}},
				{Dest: []string{"meta", "timestamp"}, Var: &contract.Var{Kind: contract.VarRequestTime}},
			}},
		},
		RespTransform: &contract.RespTransform{
			AddHeaders:    []contract.HeaderAdd{{Name: "x-served-by", Value: contract.Literal("gatewaykit")}},
			RemoveHeaders: []string{"server", "x-powered-by"},
			Envelope: object(
				field("data", leaf(contract.Var{Kind: contract.VarBody})),
				field("gateway_metadata", object(
					field("served_at", leaf(contract.Var{Kind: contract.VarResponseTime})),
					field("route", leaf(contract.Var{Kind: contract.VarRoutePath})),
				)),
			),
		},
	}
}

func Internal() *contract.Route {
	return &contract.Route{
		Path:       "/api/internal",
		Methods:    methods("GET", "POST"),
		MethodList: []string{"GET", "POST"},
		Targets:    []*contract.Target{target("http://localhost:3006", 1)},
		Balance:    contract.BalanceRoundRobin,
		Timeout:    GlobalTimeout,
		RateLimit:  GlobalRateLimit(),
		Auth: &contract.Auth{
			Type:   contract.AuthTypeAPIKey,
			Header: "x-api-key",
			Keys:   []string{"sk_live_abc123", "sk_live_def456"},
		},
		CircuitBreaker: &contract.CircuitBreakerConfig{Threshold: 5, Window: 60 * time.Second, Cooldown: 30 * time.Second},
	}
}

func methods(ms ...string) map[string]bool {
	m := make(map[string]bool, len(ms))
	for _, s := range ms {
		m[s] = true
	}
	return m
}

func target(raw string, weight int) *contract.Target {
	t, err := contract.NewTarget(raw, weight)
	if err != nil {
		panic(err)
	}
	return t
}

func object(fields ...contract.TemplateField) *contract.Template {
	return &contract.Template{Kind: contract.TemplateObject, Fields: fields}
}

func field(key string, v *contract.Template) contract.TemplateField {
	return contract.TemplateField{Key: key, Value: v}
}

func leaf(v contract.Var) *contract.Template {
	return &contract.Template{Kind: contract.TemplateVar, Var: v}
}
