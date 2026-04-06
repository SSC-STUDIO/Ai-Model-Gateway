package i18n

import (
	"fmt"

	"golang.org/x/text/language"
	"golang.org/x/text/message"
)

// Bundle provides message translation for a specific language
type Bundle struct {
	lang    language.Tag
	printer *message.Printer
	catalog map[string]string // fallback for simple key-value
}

// New creates a new Bundle for the given language
func New(lang string) *Bundle {
	tag := language.MustParse(normalizeLang(lang))
	return &Bundle{
		lang:    tag,
		printer: message.NewPrinter(tag),
		catalog: make(map[string]string),
	}
}

// NewWithCatalog creates a Bundle with loaded translations
func NewWithCatalog(lang string, catalog map[string]string) *Bundle {
	b := New(lang)
	b.catalog = catalog
	return b
}

// T translates a message key with optional arguments
func (b *Bundle) T(key string, args ...interface{}) string {
	// First try catalog lookup
	if msg, ok := b.catalog[key]; ok {
		if len(args) > 0 {
			return fmt.Sprintf(msg, args...)
		}
		return msg
	}
	// Fall back to key itself
	return key
}

// SetLanguage changes the bundle language
func (b *Bundle) SetLanguage(lang string) {
	b.lang = language.MustParse(normalizeLang(lang))
	b.printer = message.NewPrinter(b.lang)
}

func normalizeLang(lang string) string {
	switch lang {
	case "zh", "zh-CN", "zh-TW":
		return "zh"
	case "en", "en-US", "en-GB":
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
