package config

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"github.com/germangamboa95/gatewaykit/internal/yamlparse"
)

// rawInt, rawBool, rawStr and rawDur are decoded scalars with their node and field path.
type rawInt struct {
	V    int
	N    *yamlparse.Node
	Path string
}

type rawBool struct {
	V    bool
	N    *yamlparse.Node
	Path string
}

type rawStr struct {
	V    string
	N    *yamlparse.Node
	Path string
}

type rawDur struct {
	V    time.Duration
	N    *yamlparse.Node
	Path string
}

// rawConfig is the decoded config before validation and normalization.
type rawConfig struct {
	Root            *yamlparse.Node
	GatewayNode     *yamlparse.Node
	Port            *rawInt
	GlobalTimeout   *rawDur
	GlobalRateLimit *rawRateLimit
	Routes          []*rawRoute
}

type rawRateLimit struct {
	N        *yamlparse.Node
	Path     string
	Disabled bool
	Requests *rawInt
	Window   *rawDur
	Strategy *rawStr
	Per      *rawStr
}

type rawRoute struct {
	N              *yamlparse.Node
	Path           string
	RoutePath      *rawStr
	Methods        []*rawStr
	MethodsNode    *yamlparse.Node
	StripPrefix    *rawBool
	Upstream       *rawUpstream
	Timeout        *rawDur
	Retry          *rawRetry
	HealthCheck    *rawHealthCheck
	CircuitBreaker *rawCircuitBreaker
	RateLimit      *rawRateLimit
	Auth           *rawAuth
	ReqTransform   *rawTransform
	RespTransform  *rawTransform
}

type rawUpstream struct {
	N           *yamlparse.Node
	Path        string
	URL         *rawStr
	Targets     []*rawTarget
	TargetsNode *yamlparse.Node
	Balance     *rawStr
}

type rawTarget struct {
	N      *yamlparse.Node
	Path   string
	URL    *rawStr
	Weight *rawInt
}

type rawRetry struct {
	N            *yamlparse.Node
	Path         string
	Attempts     *rawInt
	Backoff      *rawStr
	InitialDelay *rawDur
	On           []*rawInt
	OnNode       *yamlparse.Node
}

type rawHealthCheck struct {
	N                  *yamlparse.Node
	Path               string
	HCPath             *rawStr
	Interval           *rawDur
	UnhealthyThreshold *rawInt
}

type rawCircuitBreaker struct {
	N         *yamlparse.Node
	Path      string
	Threshold *rawInt
	Window    *rawDur
	Cooldown  *rawDur
}

type rawAuth struct {
	N        *yamlparse.Node
	Path     string
	Type     *rawStr
	Header   *rawStr
	Keys     []*rawStr
	KeysNode *yamlparse.Node
}

// rawHeader is one headers.add entry.
type rawHeader struct {
	Name  string
	Key   *yamlparse.Node
	Value *rawStr
}

type rawTransform struct {
	N            *yamlparse.Node
	Path         string
	Request      bool
	Add          []*rawHeader
	Remove       []*rawStr
	Mapping      []*rawHeader
	MappingNode  *yamlparse.Node
	Envelope     *yamlparse.Node
	EnvelopePath string
}

// isNull reports whether n means "absent": missing or null. The parser turns
// unquoted null, ~ and empty values into Null nodes.
func isNull(n *yamlparse.Node) bool {
	return n == nil || n.Kind == yamlparse.Null
}

func join(path, key string) string {
	if path == "" {
		return key
	}
	return path + "." + key
}

func kindName(n *yamlparse.Node) string {
	switch n.Kind {
	case yamlparse.Map:
		return "a map"
	case yamlparse.Seq:
		return "a list"
	case yamlparse.Scalar:
		return strconv.Quote(n.Value)
	}
	return "null"
}

type decoder struct {
	d *diag
}

// fields checks n is a map, warns on unknown keys and returns the non-null
// children. It returns nil when n is not a map.
func (dc *decoder) fields(n *yamlparse.Node, path string, known ...string) map[string]*yamlparse.Node {
	if n.Kind != yamlparse.Map {
		dc.d.Errorf(n, path, "expected a map, got %s", kindName(n))
		return nil
	}
	out := map[string]*yamlparse.Node{}
	for _, k := range n.Keys {
		v := n.Map[k]
		ok := false
		for _, kk := range known {
			if kk == k {
				ok = true
				break
			}
		}
		if !ok {
			dc.d.Warnf(keyNode(n, k, v), join(path, k), "unknown key, ignored")
			continue
		}
		if !isNull(v) {
			out[k] = v
		}
	}
	return out
}

// keyNode picks the best location for key k of map m: the key itself, else
// its value, else the map.
func keyNode(m *yamlparse.Node, k string, v *yamlparse.Node) *yamlparse.Node {
	if p, ok := m.KeyPos[k]; ok && p.Line > 0 {
		return &yamlparse.Node{Kind: yamlparse.Scalar, Value: k, Line: p.Line, Col: p.Col}
	}
	if v != nil && v.Line > 0 {
		return v
	}
	return m
}

func (dc *decoder) scalar(n *yamlparse.Node, path, want string) bool {
	if n.Kind != yamlparse.Scalar {
		dc.d.Errorf(n, path, "expected %s, got %s", want, kindName(n))
		return false
	}
	return true
}

func (dc *decoder) intVal(n *yamlparse.Node, path string) *rawInt {
	if n == nil || !dc.scalar(n, path, "an integer") {
		return nil
	}
	v, err := strconv.Atoi(strings.TrimSpace(n.Value))
	if err != nil || strings.TrimSpace(n.Value) != n.Value {
		dc.d.Errorf(n, path, "expected an integer, got %q", n.Value)
		return nil
	}
	return &rawInt{V: v, N: n, Path: path}
}

func (dc *decoder) boolVal(n *yamlparse.Node, path string) *rawBool {
	if n == nil || !dc.scalar(n, path, "true or false") {
		return nil
	}
	switch n.Value {
	case "true":
		return &rawBool{V: true, N: n, Path: path}
	case "false":
		return &rawBool{V: false, N: n, Path: path}
	}
	dc.d.Errorf(n, path, "expected true or false, got %q", n.Value)
	return nil
}

func (dc *decoder) strVal(n *yamlparse.Node, path string) *rawStr {
	if n == nil || !dc.scalar(n, path, "a string") {
		return nil
	}
	return &rawStr{V: n.Value, N: n, Path: path}
}

// enumVal stores the value in lowercase; validation checks the allowed set.
func (dc *decoder) enumVal(n *yamlparse.Node, path string) *rawStr {
	s := dc.strVal(n, path)
	if s != nil {
		s.V = strings.ToLower(s.V)
	}
	return s
}

func (dc *decoder) durVal(n *yamlparse.Node, path string) *rawDur {
	if n == nil || !dc.scalar(n, path, "a duration") {
		return nil
	}
	v, err := parseDurationSyntax(n.Value)
	if err != nil {
		dc.d.Errorf(n, path, "%s", err.Error())
		return nil
	}
	return &rawDur{V: v, N: n, Path: path}
}

func (dc *decoder) list(n *yamlparse.Node, path string) []*yamlparse.Node {
	if n.Kind != yamlparse.Seq {
		dc.d.Errorf(n, path, "expected a list, got %s", kindName(n))
		return nil
	}
	return n.Items
}

func idx(path string, i int) string { return fmt.Sprintf("%s[%d]", path, i) }

// decode walks the node tree against the schema. It records type errors and
// unknown-key warnings in d and resolves lenient placement. It returns nil
// only when the top level is unusable.
func decode(root *yamlparse.Node, d *diag) *rawConfig {
	dc := &decoder{d: d}
	if root == nil || root.Kind != yamlparse.Map {
		if root == nil {
			root = &yamlparse.Node{Line: 1, Col: 1}
		}
		d.Errorf(root, "", "top-level value must be a map, got %s", kindName(root))
		return nil
	}
	raw := &rawConfig{Root: root}
	top := dc.fields(root, "", "gateway", "routes")
	gw := top["gateway"]
	var routesNode *yamlparse.Node
	routesPath := "routes"
	if gw == nil {
		d.Errorf(root, "gateway", "required field is missing")
	} else if g := dc.fields(gw, "gateway", "port", "global_timeout", "global_rate_limit", "routes"); g != nil {
		raw.GatewayNode = gw
		raw.Port = dc.intVal(g["port"], "gateway.port")
		raw.GlobalTimeout = dc.durVal(g["global_timeout"], "gateway.global_timeout")
		if n := g["global_rate_limit"]; n != nil {
			raw.GlobalRateLimit = dc.rateLimit(n, "gateway.global_rate_limit", false)
		}
		if n := g["routes"]; n != nil {
			if t := top["routes"]; t != nil {
				d.Errorf(keyNode(gw, "routes", n), "gateway.routes", "routes set both at top level (line %d) and under gateway (line %d)",
					keyNode(root, "routes", t).Line, keyNode(gw, "routes", n).Line)
			}
			routesNode, routesPath = n, "gateway.routes"
		}
	}
	if routesNode == nil {
		routesNode = top["routes"]
	}
	if routesNode == nil {
		d.Errorf(root, "routes", "required field is missing")
		return raw
	}
	for i, rn := range dc.list(routesNode, routesPath) {
		if r := dc.route(rn, idx(routesPath, i)); r != nil {
			raw.Routes = append(raw.Routes, r)
		}
	}
	return raw
}

var routeKeys = []string{"path", "methods", "strip_prefix", "upstream", "rate_limit", "auth",
	"circuit_breaker", "request_transform", "response_transform", "timeout", "retry", "health_check"}

var upstreamKeys = []string{"url", "targets", "balance", "timeout", "retry", "health_check", "circuit_breaker"}

func (dc *decoder) route(n *yamlparse.Node, path string) *rawRoute {
	f := dc.fields(n, path, routeKeys...)
	if f == nil {
		return nil
	}
	r := &rawRoute{N: n, Path: path}
	r.RoutePath = dc.strVal(f["path"], join(path, "path"))
	if mn := f["methods"]; mn != nil {
		r.MethodsNode = mn
		for i, it := range dc.list(mn, join(path, "methods")) {
			if s := dc.strVal(it, idx(join(path, "methods"), i)); s != nil {
				s.V = strings.ToUpper(s.V)
				r.Methods = append(r.Methods, s)
			}
		}
	}
	r.StripPrefix = dc.boolVal(f["strip_prefix"], join(path, "strip_prefix"))

	var uf map[string]*yamlparse.Node
	if un := f["upstream"]; un != nil {
		upath := join(path, "upstream")
		uf = dc.fields(un, upath, upstreamKeys...)
		if uf != nil {
			u := &rawUpstream{N: un, Path: upath}
			u.URL = dc.strVal(uf["url"], join(upath, "url"))
			if tn := uf["targets"]; tn != nil {
				u.TargetsNode = tn
				for i, it := range dc.list(tn, join(upath, "targets")) {
					tpath := idx(join(upath, "targets"), i)
					tf := dc.fields(it, tpath, "url", "weight")
					if tf == nil {
						continue
					}
					u.Targets = append(u.Targets, &rawTarget{
						N: it, Path: tpath,
						URL:    dc.strVal(tf["url"], join(tpath, "url")),
						Weight: dc.intVal(tf["weight"], join(tpath, "weight")),
					})
				}
			}
			u.Balance = dc.enumVal(uf["balance"], join(upath, "balance"))
			r.Upstream = u
		}
	}

	pick := func(key string) (*yamlparse.Node, string) {
		rn, un := f[key], uf[key]
		switch {
		case rn != nil && un != nil:
			rk, uk := keyNode(n, key, rn), keyNode(r.Upstream.N, key, un)
			dc.d.Errorf(rk, join(path, key), "set both at %s (line %d) and %s (line %d)",
				join(path, key), rk.Line, join(path, "upstream."+key), uk.Line)
			return nil, ""
		case un != nil:
			return un, join(path, "upstream."+key)
		case rn != nil:
			return rn, join(path, key)
		}
		return nil, ""
	}
	if n, p := pick("timeout"); n != nil {
		r.Timeout = dc.durVal(n, p)
	}
	if n, p := pick("retry"); n != nil {
		r.Retry = dc.retry(n, p)
	}
	if n, p := pick("health_check"); n != nil {
		r.HealthCheck = dc.healthCheck(n, p)
	}
	if n, p := pick("circuit_breaker"); n != nil {
		r.CircuitBreaker = dc.circuitBreaker(n, p)
	}
	if n := f["rate_limit"]; n != nil {
		r.RateLimit = dc.rateLimit(n, join(path, "rate_limit"), true)
	}
	if n := f["auth"]; n != nil {
		r.Auth = dc.auth(n, join(path, "auth"))
	}
	if n := f["request_transform"]; n != nil {
		r.ReqTransform = dc.transform(n, join(path, "request_transform"), true)
	}
	if n := f["response_transform"]; n != nil {
		r.RespTransform = dc.transform(n, join(path, "response_transform"), false)
	}
	return r
}

func (dc *decoder) rateLimit(n *yamlparse.Node, path string, allowFalse bool) *rawRateLimit {
	if n.Kind == yamlparse.Scalar {
		if allowFalse && n.Value == "false" && !n.Quoted {
			return &rawRateLimit{N: n, Path: path, Disabled: true}
		}
		want := "a map"
		if allowFalse {
			want = "a map or false"
		}
		dc.d.Errorf(n, path, "expected %s, got %s", want, kindName(n))
		return nil
	}
	f := dc.fields(n, path, "requests", "window", "strategy", "per")
	if f == nil {
		return nil
	}
	return &rawRateLimit{
		N: n, Path: path,
		Requests: dc.intVal(f["requests"], join(path, "requests")),
		Window:   dc.durVal(f["window"], join(path, "window")),
		Strategy: dc.enumVal(f["strategy"], join(path, "strategy")),
		Per:      dc.enumVal(f["per"], join(path, "per")),
	}
}

func (dc *decoder) retry(n *yamlparse.Node, path string) *rawRetry {
	f := dc.fields(n, path, "attempts", "backoff", "initial_delay", "on")
	if f == nil {
		return nil
	}
	r := &rawRetry{
		N: n, Path: path,
		Attempts:     dc.intVal(f["attempts"], join(path, "attempts")),
		Backoff:      dc.enumVal(f["backoff"], join(path, "backoff")),
		InitialDelay: dc.durVal(f["initial_delay"], join(path, "initial_delay")),
	}
	if on := f["on"]; on != nil {
		r.OnNode = on
		for i, it := range dc.list(on, join(path, "on")) {
			if v := dc.intVal(it, idx(join(path, "on"), i)); v != nil {
				r.On = append(r.On, v)
			}
		}
	}
	return r
}

func (dc *decoder) healthCheck(n *yamlparse.Node, path string) *rawHealthCheck {
	f := dc.fields(n, path, "path", "interval", "unhealthy_threshold")
	if f == nil {
		return nil
	}
	return &rawHealthCheck{
		N: n, Path: path,
		HCPath:             dc.strVal(f["path"], join(path, "path")),
		Interval:           dc.durVal(f["interval"], join(path, "interval")),
		UnhealthyThreshold: dc.intVal(f["unhealthy_threshold"], join(path, "unhealthy_threshold")),
	}
}

func (dc *decoder) circuitBreaker(n *yamlparse.Node, path string) *rawCircuitBreaker {
	f := dc.fields(n, path, "threshold", "window", "cooldown")
	if f == nil {
		return nil
	}
	return &rawCircuitBreaker{
		N: n, Path: path,
		Threshold: dc.intVal(f["threshold"], join(path, "threshold")),
		Window:    dc.durVal(f["window"], join(path, "window")),
		Cooldown:  dc.durVal(f["cooldown"], join(path, "cooldown")),
	}
}

func (dc *decoder) auth(n *yamlparse.Node, path string) *rawAuth {
	f := dc.fields(n, path, "type", "header", "keys")
	if f == nil {
		return nil
	}
	a := &rawAuth{
		N: n, Path: path,
		Type:   dc.enumVal(f["type"], join(path, "type")),
		Header: dc.strVal(f["header"], join(path, "header")),
	}
	if kn := f["keys"]; kn != nil {
		a.KeysNode = kn
		for i, it := range dc.list(kn, join(path, "keys")) {
			if s := dc.strVal(it, idx(join(path, "keys"), i)); s != nil {
				a.Keys = append(a.Keys, s)
			}
		}
	}
	return a
}

func (dc *decoder) transform(n *yamlparse.Node, path string, request bool) *rawTransform {
	f := dc.fields(n, path, "headers", "body")
	if f == nil {
		return nil
	}
	t := &rawTransform{N: n, Path: path, Request: request}
	if hn := f["headers"]; hn != nil {
		hpath := join(path, "headers")
		if hf := dc.fields(hn, hpath, "add", "remove"); hf != nil {
			if an := hf["add"]; an != nil {
				t.Add = dc.stringMap(an, join(hpath, "add"))
			}
			if rn := hf["remove"]; rn != nil {
				for i, it := range dc.list(rn, join(hpath, "remove")) {
					if s := dc.strVal(it, idx(join(hpath, "remove"), i)); s != nil {
						t.Remove = append(t.Remove, s)
					}
				}
			}
		}
	}
	if bn := f["body"]; bn != nil {
		bpath := join(path, "body")
		key := "envelope"
		if request {
			key = "mapping"
		}
		if bf := dc.fields(bn, bpath, key); bf != nil {
			if request {
				if mn := bf["mapping"]; mn != nil {
					t.MappingNode = mn
					t.Mapping = dc.stringMap(mn, join(bpath, "mapping"))
				}
			} else if en := bf["envelope"]; en != nil {
				t.Envelope, t.EnvelopePath = en, join(bpath, "envelope")
			}
		}
	}
	return t
}

func (dc *decoder) stringMap(n *yamlparse.Node, path string) []*rawHeader {
	if n.Kind != yamlparse.Map {
		dc.d.Errorf(n, path, "expected a map, got %s", kindName(n))
		return nil
	}
	var out []*rawHeader
	for _, k := range n.Keys {
		v := n.Map[k]
		p := join(path, k)
		if isNull(v) {
			dc.d.Errorf(keyNode(n, k, v), p, "expected a string value")
			continue
		}
		if s := dc.strVal(v, p); s != nil {
			out = append(out, &rawHeader{Name: k, Key: keyNode(n, k, v), Value: s})
		}
	}
	return out
}
