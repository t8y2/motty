package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

// Every pane in a split carries its close control on its own bar, wherever the
// keyboard is. Closing from the bar removes the pane the bar names and leaves
// the keyboard where it was; a shell that is still running asks first.
func TestSplitPaneBarClosesThePaneItNames(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	first, second := &state.sessions[0], &state.sessions[1]
	// The new split is the active one; press the first pane's grid to put the
	// keyboard back there, as a user would.
	clickPaneGrid(t, state, driver, first)
	if state.activeSessionID != first.ID {
		t.Fatalf("the first pane did not become active: %d", state.activeSessionID)
	}

	// Both bars show the control at rest, with the pointer over neither of them.
	for _, session := range []*Session{first, second} {
		pane := paneBoundsOf(t, state, size, session)
		hit := hitOf(t, driver, hitPaneBarCloseList.At(int(session.ID)))
		if bar := (gfx.Rect{X: pane.X, Y: pane.Y, Width: pane.Width, Height: infoBarHeight}); !inside(hit.Bounds, bar) {
			t.Fatalf("session %d's close control %+v is outside its bar %+v", session.ID, hit.Bounds, bar)
		}
	}

	secondHit := hitOf(t, driver, hitPaneBarCloseList.At(int(second.ID)))
	if err := driver.Click(secondHit.Bounds.X+secondHit.Bounds.Width/2, secondHit.Bounds.Y+secondHit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	// The second shell still runs, so the close asks before ending it.
	confirm := hitOf(t, driver, hitTabsCloseConfirm)
	if state.modal != modalCloseTabs || len(state.sessions) != 2 {
		t.Fatalf("closing a running pane went ahead: modal=%v sessions=%d", state.modal, len(state.sessions))
	}
	if err := driver.Click(confirm.Bounds.X+confirm.Bounds.Width/2, confirm.Bounds.Y+confirm.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if len(state.sessions) != 1 || state.sessions[0].ID != first.ID {
		t.Fatalf("after closing the second pane the sessions are %+v, want only %d", state.sessions, first.ID)
	}
	if state.activeSessionID != first.ID {
		t.Fatalf("closing a pane the keyboard was not in moved the keyboard to %d, want %d", state.activeSessionID, first.ID)
	}
}
