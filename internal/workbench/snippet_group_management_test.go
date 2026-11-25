package workbench

import (
	"slices"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
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
