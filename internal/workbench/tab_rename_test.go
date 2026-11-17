package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func TestTabMenuRenamesTheTabInPlaceAndKeepsTheNameAcrossRestarts(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	openTabMenu(state, workspace)
	state.Click(hitTabMenuRename)
	if !state.tabInlineActive() || state.tabInline.workspace != workspace {
		t.Fatalf("the name field is not open on the tab: %+v", state.tabInline)
	}
	if got := state.tabInline.editor.String(); got != state.workspaceTabs[0].Title {
		t.Fatalf("the field starts with %q, want the current name %q", got, state.workspaceTabs[0].Title)
	}
	if sel := state.tabInline.editor.Selection(); sel.Start != 0 || sel.End != state.tabInline.editor.RuneCount() {
		t.Fatalf("the current name is not selected: %+v", sel)
	}

	// Typing replaces the selected name; Enter applies it on the spot.
	state.tabInline.editor.Insert("Build box")
	state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitTabInline})
	if state.tabInlineActive() {
		t.Fatal("Enter did not close the field")
	}
	if got := state.workspaceTabs[0].Title; got != "Build box" {
		t.Fatalf("tab title = %q", got)
	}
	if got := state.tabOptions[0].Label; got != "Build box" {
		t.Fatalf("painted tab label = %q", got)
	}
	if _, texts := paintedTexts(state); !contains(texts, "Build box") {
		t.Fatalf("the new name is not painted: %v", texts)
	}

	restored := newTestState(t, state.options)
	if got := restored.workspaceTabs[0].Title; got != "Build box" {
		t.Fatalf("after a restart the tab is %q", got)
	}
}

func TestTabRenameEmptyNameKeepsTheOldOne(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID
	before := state.workspaceTabs[0].Title

	openTabMenu(state, workspace)
	state.Click(hitTabMenuRename)
	state.tabInline.editor.Insert("   ")
	state.commitTabInline()
	if state.tabInlineActive() {
		t.Fatal("an emptied name left the field open")
	}
	if state.workspaceTabs[0].Title != before {
		t.Fatalf("an emptied name changed the tab: %q, want %q", state.workspaceTabs[0].Title, before)
	}
}

func openTabMenu(state *State, workspace layoutcore.WorkspaceID) {
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitTabList.At(int(workspace)), X: 100, Y: 18})
}
