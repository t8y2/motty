package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

// hostsPanelFixture is a window on a terminal with two saved hosts in a group
// and one outside it, and the session manager open.
func hostsPanelFixture(t *testing.T) *State {
	t.Helper()
	state, _ := panelState(t, nil)
	state.hosts = []Host{
		{ID: 1, CatalogID: "a", Label: "alpha", Address: "alpha.invalid", Port: 22, User: "ops", Protocol: protocolSSH, Group: "prod"},
		{ID: 2, CatalogID: "b", Label: "beta", Address: "beta.invalid", Port: 22, User: "ops", Protocol: protocolSSH, Group: "prod"},
		{ID: 3, CatalogID: "c", Label: "gamma", Address: "gamma.invalid", Port: 22, User: "ops", Protocol: protocolSSH},
	}
	for index := range state.hosts {
		state.hosts[index].finish()
	}
	state.refreshHostGroups()
	state.visible = nil
	state.Click(hitHostsPanel)
	state.Paint(&gfx.Canvas{}, panelWide)
	return state
}

func hostsPanelRowOf(state *State, label string) (int, bool) {
	for index, entry := range state.hostsPanel.rows {
		if entry.Kind == hostTreeHost && state.hosts[entry.HostIndex].Label == label {
			return index, true
		}
	}
	return 0, false
}

// The panel takes its room from the terminals and gives it back when closed, and
// a window too narrow to share does not show it at all.
func TestHostsPanelSharesTheContentAreaWithTheTerminals(t *testing.T) {
	state, _ := panelState(t, nil)
	closed := state.baseWorkspacePixelBounds(panelWide)
	state.Click(hitHostsPanel)
	open := state.baseWorkspacePixelBounds(panelWide)
	if !state.hostsPanel.open || open.X <= closed.X || open.Width >= closed.Width {
		t.Fatalf("open panel left the terminals %+v, was %+v", open, closed)
	}
	if got := open.X - closed.X; got != state.hostsPanelExtent(closed.Width) {
		t.Errorf("the terminals start %v in, the panel takes %v", got, state.hostsPanelExtent(closed.Width))
	}
	if open.X+open.Width != closed.X+closed.Width {
		t.Errorf("the panel moved the terminals' far edge: %+v vs %+v", open, closed)
	}
	narrow := gfx.Size{Width: hostsPanelMinFrame - 1, Height: 900}
	if state.hostsPanelExtent(narrow.Width) != 0 {
		t.Error("a window too narrow to share still shows the panel")
	}
	state.Click(hitHostsPanel)
	if again := state.baseWorkspacePixelBounds(panelWide); again != closed {
		t.Errorf("closing left the terminals %+v, want %+v", again, closed)
	}
}

// A double click on a host row opens that host in a tab of its own; a single
// click only selects it.
func TestHostsPanelConnectsAHostOnDoubleClick(t *testing.T) {
	state := hostsPanelFixture(t)
	row, ok := hostsPanelRowOf(state, "beta")
	if !ok {
		t.Fatalf("beta is not listed: %+v", state.hostsPanel.rows)
	}
	target := state.hostsPanel.tree.RowID(row)
	before := len(state.sessions)
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 1, Target: target, Viewport: panelWide})
	if len(state.sessions) != before {
		t.Fatal("a single click connected")
	}
	state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2, Target: target, Viewport: panelWide})
	if len(state.sessions) != before+1 || state.sessions[len(state.sessions)-1].HostID != 2 {
		t.Fatalf("a double click on beta left %d sessions, want %d for host 2", len(state.sessions), before+1)
	}
}

// The panel filters by its own search box: what the hosts page's search says
// does not narrow it, and the panel's search does not narrow the page.
func TestHostsPanelSearchIsItsOwn(t *testing.T) {
	state := hostsPanelFixture(t)
	if _, ok := hostsPanelRowOf(state, "gamma"); !ok {
		t.Fatal("gamma is not listed before any search")
	}
	state.search.Insert("alpha")
	state.Paint(&gfx.Canvas{}, panelWide)
	if _, ok := hostsPanelRowOf(state, "gamma"); !ok {
		t.Error("the hosts page's search narrowed the panel")
	}
	state.search = text.Editor{}
	state.hostsPanel.search.SetText("gam")
	state.hostsPanel.stale = true
	state.Paint(&gfx.Canvas{}, panelWide)
	if _, ok := hostsPanelRowOf(state, "gamma"); !ok {
		t.Error("the panel's search lost the host it matches")
	}
	if _, ok := hostsPanelRowOf(state, "alpha"); ok {
		t.Error("the panel's search kept a host it does not match")
	}
	if len(state.visible) != len(state.hosts) {
		t.Errorf("the panel's search narrowed the hosts page: %d of %d", len(state.visible), len(state.hosts))
	}
}

// The panel's choice of open or closed and its width survive a restart.
func TestHostsPanelStateIsSaved(t *testing.T) {
	state := hostsPanelFixture(t)
	state.hostsPanel.width = 300
	saved := state.snapshot()
	if !saved.HostsPanelOpen || saved.HostsPanelWidth != 300 {
		t.Fatalf("saved %v at %v, want open at 300", saved.HostsPanelOpen, saved.HostsPanelWidth)
	}
}
