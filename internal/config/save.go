package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"

	"gopkg.in/yaml.v3"
)

// SaveToFile saves the configuration to a file with sensitive data masked
func SaveToFile(path string, cfg Config) error {
	RelativizePaths(&cfg, path)
	
	// SECURITY FIX: Sanitize sensitive data before saving
	// Create a copy with masked API keys and auth tokens
	sanitizedCfg := sanitizeConfigForExport(cfg)
	
	data, err := yaml.Marshal(sanitizedCfg)
	if err != nil {
		return fmt.Errorf("marshal config: %w", err)
	}
	if err := os.WriteFile(path, data, 0o600); err != nil {
		return fmt.Errorf("write config: %w", err)
	}
	return nil
}

// SaveToFileWithEncryption saves the configuration with encrypted sensitive data
func SaveToFileWithEncryption(path string, cfg Config, masterKey string) error {
	storage, err := NewSecureStorage(masterKey)
	if err != nil {
		return fmt.Errorf("failed to initialize secure storage: %w", err)
	}
	
	// Encrypt API keys before saving
	for i := range cfg.Upstreams {
		if cfg.Upstreams[i].APIKey != "" && !IsEncrypted(cfg.Upstreams[i].APIKey) {
			encrypted, err := storage.Encrypt(cfg.Upstreams[i].APIKey)
			if err != nil {
				return fmt.Errorf("failed to encrypt API key for upstream %s: %w", cfg.Upstreams[i].Name, err)
			}
			cfg.Upstreams[i].APIKey = encrypted
		}
	}
	
	// Encrypt admin auth token if present
	if cfg.Admin.AuthToken != "" && !IsEncrypted(cfg.Admin.AuthToken) {
		encrypted, err := storage.Encrypt(cfg.Admin.AuthToken)
		if err != nil {
			return fmt.Errorf("failed to encrypt admin auth token: %w", err)
		}
		cfg.Admin.AuthToken = encrypted
	}
	
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

// sanitizeConfigForExport creates a copy of the config with sensitive data masked for export
func sanitizeConfigForExport(cfg Config) Config {
	// Create a copy
	sanitized := Config{
		Listen:    cfg.Listen,
		Reload:    cfg.Reload,
		Router:    cfg.Router,
		Health:    cfg.Health,
		Admin:     cfg.Admin,
		Telemetry: cfg.Telemetry,
		Pricing:   cfg.Pricing,
		Bridge:    cfg.Bridge,
		Proxy:     cfg.Proxy,
		Upstreams: make([]Upstream, len(cfg.Upstreams)),
	}
	
	// Mask admin auth token
	if sanitized.Admin.AuthToken != "" {
		sanitized.Admin.AuthToken = MaskKey(sanitized.Admin.AuthToken)
	}
	
	// Mask API keys in upstreams
	for i, u := range cfg.Upstreams {
		sanitized.Upstreams[i] = Upstream{
			Name:                u.Name,
			BaseURL:             u.BaseURL,
			APIKey:              MaskKey(u.APIKey),
			ProviderClass:       u.ProviderClass,
			Models:              append([]string(nil), u.Models...),
			Weight:              u.Weight,
			TimeoutMs:           u.TimeoutMs,
			SameUpstreamRetries: u.SameUpstreamRetries,
			Enabled:             u.Enabled,
			Headers:             copyStringMap(u.Headers),
		}
	}
	
	return sanitized
}

func copyStringMap(m map[string]string) map[string]string {
	if m == nil {
		return nil
	}
	copied := make(map[string]string, len(m))
	for k, v := range m {
		copied[k] = v
	}
	return copied
}

func RelativizePaths(cfg *Config, sourcePath string) {
	if cfg == nil {
		return
	}
	baseDir := filepath.Dir(sourcePath)
	realBaseDir, _ := filepath.Abs(baseDir)
	realBaseDir = filepath.Clean(realBaseDir)

	if cfg.Telemetry.SQLitePath == "" {
		goto pricingPath
	}
	if filepath.IsAbs(cfg.Telemetry.SQLitePath) {
		// SECURITY FIX: 验证 SQLitePath 在基础目录内
		realPath, _ := filepath.Abs(cfg.Telemetry.SQLitePath)
		realPath = filepath.Clean(realPath)
		if !strings.HasPrefix(realPath, realBaseDir+string(filepath.Separator)) {
			// 路径不在基础目录内，保留原值
			goto pricingPath
		}
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
	// SECURITY FIX: 验证 CachePath 在基础目录内
	realPath, _ := filepath.Abs(cfg.Pricing.CachePath)
	realPath = filepath.Clean(realPath)
	if !strings.HasPrefix(realPath, realBaseDir+string(filepath.Separator)) {
		// 路径不在基础目录内，保留原值
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
