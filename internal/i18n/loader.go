package i18n

import (
	"embed"
	"encoding/json"
	"fmt"
)

//go:embed locales/*.json
var localeFS embed.FS

// LoadCatalog loads translation catalog for a language
func LoadCatalog(lang string) (map[string]string, error) {
	filename := fmt.Sprintf("locales/%s.json", normalizeLang(lang))
	data, err := localeFS.ReadFile(filename)
	if err != nil {
		return nil, fmt.Errorf("read locale file %s: %w", filename, err)
	}

	var catalog map[string]map[string]string
	if err := json.Unmarshal(data, &catalog); err != nil {
		return nil, fmt.Errorf("unmarshal locale %s: %w", lang, err)
	}

	// Flatten nested structure (e.g., {"errors": {"key": "value"}} -> {"errors.key": "value"})
	result := make(map[string]string)
	flattenMap("", catalog, result)
	return result, nil
}

func flattenMap(prefix string, src map[string]map[string]string, dst map[string]string) {
	for category, messages := range src {
		for key, value := range messages {
			fullKey := category + "." + key
			dst[fullKey] = value
		}
	}
}

// MustLoadCatalog loads catalog or panics on error (use in init)
func MustLoadCatalog(lang string) map[string]string {
	catalog, err := LoadCatalog(lang)
	if err != nil {
		// Fall back to empty catalog
		return make(map[string]string)
	}
	return catalog
}
