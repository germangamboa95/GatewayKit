package config

import (
	"encoding/json"
	"strings"
	"time"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/yamlparse"
)

const (
	DefaultPort        = 8080
	DefaultTimeout     = 30 * time.Second
	DefaultRetryDelay  = time.Second
	DefaultHCInterval  = 30 * time.Second
	DefaultHCThreshold = 3
	DefaultCBWindow    = 60 * time.Second
	DefaultCBCooldown  = 30 * time.Second
	DefaultAuthHeader  = "X-API-Key"
)

// DefaultRetryOn is the retry status list when "on" is absent.
var DefaultRetryOn = []int{502, 503, 504}

// managedHeaders are headers transforms may not touch (config spec §9).
var managedHeaders = map[string]bool{
	"host": true, "content-length": true, "transfer-encoding": true,
	"connection": true, "keep-alive": true, "te": true, "trailer": true, "upgrade": true,
}

func isManaged(name string) bool {
	n := strings.ToLower(name)
	return managedHeaders[n] || strings.HasPrefix(n, "proxy-")
}

// Normalize fills defaults, merges rate limits, builds targets and compiles
// transforms. Compile errors and transform warnings go to d. It tolerates a
// raw config that failed validation; callers must check d before using the
// result.
func Normalize(raw *Raw, d *Diag) *contract.GatewayConfig {
	if raw == nil {
		return nil
	}
	cfg := &contract.GatewayConfig{Port: DefaultPort, Routes: []*contract.Route{}}
	if raw.Port != nil {
		cfg.Port = raw.Port.V
	}
	global := DefaultTimeout
	if raw.GlobalTimeout != nil {
		global = raw.GlobalTimeout.V
	}
	for _, rr := range raw.Routes {
		cfg.Routes = append(cfg.Routes, normalizeRoute(rr, raw, global, d))
	}
	return cfg
}

func normalizeRoute(rr *RawRoute, raw *Raw, global time.Duration, d *Diag) *contract.Route {
	r := &contract.Route{Balance: contract.BalanceRoundRobin, Timeout: global, Methods: map[string]bool{}}
	if rr.RoutePath != nil {
		r.Path = NormalizePath(rr.RoutePath.V)
	}
	methods := contract.DefaultMethods
	if rr.MethodsNode != nil {
		methods = nil
		for _, m := range rr.Methods {
			methods = append(methods, m.V)
		}
	}
	for _, m := range methods {
		if !r.Methods[m] {
			r.Methods[m] = true
			r.MethodList = append(r.MethodList, m)
		}
	}
	if rr.StripPrefix != nil {
		r.StripPrefix = rr.StripPrefix.V
	}
	if u := rr.Upstream; u != nil {
		if u.Balance != nil {
			r.Balance = u.Balance.V
		}
		if u.URL != nil {
			if t, err := contract.NewTarget(u.URL.V, 1); err == nil {
				r.Targets = append(r.Targets, t)
			}
		}
		for _, rt := range u.Targets {
			w := 1
			if rt.Weight != nil {
				w = rt.Weight.V
			}
			if rt.URL == nil {
				continue
			}
			if t, err := contract.NewTarget(rt.URL.V, w); err == nil {
				r.Targets = append(r.Targets, t)
			}
		}
	}
	if rr.Timeout != nil {
		r.Timeout = rr.Timeout.V
	}
	if rt := rr.Retry; rt != nil {
		x := &contract.Retry{Backoff: contract.BackoffFixed, InitialDelay: DefaultRetryDelay, On: map[int]bool{}}
		if rt.Attempts != nil {
			x.Attempts = rt.Attempts.V
		}
		if rt.Backoff != nil {
			x.Backoff = rt.Backoff.V
		}
		if rt.InitialDelay != nil {
			x.InitialDelay = rt.InitialDelay.V
		}
		if rt.OnNode != nil {
			for _, s := range rt.On {
				x.On[s.V] = true
			}
		} else {
			for _, s := range DefaultRetryOn {
				x.On[s] = true
			}
		}
		r.Retry = x
	}
	if hc := rr.HealthCheck; hc != nil {
		x := &contract.HealthCheck{Interval: DefaultHCInterval, UnhealthyThreshold: DefaultHCThreshold}
		if hc.HCPath != nil {
			x.Path = hc.HCPath.V
		}
		if hc.Interval != nil {
			x.Interval = hc.Interval.V
		}
		if hc.UnhealthyThreshold != nil {
			x.UnhealthyThreshold = hc.UnhealthyThreshold.V
		}
		r.HealthCheck = x
	}
	if cb := rr.CircuitBreaker; cb != nil {
		x := &contract.CircuitBreakerConfig{Window: DefaultCBWindow, Cooldown: DefaultCBCooldown}
		if cb.Threshold != nil {
			x.Threshold = cb.Threshold.V
		}
		if cb.Window != nil {
			x.Window = cb.Window.V
		}
		if cb.Cooldown != nil {
			x.Cooldown = cb.Cooldown.V
		}
		r.CircuitBreaker = x
	}
	r.RateLimit = mergeRateLimit(rr.RateLimit, raw.GlobalRateLimit)
	if a := rr.Auth; a != nil {
		x := &contract.Auth{Type: contract.AuthTypeAPIKey, Header: strings.ToLower(DefaultAuthHeader)}
		if a.Type != nil {
			x.Type = a.Type.V
		}
		if a.Header != nil {
			x.Header = strings.ToLower(a.Header.V)
		}
		for _, k := range a.Keys {
			x.Keys = append(x.Keys, k.V)
		}
		r.Auth = x
	}
	if t := rr.ReqTransform; t != nil {
		add, remove := compileHeaders(t, d)
		x := &contract.ReqTransform{AddHeaders: add, RemoveHeaders: remove}
		if t.MappingNode != nil {
			x.Mapping = compileMapping(t, d)
		}
		r.ReqTransform = x
	}
	if t := rr.RespTransform; t != nil {
		add, remove := compileHeaders(t, d)
		x := &contract.RespTransform{AddHeaders: add, RemoveHeaders: remove}
		if t.Envelope != nil {
			x.Envelope = compileTemplate(t.Envelope, t.EnvelopePath, d)
		}
		r.RespTransform = x
	}
	return r
}

func mergeRateLimit(rl, g *RawRateLimit) *contract.RateLimit {
	if rl != nil && rl.Disabled || rl == nil && g == nil {
		return nil
	}
	x := &contract.RateLimit{Strategy: contract.StrategyFixedWindow, Per: contract.PerIP}
	for _, src := range []*RawRateLimit{g, rl} {
		if src == nil {
			continue
		}
		if src.Requests != nil {
			x.Requests = src.Requests.V
		}
		if src.Window != nil {
			x.Window = src.Window.V
		}
		if src.Strategy != nil {
			x.Strategy = src.Strategy.V
		}
		if src.Per != nil {
			x.Per = src.Per.V
		}
	}
	return x
}

// compileVar parses a transform value and checks where it may appear.
func compileVar(s *Str, request, envelope bool, d *Diag) (contract.Var, bool) {
	v, err := contract.ParseVar(s.V)
	if err != nil {
		d.Errorf(s.N, s.Path, "%s", err.Error())
		return v, false
	}
	switch {
	case v.Kind == contract.VarResponseTime && request:
		d.Errorf(s.N, s.Path, "%q is not allowed in a request transform", s.V)
		return v, false
	case v.Kind == contract.VarBody && !envelope:
		d.Errorf(s.N, s.Path, "%q is only allowed in response_transform.body.envelope", s.V)
		return v, false
	}
	return v, true
}

func compileHeaders(t *RawTransform, d *Diag) ([]contract.HeaderAdd, []string) {
	var add []contract.HeaderAdd
	var remove []string
	for _, h := range t.Add {
		v, ok := compileVar(h.Value, t.Request, false, d)
		if isManaged(h.Name) {
			d.Warnf(h.Value.N, h.Value.Path, "header %q is managed by the gateway; entry ignored", h.Name)
			continue
		}
		if ok {
			add = append(add, contract.HeaderAdd{Name: strings.ToLower(h.Name), Value: v})
		}
	}
	for _, s := range t.Remove {
		if isManaged(s.V) {
			d.Warnf(s.N, s.Path, "header %q is managed by the gateway; entry ignored", s.V)
			continue
		}
		remove = append(remove, strings.ToLower(s.V))
	}
	return add, remove
}

func compileMapping(t *RawTransform, d *Diag) *contract.Mapping {
	m := &contract.Mapping{Entries: []contract.MappingEntry{}}
	for _, h := range t.Mapping {
		dest := strings.Split(h.Name, ".")
		if hasEmpty(dest) {
			d.Errorf(h.Value.N, h.Value.Path, "invalid destination path %q", h.Name)
			continue
		}
		conflict := false
		for _, e := range m.Entries {
			if isPrefix(e.Dest, dest) || isPrefix(dest, e.Dest) {
				d.Errorf(h.Value.N, h.Value.Path, "destination %q conflicts with %q", h.Name, strings.Join(e.Dest, "."))
				conflict = true
				break
			}
		}
		if conflict {
			continue
		}
		e := contract.MappingEntry{Dest: dest}
		if strings.HasPrefix(h.Value.V, "$") {
			v, ok := compileVar(h.Value, true, false, d)
			if !ok {
				continue
			}
			e.Var = &v
		} else {
			e.Source = strings.Split(h.Value.V, ".")
			if hasEmpty(e.Source) {
				d.Errorf(h.Value.N, h.Value.Path, "invalid source path %q", h.Value.V)
				continue
			}
		}
		m.Entries = append(m.Entries, e)
	}
	return m
}

func hasEmpty(segs []string) bool {
	for _, s := range segs {
		if s == "" {
			return true
		}
	}
	return false
}

func isPrefix(a, b []string) bool {
	if len(a) > len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

func compileTemplate(n *yamlparse.Node, path string, d *Diag) *contract.Template {
	switch n.Kind {
	case yamlparse.Map:
		t := &contract.Template{Kind: contract.TemplateObject, Fields: []contract.TemplateField{}}
		for _, k := range n.Keys {
			t.Fields = append(t.Fields, contract.TemplateField{Key: k, Value: compileTemplate(n.Map[k], join(path, k), d)})
		}
		return t
	case yamlparse.Seq:
		t := &contract.Template{Kind: contract.TemplateArray, Items: []*contract.Template{}}
		for i, it := range n.Items {
			t.Items = append(t.Items, compileTemplate(it, idx(path, i), d))
		}
		return t
	}
	if IsNull(n) {
		return &contract.Template{Kind: contract.TemplateScalar, Raw: "null"}
	}
	if !n.Quoted && isJSONScalar(n.Value) {
		return &contract.Template{Kind: contract.TemplateScalar, Raw: n.Value}
	}
	v, _ := compileVar(&Str{V: n.Value, N: n, Path: path}, false, true, d)
	return &contract.Template{Kind: contract.TemplateVar, Var: v}
}

func isJSONScalar(s string) bool {
	if s == "true" || s == "false" {
		return true
	}
	if s == "" || !(s[0] == '-' || s[0] >= '0' && s[0] <= '9') {
		return false
	}
	var f json.Number
	return json.Unmarshal([]byte(s), &f) == nil
}
