package workbench

import (
	"testing"

	layoutcore "motty/internal/workspace"
)

func TestBindWorkspaceSessionCreatesAndBindsWithoutDialing(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID, panes := createEmptyWorkspace(t, state, 2)
	host := testWorkspaceHost("web", "web.example.com")

	session := state.bindWorkspaceSession(workspaceID, panes[1], host, state.sessionAddress(host))
	if session == nil {
		t.Fatal("no session was created")
	}
	if len(world.configs) != 0 {
		t.Fatalf("binding dialed %d connections", len(world.configs))
	}
	pane, ok := state.deck.Pane(workspaceID, panes[1])
	if !ok || pane.SessionID != layoutcore.SessionID(session.ID) {
		t.Fatalf("pane = %+v, session = %d", pane, session.ID)
	}
	if state.activeSessionID != session.ID || state.activeSection != sectionTerminal {
		t.Fatalf("active session = %d, section = %d", state.activeSessionID, state.activeSection)
	}
	if state.deck.ActiveWorkspace() != workspaceID || state.deck.ActivePane(workspaceID) != panes[1] {
		t.Fatalf("workspace = %d, active pane = %d", state.deck.ActiveWorkspace(), state.deck.ActivePane(workspaceID))
	}
	if err := state.deck.Validate(); err != nil {
		t.Fatal(err)
	}
	if len(state.sessions) != 1 {
		t.Fatalf("sessions = %d", len(state.sessions))
	}
}

func TestBindWorkspaceSessionRejectsAnOccupiedPane(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID, panes := createEmptyWorkspace(t, state, 2)
	first := testWorkspaceHost("web", "web.example.com")
	second := testWorkspaceHost("db", "db.example.com")
	bound := state.bindWorkspaceSession(workspaceID, panes[0], first, state.sessionAddress(first))
	if bound == nil {
		t.Fatal("first binding failed")
	}
	beforeSessions := len(state.sessions)
	if extra := state.bindWorkspaceSession(workspaceID, panes[0], second, state.sessionAddress(second)); extra != nil {
		t.Fatalf("occupied pane created session %+v", extra)
	}
	if len(state.sessions) != beforeSessions {
		t.Fatalf("sessions %d -> %d", beforeSessions, len(state.sessions))
	}
}

func TestBindWorkspaceSessionStartsTheConnectionSeparately(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID, panes := createEmptyWorkspace(t, state, 1)
	host := testWorkspaceHost("web", "web.example.com")
	session := state.bindWorkspaceSession(workspaceID, panes[0], host, state.sessionAddress(host))
	if session == nil || len(world.configs) != 0 {
		t.Fatalf("bind: session=%v dials=%d", session, len(world.configs))
	}
	state.startWorkspaceSession(session, host)
	if len(world.configs) != 1 || world.configs[0].Host != "web.example.com" {
		t.Fatalf("start dialed %+v", world.configs)
	}
}

func createEmptyWorkspace(t testing.TB, state *State, paneCount int) (layoutcore.WorkspaceID, []layoutcore.PaneID) {
	t.Helper()
	workspaceID, first, err := state.deck.Create("Workspace")
	if err != nil {
		t.Fatal(err)
	}
	panes := []layoutcore.PaneID{first}
	for len(panes) < paneCount {
		paneID, err := state.deck.Insert(workspaceID, panes[len(panes)-1], layoutcore.Horizontal, 0.5, layoutcore.After)
		if err != nil {
			t.Fatal(err)
		}
		panes = append(panes, paneID)
	}
	if err := state.deck.SetLayout(workspaceID, panes, layoutcore.GridSpec(uint8(min(paneCount, 3)))); err != nil {
		t.Fatal(err)
	}
	state.syncActiveSession()
	return workspaceID, panes
}

func testWorkspaceHost(label, address string) Host {
	host := Host{ID: uint32(len(address)), CatalogID: "host_" + label, Label: label, Address: address, Port: 22}
	host.finish()
	return host
}
