package config

import (
	"context"
	"io"
)

// Loader defines the interface for loading configuration from various sources.
// Implementations can load from files, environment variables, remote sources, etc.
type Loader interface {
	// Load reads and parses configuration from the loader's source.
	Load() (Config, error)
	// LoadWithDefaults reads configuration and applies default values.
	LoadWithDefaults() (Config, error)
}

// FileLoader loads configuration from a file path.
type FileLoader struct {
	Path string
}

// Load reads configuration from the file.
func (fl FileLoader) Load() (Config, error) {
	return LoadFromFile(fl.Path)
}

// LoadWithDefaults reads configuration and applies defaults.
func (fl FileLoader) LoadWithDefaults() (Config, error) {
	cfg, err := LoadFromFile(fl.Path)
	if err != nil {
		return Config{}, err
	}
	cfg.Normalize()
	return cfg, nil
}

// ReaderLoader loads configuration from an io.Reader.
type ReaderLoader struct {
	Reader io.Reader
}

// Load reads configuration from the reader.
func (rl ReaderLoader) Load() (Config, error) {
	data, err := io.ReadAll(rl.Reader)
	if err != nil {
		return Config{}, err
	}
	return ParseConfig(data)
}

// LoadWithDefaults reads configuration and applies defaults.
func (rl ReaderLoader) LoadWithDefaults() (Config, error) {
	cfg, err := rl.Load()
	if err != nil {
		return Config{}, err
	}
	cfg.Normalize()
	return cfg, nil
}

// Validator defines the interface for configuration validation.
// Implementations can perform semantic validation beyond basic syntax checking.
type Validator interface {
	// Validate checks the configuration for errors.
	// Returns nil if the configuration is valid.
	Validate(cfg *Config) error
}

// SecurityValidator validates security-related configuration settings.
type SecurityValidator struct{}

// Validate performs security-focused validation.
func (sv SecurityValidator) Validate(cfg *Config) error {
	return ValidateConfig(cfg)
}

// CompositeValidator combines multiple validators.
type CompositeValidator struct {
	Validators []Validator
}

// Validate runs all validators and returns the first error encountered.
func (cv CompositeValidator) Validate(cfg *Config) error {
	for _, v := range cv.Validators {
		if err := v.Validate(cfg); err != nil {
			return err
		}
	}
	return nil
}

// ConfigWatcher defines the interface for watching configuration file changes.
type ConfigWatcher interface {
	// Watch starts watching for changes and invokes onChange when detected.
	// Blocks until the context is cancelled or an error occurs.
	Watch(ctx context.Context, onChange func(Config)) error
}

// FileWatcher watches a configuration file for changes.
type FileWatcher struct {
	Watcher  ConfigWatcher
	FilePath string
}

// Watch starts watching the file for changes.
func (fw FileWatcher) Watch(ctx context.Context, onChange func(Config)) error {
	w := Watcher{}
	return w.WatchFile(ctx, fw.FilePath, onChange)
}

// ParseConfig parses configuration from YAML bytes.
// This is a low-level function; most callers should use a Loader instead.
func ParseConfig(data []byte) (Config, error) {
	return parseConfig(data)
}
