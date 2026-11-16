package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
)

func newTabState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	openEmptyTab(state)
	return state
}

// newTabClick delivers a click on id in an event-pass paint, as the window does:
// the page's controls read the click while they are declared.
func newTabClick(t *testing.T, state *State, id gfx.ID) {
	t.Helper()
	var canvas gfx.Canvas
	canvas.BeginEventPass(id, gfx.ClickInfo{})
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	canvas.EndEventPass()
}

func TestNewTabPageOffersThisComputerAndTheSavedHosts(t *testing.T) {
	state := newTabState(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitNewTabLocal); !ok {
		t.Fatal("no row for this computer")
	}
	for index := range min(len(state.hosts), newTabMaxHosts) {
		if _, ok := hitByID(&canvas, hitNewTabHostList.At(index)); !ok {
			t.Errorf("no row for host %d", index)
		}
	}
	if _, ok := hitByID(&canvas, hitPaneChooseHost); !ok {
		t.Fatal("no way to the full host list")
	}
}

// TestNewTabPageEditsAndAddsHosts checks the pencil and the footer's add
// button: the first opens the row's host for editing, the second adds one.
func TestNewTabPageEditsAndAddsHosts(t *testing.T) {
	state := newTabState(t)
	before := len(state.deck.Workspaces(nil))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitNewTabHostEdit.At(0)); !ok {
		t.Fatal("the host's row has no pencil")
	}
	if _, ok := hitByID(&canvas, hitNewTabHostNew); !ok {
		t.Fatal("the footer has no way to add a host")
	}
	host := state.hosts[0]
	newTabClick(t, state, hitNewTabHostEdit.At(0))
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogEdit || state.hostDialogID != host.ID {
		t.Fatalf("modal %d mode %d id %d: the pencil must open host %d for editing", state.modal, state.hostDialogMode, state.hostDialogID, host.ID)
	}
	state.modal = modalNone
	newTabClick(t, state, hitNewTabHostNew)
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogAdd {
		t.Fatalf("modal %d mode %d: the footer button must open the add dialog", state.modal, state.hostDialogMode)
	}
	if len(state.deck.Workspaces(nil)) != before {
		t.Fatalf("tabs %d -> %d: opening the dialogs must create no tab", before, len(state.deck.Workspaces(nil)))
	}
}

func TestNewTabPageConnectsAHostInThisTab(t *testing.T) {
	state := newTabState(t)
	before := len(state.workspaceTabs)
	workspace := state.deck.ActiveWorkspace()
	host := state.hosts[0]
	newTabClick(t, state, hitNewTabHostList.At(0))
	if len(state.workspaceTabs) != before || state.deck.ActiveWorkspace() != workspace {
		t.Fatalf("tabs %d -> %d: choosing a host must connect in the tab it was chosen in", before, len(state.workspaceTabs))
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.HostID != host.ID {
		t.Fatalf("active session = %+v, want host %d", session, host.ID)
	}
	if state.activeSection != sectionTerminal {
		t.Fatalf("section = %v", state.activeSection)
	}
}

func TestNewTabPageOpensALocalShellInThisTab(t *testing.T) {
	state := newTabState(t)
	before := len(state.workspaceTabs)
	newTabClick(t, state, hitNewTabLocal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.isLocal() || len(state.workspaceTabs) != before {
		t.Fatalf("local session ok=%v tabs %d -> %d", ok, before, len(state.workspaceTabs))
	}
}

func TestNewTabPageFitsASmallPane(t *testing.T) {
	state := newTabState(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 700, Height: 420})
	for _, id := range []gfx.ID{hitNewTabLocal, hitPaneChooseHost} {
		hit, ok := hitByID(&canvas, id)
		if !ok || hit.Bounds.Y+hit.Bounds.Height > 420 || hit.Bounds.X < 0 || hit.Bounds.X+hit.Bounds.Width > 700 {
			t.Fatalf("control %d outside a 700x420 window: %+v, %v", id, hit.Bounds, ok)
		}
	}
}

// TestNewTabPageClicksThroughTheDriver drives the real event pass: the page's
// Local Terminal row reads the click while it is declared.
func TestNewTabPageClicksThroughTheDriver(t *testing.T) {
	state := newTabState(t)
	var canvas gfx.Canvas
	driver, err := app.NewDriver(app.Config{
		Title: "new tab", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, &canvas)
	if err != nil {
		t.Fatal(err)
	}
	defer driver.Close()
	var row gfx.Hit
	found := false
	for _, hit := range driver.Snapshot().Hits {
		if hit.ID == hitNewTabLocal {
			row, found = hit, true
			break
		}
	}
	if !found {
		t.Fatal("the new-tab page shows no Local Terminal row")
	}
	if err := driver.Click(row.Bounds.X+row.Bounds.Width/2, row.Bounds.Y+row.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || !session.isLocal() {
		t.Fatalf("active session = %+v, want the local terminal the row opened", session)
	}
}
