//go:build windows || darwin || linux

package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"
)

// The transfer window shows both sides, so its local pane needs the same delete
// the server's pane has (issue #667).
func TestSFTPWindowDeletesLocalEntriesThroughTheSameDialog(t *testing.T) {
	state, w, _, host, localDir := dualSFTPFixture(t, 1)
	view := w.view
	w.folder(true).selectRow(localRowIndex(t, w, "tree"), 0)
	w.selectionChanged(true)
	w.invalidate()
	preview := host.windows[0].view
	if err := preview.Driver().Pump(); err != nil {
		t.Fatal(err)
	}
	preview.Rest()
	deleteHit := sftpWindowActions[0].At(sftpWindowActionIndex(hitSFTPDelete))
	if _, ok := hitByIDSnapshot(preview.Snapshot().Hits, deleteHit); !ok {
		t.Fatal("the local pane's delete button is not painted")
	}
	hasDelete := false
	for _, action := range w.selectionActions(true) {
		hasDelete = hasDelete || action.index == sftpWindowActionIndex(hitSFTPDelete)
	}
	if !hasDelete {
		t.Fatal("the local pane's action bar has no delete")
	}
	if !sftpMenuRowIDs(state.sftpMenuRows(&w.session, view))[hitSFTPMenuDelete] {
		t.Fatal("the local pane's context menu has no delete")
	}
	w.fileAction(true, hitSFTPDelete)
	if view.dialog != sftpDialogDelete || view.dialogEntry.Name != "tree" {
		t.Fatalf("window local delete dialog = %v %q", view.dialog, view.dialogEntry.Name)
	}
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, host.windows[0].view, func() bool {
		return localDeleteSettled(view, filepath.Join(localDir, "tree"))
	})
	if _, err := os.Stat(filepath.Join(localDir, "destination")); err != nil {
		t.Fatalf("the window's local delete removed an unselected folder: %v", err)
	}
	if _, err := os.Lstat(filepath.Join(localDir, "tree")); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("the window's local delete left the folder on disk")
	}
}

func localRowIndex(t *testing.T, w *sftpWindow, name string) int {
	t.Helper()
	for index := range w.folder(true).rows {
		if w.folder(true).rows[index].entry.Name == name {
			return index
		}
	}
	t.Fatalf("no local row %q", name)
	return -1
}
