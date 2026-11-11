//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
)

// The transfer window's two panes are keyed containers of the window page now,
// not pages drawn into it. Their Lists are owned and routed by that page, and the
// details painted after it read the side they belong to even when the panes swap.
func TestSFTPWindowPanePagesAreGone(t *testing.T) {
	for _, name := range []string{"sftp_window_ui.go", "sftp_window_input.go"} {
		source, err := os.ReadFile(name)
		if err != nil {
			t.Fatal(err)
		}
		for _, banned := range []string{"panePages", "pageKey", ".Key(event)"} {
			if strings.Contains(string(source), banned) {
				t.Fatalf("%s still contains the pane page bridge %q", name, banned)
			}
		}
	}
	source, err := os.ReadFile("sftp_window_ui.go")
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(source), "KeyInt(side)") {
		t.Fatal("the panes are not declared as keyed containers of the window page")
	}
}

func TestSFTPWindowPanesAreKeyedContainersOfTheWindowPage(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	for side, local := range []bool{true, false} {
		folder := w.folder(local)
		if folder.listRowsScope.IsZero() {
			t.Fatalf("side %d has no declared List rows", side)
		}
		row := w.rowID(side, 0)
		if !tree.Within(row.Scope(), tree.Feature(w.pageRoot())) {
			t.Fatalf("side %d row %v is not in the window page's tree", side, row)
		}
	}
	// Remote on the left swaps the pane order. Each side keeps its own slots, so
	// the remote address and list stay on the left while the local ones move
	// right; one shared scratch would paint both details with the last pane.
	state.sftpWindowOrder = sftpRemoteLeft
	var canvas gfx.Canvas
	w.paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if w.ui.panes[0].local != true || w.ui.panes[1].local != false {
		t.Fatalf("pane scratch sides = %t, %t", w.ui.panes[0].local, w.ui.panes[1].local)
	}
	if !(w.ui.list[1].X < w.ui.list[0].X) {
		t.Fatalf("remote list is not left of the local list: remote %+v local %+v", w.ui.list[1], w.ui.list[0])
	}
	remoteAddress, localAddress := w.ui.panes[1].address.Rect(), w.ui.panes[0].address.Rect()
	if !(remoteAddress.X < localAddress.X) {
		t.Fatalf("remote address is not left of the local address: remote %+v local %+v", remoteAddress, localAddress)
	}
}

// Arrows, focus and Enter reach the focused pane's List through the window page,
// and the submit in the page opens the row once instead of the window's own
// pre-handler opening it again.
func TestSFTPWindowPaneKeyboardGoesThroughTheWindowPage(t *testing.T) {
	state, w, _, host, localDir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if err := view.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	config := w.config()

	local, remote := w.folder(true), w.folder(false)
	if len(local.rows) < 2 || len(remote.rows) < 2 {
		t.Fatalf("panes did not declare enough rows: local %d remote %d", len(local.rows), len(remote.rows))
	}
	remoteBefore := remote.selected

	// The window page routes a key outside the app's own event loop here, so the
	// repaint its result asks for has to be requested explicitly.
	repaint := func() {
		w.window.Invalidate()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		view.Rest()
	}

	down := config.OnKey(gfx.KeyEvent{Target: w.rowID(0, 0), Key: gfx.KeyDown})
	if !down.Handled || down.Focus != w.rowID(0, 1) || !down.FocusVisible {
		t.Fatalf("local Down = %+v, want focus %v", down, w.rowID(0, 1))
	}
	repaint()
	dualWait(t, state, w, view, func() bool { return local.selected == 1 })
	if remote.selected != remoteBefore {
		t.Fatalf("local Down moved the remote selection: %d -> %d", remoteBefore, remote.selected)
	}

	// Typing a row's first letters is the window page's List typeahead.
	typed := config.OnShortcut(gfx.KeyEvent{Target: w.rowID(0, 1), Text: "d"})
	if !typed.Handled || typed.Focus != w.rowID(0, 0) {
		t.Fatalf("local typeahead = %+v, want focus %v", typed, w.rowID(0, 0))
	}
	repaint()
	dualWait(t, state, w, view, func() bool { return local.selected == 0 })

	down = config.OnKey(gfx.KeyEvent{Target: w.rowID(1, 0), Key: gfx.KeyDown})
	if !down.Handled || down.Focus != w.rowID(1, 1) || !down.FocusVisible {
		t.Fatalf("remote Down = %+v, want focus %v", down, w.rowID(1, 1))
	}
	repaint()
	dualWait(t, state, w, view, func() bool { return remote.selected == 1 })
	if local.selected != 0 {
		t.Fatalf("remote Down moved the local selection to %d", local.selected)
	}

	// Enter on the remote directory is the page's List submit; the next paint
	// opens it. A second open would try to enter docs/docs, which never exists.
	submit := config.OnKey(gfx.KeyEvent{Target: w.rowID(1, 0), Key: gfx.KeyEnter})
	if !submit.Handled {
		t.Fatalf("Enter on a pane row was not handled by the window page: %+v", submit)
	}
	repaint()
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.path == "/home/test/docs" })
	if w.view.path != "/home/test/docs" {
		t.Fatalf("Enter opened the wrong row: %q", w.view.path)
	}

	// The local pane submits the same way: row 0 is the destination directory.
	submit = config.OnKey(gfx.KeyEvent{Target: w.rowID(0, 0), Key: gfx.KeyEnter})
	if !submit.Handled {
		t.Fatalf("Enter on a local pane row was not handled by the window page: %+v", submit)
	}
	repaint()
	wantLocal := filepath.Join(localDir, "destination")
	dualWait(t, state, w, view, func() bool { return !w.view.busy && w.view.lfolder.path == wantLocal })
	if w.view.lfolder.path != wantLocal {
		t.Fatalf("Enter opened the wrong local row: %q, want %q", w.view.lfolder.path, wantLocal)
	}
}
