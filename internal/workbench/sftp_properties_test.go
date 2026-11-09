//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func sftpMenuRowIDs(rows menuRows) map[gfx.ID]bool {
	ids := make(map[gfx.ID]bool, rows.count)
	for _, entry := range rows.entries[:rows.count] {
		if !entry.separator {
			ids[entry.item.ID] = true
		}
	}
	return ids
}

func TestSinglePaneMenuSharesFileActionsPathsAndProperties(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")

	ids := sftpMenuRowIDs(state.sftpMenuRows(session, view))
	for _, id := range []gfx.ID{
		hitSFTPNewDirectory, hitSFTPUpload, hitSFTPUploadDirectory, hitSFTPNewLink,
		hitSFTPCopyPath, hitSFTPProperties,
	} {
		if !ids[id] {
			t.Errorf("single-pane remote menu lacks action %v", id)
		}
	}

	var copied string
	state.options.SetClipboardText = func(value string) error {
		copied = value
		return nil
	}
	reads := remote.readCount(view.path)
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPCopyPath)
	if copied != "/home/test/notes.txt" {
		t.Fatalf("copied path = %q", copied)
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPProperties)
	if state.modal != modalFileProperties || !state.fileProperties.open ||
		state.fileProperties.local || state.fileProperties.entry.Name != "notes.txt" {
		t.Fatalf("remote properties = %+v, modal = %d", state.fileProperties, state.modal)
	}
	if rows := state.sftpFilePropertiesRows(&state.fileProperties); len(rows) < 6 || rows[0].value != "notes.txt" || rows[1].value != "/home/test/notes.txt" {
		t.Fatalf("remote property rows = %+v", rows)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitFilePropertiesClose); !ok {
		t.Fatal("the Viva property dialog did not paint its close action")
	}
	if remote.readCount(view.path) != reads {
		t.Fatal("copying a path or reading cached properties listed the remote folder")
	}
	state.Click(hitFilePropertiesClose)
	if state.modal != modalNone || state.fileProperties.open {
		t.Fatal("the property dialog did not close")
	}

	localDir := localFixture(t)
	clickSFTPRow(t, state, view, "notes.txt") // leave the local row selectable
	openLocalPane(t, state, session, localDir)
	clickSFTPLocalRow(t, state, view, "report.txt")
	ids = sftpMenuRowIDs(state.sftpMenuRows(session, view))
	for _, id := range []gfx.ID{hitSFTPNewDirectory, hitSFTPCopyPath, hitSFTPProperties} {
		if !ids[id] {
			t.Errorf("single-pane local menu lacks action %v", id)
		}
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPCopyPath)
	if want := filepath.Join(localDir, "report.txt"); copied != want {
		t.Fatalf("copied local path = %q, want %q", copied, want)
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPProperties)
	if state.modal != modalFileProperties || !state.fileProperties.local ||
		state.fileProperties.entry.Name != "report.txt" {
		t.Fatalf("local properties = %+v, modal = %d", state.fileProperties, state.modal)
	}
}

func TestDualPanePropertiesPaintAndCloseInsideTheirWindow(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	dualClickID(t, view, dualRow(t, w, false, "notes.txt"))
	w.fileAction(false, hitSFTPProperties)
	if !w.fileProperties.open || w.fileProperties.entry.Name != "notes.txt" || w.fileProperties.local {
		t.Fatalf("dual properties = %+v", w.fileProperties)
	}
	if state.modal != modalNone {
		t.Fatalf("the independent transfer window changed the main window modal to %d", state.modal)
	}
	view.Rest()
	dualHit(t, view, hitFilePropertiesClose)
	dualClickID(t, view, hitFilePropertiesClose)
	if w.fileProperties.open {
		t.Fatal("the dual-window property dialog did not close")
	}

	w.fileAction(false, hitSFTPProperties)
	view.Rest()
	if err := view.Keys("escape"); err != nil {
		t.Fatal(err)
	}
	if w.fileProperties.open {
		t.Fatal("Escape did not close the dual-window property dialog")
	}
}

func TestSinglePanePropertiesDialogConsumesListKeys(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPProperties)
	if state.modal != modalFileProperties {
		t.Fatal("the properties dialog did not open")
	}

	for _, key := range []gfx.Key{gfx.KeyDelete, gfx.KeyF2, gfx.KeyBackspace} {
		result := state.Key(gfx.KeyEvent{Key: key})
		if !result.Handled {
			t.Fatalf("%v escaped the properties dialog", key)
		}
		if state.modal != modalFileProperties || view.dialog != sftpDialogNone {
			t.Fatalf("%v acted behind the properties dialog: modal=%d file dialog=%d", key, state.modal, view.dialog)
		}
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled {
		t.Fatal("Escape did not close the properties dialog")
	}
	if state.modal != modalNone || state.fileProperties.open {
		t.Fatal("Escape closed more than the properties dialog")
	}
}

func TestSinglePanePropertiesDialogConsumesPointerAndAcceptsItsOwnClose(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")
	state.sftpMenu = sftpMenu{open: true, session: session.ID}
	state.Click(hitSFTPProperties)
	if state.modal != modalFileProperties {
		t.Fatal("the properties dialog did not open")
	}
	selected := view.sftpFolder.selected

	result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitSFTPEntryList.At(0), X: 20, Y: 20,
	})
	if !result.Handled || view.sftpFolder.selected != selected {
		t.Fatalf("pointer input escaped the properties dialog: %+v, selection %d -> %d", result, selected, view.sftpFolder.selected)
	}
	if result := state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropOver, Paths: []string{"/tmp/file"}}); !result.Handled {
		t.Fatal("a file drop escaped the properties dialog")
	}

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitFilePropertiesClose)
	if !ok {
		t.Fatal("the properties dialog painted no close action")
	}
	if result := state.Pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary,
		Target: hitFilePropertiesClose, X: hit.Bounds.X + 2, Y: hit.Bounds.Y + 2,
	}); result.Handled {
		t.Fatalf("the dialog consumed its own click sequence: %+v", result)
	}
	state.Click(hitFilePropertiesClose)
	if state.modal != modalNone || state.fileProperties.open {
		t.Fatal("the property dialog did not close")
	}
}

func TestDualPanePropertiesDialogConsumesListKeys(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1.0)
	view := w.view
	if len(view.lfolder.rows) == 0 {
		t.Fatal("the dual-pane fixture has no local row")
	}
	view.lfolder.selectRow(0, 0)
	properties, ok := captureSFTPProperties(&view.lfolder, true)
	if !ok {
		t.Fatal("the dual-pane fixture did not capture the selected row")
	}
	w.fileProperties = properties
	if !w.fileProperties.open {
		t.Fatal("the dual-pane properties dialog did not open")
	}
	for _, key := range []gfx.Key{gfx.KeyDelete, gfx.KeyF2, gfx.KeyBackspace} {
		result := w.key(gfx.KeyEvent{Key: key, Target: sftpWindowRows[0].At(0)})
		if !result.Handled {
			t.Fatalf("%v escaped the dual-pane properties dialog", key)
		}
		if !w.fileProperties.open || view.dialog != sftpDialogNone {
			t.Fatalf("%v acted behind the window properties dialog", key)
		}
	}
}

func TestDualPanePropertiesDialogConsumesPointerInput(t *testing.T) {
	_, w, _, _, _ := dualSFTPFixture(t, 1.0)
	view := w.view
	if len(view.lfolder.rows) == 0 {
		t.Fatal("the dual-pane fixture has no local row")
	}
	view.lfolder.selectRow(0, 0)
	properties, ok := captureSFTPProperties(&view.lfolder, true)
	if !ok {
		t.Fatal("the dual-pane fixture did not capture the selected row")
	}
	w.fileProperties = properties

	bar := w.ui.bars[0].VerticalThumb
	w.drag = sftpWindowDrag{armed: true, fromLocal: true}
	result := w.pointer(gfx.PointerEvent{
		Kind: gfx.PointerDown, Target: hitSFTPWindowSplit,
		X: bar.X + bar.Width/2, Y: bar.Y + bar.Height/2,
	})
	if !result.Handled || !w.fileProperties.open {
		t.Fatalf("pointer input escaped the dual-pane properties dialog: handled=%t properties=%t", result.Handled, w.fileProperties.open)
	}
	if w.ui.barDrag != 0 || w.ui.splitting || !w.drag.armed || !w.drag.fromLocal {
		t.Fatalf("pointer input changed controls behind the dialog: bar=%d split=%t drag=%+v", w.ui.barDrag, w.ui.splitting, w.drag)
	}
}

func TestDualPanePropertiesDialogConsumesFileDrops(t *testing.T) {
	_, w, _, _, localDir := dualSFTPFixture(t, 1.0)
	view := w.view
	if len(view.lfolder.rows) == 0 {
		t.Fatal("the dual-pane fixture has no local row")
	}
	view.lfolder.selectRow(0, 0)
	properties, ok := captureSFTPProperties(&view.lfolder, true)
	if !ok {
		t.Fatal("the dual-pane fixture did not capture the selected row")
	}
	w.fileProperties = properties
	transfers := len(view.transfers)
	file := filepath.Join(localDir, "new.yml")
	if err := os.WriteFile(file, []byte("name: new\n"), 0o600); err != nil {
		t.Fatal(err)
	}

	remote := w.ui.list[1]
	over := w.fileDrop(gfx.FileDropEvent{
		Phase: gfx.FileDropOver,
		X:     remote.X + 20, Y: remote.Y + 20,
	})
	if !over.Handled || w.drag.active || !w.fileProperties.open {
		t.Fatalf("drop hover escaped the properties dialog: %+v, drag=%+v", over, w.drag)
	}
	drop := w.fileDrop(gfx.FileDropEvent{
		Phase: gfx.FileDropDrop, Paths: []string{file},
		X: remote.X + 20, Y: remote.Y + 20,
	})
	if !drop.Handled || len(view.transfers) != transfers || !w.fileProperties.open {
		t.Fatalf("drop escaped the properties dialog: %+v, transfers %d -> %d", drop, transfers, len(view.transfers))
	}

	w.queueEntries(true, []sftpclient.Entry{{Name: filepath.Base(file), Mode: 0o600}}, localDir, w.view.path)
	if len(view.transfers) != transfers {
		t.Fatalf("queueEntries bypassed the open properties dialog: transfers %d -> %d", transfers, len(view.transfers))
	}
}
