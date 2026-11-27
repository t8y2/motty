package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSnippetPickerFlatViewOffersActionsWithoutOrdering(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops"}, snippet{Name: "Second", Command: "two", Group: "Ops"})
	state.snippets.pickerGrouped = false
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, state.commandPalette.rowID(0))
	if err := view.RightClick(row.X+80, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	for _, label := range []string{state.messages.Tools.SnippetEdit, state.messages.Tools.SnippetDelete, state.messages.Tools.SnippetCopyContent} {
		dualHitLabel(t, view, label)
	}
	for _, hit := range view.Snapshot().Hits {
		if hit.Label == state.messages.Tools.SnippetMoveUp || hit.Label == state.messages.Tools.SnippetMoveDown {
			t.Fatal("flat view offered ordering")
		}
	}
	state.snippetMenu.open = false
	state.commandPaletteKey(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: gfx.KeyDown, Modifiers: gfx.ModifierAlt})
	state.chooseSnippetMenu(hitSnippetMenuDown)
	if !sameShape(pickerShape(state), []string{"First", "Second"}) || state.commandPalette.usesSnippetTree() {
		t.Fatal("flat view enabled reordering")
	}
}
