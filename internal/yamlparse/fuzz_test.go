package yamlparse

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func FuzzParseYAML(f *testing.F) {
	for _, name := range []string{"gateway", "variant-a", "variant-b", "variant-c"} {
		if src, err := os.ReadFile(filepath.Join("..", "..", "configs", name+".yaml")); err == nil {
			f.Add(src)
		}
	}
	for _, s := range []string{"a: [1, {b: c}]\n", "- - x\n  - 'y'\n", "k: \"\\u00e9\"\n", "---\na:\n...\n", "a: &a {b: *a}\n", "r: {<<: [*x]}\n", "l: &l [ {<<: *l} ]\n"} {
		f.Add([]byte(s))
	}
	f.Fuzz(func(t *testing.T, src []byte) {
		n, err := ParseYAML(src)
		if err != nil {
			var pe *Error
			if !errors.As(err, &pe) || pe.Msg == "" || pe.Line < 0 || pe.Col < 0 || (pe.Line == 0 && pe.Col != 0) {
				t.Fatalf("bad error %v", err)
			}
			return
		}
		if n == nil {
			t.Fatal("nil node without error")
		}
	})
}
