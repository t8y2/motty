package i18n

import "strings"

// Locale describes one supported language. Label is its native name, so a
// language can always be found even when the rest of the interface is unfamiliar.
type Locale struct {
	Language Language
	Tag      string
	Label    string
	// Flag names a bundled DBX SVG, or its text marker for Traditional Chinese.
	Flag string
}

var englishLocale = Locale{English, "en", "English", "us"}

// Use DBX's choices, with Simplified Chinese, Traditional Chinese and English
// first; the remaining languages keep DBX's order.
var supportedLocales = [...]Locale{
	{Chinese, "zh-CN", "简体中文", "cn"},
	{TraditionalChinese, "zh-TW", "繁體中文", "繁"},
	englishLocale,
	{Azerbaijani, "az", "Azərbaycan", "az"},
	{Spanish, "es", "Español", "es"},
	{Indonesian, "id", "Bahasa Indonesia", "id"},
	{Italian, "it", "Italiano", "it"},
	{Japanese, "ja", "日本語", "jp"},
	{Korean, "ko", "한국어", "kr"},
	{BrazilianPortuguese, "pt-BR", "Português", "br"},
	{Polish, "pl", "Polski", "pl"},
	{Russian, "ru", "Русский", "ru"},
	{Thai, "th", "ภาษาไทย", "th"},
	{Turkish, "tr", "Türkçe", "tr"},
}

// Supported returns the read-only language choices, in display order.
func Supported() []Locale { return supportedLocales[:] }

// Info returns a language's display metadata; invalid values use English.
func (language Language) Info() Locale {
	for _, locale := range supportedLocales {
		if locale.Language == language {
			return locale
		}
	}
	return englishLocale
}

// Parse accepts a persisted catalog tag without guessing a different locale.
func Parse(tag string) (Language, bool) {
	for _, locale := range supportedLocales {
		if strings.EqualFold(strings.TrimSpace(tag), locale.Tag) {
			return locale.Language, true
		}
	}
	return English, false
}

// Match resolves a system language tag the same way DBX does, including
// traditional Chinese regions and the Brazilian Portuguese catalog for pt.
func Match(tag string) (Language, bool) {
	tag = strings.ToLower(strings.ReplaceAll(strings.TrimSpace(tag), "_", "-"))
	primary, _, _ := strings.Cut(tag, "-")
	if primary == "zh" {
		if strings.Contains(tag, "hant") || strings.HasPrefix(tag, "zh-tw") || strings.HasPrefix(tag, "zh-hk") || strings.HasPrefix(tag, "zh-mo") {
			return TraditionalChinese, true
		}
		return Chinese, true
	}
	if primary == "pt" {
		return BrazilianPortuguese, true
	}
	return Parse(primary)
}
