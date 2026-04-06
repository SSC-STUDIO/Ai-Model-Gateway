package i18n

import (
	"testing"
)

func TestNew(t *testing.T) {
	b := New("zh")
	if b == nil {
		t.Fatal("expected non-nil Bundle")
	}
}

func TestBundle_T(t *testing.T) {
	catalog := map[string]string{
		"errors.test":      "test message",
		"errors.formatted": "hello %s",
	}
	b := NewWithCatalog("en", catalog)

	tests := []struct {
		key      string
		args     []interface{}
		expected string
	}{
		{"errors.test", nil, "test message"},
		{"errors.formatted", []interface{}{"world"}, "hello world"},
		{"errors.missing", nil, "errors.missing"}, // returns key if not found
	}

	for _, tt := range tests {
		t.Run(tt.key, func(t *testing.T) {
			result := b.T(tt.key, tt.args...)
			if result != tt.expected {
				t.Errorf("T(%q) = %q, want %q", tt.key, result, tt.expected)
			}
		})
	}
}

func TestNormalizeLang(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"zh", "zh"},
		{"zh-CN", "zh"},
		{"en", "en"},
		{"en-US", "en"},
		{"ja", "ja"},
		{"ko", "ko"},
		{"es", "es"},
		{"fr", "fr"},
		{"de", "de"},
		{"unknown", "zh"}, // default
	}

	for _, tt := range tests {
		t.Run(tt.input, func(t *testing.T) {
			result := normalizeLang(tt.input)
			if result != tt.expected {
				t.Errorf("normalizeLang(%q) = %q, want %q", tt.input, result, tt.expected)
			}
		})
	}
}

func TestLoadCatalog(t *testing.T) {
	// This test requires the locale files to exist
	catalog, err := LoadCatalog("en")
	if err != nil {
		t.Fatalf("LoadCatalog failed: %v", err)
	}

	// Check some expected keys
	expectedKeys := []string{
		"errors.no_provider",
		"errors.unauthorized",
		"errors.invalid_token",
	}

	for _, key := range expectedKeys {
		if _, ok := catalog[key]; !ok {
			t.Errorf("expected key %q not found in catalog", key)
		}
	}
}

func TestBundle_T_Chinese(t *testing.T) {
	catalog := map[string]string{
		"errors.no_provider": "没有可用的模型提供商",
	}
	b := NewWithCatalog("zh", catalog)

	result := b.T("errors.no_provider")
	if result != "没有可用的模型提供商" {
		t.Errorf("Chinese translation failed: got %q", result)
	}
}
