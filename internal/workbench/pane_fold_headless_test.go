package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// rectInside reports whether inner lies inside outer.
func rectInside(inner, outer gfx.Rect) bool {
	return inner.X >= outer.X && inner.Y >= outer.Y &&
		inner.X+inner.Width <= outer.X+outer.Width+0.5 &&
		inner.Y+inner.Height <= outer.Y+outer.Height+0.5
}

// Two panes waiting on their connections are two bodies of the same workbench
// page. Each pane paints its own controls; pressing the other pane's control
// makes that pane the active one and then runs the control.
func TestSplitConnectionPanesKeepTheirOwnControls(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	first := connect(t, state, world)
	if !state.splitActivePane(layoutcore.Horizontal) {
		t.Fatal("split right")
	}
	state.chooseHostSection()
	state.Click(hitHostList.At(int(state.hosts[1].ID)))
	openHost(state)
	second := world.conns[len(world.conns)-1]

	// Both connections fail, so a split keeps both progress pages up.
	first.set(func(s *sshconn.Status) { s.Phase, s.Err = sshconn.PhaseFailed, sshconn.ErrAuthFailed })
	second.set(func(s *sshconn.Status) { s.Phase, s.Err = sshconn.PhaseFailed, sshconn.ErrAuthFailed })
	state.Wake()

	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	panes := state.Panes(nil)
	if len(panes) != 2 {
		t.Fatalf("panes = %+v", panes)
	}
	firstSession, ok := state.sessionByIDPointer(panes[0].SessionID)
	if !ok {
		t.Fatal("the first pane has no session")
	}
	secondSession, ok := state.sessionByIDPointer(panes[1].SessionID)
	if !ok {
		t.Fatal("the second pane has no session")
	}
	if !state.showsProgress(firstSession) || !state.showsProgress(secondSession) {
		t.Fatal("the split dropped a connection page")
	}

	// Each pane names its own copy of the footer: no page shares a hit with
	// another.
	closeID := func(session *Session) gfx.ID {
		return tree.MountID(paneMount(session), hitConnClose)
	}
	if closeID(firstSession) == closeID(secondSession) {
		t.Fatal("two panes share one close button")
	}
	for _, session := range []*Session{firstSession, secondSession} {
		hit := hitOf(t, driver, closeID(session))
		pane := paneBoundsOf(t, state, size, session)
		if !rectInside(hit.Bounds, pane) {
			t.Fatalf("session %d: close button %+v is outside its pane %+v", session.ID, hit.Bounds, pane)
		}
	}

	// Pressing the inactive pane's close button switches to that pane and then
	// closes it, the way the window delivers a click.
	close := hitOf(t, driver, closeID(secondSession))
	centerX, centerY := close.Bounds.X+close.Bounds.Width/2, close.Bounds.Y+close.Bounds.Height/2
	if got, ok := driver.HitTest(centerX, centerY); !ok || got != closeID(secondSession) {
		t.Fatalf("the close button's centre hits %s (%d), %v, want %s (%d)", tree.Text(got), got, ok, tree.Text(closeID(secondSession)), closeID(secondSession))
	}
	if err := driver.Click(centerX, centerY); err != nil {
		t.Fatal(err)
	}
	if state.activeSessionID != firstSession.ID {
		t.Fatalf("active session = %d, want %d", state.activeSessionID, firstSession.ID)
	}
	if _, ok := state.sessionByIDPointer(secondSession.ID); ok {
		t.Fatal("the second pane's close button did not close it")
	}
}

// A restored pane's body stays inside its pane when the window is resized, and
// each pane keeps its own button.
func TestRestoredPanesFollowResize(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	openHost(first)
	openHostInSplit(t, first, 2, layoutcore.Horizontal)
	first.save()

	state := newTestState(t, options)
	for _, size := range []gfx.Size{{Width: 1400, Height: 900}, {Width: 720, Height: 520}} {
		var canvas gfx.Canvas
		state.Resize(size)
		state.Paint(&canvas, size)
		panes := state.Panes(nil)
		if len(panes) != 2 {
			t.Fatalf("%v: panes = %+v", size, panes)
		}
		seen := map[gfx.ID]bool{}
		for _, pane := range panes {
			session, ok := state.sessionByIDPointer(pane.SessionID)
			if !ok {
				t.Fatalf("%v: pane %d has no session", size, pane.ID)
			}
			if !session.restored {
				t.Fatalf("%v: session %d is not the restored body", size, session.ID)
			}
			id := state.paneIdentity(session, hitConnClose)
			if seen[id] {
				t.Fatalf("%v: two restored panes share %d", size, id)
			}
			seen[id] = true
			hit, ok := hitByID(&canvas, id)
			if !ok {
				t.Fatalf("%v: session %d has no Connect button", size, session.ID)
			}
			bounds := pixelBounds(state.workspacePixelBounds(size), pane.Bounds)
			if !rectInside(hit.Bounds, bounds) {
				t.Fatalf("%v: session %d button %+v left its pane %+v", size, session.ID, hit.Bounds, bounds)
			}
		}
	}
}

// A remote desktop is a body of the workbench page too: in a split its picture
// is the pane's own, pressing it makes that pane active and focuses the
// desktop, and a resize moves the picture with the pane.
func TestSplitDesktopPanesPaintAndFocus(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	state.sessions[0].term.live = true
	state.sessions[1].term.live = true
	state.sessions[0].desk = newDesktopView(newFollowingDesktop(800, 600, false), false)
	state.sessions[1].desk = newDesktopView(newFollowingDesktop(800, 600, false), false)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)

	first, second := &state.sessions[0], &state.sessions[1]
	if state.activeSessionID != first.ID {
		if !state.ActivatePane(first.ID) {
			t.Fatal("activate the first pane")
		}
	}
	for _, session := range []*Session{first, second} {
		hit := hitOf(t, driver, hitTerminalList.At(int(session.ID)))
		bounds := paneBoundsOf(t, state, size, session)
		if !rectInside(hit.Bounds, bounds) {
			t.Fatalf("session %d: picture %+v is outside its pane %+v", session.ID, hit.Bounds, bounds)
		}
		area := state.desktopGeometry(session).area
		if !rectInside(hit.Bounds, area) {
			t.Fatalf("session %d: picture %+v is outside the page area %+v", session.ID, hit.Bounds, area)
		}
	}

	// Pressing the second pane's picture takes the keyboard into that desktop.
	picture := hitOf(t, driver, hitTerminalList.At(int(second.ID)))
	if err := driver.Click(picture.Bounds.X+picture.Bounds.Width/2, picture.Bounds.Y+picture.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if state.activeSessionID != second.ID {
		t.Fatalf("active session = %d, want %d", state.activeSessionID, second.ID)
	}
	if focused := driver.Snapshot().Focused; focused != hitTerminalList.At(int(second.ID)) {
		t.Fatalf("focused = %d, want the second desktop %d", focused, second.ID)
	}

	// The picture follows a resize without leaving the pane.
	smaller := gfx.Size{Width: 960, Height: 620}
	state.Resize(smaller)
	driver = paneDriver(t, state, smaller)
	for _, session := range []*Session{first, second} {
		hit := hitOf(t, driver, hitTerminalList.At(int(session.ID)))
		bounds := paneBoundsOf(t, state, smaller, session)
		if !rectInside(hit.Bounds, bounds) {
			t.Fatalf("after resize: session %d picture %+v left its pane %+v", session.ID, hit.Bounds, bounds)
		}
	}
}
