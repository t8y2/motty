package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
)

func TestSnippetPickerGroupMenuRenamesExpandedHierarchy(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops/Linux"})
	state.openSnippetPicker()
	state.foldSnippetGroup("ops", true)
	state.foldSnippetGroup("ops/linux", true)
	clickPickerGroupAction(t, state, "Ops", hitSnippetFolderMenuRename)
	if state.modal != modalSnippetGroupRename || state.snippetGroups.target != "Ops" {
		t.Fatal("rename did not target the selected group")
	}
	state.snippetGroups.editor = text.Editor{}
	state.snippetGroups.editor.Insert("Tools")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalCommandPalette || state.commandPalette.returnToPicker ||
		!sameShape(pickerShape(state), []string{"#Tools", "#Linux", "First"}) {
		t.Fatalf("renamed picker: %v", pickerShape(state))
	}
	restored := newTestState(t, options)
	if restored.snippets.list[0].Group != "Tools/Linux" {
		t.Fatal("renamed descendant was not persisted")
	}
}

func TestSnippetPickerGroupMenuDeletesGroupAndKeepsCommands(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops/Linux"})
	state.openSnippetPicker()
	state.foldSnippetGroup("ops", true)
	clickPickerGroupAction(t, state, "Ops", hitSnippetFolderMenuDelete)
	if state.modal != modalSnippetGroupDelete {
		t.Fatal("group delete did not ask for confirmation")
	}
	state.Click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalCommandPalette || state.commandPalette.returnToPicker ||
		!sameShape(pickerShape(state), []string{"First"}) || state.commandPalette.groupExpanded("ops") {
		t.Fatalf("picker after group deletion: %v", pickerShape(state))
	}
	restored := newTestState(t, options)
	if len(restored.snippets.list) != 1 || restored.snippets.list[0].Group != "" || restored.snippets.list[0].Command != "one" {
		t.Fatal("group deletion did not preserve the command as ungrouped")
	}
}

func TestSnippetPickerGroupMenuCancelReturnsToPicker(t *testing.T) {
	for _, action := range []struct {
		name   string
		create bool
	}{
		{"create", true}, {"rename", false}, {"delete", false},
	} {
		t.Run(action.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			addTestSnippets(state, snippet{Name: "First", Command: "one", Group: "Ops"})
			id := hitSnippetFolderMenuRename
			if action.create {
				id = hitSnippetFolderMenuNewChild
			}
			cancel := hitSnippetGroupRenameCancel
			if action.name == "delete" {
				id, cancel = hitSnippetFolderMenuDelete, hitSnippetGroupDeleteCancel
			}
			clickPickerGroupAction(t, state, "Ops", id)
			state.Click(cancel)
			if state.modal != modalCommandPalette || state.commandPalette.returnToPicker || state.snippets.list[0].Group != "Ops" {
				t.Fatal("cancel did not return to the unchanged picker")
			}
		})
	}
}
