package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// A window offers a printable key to OnShortcut, not OnKey (see app.Config),
// so an open menu's typeahead has to be driven from there.
func TestMenuTypeaheadReachesTypedLettersThroughShortcut(t *testing.T) {
	state := newTestState(t, testOptions(t))
	rightClickHost(state, state.hosts[0].ID)
	canvas, _ := paintedTexts(state)
	target := canvas.Focused()
	if target.IsZero() {
		target = hitHostMenuConnect
	}
	result := state.Shortcut(gfx.KeyEvent{Target: target, Text: "e"})
	if !result.Handled || result.Focus != hitHostMenuEdit {
		t.Fatalf("typing e in the host menu focused %v (handled %v), want Edit", result.Focus, result.Handled)
	}
}

// A field that edits text keeps its letters: OnShortcut runs before the text
// system only to let the application own a chord.
func TestShortcutLeavesTypingToTextFields(t *testing.T) {
	state := newTestState(t, testOptions(t))
	clickHostAction(state, hitAddHost)
	for _, target := range []gfx.ID{hitDialogLabel, hitDialogIcon, hitDialogGroup} {
		if result := state.Shortcut(gfx.KeyEvent{Target: target, Text: "o"}); result.Handled {
			t.Fatalf("a letter in field %v was taken before the text system", target)
		}
	}
}
