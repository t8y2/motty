//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

// The tables of the Keys, Forwards and Proxies pages select a row where the row
// is declared (Table.RowAt), not in the window's Click. These click a real row
// through the headless driver, which is the path a window takes.
func openWorkbenchSession(t *testing.T, state *State) *headless.Session {
	t.Helper()
	session, err := headless.Open(app.Config{
		Title: "tables", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer,
		OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize,
	}, nil, headless.Options{})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { session.Close() })
	return session
}

func TestKeysRowIsSelectedByClickingIt(t *testing.T) {
	plain := writeWorkbenchIdentity(t, "id_plain", "")
	locked := writeWorkbenchIdentity(t, "id_locked", "secret")
	options := freshOptions(t)
	options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{plain, locked}, nil }
	state := newTestState(t, options)
	state.Click(hitKeychain)
	state.Click(hitKeysImport)
	session := openWorkbenchSession(t, state)

	target := state.identities[0]
	state.selectedIdentityID = state.identities[1].ID
	if err := session.ClickLabel(target.cells[0]); err != nil {
		t.Fatal(err)
	}
	if state.selectedIdentityID != target.ID {
		t.Fatalf("selected key = %d, want the clicked one %d", state.selectedIdentityID, target.ID)
	}
}

func TestForwardRowIsSelectedByClickingIt(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addForwardRule(t, state, "Web", 9000, "127.0.0.1", 80)
	addForwardRule(t, state, "Db", 9001, "127.0.0.1", 5432)
	session := openWorkbenchSession(t, state)

	target := state.forwards[0]
	state.selectedForwardID = state.forwards[1].ID
	if err := session.ClickLabel(target.cells[0]); err != nil {
		t.Fatal(err)
	}
	if state.selectedForwardID != target.ID {
		t.Fatalf("selected forward = %d, want the clicked one %d", state.selectedForwardID, target.ID)
	}
}

func TestProxyRowIsSelectedByClickingIt(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	addProxyRoute(t, state, state.hosts[1].ID, state.hosts[2].ID)
	session := openWorkbenchSession(t, state)

	target := state.proxies[0]
	state.selectedProxyID = state.proxies[1].ID
	if err := session.ClickLabel(target.cells[0]); err != nil {
		t.Fatal(err)
	}
	if state.selectedProxyID != target.ID {
		t.Fatalf("selected route = %d, want the clicked one %d", state.selectedProxyID, target.ID)
	}
}

func TestForwardEditButtonOpensDialogInHeadless(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addForwardRule(t, state, "Web", 9000, "127.0.0.1", 80)
	session := openWorkbenchSession(t, state)

	if err := session.ClickLabel(state.messages.ForwardEdit); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNewForward || !state.forwardEditor.editing {
		t.Fatalf("forward modal = %d, editing = %v", state.modal, state.forwardEditor.editing)
	}
}

func TestProxyEditButtonOpensDialogInHeadless(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	session := openWorkbenchSession(t, state)

	if err := session.ClickLabel(state.messages.ProxyEdit); err != nil {
		t.Fatal(err)
	}
	if state.modal != modalNewProxy || !state.proxyEditor.editing {
		t.Fatalf("proxy modal = %d, editing = %v", state.modal, state.proxyEditor.editing)
	}
}

// A secondary press on a host row opens its menu, and a double click connects it,
// through the window's own path: the row's paint takes the event (the event pass).
func TestHostRowTakesRightClickAndDoubleClickThroughTheWindowPath(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	checked := state.hosts[0].ID
	state.Click(hitHostCheckList.At(int(checked)))
	session := openWorkbenchSession(t, state)
	target := state.hosts[1]
	// The row, not its checkbox: both carry the host's name.
	var row gfx.Hit
	for _, hit := range session.Snapshot().Hits {
		if hit.ID == hitHostList.At(int(target.ID)) {
			row = hit
		}
	}
	if row.ID.IsZero() {
		t.Fatalf("no row for %q", target.Label)
	}
	x, y := row.Bounds.X+row.Bounds.Width/4, row.Bounds.Y+row.Bounds.Height/2

	driver := session.Driver()
	if err := driver.Press(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(x, y, gfx.PointerButtonSecondary); err != nil {
		t.Fatal(err)
	}
	if !state.hostMenu.open || state.hostMenu.host != target.ID {
		t.Fatalf("a right click on %q left the menu %+v", target.Label, state.hostMenu)
	}
	if state.hostSelected(target.ID) || !state.hostSelected(checked) {
		t.Fatalf("a right click changed the checked hosts to %v", state.hostSelections)
	}
	state.hostMenu.open = false
	// The flag changed behind the window's back, so repaint once before the
	// next gesture: an application close repaints in the same event turn.
	frame := driver.Frame()
	if err := driver.Resize(int(frame.Width), int(frame.Height)); err != nil {
		t.Fatal(err)
	}

	if err := session.DoubleClick(x, y); err != nil {
		t.Fatal(err)
	}
	if len(world.configs) != 1 {
		t.Fatalf("a double click on %q dialed %d connections, want 1", target.Label, len(world.configs))
	}
}
