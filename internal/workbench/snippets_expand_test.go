//go:build windows || darwin || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/ui"
)

func snippetTestState(t *testing.T, snippets ...snippet) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.snippets.list = snippets
	state.refreshSnippets()
	state.activeSection = sectionSnippets
	return state
}

func paintSnippetsPage(state *State, canvas *gfx.Canvas) {
	canvas.Reset()
	state.Paint(canvas, gfx.Size{Width: 1400, Height: 900})
}

func hasTextValue(canvas *gfx.Canvas, value string) bool {
	for _, text := range canvas.Texts() {
		if text.Value == value {
			return true
		}
	}
	return false
}

func TestSnippetsTreeBuildsHierarchyAndCollapsesFolders(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Restart nginx", Group: "Prod/Web", Command: "systemctl restart nginx"},
		snippet{Name: "Deploy API", Group: "Prod", Command: "kubectl apply -f api.yaml"},
		snippet{Name: "Disk usage", Command: "df -h"},
	)
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)

	// Expected pre-order: Prod (0), Web (1), Restart nginx (2), Deploy API (1), Disk usage (0)
	if len(state.snippets.treeRows) != 5 {
		t.Fatalf("treeRows = %+v, want 5 rows", state.snippets.treeRows)
	}
	wantLabels := []string{"Prod", "Web", "Restart nginx", "Deploy API", "Disk usage"}
	wantDepths := []int{0, 1, 2, 1, 0}
	for i := range wantLabels {
		if got := state.snippets.treeRows[i]; got.label != wantLabels[i] || got.depth != wantDepths[i] {
			t.Fatalf("row %d = %+v, want label=%q depth=%d", i, got, wantLabels[i], wantDepths[i])
		}
	}

	// Collapsing Prod hides its nested subfolder and snippets, leaving Prod + Disk usage.
	state.toggleSnippetTreeFolder("prod")
	if len(state.snippets.treeRows) != 2 {
		t.Fatalf("after collapse treeRows = %+v, want 2 rows", state.snippets.treeRows)
	}
	if state.snippets.treeRows[0].label != "Prod" || state.snippets.treeRows[1].label != "Disk usage" {
		t.Fatalf("collapsed rows = %+v", state.snippets.treeRows)
	}

	// Expand-all restores the whole hierarchy.
	state.Click(hitSnippetTreeExpandAll)
	if len(state.snippets.treeRows) != 5 {
		t.Fatalf("after expand all treeRows = %+v, want 5 rows", state.snippets.treeRows)
	}
}

func TestSnippetsTreeInlineEditorUpdatesAndPersists(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{
		{Name: "First", Group: "Ops", Command: "echo one", Run: true},
		{Name: "Second", Group: "Dev", Command: "echo two"},
	}
	state.refreshSnippets()
	state.activeSection = sectionSnippets
	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)

	if state.snippets.inline.name.String() != "First" || state.snippets.inline.command.String() != "echo one" {
		t.Fatalf("inline editor not loaded: name=%q cmd=%q", state.snippets.inline.name.String(), state.snippets.inline.command.String())
	}

	// Edit name, command, run, lines, and shortcut inline.
	state.snippets.inline.name.Insert(" updated")
	state.snippets.inline.command.Insert("\necho next")
	state.applyInlineSnippetEdits()
	state.Click(hitSnippetInlineRun)
	state.Click(hitSnippetInlineLines)
	chord := keymap.Chord{Modifiers: gfx.ModifierMeta | gfx.ModifierShift, Char: '9'}
	state.Click(hitSnippetInlineShortcut)
	pressChord(state, chord)

	got := state.snippets.list[0]
	if got.Name != "First updated" || got.Command != "echo one\necho next" || got.Run || !got.Lines || got.Shortcut != chord {
		t.Fatalf("inline edits did not update snippet: %+v", got)
	}

	reloaded := newTestState(t, options)
	if len(reloaded.snippets.list) != 2 || reloaded.snippets.list[0].Name != "First updated" || !reloaded.snippets.list[0].Lines || reloaded.snippets.list[0].Shortcut != chord {
		t.Fatalf("reloaded snippets = %+v", reloaded.snippets.list)
	}
}

func TestSnippetsTreeDragMovesSnippetsAndFolders(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Nginx", Group: "Prod", Command: "nginx -t"},
		snippet{Name: "App", Group: "Dev", Command: "go run ."},
		snippet{Name: "Root", Command: "ls"},
	)
	// Order: Dev(0), App(1), Prod(2), Nginx(3), Root(4)
	if len(state.snippets.treeRows) != 5 {
		t.Fatalf("treeRows = %+v", state.snippets.treeRows)
	}
	// Drag Root snippet (row 4) into Prod folder (row 2).
	if !state.snippetTreeCanDrop(4, 2, ui.TreeDropInside) {
		t.Fatal("dropping snippet into folder should be allowed")
	}
	state.dropSnippetTreeRow(ui.TreeMove{From: 4, To: 2, Where: ui.TreeDropInside})
	if !sameSnippetGroup(state.snippets.list[2].Group, "Prod") {
		t.Fatalf("Root group after drop = %q, want Prod", state.snippets.list[2].Group)
	}

	// Drag Dev folder (row 0) inside Prod folder (row 2).
	if !state.snippetTreeCanDrop(0, 2, ui.TreeDropInside) {
		t.Fatal("dropping Dev folder into Prod folder should be allowed")
	}
	state.dropSnippetTreeRow(ui.TreeMove{From: 0, To: 2, Where: ui.TreeDropInside})
	if !sameSnippetGroup(state.snippets.list[1].Group, "Prod/Dev") {
		t.Fatalf("App group after folder move = %q, want Prod/Dev", state.snippets.list[1].Group)
	}
}

func TestSnippetsTreeSearchFiltersAndExpandsMatches(t *testing.T) {
	state := snippetTestState(t,
		snippet{Name: "Nginx", Group: "Prod/Web", Command: "systemctl reload nginx"},
		snippet{Name: "Postgres", Group: "Prod/DB", Command: "pg_isready"},
	)
	state.toggleSnippetTreeFolder("prod")
	if len(state.snippets.treeRows) != 1 {
		t.Fatalf("collapsed rows = %d, want 1", len(state.snippets.treeRows))
	}
	state.Click(hitSnippetTreeSearchToggle)
	state.snippets.treeSearch.Insert("nginx")
	state.rebuildSnippetTreeRows()
	if len(state.snippets.treeRows) != 3 {
		t.Fatalf("search treeRows = %+v, want Prod -> Web -> Nginx", state.snippets.treeRows)
	}
	if state.snippets.treeRows[2].label != "Nginx" {
		t.Fatalf("matched leaf = %+v", state.snippets.treeRows[2])
	}
}

func TestSnippetsTreeBlankRightClickCreatesPersistedEmptyGroup(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{{Name: "Disk", Command: "df -h"}}
	state.refreshSnippets()
	state.activeSection = sectionSnippets

	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)

	secondary := gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitSnippetTreeScroll, X: 80, Y: 400}
	if got := pointer(state, secondary); !got.Handled || !state.snippets.blankMenu.open {
		t.Fatalf("blank right-click did not open menu: handled=%v menu=%+v", got.Handled, state.snippets.blankMenu)
	}
	chooseMenu(t, state, hitSnippetBlankNewGroup)
	if state.modal != modalSnippetGroupRename || !state.snippetGroups.creating {
		t.Fatalf("new group opened modal=%d creating=%v", state.modal, state.snippetGroups.creating)
	}
	state.snippetGroups.editor.Insert("Ops/Deploy")
	state.Click(hitSnippetGroupRenameSubmit)
	if state.modal != modalNone {
		t.Fatalf("create group failed: modal=%d error=%q", state.modal, state.snippetGroups.error)
	}
	if len(state.snippets.treeRows) != 3 || state.snippets.treeRows[0].label != "Ops" || state.snippets.treeRows[1].label != "Deploy" {
		t.Fatalf("treeRows after creating empty group = %+v", state.snippets.treeRows)
	}

	reloaded := newTestState(t, options)
	if len(reloaded.snippets.groups) != 2 || reloaded.snippets.groups[0] != "Ops" || reloaded.snippets.groups[1] != "Ops/Deploy" {
		t.Fatalf("reloaded groups = %v", reloaded.snippets.groups)
	}

	// Deleting the empty group removes it cleanly.
	state.deleteSnippetGroup("Ops")
	if len(state.snippets.groups) != 0 {
		t.Fatalf("groups after delete = %v", state.snippets.groups)
	}
}
