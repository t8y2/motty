package workbench

import (
	"testing"

	"motty/internal/term"
)

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

// TestTheCursorProgramSwitchKeepsTheUserShape covers the "Always use my style"
// switch, which is on by default: a session keeps the user's bar even when the
// program asks for a block, which is what PowerShell does through ConPTY.
func TestTheCursorProgramSwitchKeepsTheUserShape(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitSettings)
	state.selectSettingsCategory(settingsTerminal)
	clickSettingsChoice(t, state, state.settingsCursorStyleLabel(cursorBar))
	driver := labelDriver(t, state)
	driver.Resize(1100, 1400)

	switchAt := func() (x, y float32, ok bool) {
		for _, hit := range driver.Snapshot().Hits {
			if hit.ID == hitSettingsCursorProgram && !hit.IsLabel {
				return hit.Bounds.X + hit.Bounds.Width/2, hit.Bounds.Y + hit.Bounds.Height/2, true
			}
		}
		return 0, 0, false
	}
	if state.terminalCursorProgram || state.snapshot().TerminalCursorProgram {
		t.Fatal("a fresh state must keep the user's shape, not hand it to programs")
	}
	// With the switch on by default the block PowerShell asks for through
	// ConPTY does not come back, without the reader touching anything.
	screenshot := &terminal{screen: term.New(20, 3, 10)}
	state.sessions = []Session{{ID: 1, term: screenshot}}
	state.applyTerminalCursorStyle()
	screen := screenshot.screen
	screen.Write([]byte("\x1b[1 q"))
	if got := screen.Cursor().Style; got != term.CursorBar {
		t.Fatalf("the session took the program's block: cursor = %v, want the user's bar", got)
	}

	x, y, ok := switchAt()
	if !ok {
		t.Fatal("the terminal page does not offer the program-shape switch")
	}
	if err := driver.Click(x, y); err != nil {
		t.Fatal(err)
	}
	if !state.terminalCursorProgram || !state.snapshot().TerminalCursorProgram {
		t.Fatal("handing the shape to programs must be saved")
	}
	// Now a program may set the shape again.
	second := &terminal{screen: term.New(20, 3, 10)}
	state.sessions = []Session{{ID: 2, term: second}}
	state.applyTerminalCursorStyle()
	second.screen.Write([]byte("\x1b[1 q"))
	if got := second.screen.Cursor().Style; got != term.CursorBlock {
		t.Fatalf("cursor = %v, want the program's block once the switch is off", got)
	}
}
