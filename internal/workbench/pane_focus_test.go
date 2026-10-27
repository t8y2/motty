package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

// paneDriver drives a state through the real painter and input router, so a test
// sees the focus and the clicks a window would.
func paneDriver(t *testing.T, state *State, size ui.Size) *app.Driver {
	t.Helper()
	driver, err := app.NewDriver(app.Config{
		Title: "panes", Width: int(size.Width), Height: int(size.Height),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if context.Terminal {
				return state.TerminalInput(context)
			}
			return nil
		},
	}, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(driver.Close)
	return driver
}

// hitOf returns the hit a driver painted with that identity.
func hitOf(t *testing.T, driver *app.Driver, id ui.ID) ui.Hit {
	t.Helper()
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == id {
			return hit
		}
	}
	t.Fatalf("hit %d was not painted", id)
	return ui.Hit{}
}

// pagedIdentity is a page identity as the window sees it: a workspace of one
// pane paints its page in place, and panes side by side mount theirs.
func pagedIdentity(state *State, session *Session, local ui.ID) ui.ID {
	if len(state.Panes(nil)) > 1 {
		return tree.MountID(paneMount(session), local)
	}
	return local
}

// terminalOf is the identity of the terminal a session's page paints.
func terminalOf(state *State, session *Session) ui.ID {
	return pagedIdentity(state, session, hitTerminalList.At(int(session.ID)))
}

// paneBoundsOf is where a session's pane sits in the window.
func paneBoundsOf(t *testing.T, state *State, size ui.Size, session *Session) ui.Rect {
	t.Helper()
	pane, ok := state.paneForSession(session.ID)
	if !ok {
		t.Fatalf("session %d has no pane", session.ID)
	}
	return pixelBounds(state.workspacePixelBounds(size), pane.Bounds)
}

// clickPaneGrid presses the middle of a pane's terminal, the way a user puts the
// keyboard in a pane.
func clickPaneGrid(t *testing.T, state *State, driver *app.Driver, session *Session) {
	t.Helper()
	hit := hitOf(t, driver, terminalOf(state, session))
	if err := driver.Click(hit.Bounds.X+20, hit.Bounds.Y+40); err != nil {
		t.Fatal(err)
	}
}

// clickPaneBar presses a pane's bar, the ground above its terminal.
func clickPaneBar(t *testing.T, state *State, driver *app.Driver, size ui.Size, session *Session) {
	t.Helper()
	bounds := paneBoundsOf(t, state, size, session)
	if err := driver.Click(bounds.X+bounds.Width/2, bounds.Y+infoBarHeight/2); err != nil {
		t.Fatal(err)
	}
}

// clickPanePage presses a pane's page away from the controls it painted.
func clickPanePage(t *testing.T, state *State, driver *app.Driver, size ui.Size, session *Session) {
	t.Helper()
	bounds := paneBoundsOf(t, state, size, session)
	if err := driver.Click(bounds.X+bounds.Width/2, bounds.Y+bounds.Height-40); err != nil {
		t.Fatal(err)
	}
}

// twoLivePanes opens a split tab with a live shell in each pane and returns both
// shells.
func twoLivePanes(t *testing.T) (*State, *fakeShell, *fakeShell) {
	t.Helper()
	state, world, _, firstShell := liveSession(t)
	if !state.splitActivePane(layoutcore.Horizontal) {
		t.Fatal("split right")
	}
	state.Click(hitPaneChooseHost)
	state.Click(hitHostList.At(2))
	openHost(state)
	secondConn := world.conns[1]
	secondConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	secondShell := secondConn.shell(t)
	secondShell.say("ready")
	state.Wake()
	return state, firstShell, secondShell
}

// Pressing the bar of another pane makes it the pane the keyboard is in, and
// typing that follows reaches that pane's shell rather than the one that had the
// keyboard before.
func TestPressingAPaneBarTakesTheKeyboardIntoIt(t *testing.T) {
	state, firstShell, secondShell := twoLivePanes(t)
	size := ui.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	first, second := &state.sessions[0], &state.sessions[1]

	clickPaneGrid(t, state, driver, first)
	if state.activeSessionID != first.ID || driver.Snapshot().Focused != terminalOf(state, first) {
		t.Fatalf("the first pane did not take the keyboard: active=%d focused=%d", state.activeSessionID, driver.Snapshot().Focused)
	}

	clickPaneBar(t, state, driver, size, second)
	if state.activeSessionID != second.ID {
		t.Fatalf("pressing the second pane's bar activated %d, want %d", state.activeSessionID, second.ID)
	}
	if focused, want := driver.Snapshot().Focused, terminalOf(state, second); focused != want {
		t.Fatalf("the keyboard stayed at %d, want the second pane's terminal %d", focused, want)
	}
	if err := driver.Type("x"); err != nil {
		t.Fatal(err)
	}
	if firstShell.typed() != "" || secondShell.typed() != "x" {
		t.Fatalf("typed first=%q second=%q, want the second pane only", firstShell.typed(), secondShell.typed())
	}
}

// The bar of a pane that has no split around it is still part of the pane: a
// press there leaves the page under it with the keyboard, so typing goes on.
func TestPressingTheBarOfALonePaneKeepsTyping(t *testing.T) {
	state, _, _, shell := liveSession(t)
	size := ui.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	session := &state.sessions[0]

	clickPaneGrid(t, state, driver, session)
	clickPaneBar(t, state, driver, size, session)
	if focused, want := driver.Snapshot().Focused, terminalOf(state, session); focused != want {
		t.Fatalf("pressing the bar left the keyboard at %d, want the terminal %d", focused, want)
	}
	if err := driver.Type("x"); err != nil {
		t.Fatal(err)
	}
	if shell.typed() != "x" {
		t.Fatalf("typed %q, want the terminal to keep the keyboard", shell.typed())
	}
}

// A restored pane has no terminal, so pressing it puts the keyboard on the one
// action its page offers: Enter then connects that pane instead of typing into
// the pane the keyboard was in.
func TestEnterOnAPressedRestoredPaneConnectsThatPane(t *testing.T) {
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
	var livePane Pane
	for _, pane := range panes {
		if uint32(pane.SessionID) != restored.ID {
			livePane = pane
		}
	}
	live, _ := state.sessionByIDPointer(uint32(livePane.SessionID))

	// The other pane connects, so the keyboard starts in a real terminal.
	state.reconnect(live)
	liveConn := world.conns[len(world.conns)-1]
	liveConn.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	shell := liveConn.shell(t)
	shell.say("\x1b[0m")
	state.Wake()

	size := ui.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	clickPaneGrid(t, state, driver, live)
	if focused, want := driver.Snapshot().Focused, terminalOf(state, live); focused != want {
		t.Fatalf("the live pane holds %d, want its terminal %d", focused, want)
	}

	clickPanePage(t, state, driver, size, restored)
	if state.activeSessionID != restored.ID {
		t.Fatalf("pressing the restored pane activated %d, want %d", state.activeSessionID, restored.ID)
	}
	if focused, want := driver.Snapshot().Focused, pagedIdentity(state, restored, hitConnRetry); focused != want {
		t.Fatalf("the restored page left the keyboard at %d, want its Connect button %d", focused, want)
	}
	if err := driver.Key(ui.KeyEnter, 0); err != nil {
		t.Fatal(err)
	}
	if restored.conn == nil {
		t.Fatal("Enter did not start the restored pane's connection")
	}
	if state.activeSessionID != restored.ID {
		t.Fatalf("Enter switched to pane %d, want the restored pane %d", state.activeSessionID, restored.ID)
	}
}
