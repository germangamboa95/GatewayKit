package config

import (
	"strconv"
	"strings"
	"testing"

	"gatewaykit/internal/yamlparse"
)

var lineNo int

func next() int { lineNo++; return lineNo }

// m builds a map node from alternating key, value pairs. A string value
// becomes a plain scalar.
func m(kv ...any) *yamlparse.Node {
	n := &yamlparse.Node{Kind: yamlparse.Map, Map: map[string]*yamlparse.Node{}, Line: next(), Col: 1}
	for i := 0; i < len(kv); i += 2 {
		k := kv[i].(string)
		n.Keys = append(n.Keys, k)
		n.Map[k] = toNode(kv[i+1])
	}
	return n
}

func toNode(v any) *yamlparse.Node {
	switch x := v.(type) {
	case *yamlparse.Node:
		return x
	case string:
		return s(x)
	case nil:
		return &yamlparse.Node{Kind: yamlparse.Null, Line: next(), Col: 3}
	}
	panic("bad node value")
}

func s(v string) *yamlparse.Node {
	return &yamlparse.Node{Kind: yamlparse.Scalar, Value: v, Line: next(), Col: 5}
}

func q(v string) *yamlparse.Node {
	return &yamlparse.Node{Kind: yamlparse.Scalar, Value: v, Quoted: true, Line: next(), Col: 5}
}

func seq(items ...any) *yamlparse.Node {
	n := &yamlparse.Node{Kind: yamlparse.Seq, Line: next(), Col: 3}
	for _, it := range items {
		n.Items = append(n.Items, toNode(it))
	}
	return n
}

func minimalRoute(extra ...any) *yamlparse.Node {
	kv := []any{"path", q("/"), "upstream", m("url", q("http://localhost:3001"))}
	return m(append(kv, extra...)...)
}

func doc(gateway *yamlparse.Node, routes ...*yamlparse.Node) *yamlparse.Node {
	items := make([]any, len(routes))
	for i, r := range routes {
		items[i] = r
	}
	return m("gateway", gateway, "routes", seq(items...))
}

func newDiag() *Diag { return &Diag{File: "gateway.yaml"} }

func hasLine(t *testing.T, lines []string, want ...string) {
	t.Helper()
	for _, l := range lines {
		ok := true
		for _, w := range want {
			if !strings.Contains(l, w) {
				ok = false
				break
			}
		}
		if ok {
			return
		}
	}
	t.Errorf("no line contains %q in:\n%s", want, strings.Join(lines, "\n"))
}

func loc(n *yamlparse.Node) string {
	return "gateway.yaml:" + itoa(n.Line) + ":" + itoa(n.Col) + ": "
}

func itoa(i int) string { return strconv.Itoa(i) }
