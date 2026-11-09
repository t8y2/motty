package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
)

func newTabState(t *testing.T) *State {
	t.Helper()
	state := newTestState(t, testOptions(t))
	openEmptyTab(state)
	return state
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
	state.Click(hitNewTabHostEdit.At(0))
	if state.modal != modalNewHost || state.hostDialogMode != hostDialogEdit || state.hostDialogID != host.ID {
		t.Fatalf("modal %d mode %d id %d: the pencil must open host %d for editing", state.modal, state.hostDialogMode, state.hostDialogID, host.ID)
	}
	state.modal = modalNone
	state.Click(hitNewTabHostNew)
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
	state.Click(hitNewTabHostList.At(0))
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
	state.Click(hitNewTabLocal)
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
