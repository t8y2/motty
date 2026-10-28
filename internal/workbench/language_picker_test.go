package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestEveryLanguageChoiceSurvivesRestart(t *testing.T) {
	for _, locale := range i18n.Supported() {
		t.Run(locale.Tag, func(t *testing.T) {
			options := withSystemLanguages(freshOptions(t), "en-US")
			state := newTestState(t, options)
			options.StatePath = state.options.StatePath
			state.Click(hitSettings)
			state.Click(languageOptionID(locale.Language))
			if state.language != locale.Language || !state.languageChosen {
				t.Fatal("the chosen language was not applied")
			}
			saved, _, err := loadState(options.StatePath)
			if err != nil || saved.Language != locale.Tag {
				t.Fatalf("saved language=%q, err=%v", saved.Language, err)
			}
			state.Close()
			restored := newTestState(t, withSystemLanguages(options, "ja-JP"))
			if restored.language != locale.Language || !restored.languageChosen {
				t.Fatal("the system overrode an explicit language choice")
			}
		})
	}
}

func TestLanguageSelectSupportsNativeNameTypeahead(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsGeneral)
	for _, text := range []string{"Bahasa", "日本", "한국", "Türk", "繁體"} {
		state.selectTypeahead.owner = ui.ID{}
		for _, character := range text {
			result, ok := state.selectTypeaheadKey(ui.KeyEvent{Target: hitSettingsLanguageSelect, Text: string(character)})
			if !ok || !result.Handled {
				t.Fatalf("native name %q did not select its language", text)
			}
		}
		if !strings.HasPrefix(state.language.Info().Label, text) {
			t.Fatalf("native name %q selected %s", text, state.language)
		}
	}
}

func TestTraditionalChineseUsesDBXTextMarker(t *testing.T) {
	icon := languageFlag(i18n.TraditionalChinese)
	if len(icon.Pix) != 0 || languageIconText(i18n.TraditionalChinese) != "繁" {
		t.Fatal("Traditional Chinese must use DBX's text marker")
	}
}
