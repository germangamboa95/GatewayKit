package config

import (
	"net/url"
	"strings"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/yamlparse"
)

type validator struct {
	d   *Diag
	raw *Raw
}

// Validate applies every per-field and cross-field rule to raw, appending
// errors and warnings to d.
func Validate(raw *Raw, d *Diag) {
	if raw == nil {
		return
	}
	v := &validator{d: d, raw: raw}
	if p := raw.Port; p != nil && (p.V < 1 || p.V > 65535) {
		d.Errorf(p.N, p.Path, "must be an integer from 1 to 65535 (got %d)", p.V)
	}
	v.positive(raw.GlobalTimeout)
	if raw.GlobalRateLimit != nil {
		v.rateLimitFields(raw.GlobalRateLimit)
	}
	seen := map[string]*RawRoute{}
	for _, r := range raw.Routes {
		v.route(r, seen)
	}
}

func (v *validator) required(parent *yamlparse.Node, path string) {
	v.d.Errorf(parent, path, "required field is missing")
}

func (v *validator) minInt(i *Int, min int) {
	if i != nil && i.V < min {
		v.d.Errorf(i.N, i.Path, "must be an integer >= %d (got %d)", min, i.V)
	}
}

func (v *validator) positive(d *Dur) {
	if d != nil && d.V <= 0 {
		v.d.Errorf(d.N, d.Path, "duration must be greater than 0 (got %q)", d.N.Value)
	}
}

func (v *validator) enum(s *Str, allowed ...string) {
	if s == nil {
		return
	}
	for _, a := range allowed {
		if s.V == a {
			return
		}
	}
	v.d.Errorf(s.N, s.Path, "invalid value %q (allowed: %s)", s.N.Value, strings.Join(allowed, ", "))
}

func (v *validator) rateLimitFields(rl *RawRateLimit) {
	if rl.Disabled {
		return
	}
	v.minInt(rl.Requests, 1)
	v.positive(rl.Window)
	v.enum(rl.Strategy, contract.StrategyFixedWindow, contract.StrategySlidingWindow)
	v.enum(rl.Per, contract.PerIP, contract.PerGlobal)
}

// NormalizePath removes a trailing slash, keeping "/" itself.
func NormalizePath(p string) string {
	t := strings.TrimRight(p, "/")
	if t == "" {
		return "/"
	}
	return t
}

func (v *validator) route(r *RawRoute, seen map[string]*RawRoute) {
	if r.RoutePath == nil {
		if !hasKey(r.N, "path") {
			v.required(r.N, r.Path+".path")
		}
	} else if v.routePath(r.RoutePath) {
		np := NormalizePath(r.RoutePath.V)
		if prev, ok := seen[np]; ok {
			v.d.Errorf(r.RoutePath.N, r.RoutePath.Path, "duplicate route path %q (also %s)", np, prev.Path)
		} else {
			seen[np] = r
		}
		if np == "/health" {
			v.d.Warnf(r.RoutePath.N, r.RoutePath.Path,
				"route never receives GET /health; the gateway answers it")
		}
	}
	if r.MethodsNode != nil && r.MethodsNode.Kind == yamlparse.Seq && len(r.MethodsNode.Items) == 0 {
		v.d.Errorf(r.MethodsNode, r.Path+".methods", "must be a non-empty list")
	}
	for _, m := range r.Methods {
		v.enum(m, contract.DefaultMethods...)
	}
	if r.Upstream == nil {
		if !hasKey(r.N, "upstream") {
			v.required(r.N, r.Path+".upstream")
		}
	} else {
		v.upstream(r.Upstream)
	}
	v.positive(r.Timeout)
	if rt := r.Retry; rt != nil {
		if rt.Attempts == nil && !hasKey(rt.N, "attempts") {
			v.required(rt.N, rt.Path+".attempts")
		}
		v.minInt(rt.Attempts, 1)
		v.enum(rt.Backoff, contract.BackoffFixed, contract.BackoffExponential)
		if rt.OnNode != nil && rt.OnNode.Kind == yamlparse.Seq && len(rt.OnNode.Items) == 0 {
			v.d.Errorf(rt.OnNode, rt.Path+".on", "must be a non-empty list")
		}
		for _, s := range rt.On {
			if s.V < 100 || s.V > 599 {
				v.d.Errorf(s.N, s.Path, "must be a status from 100 to 599 (got %d)", s.V)
			}
		}
	}
	if hc := r.HealthCheck; hc != nil {
		if hc.HCPath == nil {
			if !hasKey(hc.N, "path") {
				v.required(hc.N, hc.Path+".path")
			}
		} else if !strings.HasPrefix(hc.HCPath.V, "/") {
			v.d.Errorf(hc.HCPath.N, hc.HCPath.Path, "must start with \"/\" (got %q)", hc.HCPath.V)
		}
		v.positive(hc.Interval)
		v.minInt(hc.UnhealthyThreshold, 1)
	}
	if cb := r.CircuitBreaker; cb != nil {
		if cb.Threshold == nil && !hasKey(cb.N, "threshold") {
			v.required(cb.N, cb.Path+".threshold")
		}
		v.minInt(cb.Threshold, 1)
		v.positive(cb.Window)
		v.positive(cb.Cooldown)
	}
	if rl := r.RateLimit; rl != nil {
		v.rateLimitFields(rl)
	}
	v.mergedRateLimit(r)
	if a := r.Auth; a != nil {
		if a.Type == nil {
			if !hasKey(a.N, "type") {
				v.required(a.N, a.Path+".type")
			}
		} else {
			v.enum(a.Type, contract.AuthTypeAPIKey)
		}
		if a.Header != nil && strings.TrimSpace(a.Header.V) == "" {
			v.d.Errorf(a.Header.N, a.Header.Path, "must not be empty")
		}
		if a.KeysNode == nil {
			v.required(a.N, a.Path+".keys")
		} else if a.KeysNode.Kind == yamlparse.Seq && len(a.KeysNode.Items) == 0 {
			v.d.Errorf(a.KeysNode, a.Path+".keys", "must hold at least one key")
		}
		for _, k := range a.Keys {
			if k.V == "" {
				v.d.Errorf(k.N, k.Path, "key must not be empty")
			}
		}
	}
}

func hasKey(n *yamlparse.Node, k string) bool {
	if n == nil || n.Kind != yamlparse.Map {
		return false
	}
	c, ok := n.Map[k]
	return ok && !IsNull(c)
}

func (v *validator) routePath(s *Str) bool {
	p := s.V
	switch {
	case !strings.HasPrefix(p, "/"):
		v.d.Errorf(s.N, s.Path, "must start with \"/\" (got %q)", p)
	case strings.Contains(p, "*"):
		v.d.Errorf(s.N, s.Path, "must not contain \"*\" (got %q)", p)
	case strings.ContainsAny(p, "?#"):
		v.d.Errorf(s.N, s.Path, "must not contain a query string or fragment (got %q)", p)
	case strings.Contains(p, "/:"):
		v.d.Errorf(s.N, s.Path, "must not contain a :param segment (got %q)", p)
	default:
		return true
	}
	return false
}

func (v *validator) url(s *Str) {
	u, err := url.Parse(s.V)
	switch {
	case err != nil:
		v.d.Errorf(s.N, s.Path, "invalid URL %q", s.V)
	case u.Scheme != "http" && u.Scheme != "https":
		v.d.Errorf(s.N, s.Path, "URL must use http or https (got %q)", s.V)
	case u.Host == "" || u.Hostname() == "":
		v.d.Errorf(s.N, s.Path, "URL must have a host (got %q)", s.V)
	case u.RawQuery != "" || u.ForceQuery:
		v.d.Errorf(s.N, s.Path, "URL must not have a query string (got %q)", s.V)
	case u.Fragment != "":
		v.d.Errorf(s.N, s.Path, "URL must not have a fragment (got %q)", s.V)
	}
}

func (v *validator) upstream(u *RawUpstream) {
	if (u.URL != nil) == (u.TargetsNode != nil) {
		if u.URL != nil || !hasKey(u.N, "url") {
			v.d.Errorf(u.N, u.Path, "exactly one of \"url\" or \"targets\" is required")
		}
	}
	if u.URL != nil {
		v.url(u.URL)
	}
	if u.TargetsNode != nil && u.TargetsNode.Kind == yamlparse.Seq && len(u.TargetsNode.Items) == 0 {
		v.d.Errorf(u.TargetsNode, u.Path+".targets", "must hold at least one target")
	}
	seen := map[string]string{}
	for _, t := range u.Targets {
		if t.URL == nil {
			if !hasKey(t.N, "url") {
				v.required(t.N, t.Path+".url")
			}
		} else {
			v.url(t.URL)
			if prev, ok := seen[t.URL.V]; ok {
				v.d.Errorf(t.URL.N, t.URL.Path, "duplicate target URL %q (also %s)", t.URL.V, prev)
			} else {
				seen[t.URL.V] = t.Path
			}
		}
		v.minInt(t.Weight, 1)
	}
	v.enum(u.Balance, contract.BalanceRoundRobin, contract.BalanceWeightedRoundRobin)
	single := u.URL != nil && u.TargetsNode == nil || u.URL == nil && len(u.Targets) == 1
	if single {
		if u.Balance != nil && (u.Balance.V == contract.BalanceRoundRobin || u.Balance.V == contract.BalanceWeightedRoundRobin) {
			v.d.Warnf(u.Balance.N, u.Balance.Path, "has no effect with only one target")
		}
		for _, t := range u.Targets {
			if t.Weight != nil {
				v.d.Warnf(t.Weight.N, t.Weight.Path, "has no effect with only one target")
			}
		}
	}
}

func (v *validator) mergedRateLimit(r *RawRoute) {
	rl, g := r.RateLimit, v.raw.GlobalRateLimit
	if rl != nil && rl.Disabled || rl == nil && g == nil {
		return
	}
	at := g
	if rl != nil {
		at = rl
	}
	if at == nil {
		return
	}
	hasReq := rl != nil && rl.Requests != nil || g != nil && g.Requests != nil
	hasWin := rl != nil && rl.Window != nil || g != nil && g.Window != nil
	if !hasReq && !failedField(at.N, "requests") && !(g != nil && failedField(g.N, "requests")) {
		v.d.Errorf(at.N, r.Path+".rate_limit", "no \"requests\" after merging with gateway.global_rate_limit")
	}
	if !hasWin && !failedField(at.N, "window") && !(g != nil && failedField(g.N, "window")) {
		v.d.Errorf(at.N, r.Path+".rate_limit", "no \"window\" after merging with gateway.global_rate_limit")
	}
}

// failedField reports whether a key is present but failed to decode, so a
// type error already covers it.
func failedField(n *yamlparse.Node, k string) bool { return hasKey(n, k) }
