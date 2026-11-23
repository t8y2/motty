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

// The panel offers expand-all and collapse-all beside its search box. Each acts
// on every group through the state the hosts page shares, and offers itself only
// while it would change something: a search holds the groups open on its own.
func TestHostsPanelExpandsAndCollapsesEveryGroup(t *testing.T) {
	state := hostsPanelFixture(t)
	offered := func(id gfx.ID) (found, disabled bool) {
		canvas, _ := paintedTexts(state)
		hit, found := hitByID(canvas, id)
		return found, hit.Disabled
	}
	state.setHostGroupsExpanded(true)
	if found, disabled := offered(hitHostsPanelExpand); !found || !disabled {
		t.Errorf("with every group open expand all: found=%v disabled=%v, want found and disabled", found, disabled)
	}
	if found, disabled := offered(hitHostsPanelCollapse); !found || disabled {
		t.Errorf("with every group open collapse all: found=%v disabled=%v, want enabled", found, disabled)
	}
	open := len(state.hostsPanel.rows)
	state.setHostGroupsExpanded(false)
	state.Paint(&gfx.Canvas{}, panelWide)
	if len(state.hostsPanel.rows) >= open {
		t.Errorf("collapsing left %d rows, was %d", len(state.hostsPanel.rows), open)
	}
	if found, disabled := offered(hitHostsPanelExpand); !found || disabled {
		t.Errorf("with every group closed expand all: found=%v disabled=%v, want enabled", found, disabled)
	}
	if _, disabled := offered(hitHostsPanelCollapse); !disabled {
		t.Error("with every group closed collapse all is still offered")
	}
	state.setHostGroupsExpanded(true)
	state.hostsPanel.search.SetText("alp")
	state.hostsPanel.stale = true
	if _, disabled := offered(hitHostsPanelCollapse); !disabled {
		t.Error("collapse all is offered while a search holds the groups open")
	}
}

// The panel's search box offers a clear button when text is present.
func TestHostsPanelSearchClearButton(t *testing.T) {
	state := hostsPanelFixture(t)
	clear := func() (gfx.Hit, bool) {
		canvas, _ := paintedTexts(state)
		return hitByLabelAny(canvas, state.messages.SearchClear)
	}
	if _, ok := clear(); ok {
		t.Fatal("clear button visible on empty search")
	}

	state.hostsPanel.search.SetText("gam")
	state.hostsPanel.stale = true
	state.Paint(&gfx.Canvas{}, panelWide)
	button, ok := clear()
	if !ok {
		t.Fatal("clear button missing when search has text")
	}

	clickInEventPass(t, state, panelWide, button.ID)
	if state.hostsPanel.search.Text() != "" {
		t.Fatalf("clear button left text: %q", state.hostsPanel.search.Text())
	}
	if _, ok := hostsPanelRowOf(state, "alpha"); !ok {
		t.Error("clearing panel search did not restore alpha")
	}
	if _, ok := hostsPanelRowOf(state, "gamma"); !ok {
		t.Error("clearing panel search did not keep gamma")
	}

	if _, ok := clear(); ok {
		t.Fatal("clear button still present after clearing")
	}
}

func TestHostsPanelConnectTwiceDoesNotLimitTabs(t *testing.T) {
	state := hostsPanelFixture(t)
	initial := len(state.sessions)
	host := state.hosts[0]
	// First connect from session manager opens a session.
	state.connectHostsPanelHost(host)
	if len(state.sessions) != initial+1 {
		t.Fatalf("first connect: sessions=%d, want %d", len(state.sessions), initial+1)
	}
	firstID := state.activeSessionID

	// Second connect from session manager: user double clicks the same host again
	state.connectHostsPanelHost(host)
	if len(state.sessions) != initial+2 {
		t.Fatalf("second connect: sessions=%d, want %d (tabs must not be limited)", len(state.sessions), initial+2)
	}
	if state.activeSessionID == firstID {
		t.Fatalf("second connect: active session=%d, want new session", state.activeSessionID)
	}
}

func TestHostsPanelConnectReusesWhenEnabled(t *testing.T) {
	state := hostsPanelFixture(t)
	state.startupReuse = true
	initial := len(state.sessions)
	host := state.hosts[0]
	state.connectHostsPanelHost(host)
	if len(state.sessions) != initial+1 {
		t.Fatalf("first connect: sessions=%d, want %d", len(state.sessions), initial+1)
	}
	firstID := state.activeSessionID

	// Second connect reuses the existing session when startupReuse is enabled.
	state.connectHostsPanelHost(host)
	if len(state.sessions) != initial+1 {
		t.Fatalf("second connect: sessions=%d, want %d (must reuse)", len(state.sessions), initial+1)
	}
	if state.activeSessionID != firstID {
		t.Fatalf("second connect: active session=%d, want %d", state.activeSessionID, firstID)
	}
}
