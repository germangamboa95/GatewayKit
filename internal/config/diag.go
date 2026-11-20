package config

import (
	"fmt"
	"strings"

	"github.com/germangamboa95/gatewaykit/internal/yamlparse"
)

// diag collects errors and warnings, each formatted as
// file:line:col: field.path: message.
type diag struct {
	File     string
	Errors   []string
	Warnings []string
}

func (d *diag) format(n *yamlparse.Node, path, msg string) string {
	line, col := 1, 1
	if n != nil {
		line, col = n.Line, n.Col
	}
	if path == "" {
		return fmt.Sprintf("%s:%d:%d: %s", d.File, line, col, msg)
	}
	return fmt.Sprintf("%s:%d:%d: %s: %s", d.File, line, col, path, msg)
}

func (d *diag) Errorf(n *yamlparse.Node, path, format string, args ...any) {
	d.Errors = append(d.Errors, d.format(n, path, fmt.Sprintf(format, args...)))
}

func (d *diag) Warnf(n *yamlparse.Node, path, format string, args ...any) {
	d.Warnings = append(d.Warnings, d.format(n, path, fmt.Sprintf(format, args...)))
}

// Err returns nil when no errors were collected.
func (d *diag) Err() error {
	if len(d.Errors) == 0 {
		return nil
	}
	return &LoadError{File: d.File, Problems: append([]string(nil), d.Errors...)}
}

// LoadError holds every problem found in one config file.
type LoadError struct {
	File     string
	Problems []string
}

// Error returns every problem, one per line.
func (e *LoadError) Error() string { return strings.Join(e.Problems, "\n") }

// Summary is the header line printed before the problems.
func (e *LoadError) Summary() string {
	word := "problems"
	if len(e.Problems) == 1 {
		word = "problem"
	}
	return fmt.Sprintf("config error: %d %s in %s", len(e.Problems), word, e.File)
}
