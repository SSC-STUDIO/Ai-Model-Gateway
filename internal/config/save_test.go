package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestSaveToFileCreatesParentDirectories(t *testing.T) {
	path := filepath.Join(t.TempDir(), "nested", "config", "gateway.yaml")
	cfg := Config{
		Upstreams: []Upstream{
			{Name: "alpha", BaseURL: "https://alpha.example.com", Weight: 1},
		},
	}
	cfg.Normalize()

	if err := SaveToFile(path, cfg); err != nil {
		t.Fatalf("save config: %v", err)
	}

	if _, err := os.Stat(path); err != nil {
		t.Fatalf("expected config file to exist: %v", err)
	}

	loaded, err := LoadFromFile(path)
	if err != nil {
		t.Fatalf("load config: %v", err)
	}
	if len(loaded.Upstreams) != 1 || loaded.Upstreams[0].Name != "alpha" {
		t.Fatalf("unexpected loaded config: %#v", loaded.Upstreams)
	}
}
