//go:build windows || darwin || linux

package workbench

import (
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"github.com/vivaui/viva/text"
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

	treeRect := state.sectionBodies.snippets.tree.Rect()
	secondary := gfx.PointerEvent{
		Kind:   gfx.PointerDown,
		Button: gfx.PointerButtonSecondary,
		Target: hitSnippetTreeScroll,
		X:      treeRect.X + treeRect.Width/2,
		Y:      treeRect.Y + treeRect.Height - 40,
	}
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

	// Deleting the empty group through the folder confirmation dialog removes it cleanly.
	state.openSnippetGroupDeleteDirect("Ops")
	if state.modal != modalSnippetGroupDelete || state.snippetGroups.target != "Ops" {
		t.Fatalf("delete empty group opened modal=%d target=%q", state.modal, state.snippetGroups.target)
	}
	state.Click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalNone || len(state.snippets.groups) != 0 {
		t.Fatalf("after confirm: modal=%d groups=%v", state.modal, state.snippets.groups)
	}
}

func TestSnippetsTreeFolderDeleteConfirmation(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{
		{Name: "Restart", Group: "Ops/Web", Command: "systemctl restart nginx"},
		{Name: "Status", Group: "Ops", Command: "uptime"},
	}
	state.refreshSnippets()
	state.save()
	state.activeSection = sectionSnippets

	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)

	// Open the folder menu for "Ops" and choose Delete.
	state.snippets.folderMenu = snippetFolderMenu{open: true, group: "Ops", at: gfx.Point{X: 100, Y: 100}}
	state.chooseSnippetFolderMenu(hitSnippetFolderMenuDelete)
	if state.modal != modalSnippetGroupDelete || state.snippetGroups.target != "Ops" {
		t.Fatalf("folder delete opened modal=%d target=%q", state.modal, state.snippetGroups.target)
	}

	// Canceling keeps the group and its snippets intact.
	state.Click(hitSnippetGroupDeleteCancel)
	if state.modal != modalNone {
		t.Fatalf("cancel left modal=%d", state.modal)
	}
	if state.snippets.list[0].Group != "Ops/Web" || state.snippets.list[1].Group != "Ops" {
		t.Fatalf("cancel modified groups: %+v", state.snippets.list)
	}

	// Confirming ungroups the subtree, closes back to the page, and persists.
	state.snippets.folderMenu = snippetFolderMenu{open: true, group: "Ops", at: gfx.Point{X: 100, Y: 100}}
	state.chooseSnippetFolderMenu(hitSnippetFolderMenuDelete)
	state.Click(hitSnippetGroupDeleteConfirm)
	if state.modal != modalNone {
		t.Fatalf("confirm left modal=%d, want modalNone", state.modal)
	}
	if len(state.snippets.groups) != 0 || state.snippets.list[0].Group != "" || state.snippets.list[1].Group != "" {
		t.Fatalf("after delete confirm: groups=%v list=%+v", state.snippets.groups, state.snippets.list)
	}
	if reloaded := newTestState(t, options); len(reloaded.snippets.groups) != 0 || reloaded.snippets.list[0].Group != "" {
		t.Fatalf("reloaded after delete confirm: groups=%v list=%+v", reloaded.snippets.groups, reloaded.snippets.list)
	}
}

func TestSnippetsTreeAltArrowReordersSiblingsAndFollowsFocus(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.snippets.list = []snippet{
		{Name: "First", Group: "Ops", Command: "echo 1"},
		{Name: "Other", Group: "Dev", Command: "echo other"},
		{Name: "Second", Group: "Ops", Command: "echo 2"},
	}
	state.refreshSnippets()
	state.save()
	state.activeSection = sectionSnippets

	var canvas gfx.Canvas
	paintSnippetsPage(state, &canvas)

	// Find the tree row for "Second" (snippet index 2, inside Ops).
	secondRow := -1
	for i, row := range state.snippets.treeRows {
		if !row.folder && row.snippet == 2 {
			secondRow = i
			break
		}
	}
	if secondRow < 0 {
		t.Fatalf("Second row not found in %+v", state.snippets.treeRows)
	}

	// Alt+Up swaps Second with its Ops sibling First (skipping Dev's Other) and
	// keeps keyboard focus on the moved row.
	res := state.Key(gfx.KeyEvent{
		Target:    state.snippets.tree.RowID(secondRow),
		Key:       gfx.KeyUp,
		Modifiers: gfx.ModifierAlt,
	})
	if !res.Handled || !res.Repaint {
		t.Fatalf("Alt+Up result = %+v", res)
	}
	if state.snippets.list[0].Name != "Second" || state.snippets.list[1].Name != "Other" || state.snippets.list[2].Name != "First" {
		t.Fatalf("order after Alt+Up = %+v", state.snippets.list)
	}
	if wantFocus := state.snippetTreeRowIDForSnippet(0); res.Focus != wantFocus {
		t.Fatalf("focus after Alt+Up = %v, want %v", res.Focus, wantFocus)
	}

	// Alt+Down moves it back down using the returned focus target.
	res = state.Key(gfx.KeyEvent{
		Target:    res.Focus,
		Key:       gfx.KeyDown,
		Modifiers: gfx.ModifierAlt,
	})
	if !res.Handled || state.snippets.list[0].Name != "First" || state.snippets.list[2].Name != "Second" {
		t.Fatalf("after Alt+Down: res=%+v list=%+v", res, state.snippets.list)
	}

	// Persists across reload.
	res = state.Key(gfx.KeyEvent{
		Target:    res.Focus,
		Key:       gfx.KeyUp,
		Modifiers: gfx.ModifierAlt,
	})
	if !res.Handled {
		t.Fatalf("second Alt+Up not handled")
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[0].Name != "Second" || reloaded.snippets.list[2].Name != "First" {
		t.Fatalf("reloaded order after Alt+Up = %+v", reloaded.snippets.list)
	}
}

func TestSnippetsInlineEditorCoalescesSavesAndFlushesOnBlurAndSwitch(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.snippets.inline.saveDelay = 15 * time.Millisecond
	state.snippets.list = []snippet{
		{Name: "First", Group: "Ops", Command: "echo one", Run: true},
		{Name: "Second", Group: "Ops", Command: "echo two", Run: true},
	}
	state.refreshSnippets()
	state.save()
	state.activeSection = sectionSnippets

	var canvas gfx.Canvas
	canvas.Reset()
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, hitSnippetInlineName, false)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})

	// Rapid keystrokes update memory immediately without writing to disk on each key.
	for _, suffix := range []string{" a", "b", "c"} {
		state.snippets.inline.name.Insert(suffix)
		state.applyInlineSnippetEdits()
	}
	if got := state.snippets.list[0].Name; got != "First abc" {
		t.Fatalf("in-memory name = %q, want %q", got, "First abc")
	}
	if !state.snippets.inline.dirty || state.snippets.inline.saveTimer == nil {
		t.Fatalf("expected dirty inline state with pending timer, got dirty=%v timer=%v", state.snippets.inline.dirty, state.snippets.inline.saveTimer)
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[0].Name != "First" {
		t.Fatalf("inline edit wrote to store mid-typing: got %q, want %q", reloaded.snippets.list[0].Name, "First")
	}

	// Blurring the inline editor schedules a flush outside Paint.
	canvas.Reset()
	canvas.SetInteraction(gfx.ID{}, gfx.ID{}, gfx.ID{}, false)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	state.Wake()
	if state.snippets.inline.dirty || state.snippets.inline.saveTimer != nil {
		t.Fatalf("blur did not flush inline save: dirty=%v timer=%v", state.snippets.inline.dirty, state.snippets.inline.saveTimer)
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[0].Name != "First abc" {
		t.Fatalf("reloaded name after blur = %q, want %q", reloaded.snippets.list[0].Name, "First abc")
	}

	// Switching selection in the tree flushes pending edits before loading the next snippet.
	state.snippets.inline.command.Insert(" && true")
	state.applyInlineSnippetEdits()
	if !state.snippets.inline.dirty {
		t.Fatal("expected dirty state after command edit")
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[0].Command != "echo one" {
		t.Fatalf("command edit wrote before flush: got %q", reloaded.snippets.list[0].Command)
	}
	secondRow := -1
	for i, row := range state.snippets.treeRows {
		if !row.folder && row.snippet == 1 {
			secondRow = i
			break
		}
	}
	if secondRow < 0 {
		t.Fatalf("second snippet row not found in %+v", state.snippets.treeRows)
	}
	rowID := state.snippets.tree.RowID(secondRow)
	canvas.Reset()
	canvas.BeginEventPass(rowID, gfx.ClickInfo{})
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	canvas.EndEventPass()
	if state.snippets.selected != 1 {
		t.Fatalf("clicking second row left selected=%d, want 1", state.snippets.selected)
	}
	if state.snippets.inline.dirty || state.snippets.inline.saveTimer != nil {
		t.Fatalf("selection switch did not flush inline save: dirty=%v timer=%v", state.snippets.inline.dirty, state.snippets.inline.saveTimer)
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[0].Command != "echo one && true" {
		t.Fatalf("reloaded command after switch = %q, want %q", reloaded.snippets.list[0].Command, "echo one && true")
	}

	// Settling without blur or selection switch writes once via the injected debounce timer.
	state.snippets.inline.name.Insert(" settled")
	state.applyInlineSnippetEdits()
	if reloaded := newTestState(t, options); reloaded.snippets.list[1].Name != "Second" {
		t.Fatalf("second snippet wrote before debounce settled: got %q", reloaded.snippets.list[1].Name)
	}
	deadline := time.Now().Add(2 * time.Second)
	for state.snippets.inline.saveTimer != nil {
		if time.Now().After(deadline) {
			t.Fatal("coalesced inline snippet save timer did not fire")
		}
		state.Wake()
		time.Sleep(5 * time.Millisecond)
	}
	if state.snippets.inline.dirty {
		t.Fatal("dirty flag remained set after coalesced save")
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[1].Name != "Second settled" {
		t.Fatalf("reloaded name after debounce = %q, want %q", reloaded.snippets.list[1].Name, "Second settled")
	}
}

func TestSnippetsInlineEditorPreservesInvalidDraftAcrossSwitchAndLeave(t *testing.T) {
	options := freshOptions(t)
	state := newTestState(t, options)
	state.snippets.inline.saveDelay = time.Hour
	state.snippets.list = []snippet{
		{Name: "First", Group: "Ops", Command: "echo one", Run: true},
		{Name: "Second", Group: "Ops", Command: "echo two", Run: true},
	}
	state.refreshSnippets()
	state.save()
	state.Click(hitSnippets)

	var canvas gfx.Canvas
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		canvas.RunAfterPaint()
	}
	clickRow := func(snippetIndex int) {
		t.Helper()
		rowIdx := -1
		for i, row := range state.snippets.treeRows {
			if !row.folder && row.snippet == snippetIndex {
				rowIdx = i
				break
			}
		}
		if rowIdx < 0 {
			t.Fatalf("snippet %d row not found", snippetIndex)
		}
		canvas.Reset()
		canvas.BeginEventPass(state.snippets.tree.RowID(rowIdx), gfx.ClickInfo{})
		state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
		canvas.EndEventPass()
		canvas.RunAfterPaint()
	}

	paint()

	// Clear the name of the first snippet so validation fails, then switch away
	// to the second snippet and back: the unvalidated edit and error must stay.
	state.snippets.inline.name = text.Editor{}
	state.snippets.inline.command = text.Editor{}
	state.snippets.inline.command.Insert("echo edited-but-unnamed")
	state.applyInlineSnippetEdits()
	if state.snippets.inline.error != state.messages.Tools.SnippetErrorName {
		t.Fatalf("error = %q, want %q", state.snippets.inline.error, state.messages.Tools.SnippetErrorName)
	}
	clickRow(1)
	if state.snippets.selected != 1 || state.snippets.inline.name.String() != "Second" || state.snippets.inline.error != "" {
		t.Fatalf("second snippet state = selected:%d name:%q err:%q", state.snippets.selected, state.snippets.inline.name.String(), state.snippets.inline.error)
	}
	clickRow(0)
	if state.snippets.inline.name.String() != "" || state.snippets.inline.command.String() != "echo edited-but-unnamed" || state.snippets.inline.error != state.messages.Tools.SnippetErrorName {
		t.Fatalf("switching back lost unvalidated draft: name=%q cmd=%q err=%q",
			state.snippets.inline.name.String(), state.snippets.inline.command.String(), state.snippets.inline.error)
	}

	// Leaving the Snippets page and returning must keep the unvalidated draft.
	state.Click(hitHosts)
	paint()
	state.Click(hitSnippets)
	paint()
	if state.snippets.inline.name.String() != "" || state.snippets.inline.command.String() != "echo edited-but-unnamed" || state.snippets.inline.error != state.messages.Tools.SnippetErrorName {
		t.Fatalf("leaving section lost unvalidated draft: name=%q cmd=%q err=%q",
			state.snippets.inline.name.String(), state.snippets.inline.command.String(), state.snippets.inline.error)
	}

	// Fixing the name clears the draft error, and leaving the section flushes
	// the pending save at the section-switch entry point without Paint.
	state.snippets.inline.name.Insert("First fixed")
	state.applyInlineSnippetEdits()
	if state.snippets.inline.error != "" || !state.snippets.inline.dirty {
		t.Fatalf("after fix: error=%q dirty=%v", state.snippets.inline.error, state.snippets.inline.dirty)
	}
	state.Click(hitHosts)
	if state.snippets.inline.dirty {
		t.Fatal("leaving Snippets section did not flush pending inline save")
	}
	if reloaded := newTestState(t, options); reloaded.snippets.list[0].Name != "First fixed" || reloaded.snippets.list[0].Command != "echo edited-but-unnamed" {
		t.Fatalf("reloaded snippet after section leave = %+v", reloaded.snippets.list[0])
	}

	// New-snippet draft survives leaving the section and switching to another
	// snippet, and is only cleared when explicitly cancelled.
	state.Click(hitSnippets)
	state.Click(hitSnippetAdd)
	state.snippets.inline.name.Insert("Draft Name")
	state.applyInlineSnippetEdits()
	if !state.snippets.inline.draft || state.snippets.inline.error != state.messages.Tools.SnippetErrorCommand {
		t.Fatalf("new draft state: draft=%v err=%q", state.snippets.inline.draft, state.snippets.inline.error)
	}
	state.Click(hitHosts)
	paint()
	state.Click(hitSnippets)
	paint()
	if !state.snippets.inline.draft || state.snippets.inline.name.String() != "Draft Name" || state.snippets.inline.error != state.messages.Tools.SnippetErrorCommand {
		t.Fatalf("leaving section lost new draft: draft=%v name=%q err=%q",
			state.snippets.inline.draft, state.snippets.inline.name.String(), state.snippets.inline.error)
	}
	clickRow(1)
	if state.snippets.inline.draft {
		t.Fatal("clicking an existing snippet should leave draft mode")
	}
	state.Click(hitSnippetAdd)
	if !state.snippets.inline.draft || state.snippets.inline.name.String() != "Draft Name" {
		t.Fatalf("re-opening new draft did not restore stashed draft: draft=%v name=%q",
			state.snippets.inline.draft, state.snippets.inline.name.String())
	}
	state.Click(hitSnippetRemove)
	if state.snippets.inline.draft || state.snippets.inline.savedDraft != nil {
		t.Fatalf("cancelling new draft left draft=%v savedDraft=%v", state.snippets.inline.draft, state.snippets.inline.savedDraft)
	}
}
