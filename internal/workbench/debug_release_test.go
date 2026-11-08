//go:build !dev

package workbench

import (
	"testing"

	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

// A release build has no component gallery: a state file that saved it falls
// back to the hosts, and neither the sidebar nor the command palette offers it.
func TestReleaseBuildHasNoDebugGallery(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.restoreWorkspace("debug")
	if state.activeSection != sectionHosts {
		t.Fatalf("restoring the debug workspace opened section %d, want hosts", state.activeSection)
	}

	_, texts := paintedTexts(state)
	if contains(texts, state.messages.NavDebug) {
		t.Fatalf("the sidebar still offers %q: %v", state.messages.NavDebug, texts)
	}

	state.rebuildCommandPalette()
	palette := &state.commandPalette
	for index := 0; index < len(palette.entries); index++ {
		if palette.entries[index].label == state.messages.NavDebug {
			t.Fatalf("the command palette still offers %q", state.messages.NavDebug)
		}
	}
}

// The inspector is a development tool: its chord means nothing in a release build.
func TestReleaseBuildHasNoInspector(t *testing.T) {
	state := newTestState(t, testOptions(t))
	chord := ui.KeyEvent{Modifiers: keymap.Primary | ui.ModifierAlt | ui.ModifierShift, Text: "i"}
	if result := state.Shortcut(chord); result.Handled {
		t.Fatalf("the inspector chord was taken in a release build: %+v", result)
	}
}
