package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceLaunchKeepsAFailedPaneAndRetriesOnlyThatSession(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	plan := WorkspaceLaunchPlan{
		Hosts: []Host{
			testWorkspaceHost("web", "web.example.com"),
			testWorkspaceHost("db", "db.example.com"),
			testWorkspaceHost("cache", "cache.example.com"),
		},
		Layout: layoutcore.GridSpec(3),
	}
	workspaceID, sessions, err := state.launchWorkspace(plan, "Operations")
	if err != nil {
		t.Fatal(err)
	}
	if len(world.conns) != len(sessions) {
		t.Fatalf("dials = %d, sessions = %d", len(world.conns), len(sessions))
	}

	world.conns[1].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = errors.New("database unreachable")
	})
	for _, connection := range world.conns {
		connection.set(func(status *sshconn.Status) {
			if status.Phase == sshconn.PhaseFailed {
				return
			}
			status.Phase = sshconn.PhaseConnected
		})
	}
	state.Wake()

	panes := state.deck.Panes(workspaceID, nil)
	if len(panes) != 3 {
		t.Fatalf("panes = %+v", panes)
	}
	for index, pane := range panes {
		if pane.SessionID != layoutcore.SessionID(sessions[index].ID) {
			t.Fatalf("pane %d = %+v, want session %d", index, pane, sessions[index].ID)
		}
	}
	failed, _ := state.sessionByIDPointer(sessions[1].ID)
	peers := []*Session{}
	for _, index := range []int{0, 2} {
		peer, _ := state.sessionByIDPointer(sessions[index].ID)
		peers = append(peers, peer)
	}
	if !failed.failed() || failed.term == nil || failed.term.live {
		t.Fatalf("failed pane = %+v", failed)
	}
	for _, peer := range peers {
		if peer.failed() || peer.phase != sshconn.PhaseConnected {
			t.Fatalf("successful peer = %+v", peer)
		}
	}
	if !state.canReconnect(failed) {
		t.Fatal("the failed pane has no retry path")
	}

	if !state.ActivatePane(failed.ID) {
		t.Fatal("failed pane could not be activated")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, state.paneIdentity(failed, hitConnRetry)); !ok {
		t.Fatal("the failed pane does not paint its retry control")
	}

	dialsBefore := len(world.conns)
	state.Click(state.paneIdentity(failed, hitConnRetry))
	if len(world.conns) != dialsBefore+1 {
		t.Fatalf("retry dialed %d connections, want one", len(world.conns)-dialsBefore)
	}
	for index, session := range state.sessions {
		if session.ID == failed.ID {
			if state.sessions[index].phase != sshconn.PhaseConnecting {
				t.Fatalf("retry left phase %d", state.sessions[index].phase)
			}
			continue
		}
		if session.ID != peers[0].ID && session.ID != peers[1].ID {
			continue
		}
		if session.phase != sshconn.PhaseConnected {
			t.Fatalf("retry disturbed peer %d: phase %d", session.ID, session.phase)
		}
	}
}
