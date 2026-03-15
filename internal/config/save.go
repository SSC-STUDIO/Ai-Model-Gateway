package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

func SaveToFile(path string, cfg Config) error {
	RelativizePaths(&cfg, path)
	data, err := yaml.Marshal(cfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func RelativizePaths(cfg *Config, sourcePath string) {
	if cfg == nil {
		return
	}
	baseDir := filepath.Dir(sourcePath)
	if cfg.Telemetry.SQLitePath == "" {
		goto pricingPath
	}
	if filepath.IsAbs(cfg.Telemetry.SQLitePath) {
		rel, err := filepath.Rel(baseDir, cfg.Telemetry.SQLitePath)
		if err == nil && !strings.HasPrefix(rel, "..") {
			cfg.Telemetry.SQLitePath = filepath.Clean(rel)
		}
	}

pricingPath:
	if cfg.Pricing.CachePath == "" {
		return
	}
	if !filepath.IsAbs(cfg.Pricing.CachePath) {
		return
	}
	rel, err := filepath.Rel(baseDir, cfg.Pricing.CachePath)
	if err != nil {
		return
	}
	if strings.HasPrefix(rel, "..") {
		return
	}
	cfg.Pricing.CachePath = filepath.Clean(rel)
}
