package workbench

import (
	"os"
	"path"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
)

func selectSFTPNames(t testing.TB, state *State, view *sftpView, names ...string) {
	t.Helper()
	for index, name := range names {
		var modifiers gfx.Modifiers
		if index > 0 {
			modifiers = keymap.Primary
		}
		view.sftpFolder.selectRow(sftpRowIndex(t, view, name), modifiers)
	}
	state.Wake()
}

func remotePermissionMode(t testing.TB, remote *workbenchSFTPRemote, remotePath string) os.FileMode {
	t.Helper()
	remote.mu.Lock()
	defer remote.mu.Unlock()
	for _, info := range remote.entries[path.Dir(remotePath)] {
		if info.Name() == path.Base(remotePath) {
			return info.Mode().Perm()
		}
	}
	t.Fatalf("remote entry %q not found", remotePath)
	return 0
}

func sftpMenuHasRow(rows menuRows, id gfx.ID) bool {
	for _, entry := range rows.entries[:rows.count] {
		if !entry.separator && entry.item.ID == id {
			return true
		}
	}
	return false
}

func TestSFTPBatchChmodIsOfferedInMainMenus(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	selectSFTPNames(t, state, view, "docs", "notes.txt")

	rows := state.sftpMenuRows(session, view)
	if !sftpMenuHasRow(rows, hitSFTPMenuChmod) {
		t.Fatal("remote multi-selection has no permissions action in its context menu")
	}
	if sftpMenuHasRow(rows, hitSFTPMenuChown) {
		t.Fatal("ownership unexpectedly became a multi-selection action")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitSFTPChmod); !ok || hit.Disabled {
		t.Fatalf("remote multi-selection has no enabled toolbar permissions action: %+v, %t", hit, ok)
	}
}

func TestSFTPBatchChmodAppliesOneModeToEverySelectedItem(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	selectSFTPNames(t, state, view, "docs", "notes.txt")

	state.Click(hitSFTPChmod)
	if view.dialog != sftpDialogChmod || len(view.dialogEntries) != 2 {
		t.Fatalf("batch permissions dialog = %d, entries = %+v", view.dialog, view.dialogEntries)
	}
	if view.dialogEditor.String() != "" {
		t.Fatalf("mixed modes were prefilled as %q", view.dialogEditor.String())
	}
	replaceSFTPDialogText(t, state, "640")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.dialog == sftpDialogNone && !view.busy &&
			remotePermissionMode(t, remote, "/home/test/docs") == 0o640 &&
			remotePermissionMode(t, remote, "/home/test/notes.txt") == 0o640
	})
	if !strings.Contains(view.status, "640") {
		t.Fatalf("batch permissions status = %q", view.status)
	}
}

func TestSFTPRecursiveChmodWalksFoldersWithoutFollowingLinks(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	if err := remote.Mkdir("/home/test/docs/nested"); err != nil {
		t.Fatal(err)
	}
	file, err := remote.Create("/home/test/docs/nested/data.txt")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := file.Write([]byte("nested")); err != nil {
		t.Fatal(err)
	}
	if err := file.Close(); err != nil {
		t.Fatal(err)
	}
	if err := remote.Symlink("../notes.txt", "/home/test/docs/notes-link"); err != nil {
		t.Fatal(err)
	}

	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPChmod)
	if view.dialogEditor.String() != "755" {
		t.Fatalf("folder permissions were not prefilled: %q", view.dialogEditor.String())
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	recursive, ok := hitByID(&canvas, hitSFTPChmodRecursive)
	if !ok || recursive.Checked {
		t.Fatalf("recursive permissions option = %+v, %t", recursive, ok)
	}
	driver := modalEventDriver(t, state)
	clickPaintedHit(t, driver, hitSFTPChmodRecursive)
	if !view.chmodRecursive {
		t.Fatal("recursive permissions option did not turn on through the pointer event pass")
	}
	app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&view.dialogEditor)
	view.dialogEditor.Insert("700")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.dialog == sftpDialogNone && !view.busy &&
			remotePermissionMode(t, remote, "/home/test/docs") == 0o700 &&
			remotePermissionMode(t, remote, "/home/test/docs/guide.md") == 0o700 &&
			remotePermissionMode(t, remote, "/home/test/docs/nested") == 0o700 &&
			remotePermissionMode(t, remote, "/home/test/docs/nested/data.txt") == 0o700
	})
	if mode := remotePermissionMode(t, remote, "/home/test/notes.txt"); mode != 0o644 {
		t.Fatalf("file outside the selected folder changed to %03o", mode)
	}
	if mode := remotePermissionMode(t, remote, "/home/test/docs/notes-link"); mode != 0o777 {
		t.Fatalf("symbolic link inside the folder was changed or followed: %03o", mode)
	}

	state.Click(hitSFTPDialogCancel)
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPChmod)
	if sftpChmodRecursiveAvailable(view) {
		t.Fatal("file-only selection offers recursive permissions")
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitSFTPChmodRecursive); ok {
		t.Fatal("file-only permissions dialog paints the recursive option")
	}
}

func TestSFTPWindowBatchChmodThroughContextMenu(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1)
	preview := host.windows[0].view
	w.refresh(false)
	dualWait(t, state, w, preview, func() bool { return !w.view.busy && len(w.view.rows) >= 2 })
	selectSFTPNames(t, state, w.view, "docs", "notes.txt")
	w.selectionChanged(false)
	w.invalidate()
	if err := preview.RightClickLabel("notes.txt"); err != nil {
		t.Fatal(err)
	}
	actions := w.selectionActions(false)
	index := sftpWindowActionIndex(hitSFTPChmod)
	found := false
	for _, action := range actions {
		if action.index == index {
			found = true
			break
		}
	}
	if index < 0 || !found {
		t.Fatalf("detached window has no batch permissions action: %+v", actions)
	}
	dualClickMenuLabel(t, preview, w.windowMenuRowLabel(sftpWindowMenuActions.At(index)))
	if w.view.dialog != sftpDialogChmod || len(w.view.dialogEntries) != 2 {
		t.Fatalf("detached batch permissions dialog = %d, entries = %+v", w.view.dialog, w.view.dialogEntries)
	}
	if w.view.dialogEditor.String() != "" {
		t.Fatalf("detached mixed modes were prefilled as %q", w.view.dialogEditor.String())
	}
}
