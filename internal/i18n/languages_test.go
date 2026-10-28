package i18n

import (
	"reflect"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestSupportedLanguagesMatchDBX(t *testing.T) {
	want := []string{"zh-CN", "zh-TW", "en", "az", "es", "id", "it", "ja", "ko", "pt-BR", "pl", "ru", "th", "tr"}
	wantFlags := []string{"cn", "繁", "us", "az", "es", "id", "it", "jp", "kr", "br", "pl", "ru", "th", "tr"}
	if len(Supported()) != len(want) || int(LanguageCount) != len(want) {
		t.Fatalf("supported catalogs = %d, want %d", len(Supported()), len(want))
	}
	seen := make(map[Language]bool)
	for index, locale := range Supported() {
		if locale.Tag != want[index] || locale.Language.String() != locale.Tag || seen[locale.Language] || locale.Label == "" || locale.Flag != wantFlags[index] {
			t.Fatalf("invalid language choice: %+v", locale)
		}
		seen[locale.Language] = true
		if language, ok := Parse(locale.Tag); !ok || language != locale.Language {
			t.Fatalf("cannot restore %s", locale.Tag)
		}
	}
}

func TestSystemLanguageTagsMatchDBX(t *testing.T) {
	for tag, want := range map[string]Language{
		"en-US": English, "az-Latn-AZ": Azerbaijani, "es-MX": Spanish,
		"id-ID": Indonesian, "it-IT": Italian, "ja-JP": Japanese, "ko-KR": Korean,
		"pt-PT": BrazilianPortuguese, "pt_BR": BrazilianPortuguese,
		"pl-PL": Polish, "ru-RU": Russian, "th-TH": Thai, "tr-TR": Turkish,
		"zh": Chinese, "zh-Hans-CN": Chinese, "zh-TW": TraditionalChinese,
		"zh_HK": TraditionalChinese, "zh-MO": TraditionalChinese, "zh-Hant": TraditionalChinese,
	} {
		if got, ok := Match(tag); !ok || got != want {
			t.Errorf("Match(%q) = %v, %v; want %v", tag, got, ok, want)
		}
	}
	if _, ok := Match("fr-FR"); ok {
		t.Fatal("an unsupported language was guessed")
	}
	if _, ok := Parse("ja-JP"); ok {
		t.Fatal("a persisted catalog tag was guessed")
	}
}

func TestAdditionalCatalogsPreserveEveryFormatArgument(t *testing.T) {
	for _, locale := range Supported() {
		if locale.Language == English || locale.Language == Chinese {
			continue
		}
		checkArguments(t, reflect.ValueOf(For(English)), reflect.ValueOf(For(locale.Language)), locale.Tag)
	}
}

var argumentPattern = regexp.MustCompile(`%(?:\[[0-9]+\])?[-+ #0-9.*]*(?:\[[0-9]+\])?[A-Za-z%]`)
var argumentIndex = regexp.MustCompile(`\[([0-9]+)\]`)

func formatArguments(value string) []string {
	var result []string
	next := 1
	for _, directive := range argumentPattern.FindAllString(value, -1) {
		if directive == "%%" {
			continue
		}
		index := next
		if explicit := argumentIndex.FindStringSubmatch(directive); len(explicit) != 0 {
			index, _ = strconv.Atoi(explicit[1])
		}
		result = append(result, strconv.Itoa(index)+":"+argumentIndex.ReplaceAllString(directive, ""))
		next = index + 1
	}
	sort.Strings(result)
	return result
}

func checkArguments(t *testing.T, source, target reflect.Value, path string) {
	t.Helper()
	for index := 0; index < source.NumField(); index++ {
		field := source.Type().Field(index)
		name := path + "." + field.Name
		en, translated := source.Field(index), target.Field(index)
		if en.Kind() == reflect.Struct {
			checkArguments(t, en, translated, name)
			continue
		}
		if en.Kind() != reflect.String {
			continue
		}
		before := formatArguments(en.String())
		after := formatArguments(translated.String())
		if !reflect.DeepEqual(before, after) {
			t.Errorf("%s: format arguments changed: %v -> %v (%q)", name, before, after, translated.String())
		}
		if strings.Contains(translated.String(), "MT_REVIEW_REQUIRED") || strings.Contains(translated.String(), "ZXPH") || strings.Contains(translated.String(), "ZXQ000") {
			t.Errorf("%s: incomplete translation", name)
		}
	}
}
