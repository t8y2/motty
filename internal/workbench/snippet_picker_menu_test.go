package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
)

func TestSnippetPickerRightClickEditsAndReturnsToSearch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "echo first"}, snippet{Name: "Second", Command: "echo second"})
	state.openSnippetPicker()
	state.commandPalette.search.Insert("Second")
	state.refreshCommandPalette()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, hitCommandPaletteItems.At(0))
	if err := view.RightClick(row.X+80, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.snippetMenu.open || !state.snippetMenu.fromPicker || state.snippetMenu.index != 1 {
		t.Fatalf("menu: %+v", state.snippetMenu)
	}
	if err := view.ClickLabel(state.messages.Tools.SnippetEdit); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalSnippet || state.snippets.editing != 1 {
		t.Fatal("edit opened the wrong snippet")
	}
	state.snippets.command = text.Editor{}
	state.snippets.command.Insert("echo changed")
	state.submitSnippet()
	if state.modal != modalCommandPalette || state.snippets.list[1].Command != "echo changed" ||
		state.commandPalette.search.String() != "Second" {
		t.Fatal("edit did not return to the updated picker")
	}
	if got := pickerShape(state); !sameShape(got, []string{"Second"}) {
		t.Fatalf("results: %v", got)
	}
}

func TestSnippetPickerRightClickDeletesAndReturnsToList(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state, snippet{Name: "First", Command: "echo first"}, snippet{Name: "Second", Command: "echo second"})
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, hitCommandPaletteItems.At(0))
	if err := view.RightClick(row.X+80, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.messages.Tools.SnippetDelete); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalSnippetDelete {
		t.Fatal("delete confirmation was not shown")
	}
	state.Click(hitSnippetDeleteConfirm)
	if state.modal != modalCommandPalette || !sameShape(pickerShape(state), []string{"Second"}) {
		t.Fatalf("after delete: %v", pickerShape(state))
	}
}
