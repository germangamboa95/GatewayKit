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
		{"double hex escape", `"a\x41"`, "aA", true},
		{"str tag", `!!str 8080`, "8080", true},
		{"str tag on null word", `!!str null`, "null", true},
		{"literal block", "|\n  line one\n  line two", "line one\nline two\n", true},
		{"literal block strip", "|-\n  text", "text", true},
		{"folded block", ">\n  one\n  two", "one two\n", true},
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

func TestMultiLineScalars(t *testing.T) {
	src := "desc: first part\n  second part\nq: \"a\n  b\"\n"
	if got := dump(mustParse(t, src), true); got != `{"desc": "first part second part"@1:7, "q": "a b"q@3:4}@1:1` {
		t.Errorf("got %s", got)
	}
}
