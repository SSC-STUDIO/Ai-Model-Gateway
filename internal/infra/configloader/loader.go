// Package configloader loads and parses v2 configuration files.
package configloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"

	"ai-model-gateway/internal/core"

	"gopkg.in/yaml.v3"
)

// LoadFromFile reads and parses a v2 config YAML file.
// The returned Config is normalised with defaults applied.
// Relative paths (e.g. sqlite_path) are resolved relative to the config file's directory.
func LoadFromFile(path string) (*core.Config, error) {
	f, err := os.Open(path)
	if err != nil {
		return nil, fmt.Errorf("open config %s: %w", path, err)
	}
	defer f.Close()

	cfg, err := LoadFromReader(f)
	if err != nil {
		return nil, err
	}

	// Resolve relative paths relative to config file directory.
	baseDir := filepath.Dir(path)
	if cfg.Telemetry.SQLitePath != "" && !filepath.IsAbs(cfg.Telemetry.SQLitePath) {
		cfg.Telemetry.SQLitePath = filepath.Clean(filepath.Join(baseDir, cfg.Telemetry.SQLitePath))
	}
	if cfg.Pricing.CachePath != "" && !filepath.IsAbs(cfg.Pricing.CachePath) {
		cfg.Pricing.CachePath = filepath.Clean(filepath.Join(baseDir, cfg.Pricing.CachePath))
	}
	return cfg, nil
}

// LoadFromReader parses a v2 config from an io.Reader.
func LoadFromReader(r io.Reader) (*core.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes raw YAML bytes into a normalised and validated Config.
func Parse(data []byte) (*core.Config, error) {
	var cfg core.Config
	if err := yaml.Unmarshal(data, &cfg); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return &cfg, nil
}
