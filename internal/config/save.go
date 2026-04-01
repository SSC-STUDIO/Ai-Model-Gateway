package config

import (
	"errors"
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
	if err := writeFileAtomically(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

func writeFileAtomically(path string, data []byte, perm os.FileMode) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}

	temp, err := os.CreateTemp(dir, filepath.Base(path)+".tmp-*")
	if err != nil {
		return err
	}
	tempPath := temp.Name()
	success := false
	defer func() {
		if !success {
			_ = os.Remove(tempPath)
		}
	}()

	if err := temp.Chmod(perm); err != nil {
		_ = temp.Close()
		return err
	}
	if _, err := temp.Write(data); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Sync(); err != nil {
		_ = temp.Close()
		return err
	}
	if err := temp.Close(); err != nil {
		return err
	}

	if err := os.Rename(tempPath, path); err != nil {
		if removeErr := os.Remove(path); removeErr != nil && !errors.Is(removeErr, os.ErrNotExist) {
			return removeErr
		}
		if err := os.Rename(tempPath, path); err != nil {
			return err
		}
	}

	success = true
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
