package workbench

import (
	"github.com/vivaui/viva/app"
	"sync/atomic"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// twoWindows makes two windows of one group, side by side on a pretend screen:
// the first covers x 0..1399, the second x 1500..2899. wakes counts each one's
// invalidations.
type twoWindows struct {
	first, second   *State
	firstW, secondW *world
	firstWakes      atomic.Int32
	secondWakes     atomic.Int32
	torn            []*State
}

func newTwoWindows(t *testing.T) *twoWindows {
	t.Helper()
	pair := &twoWindows{}
	group := NewGroup()
	make1 := func(wakes *atomic.Int32, x float32) (*State, *world) {
		world, options := newWorld(t)
		options.Group = group
		options.Invalidate = func() { wakes.Add(1) }
		options.TearOff = func(origin ui.Point, adopt func(*State)) error {
			_, fresh := newWorld(t)
			fresh.Group, fresh.Blank = group, true
			fresh.Invalidate = func() {}
			window := newTestState(t, fresh)
			pair.torn = append(pair.torn, window)
			window.SetWindow(func() (ui.Rect, bool) { return ui.Rect{X: origin.X, Y: origin.Y, Width: 1400, Height: 900}, true }, func() bool { return true })
			adopt(window)
			return nil
		}
		state := newTestState(t, options)
		state.SetWindow(func() (ui.Rect, bool) { return ui.Rect{X: x, Width: 1400, Height: 900}, true }, func() bool { return true })
		return state, world
	}
	pair.first, pair.firstW = make1(&pair.firstWakes, 0)
	pair.second, pair.secondW = make1(&pair.secondWakes, 1500)
	return pair
}

// liveTab opens a host in state and brings its shell up.
func liveTab(t *testing.T, state *State, w *world, host int) *fakeConn {
	t.Helper()
	state.activeSection = sectionHosts
	state.selectedHostID = state.hosts[host].ID
	fake := connect(t, state, w)
	fake.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.ServerVersion = "SSH-2.0-Test"
		s.Method = "password"
	})
	state.Wake()
	fake.shell(t).say("\x1b[0m")
	state.Wake()
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	return fake
}

// dragAcross presses on a tab of from and drags it to the screen point, with the
// window coordinates a platform would report once the pointer has left.
func dragAcross(from *State, id layoutcore.WorkspaceID, screen ui.Point, windowOrigin ui.Point) {
	target := hitTabList.At(int(id))
	start := tabCenter(from, id)
	from.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: target, X: start.X, Y: start.Y, ScreenX: windowOrigin.X + start.X, ScreenY: windowOrigin.Y + start.Y, Viewport: from.viewport, ClickCount: 1})
	for step := 1; step <= 4; step++ {
		fraction := float32(step) / 4
		x := start.X + (screen.X-windowOrigin.X-start.X)*fraction
		y := start.Y + (screen.Y-windowOrigin.Y-start.Y)*fraction
		from.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Target: target, Captured: true, X: x, Y: y, ScreenX: windowOrigin.X + x, ScreenY: windowOrigin.Y + y, Viewport: from.viewport})
	}
	end := ui.Point{X: screen.X - windowOrigin.X, Y: screen.Y - windowOrigin.Y}
	from.Pointer(ui.PointerEvent{Kind: ui.PointerUp, Button: ui.PointerButtonPrimary, Target: target, Captured: true, X: end.X, Y: end.Y, ScreenX: screen.X, ScreenY: screen.Y, Viewport: from.viewport, ClickCount: 1})
}

func TestATabDraggedOntoAnotherWindowMovesWithItsLiveSession(t *testing.T) {
	pair := newTwoWindows(t)
	live := liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]
	oldSession := pair.first.deck.Panes(moving, nil)[0].SessionID

	// Over the second window's middle: not a pane edge, so it becomes a tab there.
	dragAcross(pair.first, moving, ui.Point{X: 2200, Y: 450}, ui.Point{})

	if len(tabOrder(pair.first)) != 1 || len(pair.first.sessions) != 1 {
		t.Fatalf("the first window kept %d tabs and %d sessions", len(tabOrder(pair.first)), len(pair.first.sessions))
	}
	if len(tabOrder(pair.second)) != 1 || len(pair.second.sessions) != 1 {
		t.Fatalf("the second window has %d tabs and %d sessions", len(tabOrder(pair.second)), len(pair.second.sessions))
	}
	if live.canceled != 0 {
		t.Fatal("moving the tab canceled its connection")
	}
	session := &pair.second.sessions[0]
	if session.conn != Connection(live) || pair.second.deck.Panes(tabOrder(pair.second)[0], nil)[0].SessionID != layoutcore.SessionID(session.ID) {
		t.Fatalf("the session in the second window is not the live one: %+v", session.ID)
	}
	if pair.second.activeSection != sectionTerminal || pair.second.activeSessionID != session.ID {
		t.Fatalf("section %d active %d", pair.second.activeSection, pair.second.activeSessionID)
	}
	_ = oldSession

	// Its goroutines now wake the second window, not the first.
	beforeFirst, beforeSecond := pair.firstWakes.Load(), pair.secondWakes.Load()
	live.configs[0].Notify()
	if pair.firstWakes.Load() != beforeFirst || pair.secondWakes.Load() == beforeSecond {
		t.Fatalf("wakes: first %d->%d, second %d->%d", beforeFirst, pair.firstWakes.Load(), beforeSecond, pair.secondWakes.Load())
	}
	// Typing in the second window reaches the same shell, and its output shows there.
	shell := live.shell(t)
	if !pair.second.typed(session, []byte("ls\r")) || shell.typed() != "ls\r" {
		t.Fatalf("typed %q", shell.typed())
	}
	shell.say("moved\r\n")
	pair.second.Wake()
	if text := pair.second.sessions[0].term.screen.Text(); text == "" {
		t.Fatal("output does not reach the second window")
	}
	// The first window no longer saves it.
	saved, _, _ := loadState(pair.first.options.StatePath)
	if saved.Deck == nil || len(saved.Deck.Workspaces) != 1 {
		t.Fatal("the first window still saves the moved tab")
	}
}

func TestATabDroppedOnAPaneEdgeOfAnotherWindowDocksThere(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.second, pair.secondW, 1)
	moving := tabOrder(pair.first)[0]
	frame := pair.second.workspacePixelBounds(pair.second.viewport)

	// The right edge of the second window's pane.
	dragAcross(pair.first, moving, ui.Point{X: 1500 + frame.X + frame.Width*0.95, Y: frame.Y + frame.Height/2}, ui.Point{})
	if len(tabOrder(pair.second)) != 1 {
		t.Fatalf("a dock added a tab: %v", tabOrder(pair.second))
	}
	panes := pair.second.deck.Panes(tabOrder(pair.second)[0], nil)
	if len(panes) != 2 {
		t.Fatalf("%d panes after the dock", len(panes))
	}
	if len(pair.second.sessions) != 2 || len(pair.first.sessions) != 0 {
		t.Fatalf("sessions: first %d second %d", len(pair.first.sessions), len(pair.second.sessions))
	}
}

func TestWhileOverAnotherWindowItShowsWhereTheTabWouldLand(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.second, pair.secondW, 1)
	moving := tabOrder(pair.first)[0]
	target := hitTabList.At(int(moving))
	start := tabCenter(pair.first, moving)
	pair.first.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, Target: target, X: start.X, Y: start.Y, ScreenX: start.X, ScreenY: start.Y, Viewport: pair.first.viewport})
	frame := pair.second.workspacePixelBounds(pair.second.viewport)
	at := ui.Point{X: 1500 + frame.X + frame.Width*0.05, Y: frame.Y + frame.Height/2}
	pair.first.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Target: target, Captured: true, X: at.X, Y: at.Y, ScreenX: at.X, ScreenY: at.Y, Viewport: pair.first.viewport})
	if !pair.second.foreign.active || pair.second.foreign.drop.kind != dropDock || pair.second.foreign.drop.edge != dockLeft {
		t.Fatalf("the second window shows %+v", pair.second.foreign)
	}
	var canvas ui.Canvas
	pair.second.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	zone := false
	for _, rectangle := range canvas.Rectangles() {
		if rectangle.Layer == tabDragLayer && rectangle.Color.Alpha < 1 && rectangle.Bounds == pair.second.foreign.drop.zone {
			zone = true
		}
	}
	if !zone {
		t.Fatal("no wash painted in the window the tab is over")
	}
	// Back over its own window: the other one stops showing it.
	pair.first.Pointer(ui.PointerEvent{Kind: ui.PointerMove, Target: target, Captured: true, X: start.X + 50, Y: start.Y, ScreenX: start.X + 50, ScreenY: start.Y, Viewport: pair.first.viewport})
	if pair.second.foreign.active {
		t.Fatal("the second window still shows a drag that left it")
	}
	pair.first.Key(ui.KeyEvent{Key: ui.KeyEscape})
}

func TestATabDroppedOutsideEveryWindowOpensAWindowOfItsOwn(t *testing.T) {
	pair := newTwoWindows(t)
	live := liveTab(t, pair.first, pair.firstW, 0)
	liveTab(t, pair.first, pair.firstW, 1)
	moving := tabOrder(pair.first)[0]

	dragAcross(pair.first, moving, ui.Point{X: 3300, Y: 500}, ui.Point{})

	if len(pair.torn) != 1 {
		t.Fatalf("%d windows were opened", len(pair.torn))
	}
	window := pair.torn[0]
	if len(window.sessions) != 1 || window.sessions[0].conn != Connection(live) || len(tabOrder(window)) != 1 {
		t.Fatalf("the new window has %d sessions, %d tabs", len(window.sessions), len(tabOrder(window)))
	}
	if len(window.sessions) == 1 && window.activeSessionID != window.sessions[0].ID {
		t.Fatal("the torn-off session is not on show")
	}
	if len(tabOrder(pair.first)) != 1 || live.canceled != 0 {
		t.Fatalf("first window tabs %d, canceled %d", len(tabOrder(pair.first)), live.canceled)
	}
}

func TestADropOutsideWithNoWayToOpenAWindowChangesNothing(t *testing.T) {
	pair := newTwoWindows(t)
	pair.first.options.TearOff = nil
	liveTab(t, pair.first, pair.firstW, 0)
	moving := tabOrder(pair.first)[0]
	dragAcross(pair.first, moving, ui.Point{X: 3300, Y: 500}, ui.Point{})
	if len(tabOrder(pair.first)) != 1 || len(pair.first.sessions) != 1 {
		t.Fatalf("tabs %d sessions %d", len(tabOrder(pair.first)), len(pair.first.sessions))
	}
}

func TestAHiddenOrClosedWindowIsNotADropTarget(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	pair.second.SetWindow(pair.second.frame, func() bool { return false })
	if pair.first.options.Group.windowAt(pair.first, ui.Point{X: 2000, Y: 100}) != nil {
		t.Fatal("a hidden window takes drops")
	}
	pair.second.SetWindow(pair.second.frame, func() bool { return true })
	if pair.first.options.Group.windowAt(pair.first, ui.Point{X: 2000, Y: 100}) != pair.second {
		t.Fatal("a visible window under the point does not")
	}
	pair.second.Close()
	if pair.first.options.Group.windowAt(pair.first, ui.Point{X: 2000, Y: 100}) != nil {
		t.Fatal("a closed window takes drops")
	}
}

func TestAMovedSessionTypesAndSearchesInItsNewWindow(t *testing.T) {
	pair := newTwoWindows(t)
	live := liveTab(t, pair.first, pair.firstW, 0)
	moving := tabOrder(pair.first)[0]
	dragAcross(pair.first, moving, ui.Point{X: 2200, Y: 450}, ui.Point{})
	session := &pair.second.sessions[0]
	pair.second.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})

	// The text system's handler is the new window's, and the session's new number.
	input := pair.second.TerminalInput(app.TextInputContext{ID: hitTerminalList.At(int(session.ID)), Terminal: true})
	if input == nil || input.OnEvent == nil {
		t.Fatal("the moved terminal takes no text")
	}
	if _, err := input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "echo hi"}); err != nil {
		t.Fatal(err)
	}
	if got := live.shell(t).typed(); got != "echo hi" {
		t.Fatalf("the shell got %q", got)
	}
}

func TestATabWithAFileTransferRunningStaysWhereItIs(t *testing.T) {
	pair := newTwoWindows(t)
	liveTab(t, pair.first, pair.firstW, 0)
	moving := tabOrder(pair.first)[0]
	pair.first.transfer.phase = transferRunning
	pair.first.transfer.sessionID = pair.first.sessions[0].ID
	dragAcross(pair.first, moving, ui.Point{X: 2200, Y: 450}, ui.Point{})
	if len(pair.first.sessions) != 1 || len(pair.second.sessions) != 0 {
		t.Fatalf("sessions: first %d second %d", len(pair.first.sessions), len(pair.second.sessions))
	}
	if pair.first.status != pair.first.messages.Tools.TabMoveBusy {
		t.Fatalf("status %q", pair.first.status)
	}
}

func TestMovingAWindowsLastTabClosesTheEmptiedWindow(t *testing.T) {
	pair := newTwoWindows(t)
	closed := 0
	pair.first.options.CloseWindow = func() { closed++ }
	liveTab(t, pair.first, pair.firstW, 0)
	moving := tabOrder(pair.first)[0]

	dragAcross(pair.first, moving, ui.Point{X: 2200, Y: 450}, ui.Point{})

	if len(tabOrder(pair.second)) != 1 {
		t.Fatalf("the tab did not arrive: %d", len(tabOrder(pair.second)))
	}
	if closed != 1 {
		t.Fatalf("the emptied window was asked to close %d times", closed)
	}
}
