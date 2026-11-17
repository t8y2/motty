package workbench

import (
	"errors"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceConnectionStateCountsPromptsPendingAndFailuresSettled(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	plan := WorkspaceLaunchPlan{
		Hosts: []Host{
			testWorkspaceHost("web", "web.example.com"),
			testWorkspaceHost("db", "db.example.com"),
			testWorkspaceHost("cache", "cache.example.com"),
			testWorkspaceHost("edge", "edge.example.com"),
			testWorkspaceHost("worker", "worker.example.com"),
		},
		Layout: layoutcore.GridSpec(2),
	}
	workspaceID, sessions, err := state.launchWorkspace(plan, "State")
	if err != nil {
		t.Fatal(err)
	}
	if len(world.conns) != len(sessions) {
		t.Fatalf("dials = %d, sessions = %d", len(world.conns), len(sessions))
	}

	world.conns[0].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseHostKeyPrompt
		status.HostKey = testHostKey
	})
	world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhasePasswordPrompt })
	world.conns[2].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	world.conns[3].set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseFailed
		status.Err = errors.New("unreachable")
	})
	world.conns[4].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()

	status := state.workspaceConnectionState(workspaceID)
	if status.Panes != 5 || status.Pending != 3 || status.Connected != 0 || status.Failed != 1 || status.Closed != 1 {
		t.Fatalf("state = %+v", status)
	}
	if workspaceCanBroadcast(state.workspaceConnectionState(workspaceID)) {
		t.Fatal("a pending workspace could broadcast")
	}

	for _, index := range []int{2} {
		session, _ := state.sessionByIDPointer(sessions[index].ID)
		if session.term == nil || session.term.shell == nil {
			t.Fatalf("connected pane %d has no shell", index)
		}
		session.term.live = true
	}
	status = state.workspaceConnectionState(workspaceID)
	if status.Pending != 2 || status.Connected != 1 || status.Failed != 1 || status.Closed != 1 {
		t.Fatalf("state after shells = %+v", status)
	}
	if workspaceCanBroadcast(state.workspaceConnectionState(workspaceID)) {
		t.Fatal("broadcast allowed before every attempt settled")
	}

	world.conns[0].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	world.conns[1].set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseFailed; status.Err = sshconn.ErrAuthFailed })
	state.Wake()
	for _, index := range []int{0, 1} {
		session, _ := state.sessionByIDPointer(sessions[index].ID)
		if index == 0 {
			if session.term == nil || session.term.shell == nil {
				t.Fatalf("pane %d has no shell", index)
			}
			session.term.live = true
		}
	}
	status = state.workspaceConnectionState(workspaceID)
	if status.Pending != 0 || status.Connected != 2 || status.Failed != 2 || status.Closed != 1 {
		t.Fatalf("settled state = %+v", status)
	}
	if !workspaceCanBroadcast(state.workspaceConnectionState(workspaceID)) {
		t.Fatal("a settled workspace with two shells could not broadcast")
	}
}

func TestWorkspaceConnectionStateTreatsShellWaitAsPending(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	plan := WorkspaceLaunchPlan{
		Hosts:  []Host{testWorkspaceHost("web", "web.example.com"), testWorkspaceHost("db", "db.example.com")},
		Layout: layoutcore.GridSpec(2),
	}
	workspaceID, sessions, err := state.launchWorkspace(plan, "Shells")
	if err != nil {
		t.Fatal(err)
	}
	for _, connection := range world.conns {
		connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	}
	state.Wake()
	status := state.workspaceConnectionState(workspaceID)
	if status.Pending != 2 || status.Connected != 0 {
		t.Fatalf("shell wait state = %+v", status)
	}

	for _, launched := range sessions {
		session, _ := state.sessionByIDPointer(launched.ID)
		if session.term == nil || session.term.shell == nil {
			t.Fatalf("session %d has no shell", launched.ID)
		}
		session.term.live = true
	}
	if !workspaceCanBroadcast(state.workspaceConnectionState(workspaceID)) {
		t.Fatal("two live shells could not broadcast")
	}
}

func TestWorkspaceEmptyPaneDoesNotBlockSettledBroadcast(t *testing.T) {
	state, workspaceID, _ := launchWorkspaceWithConnections(t, []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	})
	emptyPane, err := state.deck.Insert(workspaceID, state.deck.Panes(workspaceID, nil)[0].ID, layoutcore.Horizontal, 0.5, layoutcore.After)
	if err != nil {
		t.Fatal(err)
	}
	panes := state.deck.Panes(workspaceID, nil)
	if len(panes) != 3 || panes[0].SessionID == 0 && panes[1].SessionID == 0 ||
		panes[1].SessionID == 0 && panes[2].SessionID == 0 ||
		panes[0].SessionID != 0 && panes[1].SessionID != 0 && panes[2].SessionID != 0 {
		t.Fatalf("panes after inserting an empty one = %+v", panes)
	}
	if pane, ok := state.deck.Pane(workspaceID, emptyPane); !ok || pane.SessionID != 0 {
		t.Fatalf("inserted pane = %+v", pane)
	}
	status := state.workspaceConnectionState(workspaceID)
	if status.Empty != 1 || status.Pending != 0 || status.Connected != 2 {
		t.Fatalf("status = %+v", status)
	}
	if !workspaceCanBroadcast(status) {
		t.Fatal("an empty pane blocked two settled shells from broadcasting")
	}
}

func TestManagedWorkspaceHeaderAndVerticalTabsReserveSeparateEdges(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.setTabOrientation(tabsVertical)
	workspaceID := launchTemporaryWorkspace(t, state, 0, 1)
	size := gfx.Size{Width: 1400, Height: 900}

	bounds := state.workspacePixelBoundsFor(workspaceID, size)
	if want := state.verticalTabsReserve(size); bounds.X != want {
		t.Fatalf("workspace left = %v, want vertical-tab reserve %v", bounds.X, want)
	}
	if bounds.Y != windowBarHeight+workspaceHeaderHeight {
		t.Fatalf("workspace top = %v, want below the header", bounds.Y)
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	header, ok := hitByID(&canvas, hitWorkspaceHeaderLayout)
	if !ok {
		t.Fatal("managed workspace header is missing")
	}
	if header.Bounds.X < bounds.X || header.Bounds.Y+header.Bounds.Height > bounds.Y {
		t.Fatalf("header %+v overlaps the pane bounds %+v", header.Bounds, bounds)
	}
}
