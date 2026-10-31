package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

func menuHas(state *State, id ui.ID) bool {
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	_, ok := hitByID(&canvas, id)
	return ok
}

// A program that has hung leaves a connection that is still sound, so nothing
// offers to end or restart it; Disconnect and Reconnect do, after asking.
func TestDisconnectEndsALiveSessionButKeepsItsTab(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	session := &state.sessions[0]
	workspace := state.deck.ActiveWorkspace()

	rightClickTab(state, hitTabList.At(int(workspace)))
	if !menuHas(state, hitTabMenuDisconnect) || !menuHas(state, hitTabMenuRestart) || menuHas(state, hitTabMenuReconnect) {
		t.Fatal("a running session must offer Disconnect and Reconnect, and not the dead-session Connect")
	}
	state.Click(hitTabMenuDisconnect)
	if state.modal != modalSessionLink || fake.canceled != 0 {
		t.Fatalf("modal=%v canceled=%d: ending a running session must wait for the answer", state.modal, fake.canceled)
	}
	state.Click(hitSessionLinkCancel)
	if state.modal != modalNone || fake.canceled != 0 || session.term.ended {
		t.Fatalf("Cancel left modal=%v canceled=%d ended=%v", state.modal, fake.canceled, session.term.ended)
	}

	rightClickTab(state, hitTabList.At(int(workspace)))
	state.Click(hitTabMenuDisconnect)
	state.Click(hitSessionLinkConfirm)
	if fake.canceled != 1 || !shell.closed || !session.term.ended || len(state.sessions) != 1 {
		t.Fatalf("canceled=%d shell closed=%v ended=%v sessions=%d", fake.canceled, shell.closed, session.term.ended, len(state.sessions))
	}
	if screen := screenText(state, 0); !strings.Contains(screen, state.messages.ConnClosed) || !strings.Contains(screen, state.messages.SessionReconnectHint) {
		t.Fatalf("screen = %q", screen)
	}

	// The connection then reports that it was canceled. That is the user's
	// doing, not a failure: no error page, no failure status.
	fake.set(func(status *sshconn.Status) { status.Phase, status.Err = sshconn.PhaseClosed, sshconn.ErrCanceled })
	state.Wake()
	if session.failed() || session.err != nil || session.tone == toneFail || strings.HasPrefix(state.status, state.messages.StatusConnectFailed) {
		t.Fatalf("failed=%v err=%v tone=%v status=%q", session.failed(), session.err, session.tone, state.status)
	}
	if strings.Contains(screenText(state, 0), state.messages.ConnCanceled) {
		t.Fatalf("screen = %q: the cancel the user asked for is not reported as one", screenText(state, 0))
	}

	rightClickTab(state, hitTabList.At(int(workspace)))
	if !menuHas(state, hitTabMenuReconnect) || menuHas(state, hitTabMenuDisconnect) || menuHas(state, hitTabMenuRestart) {
		t.Fatal("a disconnected tab offers Connect, and nothing to end")
	}
	state.Click(hitTabMenuReconnect)
	if state.modal != modalNone {
		t.Fatal("connecting a dead tab asks nothing: there is nothing to lose")
	}
}

func TestReconnectRestartsALiveSessionInTheSameTabAfterAsking(t *testing.T) {
	state, world, fake, _ := liveSession(t)
	session := &state.sessions[0]
	workspace := state.deck.ActiveWorkspace()

	rightClickTab(state, hitTabList.At(int(workspace)))
	state.Click(hitTabMenuRestart)
	if state.modal != modalSessionLink || len(world.conns) != 1 {
		t.Fatalf("modal=%v conns=%d", state.modal, len(world.conns))
	}
	state.Click(hitSessionLinkCancel)
	if len(world.conns) != 1 || fake.canceled != 0 {
		t.Fatalf("Cancel dialed anyway: conns=%d canceled=%d", len(world.conns), fake.canceled)
	}

	rightClickTab(state, hitTabList.At(int(workspace)))
	state.Click(hitTabMenuRestart)
	state.Click(hitSessionLinkConfirm)
	if len(world.conns) != 2 || fake.canceled != 1 || len(state.sessions) != 1 || state.deck.ActiveWorkspace() != workspace {
		t.Fatalf("conns=%d canceled=%d sessions=%d", len(world.conns), fake.canceled, len(state.sessions))
	}
	if session.conn != Connection(world.conns[1]) {
		t.Fatal("the tab did not take the new connection")
	}
}

// What is asked in the terminal's own menu reaches the same confirmation.
func TestTerminalMenuOffersDisconnectOnlyWhileItIsConnected(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	session := &state.sessions[0]
	if !state.canDisconnect(session) || !state.canReconnectLive(session) || state.canReconnect(session) {
		t.Fatal("a running session can be ended and restarted, and is not one to connect")
	}
	state.openTerminalMenu(session)
	if !menuHas(state, hitTerminalMenuDisconnect) || !menuHas(state, hitTerminalMenuRestart) {
		t.Fatal("the terminal menu lacks Disconnect or Reconnect")
	}
	state.Click(hitTerminalMenuDisconnect)
	if state.modal != modalSessionLink || !state.sessionLink.disconnect || state.sessionLink.session != session.ID {
		t.Fatalf("modal=%v link=%+v", state.modal, state.sessionLink)
	}
	state.Click(hitSessionLinkConfirm)
	if fake.canceled != 1 || state.canDisconnect(session) {
		t.Fatalf("canceled=%d", fake.canceled)
	}
}

// A telnet or serial tab and a local shell have no connection of their own to
// report an end: the tab is ended by what the user did, and starts again.
func TestDisconnectEndsAStreamSessionAndItCanConnectAgain(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)
	addStreamHost(state, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the shell", func() bool { return session.term.shell != nil })
	session.term.live = true

	if !state.canDisconnect(session) {
		t.Fatal("a connected telnet tab cannot be disconnected")
	}
	state.askSessionLink(session, true)
	state.Click(hitSessionLinkConfirm)
	if !session.term.ended || session.term.shell != nil || !streams.shells[0].closed {
		t.Fatalf("ended=%v shell=%v closed=%v", session.term.ended, session.term.shell, streams.shells[0].closed)
	}
	if !state.canReconnect(session) || state.canDisconnect(session) {
		t.Fatal("a disconnected tab connects again, and has nothing left to disconnect")
	}
	state.reconnect(session)
	waitUntil(t, state, "the new stream", func() bool { return session.term.shell != nil })
	if len(streams.requests) != 2 {
		t.Fatalf("requests = %d", len(streams.requests))
	}
}

func TestDisconnectedLocalShellEndsItsProcess(t *testing.T) {
	state, session := openLocal(t)
	state.Wake()
	if !state.canDisconnect(session) {
		t.Fatal("a running local shell cannot be disconnected")
	}
	state.disconnect(session)
	if !session.term.ended || session.term.shell != nil || !state.canReconnect(session) {
		t.Fatalf("ended=%v shell=%v", session.term.ended, session.term.shell)
	}
}

// The mark is cleared by the next connection, or a later real failure would be hidden.
func TestADisconnectIsForgottenByTheNextConnection(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	state.disconnect(session)
	if !session.disconnected {
		t.Fatal("disconnect did not mark the session")
	}
	state.reconnect(session)
	if session.disconnected {
		t.Fatal("a new connection kept the mark of the last one")
	}
}

// A remote desktop ends like a lost connection does: the picture stays, dimmed,
// behind the page that offers to start it again.
func TestDisconnectEndsARemoteDesktopAndItReconnects(t *testing.T) {
	fixture := startVNCFixture(t, "secret")
	state, session := vncState(t, fixture)
	connectDesktop(t, state, session, "secret")
	if !state.canDisconnect(session) || !state.canReconnectLive(session) {
		t.Fatal("a connected desktop can be neither disconnected nor reconnected")
	}
	state.askSessionLink(session, true)
	state.Click(hitSessionLinkConfirm)
	if !session.desk.ended || !state.canReconnect(session) || state.canDisconnect(session) {
		t.Fatalf("ended=%v canReconnect=%v", session.desk.ended, state.canReconnect(session))
	}
	state.reconnect(session)
	connectDesktop(t, state, session, "secret")
	if fixture.accepted != 2 {
		t.Fatalf("server accepted %d connections, want 2", fixture.accepted)
	}
}

// A shell in a container is borrowed from its host's connection and has no way
// to be started again in its tab, so it can be ended and not reconnected.
func TestAContainerSessionCanBeDisconnectedButNotReconnected(t *testing.T) {
	state, _, _, _ := liveSession(t)
	session := &state.sessions[0]
	session.containerCommand = "docker exec -it web sh"
	if !state.canDisconnect(session) || state.canReconnectLive(session) || state.canReconnect(session) {
		t.Fatal("a running container shell offers Disconnect and nothing to connect")
	}
}
