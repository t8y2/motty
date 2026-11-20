package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

// The broadcast key is the command palette's and the toolbar button's toggle on
// a chord: in a split tab it starts and stops the broadcast, and in a tab that
// cannot broadcast it says why instead of failing silently.
func TestBroadcastShortcutTogglesTheTabsBroadcast(t *testing.T) {
	state, first, second, firstID, secondID := twoPanes(t)
	chord := state.shortcuts.chords.Chord(shortcutBroadcast)
	if !chord.Valid() {
		t.Fatal("broadcast has no default shortcut")
	}

	if result := pressChord(state, chord); !result.Handled || !state.broadcasting(firstID) || !state.broadcasting(secondID) {
		t.Fatalf("shortcut did not start the broadcast: result=%+v broadcast=%d", result, state.broadcast)
	}
	insertText(state, secondID, "ls")
	if first.typed() != "ls" || second.typed() != "ls" {
		t.Fatalf("while broadcasting: first %q, second %q", first.typed(), second.typed())
	}

	if result := pressChord(state, chord); !result.Handled || state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastOff {
		t.Fatalf("shortcut did not stop the broadcast: result=%+v broadcast=%d status=%q", result, state.broadcast, state.status)
	}
}

func TestBroadcastShortcutInATabWithOnePaneSaysWhyItCannotStart(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	state.activeSection = sectionTerminal

	pressChord(state, state.shortcuts.chords.Chord(shortcutBroadcast))
	if state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastNeedsSplit {
		t.Fatalf("broadcast=%d status=%q", state.broadcast, state.status)
	}
}

// Away from the terminal there is no tab in view to broadcast to, so the press
// is not taken and stays with the page that has the keyboard.
func TestBroadcastShortcutLeavesOtherPagesAlone(t *testing.T) {
	state, _, _, _, _ := twoPanes(t)
	showShortcutSettings(state)
	if result := pressChord(state, state.shortcuts.chords.Chord(shortcutBroadcast)); result.Handled || state.broadcast != 0 {
		t.Fatalf("shortcut acted outside the terminal: result=%+v broadcast=%d", result, state.broadcast)
	}
}

// The chord is rebindable and clearable like any built-in one: a cleared
// broadcast key is no longer taken, so the terminal program receives it, and a
// user's existing binding on the new default is never replaced.
func TestBroadcastShortcutCanBeReboundClearedAndNeverTakesAUsersChord(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	state.Click(hitShortcutRowList.At(int(shortcutBroadcast - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutBroadcast {
		t.Fatal("the broadcast settings row did not open shortcut capture")
	}
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt, Char: 'x'}
	pressChord(state, custom)
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutBroadcast) != custom {
		t.Fatal("the custom broadcast shortcut was not persisted")
	}
	if got := restored.shortcuts.actionFor(defaultShortcuts().chords.Chord(shortcutBroadcast)); got != shortcutNone {
		t.Fatalf("the old default still runs %v after rebinding", got)
	}

	restored.openShortcutCapture(shortcutBroadcast)
	restored.Click(hitShortcutCaptureDelete)
	restored = newTestState(t, options)
	chord := defaultShortcuts().chords.Chord(shortcutBroadcast)
	if restored.shortcuts.chords.Chord(shortcutBroadcast).Valid() {
		t.Fatal("a cleared broadcast shortcut returned after restarting")
	}
	restored.activeSection = sectionTerminal
	if result := pressChord(restored, chord); result.Handled {
		t.Fatal("a cleared broadcast shortcut still took the key from the terminal")
	}

	taken := restoreShortcuts([]savedShortcut{
		{Action: "find", Key: string(chord.Char), Modifiers: uint8(chord.Modifiers)},
	})
	if taken.chords.Chord(shortcutFind) != chord || taken.chords.Chord(shortcutBroadcast).Valid() {
		t.Fatal("the new default replaced a chord the user had already bound")
	}
}
