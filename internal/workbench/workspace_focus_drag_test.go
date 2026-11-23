package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
	"reflect"
	"testing"
)

func TestWorkspaceFocusTabDragReordersAndPersists(t *testing.T) {
	state, _, _, _ := liveSession(t)
	for len(state.sessions) < 3 {
		state.duplicateTerminal(terminalTestHitByID(state, state.activeSessionID))
	}
	workspace := state.deck.ActiveWorkspace()
	original := state.deck.Panes(workspace, nil)
	layout, _ := state.deck.Layout(workspace)
	if err := state.deck.SetRatio(workspace, layout.Root(), .37); err != nil {
		t.Fatal(err)
	}
	shape := layout.Spec()
	active := state.activeSessionID
	state.togglePaneMaximized(0)
	driver := paneDriver(t, state, gfx.Size{Width: 1400, Height: 900})
	drag := func(from, to layoutcore.PaneID, after bool) {
		t.Helper()
		source := hitOf(t, driver, hitWorkspaceFocusTab.At(int(from))).Bounds
		target := hitOf(t, driver, hitWorkspaceFocusTab.At(int(to))).Bounds
		x := target.X + 3
		if after {
			x = target.X + target.Width - 3
		}
		if err := driver.Press(source.X+source.Width/2, source.Y+source.Height/2, gfx.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
		if err := driver.Move(x, target.Y+target.Height/2); err != nil {
			t.Fatal(err)
		}
		if !state.focusTabs.drag.active {
			t.Fatal("drag did not start")
		}
		if err := driver.Release(x, target.Y+target.Height/2, gfx.PointerButtonPrimary); err != nil {
			t.Fatal(err)
		}
	}
	drag(original[0].ID, original[2].ID, true)
	want := []layoutcore.Pane{original[1], original[2], original[0]}
	if got := state.deck.Panes(workspace, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("order: %+v", got)
	}
	if !state.deck.PaneMaximized(workspace) || state.activeSessionID != active || !layout.Spec().Equal(shape) {
		t.Fatalf("maximized=%v active=%d want=%d shape=%+v want=%+v", state.deck.PaneMaximized(workspace), state.activeSessionID, active, layout.Spec(), shape)
	}
	restored := newTestState(t, state.options)
	if got := restored.deck.Panes(workspace, nil); !reflect.DeepEqual(got, want) || !restored.deck.PaneMaximized(workspace) {
		t.Fatalf("restart lost order or mode: %+v", got)
	}
	drag(original[0].ID, original[1].ID, false)
	if got := state.deck.Panes(workspace, nil); !reflect.DeepEqual(got, original) {
		t.Fatalf("reverse order: %+v", got)
	}
	state.togglePaneMaximized(0)
	for index, pane := range state.Panes(nil) {
		if pane.ID != original[index].ID || pane.Bounds.Width <= 0 || pane.Bounds.Height <= 0 {
			t.Fatalf("tiled pane: %+v", pane)
		}
	}
}

func TestWorkspaceFocusTabClickStillSelects(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	state.togglePaneMaximized(0)
	driver := paneDriver(t, state, gfx.Size{Width: 1200, Height: 800})
	panes := state.deck.Panes(state.deck.ActiveWorkspace(), nil)
	hit := hitOf(t, driver, hitWorkspaceFocusTab.At(int(panes[0].ID)))
	if err := driver.Click(hit.Bounds.X+20, hit.Bounds.Y+10); err != nil {
		t.Fatal(err)
	}
	if state.activeSessionID != uint32(panes[0].SessionID) || state.focusTabs.drag.active {
		t.Fatal("click did not select terminal")
	}
}
