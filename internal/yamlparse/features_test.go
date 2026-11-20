package yamlparse

import (
	"strings"
	"testing"
)

// Expanded aliases keep the anchored node's positions. yaml.v3 places an
// anchored node at its '&'.
func TestAliases(t *testing.T) {
	src := "base: &b { url: u }\nx: *b\nlist:\n  - &i item\n  - *i\n"
	want := `{"base": {"url": "u"@1:17}@1:7, "x": {"url": "u"@1:17}@1:7, "list": ["item"@4:5, "item"@4:5]@4:3}@1:1`
	root := mustParse(t, src)
	if got := dump(root, true); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if root.Map["base"] == root.Map["x"] {
		t.Error("alias shares the anchored node; want a copy")
	}
	if got := dump(mustParse(t, "k: &k key\n*k : 1\n"), false); got != `{"k": "key", "key": "1"}` {
		t.Errorf("alias key: got %s", got)
	}
}

func TestMergeKeys(t *testing.T) {
	src := "base: &b\n  a: 1\n  b: 2\nr:\n  <<: *b\n  b: 3\n"
	want := `{"base": {"a": "1"@2:6, "b": "2"@3:6}@1:7, "r": {"b": "3"@6:6, "a": "1"@2:6}@5:3}@1:1`
	root := mustParse(t, src)
	if got := dump(root, true); got != want {
		t.Errorf("got  %s\nwant %s", got, want)
	}
	if p := root.Map["r"].KeyPos["a"]; p != (Pos{Line: 2, Col: 3}) {
		t.Errorf("merged key position %+v, want 2:3", p)
	}
	list := "x: &x {a: 1, b: 1}\ny: &y {b: 2, c: 2}\nr: {<<: [*x, *y], c: 3}\n"
	if got := dump(mustParse(t, list).Map["r"], false); got != `{"c": "3", "a": "1", "b": "1"}` {
		t.Errorf("merge list: got %s", got)
	}
	if got := dump(mustParse(t, "r: {<<: {a: 1}}\n"), false); got != `{"r": {"a": "1"}}` {
		t.Errorf("inline merge: got %s", got)
	}
	if got := dump(mustParse(t, "\"<<\": 1\n"), false); got != `{"<<": "1"}` {
		t.Errorf("quoted << is a plain key: got %s", got)
	}
}

func TestIndicatorsInsideValues(t *testing.T) {
	src := "a: x&y\nb: x*y\nc: x!y\nd: a|b\ne: a>b\nf: a?b\ng: \"&x\"\nh: '*x'\ni: <<x\n"
	want := `{"a": "x&y", "b": "x*y", "c": "x!y", "d": "a|b", "e": "a>b", "f": "a?b", "g": "&x"q, "h": "*x"q, "i": "<<x"}`
	if got := dump(mustParse(t, src), false); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
}

func TestRejected(t *testing.T) {
	const head = "gateway:\n  port: 8080\n"
	mustFailAt(t, head+"---\nother: 1\n", 3, 1, "multiple documents")
	mustFailAt(t, head+"...\n---\nother: 1\n", 4, 1, "multiple documents")
	mustFailAt(t, head+"v: !foo x\n", 3, 4, "tag !foo")
	mustFailAt(t, head+"v: !!int 3\n", 3, 4, "tag !!int")
	mustFailAt(t, head+"v: !!map {a: 1}\n", 3, 4, "tag !!map")
	mustFailAt(t, head+"!foo k: 1\n", 3, 1, "tag !foo")
	mustFailAt(t, head+"a: &a [x, *a]\n", 3, 11, "alias *a")
	mustFailAt(t, head+"a: &a {k: {<<: *a}}\n", 3, 16, "alias *a")
	mustFailAt(t, head+"l: &l [ {<<: *l} ]\n", 3, 14, "alias *l")
	mustFailAt(t, head+"m: &m {k: [{<<: *m}]}\n", 3, 17, "alias *m")
	mustFailAt(t, head+"s: &s x\nr: {<<: *s}\n", 4, 5, "merge key")
	mustFailAt(t, head+"r: {<<: [x]}\n", 3, 5, "merge key")
	mustFailAt(t, head+"r:\n  <<:\n", 4, 3, "merge key")
}

func TestAliasExpansionLimit(t *testing.T) {
	var b strings.Builder
	b.WriteString("l0: &l0 [" + strings.Repeat("x, ", 9) + "x]\n")
	for i := 1; i <= 5; i++ {
		ref := "*l" + string(rune('0'+i-1))
		b.WriteString("l" + string(rune('0'+i)) + ": &l" + string(rune('0'+i)) + " [" + strings.Repeat(ref+", ", 9) + ref + "]\n")
	}
	mustFail(t, b.String(), "aliases expand to more than")
}
