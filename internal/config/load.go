package config

import (
	"fmt"
	"os"

	"gatewaykit/internal/contract"
	"gatewaykit/internal/yamlparse"
)

// Load reads, parses, decodes, validates and normalizes the config at path.
// It returns the warnings even when it fails. A non-nil error from a config
// problem is a *LoadError.
func Load(path string) (*contract.GatewayConfig, []string, error) {
	src, err := os.ReadFile(path)
	if err != nil {
		return nil, nil, fmt.Errorf("config: %w", err)
	}
	root, err := yamlparse.ParseYAML(src)
	if err != nil {
		return nil, nil, &LoadError{File: path, Problems: []string{fmt.Sprintf("%s:%s", path, err.Error())}}
	}
	return LoadNode(path, root)
}

// LoadNode runs decode, validate and normalize on a parsed tree.
func LoadNode(file string, root *yamlparse.Node) (*contract.GatewayConfig, []string, error) {
	d := &Diag{File: file}
	raw := Decode(root, d)
	Validate(raw, d)
	cfg := Normalize(raw, d)
	if err := d.Err(); err != nil {
		return nil, d.Warnings, err
	}
	return cfg, d.Warnings, nil
}
