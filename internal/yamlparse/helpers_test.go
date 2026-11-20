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

// mustFail asserts a parse error on wantLine whose message contains want.
func mustFail(t *testing.T, src string, wantLine int, want string) *Error {
	t.Helper()
	n, err := ParseYAML([]byte(src))
	if err == nil {
		t.Fatalf("ParseYAML(%q) = %s, want error", src, dump(n, true))
	}
	var pe *Error
	if !errors.As(err, &pe) {
		t.Fatalf("error %v is %T, want *Error", err, err)
	}
	if pe.Line != wantLine {
		t.Errorf("ParseYAML(%q) error %q: line %d, want %d", src, err, pe.Line, wantLine)
	}
	if !strings.Contains(pe.Msg, want) {
		t.Errorf("ParseYAML(%q) error %q, want it to contain %q", src, err, want)
	}
	if !strings.HasPrefix(err.Error(), fmt.Sprintf("%d:%d: ", pe.Line, pe.Col)) {
		t.Errorf("error %q lacks line:col prefix", err)
	}
	return pe
}
