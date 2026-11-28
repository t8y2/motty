package workbench

import "testing"

func TestSnippetPickerTreeTypeaheadExecutesFocusedCommand(t *testing.T) {
	world, options := newSharingWorld(t)
	state := newTestState(t, options)
	world.connectFirst(t, state)
	addTestSnippets(state, snippet{Name: "Alpha", Command: "echo alpha"}, snippet{Name: "Bravo", Command: "echo bravo"})
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	if err := view.Keys("Tab"); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("b"); err != nil {
		t.Fatal(err)
	}
	if view.Snapshot().Focused != state.commandPalette.rowID(1) {
		t.Fatalf("typeahead focus: %v", view.Snapshot().Focused)
	}
	if state.commandPalette.selected != 1 {
		t.Fatal("typeahead did not select the focused command")
	}
	if err := view.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	if got := world.conns[0].shell(t).typed(); got != "echo bravo" {
		t.Fatalf("sent %q", got)
	}
}
