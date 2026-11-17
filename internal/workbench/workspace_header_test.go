package workbench

import (
	"fmt"
	"slices"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sshconn"
	layoutcore "motty/internal/workspace"
)

func TestWorkspaceHeaderAppearsOnlyForManagedWorkspaces(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)

	connect(t, state, world)
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	for _, id := range []gfx.ID{
		hitWorkspaceHeaderBack,
		hitWorkspaceHeaderBroadcast,
		hitWorkspaceHeaderLayout,
	} {
		if _, ok := hitByID(canvas, id); ok {
			t.Fatalf("an ordinary session paints workspace header control %d", id)
		}
	}

	workspaceID := launchTemporaryWorkspace(t, state, 0, 1)
	canvas, texts := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	for _, id := range []gfx.ID{
		hitWorkspaceHeaderBack,
		hitWorkspaceHeaderBroadcast,
		hitWorkspaceHeaderLayout,
	} {
		panelHit(t, canvas, id)
	}
	if !hasText(texts, fmt.Sprintf(state.messages.WorkspaceHeaderPendingFmt, 2)) ||
		!hasText(texts, state.workspaceTitle(workspaceID)) {
		t.Fatalf("managed workspace header texts = %q", texts)
	}
	if hit := panelHit(t, canvas, hitWorkspaceHeaderBroadcast); !hit.Disabled {
		t.Fatalf("broadcast is enabled before the panes connect: %+v", hit)
	}
}

func TestWorkspaceHeaderBackKeepsSessionsAndLeavesBroadcastOff(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	workspaceID := launchTemporaryWorkspace(t, state, 0, 1)
	for _, connection := range world.conns {
		finishWorkspaceConnection(t, state, connection)
	}
	state.broadcast = workspaceID
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	sessions := len(state.sessions)
	panes := len(state.deck.Panes(workspaceID, nil))

	state.Click(hitWorkspaceHeaderBack)

	if state.activeSection != sectionWorkspaces || state.broadcast != 0 ||
		state.status != state.messages.Tools.StatusBroadcastOff {
		t.Fatalf("back left section=%d broadcast=%d status=%q", state.activeSection, state.broadcast, state.status)
	}
	if len(state.sessions) != sessions || len(state.deck.Panes(workspaceID, nil)) != panes {
		t.Fatalf("back changed sessions=%d/%d panes=%d/%d", len(state.sessions), sessions,
			len(state.deck.Panes(workspaceID, nil)), panes)
	}
}

func TestWorkspaceHeaderLayoutAndOrderPreserveSessionsAndTemplate(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	template := mustWorkspaceTemplate(t, "Operations", workspaceCatalogIDs(state, 0, 1, 2), 3)
	state.templates = []WorkspaceTemplate{template}
	state.activeSection = sectionWorkspaces
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateOpen.At(0))
	workspaceID := state.deck.ActiveWorkspace()
	if len(world.configs) != 3 {
		t.Fatalf("template launched %d connections", len(world.configs))
	}
	active := state.activeSessionID
	sessions := append([]Session(nil), state.sessions...)

	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceHeaderLayout)
	canvas, _ = paintAt(state, gfx.Size{Width: 1400, Height: 900})
	panelHit(t, canvas, hitWorkspaceHeaderPresets.At(1))
	state.Click(hitWorkspaceHeaderPresets.At(1))

	if !isGrid(state.managedWorkspaceLayouts[workspaceID], 2) || state.activeSessionID != active {
		t.Fatalf("layout changed columns=%+v active=%+v/%+v",
			state.managedWorkspaceLayouts[workspaceID], state.activeSessionID, active)
	}
	if !state.templateUnchanged(template) {
		t.Fatalf("layout changed template: %+v", state.templates[0])
	}
	if ids := sessionIDs(state.sessions); !slices.Equal(ids, sessionIDs(sessions)) {
		t.Fatalf("layout changed sessions %v -> %v", sessionIDs(sessions), ids)
	}

	state.Click(hitWorkspaceHeaderLayoutDismiss)
	state.Click(hitWorkspaceHeaderLayout)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	before := paneSessionIDs(state.deck.Panes(workspaceID, nil))
	activeIndex := slices.Index(before, active)
	target := 0
	if activeIndex == 0 {
		target = 1
	}
	state.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: activeIndex}, target)

	after := paneSessionIDs(state.deck.Panes(workspaceID, nil))
	if state.activeSessionID != active || slices.Equal(before, after) {
		t.Fatalf("move earlier active=%d/%d panes=%v -> %v", state.activeSessionID, active, before, after)
	}
	if !state.templateUnchanged(template) {
		t.Fatalf("reorder changed template: %+v", state.templates[0])
	}
}

func TestWorkspaceHeaderReapplyRestoresSavedOrderAndColumns(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	template := mustWorkspaceTemplate(t, "Production", workspaceCatalogIDs(state, 2, 0, 1), 2)
	state.templates = []WorkspaceTemplate{template}
	state.activeSection = sectionWorkspaces
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateOpen.At(0))
	workspaceID := state.deck.ActiveWorkspace()

	if got, want := paneHostAddresses(state, workspaceID), []string{
		"192.168.1.1", "prod.example.com", "staging.example.com",
	}; !slices.Equal(got, want) {
		t.Fatalf("initial template order = %v, want %v", got, want)
	}
	state.Click(hitWorkspaceHeaderLayout)
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceHeaderPresets.At(0))
	state.moveWorkspaceCard(workspaceCardRegion{view: workspaceCardsRuntime, index: 0}, 1)

	world.configs = nil
	state.activeSection = sectionWorkspaces
	paintAt(state, gfx.Size{Width: 1400, Height: 900})
	state.Click(hitWorkspaceTemplateOpen.At(0))
	reapplied := state.deck.ActiveWorkspace()
	if reapplied == workspaceID || !isGrid(state.managedWorkspaceLayouts[reapplied], 2) {
		t.Fatalf("reapplied workspace=%+v columns=%+v", reapplied, state.managedWorkspaceLayouts[reapplied])
	}
	if got, want := paneHostAddresses(state, reapplied), []string{
		"192.168.1.1", "prod.example.com", "staging.example.com",
	}; !slices.Equal(got, want) {
		t.Fatalf("reapplied order = %v, want saved %v", got, want)
	}
	if !state.templateUnchanged(template) {
		t.Fatalf("reapply changed template: %+v", state.templates[0])
	}
}

func TestWorkspaceHeaderNarrowWindowKeepsControlsInsideBounds(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	launchTemporaryWorkspace(t, state, 0, 1, 2)
	size := gfx.Size{Width: 420, Height: 620}
	canvas, _ := paintAt(state, size)
	for _, id := range []gfx.ID{
		hitWorkspaceHeaderBack,
		hitWorkspaceHeaderBroadcast,
		hitWorkspaceHeaderLayout,
	} {
		hit := panelHit(t, canvas, id)
		if hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > size.Width ||
			hit.Bounds.Y < windowBarHeight || hit.Bounds.Y+hit.Bounds.Height > windowBarHeight+workspaceHeaderHeight {
			t.Fatalf("narrow header control %d is outside its bar: %+v", id, hit.Bounds)
		}
	}
}

func finishWorkspaceConnection(t *testing.T, state *State, connection *fakeConn) {
	t.Helper()
	connection.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	state.Wake()
	for index := range state.sessions {
		if state.sessions[index].phase == sshconn.PhaseConnected && state.sessions[index].term != nil {
			state.sessions[index].term.live = true
		}
	}
}

func (state *State) templateUnchanged(template WorkspaceTemplate) bool {
	for _, saved := range state.templates {
		if saved.ID == template.ID {
			return saved.Name == template.Name &&
				saved.Columns == template.Columns &&
				slices.Equal(saved.HostIDs, template.HostIDs)
		}
	}
	return false
}

func sessionIDs(sessions []Session) []uint32 {
	ids := make([]uint32, len(sessions))
	for index := range sessions {
		ids[index] = sessions[index].ID
	}
	return ids
}

func paneSessionIDs(panes []layoutcore.Pane) []uint32 {
	ids := make([]uint32, len(panes))
	for index := range panes {
		ids[index] = uint32(panes[index].SessionID)
	}
	return ids
}

func paneHostAddresses(state *State, workspaceID layoutcore.WorkspaceID) []string {
	addresses := make([]string, 0, maxWorkspaceTemplateHosts)
	for _, pane := range state.deck.Panes(workspaceID, nil) {
		session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
		if ok {
			addresses = append(addresses, session.host.Address)
		}
	}
	return addresses
}
