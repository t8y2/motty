package workbench

import (
	"maps"
	"testing"
)

func TestSnippetPickerTreeMovesGroupsUsingPageRules(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "A cmd", Command: "a", Group: "A"},
		snippet{Name: "B cmd", Command: "b", Group: "B/Child"})
	state.snippets.treeCollapsedGroups = map[string]bool{"a": true}
	state.snippets.selected = 0
	state.snippets.selectedFolder = "A"
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	source := dualHit(t, view, state.commandPalette.rowID(1))
	target := dualHit(t, view, state.commandPalette.rowID(0))
	dualDrag(t, view, source, target)
	if state.snippets.list[1].Group != "A/B/Child" {
		t.Fatalf("group move did not use the directory path: %q", state.snippets.list[1].Group)
	}
	if !sameShape(pickerShape(state), []string{"#A", "#B", "A cmd"}) {
		t.Fatalf("picker after moving group: %v", pickerShape(state))
	}
	if state.snippets.selected != 0 || state.snippets.selectedFolder != "A" ||
		!maps.Equal(state.snippets.treeCollapsedGroups, map[string]bool{"a": true}) {
		t.Fatal("picker move changed the settings tree UI state")
	}
	restored := newTestState(t, options)
	if restored.snippets.list[1].Group != "A/B/Child" {
		t.Fatal("shared group path was not persisted")
	}
	state.rebuildSnippetTreeRows()
	if !sameShape(state.snippets.groups, []string{"A", "A/B", "A/B/Child"}) {
		t.Fatalf("shared group order: %v", state.snippets.groups)
	}
}
