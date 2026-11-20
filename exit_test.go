package main

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strings"
	"testing"
)

var binPath string

func TestMain(m *testing.M) {
	dir, err := os.MkdirTemp("", "gatewaykit-bin-")
	if err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
	binPath = filepath.Join(dir, "gatewaykit")
	if runtime.GOOS == "windows" {
		binPath += ".exe"
	}
	code := m.Run()
	os.RemoveAll(dir)
	os.Exit(code)
}

func discardLogger() *slog.Logger { return slog.New(slog.NewTextHandler(io.Discard, nil)) }

func goTool() string {
	if p, err := exec.LookPath("go"); err == nil {
		return p
	}
	return filepath.Join(runtime.GOROOT(), "bin", "go")
}

func buildBinary(t *testing.T) string {
	t.Helper()
	if _, err := os.Stat(binPath); err == nil {
		return binPath
	}
	wd, err := os.Getwd()
	if err != nil {
		t.Fatal(err)
	}
	cmd := exec.Command(goTool(), "build", "-o", binPath, ".")
	cmd.Dir = wd
	if out, err := cmd.CombinedOutput(); err != nil {
		t.Fatalf("go build: %v\n%s", err, out)
	}
	return binPath
}

func runBinary(t *testing.T, dir string, env []string, args ...string) (int, string) {
	t.Helper()
	cmd := exec.Command(buildBinary(t), args...)
	cmd.Dir = dir
	cmd.Env = append(os.Environ(), env...)
	var stderr bytes.Buffer
	cmd.Stderr = &stderr
	err := cmd.Run()
	var ee *exec.ExitError
	switch {
	case err == nil:
		return 0, stderr.String()
	case errors.As(err, &ee):
		return ee.ExitCode(), stderr.String()
	default:
		t.Fatalf("run: %v", err)
		return -1, ""
	}
}

func TestExitBrokenConfig(t *testing.T) {
	wd, _ := os.Getwd()
	code, stderr := runBinary(t, wd, []string{"GATEWAY_CONFIG="}, "testdata/broken.yaml")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, stderr)
	}
	if !strings.Contains(stderr, "config error: 3 problems") {
		t.Errorf("missing summary:\n%s", stderr)
	}
	re := regexp.MustCompile(`(?m)^\s+testdata/broken\.yaml:(\d+):(\d+): (.+)$`)
	got := re.FindAllStringSubmatch(stderr, -1)
	if len(got) != 3 {
		t.Fatalf("located errors = %d, want 3\n%s", len(got), stderr)
	}
	want := []string{
		`routes[0].upstream: exactly one of "url" or "targets" is required`,
		`routes[1].upstream.targets[0].weight: must be an integer >= 1 (got 0)`,
		`routes[2].response_transform.headers.add.X-Time: "$requst_time" is not a known variable`,
	}
	for i, g := range got {
		if g[3] != want[i] {
			t.Errorf("error %d = %q, want %q", i, g[3], want[i])
		}
	}
}

func TestExitMissingConfig(t *testing.T) {
	dir := t.TempDir()
	code, stderr := runBinary(t, dir, []string{"GATEWAY_CONFIG=from-env.yaml"},
		"from-arg.yaml", "--config", "from-flag.yaml")
	if code != 1 {
		t.Fatalf("exit = %d, want 1\n%s", code, stderr)
	}
	for _, loc := range []string{"from-arg.yaml", "from-flag.yaml", "from-env.yaml", "./gateway.yaml"} {
		if !strings.Contains(stderr, loc) {
			t.Errorf("stderr does not list %s:\n%s", loc, stderr)
		}
	}
}
