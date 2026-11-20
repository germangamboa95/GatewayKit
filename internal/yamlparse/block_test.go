package yamlparse

import "testing"

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
			`["a"@1:3, {"b": "1"@2:6, "c": "2"@3:6}@2:3, null@4:1, null@5:1]@1:1`},
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

func TestBlockErrors(t *testing.T) {
	cases := []struct {
		name, src string
		line      int
		want      string
	}{
		{"duplicate key", "port: 8080\nhost: x\nport: 9090\n", 3, `duplicate key "port"`},
		{"duplicate nested key", "a:\n  b: 1\n  b: 2\n", 3, "duplicate key"},
		{"duplicate quoted key", "a: 1\n\"a\": 2\n", 2, "duplicate key"},
		{"seq entry in map", "a: 1\n- b\n", 2, "sequence entry"},
		{"not a key", "a: 1\nb\n", 2, "mapping key"},
		{"dash after key", "a: - b\n", 1, "same line"},
		{"nested map on one line", "a: b: c\n", 1, "nested mapping"},
		{"bad seq indent", "-\n    a: 1\n  b: 2\n", 3, "indentation"},
		{"map under seq dedent", "- a: 1\n b: 2\n", 2, "indentation"},
		{"trailing content after root", "a: 1\n  b: 2\n", 2, "multi-line"},
		{"empty key", ": v\n", 1, "unexpected ':'"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			mustFail(t, c.src, c.line, c.want)
		})
	}
	pe := mustFail(t, "a:\n  port: 1\n  port: 2\n", 3, "duplicate")
	if pe.Col != 3 {
		t.Errorf("duplicate key col %d, want 3", pe.Col)
	}
}
