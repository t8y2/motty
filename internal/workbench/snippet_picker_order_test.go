package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestSnippetPickerKeyboardReordersSearchAndPersists(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	shell := world.conns[0].shell(t)
	addTestSnippets(state,
		snippet{Name: "First", Command: "echo first", Group: "Ops/Prod"},
		snippet{Name: "Other", Command: "echo other", Group: "Other"},
		snippet{Name: "Last", Command: "echo last", Group: "Ops/Prod"},
	)
	state.snippets.filter = snippetFilter{group: "Other"}
	state.openSnippetPicker()
	state.commandPalette.search.Insert("Ops")
	state.refreshCommandPalette()
	for _, step := range []struct {
		key  gfx.Key
		want []string
	}{
		{gfx.KeyDown, []string{"Last", "First"}},
		{gfx.KeyUp, []string{"First", "Last"}},
		{gfx.KeyDown, []string{"Last", "First"}},
	} {
		result, handled := state.commandPaletteKey(gfx.KeyEvent{Target: hitCommandPaletteSearch, Key: step.key, Modifiers: gfx.ModifierAlt})
		if !handled || !result.Handled || result.Focus != hitCommandPaletteSearch || !sameShape(pickerShape(state), step.want) {
			t.Fatalf("key=%v result=%+v order=%v", step.key, result, pickerShape(state))
		}
	}
	if shell.typed() != "" || state.modal != modalCommandPalette || state.commandPalette.search.String() != "Ops" {
		t.Fatal("reorder sent a command or lost the picker search")
	}
	restored := newTestState(t, options)
	restored.openSnippetPicker()
	restored.commandPalette.search.Insert("Ops")
	restored.refreshCommandPalette()
	if !sameShape(pickerShape(restored), []string{"Last", "First"}) {
		t.Fatalf("restored order=%v", pickerShape(restored))
	}
}

func TestSnippetPickerMenuMovesUsePickerOrder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "one"}, snippet{Name: "Second", Command: "two"})
	state.snippets.filter = snippetFilter{group: "Unrelated"}
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	for _, step := range []struct {
		row   int
		label string
		want  []string
	}{
		{0, state.messages.Tools.SnippetMoveDown, []string{"Second", "First"}},
		{1, state.messages.Tools.SnippetMoveUp, []string{"First", "Second"}},
	} {
		row := dualHit(t, view, state.commandPalette.rowID(step.row))
		if err := view.RightClick(row.X+80, row.Y+row.Height/2); err != nil {
			t.Fatal(err)
		}
		if err := view.ClickLabel(step.label); err != nil {
			t.Fatal(err)
		}
		if !sameShape(pickerShape(state), step.want) || state.modal != modalCommandPalette {
			t.Fatalf("menu move: %v", pickerShape(state))
		}
	}
}
