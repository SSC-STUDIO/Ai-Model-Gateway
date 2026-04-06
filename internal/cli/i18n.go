package cli

import (
	"embed"
	"encoding/json"
	"fmt"
	"os"
	"strings"
)

//go:embed locales/*.json
var localeFS embed.FS

var (
	currentLang = "zh"
	catalog     map[string]string
)

// SetLanguage sets the current language
func SetLanguage(lang string) {
	currentLang = normalizeLang(lang)
	loadCatalog()
}

// T translates a key with optional arguments
func T(key string, args ...interface{}) string {
	if catalog == nil {
		loadCatalog()
	}
	msg, ok := catalog[key]
	if !ok {
		return key
	}
	if len(args) > 0 {
		return fmt.Sprintf(msg, args...)
	}
	return msg
}

func loadCatalog() {
	catalog = make(map[string]string)
	data, err := localeFS.ReadFile(fmt.Sprintf("locales/%s.json", currentLang))
	if err != nil {
		// Fall back to zh
		data, _ = localeFS.ReadFile("locales/zh.json")
	}

	var nested map[string]map[string]string
	if err := json.Unmarshal(data, &nested); err != nil {
		return
	}

	// Flatten nested structure
	for category, messages := range nested {
		for key, value := range messages {
			catalog[category+"."+key] = value
		}
	}
}

func normalizeLang(lang string) string {
	switch strings.ToLower(lang) {
	case "zh", "zh-cn", "zh-tw":
		return "zh"
	case "en", "en-us", "en-gb":
		return "en"
	case "ja":
		return "ja"
	case "ko":
		return "ko"
	case "es":
		return "es"
	case "fr":
		return "fr"
	case "de":
		return "de"
	default:
		return "zh"
	}
}

// GetLanguageFromEnv gets language from LANG environment variable
func GetLanguageFromEnv() string {
	lang := os.Getenv("LANG")
	if lang == "" {
		return "zh"
	}
	// LANG is usually like en_US.UTF-8
	parts := strings.Split(lang, "_")
	if len(parts) > 0 {
		return strings.ToLower(parts[0])
	}
	return "zh"
}
