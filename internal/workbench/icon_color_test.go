package workbench

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/i18n"
)

func TestIconColorsPreserveArtworkAndCacheEachChoice(t *testing.T) {
	original := bytes.Clone(AppIcon().RGBA)
	ids := map[uint64]bool{}
	for color := iconMint; color < iconColorCount; color++ {
		art := color.artwork()
		preview := color.previewBitmap()
		if art.window.Size != AppIcon().Size || len(art.window.RGBA) != len(original) {
			t.Fatalf("%s changed the icon dimensions", color.saved())
		}
		for offset := 0; offset < len(original); offset += 4 {
			if art.window.RGBA[offset+3] != original[offset+3] {
				t.Fatalf("%s changed the icon's alpha", color.saved())
			}
		}
		if color != iconMint && bytes.Equal(art.window.RGBA, original) {
			t.Fatalf("%s did not recolor the mark", color.saved())
		}
		if preview.Width == 0 || len(preview.Pix) == 0 {
			t.Fatalf("%s has no effect preview", color.saved())
		}
		if color != iconMint && bytes.Equal(preview.Pix, iconMint.previewBitmap().Pix) {
			t.Fatalf("%s still previews the default color", color.saved())
		}
		if !bytes.Equal(art.window.RGBA[:4], original[:4]) {
			t.Fatalf("%s recolored the neutral background", color.saved())
		}
		for _, id := range []uint64{art.dark.ID, art.light.ID, preview.ID} {
			if ids[id] {
				t.Fatalf("%s reused a bitmap cache identity", color.saved())
			}
			ids[id] = true
		}
		if &color.artwork().window.RGBA[0] != &art.window.RGBA[0] {
			t.Fatalf("%s rebuilt the artwork", color.saved())
		}
	}
	if !bytes.Equal(AppIcon().RGBA, original) {
		t.Fatal("recoloring modified the original artwork")
	}
	for _, name := range []string{"", "unknown", "BLUE"} {
		if iconColorFromString(name) != iconMint {
			t.Fatalf("%q did not fall back to the original icon", name)
		}
	}
}

func TestChinaRedReplacesSkyAndKeepsPixelFormats(t *testing.T) {
	if iconColorFromString("red") != iconRed || iconColorFromString("sky") != iconRed || iconRed.saved() != "red" {
		t.Fatal("China red was not selected or migrated from a test build's sky blue")
	}
	rgba := []byte{98, 206, 136, 255, 80, 80, 80, 128}
	bgra := []byte{136, 206, 98, 255, 80, 80, 80, 128}
	iconRed.recolor(rgba, false)
	iconRed.recolor(bgra, true)
	if !bytes.Equal(rgba, []byte{206, 40, 17, 255, 80, 80, 80, 128}) ||
		!bytes.Equal(bgra, []byte{17, 40, 206, 255, 80, 80, 80, 128}) {
		t.Fatalf("China red changed neutral pixels, alpha or pixel ordering: RGBA=%v BGRA=%v", rgba, bgra)
	}
}

func TestLogoColorSettingsUpdateNativeIconsAndSurviveRestart(t *testing.T) {
	options := freshOptions(t)
	options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	var native []app.WindowIcon
	options.SetAppIcon = func(icon app.WindowIcon) { native = append(native, icon) }
	first := newTestState(t, options)
	first.Click(hitSettings)
	for color := iconBlue; color < iconColorCount; color++ {
		first.Click(hitSettingsIconColorChoose)
		clickSettingsChoice(t, first, first.iconColorLabel(color))
		if first.iconColorChoice != color || len(native) != int(color)-1 {
			t.Fatal("a preview was not selected or applied before confirmation")
		}
		if !settingsChoiceHit(t, first, first.iconColorLabel(color)).Checked {
			t.Fatalf("%s has no selection ring", color.saved())
		}
		first.Click(hitIconColorConfirm)
		if first.iconColor != color || len(native) != int(color) {
			t.Fatalf("%s was not applied once: choice=%v updates=%d", color.saved(), first.iconColor, len(native))
		}
		if !bytes.Equal(native[len(native)-1].RGBA, first.WindowIcon().RGBA) {
			t.Fatal("the native window did not receive the selected artwork")
		}
		if saved, _, err := loadState(options.StatePath); err != nil || saved.IconColor != color.saved() {
			t.Fatalf("%s was not saved: color=%q error=%v", color.saved(), saved.IconColor, err)
		}
	}
	before := len(native)
	first.Click(hitSettingsIconColorChoose)
	clickSettingsChoice(t, first, first.iconColorLabel(iconGraphite))
	first.Click(hitIconColorConfirm)
	if len(native) != before {
		t.Fatal("choosing the current color replaced native icons again")
	}
	second := newTestState(t, options)
	if second.iconColor != iconGraphite || len(native) != before+1 {
		t.Fatal("the saved color was not restored to native icons")
	}
}

func TestLogoColorPickerCancellationPreservesTheAppliedChoice(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitSettings)
	state.setIconColor(iconViolet)
	state.save()
	for _, dismiss := range []gfx.ID{hitIconColorCancel, hitIconColorClose, hitIconColorDismiss} {
		state.Click(hitSettingsIconColorChoose)
		if state.iconColorChoice != iconViolet {
			t.Fatal("the picker did not start from the applied choice")
		}
		clickSettingsChoice(t, state, state.iconColorLabel(iconOrange))
		state.Click(dismiss)
		if state.modal != modalNone || state.iconColor != iconViolet {
			t.Fatal("dismissing the picker applied an unconfirmed choice")
		}
	}
}

func TestLogoColorChangesReachOtherWindows(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	firstUpdates, secondUpdates := 0, 0
	pair.first.options.SetAppIcon = func(app.WindowIcon) { firstUpdates++ }
	pair.second.options.SetAppIcon = func(app.WindowIcon) { secondUpdates++ }
	pair.first.setIconColor(iconViolet)
	pair.first.save()
	if pair.second.iconColor != iconViolet || firstUpdates != 1 || secondUpdates != 1 {
		t.Fatalf("color=%v native updates=%d/%d", pair.second.iconColor, firstUpdates, secondUpdates)
	}
	// An unrelated save must keep both windows' current choice.
	pair.second.setNotifications(false)
	pair.second.save()
	if pair.first.iconColor != iconViolet || firstUpdates != 1 || secondUpdates != 1 {
		t.Fatal("an unrelated setting reset or redundantly reapplied icons")
	}
}

func TestLogoColorsAreSearchableInEveryLanguage(t *testing.T) {
	for _, language := range []i18n.Language{i18n.English, i18n.Chinese, i18n.TraditionalChinese} {
		state := newTestState(t, freshOptions(t))
		state.setLanguage(language)
		searchSettings(t, state, "logo")
		found := false
		for _, entry := range state.settings.results[:state.settings.count] {
			found = found || entry.label == state.messages.SettingsIconColor
		}
		if !found {
			t.Fatalf("%s cannot find Logo color", language)
		}
	}
}
