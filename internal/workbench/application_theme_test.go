package workbench

import (
	"math"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestApplicationThemeFollowsPinnedTerminalSource(t *testing.T) {
	var native app.Appearance
	options := freshOptions(t)
	options.SetNativeAppearance = func(look app.Appearance) { native = look }
	state := newTestState(t, options)
	state.setTerminalScheme("dracula")
	state.chooseAppearanceMode(appearancePinnedLight)
	state.setTerminalScheme("github-light")
	state.chooseAppearanceMode(appearancePinnedDark)
	state.chooseAppearanceMode(appearanceFollowTerminal)

	if state.activeScheme() != "dracula" || state.appearance != appearanceDark || native != app.AppearanceDark {
		t.Fatalf("followed scheme %q, appearance %v, native appearance %v", state.activeScheme(), state.appearance, native)
	}
	scheme, _ := state.schemeByID("dracula")
	if rgb8(state.pal.success) != scheme.ansi[2] || contrastRatio(rgb8(state.pal.muted), rgb8(state.pal.surface)) < 4.5 {
		t.Fatalf("linked success color %v and muted text %v", state.pal.success, state.pal.muted)
	}
	state.setTerminalScheme("solarized-light")
	state.save()
	restored := newTestState(t, options)
	if restored.appearanceMode != appearanceFollowTerminal || restored.activeScheme() != "solarized-light" ||
		restored.appearance != appearanceLight || native != app.AppearanceLight {
		t.Fatalf("restored mode %v, scheme %q, appearance %v, native appearance %v",
			restored.appearanceMode, restored.activeScheme(), restored.appearance, native)
	}
	restored.chooseAppearanceMode(appearancePinnedLight)
	if restored.activeScheme() != "github-light" {
		t.Fatalf("independent light scheme was changed to %q", restored.activeScheme())
	}
}

func TestApplicationThemeColorPickerAndPersistence(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsAppearance)
	driver := paneDriver(t, state, gfx.Size{Width: MinimumWindowWidth, Height: 2100})
	for _, mode := range []appearanceMode{appearancePinnedLight, appearanceFollowSystem, appearancePinnedDark, appearanceFollowTerminal, appearanceCustom} {
		label := state.settingsAppearanceLabel(mode)
		hit, ok := settingsChoiceInHits(driver.Snapshot().Hits, label, true)
		if !ok {
			t.Fatalf("appearance option %q is missing", label)
		}
		clickPaintedHit(t, driver, hit.ID)
		if state.appearanceMode != mode {
			t.Fatalf("clicked %q, appearance mode = %v", label, state.appearanceMode)
		}
	}
	id := tree.ID(applicationThemeHits, "shell")
	clickPaintedHit(t, driver, id)
	picker := &state.applicationThemeUI.pickers[0]
	if !picker.Open() {
		t.Fatal("application color picker did not open")
	}
	color := gfx.RGB(59, 100, 128)
	picker.SetDraftValue(components.ColorPickerValue{}.WithColor(color))
	if draft, open := picker.Draft(); !open || rgb8(draft) != rgb8(color) || state.pal.shell == color {
		t.Fatalf("draft %v and current shell %v", draft, state.pal.shell)
	}
	for _, hit := range driver.Snapshot().Hits {
		if hit.Label == state.messages.ColorPickerApply && hit.Role == gfx.RoleButton {
			clickPaintedHit(t, driver, hit.ID)
			break
		}
	}
	if state.pal.shell != color || state.pal.tab == darkPalette.tab {
		t.Fatalf("shell %v and derived tab %v", state.pal.shell, state.pal.tab)
	}
	hover := gfx.RGB(79, 58, 81)
	state.setApplicationColor("tabHover", hover)
	restored := newTestState(t, state.options)
	if restored.appearanceMode != appearanceCustom || restored.pal.shell != color || restored.pal.tabHover != hover {
		t.Fatalf("restored mode %v, shell %v and tab hover %v", restored.appearanceMode, restored.pal.shell, restored.pal.tabHover)
	}
	restored.resetApplicationColors()
	if restored.pal.shell != darkPalette.shell || restored.pal.tabHover != darkPalette.tabHover {
		t.Fatalf("reset shell %v and tab hover %v", restored.pal.shell, restored.pal.tabHover)
	}
}

func TestApplicationGroundKeepsTextOpaque(t *testing.T) {
	options := freshOptions(t)
	options.TerminalTransparency = true
	state := newTestState(t, options)
	state.windowComposited.Store(true)
	state.chooseAppearanceMode(appearanceCustom)
	value := 35
	state.applicationTheme.Opacity = &value
	state.save()
	restored := newTestState(t, options)
	restored.windowComposited.Store(true)
	if restored.applicationGround(restored.pal.workspace).Alpha != .35 || restored.pal.text.Alpha != 1 ||
		restored.terminalGround().Alpha != 1 {
		t.Fatalf("workspace alpha %v, text alpha %v, terminal alpha %v",
			restored.applicationGround(restored.pal.workspace).Alpha, restored.pal.text.Alpha, restored.terminalGround().Alpha)
	}
	var canvas gfx.Canvas
	restored.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	alpha := float32(0)
	for _, rect := range canvas.Rectangles() {
		if pointInRect(600, 2, rect.Bounds) && (!rect.HasClip || pointInRect(600, 2, rect.ClipOf)) {
			alpha = rect.Color.Alpha + alpha*(1-rect.Color.Alpha)
		}
	}
	if math.Abs(float64(alpha)-0.35) > .001 {
		t.Fatalf("title bar alpha = %v", alpha)
	}
}

func TestCustomApplicationThemeDoesNotChangeTerminalTheme(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.setTerminalScheme("dracula")
	state.chooseAppearanceMode(appearanceCustom)
	state.setApplicationColor("workspace", gfx.RGB(240, 240, 247))
	if state.appearance != appearanceLight || state.activeScheme() != "dracula" || !state.termPal.dark {
		t.Fatalf("custom appearance %v, terminal scheme %q, dark %v", state.appearance, state.activeScheme(), state.termPal.dark)
	}
	state.chooseAppearanceMode(appearanceFollowTerminal)
	if state.activeScheme() != "dracula" || state.appearance != appearanceDark {
		t.Fatalf("followed scheme %q and appearance %v", state.activeScheme(), state.appearance)
	}
}
