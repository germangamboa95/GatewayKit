package config

import (
	"os"
	"path/filepath"
	"reflect"
	"strings"
	"testing"
	"time"

	"gatewaykit/internal/yamlparse"
)

func requireParser(t *testing.T) {
	t.Helper()
	if _, err := yamlparse.ParseYAML([]byte("a: 1\n")); err != nil {
		t.Skipf("yamlparse not available: %v", err)
	}
}

func TestLoadConfigs(t *testing.T) {
	requireParser(t)
	cfg, warns, err := Load("../../configs/gateway.yaml")
	if err != nil || len(warns) != 0 {
		t.Fatalf("sample: err %v warns %v", err, warns)
	}
	if len(cfg.Routes) != 5 {
		t.Fatalf("sample routes %d", len(cfg.Routes))
	}
	for _, f := range []string{"variant-a.yaml", "variant-b.yaml", "variant-c.yaml"} {
		if _, _, err := Load(filepath.Join("../../configs", f)); err != nil {
			t.Errorf("%s: %v", f, err)
		}
	}
	b, _, err := Load("../../configs/variant-b.yaml")
	if err != nil {
		t.Fatal(err)
	}
	if b.Port != 9090 || b.Routes[0].Path != "/v2" || b.Routes[1].Path != "/v2/admin" || b.Routes[0].Timeout != 2*time.Second {
		t.Errorf("variant b %+v %+v", b, b.Routes[0])
	}
}

func TestLoadSampleMatchesFixture(t *testing.T) {
	requireParser(t)
	cfg, _, err := Load("../../configs/gateway.yaml")
	if err != nil {
		t.Fatal(err)
	}
	want := Sample()
	if !reflect.DeepEqual(cfg, want) {
		for i := range want.Routes {
			if !reflect.DeepEqual(cfg.Routes[i], want.Routes[i]) {
				t.Errorf("route %d:\n got %+v\nwant %+v", i, cfg.Routes[i], want.Routes[i])
			}
		}
		t.Errorf("sample does not match fixture")
	}
}

func TestLoadErrors(t *testing.T) {
	requireParser(t)
	dir := t.TempDir()
	p := filepath.Join(dir, "gateway.yaml")
	src := "gateway: {}\nroutes:\n  - path: \"/\"\n    upstream:\n      url: \"http://localhost:3001\"\n      balance: random\n"
	if err := os.WriteFile(p, []byte(src), 0o644); err != nil {
		t.Fatal(err)
	}
	_, _, err := Load(p)
	if err == nil || !strings.HasPrefix(err.Error(), p+":6:") || !strings.Contains(err.Error(), "routes[0].upstream.balance") {
		t.Fatalf("err = %v", err)
	}

	if err := os.WriteFile(p, []byte("gateway:\n\tport: 1\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if _, _, err := Load(p); err == nil || !strings.HasPrefix(err.Error(), p+":") {
		t.Fatalf("parse err = %v", err)
	}

	if _, _, err := Load(filepath.Join(dir, "missing.yaml")); err == nil {
		t.Fatal("want error for missing file")
	}
}
