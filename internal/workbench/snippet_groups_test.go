package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
)

// "cd " and "ssh " are sent to be finished by hand: the space the user typed
// after them is part of the command, through saving, restoring and sending.
func TestSnippetCommandKeepsItsSurroundingSpace(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("  go to  ")
	state.snippets.command.Insert(" cd ")
	state.snippets.run = false
	state.Click(hitSnippetSubmit)
	if len(state.snippets.list) != 1 {
		t.Fatal("the snippet was not saved")
	}
	if got := state.snippets.list[0]; got.Name != "go to" || got.Command != " cd " {
		t.Fatalf("saved name %q, command %q", got.Name, got.Command)
	}

	state.openSnippetDialog(0)
	if got := state.snippets.command.String(); got != " cd " {
		t.Fatalf("editing shows command %q", got)
	}

	saved := savedState{Snippets: []savedSnippet{{Name: "ssh", Command: "ssh "}}}
	state.restoreTerminalTools(saved)
	if got := state.snippets.list[0].Command; got != "ssh " {
		t.Fatalf("restored command %q", got)
	}
}

func TestSnippetCommandOfOnlySpaceIsRefused(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("blank")
	state.snippets.command.Insert("   ")
	state.Click(hitSnippetSubmit)
	if state.modal != modalSnippet || len(state.snippets.list) != 0 || state.snippets.dialogError == "" {
		t.Fatal("a command of only space was saved")
	}
}

func addTestSnippets(state *State, entries ...snippet) {
	state.snippets.list = append(state.snippets.list[:0], entries...)
	state.refreshSnippets()
}

// The page lists the snippets of one group at a time, and a group exists only
// while a snippet names it.
func TestSnippetFilterShowsOneGroup(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state,
		snippet{Name: "a", Command: "a", Group: "Ops"},
		snippet{Name: "b", Command: "b"},
		snippet{Name: "c", Command: "c", Group: "dev"},
		snippet{Name: "d", Command: "d", Group: "ops"},
	)
	form := &state.snippets
	if len(form.groups) != 2 {
		t.Fatalf("groups = %v, want Ops and dev once each", form.groups)
	}
	if len(form.visible) != 4 {
		t.Fatalf("unfiltered page shows %d rows", len(form.visible))
	}

	form.filter = snippetFilter{group: "OPS"}
	state.refreshSnippets()
	if len(form.visible) != 2 {
		t.Fatalf("filter by Ops shows %v", form.visible)
	}
	for _, index := range form.visible {
		if !sameSnippetGroup(form.list[index].Group, "ops") {
			t.Fatalf("row %d of group %q is shown under Ops", index, form.list[index].Group)
		}
	}
	if form.selected != form.visible[0] && !containsInt(form.visible, form.selected) {
		t.Fatalf("selected row %d is hidden by the filter", form.selected)
	}

	form.filter = snippetFilter{ungrouped: true}
	state.refreshSnippets()
	if len(form.visible) != 1 || form.list[form.visible[0]].Name != "b" {
		t.Fatalf("ungrouped filter shows %v", form.visible)
	}

	// Removing the last snippet of the filtered group must not leave an empty page.
	form.filter = snippetFilter{group: "dev"}
	state.refreshSnippets()
	state.deleteSnippet(2)
	if !form.filter.all() || len(form.visible) != 3 {
		t.Fatalf("after the group went away: filter %+v, %d rows", form.filter, len(form.visible))
	}
}

func containsInt(values []int, want int) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func TestSnippetFilterIsChosenFromThePage(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "a", Command: "a", Group: "Ops"},
		snippet{Name: "b", Command: "b"},
	)
	state.Click(hitSnippetFilter)
	if !state.snippets.filterOpen {
		t.Fatal("the filter did not open")
	}
	state.Click(hitSnippetFilterList.At(0))
	if state.snippets.filterOpen || state.snippets.filter.group != "Ops" || len(state.snippets.visible) != 1 {
		t.Fatalf("filter = %+v open=%v visible=%v", state.snippets.filter, state.snippets.filterOpen, state.snippets.visible)
	}
	state.Click(hitSnippetFilter)
	state.Click(hitSnippetFilterAll)
	if !state.snippets.filter.all() || len(state.snippets.visible) != 2 {
		t.Fatalf("All groups left filter %+v", state.snippets.filter)
	}
}

// A snippet saved while another group is filtered must not vanish from the page.
func TestSavedSnippetStaysVisibleUnderTheFilter(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"}, snippet{Name: "b", Command: "b", Group: "dev"})
	state.snippets.filter = snippetFilter{group: "Ops"}
	state.refreshSnippets()

	state.openSnippetDialog(-1)
	state.snippets.name.Insert("c")
	state.snippets.command.Insert("c")
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone {
		t.Fatalf("save failed: %q", state.snippets.dialogError)
	}
	if !containsInt(state.snippets.visible, state.snippets.selected) {
		t.Fatal("the snippet just saved is hidden by the filter")
	}
}

func TestSnippetGroupIsChosenOrCreatedInTheDialog(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state, snippet{Name: "a", Command: "a", Group: "Ops"})

	// An existing group, picked by a different spelling, keeps its one name.
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("b")
	state.snippets.command.Insert("b")
	state.snippets.groupPicker.Query.Insert("ops")
	state.Click(hitSnippetSubmit)
	if state.modal != modalNone || state.snippets.list[1].Group != "Ops" {
		t.Fatalf("group = %q, modal %v", state.snippets.list[1].Group, state.modal)
	}

	// A new group is created by typing it, with Enter or Save.
	state.openSnippetDialog(-1)
	state.snippets.name.Insert("c")
	state.snippets.command.Insert("c")
	state.snippets.groupPicker.Query.Insert("Dev")
	state.Key(ui.KeyEvent{Target: hitSnippetGroup, Key: ui.KeyEnter})
	if state.modal != modalSnippet || state.snippets.groupPicker.Value() != "Dev" {
		t.Fatalf("Enter in the group field: modal %v, value %q", state.modal, state.snippets.groupPicker.Value())
	}
	state.Key(ui.KeyEvent{Target: hitSnippetGroup, Key: ui.KeyEnter})
	if state.modal != modalNone || state.snippets.list[2].Group != "Dev" {
		t.Fatalf("second Enter did not save into Dev: %+v", state.snippets.list[2])
	}

	// Editing shows the group, and clearing it ungroups the snippet.
	state.openSnippetDialog(2)
	if state.snippets.groupPicker.Value() != "Dev" {
		t.Fatalf("editing shows group %q", state.snippets.groupPicker.Value())
	}
	state.Click(hitSnippetGroupClear)
	state.Click(hitSnippetSubmit)
	if state.snippets.list[2].Group != "" {
		t.Fatalf("group after clearing = %q", state.snippets.list[2].Group)
	}
}

func TestSnippetGroupSurvivesRestore(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.restoreTerminalTools(savedState{Snippets: []savedSnippet{{Name: "a", Command: "a", Group: " Ops "}}})
	if got := state.snippets.list[0].Group; got != "Ops" {
		t.Fatalf("restored group %q", got)
	}
}

// The terminal's picker is sectioned by group while nothing is searched, and a
// search shows the matches alone, found by group name too.
func TestSnippetPickerSectionsByGroup(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	addTestSnippets(state,
		snippet{Name: "a", Command: "a", Group: "Ops"},
		snippet{Name: "b", Command: "b"},
		snippet{Name: "c", Command: "c", Group: "Dev"},
	)
	state.openSnippetPicker()
	palette := &state.commandPalette
	var shape []string
	for _, index := range palette.visible {
		entry := palette.entries[index]
		if entry.header {
			shape = append(shape, "#"+entry.label)
		} else {
			shape = append(shape, entry.label)
		}
	}
	want := []string{"#Dev", "c", "#Ops", "a", "#" + state.messages.Tools.SnippetFilterNone, "b"}
	if len(shape) != len(want) {
		t.Fatalf("picker rows %v, want %v", shape, want)
	}
	for i := range want {
		if shape[i] != want[i] {
			t.Fatalf("picker rows %v, want %v", shape, want)
		}
	}
	if palette.entries[palette.visible[palette.selected]].header {
		t.Fatal("a heading holds the selection")
	}

	palette.search.Insert("ops")
	state.refreshCommandPalette()
	if len(palette.visible) != 1 || palette.entries[palette.visible[0]].label != "a" {
		t.Fatalf("search by group name shows %d rows", len(palette.visible))
	}
}
