package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func TestSnippetPickerDefaultShortcutIsUnclaimed(t *testing.T) {
	shortcuts := defaultShortcuts()
	want := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierShift, Char: 'g'}
	if got := shortcuts.chords.Chord(shortcutSnippetPicker); got != want {
		t.Fatalf("snippet picker shortcut = %+v, want %+v", got, want)
	}
	for action := shortcutAction(1); action < shortcutActionCount; action++ {
		chord := shortcuts.chords.Chord(action)
		if !chord.Valid() || shortcuts.boundAction(chord) != action {
			t.Fatalf("%s lost its default shortcut: %+v", action, chord)
		}
		if action != shortcutSnippetPicker && chord == want {
			t.Fatalf("snippet picker shares its shortcut with %s", action)
		}
	}

	// Upgrading an existing configuration must preserve a user's binding that
	// already owns the new default, leaving the picker available to configure.
	restored := restoreShortcuts([]savedShortcut{
		{Action: "find", Key: "g", Modifiers: uint8(want.Modifiers)},
	})
	if restored.chords.Chord(shortcutFind) != want || restored.chords.Chord(shortcutSnippetPicker).Valid() {
		t.Fatal("the new default replaced an existing custom shortcut")
	}
}

func TestSnippetPickerShortcutOpensSearchAndSendsChosenCommand(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	state.snippets.list = []snippet{{Name: "Disk usage", Command: "df -h"}}
	state.refreshSnippets()
	state.activeSection = sectionTerminal

	result := pressChord(state, state.shortcuts.chords.Chord(shortcutSnippetPicker))
	if !result.Handled || !result.Repaint || result.Focus != hitCommandPaletteSearch ||
		state.modal != modalCommandPalette || !state.commandPalette.snippets {
		t.Fatalf("shortcut did not focus snippet search: result=%+v modal=%d", result, state.modal)
	}
	if shell.typed() != "" {
		t.Fatalf("opening the picker sent terminal input: %q", shell.typed())
	}
	state.Click(hitCommandPaletteItems.At(0))
	if state.modal != modalNone || shell.typed() != "df -h" {
		t.Fatalf("choosing a snippet left modal=%d terminal input=%q", state.modal, shell.typed())
	}
}

func TestSnippetPickerShortcutCanBeConfiguredAndRestored(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	showShortcutSettings(state)
	if got := state.shortcutActionLabel(shortcutSnippetPicker); got == "" || got != state.messages.Tools.SnippetsTitle {
		t.Fatalf("snippet picker settings label = %q", got)
	}
	state.Click(hitShortcutRowList.At(int(shortcutSnippetPicker - 1)))
	if state.modal != modalShortcutCapture || state.shortcutCapture != shortcutSnippetPicker {
		t.Fatal("the snippet picker settings row did not open shortcut capture")
	}
	pressChord(state, state.shortcuts.chords.Chord(shortcutCommandPalette))
	if state.modal != modalShortcutCapture || state.shortcutError == "" {
		t.Fatal("the picker accepted a shortcut already owned by the command palette")
	}
	custom := keymap.Chord{Modifiers: keymap.Primary | gfx.ModifierAlt, Char: 'x'}
	pressChord(state, custom)
	restored := newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutSnippetPicker) != custom {
		t.Fatal("the custom snippet picker shortcut was not persisted")
	}
	if result := pressChord(restored, custom); !result.Handled || !restored.commandPalette.snippets {
		t.Fatal("the custom shortcut did not open the snippet picker")
	}
	restored.closeCommandPalette()
	if result := pressChord(restored, defaultShortcuts().chords.Chord(shortcutSnippetPicker)); result.Handled {
		t.Fatal("the old default shortcut remained active after rebinding")
	}
	restored.openShortcutCapture(shortcutSnippetPicker)
	restored.Click(hitShortcutCaptureDelete)
	restored = newTestState(t, options)
	if restored.shortcuts.chords.Chord(shortcutSnippetPicker).Valid() {
		t.Fatal("a deleted snippet picker shortcut returned after restarting")
	}
	restored.openShortcutCapture(shortcutSnippetPicker)
	restored.Click(hitShortcutCaptureReset)
	if got, want := restored.shortcuts.chords.Chord(shortcutSnippetPicker), defaultShortcuts().chords.Chord(shortcutSnippetPicker); got != want {
		t.Fatalf("reset snippet picker shortcut = %+v, want %+v", got, want)
	}
}
