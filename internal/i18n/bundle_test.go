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

func TestSetLanguage(t *testing.T) {
	b := New("zh")
	if b.lang.String() != "zh" {
		t.Fatalf("initial language = %q, want zh", b.lang.String())
	}

	b.SetLanguage("en")
	if b.lang.String() != "en" {
		t.Errorf("after SetLanguage(en): lang = %q, want en", b.lang.String())
	}

	b.SetLanguage("ja")
	if b.lang.String() != "ja" {
		t.Errorf("after SetLanguage(ja): lang = %q, want ja", b.lang.String())
	}

	// Verify T still works after language change
	b.catalog["test.key"] = "translated"
	if got := b.T("test.key"); got != "translated" {
		t.Errorf("T after SetLanguage = %q, want translated", got)
	}
}

func TestMustLoadCatalog_Success(t *testing.T) {
	langs := []string{"zh", "en", "ja", "ko", "es", "fr", "de"}
	for _, lang := range langs {
		t.Run(lang, func(t *testing.T) {
			catalog := MustLoadCatalog(lang)
			if len(catalog) == 0 {
				t.Errorf("MustLoadCatalog(%q) returned empty catalog", lang)
			}
		})
	}
}

func TestMustLoadCatalog_FallbackLang(t *testing.T) {
	// unknown language normalizes to "zh", so MustLoadCatalog succeeds
	catalog := MustLoadCatalog("xx-unknown")
	if len(catalog) == 0 {
		t.Error("MustLoadCatalog(normalized to zh) returned empty catalog")
	}
}

func TestLoadCatalog_InvalidLang(t *testing.T) {
	// normalizeLang maps everything to one of the known languages,
	// so we cannot trigger a genuine ReadFile error via normal API.
	// But we can verify all supported languages load without error.
	langs := []string{"zh", "zh-CN", "zh-TW", "en", "en-US", "en-GB", "ja", "ko", "es", "fr", "de"}
	for _, lang := range langs {
		t.Run(lang, func(t *testing.T) {
			catalog, err := LoadCatalog(lang)
			if err != nil {
				t.Errorf("LoadCatalog(%q) error: %v", lang, err)
			}
			if len(catalog) == 0 {
				t.Errorf("LoadCatalog(%q) returned empty catalog", lang)
			}
		})
	}
}
