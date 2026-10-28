package i18n

import (
	"reflect"
	"regexp"
	"strings"
	"testing"
	"unicode"
)

// A language that forgot a string would paint blank labels. Reflection is fine
// here: it runs in tests, never on a paint path.
func TestEveryLanguageFillsEveryMessage(t *testing.T) {
	for _, locale := range Supported() {
		language := locale.Language
		checkFilled(t, reflect.ValueOf(For(language)), language.String())
	}
}

// checkFilled walks the nested catalogs: the fields of Messages hold other
// structs, so a shallow check would miss a missing tool or panel string.
func checkFilled(t *testing.T, value reflect.Value, path string) {
	t.Helper()
	switch value.Kind() {
	case reflect.String:
		if strings.TrimSpace(value.String()) == "" {
			t.Errorf("%s is empty", path)
		}
	case reflect.Struct:
		for index := 0; index < value.NumField(); index++ {
			field := value.Type().Field(index)
			if field.PkgPath != "" {
				continue
			}
			checkFilled(t, value.Field(index), path+"."+field.Name)
		}
	}
}

// The Chinese catalog must not silently fall back to English text for the
// strings a user reads constantly, and format strings must keep their verbs.
func TestChineseCatalogIsTranslated(t *testing.T) {
	compareCatalogs(t, reflect.ValueOf(For(English)), reflect.ValueOf(For(Chinese)), "")
}

func compareCatalogs(t *testing.T, english, chinese reflect.Value, prefix string) {
	t.Helper()
	for index := 0; index < english.NumField(); index++ {
		field := english.Type().Field(index)
		if field.PkgPath != "" {
			continue
		}
		name := prefix + field.Name
		en, zh := english.Field(index), chinese.Field(index)
		if en.Kind() == reflect.Struct {
			compareCatalogs(t, en, zh, name+".")
			continue
		}
		if en.Kind() != reflect.String {
			continue
		}
		if strings.Count(en.String(), "%") != strings.Count(zh.String(), "%") {
			t.Errorf("%s: verbs differ between %q and %q", name, en, zh)
		}
		if strings.HasSuffix(field.Name, "Fmt") && hasWords(en.String()) && !hasHan(zh.String()) {
			t.Errorf("%s: %q has no Chinese text", name, zh)
		}
	}
}

// formatVerb matches a printf directive, so a purely numeric format like
// "%d × %d" is not mistaken for untranslated words.
var formatVerb = regexp.MustCompile(`%[-+ #0-9.*]*[A-Za-z]`)

func hasWords(text string) bool {
	for _, r := range formatVerb.ReplaceAllString(text, "") {
		if unicode.IsLetter(r) {
			return true
		}
	}
	return false
}

func hasHan(value string) bool {
	for _, r := range value {
		if unicode.Is(unicode.Han, r) {
			return true
		}
	}
	return false
}
