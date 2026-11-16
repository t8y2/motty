package i18n

import (
	"reflect"
	"strings"
	"testing"
)

// The assistant's texts sit in a nested struct that the catalog-wide test does
// not look into.
func TestAISettingsCatalogIsCompleteInBothLanguages(t *testing.T) {
	english := reflect.ValueOf(For(English).AISettings)
	chinese := reflect.ValueOf(For(Chinese).AISettings)
	for index := 0; index < english.NumField(); index++ {
		name := english.Type().Field(index).Name
		en, zh := english.Field(index).String(), chinese.Field(index).String()
		if strings.TrimSpace(en) == "" || strings.TrimSpace(zh) == "" {
			t.Errorf("%s is empty: %q / %q", name, en, zh)
			continue
		}
		if strings.Count(en, "%") != strings.Count(zh, "%") {
			t.Errorf("%s: format verbs differ between %q and %q", name, en, zh)
		}
		// Product and protocol names stay as the vendors write them.
		keepsName := strings.HasPrefix(name, "FormBaseURL") || name == "ProviderKindClaude" || name == "FormFormatChat" || name == "FormFormatResponses"
		if !hasHan(zh) && !keepsName {
			t.Errorf("%s: %q has no Chinese text", name, zh)
		}
	}
}
