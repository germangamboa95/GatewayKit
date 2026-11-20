package yamlparse

import (
	"errors"
	"fmt"
	"strconv"
	"strings"
	"testing"
)

// dump renders a node tree compactly. With pos, each node gets @line:col.
func dump(n *Node, pos bool) string {
	var b strings.Builder
	writeNode(&b, n, pos)
	return b.String()
}

func writeNode(b *strings.Builder, n *Node, pos bool) {
	switch n.Kind {
	case Null:
		b.WriteString("null")
	case Scalar:
		b.WriteString(strconv.Quote(n.Value))
		if n.Quoted {
			b.WriteString("q")
		}
	case Seq:
		b.WriteString("[")
		for i, it := range n.Items {
			if i > 0 {
				b.WriteString(", ")
			}
			writeNode(b, it, pos)
		}
		b.WriteString("]")
	case Map:
		b.WriteString("{")
		if len(n.Keys) != len(n.Map) {
			fmt.Fprintf(b, "!keys=%d,map=%d!", len(n.Keys), len(n.Map))
		}
		for i, k := range n.Keys {
			if i > 0 {
				b.WriteString(", ")
			}
			b.WriteString(strconv.Quote(k))
			b.WriteString(": ")
			writeNode(b, n.Map[k], pos)
		}
		b.WriteString("}")
	}
	if pos {
		fmt.Fprintf(b, "@%d:%d", n.Line, n.Col)
	}
}

func mustParse(t *testing.T, src string) *Node {
	t.Helper()
	n, err := ParseYAML([]byte(src))
	if err != nil {
		t.Fatalf("ParseYAML(%q): %v", src, err)
	}
	return n
}

// mustFail asserts a parse error whose message contains want and returns
// it, so callers can check its position.
func mustFail(t *testing.T, src, want string) *Error {
	t.Helper()
	n, err := ParseYAML([]byte(src))
	if err == nil {
		t.Fatalf("ParseYAML(%q) = %s, want error", src, dump(n, true))
	}
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is %T, want *Error", err, err)
	}
	if !strings.Contains(pe.Msg, want) {
		t.Errorf("ParseYAML(%q) error %q, want it to contain %q", src, err, want)
	}
	return pe
}

// mustFailAt asserts a parse error at line:col whose message contains want.
func mustFailAt(t *testing.T, src string, line, col int, want string) {
	t.Helper()
	pe := mustFail(t, src, want)
	if pe.Line != line || pe.Col != col {
		t.Errorf("ParseYAML(%q) error %q at %d:%d, want %d:%d", src, pe, pe.Line, pe.Col, line, col)
	}
	if prefix := fmt.Sprintf("%d:%d: ", line, col); !strings.HasPrefix(pe.Error(), prefix) {
		t.Errorf("error %q lacks %q prefix", pe, prefix)
	}
}
