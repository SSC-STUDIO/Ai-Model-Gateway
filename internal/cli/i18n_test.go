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

func TestNormalizeLang_AllCases(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"zh", "zh"},
		{"zh-CN", "zh"},
		{"zh-TW", "zh"},
		{"ZH", "zh"},
		{"en", "en"},
		{"en-US", "en"},
		{"en-GB", "en"},
		{"EN", "en"},
		{"ja", "ja"},
		{"JA", "ja"},
		{"ko", "ko"},
		{"KO", "ko"},
		{"es", "es"},
		{"ES", "es"},
		{"fr", "fr"},
		{"FR", "fr"},
		{"de", "de"},
		{"DE", "de"},
		{"unknown", "zh"},
		{"", "zh"},
		{"pt", "zh"},
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

func TestGetLanguageFromEnv(t *testing.T) {
	// Test with LANG set
	t.Setenv("LANG", "en_US.UTF-8")
	lang := GetLanguageFromEnv()
	if lang != "en" {
		t.Errorf("expected 'en' for LANG=en_US.UTF-8, got %q", lang)
	}
}

func TestGetLanguageFromEnv_Empty(t *testing.T) {
	// Test with empty LANG
	t.Setenv("LANG", "")
	lang := GetLanguageFromEnv()
	if lang != "zh" {
		t.Errorf("expected 'zh' for empty LANG, got %q", lang)
	}
}

func TestGetLanguageFromEnv_NoUnderscore(t *testing.T) {
	// Test with LANG without underscore
	t.Setenv("LANG", "en")
	lang := GetLanguageFromEnv()
	if lang != "en" {
		t.Errorf("expected 'en' for LANG=en, got %q", lang)
	}
}
