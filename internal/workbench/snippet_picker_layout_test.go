//go:build windows || darwin || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestSnippetPickerFlatOrderFollowsManualOrder(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addTestSnippets(state,
		snippet{Name: "Git first", Command: "git status", Group: "git"},
		snippet{Name: "AI", Command: "codex", Group: "ai"},
		snippet{Name: "Git second", Command: "git diff", Group: "git"},
	)
	state.openSnippetPicker()
	if got := pickerShape(state); !sameShape(got, []string{"Git first", "AI", "Git second"}) {
		t.Fatalf("default order = %v", got)
	}
	state.closeCommandPalette()
	if !state.moveSnippetTo(2, 0) {
		t.Fatal("manual move failed")
	}
	state.openSnippetPicker()
	if got := pickerShape(state); !sameShape(got, []string{"Git second", "Git first", "AI"}) {
		t.Fatalf("moved order = %v", got)
	}
	state.commandPalette.search.Insert("git")
	state.refreshCommandPalette()
	if got := pickerShape(state); !sameShape(got, []string{"Git second", "Git first"}) {
		t.Fatalf("search order = %v", got)
	}
}

func TestSnippetPickerGroupingTogglePersistsAndKeepsSearch(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state,
		snippet{Name: "Git", Command: "git status", Group: "git"},
		snippet{Name: "AI", Command: "codex", Group: "ai"},
	)
	state.openSnippetPicker()
	state.commandPalette.search.Insert("git")
	state.Click(hitSnippetPickerGrouped)
	if !state.snippets.pickerGrouped || state.modal != modalCommandPalette ||
		state.commandPalette.search.String() != "git" || !sameShape(pickerShape(state), []string{"Git"}) {
		t.Fatalf("toggle lost search or results: %v", pickerShape(state))
	}
	restored := newTestState(t, options)
	restored.openSnippetPicker()
	if !restored.snippets.pickerGrouped || !sameShape(pickerShape(restored), []string{"#ai", "#git"}) {
		t.Fatalf("restored grouping = %v", pickerShape(restored))
	}
	state.commandPalette.search = text.Editor{}
	state.Click(hitSnippetPickerGrouped)
	if state.snippets.pickerGrouped || !sameShape(pickerShape(state), []string{"Git", "AI"}) {
		t.Fatalf("flat toggle = %v", pickerShape(state))
	}
	restored = newTestState(t, options)
	if restored.snippets.pickerGrouped {
		t.Fatal("flat preference was not saved")
	}
}

func TestSnippetPickerGroupingCheckboxIsBesideBarToggle(t *testing.T) {
	state := openCommandsDrawerTest(t)
	state.openSnippetPicker()
	view := openPickerView(t, state, 1)
	bar, barOK := hitByIDSnapshot(view.Snapshot().Hits, hitCommandsBarToggle)
	group, groupOK := hitByIDSnapshot(view.Snapshot().Hits, hitSnippetPickerGrouped)
	if !barOK || !groupOK || group.Role != gfx.RoleCheckbox || group.Checked ||
		group.Bounds.Y != bar.Bounds.Y || group.Bounds.X < bar.Bounds.X+bar.Bounds.Width {
		t.Fatalf("checkbox layout: bar=%+v group=%+v", bar, group)
	}
	if err := view.Click(group.Bounds.X+group.Bounds.Width/2, group.Bounds.Y+group.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	group, groupOK = hitByIDSnapshot(view.Snapshot().Hits, hitSnippetPickerGrouped)
	if !groupOK || !group.Checked {
		t.Fatal("group checkbox did not toggle")
	}
}
