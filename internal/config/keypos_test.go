package config

import (
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"gatewaykit/internal/yamlparse"
)

func loadSrc(t *testing.T, src string) ([]string, error) {
	t.Helper()
	root, err := yamlparse.ParseYAML([]byte(src))
	if err != nil {
		t.Fatal(err)
	}
	_, warns, err := LoadNode("gateway.yaml", root)
	return warns, err
}

func TestUnknownKeyWarningAtKey(t *testing.T) {
	src := "gateway: {}\nroutes:\n  - path: \"/\"\n    upstream: { url: \"http://localhost:3001\" }\n    cache_ttl: \"10s\"\n    extra:\n      a: 1\n"
	warns, err := loadSrc(t, src)
	if err != nil {
		t.Fatal(err)
	}
	hasLine(t, warns, "gateway.yaml:5:5: routes[0].cache_ttl: unknown key")
	hasLine(t, warns, "gateway.yaml:6:5: routes[0].extra: unknown key")
}

func TestMappingConflictAtKey(t *testing.T) {
	src := "gateway: {}\nroutes:\n  - path: \"/\"\n    upstream: { url: \"http://localhost:3001\" }\n    request_transform:\n      body:\n        mapping:\n          a: \"x\"\n          a.b: \"y\"\n"
	_, err := loadSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "gateway.yaml:9:11: routes[0].request_transform.body.mapping.a.b: destination") {
		t.Fatalf("got %v", err)
	}
}

func TestBothPlacementsNameKeyLines(t *testing.T) {
	src := "gateway:\n  routes:\n    - path: \"/a\"\n      upstream: { url: \"http://localhost:3001\" }\nroutes:\n  - path: \"/b\"\n    upstream: { url: \"http://localhost:3001\" }\n"
	_, err := loadSrc(t, src)
	if err == nil || !strings.Contains(err.Error(), "top level (line 5) and under gateway (line 2)") {
		t.Fatalf("got %v", err)
	}
}

func TestLoadCleansDisplayPath(t *testing.T) {
	dir := t.TempDir()
	if err := os.WriteFile(filepath.Join(dir, "gateway.yaml"), []byte("gateway: {port: abc}\nroutes: []\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	t.Chdir(dir)
	_, _, err := Load("./gateway.yaml")
	var le *LoadError
	if !errors.As(err, &le) {
		t.Fatalf("got %v", err)
	}
	if le.Summary() != "config error: 1 problem in gateway.yaml" || !strings.HasPrefix(le.Problems[0], "gateway.yaml:1:17: gateway.port:") {
		t.Fatalf("got %q %q", le.Summary(), le.Problems)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.yaml"), []byte("a: [\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = Load("./gateway.yaml")
	if !errors.As(err, &le) || !strings.HasPrefix(le.Problems[0], "gateway.yaml:1: ") {
		t.Fatalf("got %v", err)
	}
	if err := os.WriteFile(filepath.Join(dir, "gateway.yaml"), []byte("a: 1\nb: *nope\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err = Load("./gateway.yaml")
	if !errors.As(err, &le) || le.Problems[0] != "gateway.yaml: unknown anchor 'nope' referenced" {
		t.Fatalf("got %v", err)
	}
}
