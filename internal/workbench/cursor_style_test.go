package workbench

import "testing"

// TestCursorStyleSettingPersists checks that choosing a shape records it and
// saves it for the launches to come.
func TestCursorStyleSettingPersists(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	if state.terminalCursorStyle != cursorBlock {
		t.Fatal("the default shape is a block")
	}
	clickSettingsChoice(t, state, state.settingsCursorStyleLabel(cursorBar))
	if state.terminalCursorStyle != cursorBar {
		t.Fatal("choosing the bar must record it")
	}
	if state.snapshot().TerminalCursor != "bar" {
		t.Fatal("the choice must be saved")
	}
	clickSettingsChoice(t, state, state.settingsCursorStyleLabel(cursorUnderline))
	if state.terminalCursorStyle != cursorUnderline || state.snapshot().TerminalCursor != "underline" {
		t.Fatal("the underline must replace the bar")
	}
}
