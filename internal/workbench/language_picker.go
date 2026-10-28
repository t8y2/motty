package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/workbench/flags"
)

var (
	hitSettingsLanguageSelect  = tree.ID(settingsHits, "language-select")
	hitSettingsLanguageDismiss = tree.ID(settingsHits, "language-dismiss")
	hitSettingsLanguageItems   = tree.Sub(settingsHits, tree.Name("language-items"))
)

type languagePickerUI struct {
	open, reveal bool
	bounds       ui.Rect
}

func languageOptionID(language i18n.Language) ui.ID {
	switch language {
	case i18n.English:
		return hitSettingsLanguageEnglish
	case i18n.Chinese:
		return hitSettingsLanguageChinese
	default:
		return hitSettingsLanguageItems.At(int(language))
	}
}

var languageFlagImages = map[string]func() ui.Bitmap{
	"us": embeddedBitmap(flags.Us, 1<<61|100),
	"az": embeddedBitmap(flags.Az, 1<<61|101),
	"es": embeddedBitmap(flags.Es, 1<<61|102),
	"id": embeddedBitmap(flags.Id, 1<<61|103),
	"it": embeddedBitmap(flags.It, 1<<61|104),
	"jp": embeddedBitmap(flags.Jp, 1<<61|105),
	"kr": embeddedBitmap(flags.Kr, 1<<61|106),
	"br": embeddedBitmap(flags.Br, 1<<61|107),
	"pl": embeddedBitmap(flags.Pl, 1<<61|108),
	"ru": embeddedBitmap(flags.Ru, 1<<61|109),
	"th": embeddedBitmap(flags.Th, 1<<61|110),
	"tr": embeddedBitmap(flags.Tr, 1<<61|111),
	"cn": embeddedBitmap(flags.Cn, 1<<61|112),
}

func languageFlag(language i18n.Language) ui.Bitmap {
	if load := languageFlagImages[language.Info().Flag]; load != nil {
		return load()
	}
	return ui.Bitmap{}
}

func languageIconText(language i18n.Language) string {
	if language == i18n.TraditionalChinese {
		return language.Info().Flag
	}
	return ""
}

func (state *State) clickLanguagePicker(id ui.ID) bool {
	picker := &state.languagePicker
	switch id {
	case hitSettingsLanguageSelect:
		picker.open = !picker.open
		picker.reveal = picker.open
		return true
	case hitSettingsLanguageDismiss:
		picker.open = false
		return true
	}
	for _, locale := range i18n.Supported() {
		if id == languageOptionID(locale.Language) {
			picker.open = false
			if state.chooseLanguage(locale.Language) {
				state.save()
			}
			return true
		}
	}
	return false
}

func (state *State) languagePickerKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Target != hitSettingsLanguageSelect || event.Release || state.languagePicker.open {
		return ui.EventResult{}, false
	}
	if event.Key != ui.KeyDown && event.Key != ui.KeyUp {
		return ui.EventResult{}, false
	}
	state.languagePicker.open, state.languagePicker.reveal = true, true
	return ui.EventResult{Handled: true, Repaint: true, Focus: languageOptionID(state.language), FocusVisible: true}, true
}

func (state *State) paintLanguagePicker(canvas *ui.Canvas) {
	picker := &state.languagePicker
	bounds := picker.bounds
	if bounds.Width <= 0 || bounds.Y < windowBarHeight || bounds.Bottom() > state.viewport.Height {
		picker.open = false
		return
	}
	locales := i18n.Supported()
	below := max(state.viewport.Height-8-bounds.Bottom()-4, 0)
	above := max(bounds.Y-windowBarHeight-8, 0)
	height := min(float32(8+len(locales)*32), max(above, below))
	content := ui.Rect{X: bounds.X, Y: bounds.Bottom() + 4, Width: bounds.Width, Height: height}
	if below < height {
		content.Y = bounds.Y - height - 4
	}
	scroll := state.selectScroll(picker.open, hitSettingsLanguageDismiss)
	if picker.open && picker.reveal {
		for index, locale := range locales {
			if locale.Language == state.language {
				scroll.Offset = max(0, min(float32(index*32), float32(len(locales)*32)+8-height))
				break
			}
		}
		canvas.SetInitialFocus(languageOptionID(state.language))
		picker.reveal = false
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitSettingsLanguageSelect, Label: state.messages.SettingsLanguage,
		Value: state.language.Info().Label, IconImage: languageFlag(state.language), IconText: languageIconText(state.language),
		Open: picker.open, Content: content, Layer: popupLayer,
		Scope: hitSettingsLanguageDismiss, DismissID: hitSettingsLanguageDismiss,
		Viewport: state.windowRect(), Scroll: scroll,
	})
	defer control.End()
	if picker.open {
		state.menuPlaced(control.ContentBounds())
		for _, locale := range locales {
			control.AddItem(components.SelectItem{ID: languageOptionID(locale.Language), Label: locale.Label, Value: locale.Tag, Selected: locale.Language == state.language, IconImage: languageFlag(locale.Language), IconText: languageIconText(locale.Language)})
		}
	}
}
