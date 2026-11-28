package workbench

import (
	"github.com/vivaui/viva/gfx"
	"testing"
)

func TestCustomThemeRefreshesExistingTerminalAndOtherWindow(t *testing.T) {
	pair := newWindowsOnOneStore(t)
	liveTab(t, pair.second, pair.secondDialer, 0)
	session := pair.second.sessions[0].ID
	pair.first.setTerminalScheme("dracula")
	pair.first.openThemeEditor(true, false)
	pair.first.themeEditor.name.SetText("Shared theme")
	pair.first.themeEditor.colors[0] = gfx.RGB(30, 40, 60)
	pair.first.clickThemeEditor(hitThemeApply)
	id := pair.first.activeScheme()
	if pair.second.activeScheme() != id || pair.second.termPal.terminal != gfx.RGB(30, 40, 60) {
		t.Fatalf("second window did not receive theme: %s", pair.second.activeScheme())
	}
	pair.first.openThemeEditor(false, false)
	pair.first.themeEditor.colors[1] = gfx.RGB(220, 230, 240)
	pair.first.clickThemeEditor(hitThemeSave)
	if pair.second.termPal.termFG != gfx.RGB(220, 230, 240) || pair.second.sessions[0].ID != session {
		t.Fatal("editing did not refresh existing terminal in second window")
	}
}

func TestCustomThemeFollowKeepsSourceWhenBrightnessChanges(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.setTerminalScheme("dracula")
	state.openThemeEditor(true, false)
	state.clickThemeEditor(hitThemeApply)
	id := state.activeScheme()
	state.chooseAppearanceMode(appearanceFollowTerminal)
	state.openThemeEditor(false, false)
	state.themeEditor.colors[0] = gfx.RGB(240, 245, 250)
	state.themeEditor.colors[1] = gfx.RGB(25, 30, 40)
	state.clickThemeEditor(hitThemeSave)
	if state.activeScheme() != id || state.appearance != appearanceLight || state.pal.workspace != gfx.RGB(240, 245, 250) {
		t.Fatal("follow lost its source after changing brightness")
	}
	again := newTestState(t, state.options)
	if again.activeScheme() != id || again.appearance != appearanceLight {
		t.Fatal("follow source was not restored")
	}
}
