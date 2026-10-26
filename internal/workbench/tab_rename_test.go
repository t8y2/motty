package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func TestTabMenuRenamesTheTabAndKeepsTheNameAcrossRestarts(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID

	openTabMenu(state, workspace)
	state.Click(hitTabMenuRename)
	if state.modal != modalRenameTab {
		t.Fatalf("modal = %v, want the rename dialog", state.modal)
	}
	if got := state.tabRename.editor.String(); got != state.workspaceTabs[0].Title {
		t.Fatalf("the field starts with %q, want the current name %q", got, state.workspaceTabs[0].Title)
	}

	// Typing replaces the selected name; Enter submits.
	state.tabRename.editor.Insert("Build box")
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitTabRenameField})
	if state.modal != modalNone {
		t.Fatal("Enter did not close the dialog")
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

func TestTabRenameRefusesAnEmptyNameAndCancelKeepsTheOldOne(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	workspace := state.workspaceTabs[0].ID
	before := state.workspaceTabs[0].Title

	openTabMenu(state, workspace)
	state.Click(hitTabMenuRename)
	state.tabRename.editor.Insert("   ")
	state.Click(hitTabRenameSubmit)
	if state.modal != modalRenameTab || state.dialogError != state.messages.ErrTabNameRequired {
		t.Fatalf("an empty name was accepted: modal=%v error=%q", state.modal, state.dialogError)
	}
	state.Click(hitTabRenameCancel)
	if state.modal != modalNone || state.workspaceTabs[0].Title != before {
		t.Fatalf("cancel changed the tab: modal=%v title=%q", state.modal, state.workspaceTabs[0].Title)
	}
}

func openTabMenu(state *State, workspace layoutcore.WorkspaceID) {
	pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Target: hitTabList.At(int(workspace)), X: 100, Y: 18})
}
