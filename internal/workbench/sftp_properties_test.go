//go:build windows || darwin || linux

package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/ui"
)

func sftpMenuRowIDs(rows menuRows) map[ui.ID]bool {
	ids := make(map[ui.ID]bool, rows.count)
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
	for _, id := range []ui.ID{
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
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
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
	for _, id := range []ui.ID{hitSFTPNewDirectory, hitSFTPCopyPath, hitSFTPProperties} {
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
