package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

func snippetGroupsManagerState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, freshOptions(t))
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "deploy", Command: "kubectl apply -f staging/", Group: "Ops", Run: true},
		snippet{Name: "logs", Command: "journalctl -fu app", Group: "Ops"},
		snippet{Name: "editor", Command: "code .", Group: "dev"},
	)
	return state
}

func snippetGroupAt(t *testing.T, state *State, name string) int {
	t.Helper()
	for index, group := range state.snippets.groups {
		if sameSnippetGroup(group, name) {
			return index
		}
	}
	t.Fatalf("no group %q in %v", name, state.snippets.groups)
	return -1
}

// Renaming a group rewrites the name on every command that carries it, leaves
// the other groups alone, and keeps a filter on the renamed group following it.
func TestRenamingASnippetGroupRenamesItsCommandsAndFilter(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "deploy", Command: "kubectl apply -f staging/", Group: "Ops", Run: true},
		snippet{Name: "logs", Command: "journalctl -fu app", Group: "Ops"},
		snippet{Name: "editor", Command: "code .", Group: "dev"},
	)
	state.snippets.filter = snippetFilter{group: "Ops"}
	state.refreshSnippets()

	state.Click(hitSnippetGroups)
	if state.modal != modalSnippetGroups {
		t.Fatalf("Manage groups opened modal %d", state.modal)
	}
	state.Click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Ops")))
	if state.modal != modalSnippetGroupRename || state.snippetGroups.editor.String() != "Ops" {
		t.Fatalf("rename dialog: modal %d, name %q", state.modal, state.snippetGroups.editor.String())
	}
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "Prod")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalSnippetGroups {
		t.Fatalf("saving the rename did not return to the manager: modal %d", state.modal)
	}
	if got := []string{state.snippets.list[0].Group, state.snippets.list[1].Group, state.snippets.list[2].Group}; !slices.Equal(got, []string{"Prod", "Prod", "dev"}) {
		t.Fatalf("groups after rename = %v", got)
	}
	if state.snippets.filter.group != "Prod" {
		t.Fatalf("filter = %q, want the renamed group", state.snippets.filter.group)
	}
	if !slices.Equal(state.snippets.groups, []string{"dev", "Prod"}) {
		t.Fatalf("groups = %v", state.snippets.groups)
	}
	if state.snippets.list[0].Command != "kubectl apply -f staging/" || !state.snippets.list[0].Run || state.snippets.list[1].Command != "journalctl -fu app" {
		t.Fatal("rename changed a command or its run setting")
	}
	if !strings.Contains(state.status, "Prod") {
		t.Fatalf("status %q does not mention the rename", state.status)
	}

	restored := newTestState(t, options)
	if got := []string{restored.snippets.list[0].Group, restored.snippets.list[1].Group, restored.snippets.list[2].Group}; !slices.Equal(got, []string{"Prod", "Prod", "dev"}) {
		t.Fatalf("restored groups = %v", got)
	}
}

// A name another group already has is refused, as is a name no group may carry,
// and neither changes the commands.
func TestRenamingASnippetGroupRejectsInvalidAndConflictingNames(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Ops")))

	for _, name := range []string{"", "   ", "dev", "DEV", strings.Repeat("g", maxSnippetGroup+1), "bad\x01name"} {
		state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, name)
		state.Click(hitSnippetGroupRenameSubmit)
		if state.modal != modalSnippetGroupRename || state.snippetGroups.error == "" {
			t.Fatalf("name %q: modal %d, error %q", name, state.modal, state.snippetGroups.error)
		}
		if got := []string{state.snippets.list[0].Group, state.snippets.list[1].Group, state.snippets.list[2].Group}; !slices.Equal(got, []string{"Ops", "Ops", "dev"}) {
			t.Fatalf("name %q changed groups to %v", name, got)
		}
	}
}

// A group's one spelling may change case without merging it into another group.
func TestRenamingASnippetGroupChangesOnlyItsCase(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Ops")))
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "ops")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalSnippetGroups || state.snippetGroups.error != "" {
		t.Fatalf("case-only rename: modal %d, error %q", state.modal, state.snippetGroups.error)
	}
	if state.snippets.list[0].Group != "ops" || state.snippets.list[1].Group != "ops" || state.snippets.list[2].Group != "dev" {
		t.Fatalf("groups = %v", []string{state.snippets.list[0].Group, state.snippets.list[1].Group, state.snippets.list[2].Group})
	}
	if !slices.Equal(state.snippets.groups, []string{"dev", "ops"}) {
		t.Fatalf("groups = %v", state.snippets.groups)
	}
}

// Deleting a group keeps its commands on the page as ungrouped ones, at the
// same place and in the same order, and a filter on it shows everything again.
func TestDeletingASnippetGroupKeepsItsCommandsUngrouped(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "deploy", Command: "kubectl apply -f staging/", Group: "Ops", Run: true},
		snippet{Name: "logs", Command: "journalctl -fu app", Group: "Ops"},
		snippet{Name: "editor", Command: "code .", Group: "dev"},
	)
	state.snippets.filter = snippetFilter{group: "Ops"}
	state.refreshSnippets()

	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupDeleteList.At(snippetGroupAt(t, state, "Ops")))
	if state.modal != modalSnippetGroupDelete {
		t.Fatalf("delete opened modal %d", state.modal)
	}
	_, texts := paintedTexts(state)
	if state.snippetGroupCount("Ops") != 2 || !contains(texts, "Ops") {
		t.Fatalf("the confirmation does not name the group and its count: %v", texts)
	}
	state.Click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalSnippetGroups {
		t.Fatalf("confirming the delete did not return to the manager: modal %d", state.modal)
	}
	if !slices.Equal(state.snippets.groups, []string{"dev"}) {
		t.Fatalf("groups after delete = %v", state.snippets.groups)
	}
	if !state.snippets.filter.all() {
		t.Fatalf("filter = %+v, want everything again", state.snippets.filter)
	}
	names := func() []string {
		return []string{state.snippets.list[0].Name, state.snippets.list[1].Name, state.snippets.list[2].Name}
	}
	if got := names(); !slices.Equal(got, []string{"deploy", "logs", "editor"}) {
		t.Fatalf("commands after delete = %v", got)
	}
	if state.snippets.list[0].Group != "" || state.snippets.list[1].Group != "" ||
		state.snippets.list[0].Command != "kubectl apply -f staging/" || !state.snippets.list[0].Run ||
		state.snippets.list[1].Command != "journalctl -fu app" {
		t.Fatalf("delete changed the commands: %+v", state.snippets.list)
	}
	if !strings.Contains(state.status, "Ops") {
		t.Fatalf("status %q does not mention the deleted group", state.status)
	}

	restored := newTestState(t, options)
	if restored.snippets.list[0].Group != "" || restored.snippets.list[1].Group != "" || restored.snippets.list[2].Group != "dev" {
		t.Fatalf("restored groups = %+v", restored.snippets.list)
	}
}

func TestCancellingSnippetGroupDeleteKeepsTheGroup(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupDeleteList.At(snippetGroupAt(t, state, "Ops")))
	state.Click(hitSnippetGroupDeleteCancel)
	if state.modal != modalSnippetGroups {
		t.Fatalf("cancelling did not return to the manager: modal %d", state.modal)
	}
	if state.snippets.list[0].Group != "Ops" || state.snippets.list[1].Group != "Ops" || !slices.Contains(state.snippets.groups, "Ops") {
		t.Fatal("cancelling deleted the group")
	}
}

func TestCancellingSnippetGroupRenameKeepsTheName(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Ops")))
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "Prod")
	state.Click(hitSnippetGroupRenameCancel)
	if state.modal != modalSnippetGroups || state.snippets.list[0].Group != "Ops" || state.snippets.list[1].Group != "Ops" {
		t.Fatal("cancelling renamed the group")
	}
}

// Opening Manage groups while the inline editor is open updates the editor's
// group field when a group is renamed or deleted while preserving unsaved text.
func TestSnippetGroupManagerFromTheEditorReturnsToTheEditor(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.focusInlineSnippet(1) // "logs", in Ops
	state.snippets.inline.name.Insert(" updated")
	state.applyInlineSnippetEdits()
	state.Click(hitSnippetGroups)
	if state.modal != modalSnippetGroups {
		t.Fatalf("Manage groups opened modal %d", state.modal)
	}
	state.Click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Ops")))
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "Prod")
	state.Click(hitSnippetGroupRenameSubmit)
	state.Click(hitSnippetGroupsDone)
	if state.modal != modalNone {
		t.Fatalf("closing the manager left modal %d", state.modal)
	}
	if state.snippets.inline.groupPicker.Value() != "Prod" {
		t.Fatalf("the editor's group field = %q, want the renamed group", state.snippets.inline.groupPicker.Value())
	}
	if state.snippets.inline.name.String() != "logs updated" {
		t.Fatalf("the editor lost its edited name: %q", state.snippets.inline.name.String())
	}
	if state.snippets.list[1].Group != "Prod" || state.snippets.list[1].Name != "logs updated" {
		t.Fatalf("the edited snippet was not saved into the renamed group: %+v", state.snippets.list[1])
	}
}

func TestSnippetGroupManagerFromTheEditorClearsADeletedGroup(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.focusInlineSnippet(0) // "deploy", in Ops
	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupDeleteList.At(snippetGroupAt(t, state, "Ops")))
	state.Click(hitSnippetGroupDeleteConfirm)
	state.Click(hitSnippetGroupsDone)
	if state.modal != modalNone {
		t.Fatalf("closing the manager left modal %d", state.modal)
	}
	if state.snippets.inline.groupPicker.Value() != "" {
		t.Fatalf("the editor's group field = %q, want none", state.snippets.inline.groupPicker.Value())
	}
	if state.snippets.list[0].Group != "" || state.snippets.list[0].Name != "deploy" {
		t.Fatalf("the edited snippet was not saved as ungrouped: %+v", state.snippets.list[0])
	}
}

// Coming back from the manager also refreshes the group list the inline editor
// offers: a group deleted while the manager was open is gone from the choices,
// while an unfinished group name the user typed is kept.
func TestSnippetGroupManagerFromTheEditorRefreshesItsChoices(t *testing.T) {
	state := snippetGroupsManagerState(t)
	state.focusInlineSnippet(0) // "deploy", in Ops
	state.snippets.inline.groupPicker.Query.Insert("new-group")
	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupDeleteList.At(snippetGroupAt(t, state, "dev")))
	state.Click(hitSnippetGroupDeleteConfirm)
	state.Click(hitSnippetGroupsDone)
	if state.modal != modalNone {
		t.Fatalf("closing the manager left modal %d", state.modal)
	}
	for _, option := range state.snippets.inline.groupPicker.Options {
		if option.Value == "dev" {
			t.Fatalf("the deleted group is still offered: %+v", state.snippets.inline.groupPicker.Options)
		}
	}
	if state.snippets.inline.groupPicker.Value() != "Ops" || state.snippets.inline.groupPicker.Query.String() != "new-group" {
		t.Fatalf("value %q, query %q: the editor lost its place or its typed group",
			state.snippets.inline.groupPicker.Value(), state.snippets.inline.groupPicker.Query.String())
	}
}

func TestRenamingANestedSnippetGroupRemapsItsSubtreeAndRejectsCycles(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "prod", Command: "echo prod", Group: "Clients/Acme/Prod"},
		snippet{Name: "stage", Command: "echo stage", Group: "Clients/Acme/Staging"},
		snippet{Name: "beta", Command: "echo beta", Group: "Clients/Beta"},
		snippet{Name: "ops", Command: "echo ops", Group: "Ops"},
	)
	state.snippets.filter = snippetFilter{group: "Clients/Acme/Prod"}
	state.refreshSnippets()

	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Clients")))
	// Reject moving a group inside itself.
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "Clients/Sub")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalSnippetGroupRename || state.snippetGroups.error == "" {
		t.Fatalf("self-nesting rename was accepted: modal=%d err=%q", state.modal, state.snippetGroups.error)
	}

	// Rename the parent group: every descendant and the active filter remap.
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "Customers")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalSnippetGroups || state.snippetGroups.error != "" {
		t.Fatalf("rename failed: modal=%d err=%q", state.modal, state.snippetGroups.error)
	}
	got := []string{
		state.snippets.list[0].Group,
		state.snippets.list[1].Group,
		state.snippets.list[2].Group,
		state.snippets.list[3].Group,
	}
	want := []string{"Customers/Acme/Prod", "Customers/Acme/Staging", "Customers/Beta", "Ops"}
	if !slices.Equal(got, want) {
		t.Fatalf("groups after rename = %v, want %v", got, want)
	}
	if state.snippets.filter.group != "Customers/Acme/Prod" {
		t.Fatalf("filter = %q, want Customers/Acme/Prod", state.snippets.filter.group)
	}
}

func TestDeletingANestedSnippetGroupUngroupsItsSubtree(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "prod", Command: "echo prod", Group: "Clients/Acme/Prod"},
		snippet{Name: "stage", Command: "echo stage", Group: "Clients/Acme/Staging"},
		snippet{Name: "beta", Command: "echo beta", Group: "Clients/Beta"},
	)
	state.snippets.filter = snippetFilter{group: "Clients/Acme/Prod"}
	state.refreshSnippets()

	state.Click(hitSnippetGroups)
	state.Click(hitSnippetGroupDeleteList.At(snippetGroupAt(t, state, "Clients/Acme")))
	if state.snippetGroupCount("Clients/Acme") != 2 {
		t.Fatalf("subtree count = %d, want 2", state.snippetGroupCount("Clients/Acme"))
	}
	state.Click(hitSnippetGroupDeleteConfirm)
	got := []string{state.snippets.list[0].Group, state.snippets.list[1].Group, state.snippets.list[2].Group}
	if !slices.Equal(got, []string{"", "", "Clients/Beta"}) {
		t.Fatalf("groups after subtree delete = %v", got)
	}
	if !state.snippets.filter.all() {
		t.Fatalf("filter = %+v, want reset to all", state.snippets.filter)
	}
}

// Deleting a group with the "delete all commands" choice selected removes the
// group, its subgroups, and every command beneath it, while leaving other
// groups intact. Opening the confirmation defaults to keeping commands.
func TestDeletingASnippetGroupWithSnippetsRemovesAllSubtreeCommands(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "deploy", Command: "kubectl apply -f staging/", Group: "Ops", Run: true},
		snippet{Name: "web", Command: "nginx -t", Group: "Ops/Web"},
		snippet{Name: "editor", Command: "code .", Group: "dev"},
	)

	state.snippets.folderMenu = snippetFolderMenu{open: true, group: "Ops", at: gfx.Point{X: 100, Y: 100}}
	state.chooseSnippetFolderMenu(hitSnippetFolderMenuDelete)
	if state.modal != modalSnippetGroupDelete || state.snippetGroups.deleteSnippets {
		t.Fatalf("delete group opened modal=%d deleteSnippets=%v, want default false", state.modal, state.snippetGroups.deleteSnippets)
	}
	state.Click(hitSnippetGroupDeletePurge)
	if !state.snippetGroups.deleteSnippets {
		t.Fatal("selecting purge radio did not set deleteSnippets")
	}
	state.Click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalNone {
		t.Fatalf("confirm left modal %d", state.modal)
	}
	if len(state.snippets.list) != 1 || state.snippets.list[0].Name != "editor" {
		t.Fatalf("remaining snippets = %+v, want only editor", state.snippets.list)
	}
	if !slices.Equal(state.snippets.groups, []string{"dev"}) {
		t.Fatalf("groups after delete with snippets = %v, want [dev]", state.snippets.groups)
	}
	restored := newTestState(t, options)
	if len(restored.snippets.list) != 1 || restored.snippets.list[0].Name != "editor" {
		t.Fatalf("restored snippets = %+v, want only editor", restored.snippets.list)
	}
}

// Dragging a grouped snippet before/after its top-level folder or choosing
// "Remove from group" from its context menu ungroups it without leaving behind
// an implicit empty group.
func TestMovingSnippetToRootViaDropAndContextMenu(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "only", Command: "echo only", Group: "Ops"},
		snippet{Name: "devcmd", Command: "echo dev", Group: "Dev/Sub"},
	)

	// Tree rows: Dev(0), Sub(1), devcmd(2), Ops(3), only(4).
	if state.snippetTreeCanDrop(4, 3, ui.TreeDropInside) {
		t.Fatal("dropping snippet inside its own group should be rejected")
	}
	if !state.snippetTreeCanDrop(4, 3, ui.TreeDropBefore) {
		t.Fatal("dropping snippet before its top-level folder should ungroup it")
	}
	state.dropSnippetTreeRow(ui.TreeMove{From: 4, To: 3, Where: ui.TreeDropBefore})
	if state.snippets.list[0].Group != "" {
		t.Fatalf("snippet group after root drop = %q, want empty", state.snippets.list[0].Group)
	}
	// Dropping an ungrouped snippet before a top-level folder is a no-op and rejected.
	devRow := -1
	onlyRow := -1
	for i, row := range state.snippets.treeRows {
		if row.folder && sameSnippetGroup(row.group, "Dev") {
			devRow = i
		}
		if !row.folder && row.snippet == 0 {
			onlyRow = i
		}
	}
	if devRow < 0 || onlyRow < 0 {
		t.Fatalf("missing rows dev=%d only=%d in %+v", devRow, onlyRow, state.snippets.treeRows)
	}
	if state.snippetTreeCanDrop(onlyRow, devRow, ui.TreeDropBefore) {
		t.Fatal("dropping an ungrouped snippet before a top-level folder should be rejected")
	}
	// Existing empty-group semantics: an implicit group vanishes once its last
	// command moves away.
	if slices.Contains(state.snippets.groups, "Ops") {
		t.Fatalf("implicit emptied group Ops remained: groups=%v", state.snippets.groups)
	}

	// Remove a nested snippet from its group via its context menu.
	state.snippetMenu = snippetMenu{open: true, index: 1, name: "devcmd", at: gfx.Point{X: 100, Y: 100}}
	state.chooseSnippetMenu(hitSnippetMenuRemoveGroup)
	if state.snippets.list[1].Group != "" {
		t.Fatalf("snippet group after remove from group = %q, want empty", state.snippets.list[1].Group)
	}
	if len(state.snippets.groups) != 0 {
		t.Fatalf("groups after ungrouping last snippet = %v, want empty", state.snippets.groups)
	}
	restored := newTestState(t, options)
	if restored.snippets.list[0].Group != "" || restored.snippets.list[1].Group != "" {
		t.Fatalf("restored groups = %+v, want both ungrouped", restored.snippets.list)
	}
}

// Creating a subgroup keeps the parent path in the Parent selector rather than
// prefilling slashes in the Group name field, and renaming a nested group edits
// only its leaf name while preserving its parent path.
func TestSnippetGroupDialogSeparatesParentFromLeafName(t *testing.T) {
	state := newTestState(t, freshOptions(t))
	state.activeSection = sectionSnippets
	addTestSnippets(state, snippet{Name: "cmd", Command: "echo hi", Group: "Ops/Prod/API"})

	// Creating a child under Ops/Prod/API prefills parent, leaves name empty.
	state.openSnippetGroupCreate("Ops/Prod/API")
	if state.snippetGroups.parent != "Ops/Prod/API" || state.snippetGroups.editor.String() != "" {
		t.Fatalf("create child state: parent=%q editor=%q", state.snippetGroups.parent, state.snippetGroups.editor.String())
	}
	// Slashes inside the leaf field are rejected while a parent is selected.
	state.snippetGroups.editor.Insert("bad/leaf")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalSnippetGroupRename || state.snippetGroups.error == "" {
		t.Fatalf("slash in child leaf was accepted: modal=%d err=%q", state.modal, state.snippetGroups.error)
	}
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "logs")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalNone || !slices.Contains(state.snippets.groups, "Ops/Prod/API/logs") {
		t.Fatalf("creating child failed: modal=%d err=%q groups=%v", state.modal, state.snippetGroups.error, state.snippets.groups)
	}

	// Renaming a nested group prefills only the leaf name and keeps its parent.
	state.openSnippetGroupRenameDirect("Ops/Prod/API")
	if state.snippetGroups.parent != "Ops/Prod" || state.snippetGroups.editor.String() != "API" {
		t.Fatalf("rename nested state: parent=%q editor=%q", state.snippetGroups.parent, state.snippetGroups.editor.String())
	}
	state.snippetGroups.editor.ReplaceRange(text.Range{Start: 0, End: state.snippetGroups.editor.RuneCount()}, "Edge")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalNone || state.snippets.list[0].Group != "Ops/Prod/Edge" {
		t.Fatalf("rename nested failed: modal=%d err=%q group=%q", state.modal, state.snippetGroups.error, state.snippets.list[0].Group)
	}
}
