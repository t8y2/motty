package workbench

import (
	"testing"

	"motty/internal/sshconn"
)

func TestWindowReconnectConfirmsBeforeRestartingALiveSession(t *testing.T) {
	state, world, connection, shell := liveSession(t)
	session := &state.sessions[0]
	workspace := state.deck.ActiveWorkspace()
	canvas, _ := paintedTexts(state)
	button := panelHit(t, canvas, hitWindowReconnect)
	transfers := panelHit(t, canvas, hitTransferCenter)
	if button.Disabled || button.Label != state.messages.MenuReconnect || button.Hint != state.messages.MenuReconnect {
		t.Fatalf("reconnect button = %+v", button)
	}
	if button.Bounds.X+button.Bounds.Width > transfers.Bounds.X {
		t.Fatal("reconnect must precede transfers in the default window toolbar")
	}

	state.Click(hitWindowReconnect)
	if state.modal != modalSessionLink || state.sessionLink.session != session.ID || state.sessionLink.disconnect || connection.canceled != 0 {
		t.Fatalf("modal=%v link=%+v canceled=%d", state.modal, state.sessionLink, connection.canceled)
	}
	state.Click(hitSessionLinkCancel)
	if state.modal != modalNone || len(world.conns) != 1 || connection.canceled != 0 || shell.closed {
		t.Fatal("canceling reconnect changed the running session")
	}
	state.Click(hitWindowReconnect)
	state.Click(hitSessionLinkConfirm)
	if len(world.conns) != 2 || connection.canceled != 1 || !shell.closed || len(state.sessions) != 1 || state.deck.ActiveWorkspace() != workspace {
		t.Fatal("confirmed reconnect did not replace only the connection in the original tab")
	}
	if session.conn != Connection(world.conns[1]) {
		t.Fatal("the active session did not take the new connection")
	}
}

func TestWindowReconnectIsDisabledWithoutAnEligibleVisibleSession(t *testing.T) {
	t.Run("no session", func(t *testing.T) {
		world, options := newWorld(t)
		state := newTestState(t, options)
		canvas, _ := paintedTexts(state)
		if !panelHit(t, canvas, hitWindowReconnect).Disabled {
			t.Fatal("reconnect is enabled without a session")
		}
		state.Click(hitWindowReconnect)
		if len(world.conns) != 0 || state.modal != modalNone {
			t.Fatal("reconnect without a session started an action")
		}
	})
	for _, name := range []string{"background session", "connecting", "container session"} {
		t.Run(name, func(t *testing.T) {
			state, world, connection, _ := liveSession(t)
			switch name {
			case "background session":
				state.Click(hitHosts)
			case "connecting":
				state.sessions[0].phase = sshconn.PhaseConnecting
			case "container session":
				state.sessions[0].containerCommand = "docker exec -it test sh"
			}
			canvas, _ := paintedTexts(state)
			if !panelHit(t, canvas, hitWindowReconnect).Disabled {
				t.Fatal("reconnect is enabled for an ineligible session")
			}
			state.Click(hitWindowReconnect)
			if state.modal != modalNone || connection.canceled != 0 || len(world.conns) != 1 {
				t.Fatal("disabled reconnect changed the session")
			}
		})
	}
}

func TestWindowReconnectDialsEndedAndRestoredSessionsWithoutAsking(t *testing.T) {
	for _, name := range []string{"disconnected", "failed", "restored"} {
		t.Run(name, func(t *testing.T) {
			state, world, connection, shell := liveSession(t)
			switch name {
			case "disconnected":
				state.disconnect(&state.sessions[0])
			case "failed":
				shell.end(errTestWorkspaceFailure)
				connection.set(func(status *sshconn.Status) {
					status.Phase, status.Err = sshconn.PhaseFailed, errTestWorkspaceFailure
				})
				state.Wake()
			case "restored":
				state = newTestState(t, state.options)
			}
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			canvas, _ := paintedTexts(state)
			if panelHit(t, canvas, hitWindowReconnect).Disabled {
				t.Fatal("reconnect is disabled for a session that can connect")
			}
			state.Click(hitWindowReconnect)
			if state.modal != modalNone || len(world.conns) != 2 || session.phase != sshconn.PhaseConnecting || session.restored {
				t.Fatalf("modal=%v conns=%d phase=%v restored=%t", state.modal, len(world.conns), session.phase, session.restored)
			}
			canvas, _ = paintedTexts(state)
			if !panelHit(t, canvas, hitWindowReconnect).Disabled {
				t.Fatal("reconnect stayed enabled during the new connection attempt")
			}
			state.Click(hitWindowReconnect)
			if len(world.conns) != 2 || state.modal != modalNone {
				t.Fatal("a repeated click started another connection attempt")
			}
		})
	}
}

func TestWindowReconnectTargetsOnlyTheFocusedPane(t *testing.T) {
	state, _, sessions := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	for _, chosen := range sessions {
		if !state.ActivatePane(chosen.ID) {
			t.Fatal("could not focus the pane")
		}
		state.Click(hitWindowReconnect)
		if state.modal != modalSessionLink || state.sessionLink.session != chosen.ID {
			t.Fatalf("focused=%d reconnect target=%+v", chosen.ID, state.sessionLink)
		}
		state.Click(hitSessionLinkCancel)
	}

	first, _ := state.sessionByIDPointer(sessions[0].ID)
	second, _ := state.sessionByIDPointer(sessions[1].ID)
	firstConnection := first.conn
	secondConnection := second.conn.(*fakeConn)
	state.Click(hitWindowReconnect)
	state.Click(hitSessionLinkConfirm)
	if first.conn != firstConnection || second.conn == Connection(secondConnection) || secondConnection.canceled != 1 || len(state.sessions) != 2 {
		t.Fatal("reconnect did not preserve the other pane's connection")
	}
}
