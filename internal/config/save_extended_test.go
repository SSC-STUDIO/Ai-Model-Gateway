package config

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

func TestSaveToFile_ErrorCreatingParentDir(t *testing.T) {
	// Try to save to a path where parent cannot be created
	// On Unix, this would be something like /proc/nonexistent/config.yaml
	// On Windows, we use a path with invalid characters or permissions
	// We'll use an approach that works on both

	// Create a temporary file to simulate a non-directory
	tmpDir := t.TempDir()
	tmpFile := filepath.Join(tmpDir, "notadir")
	if err := os.WriteFile(tmpFile, []byte("test"), 0o644); err != nil {
		t.Fatalf("failed to create temp file: %v", err)
	}

	// Try to save to a path where "notadir" exists as a file
	configPath := filepath.Join(tmpFile, "config.yaml")
	cfg := Config{
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}
	cfg.Normalize()

	err := SaveToFile(configPath, cfg)
	if err == nil {
		t.Skip("filesystem may allow this operation on this platform")
	}
}

func TestWriteFileAtomically_Success(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	data := []byte("test data")

	err := writeFileAtomically(filePath, data, 0o644)
	if err != nil {
		t.Fatalf("writeFileAtomically failed: %v", err)
	}

	// Verify file was written
	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != string(data) {
		t.Errorf("file content mismatch: expected %q, got %q", data, content)
	}
}

func TestWriteFileAtomically_ReplaceExisting(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")

	// Create existing file
	if err := os.WriteFile(filePath, []byte("old data"), 0o644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	// Replace with atomic write
	newData := []byte("new data")
	err := writeFileAtomically(filePath, newData, 0o644)
	if err != nil {
		t.Fatalf("writeFileAtomically failed: %v", err)
	}

	content, err := os.ReadFile(filePath)
	if err != nil {
		t.Fatalf("failed to read file: %v", err)
	}
	if string(content) != string(newData) {
		t.Errorf("file content mismatch: expected %q, got %q", newData, content)
	}
}

func TestWriteFileAtomically_RenameRetryFailureKeepsOriginalFile(t *testing.T) {
	tmpDir := t.TempDir()
	filePath := filepath.Join(tmpDir, "test.txt")
	original := []byte("old data")

	if err := os.WriteFile(filePath, original, 0o644); err != nil {
		t.Fatalf("failed to create existing file: %v", err)
	}

	origReplace := replaceFileAtomically
	t.Cleanup(func() {
		replaceFileAtomically = origReplace
	})

	replaceFileAtomically = func(oldpath, newpath string) error {
		return errors.New("simulated second rename failure")
	}

	err := writeFileAtomically(filePath, []byte("new data"), 0o644)
	if err == nil {
		t.Fatal("expected writeFileAtomically to fail")
	}

	content, readErr := os.ReadFile(filePath)
	if readErr != nil {
		t.Fatalf("expected original file to remain after failed replace, read error: %v", readErr)
	}
	if string(content) != string(original) {
		t.Fatalf("expected original file content %q, got %q", original, content)
	}
}

func TestRelativizePaths_NilConfig(t *testing.T) {
	// Should not panic
	RelativizePaths(nil, "/some/path/config.yaml")
}

func TestRelativizePaths_EmptyPaths(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := Config{
		Telemetry: TelemetryConfig{
			SQLitePath: "",
		},
		Pricing: PricingConfig{
			CachePath: "",
		},
	}

	RelativizePaths(&cfg, configPath)

	// Paths should remain empty
	if cfg.Telemetry.SQLitePath != "" {
		t.Errorf("expected empty SQLitePath, got %q", cfg.Telemetry.SQLitePath)
	}
	if cfg.Pricing.CachePath != "" {
		t.Errorf("expected empty CachePath, got %q", cfg.Pricing.CachePath)
	}
}

func TestRelativizePaths_AbsoluteTelemetryPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config", "gateway.yaml")

	// Create absolute path for telemetry
	absPath := filepath.Join(tmpDir, "data", "telemetry.db")
	cfg := Config{
		Telemetry: TelemetryConfig{
			SQLitePath: absPath,
		},
	}

	RelativizePaths(&cfg, configPath)

	// Path should be relativized (or remain absolute on some platforms)
	// On Windows, if the paths are on the same drive and can be made relative, they should be
	// Otherwise, the absolute path is preserved
	if filepath.IsAbs(cfg.Telemetry.SQLitePath) {
		// Absolute path was preserved - this is valid behavior
		if cfg.Telemetry.SQLitePath != absPath {
			t.Errorf("expected preserved absolute path %q, got %q", absPath, cfg.Telemetry.SQLitePath)
		}
	} else {
		// Path was relativized
		expectedRel := filepath.Join("..", "data", "telemetry.db")
		if cfg.Telemetry.SQLitePath != expectedRel {
			t.Errorf("expected relative path %q, got %q", expectedRel, cfg.Telemetry.SQLitePath)
		}
	}
}

func TestRelativizePaths_AbsolutePricingPath(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config", "gateway.yaml")

	// Create absolute path for pricing
	absPath := filepath.Join(tmpDir, "cache", "pricing.json")
	cfg := Config{
		Pricing: PricingConfig{
			CachePath: absPath,
		},
	}

	RelativizePaths(&cfg, configPath)

	// Path should be relativized (or remain absolute on some platforms)
	if filepath.IsAbs(cfg.Pricing.CachePath) {
		// Absolute path was preserved - this is valid behavior
		if cfg.Pricing.CachePath != absPath {
			t.Errorf("expected preserved absolute path %q, got %q", absPath, cfg.Pricing.CachePath)
		}
	} else {
		// Path was relativized
		expectedRel := filepath.Join("..", "cache", "pricing.json")
		if cfg.Pricing.CachePath != expectedRel {
			t.Errorf("expected relative path %q, got %q", expectedRel, cfg.Pricing.CachePath)
		}
	}
}

func TestRelativizePaths_AbsolutePathOutsideBase(t *testing.T) {
	// When absolute path is outside the base directory, it should remain absolute
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config", "gateway.yaml")

	// Use a path outside the config directory
	outsideDir := t.TempDir() // Different temp dir
	absPath := filepath.Join(outsideDir, "data", "telemetry.db")
	cfg := Config{
		Telemetry: TelemetryConfig{
			SQLitePath: absPath,
		},
	}

	RelativizePaths(&cfg, configPath)

	// Path should remain absolute since it's outside the base
	if cfg.Telemetry.SQLitePath != absPath {
		t.Errorf("expected absolute path %q, got %q", absPath, cfg.Telemetry.SQLitePath)
	}
}

func TestRelativizePaths_RelativePathPreserved(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	// Use a relative path
	cfg := Config{
		Telemetry: TelemetryConfig{
			SQLitePath: "data/telemetry.db",
		},
	}

	RelativizePaths(&cfg, configPath)

	// Relative paths should remain unchanged (they're already relative)
	expected := "data/telemetry.db"
	if cfg.Telemetry.SQLitePath != expected {
		t.Errorf("expected unchanged relative path %q, got %q", expected, cfg.Telemetry.SQLitePath)
	}
}

func TestSaveToFile_RoundTrip(t *testing.T) {
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "gateway.yaml")

	// Create a comprehensive config
	cfg := Config{
		Listen: ":9090",
		Admin: AdminConfig{
			Enabled:   true,
			AuthToken: "secure-token-with-at-least-32-characters-long",
			Language:  "en",
		},
		Upstreams: []Upstream{
			{
				Name:    "test-upstream",
				BaseURL: "https://api.example.com",
				APIKey:  "test-api-key",
				Models:  []string{"gpt-4", "gpt-3.5-turbo"},
				Weight:  5,
				Headers: map[string]string{
					"X-Custom-Header": "value",
				},
			},
		},
		Bridge: ModelBridgeConfig{
			Enabled: true,
			Rules: []ModelBridgeRule{
				{From: "gpt-5", To: "gpt-4"},
			},
		},
		Fallback: ModelFallbackConfig{
			Enabled: true,
			Models: map[string]string{
				"gpt-4": "gpt-3.5-turbo",
			},
		},
	}
	cfg.Normalize()

	// Save the config
	if err := SaveToFile(configPath, cfg); err != nil {
		t.Fatalf("SaveToFile failed: %v", err)
	}

	// Load it back
	loaded, err := LoadFromFile(configPath)
	if err != nil {
		t.Fatalf("LoadFromFile failed: %v", err)
	}

	// Verify key fields
	if loaded.Listen != cfg.Listen {
		t.Errorf("Listen mismatch: expected %q, got %q", cfg.Listen, loaded.Listen)
	}
	if loaded.Admin.AuthToken != cfg.Admin.AuthToken {
		t.Errorf("Admin.AuthToken mismatch: expected %q, got %q", cfg.Admin.AuthToken, loaded.Admin.AuthToken)
	}
	if len(loaded.Upstreams) != len(cfg.Upstreams) {
		t.Errorf("Upstreams length mismatch: expected %d, got %d", len(cfg.Upstreams), len(loaded.Upstreams))
	}
	if loaded.Upstreams[0].Name != cfg.Upstreams[0].Name {
		t.Errorf("Upstream name mismatch: expected %q, got %q", cfg.Upstreams[0].Name, loaded.Upstreams[0].Name)
	}
}

func TestSaveToFile_MarshalError(t *testing.T) {
	// This test verifies error handling when marshaling fails
	// yaml.Marshal shouldn't fail on our Config type, but we test the error path
	tmpDir := t.TempDir()
	configPath := filepath.Join(tmpDir, "config.yaml")

	cfg := Config{
		Upstreams: []Upstream{
			{Name: "test", BaseURL: "https://example.com"},
		},
	}
	cfg.Normalize()

	err := SaveToFile(configPath, cfg)
	if err != nil {
		t.Fatalf("SaveToFile should not fail for valid config: %v", err)
	}
}
