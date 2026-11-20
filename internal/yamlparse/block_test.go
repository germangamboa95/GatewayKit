package yamlparse

import (
	"fmt"
	"testing"
)

func TestBlockTrees(t *testing.T) {
	cases := []struct{ name, src, want string }{
		{"nested map", "gateway:\n  port: 8080\n",
			`{"gateway": {"port": "8080"@2:9}@2:3}@1:1`},
		{"key order", "port: 8080\nglobal_timeout: 30s\nglobal_rate_limit:\n",
			`{"port": "8080"@1:7, "global_timeout": "30s"@2:17, "global_rate_limit": null@3:1}@1:1`},
		{"seq at parent indent", "routes:\n- path: \"/x\"\n  upstream:\n    url: u\n",
			`{"routes": [{"path": "/x"q@2:9, "upstream": {"url": "u"@4:10}@4:5}@2:3]@2:1}@1:1`},
		{"seq deeper", "routes:\n  - path: \"/x\"\n    upstream:\n      url: u\n",
			`{"routes": [{"path": "/x"q@2:11, "upstream": {"url": "u"@4:12}@4:7}@2:5]@2:3}@1:1`},
		{"seq of scalars", "k:\n  - a\n  - b\n",
			`{"k": ["a"@2:5, "b"@3:5]@2:3}@1:1`},
		{"root seq", "- a\n- b: 1\n  c: 2\n-\n- \n",
			`["a"@1:3, {"b": "1"@2:6, "c": "2"@3:6}@2:3, null@4:2, null@5:2]@1:1`},
		{"nested seq inline", "- - a\n  - b\n- c\n",
			`[["a"@1:5, "b"@2:5]@1:3, "c"@3:3]@1:1`},
		{"item block on next line", "-\n  a: 1\n- b\n",
			`[{"a": "1"@2:6}@2:3, "b"@3:3]@1:1`},
		{"empty value then sibling", "a:\nb: 1\n",
			`{"a": null@1:1, "b": "1"@2:4}@1:1`},
		{"empty value then dedent", "x:\n  a:\ny: 1\n",
			`{"x": {"a": null@2:3}@2:3, "y": "1"@3:4}@1:1`},
		{"empty value in item map", "- key:\n- a\n",
			`[{"key": null@1:3}@1:3, "a"@2:3]@1:1`},
		{"seq at parent indent in item", "- key:\n  - a\n  - b\n- c\n",
			`[{"key": ["a"@2:5, "b"@3:5]@2:3}@1:3, "c"@4:3]@1:1`},
		{"parent indent seq then sibling", "k:\n- a\nother: 1\n",
			`{"k": ["a"@2:3]@2:1, "other": "1"@3:8}@1:1`},
		{"dotted and dashed keys", "user.id: a\nX-Gateway: b\n",
			`{"user.id": "a"@1:10, "X-Gateway": "b"@2:12}@1:1`},
		{"quoted keys", "\"X-Request-Start\": a\n'k''s': b\n",
			`{"X-Request-Start": "a"@1:20, "k's": "b"@2:9}@1:1`},
		{"key with space before colon", "a  : 1\n",
			`{"a": "1"@1:6}@1:1`},
		{"same key in different maps", "- path: /a\n- path: /b\n",
			`[{"path": "/a"@1:9}@1:3, {"path": "/b"@2:9}@2:3]@1:1`},
		{"scalar on next line", "a:\n  hello\nb: 2\n",
			`{"a": "hello"@2:3, "b": "2"@3:4}@1:1`},
		{"root scalar", "hello\n", `"hello"@1:1`},
		{"url value", "url: http://localhost:3001/base\n",
			`{"url": "http://localhost:3001/base"@1:6}@1:1`},
		{"literal colon in value", "v: $literal:gateway\n",
			`{"v": "$literal:gateway"@1:4}@1:1`},
		{"null words", "a: null\nb: ~\nc: \"null\"\n",
			`{"a": null@1:4, "b": null@2:4, "c": "null"q@3:4}@1:1`},
		{"unicode column", "é: x\n", `{"é": "x"@1:4}@1:1`},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			if got := dump(mustParse(t, c.src), true); got != c.want {
				t.Errorf("ParseYAML(%q)\n got %s\nwant %s", c.src, got, c.want)
			}
		})
	}
}

func TestSeqIndentEquivalence(t *testing.T) {
	a := "routes:\n- path: /x\n  methods: [GET]\n- path: /y\n"
	b := "routes:\n    - path: /x\n      methods: [GET]\n    - path: /y\n"
	if x, y := dump(mustParse(t, a), false), dump(mustParse(t, b), false); x != y {
		t.Errorf("parent indent %s != deeper %s", x, y)
	}
}

func TestDuplicateKeys(t *testing.T) {
	mustFailAt(t, "port: 8080\nhost: x\nport: 9090\n", 3, 1, `duplicate key "port"`)
	mustFailAt(t, "a:\n  port: 1\n  port: 2\n", 3, 3, "duplicate key")
	mustFailAt(t, "a: 1\n\"a\": 2\n", 2, 1, "duplicate key")
	mustFailAt(t, "v: {a: 1, a: 2}\n", 1, 11, "duplicate key")
}

func TestComplexKeys(t *testing.T) {
	mustFailAt(t, "[a]: b\n", 1, 1, "complex keys")
	mustFailAt(t, "v: {[a]: b}\n", 1, 5, "complex keys")
	mustFailAt(t, "? {a: 1}\n: v\n", 1, 3, "complex keys")
	if got := dump(mustParse(t, "? key\n: value\n"), false); got != `{"key": "value"}` {
		t.Errorf("explicit scalar key: got %s", got)
	}
}

// TestInvalidYAML checks that yaml.v3 syntax errors come back as *Error.
// yaml.v3 reports a line but no column, and drops the line for errors on
// line 1, so only the type and the line format are checked here.
func TestInvalidYAML(t *testing.T) {
	for _, src := range []string{
		"a: 1\n- b\n",
		"a: 1\nb\n",
		"a: - b\n",
		"a: b: c\n",
		"a:\n    b: 1\n  c: 2\n",
		"- a: 1\n b: 2\n",
		"gateway:\n\tport: 8080\n",
		"v: [a, {b: c\n",
		"v: [a, , b]\n",
		"v: \"abc\n",
		"v: \"\\uzzzz\"\n",
		"v: *missing\n",
		"v: \"\x01\"\n",
	} {
		pe := mustFail(t, src, "")
		want := pe.Msg
		if pe.Line > 0 {
			want = fmt.Sprintf("%d: %s", pe.Line, pe.Msg)
		}
		if pe.Msg == "" || pe.Col != 0 || pe.Error() != want {
			t.Errorf("ParseYAML(%q) error %q: line %d col %d", src, pe, pe.Line, pe.Col)
		}
	}
	if pe := mustFail(t, "a: 1\nb:\n\tc: 2\n", ""); pe.Line != 3 {
		t.Errorf("tab error line %d, want 3", pe.Line)
	}
}
