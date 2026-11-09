package workbench

import (
	"errors"
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestLaunchTwelveHostsCreatesOneWorkspaceAndDialsEveryHost(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	hosts := make([]Host, layoutcore.MaxPanes)
	for index := range hosts {
		hosts[index] = testWorkspaceHost(fmt.Sprintf("server-%d", index), fmt.Sprintf("server-%d.example.com", index))
	}
	workspaceID, sessions, err := state.launchWorkspace(WorkspaceLaunchPlan{Hosts: hosts, Layout: layoutcore.GridSpec(3)}, "Fleet")
	if err != nil {
		t.Fatal(err)
	}
	if len(sessions) != layoutcore.MaxPanes || len(state.deck.Panes(workspaceID, nil)) != layoutcore.MaxPanes || len(world.configs) != layoutcore.MaxPanes {
		t.Fatalf("sessions=%d panes=%d dials=%d", len(sessions), len(state.deck.Panes(workspaceID, nil)), len(world.configs))
	}
	if err := state.deck.Validate(); err != nil {
		t.Fatal(err)
	}
	restored, err := layoutcore.RestoreDeck(state.deck.DTO())
	if err != nil || len(restored.Panes(workspaceID, nil)) != layoutcore.MaxPanes {
		t.Fatalf("restored panes=%d err=%v", len(restored.Panes(workspaceID, nil)), err)
	}
	size := gfx.Size{Width: 1600, Height: 1100}
	state.Resize(size)
	canvas, _ := paintAt(state, size)
	frame := state.workspacePixelBounds(size)
	for index, pane := range state.Panes(nil) {
		session, ok := state.sessionByIDPointer(pane.SessionID)
		if !ok {
			t.Fatalf("pane %d lost its session", index)
		}
		terminalID := tree.MountID(paneMount(session), hitTerminalList.At(int(session.ID)))
		terminal, ok := hitByID(canvas, terminalID)
		bounds := pixelBounds(frame, pane.Bounds)
		if !ok || terminal.Bounds.Width <= 0 || terminal.Bounds.Height <= 0 ||
			terminal.Bounds.X < bounds.X || terminal.Bounds.Y < bounds.Y ||
			terminal.Bounds.X+terminal.Bounds.Width > bounds.X+bounds.Width ||
			terminal.Bounds.Y+terminal.Bounds.Height > bounds.Y+bounds.Height {
			t.Fatalf("pane %d terminal %+v escapes bounds %+v (found=%v)", index, terminal.Bounds, bounds, ok)
		}
	}
}

func TestLaunchWorkspaceInstallsEveryStructureBeforeDialing(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	catalog := []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
		testWorkspaceHost("cache", "cache.example.com"),
	}
	plan, err := PlanWorkspaceLaunch(catalog, []string{"host_cache", "host_web", "host_db"}, layoutcore.GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	world.onDial = func(call int) {
		workspaceID := state.deck.ActiveWorkspace()
		panes := state.deck.Panes(workspaceID, nil)
		if len(panes) != 3 {
			t.Errorf("dial %d saw %d panes", call, len(panes))
		}
		bound := 0
		for _, pane := range panes {
			if pane.SessionID != 0 {
				bound++
			}
		}
		if bound != 3 {
			t.Errorf("dial %d saw %d bound panes", call, bound)
		}
	}
	workspaceID, sessions, err := state.launchWorkspace(plan, "Operations")
	if err != nil {
		t.Fatal(err)
	}
	if workspaceID == 0 || len(sessions) != 3 {
		t.Fatalf("workspace = %d, sessions = %d", workspaceID, len(sessions))
	}
	panes := state.deck.Panes(workspaceID, nil)
	if len(panes) != 3 || panes[0].SessionID != layoutcore.SessionID(sessions[0].ID) ||
		panes[1].SessionID != layoutcore.SessionID(sessions[1].ID) ||
		panes[2].SessionID != layoutcore.SessionID(sessions[2].ID) {
		t.Fatalf("panes = %+v", panes)
	}
	if len(world.configs) != 3 {
		t.Fatalf("dialed %d connections", len(world.configs))
	}
	if state.deck.ActiveWorkspace() != workspaceID || state.activeSection != sectionTerminal {
		t.Fatalf("active workspace = %d, section = %d", state.deck.ActiveWorkspace(), state.activeSection)
	}
	layout, _ := state.deck.Layout(workspaceID)
	assertGridRects(t, layout, []gridRect{
		{0, 0, 0.5, 0.5}, {0.5, 0, 0.5, 0.5},
		{0, 0.5, 1, 0.5},
	})
	if err := state.deck.Validate(); err != nil {
		t.Fatal(err)
	}
}

func TestFailedWorkspaceStructureDoesNotDial(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.saveBlocked = false
	state.pauseSave = 0
	plan := WorkspaceLaunchPlan{Hosts: []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
	}, Layout: layoutcore.GridSpec(2)}
	for index := 0; index < layoutcore.MaxWorkspaces; index++ {
		if _, _, err := state.deck.Create("Filler"); err != nil {
			t.Fatal(err)
		}
	}
	if _, _, err := state.launchWorkspace(plan, "Too many"); err == nil {
		t.Fatal("launch succeeded with a full deck")
	}
	if len(world.configs) != 0 {
		t.Fatalf("structural failure dialed %d connections", len(world.configs))
	}
	if len(state.sessions) != 0 {
		t.Fatalf("structural failure left %d sessions", len(state.sessions))
	}
}

func TestLaunchWorkspaceRejectsAnInvalidPlanBeforeCreatingAnything(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	before := len(state.deck.Workspaces(nil))
	tests := []struct {
		name string
		plan WorkspaceLaunchPlan
	}{
		{"empty", WorkspaceLaunchPlan{Layout: layoutcore.GridSpec(1)}},
		{"too many", WorkspaceLaunchPlan{Hosts: make([]Host, layoutcore.MaxPanes+1), Layout: layoutcore.GridSpec(2)}},
		{"columns", WorkspaceLaunchPlan{Hosts: []Host{testWorkspaceHost("web", "web.example.com")}, Layout: layoutcore.GridSpec(4)}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			if _, _, err := state.launchWorkspace(test.plan, "Bad"); err == nil {
				t.Fatal("invalid plan succeeded")
			}
			if len(state.sessions) != 0 || len(state.deck.Workspaces(nil)) != before || len(world.configs) != 0 {
				t.Fatalf("sessions=%d workspaces=%d dials=%d", len(state.sessions), len(state.deck.Workspaces(nil)), len(world.configs))
			}
		})
	}
}

func TestLaunchWorkspaceStartsConnectionsInHostOrder(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	catalog := []Host{
		testWorkspaceHost("web", "web.example.com"),
		testWorkspaceHost("db", "db.example.com"),
		testWorkspaceHost("cache", "cache.example.com"),
	}
	plan, err := PlanWorkspaceLaunch(catalog, []string{"host_cache", "host_web", "host_db"}, layoutcore.GridSpec(3))
	if err != nil {
		t.Fatal(err)
	}
	if _, _, err := state.launchWorkspace(plan, "Ordered"); err != nil {
		t.Fatal(err)
	}
	got := make([]string, len(world.configs))
	for index := range world.configs {
		got[index] = world.configs[index].Host
	}
	want := []string{"web.example.com", "db.example.com", "cache.example.com"}
	for index := range want {
		if got[index] != want[index] {
			t.Fatalf("dial order = %v, want %v", got, want)
		}
	}
}

func TestLaunchWorkspaceUsesOneDeckTabAndLeavesBroadcastOff(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	catalog := []Host{testWorkspaceHost("web", "web.example.com"), testWorkspaceHost("db", "db.example.com")}
	plan, err := PlanWorkspaceLaunch(catalog, []string{"host_web", "host_db"}, layoutcore.GridSpec(2))
	if err != nil {
		t.Fatal(err)
	}
	tabs := len(state.deck.Workspaces(nil))
	workspaceID, _, err := state.launchWorkspace(plan, "Pair")
	if err != nil {
		t.Fatal(err)
	}
	if len(state.deck.Workspaces(nil)) != tabs+1 || state.deck.ActiveWorkspace() != workspaceID {
		t.Fatalf("tabs %d -> %d, active=%d", tabs, len(state.deck.Workspaces(nil)), state.deck.ActiveWorkspace())
	}
	if state.broadcast != 0 {
		t.Fatalf("broadcast enabled on launch: %d", state.broadcast)
	}
	for index := range state.sessions {
		if state.sessions[index].phase == sshconn.PhaseConnected {
			t.Fatalf("fake dial reported connected before the test applied a status")
		}
	}
}

func TestLaunchWorkspaceSaveFailureLeavesNoTabSessionOrIdentity(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	previous, _, err := state.deck.Create("Existing")
	if err != nil {
		t.Fatal(err)
	}
	if !state.deck.ActivateWorkspace(previous) {
		t.Fatal("activate existing workspace")
	}
	state.activeSection = sectionWorkspaces
	state.nextSessionID = 41
	state.activeSessionID = 0
	state.broadcast = previous
	config := state.config
	workspacesBefore := len(state.deck.Workspaces(nil))
	sessionsBefore := len(state.sessions)
	pauseSaveBefore := state.pauseSave

	plan := WorkspaceLaunchPlan{
		Hosts: []Host{
			testWorkspaceHost("web", "web.example.com"),
			testWorkspaceHost("db", "db.example.com"),
		},
		Layout: layoutcore.GridSpec(2),
	}
	state.config = nil
	_, _, err = state.launchWorkspaceWithIdentity(plan, "Rollback", workspaceLaunchIdentity{
		managedLayout: layoutcore.GridSpec(2), temporary: true, temporaryLayout: layoutcore.GridSpec(2),
	})
	if !errors.Is(err, ErrWorkspaceLaunchSave) {
		t.Fatalf("launch error = %v", err)
	}
	if len(world.configs) != 0 {
		t.Fatalf("failed save dialed %d connections", len(world.configs))
	}
	if len(state.deck.Workspaces(nil)) != workspacesBefore || len(state.sessions) != sessionsBefore {
		t.Fatalf("failed save left workspaces=%d sessions=%d, want %d and %d",
			len(state.deck.Workspaces(nil)), len(state.sessions), workspacesBefore, sessionsBefore)
	}
	if state.deck.ActiveWorkspace() != previous || state.activeSection != sectionWorkspaces ||
		state.nextSessionID != 41 || state.activeSessionID != 0 || state.broadcast != previous {
		t.Fatalf("rollback active=%d section=%d next=%d session=%d broadcast=%d",
			state.deck.ActiveWorkspace(), state.activeSection, state.nextSessionID,
			state.activeSessionID, state.broadcast)
	}
	if state.pauseSave != pauseSaveBefore {
		t.Fatalf("rollback pauseSave = %d, want %d", state.pauseSave, pauseSaveBefore)
	}
	if len(state.managedWorkspaceLayouts) != 0 || len(state.temporaryWorkspaceLayouts) != 0 {
		t.Fatalf("rollback left identity managed=%v temporary=%v",
			state.managedWorkspaceLayouts, state.temporaryWorkspaceLayouts)
	}

	state.config = config
	retry := WorkspaceLaunchPlan{
		Hosts:  []Host{testWorkspaceHost("cache", "cache.example.com")},
		Layout: layoutcore.GridSpec(1),
	}
	if _, _, err := state.launchWorkspace(retry, "Retry"); err != nil {
		t.Fatalf("launch after rollback: %v", err)
	}
	if state.pauseSave != pauseSaveBefore {
		t.Fatalf("retry pauseSave = %d, want %d", state.pauseSave, pauseSaveBefore)
	}
}
