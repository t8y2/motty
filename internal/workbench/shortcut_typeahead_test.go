package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

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
