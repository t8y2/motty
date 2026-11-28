//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// The manager is a real page dialog: the button beside the filter opens it, a
// row's pencil renames the group through the native field, and its trash asks
// before the commands become ungrouped. Nothing here talks to the state
// directly but the clicks and keys a user would make.
func TestSnippetGroupsManagerNativeRenameAndDelete(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.Chinese)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "deploy", Command: "kubectl apply -f staging/", Group: "Ops", Run: true},
		snippet{Name: "logs", Command: "journalctl -fu app", Group: "Ops"},
		snippet{Name: "editor", Command: "code .", Group: "dev"},
	)

	view, err := headless.Open(app.Config{
		Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click := func(id gfx.ID) {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("missing hit %s; frame has %v", tree.Text(id), view.Texts())
	}
	m := state.messages.Tools
	clean := func(where string) {
		t.Helper()
		if problems := headless.CheckLayout(view.Snapshot()); len(problems) > 0 {
			t.Fatalf("%s: %v", where, problems)
		}
	}

	click(hitSnippetGroups)
	if state.modal != modalSnippetGroups {
		t.Fatalf("Manage groups opened modal %d", state.modal)
	}
	texts := strings.Join(view.Texts(), "\n")
	if !strings.Contains(texts, m.SnippetGroupsManage) || !strings.Contains(texts, fmt.Sprintf(m.SnippetGroupCountFmt, 2)) {
		t.Fatalf("the manager does not name itself or count the group's commands: %v", view.Texts())
	}

	clean("group manager")
	click(hitSnippetGroupRenameList.At(snippetGroupAt(t, state, "Ops")))
	if state.modal != modalSnippetGroupRename {
		t.Fatalf("the pencil opened modal %d", state.modal)
	}
	if err := view.Type("Prod"); err != nil {
		t.Fatal(err)
	}
	if err := view.Keys("Enter"); err != nil {
		t.Fatal(err)
	}
	clean("group manager after rename")
	if state.modal != modalSnippetGroups || state.snippetGroups.error != "" {
		t.Fatalf("native rename: modal %d, error %q", state.modal, state.snippetGroups.error)
	}
	if !sameSnippetGroup(state.snippets.list[0].Group, "Prod") || !sameSnippetGroup(state.snippets.list[1].Group, "Prod") {
		t.Fatalf("native rename left %+v", state.snippets.list)
	}

	click(hitSnippetGroupDeleteList.At(snippetGroupAt(t, state, "dev")))
	if state.modal != modalSnippetGroupDelete {
		t.Fatalf("the trash opened modal %d", state.modal)
	}
	clean("delete group confirmation")
	click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalSnippetGroups {
		t.Fatalf("confirming did not return to the manager: modal %d", state.modal)
	}
	if strings.Contains(strings.Join(view.Texts(), "\n"), "dev") {
		t.Fatalf("the deleted group is still listed: %v", view.Texts())
	}
	if state.snippets.list[2].Group != "" || state.snippets.list[2].Command != "code ." {
		t.Fatalf("delete changed the command: %+v", state.snippets.list[2])
	}

	clean("group manager after delete")
	click(hitSnippetGroupsDone)
	if state.modal != modalNone {
		t.Fatalf("closing the manager left modal %d", state.modal)
	}
	if got := strings.Join(view.Texts(), "\n"); !strings.Contains(got, "editor") || !strings.Contains(got, "Prod") {
		t.Fatalf("the page does not show the renamed and ungrouped commands: %v", view.Texts())
	}
}

// Right-clicking a folder offers a single "Delete group" action whose
// confirmation lets the user choose between keeping commands ungrouped
// (default) or deleting them with the group, and right-clicking a grouped
// command offers "Remove from group".
func TestSnippetFolderDeleteWithSnippetsAndMoveToRootHeadless(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.setLanguage(i18n.Chinese)
	state.activeSection = sectionSnippets
	addTestSnippets(state,
		snippet{Name: "deploy", Command: "kubectl apply -f staging/", Group: "Ops", Run: true},
		snippet{Name: "web", Command: "nginx -t", Group: "Ops/Web"},
		snippet{Name: "editor", Command: "code .", Group: "dev"},
	)

	view, err := headless.Open(app.Config{
		Width: 1200, Height: 800, Tree: Tree(), Paint: state.Paint,
		OnKey: state.Key, OnPointer: state.Pointer, OnScroll: state.Scroll,
		TextInput: func(c app.TextInputContext) *app.TextInput {
			if editor := state.EditorFor(c.ID); editor != nil {
				return &app.TextInput{Editor: editor}
			}
			return nil
		},
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	click := func(id gfx.ID) {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				if err := view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("missing hit %s; frame has %v", tree.Text(id), view.Texts())
	}
	clickTopLabel := func(label string) {
		t.Helper()
		hits := view.Snapshot().Hits
		for i := len(hits) - 1; i >= 0; i-- {
			if hits[i].Label == label {
				if err := view.Click(hits[i].Bounds.X+hits[i].Bounds.Width/2, hits[i].Bounds.Y+hits[i].Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("missing menu item %q; frame has %v", label, view.Texts())
	}
	rightClick := func(id gfx.ID) {
		t.Helper()
		for _, hit := range view.Snapshot().Hits {
			if hit.ID == id {
				if err := view.RightClick(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
					t.Fatal(err)
				}
				return
			}
		}
		t.Fatalf("missing hit %s; frame has %v", tree.Text(id), view.Texts())
	}
	clean := func(where string) {
		t.Helper()
		if problems := headless.CheckLayout(view.Snapshot()); len(problems) > 0 {
			t.Fatalf("%s: %v", where, problems)
		}
	}
	m := state.messages.Tools

	// Right-click Ops folder -> Delete group -> switch radio to delete all commands -> Confirm.
	opsRow := -1
	for i, row := range state.snippets.treeRows {
		if row.folder && sameSnippetGroup(row.group, "Ops") {
			opsRow = i
			break
		}
	}
	if opsRow < 0 {
		t.Fatalf("Ops folder row not found in %+v", state.snippets.treeRows)
	}
	rightClick(state.snippets.tree.RowID(opsRow))
	clean("folder context menu")
	clickTopLabel(m.SnippetGroupDelete)
	if state.modal != modalSnippetGroupDelete || state.snippetGroups.deleteSnippets {
		t.Fatalf("delete group opened modal=%d deleteSnippets=%v, want default false", state.modal, state.snippetGroups.deleteSnippets)
	}
	clean("delete group confirmation default")
	if texts := strings.Join(view.Texts(), "\n"); !strings.Contains(texts, m.SnippetGroupDeleteKeep) || !strings.Contains(texts, m.SnippetGroupDeletePurge) {
		t.Fatalf("confirmation missing radio choices: %v", view.Texts())
	}
	click(hitSnippetGroupDeletePurge)
	if !state.snippetGroups.deleteSnippets {
		t.Fatal("clicking purge radio did not select deleteSnippets")
	}
	clean("delete group confirmation purge")
	click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalNone || len(state.snippets.list) != 1 || state.snippets.list[0].Name != "editor" {
		t.Fatalf("after delete with snippets: modal=%d list=%+v", state.modal, state.snippets.list)
	}

	// Right-click remaining grouped command "editor" -> Remove from group.
	editorRow := -1
	for i, row := range state.snippets.treeRows {
		if !row.folder && row.snippet == 0 {
			editorRow = i
			break
		}
	}
	if editorRow < 0 {
		t.Fatalf("editor row not found in %+v", state.snippets.treeRows)
	}
	rightClick(state.snippets.tree.RowID(editorRow))
	clean("snippet context menu")
	clickTopLabel(state.messages.MoveGroupNone)
	if state.snippets.list[0].Group != "" {
		t.Fatalf("editor group after remove from group = %q, want empty", state.snippets.list[0].Group)
	}
	clean("snippets page after remove from group")
}
