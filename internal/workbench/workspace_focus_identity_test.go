package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
	"reflect"
	"testing"
)

func requireWorkspaceHeaderIdentity(t *testing.T, state *State, managed bool) {
	t.Helper()
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	_, header := hitByID(canvas, hitWorkspaceHeaderLayout)
	if state.workspaceManaged(state.deck.ActiveWorkspace()) != managed || header != managed {
		t.Fatalf("workspace identity changed: managed=%v header=%v want=%v", state.workspaceManaged(state.deck.ActiveWorkspace()), header, managed)
	}
}

func TestWorkspaceFocusDragKeepsHeaderIdentityAcrossRestart(t *testing.T) {
	for _, managed := range []bool{false, true} {
		state, _, _ := twoLivePanes(t)
		openHostInSplit(t, state, 1, layoutcore.Horizontal)
		openHostInSplit(t, state, 2, layoutcore.Vertical)
		workspace := state.deck.ActiveWorkspace()
		if managed {
			layout, _ := state.deck.Layout(workspace)
			state.managedWorkspaceLayouts[workspace] = layout.Spec()
		}
		state.togglePaneMaximized(0)
		requireWorkspaceHeaderIdentity(t, state, managed)
		before := state.workspacePixelBounds(state.viewport)
		panes := state.deck.Panes(workspace, nil)
		driver := paneDriver(t, state, state.viewport)
		from := hitOf(t, driver, hitWorkspaceFocusTab.At(int(panes[3].ID))).Bounds
		to := hitOf(t, driver, hitWorkspaceFocusTab.At(int(panes[2].ID))).Bounds
		if err := driver.Press(from.X+40, from.Y+12, gfx.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
		if err := driver.Move(to.X+3, to.Y+12); err != nil {
			t.Fatal(err)
		}
		if err := driver.Release(to.X+3, to.Y+12, gfx.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
		requireWorkspaceHeaderIdentity(t, state, managed)
		if state.workspacePixelBounds(state.viewport) != before {
			t.Fatal("reorder moved terminal content")
		}
		want := []layoutcore.Pane{panes[0], panes[1], panes[3], panes[2]}
		if !reflect.DeepEqual(state.deck.Panes(workspace, nil), want) {
			t.Fatal("order did not change")
		}
		restored := newTestState(t, state.options)
		requireWorkspaceHeaderIdentity(t, restored, managed)
		if !reflect.DeepEqual(restored.deck.Panes(workspace, nil), want) || !restored.deck.PaneMaximized(workspace) {
			t.Fatal("restart lost order or maximization")
		}
	}
}

func TestDuplicateTerminalKeepsHeaderIdentityAcrossRestart(t *testing.T) {
	for _, managed := range []bool{false, true} {
		state, _, _ := twoLivePanes(t)
		workspace := state.deck.ActiveWorkspace()
		if managed {
			layout, _ := state.deck.Layout(workspace)
			state.managedWorkspaceLayouts[workspace] = layout.Spec()
		}
		state.togglePaneMaximized(0)
		state.duplicateTerminal(terminalTestHitByID(state, state.activeSessionID))
		if len(state.deck.Panes(workspace, nil)) != 3 {
			t.Fatal("duplicate did not create a terminal")
		}
		requireWorkspaceHeaderIdentity(t, state, managed)
		restored := newTestState(t, state.options)
		requireWorkspaceHeaderIdentity(t, restored, managed)
	}
}
