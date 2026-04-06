package cli

import (
	"testing"
)

func TestSetLanguage(t *testing.T) {
	SetLanguage("en")
	if currentLang != "en" {
		t.Errorf("expected lang=en, got %s", currentLang)
	}

	SetLanguage("zh")
	if currentLang != "zh" {
		t.Errorf("expected lang=zh, got %s", currentLang)
	}
}

func TestT(t *testing.T) {
	SetLanguage("en")

	// Test existing key
	msg := T("cli.config_valid")
	if msg != "✓ Configuration is valid" {
		t.Errorf("unexpected message: %s", msg)
	}

	// Test missing key (returns key)
	msg = T("nonexistent.key")
	if msg != "nonexistent.key" {
		t.Errorf("expected key for missing, got: %s", msg)
	}
}

func TestT_Formatted(t *testing.T) {
	SetLanguage("en")

	msg := T("cli.gateway_failed", "test error")
	expected := "Gateway failed: test error"
	if msg != expected {
		t.Errorf("expected %q, got %q", expected, msg)
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
		{"unknown", "zh"},
	}

	for _, tt := range tests {
		result := normalizeLang(tt.input)
		if result != tt.expected {
			t.Errorf("normalizeLang(%q) = %q, want %q", tt.input, result, tt.expected)
		}
	}
}

func TestLanguageSwitching(t *testing.T) {
	// Test Chinese
	SetLanguage("zh")
	msg := T("cli.config_valid")
	if msg != "✓ 配置有效" {
		t.Errorf("Chinese translation failed: %s", msg)
	}

	// Test English
	SetLanguage("en")
	msg = T("cli.config_valid")
	if msg != "✓ Configuration is valid" {
		t.Errorf("English translation failed: %s", msg)
	}
}
