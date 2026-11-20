package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestResolvePath(t *testing.T) {
	dir := t.TempDir()
	mk := func(name string) string {
		p := filepath.Join(dir, name)
		if err := os.WriteFile(p, []byte("gateway: {}\nroutes: []\n"), 0o644); err != nil {
			t.Fatal(err)
		}
		return p
	}
	a, b, c := mk("a.yaml"), mk("b.yaml"), mk("c.yaml")
	env := func(v string) func(string) string {
		return func(k string) string {
			if k == "GATEWAY_CONFIG" {
				return v
			}
			return ""
		}
	}
	cases := []struct {
		name string
		args []string
		env  string
		want string
	}{
		{"arg wins", []string{a, "--config", b}, c, a},
		{"flag before arg", []string{"--config", b, a}, c, a},
		{"flag beats env", []string{"--config", b}, c, b},
		{"flag equals form", []string{"--config=" + b}, c, b},
		{"env beats default", nil, c, c},
	}
	for _, tc := range cases {
		got, err := ResolvePath(tc.args, env(tc.env))
		if err != nil || got != tc.want {
			t.Errorf("%s: got %q, %v; want %q", tc.name, got, err, tc.want)
		}
	}
}

func TestResolvePathDefault(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	if _, err := ResolvePath(nil, func(string) string { return "" }); err == nil ||
		!strings.Contains(err.Error(), DefaultPath) {
		t.Fatalf("missing default: err = %v", err)
	}
	if err := os.WriteFile("gateway.yaml", []byte("gateway: {}\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	got, err := ResolvePath(nil, func(string) string { return "" })
	if err != nil || got != DefaultPath {
		t.Fatalf("got %q, %v", got, err)
	}
}

func TestResolvePathListsTried(t *testing.T) {
	dir := t.TempDir()
	t.Chdir(dir)
	_, err := ResolvePath([]string{"x.yaml", "--config", "y.yaml"}, func(string) string { return "z.yaml" })
	if err == nil {
		t.Fatal("want error")
	}
	for _, p := range []string{"x.yaml", "y.yaml", "z.yaml", DefaultPath} {
		if !strings.Contains(err.Error(), p) {
			t.Errorf("error %q does not list %s", err, p)
		}
	}
}
