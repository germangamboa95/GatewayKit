package yamlparse

import "testing"

func TestUnsupportedConstructs(t *testing.T) {
	const head = "gateway:\n  port: 8080\n"
	cases := []struct {
		name, src string
		line      int
		want      string
	}{
		{"anchor", head + "users: &mock\n  - a\n", 3, "anchors"},
		{"anchor on item", head + "list:\n  - &a x\n", 4, "anchors"},
		{"anchor in flow", head + "v: [&a x]\n", 3, "anchors"},
		{"alias", head + "users: *mock\n", 3, "aliases"},
		{"alias in flow", head + "v: {k: *a}\n", 3, "aliases"},
		{"alias key", head + "*a : 1\n", 3, "aliases"},
		{"merge key", head + "base:\n  <<: *mock\n", 4, "merge keys"},
		{"merge key flow", head + "v: {<<: x}\n", 3, "merge keys"},
		{"tag", head + "v: !!str 8080\n", 3, "tags"},
		{"local tag", head + "v: !foo x\n", 3, "tags"},
		{"tag on item", head + "list:\n  - !!int 3\n", 4, "tags"},
		{"tag on quoted", head + "v: !!str \"x\"\n", 3, "tags"},
		{"literal block", head + "v: |\n  line one\n  line two\n", 3, "block scalars"},
		{"literal block strip", head + "v: |-\n  text\n", 3, "block scalars"},
		{"folded block", head + "v: >\n  text\n", 3, "block scalars"},
		{"folded on item", head + "list:\n  - >\n    text\n", 4, "block scalars"},
		{"second document", head + "---\nother: 1\n", 3, "multiple documents"},
		{"second document after end", head + "...\n---\nother: 1\n", 4, "multiple documents"},
		{"content after end", head + "...\nother: 1\n", 4, "multiple documents"},
		{"two leading markers", "---\n---\n" + head, 2, "multiple documents"},
		{"complex key", head + "? key\n: value\n", 3, "complex keys"},
		{"bare complex key", head + "?\n", 3, "complex keys"},
		{"complex key in flow", head + "v: {? a: b}\n", 3, "complex keys"},
		{"multi-line plain value", head + "desc: first part\n  second part\n", 4, "multi-line"},
		{"multi-line plain item", head + "list:\n  - first\n    second\n", 5, "multi-line"},
		{"multi-line nested plain", head + "desc:\n  first\n  second\n", 5, "multi-line"},
		{"multi-line root plain", "first\nsecond\n", 2, "multi-line"},
		{"multi-line quoted", head + "v: \"a\n  b\"\n", 3, "unterminated"},
		{"flow then continuation", head + "v: [a]\n  b\n", 4, "multi-line"},
		{"directive", "%YAML 1.2\n---\na: 1\n", 1, "directives"},
		{"reserved", head + "v: @x\n", 3, "reserved"},
		{"marker content", "--- a: 1\n", 1, "'---' line"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustFail(t, c.src, c.line, c.want)
		})
	}
}

func TestIndicatorsInsideValues(t *testing.T) {
	src := "a: x&y\nb: x*y\nc: x!y\nd: a|b\ne: a>b\nf: a?b\ng: \"&x\"\nh: '*x'\ni: <<x\n"
	want := `{"a": "x&y", "b": "x*y", "c": "x!y", "d": "a|b", "e": "a>b", "f": "a?b", "g": "&x"q, "h": "*x"q, "i": "<<x"}`
	if got := dump(mustParse(t, src), false); got != want {
		t.Errorf("got %s\nwant %s", got, want)
	}
	if got := dump(mustParse(t, "---: 1\n\"<<\": 2\n"), false); got != `{"---": "1", "<<": "2"}` {
		t.Errorf("got %s", got)
	}
}
