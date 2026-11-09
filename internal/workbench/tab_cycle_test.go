package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

// tabIndex is where the active tab stands on the tab bar.
func tabIndex(state *State) int {
	active := state.deck.ActiveWorkspace()
	for index, workspace := range state.deck.Workspaces(nil) {
		if workspace.ID == active {
			return index
		}
	}
	return -1
}

func TestControlTabWalksTheTabsAndWrapsAround(t *testing.T) {
	state, _ := threeTabs(t)
	next := gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl}
	previous := gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl | gfx.ModifierShift}

	for _, want := range []int{0, 1, 2} { // from the last tab around to the first
		if result := state.Key(next); !result.Handled {
			t.Fatalf("Control+Tab was not handled: %+v", result)
		}
		if got := tabIndex(state); got != want {
			t.Fatalf("after Control+Tab tab = %d, want %d", got, want)
		}
	}
	for _, want := range []int{1, 0, 2} {
		state.Key(previous)
		if got := tabIndex(state); got != want {
			t.Fatalf("after Control+Shift+Tab tab = %d, want %d", got, want)
		}
	}
	if state.activeSessionID != state.sessions[2].ID {
		t.Fatalf("the keyboard stayed on session %d after moving to the third tab", state.activeSessionID)
	}
}

func TestControlTabLeavesTheHostListAndRepeatsWhenHeld(t *testing.T) {
	state, _ := threeTabs(t)
	state.Click(hitHosts)
	if state.activeSection != sectionHosts {
		t.Fatal("the test needs the host list in front")
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl})
	if state.activeSection != sectionTerminal || tabIndex(state) != 0 {
		t.Fatalf("section=%d tab=%d, want the terminal on the first tab", state.activeSection, tabIndex(state))
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl, Repeat: true})
	if tabIndex(state) != 1 {
		t.Fatalf("a held Control+Tab stopped at tab %d", tabIndex(state))
	}
	// The release of a press the terminal never saw is not for the terminal.
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl, Release: true}); !result.Handled {
		t.Fatal("the release of Control+Tab reached the terminal")
	}
	if tabIndex(state) != 1 {
		t.Fatal("a release moved the tab")
	}
}

func TestControlTabDoesNothingBehindADialogOrWithOneTab(t *testing.T) {
	state, _ := threeTabs(t)
	state.modal = modalNewHost
	before := tabIndex(state)
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyTab, Modifiers: gfx.ModifierControl}); result.Handled && tabIndex(state) != before {
		t.Fatal("Control+Tab switched tabs behind a dialog")
	}
	if tabIndex(state) != before {
		t.Fatal("Control+Tab switched tabs behind a dialog")
	}
	state.modal = modalNone

	single, _, _, _ := liveSession(t)
	if single.cycleWorkspace(1) {
		t.Fatal("a lone tab has nowhere to go")
	}
}

func TestTabSwitchShortcutsCanBeRebound(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	want := map[shortcutAction]keymap.Chord{
		shortcutNextTab:     {Modifiers: gfx.ModifierControl, Key: gfx.KeyTab},
		shortcutPreviousTab: {Modifiers: gfx.ModifierControl | gfx.ModifierShift, Key: gfx.KeyTab},
	}
	for action, chord := range want {
		if state.shortcuts.chords.Chord(action) != chord {
			t.Fatalf("%v default = %+v, want %+v", action, state.shortcuts.chords.Chord(action), chord)
		}
	}

	state.openShortcutCapture(shortcutNextTab)
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyPageDown, Modifiers: gfx.ModifierControl})
	custom := keymap.Chord{Modifiers: gfx.ModifierControl, Key: gfx.KeyPageDown}
	if !result.Handled || state.modal != modalNone || state.shortcuts.chords.Chord(shortcutNextTab) != custom {
		t.Fatalf("capture result=%+v modal=%d chord=%+v", result, state.modal, state.shortcuts.chords.Chord(shortcutNextTab))
	}
	if label := state.shortcuts.labels[shortcutNextTab]; label != "Ctrl+PageDown" {
		t.Fatalf("label = %q", label)
	}

	// A chord another action holds is refused, named key or not.
	state.openShortcutCapture(shortcutPreviousTab)
	state.Key(gfx.KeyEvent{Key: gfx.KeyPageDown, Modifiers: gfx.ModifierControl})
	if state.modal != modalShortcutCapture || state.shortcutError == "" {
		t.Fatalf("a taken chord was accepted: modal=%d error=%q", state.modal, state.shortcutError)
	}
	state.clickShortcutCapture(hitShortcutCaptureCancel)

	state.save()
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutNextTab) != custom || restored.shortcuts.chords.Chord(shortcutPreviousTab) != want[shortcutPreviousTab] {
		t.Fatalf("restored next=%+v previous=%+v", restored.shortcuts.chords.Chord(shortcutNextTab), restored.shortcuts.chords.Chord(shortcutPreviousTab))
	}
}

func TestDigitShortcutSelectsTheTabAtThatPosition(t *testing.T) {
	state, _ := threeTabs(t)
	state.Click(hitHosts)
	state.setTabNumbers(true)
	digit := func(text string, repeat bool) gfx.EventResult {
		return state.Shortcut(gfx.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: keymap.Primary, Text: text, Repeat: repeat})
	}

	if result := digit("2", false); !result.Handled {
		t.Fatal("the digit shortcut was not handled")
	}
	if got := tabIndex(state); got != 1 || state.activeSection != sectionTerminal {
		t.Fatalf("tab=%d section=%d, want the second tab in front", got, state.activeSection)
	}
	if result := digit("3", true); !result.Handled || tabIndex(state) != 1 {
		t.Fatal("a held digit moved the tab")
	}
	if result := digit("1", false); !result.Handled || tabIndex(state) != 0 {
		t.Fatal("the first digit did not return to the first tab")
	}
	// Control plus a digit is a terminal control code, so a digit with no tab
	// behind it stays with the program; Command has no meaning there.
	result := digit("9", false)
	if keymap.Primary == gfx.ModifierControl && result.Handled {
		t.Fatal("a missing tab swallowed a terminal control code")
	}
	if got := tabIndex(state); got != 0 {
		t.Fatalf("a missing tab moved to %d", got)
	}
}

func TestTabNumberShortcutIsOneConfigurableFamily(t *testing.T) {
	state, _ := threeTabs(t)
	primary := keymap.Primary
	if got, want := state.shortcuts.chords.Chord(shortcutTabNumber), (keymap.Chord{Modifiers: primary, Char: '1'}); got != want {
		t.Fatalf("default = %+v, want %+v", got, want)
	}
	for _, digit := range []byte{'1', '5', '9'} {
		if got := state.shortcuts.actionFor(keymap.Chord{Modifiers: primary, Char: digit}); got != shortcutTabNumber {
			t.Fatalf("%c maps to %v, want the tab-number action", digit, got)
		}
	}

	// The row is rebound like any other action: one chord names the modifiers,
	// and the whole digit row follows it.
	state.openShortcutCapture(shortcutTabNumber)
	custom := keymap.Chord{Modifiers: gfx.ModifierAlt, Char: '3'}
	state.Key(gfx.KeyEvent{Modifiers: custom.Modifiers, Text: string(custom.Char)})
	if state.modal != modalNone || state.shortcuts.chords.Chord(shortcutTabNumber) != custom {
		t.Fatalf("capture left modal=%d chord=%+v", state.modal, state.shortcuts.chords.Chord(shortcutTabNumber))
	}
	if label := state.shortcuts.labels[shortcutTabNumber]; label != "Alt+1…9" {
		t.Fatalf("label = %q", label)
	}
	if got := state.shortcuts.actionFor(keymap.Chord{Modifiers: custom.Modifiers, Char: '7'}); got != shortcutTabNumber {
		t.Fatalf("the rebound family did not cover 7: %v", got)
	}
	if got := state.shortcuts.actionFor(keymap.Chord{Modifiers: primary, Char: '5'}); got == shortcutTabNumber {
		t.Fatal("the old modifiers still matched after the rebind")
	}

	state.save()
	restored := newTestState(t, state.options)
	if restored.shortcuts.chords.Chord(shortcutTabNumber) != custom || restored.shortcuts.labels[shortcutTabNumber] != "Alt+1…9" {
		t.Fatalf("restored chord=%+v label=%q", restored.shortcuts.chords.Chord(shortcutTabNumber), restored.shortcuts.labels[shortcutTabNumber])
	}

	// Only a digit can name the row.
	state.openShortcutCapture(shortcutTabNumber)
	state.Key(gfx.KeyEvent{Modifiers: custom.Modifiers, Text: "a"})
	if state.modal != modalShortcutCapture || state.shortcutError != state.messages.ShortcutsTabNumberInvalid {
		t.Fatalf("a letter was accepted: modal=%d error=%q", state.modal, state.shortcutError)
	}
	state.clickShortcutCapture(hitShortcutCaptureCancel)
}

func TestANamedKeyNeverBecomesASnippetShortcut(t *testing.T) {
	chord := keymap.Chord{Modifiers: gfx.ModifierControl | gfx.ModifierShift, Key: gfx.KeyTab}
	if !chord.Valid() || validForSnippet(chord) {
		t.Fatalf("valid=%v validForSnippet=%v", chord.Valid(), validForSnippet(chord))
	}
}
