package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func launchWorkspaceWithConnections(t *testing.T, hosts []Host) (*State, layoutcore.WorkspaceID, []Session) {
	t.Helper()
	world, options := newWorld(t)
	state := newTestState(t, options)
	plan := WorkspaceLaunchPlan{Hosts: hosts, Layout: layoutcore.GridSpec(uint8(min(len(hosts), 3)))}
	workspaceID, sessions, err := state.launchWorkspace(plan, "Broadcast")
	if err != nil {
		t.Fatal(err)
	}
	for index := range world.conns {
		world.conns[index].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	for index := range sessions {
		session, _ := state.sessionByIDPointer(sessions[index].ID)
		session.term.live = true
	}
	return state, workspaceID, sessions
}

func TestWorkspaceBroadcastWaitsForEveryAttemptAndTwoShells(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	plan := WorkspaceLaunchPlan{
		Hosts: []Host{
			testWorkspaceHost("web", "web.example.com"),
			testWorkspaceHost("db", "db.example.com"),
		},
		Layout: layoutcore.GridSpec(2),
	}
	workspaceID, sessions, err := state.launchWorkspace(plan, "Broadcast")
	if err != nil {
		t.Fatal(err)
	}

	state.Click(hitBroadcast)
	if state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastWait {
		t.Fatalf("early broadcast = %d, status = %q", state.broadcast, state.status)
	}

	for index := range world.conns {
		world.conns[index].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	first, _ := state.sessionByIDPointer(sessions[0].ID)
	first.term.live = true
	state.Click(hitBroadcast)
	if state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastWait {
		t.Fatalf("shell-wait broadcast = %d, status = %q", state.broadcast, state.status)
	}

	second, _ := state.sessionByIDPointer(sessions[1].ID)
	second.term.live = true
	state.Click(hitBroadcast)
	if state.broadcast != workspaceID || state.status == state.messages.Tools.StatusBroadcastWait {
		t.Fatalf("ready broadcast = %d, status = %q", state.broadcast, state.status)
	}
}

func TestWorkspaceBroadcastNeedsTwoConnectedShellsAfterFailuresSettle(t *testing.T) {
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
	workspaceID, sessions, err := state.launchWorkspace(plan, "Broadcast")
	if err != nil {
		t.Fatal(err)
	}
	for index := range world.conns {
		world.conns[index].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	for _, index := range []int{0, 1, 2} {
		session, _ := state.sessionByIDPointer(sessions[index].ID)
		if session.term != nil && session.term.shell != nil {
			session.term.live = true
		}
	}
	world.conns[1].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = errTestWorkspaceFailure
	})
	world.conns[2].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if workspaceCanBroadcast(state.workspaceConnectionState(workspaceID)) {
		t.Fatal("one connected shell could broadcast")
	}
	state.Click(hitBroadcast)
	if state.broadcast != 0 || state.status != state.messages.Tools.StatusBroadcastNeedsTwo {
		t.Fatalf("broadcast = %d, status = %q", state.broadcast, state.status)
	}
}

func TestWorkspaceBroadcastButtonIsDisabledUntilReady(t *testing.T) {
	state, _, sessions := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	for _, launched := range sessions {
		session, _ := state.sessionByIDPointer(launched.ID)
		session.term.live = false
	}
	if !state.ActivatePane(sessions[0].ID) {
		t.Fatal("activate the first pane")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitBroadcast); !ok || !hit.Disabled {
		t.Fatalf("broadcast hit = %+v, present=%v", hit, ok)
	}
}

func TestWorkspaceBroadcastClearsOnTabChangeLeaveAndNewLaunch(t *testing.T) {
	state, workspaceID, sessions := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	state.Click(hitBroadcast)
	if state.broadcast != workspaceID {
		t.Fatalf("broadcast = %d", state.broadcast)
	}

	otherWorkspace, _, err := state.deck.Create("Other")
	if err != nil {
		t.Fatal(err)
	}
	if !state.activateWorkspace(otherWorkspace) || state.broadcast != 0 {
		t.Fatalf("tab change left broadcast %d", state.broadcast)
	}

	if !state.activateWorkspace(workspaceID) {
		t.Fatal("return to workspace")
	}
	for _, launched := range sessions {
		session, _ := state.sessionByIDPointer(launched.ID)
		session.term.live = true
	}
	state.Click(hitBroadcast)
	expected := state.broadcast
	state.activeSection = sectionHosts
	state.leaveTerminalSection()
	if expected == 0 || state.broadcast != 0 {
		t.Fatalf("leaving the workspace left broadcast %d", state.broadcast)
	}

	state.activeSection = sectionTerminal
	state.Click(hitBroadcast)
	if _, _, err := state.launchWorkspace(WorkspaceLaunchPlan{
		Hosts:  []Host{testWorkspaceHost("api", "api.example.com"), testWorkspaceHost("cache", "cache.example.com")},
		Layout: layoutcore.GridSpec(2),
	}, "New"); err != nil {
		t.Fatal(err)
	}
	if state.broadcast != 0 {
		t.Fatalf("new workspace inherited broadcast %d", state.broadcast)
	}
	if len(sessions) != 2 {
		t.Fatalf("sessions = %d", len(sessions))
	}
}

func TestWorkspaceBroadcastClearsWhenAPaneReconnects(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID, sessions, err := state.launchWorkspace(WorkspaceLaunchPlan{
		Hosts:  []Host{testWorkspaceHost("web", "web.example.com"), testWorkspaceHost("db", "db.example.com")},
		Layout: layoutcore.GridSpec(2),
	}, "Reconnect")
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range world.conns {
		connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	for _, launched := range sessions {
		session, _ := state.sessionByIDPointer(launched.ID)
		session.term.live = true
	}
	state.Click(hitBroadcast)
	if state.broadcast != workspaceID {
		t.Fatalf("broadcast = %d", state.broadcast)
	}
	reconnected, _ := state.sessionByIDPointer(sessions[0].ID)
	state.reconnect(reconnected)
	if state.broadcast != 0 {
		t.Fatalf("reconnect left broadcast %d", state.broadcast)
	}
}

func TestWorkspaceBroadcastSkipsAFailedShellAndStopsOnReconnect(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID, sessions, err := state.launchWorkspace(WorkspaceLaunchPlan{
		Hosts: []Host{
			testWorkspaceHost("web", "web.example.com"),
			testWorkspaceHost("db", "db.example.com"),
			testWorkspaceHost("cache", "cache.example.com"),
		},
		Layout: layoutcore.GridSpec(3),
	}, "Filtered")
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range world.conns {
		connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	for _, launched := range sessions[:2] {
		session, _ := state.sessionByIDPointer(launched.ID)
		session.term.live = true
	}
	world.conns[2].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = errTestWorkspaceFailure
	})
	state.Wake()
	state.Click(hitBroadcast)
	if state.broadcast != workspaceID {
		t.Fatalf("broadcast = %d", state.broadcast)
	}
	source, _ := state.sessionByIDPointer(sessions[0].ID)
	firstPeers, _ := state.sessionByIDPointer(sessions[1].ID)
	firstPeers.term.shell.(*fakeShell).mu.Lock()
	firstPeers.term.shell.(*fakeShell).input = nil
	firstPeers.term.shell.(*fakeShell).mu.Unlock()
	state.typed(source, []byte("uptime\r"))
	if got := firstPeers.term.shell.(*fakeShell).typed(); got != "uptime\r" {
		t.Fatalf("connected peer received %q", got)
	}

	world.conns[1].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = sshconn.ErrDisconnected
	})
	state.Wake()
	if state.broadcast != 0 {
		t.Fatalf("a reconnect left broadcast %d", state.broadcast)
	}
}

var errTestWorkspaceFailure = workspaceTestError("workspace test failure")

type workspaceTestError string

func (err workspaceTestError) Error() string { return string(err) }
