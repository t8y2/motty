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

func TestANamedKeyNeverBecomesASnippetShortcut(t *testing.T) {
	chord := shortcutChord{Modifiers: ui.ModifierControl | ui.ModifierShift, Named: ui.KeyTab}
	if !chord.valid() || chord.validForSnippet() {
		t.Fatalf("valid=%v validForSnippet=%v", chord.valid(), chord.validForSnippet())
	}
}
