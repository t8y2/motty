package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// mixedSplitState opens a saved split tab and reconnects one of its two panes,
// leaving the other on its restored page.
func mixedSplitState(t *testing.T) (*State, *Session, *Session, gfx.Size) {
	t.Helper()
	world, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	openHostInSplit(t, first, 2, layoutcore.Horizontal)
	first.save()

	state := newTestState(t, options)
	panes := state.Panes(nil)
	if len(panes) != 2 {
		t.Fatalf("restored panes = %+v", panes)
	}
	restored, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !restored.restored {
		t.Fatal("setup: the restored tab does not rest on a page")
	}
	var otherPane Pane
	for _, pane := range panes {
		if uint32(pane.SessionID) != restored.ID {
			otherPane = pane
		}
	}
	live, ok := state.sessionByIDPointer(uint32(otherPane.SessionID))
	if !ok {
		t.Fatal("setup: the split has no other pane")
	}
	state.reconnect(live)
	liveConn := world.conns[len(world.conns)-1]
	liveConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	shell := liveConn.shell(t)
	shell.say("\x1b[0m")
	state.Wake()
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	return state, restored, live, size
}

// The shared rail's room belongs to the workspace, not to the pane with the
// keyboard. Moving focus between a live terminal and a restored pane beside it
// must keep both pane rectangles and the live PTY's geometry, instead of taking
// the rail's width away and resizing the terminal.
func TestMixedSplitKeepsTheRailRoomAcrossFocusChanges(t *testing.T) {
	state, restored, live, size := mixedSplitState(t)
	if !state.ActivatePane(restored.ID) {
		t.Fatal("activate the restored pane")
	}
	restoredBounds := paneBoundsOf(t, state, size, restored)
	liveBounds := paneBoundsOf(t, state, size, live)
	geometry := state.layoutForSession(live.ID)
	cols, rows := live.term.screen.Size()

	// The restored pane has no terminal of its own; the live pane beside it still
	// owns the rail, so its room stays reserved.
	if !state.ActivatePane(live.ID) {
		t.Fatal("activate the live pane")
	}
	if got := paneBoundsOf(t, state, size, restored); got != restoredBounds {
		t.Fatalf("the restored pane moved with focus: %+v -> %+v", restoredBounds, got)
	}
	if got := paneBoundsOf(t, state, size, live); got != liveBounds {
		t.Fatalf("the live pane moved with focus: %+v -> %+v", liveBounds, got)
	}
	if got := state.layoutForSession(live.ID); got != geometry {
		t.Fatalf("the live PTY was laid out again with focus: %+v -> %+v", geometry, got)
	}
	if gotCols, gotRows := live.term.screen.Size(); gotCols != cols || gotRows != rows {
		t.Fatalf("the live screen resized with focus: %dx%d -> %dx%d", cols, rows, gotCols, gotRows)
	}
}

// With the keyboard on the restored pane, the rail keeps the live pane's tools
// in their places, disabled, rather than emptying and leaving the room blank.
func TestMixedSplitKeepsRailToolsVisibleButDisabled(t *testing.T) {
	state, restored, live, size := mixedSplitState(t)
	record := func() gfx.Hit {
		t.Helper()
		var canvas gfx.Canvas
		state.Paint(&canvas, size)
		hit, ok := hitByID(&canvas, hitTerminalRecord)
		if !ok {
			t.Fatal("the rail's record tool is not painted")
		}
		return hit
	}

	if !state.ActivatePane(restored.ID) {
		t.Fatal("activate the restored pane")
	}
	if hit := record(); !hit.Disabled {
		t.Fatalf("the rail's tool is enabled over a restored pane: %+v", hit)
	}
	if !state.ActivatePane(live.ID) {
		t.Fatal("activate the live pane")
	}
	if hit := record(); hit.Disabled {
		t.Fatalf("the rail's tool is disabled over its own live pane: %+v", hit)
	}
}

// While a connection's shell is coming up, the rail keeps the room already
// reserved for it and shows the tools it will offer, disabled, instead of
// leaving a blank strip that later fills in.
func TestConnectingSessionKeepsRailToolsVisibleButDisabled(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	if len(state.sessions) != 1 {
		t.Fatalf("sessions = %d", len(state.sessions))
	}
	session := &state.sessions[0]
	if !state.showsProgress(session) {
		t.Fatal("setup: the connection is not in progress")
	}
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	hit, ok := hitByID(&canvas, hitTerminalRecord)
	if !ok {
		t.Fatal("the rail's record tool is not painted while the shell comes up")
	}
	if !hit.Disabled {
		t.Fatalf("the rail's tool is enabled before the shell exists: %+v", hit)
	}
}

// A workspace that rests entirely on restored pages has no terminal to own the
// shared rail, so it keeps the whole width for its panes.
func TestRestoredSplitKeepsTheWholeWidth(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	openHostInSplit(t, first, 2, layoutcore.Horizontal)
	first.save()

	state := newTestState(t, options)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	var right float32
	for _, pane := range state.Panes(nil) {
		session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
		if !ok || !session.restored {
			t.Fatalf("setup: pane %+v does not rest on a restored session", pane)
		}
		bounds := paneBoundsOf(t, state, size, session)
		right = max(right, bounds.X+bounds.Width)
	}
	if right != size.Width {
		t.Fatalf("restored panes end at %v, want the window's %v: a rail was reserved for no terminal", right, size.Width)
	}
}
