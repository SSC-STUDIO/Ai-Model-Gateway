// Package configloader loads and parses configuration files.
package configloader

import (
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"

	"ai-model-gateway/internal/core"

	"gopkg.in/yaml.v3"
)

var envVarPattern = regexp.MustCompile(`\$\{([A-Za-z_][A-Za-z0-9_]*)\}|\$([A-Za-z_][A-Za-z0-9_]*)`)

// LoadFromFile reads and parses a config YAML file.
// The returned Config is normalised with defaults applied.
// Relative paths (e.g. sqlite_path) are resolved relative to the config file's directory.
func LoadFromFile(path string) (*core.Config, error) {
	data, err := os.ReadFile(path)
	if err != nil {
		return nil, fmt.Errorf("read config %s: %w", path, err)
	}
	return loadFromBytes(path, data)
}

// LoadFromReader parses a config from an io.Reader.
func LoadFromReader(r io.Reader) (*core.Config, error) {
	data, err := io.ReadAll(r)
	if err != nil {
		return nil, fmt.Errorf("read config: %w", err)
	}
	return Parse(data)
}

// Parse decodes raw YAML bytes into a normalised and validated Config.
func Parse(data []byte) (*core.Config, error) {
	var root yaml.Node
	if err := yaml.Unmarshal(data, &root); err != nil {
		return nil, fmt.Errorf("parse config yaml: %w", err)
	}
	if err := expandEnvInNode(&root); err != nil {
		return nil, err
	}

	var cfg core.Config
	if err := root.Decode(&cfg); err != nil {
		return nil, fmt.Errorf("decode config yaml: %w", err)
	}
	cfg.Normalize()
	if err := cfg.Validate(); err != nil {
		return nil, fmt.Errorf("validate config: %w", err)
	}
	return &cfg, nil
}

func loadFromBytes(path string, data []byte) (*core.Config, error) {
	cfg, err := Parse(data)
	if err != nil {
		return nil, err
	}

	baseDir := filepath.Dir(path)
	cfg.Telemetry.SQLitePath = resolveConfigPath(baseDir, cfg.Telemetry.SQLitePath)
	cfg.Pricing.CachePath = resolveConfigPath(baseDir, cfg.Pricing.CachePath)
	cfg.Pricing.FX.CachePath = resolveConfigPath(baseDir, cfg.Pricing.FX.CachePath)
	return cfg, nil
}

func resolveConfigPath(baseDir, raw string) string {
	if raw == "" {
		return ""
	}
	normalized := filepath.FromSlash(strings.ReplaceAll(raw, "\\", "/"))
	if filepath.IsAbs(normalized) {
		return filepath.Clean(normalized)
	}
	return filepath.Clean(filepath.Join(baseDir, normalized))
}

func expandEnvInNode(node *yaml.Node) error {
	if node == nil {
		return nil
	}
	if node.Kind == yaml.ScalarNode {
		expanded, err := expandEnvValue(node.Value)
		if err != nil {
			return err
		}
		node.Value = expanded
	}
	for i := range node.Content {
		if err := expandEnvInNode(node.Content[i]); err != nil {
			return err
		}
	}
	return nil
}

func expandEnvValue(value string) (string, error) {
	missing := make(map[string]struct{})
	expanded := envVarPattern.ReplaceAllStringFunc(value, func(match string) string {
		name := strings.TrimPrefix(match, "$")
		name = strings.TrimPrefix(name, "{")
		name = strings.TrimSuffix(name, "}")
		if value, ok := os.LookupEnv(name); ok {
			return value
		}
		missing[name] = struct{}{}
		return ""
	})

	if len(missing) == 0 {
		return expanded, nil
	}

	names := make([]string, 0, len(missing))
	for name := range missing {
		names = append(names, name)
	}
	sort.Strings(names)
	return "", fmt.Errorf("expand config env: undefined environment variables: %s", strings.Join(names, ", "))
}
