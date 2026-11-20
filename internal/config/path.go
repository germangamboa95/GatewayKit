package config

import (
	"errors"
	"fmt"
	"os"
	"strings"
)

// DefaultPath is the last location ResolvePath tries.
const DefaultPath = "./gateway.yaml"

// ResolvePath picks the config path from, in order: the first positional
// argument, --config, GATEWAY_CONFIG, then ./gateway.yaml. args excludes the
// program name. The chosen file must exist; otherwise the error lists every
// location tried.
func ResolvePath(args []string, getenv func(string) string) (string, error) {
	var positional, flag string
	for i := 0; i < len(args); i++ {
		a := args[i]
		switch {
		case a == "--config" || a == "-config":
			if i+1 >= len(args) {
				return "", errors.New("--config needs a path")
			}
			if flag == "" {
				flag = args[i+1]
			}
			i++
		case strings.HasPrefix(a, "--config="):
			if flag == "" {
				flag = strings.TrimPrefix(a, "--config=")
			}
		case strings.HasPrefix(a, "-config="):
			if flag == "" {
				flag = strings.TrimPrefix(a, "-config=")
			}
		case positional == "" && !strings.HasPrefix(a, "-"):
			positional = a
		}
	}
	env := ""
	if getenv != nil {
		env = getenv("GATEWAY_CONFIG")
	}
	var tried []string
	for _, c := range []struct{ src, path string }{
		{"argument", positional},
		{"--config", flag},
		{"GATEWAY_CONFIG", env},
		{"default", DefaultPath},
	} {
		if c.path == "" {
			continue
		}
		if st, err := os.Stat(c.path); err == nil && !st.IsDir() {
			return c.path, nil
		}
		tried = append(tried, fmt.Sprintf("%s (%s)", c.path, c.src))
	}
	return "", fmt.Errorf("config file not found; tried: %s", strings.Join(tried, ", "))
}
