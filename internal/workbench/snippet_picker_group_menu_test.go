package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func clickPickerGroupAction(t *testing.T, state *State, group string, action gfx.ID) {
	t.Helper()
	state.openSnippetPicker()
	for parent := snippetGroupParent(group); parent != ""; parent = snippetGroupParent(parent) {
		state.foldSnippetGroup(strings.ToLower(parent), true)
	}
	view := openPickerView(t, state, 1)
	row := -1
	for at, index := range state.commandPalette.visible {
		if state.commandPalette.entries[index].foldKey == strings.ToLower(group) {
			row = at
			break
		}
	}
	if row < 0 {
		t.Fatalf("group %q not visible", group)
	}
	bounds := dualHit(t, view, state.commandPalette.rowID(row))
	if err := view.RightClick(bounds.X+80, bounds.Y+bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if !state.snippets.folderMenu.open || !state.snippets.folderMenu.fromPicker || state.snippets.folderMenu.group != group {
		t.Fatalf("group menu: %+v", state.snippets.folderMenu)
	}
	labels := map[gfx.ID]string{
		hitSnippetFolderMenuAdd:      state.messages.Tools.SnippetFolderAdd,
		hitSnippetFolderMenuNewChild: state.messages.HostGroupActions.NewChild,
		hitSnippetFolderMenuRename:   state.messages.Tools.SnippetGroupRename,
		hitSnippetFolderMenuDelete:   state.messages.Tools.SnippetGroupDelete,
	}
	for _, label := range labels {
		dualHitLabel(t, view, label)
	}
	if err := view.ClickLabel(labels[action]); err != nil {
		t.Fatal(err)
	}
	if action == hitSnippetFolderMenuAdd && view.Snapshot().Focused != hitSnippetInlineName {
		t.Fatal("new snippet did not receive keyboard focus")
	}
}

func TestSnippetPickerGroupMenuAddsCommandToNestedGroup(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops/Linux"})
	clickPickerGroupAction(t, state, "Ops/Linux", hitSnippetFolderMenuAdd)
	if state.modal != modalNone || state.activeSection != sectionSnippets || !state.snippets.inline.draft || state.snippets.inline.loadedSnap.Group != "Ops/Linux" {
		t.Fatal("new snippet did not open in the selected group")
	}
	state.snippets.inline.name.Insert("Second")
	state.snippets.inline.command.Insert("two")
	state.applyInlineSnippetEdits()
	state.flushInlineSnippetSave()
	restored := newTestState(t, options)
	if len(restored.snippets.list) != 2 || restored.snippets.list[1].Group != "Ops/Linux" {
		t.Fatal("new snippet group was not persisted")
	}
}

func TestSnippetPickerGroupMenuCreatesVisibleEmptyChild(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops"})
	clickPickerGroupAction(t, state, "Ops", hitSnippetFolderMenuNewChild)
	if state.modal != modalSnippetGroupRename || state.snippetGroups.editor.String() != "Ops/" {
		t.Fatal("child group dialog did not use the parent path")
	}
	state.snippetGroups.editor = text.Editor{}
	state.snippetGroups.editor.Insert("Ops/Empty")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalCommandPalette || !sameShape(pickerShape(state), []string{"#Ops", "#Empty", "First"}) {
		t.Fatalf("new child not visible: %v", pickerShape(state))
	}
	restored := newTestState(t, options)
	if !sameShape(restored.snippets.customGroups, []string{"Ops/Empty"}) {
		t.Fatalf("saved groups: %v", restored.snippets.customGroups)
	}
}
