package workbench

import (
	"testing"

	"motty/internal/i18n"
)

func withSystemLanguages(options Options, languages ...string) Options {
	options.PreferredLanguages = func() []string { return languages }
	return options
}

func TestFirstRunFollowsTheSystemLanguage(t *testing.T) {
	for _, test := range []struct {
		name      string
		languages []string
		want      i18n.Language
	}{
		{"simplified Chinese", []string{"zh-Hans-CN", "en-US"}, i18n.Chinese},
		{"traditional Chinese", []string{"zh-TW"}, i18n.TraditionalChinese},
		{"traditional Chinese script", []string{"zh-Hant-HK"}, i18n.TraditionalChinese},
		{"Japanese", []string{"ja-JP"}, i18n.Japanese},
		{"Korean", []string{"ko-KR"}, i18n.Korean},
		{"Portuguese", []string{"pt-PT"}, i18n.BrazilianPortuguese},
		{"Thai", []string{"th-TH"}, i18n.Thai},
		{"English first", []string{"en-GB", "zh-CN"}, i18n.English},
		{"the first language with a catalog wins", []string{"fr-FR", "zh-CN"}, i18n.Chinese},
		{"none with a catalog", []string{"fr-FR", "de-DE"}, i18n.English},
		{"nothing reported", nil, i18n.English},
	} {
		state := newTestState(t, withSystemLanguages(freshOptions(t), test.languages...))
		if state.language != test.want || state.languageChosen {
			t.Errorf("%s: language %v chosen %v, want %v unchosen", test.name, state.language, state.languageChosen, test.want)
		}
		if state.messages.NavHosts != i18n.For(test.want).NavHosts {
			t.Errorf("%s: the catalog does not follow the language", test.name)
		}
	}
}

func TestAnUnchosenLanguageIsNotWrittenSoTheSystemKeepsDeciding(t *testing.T) {
	options := withSystemLanguages(freshOptions(t), "zh-CN")
	first := newTestState(t, options)
	first.save()
	if saved, _, _ := loadState(options.StatePath); saved.Language != "" {
		t.Fatalf("an unchosen language was saved as %q", saved.Language)
	}
	// The system language changes between runs: the application follows.
	second := newTestState(t, withSystemLanguages(options, "en-US"))
	if second.language != i18n.English {
		t.Fatalf("language = %v, want English after the system changed", second.language)
	}
}

func TestAChosenLanguageStaysWhateverTheSystemSays(t *testing.T) {
	options := withSystemLanguages(freshOptions(t), "zh-CN")
	first := newTestState(t, options)
	first.Click(hitSettings)
	first.Click(hitSettingsLanguageEnglish) // English on a Chinese system
	if first.language != i18n.English || !first.languageChosen {
		t.Fatalf("language %v chosen %v", first.language, first.languageChosen)
	}
	second := newTestState(t, options)
	if second.language != i18n.English || !second.languageChosen {
		t.Fatalf("after a restart: language %v chosen %v", second.language, second.languageChosen)
	}
}

func TestChoosingTheLanguageAlreadyShowingStillCountsAsAChoice(t *testing.T) {
	options := withSystemLanguages(freshOptions(t), "zh-CN")
	first := newTestState(t, options)
	first.Click(hitSettings)
	first.Click(hitSettingsLanguageChinese) // already Chinese, and now deliberately
	if !first.languageChosen {
		t.Fatal("the choice was not recorded")
	}
	second := newTestState(t, withSystemLanguages(options, "en-US"))
	if second.language != i18n.Chinese {
		t.Fatalf("a deliberate Chinese was overridden by the system: %v", second.language)
	}
}

// useChinese switches the catalog the way a user does: the window bar no longer
// has a language button, so Settings is where the choice is made. The page the
// caller was on is put back so the test carries on where it was.
func useChinese(t testing.TB, state *State) {
	t.Helper()
	section := state.activeSection
	state.Click(hitSettings)
	state.Click(hitSettingsLanguageChinese)
	state.activeSection = section
}
