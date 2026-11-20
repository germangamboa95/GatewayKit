package yamlparse

import "testing"

func TestIndentWidth(t *testing.T) {
	two := "gateway:\n  port: 8080\n  limit:\n    requests: 5\n"
	four := "gateway:\n    port: 8080\n    limit:\n        requests: 5\n"
	want := `{"gateway": {"port": "8080", "limit": {"requests": "5"}}}`
	for _, src := range []string{two, four} {
		if got := dump(mustParse(t, src), false); got != want {
			t.Errorf("ParseYAML(%q) = %s, want %s", src, got, want)
		}
	}
	mixed := "a:\n  b: 1\nc:\n      d: 2\n"
	if got := dump(mustParse(t, mixed), false); got != `{"a": {"b": "1"}, "c": {"d": "2"}}` {
		t.Errorf("per-block widths: got %s", got)
	}
}

func TestInconsistentIndent(t *testing.T) {
	mustFail(t, "a:\n    b: 1\n  c: 2\n", 3, "indentation")
	mustFail(t, "a:\n  b: 1\n   c: 2\n", 3, "")
}

func TestTabInIndent(t *testing.T) {
	cases := []struct {
		src  string
		line int
	}{
		{"gateway:\n\tport: 8080\n", 2},
		{"gateway:\n  \tport: 8080\n", 2},
		{"a: 1\nb:\n  - x\n  -\ty\n", 4},
	}
	for _, c := range cases {
		pe := mustFail(t, c.src, c.line, "tab")
		if pe.Col < 1 {
			t.Errorf("bad col %d", pe.Col)
		}
	}
	if got := dump(mustParse(t, "a:\t1 \t# c\n\t\n"), false); got != `{"a": "1"}` {
		t.Errorf("tab as separator: got %s", got)
	}
}

func TestComments(t *testing.T) {
	cases := []struct{ src, want string }{
		{"timeout: \"5s\" # Override global timeout\n", `{"timeout": "5s"q}`},
		{"v: \"a # b\"\n", `{"v": "a # b"q}`},
		{"v: 'a # b' # c\n", `{"v": "a # b"q}`},
		{"v: a#b\n", `{"v": "a#b"}`},
		{"v: a #b\n", `{"v": "a"}`},
		{"v: a\t#b\n", `{"v": "a"}`},
		{"# top\n  # indented\na: 1 # x: y\n#end", `{"a": "1"}`},
		{"a: # note\n  b: 1\n", `{"a": {"b": "1"}}`},
		{"a: [1, 2] # c\n", `{"a": ["1", "2"]}`},
		{"a: { k: v } # c\n", `{"a": {"k": "v"}}`},
		{"- x # c\n- 'y' # c\n", `["x", "y"q]`},
		{"key#1: v\n", `{"key#1": "v"}`},
	}
	for _, c := range cases {
		if got := dump(mustParse(t, c.src), false); got != c.want {
			t.Errorf("ParseYAML(%q) = %s, want %s", c.src, got, c.want)
		}
	}
	mustFail(t, "a: \"x\"#c\n", 1, "unexpected content")
}

func TestDocumentMarkers(t *testing.T) {
	body := "gateway:\n  port: 8080\nroutes:\n  - path: /x\n"
	want := dump(mustParse(t, body), false)
	for _, src := range []string{
		"---\n" + body + "...\n",
		"--- # doc\n" + body + "... # end\n",
		"# c\n---\n" + body + "...\n# trailing\n\n",
	} {
		if got := dump(mustParse(t, src), false); got != want {
			t.Errorf("ParseYAML(%q) = %s, want %s", src, got, want)
		}
	}
	if got := dump(mustParse(t, "---\na: 1\n"), true); got != `{"a": "1"@2:4}@2:1` {
		t.Errorf("positions after marker: %s", got)
	}
}

func TestEmptyDocument(t *testing.T) {
	for _, src := range []string{"", "\n\n", "# only\n", "---\n...\n"} {
		if n := mustParse(t, src); n.Kind != Null {
			t.Errorf("ParseYAML(%q) kind %v, want null", src, n.Kind)
		}
	}
}

func TestLineEndings(t *testing.T) {
	src := "\ufeffa: 1\r\nb:\r\n  - x\r\n"
	if got := dump(mustParse(t, src), true); got != `{"a": "1"@1:4, "b": ["x"@3:5]@3:3}@1:1` {
		t.Errorf("CRLF/BOM: got %s", got)
	}
}
