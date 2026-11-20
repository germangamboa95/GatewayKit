package yamlparse

import "testing"

func TestScalarStyles(t *testing.T) {
	cases := []struct {
		name, value string
		want        string
		quoted      bool
	}{
		{"plain", `fixed_window`, "fixed_window", false},
		{"plain number", `8080`, "8080", false},
		{"plain bool word", `no`, "no", false},
		{"plain trailing space", "abc   ", "abc", false},
		{"plain inner spaces", `a b  c`, "a b  c", false},
		{"plain quote inside", `it's "x"`, `it's "x"`, false},
		{"plain dash start", `-5`, "-5", false},
		{"plain dollar", `$request_time`, "$request_time", false},
		{"double empty", `""`, "", true},
		{"double escapes", `"a\"b\\c\nd\teé"`, "a\"b\\c\nd\teé", true},
		{"double unicode", `"éA"`, "éA", true},
		{"double surrogate pair", `"😀"`, "\U0001F600", true},
		{"double raw utf8", `"héllo"`, "héllo", true},
		{"double single quote", `"it's"`, "it's", true},
		{"double number", `"8080"`, "8080", true},
		{"single doubled quote", `'it''s'`, "it's", true},
		{"single empty", `''`, "", true},
		{"single backslash literal", `'a\nb'`, `a\nb`, true},
		{"single only quotes", `''''`, "'", true},
		{"double hash", `"a # b"`, "a # b", true},
		{"double null word", `"~"`, "~", true},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			n := mustParse(t, "v: "+c.value+"\n").Map["v"]
			if n.Kind != Scalar || n.Value != c.want || n.Quoted != c.quoted {
				t.Errorf("v: %s => kind %v value %q quoted %v, want %q quoted %v",
					c.value, n.Kind, n.Value, n.Quoted, c.want, c.quoted)
			}
			if n.Line != 1 || n.Col != 4 {
				t.Errorf("position %d:%d, want 1:4", n.Line, n.Col)
			}
		})
	}
}

func TestScalarErrors(t *testing.T) {
	cases := []struct {
		name, src string
		col       int
		want      string
	}{
		{"unknown escape", `v: "a\x41"`, 6, `unsupported escape \x`},
		{"slash escape", `v: "a\/b"`, 6, "unsupported escape"},
		{"short unicode", `v: "\u12"`, 5, "4 hex"},
		{"bad hex", `v: "\uzzzz"`, 5, "4 hex"},
		{"lone surrogate", `v: "\ud83d"`, 5, "surrogate"},
		{"unterminated double", `v: "abc`, 4, "unterminated"},
		{"trailing backslash", `v: "abc\`, 4, "unterminated"},
		{"unterminated single", `v: 'it''s`, 4, "unterminated"},
		{"content after quote", `v: "a" b`, 8, "unexpected content"},
	}
	for _, c := range cases {
		t.Run(c.name, func(t *testing.T) {
			pe := mustFail(t, "a: 1\n"+c.src+"\n", 2, c.want)
			if pe.Col != c.col {
				t.Errorf("col %d, want %d", pe.Col, c.col)
			}
		})
	}
}
