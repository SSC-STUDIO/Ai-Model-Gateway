package config

import (
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestFileLoader_Load(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
listen: ":9090"
upstreams:
  - name: test
    base_url: https://example.com
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	loader := FileLoader{Path: configPath}
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
}

func TestFileLoader_Load_Error(t *testing.T) {
	loader := FileLoader{Path: "/nonexistent/config.yaml"}
	_, err := loader.Load()
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestFileLoader_LoadWithDefaults(t *testing.T) {
	// Create a temp config file
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")
	content := `
listen: ":9090"
upstreams:
  - name: test
    base_url: https://example.com
`
	if err := os.WriteFile(configPath, []byte(content), 0o644); err != nil {
		t.Fatalf("failed to write config file: %v", err)
	}

	loader := FileLoader{Path: configPath}
	cfg, err := loader.LoadWithDefaults()
	if err != nil {
		t.Fatalf("LoadWithDefaults failed: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}

	// Verify defaults were applied
	if cfg.Router.MaxRetries != 2 {
		t.Errorf("expected default MaxRetries 2, got %d", cfg.Router.MaxRetries)
	}
}

func TestFileLoader_LoadWithDefaults_Error(t *testing.T) {
	loader := FileLoader{Path: "/nonexistent/config.yaml"}
	_, err := loader.LoadWithDefaults()
	if err == nil {
		t.Error("expected error for non-existent file")
	}
}

func TestReaderLoader_Load(t *testing.T) {
	content := `
listen: ":9090"
upstreams:
  - name: test
    base_url: https://example.com
`
	loader := ReaderLoader{Reader: strings.NewReader(content)}
	cfg, err := loader.Load()
	if err != nil {
		t.Fatalf("Load failed: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}
}

func TestReaderLoader_Load_InvalidYAML(t *testing.T) {
	content := `{invalid yaml :::`
	loader := ReaderLoader{Reader: strings.NewReader(content)}
	_, err := loader.Load()
	if err == nil {
		t.Error("expected error for invalid YAML")
	}
}

func TestReaderLoader_Load_ReadError(t *testing.T) {
	// Use a reader that will fail
	loader := ReaderLoader{Reader: &errorReader{}}
	_, err := loader.Load()
	if err == nil {
		t.Error("expected error from failing reader")
	}
}

func TestReaderLoader_LoadWithDefaults(t *testing.T) {
	content := `
listen: ":9090"
upstreams:
  - name: test
    base_url: https://example.com
`
	loader := ReaderLoader{Reader: strings.NewReader(content)}
	cfg, err := loader.LoadWithDefaults()
	if err != nil {
		t.Fatalf("LoadWithDefaults failed: %v", err)
	}

	if cfg.Listen != ":9090" {
		t.Errorf("expected listen :9090, got %s", cfg.Listen)
	}

	// Verify defaults were applied
	if cfg.Router.Strategy != RouterStrategyHealthWeightedRR {
		t.Errorf("expected default strategy %s, got %s", RouterStrategyHealthWeightedRR, cfg.Router.Strategy)
	}
}

func TestSecurityValidator_Validate(t *testing.T) {
	validator := SecurityValidator{}

	tests := []struct {
		name    string
		cfg     *Config
		wantErr bool
	}{
		{
			name: "valid secure config",
			cfg: &Config{
				Admin: AdminConfig{
					Enabled:   true,
					AuthToken: "secure-token-with-at-least-32-characters-long",
				},
			},
			wantErr: false,
		},
		{
			name: "admin disabled no token needed",
			cfg: &Config{
				Admin: AdminConfig{Enabled: false},
			},
			wantErr: false,
		},
		{
			name: "admin enabled with default token",
			cfg: &Config{
				Admin: AdminConfig{
					Enabled:   true,
					AuthToken: "change-me-admin-token",
				},
			},
			wantErr: true,
		},
		{
			name: "admin enabled with short token",
			cfg: &Config{
				Admin: AdminConfig{
					Enabled:   true,
					AuthToken: "short",
				},
			},
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			err := validator.Validate(tt.cfg)
			if tt.wantErr && err == nil {
				t.Error("expected error")
			}
			if !tt.wantErr && err != nil {
				t.Errorf("unexpected error: %v", err)
			}
		})
	}
}

func TestCompositeValidator_Validate(t *testing.T) {
	// Test with no validators
	cv := CompositeValidator{Validators: []Validator{}}
	err := cv.Validate(&Config{})
	if err != nil {
		t.Errorf("empty CompositeValidator should return nil, got: %v", err)
	}

	// Test with one failing validator
	cv = CompositeValidator{
		Validators: []Validator{
			SecurityValidator{},
		},
	}
	err = cv.Validate(&Config{
		Admin: AdminConfig{Enabled: true, AuthToken: "short"},
	})
	if err == nil {
		t.Error("expected error from SecurityValidator")
	}

	// Test with multiple validators (first succeeds)
	cv = CompositeValidator{
		Validators: []Validator{
			SecurityValidator{},
		},
	}
	err = cv.Validate(&Config{
		Admin: AdminConfig{Enabled: false},
	})
	if err != nil {
		t.Errorf("unexpected error: %v", err)
	}
}

func TestParseConfigInterface(t *testing.T) {
	tests := []struct {
		name    string
		content string
		wantErr bool
	}{
		{
			name: "valid config",
			content: `
listen: ":8080"
upstreams:
  - name: test
    base_url: https://example.com
`,
			wantErr: false,
		},
		{
			name:    "invalid YAML",
			content: `{invalid`,
			wantErr: true,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg, err := ParseConfig([]byte(tt.content))
			if tt.wantErr {
				if err == nil {
					t.Error("expected error")
				}
			} else {
				if err != nil {
					t.Errorf("unexpected error: %v", err)
				}
				if cfg.Listen != ":8080" && tt.name == "valid config" {
					t.Errorf("expected Listen :8080, got %s", cfg.Listen)
				}
			}
		})
	}
}

// errorReader is a reader that always returns an error
type errorReader struct{}

func (r *errorReader) Read(p []byte) (n int, err error) {
	return 0, os.ErrInvalid
}
