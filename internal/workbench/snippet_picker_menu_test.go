package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
)

func TestSnippetPickerRightClickEditsInlineAndPersists(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "First", Command: "echo first"}, snippet{Name: "Second", Command: "echo second", Group: "Ops/Linux"})
	state.openSnippetPicker()
	state.commandPalette.search.Insert("Second")
	state.refreshCommandPalette()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, state.commandPalette.rowID(0))
	if err := view.RightClick(row.X+80, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.snippetMenu.open || !state.snippetMenu.fromPicker || state.snippetMenu.index != 1 {
		t.Fatalf("menu: %+v", state.snippetMenu)
	}
	if err := view.ClickLabel(state.messages.Tools.SnippetEdit); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNone || state.activeSection != sectionSnippets ||
		state.snippets.selected != 1 || state.snippets.inline.loadedIndex != 1 ||
		view.Snapshot().Focused != hitSnippetInlineName || state.commandPalette.returnToPicker {
		t.Fatalf("inline edit: modal=%v section=%v selected=%d loaded=%d focus=%v pending=%v want=%v returning=%v", state.modal, state.activeSection, state.snippets.selected, state.snippets.inline.loadedIndex, view.Snapshot().Focused, state.pendingFocus, hitSnippetInlineName, state.commandPalette.returnToPicker)
	}
	if state.snippetTreeRowIDForSnippet(1).IsZero() {
		t.Fatal("edit did not reveal the snippet in its directory")
	}
	state.snippets.inline.command = text.Editor{}
	state.snippets.inline.command.Insert("echo changed")
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	restored := newTestState(t, options)
	if restored.snippets.list[1].Command != "echo changed" {
		t.Fatal("inline edit was not persisted")
	}
	state.openSnippetPicker()
	state.commandPalette.search.Insert("Second")
	state.refreshCommandPalette()
	if got := pickerShape(state); !sameShape(got, []string{"Second"}) {
		t.Fatalf("results: %v", got)
	}
}

func TestSnippetPickerRightClickDeletesAndReturnsToList(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "First", Command: "echo first"}, snippet{Name: "Second", Command: "echo second"})
	state.focusInlineSnippet(0)
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	row := dualHit(t, view, state.commandPalette.rowID(0))
	if err := view.RightClick(row.X+80, row.Y+row.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := view.ClickLabel(state.messages.Tools.SnippetDelete); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalSnippetDelete {
		t.Fatal("delete confirmation was not shown")
	}
	state.Click(hitSnippetDeleteCancel)
	if state.modal != modalCommandPalette || !sameShape(pickerShape(state), []string{"First", "Second"}) {
		t.Fatal("cancel did not return to the unchanged picker")
	}
	state.chooseSnippetMenu(hitSnippetMenuDelete)
	state.Click(hitSnippetDeleteConfirm)
	if state.modal != modalCommandPalette || !sameShape(pickerShape(state), []string{"Second"}) {
		t.Fatalf("after delete: %v", pickerShape(state))
	}
	if state.snippets.inline.loadedSnap.Name != "Second" || state.commandPalette.returnToPicker {
		t.Fatal("delete left a stale inline editor or picker return state")
	}
	restored := newTestState(t, options)
	if len(restored.snippets.list) != 1 || restored.snippets.list[0].Name != "Second" {
		t.Fatal("delete was not persisted")
	}
}
