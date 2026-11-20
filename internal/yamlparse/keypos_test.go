package yamlparse

import "testing"

func TestKeyPositions(t *testing.T) {
	src := "gateway:\n  port: 8080\nroutes:\n  - path: \"/x\"\n    cache_ttl:\n      a: { \"k.y\": 1 }\n"
	n, err := ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	check := func(m *Node, k string, line, col int) {
		t.Helper()
		p, ok := m.KeyPos[k]
		if !ok || p.Line != line || p.Col != col {
			t.Errorf("key %q at %+v (ok %v), want %d:%d", k, p, ok, line, col)
		}
	}
	check(n, "gateway", 1, 1)
	check(n, "routes", 3, 1)
	check(n.Map["gateway"], "port", 2, 3)
	r := n.Map["routes"].Items[0]
	check(r, "path", 4, 5)
	check(r, "cache_ttl", 5, 5)
	check(r.Map["cache_ttl"], "a", 6, 7)
	check(r.Map["cache_ttl"].Map["a"], "k.y", 6, 12)
}
