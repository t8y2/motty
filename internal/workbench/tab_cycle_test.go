package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
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
	next := ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl}
	previous := ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl | ui.ModifierShift}

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
	state.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl})
	if state.activeSection != sectionTerminal || tabIndex(state) != 0 {
		t.Fatalf("section=%d tab=%d, want the terminal on the first tab", state.activeSection, tabIndex(state))
	}
	state.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl, Repeat: true})
	if tabIndex(state) != 1 {
		t.Fatalf("a held Control+Tab stopped at tab %d", tabIndex(state))
	}
	// The release of a press the terminal never saw is not for the terminal.
	if result := state.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl, Release: true}); !result.Handled {
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
	if result := state.Key(ui.KeyEvent{Key: ui.KeyTab, Modifiers: ui.ModifierControl}); result.Handled && tabIndex(state) != before {
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
	want := map[shortcutAction]shortcutChord{
		shortcutNextTab:     {Modifiers: ui.ModifierControl, Named: ui.KeyTab},
		shortcutPreviousTab: {Modifiers: ui.ModifierControl | ui.ModifierShift, Named: ui.KeyTab},
	}
	for action, chord := range want {
		if state.shortcuts.chords[action] != chord {
			t.Fatalf("%v default = %+v, want %+v", action, state.shortcuts.chords[action], chord)
		}
	}

	state.openShortcutCapture(shortcutNextTab)
	result := state.Key(ui.KeyEvent{Key: ui.KeyPageDown, Modifiers: ui.ModifierControl})
	custom := shortcutChord{Modifiers: ui.ModifierControl, Named: ui.KeyPageDown}
	if !result.Handled || state.modal != modalNone || state.shortcuts.chords[shortcutNextTab] != custom {
		t.Fatalf("capture result=%+v modal=%d chord=%+v", result, state.modal, state.shortcuts.chords[shortcutNextTab])
	}
	if label := state.shortcuts.labels[shortcutNextTab]; label != "Ctrl+PageDown" {
		t.Fatalf("label = %q", label)
	}

	// A chord another action holds is refused, named key or not.
	state.openShortcutCapture(shortcutPreviousTab)
	state.Key(ui.KeyEvent{Key: ui.KeyPageDown, Modifiers: ui.ModifierControl})
	if state.modal != modalShortcutCapture || state.shortcutError == "" {
		t.Fatalf("a taken chord was accepted: modal=%d error=%q", state.modal, state.shortcutError)
	}
	state.clickShortcutCapture(hitShortcutCaptureCancel)

	state.save()
	restored := newTestState(t, options)
	if restored.shortcuts.chords[shortcutNextTab] != custom || restored.shortcuts.chords[shortcutPreviousTab] != want[shortcutPreviousTab] {
		t.Fatalf("restored next=%+v previous=%+v", restored.shortcuts.chords[shortcutNextTab], restored.shortcuts.chords[shortcutPreviousTab])
	}
}

func TestDigitShortcutSelectsTheTabAtThatPosition(t *testing.T) {
	state, _ := threeTabs(t)
	state.Click(hitHosts)
	digit := func(text string, repeat bool) ui.EventResult {
		return state.Shortcut(ui.KeyEvent{Target: state.activeTerminalFocusHit(), Modifiers: defaultShortcutModifier(), Text: text, Repeat: repeat})
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
	if defaultShortcutModifier() == ui.ModifierControl && result.Handled {
		t.Fatal("a missing tab swallowed a terminal control code")
	}
	if got := tabIndex(state); got != 0 {
		t.Fatalf("a missing tab moved to %d", got)
	}
}

func TestTabNumberShortcutIsOneConfigurableFamily(t *testing.T) {
	state, _ := threeTabs(t)
	primary := defaultShortcutModifier()
	if got, want := state.shortcuts.chords[shortcutTabNumber], (shortcutChord{Modifiers: primary, Key: '1'}); got != want {
		t.Fatalf("default = %+v, want %+v", got, want)
	}
	for _, digit := range []byte{'1', '5', '9'} {
		if got := state.shortcuts.actionFor(shortcutChord{Modifiers: primary, Key: digit}); got != shortcutTabNumber {
			t.Fatalf("%c maps to %v, want the tab-number action", digit, got)
		}
	}

	// The row is rebound like any other action: one chord names the modifiers,
	// and the whole digit row follows it.
	state.openShortcutCapture(shortcutTabNumber)
	custom := shortcutChord{Modifiers: ui.ModifierAlt, Key: '3'}
	state.Key(ui.KeyEvent{Modifiers: custom.Modifiers, Text: string(custom.Key)})
	if state.modal != modalNone || state.shortcuts.chords[shortcutTabNumber] != custom {
		t.Fatalf("capture left modal=%d chord=%+v", state.modal, state.shortcuts.chords[shortcutTabNumber])
	}
	if label := state.shortcuts.labels[shortcutTabNumber]; label != "Alt+1…9" {
		t.Fatalf("label = %q", label)
	}
	if got := state.shortcuts.actionFor(shortcutChord{Modifiers: custom.Modifiers, Key: '7'}); got != shortcutTabNumber {
		t.Fatalf("the rebound family did not cover 7: %v", got)
	}
	if got := state.shortcuts.actionFor(shortcutChord{Modifiers: primary, Key: '5'}); got == shortcutTabNumber {
		t.Fatal("the old modifiers still matched after the rebind")
	}

	state.save()
	restored := newTestState(t, state.options)
	if restored.shortcuts.chords[shortcutTabNumber] != custom || restored.shortcuts.labels[shortcutTabNumber] != "Alt+1…9" {
		t.Fatalf("restored chord=%+v label=%q", restored.shortcuts.chords[shortcutTabNumber], restored.shortcuts.labels[shortcutTabNumber])
	}

	// Only a digit can name the row.
	state.openShortcutCapture(shortcutTabNumber)
	state.Key(ui.KeyEvent{Modifiers: custom.Modifiers, Text: "a"})
	if state.modal != modalShortcutCapture || state.shortcutError != state.messages.ShortcutsTabNumberInvalid {
		t.Fatalf("a letter was accepted: modal=%d error=%q", state.modal, state.shortcutError)
	}
	state.clickShortcutCapture(hitShortcutCaptureCancel)
}

func TestANamedKeyNeverBecomesASnippetShortcut(t *testing.T) {
	chord := shortcutChord{Modifiers: ui.ModifierControl | ui.ModifierShift, Named: ui.KeyTab}
	if !chord.valid() || chord.validForSnippet() {
		t.Fatalf("valid=%v validForSnippet=%v", chord.valid(), chord.validForSnippet())
	}
}
