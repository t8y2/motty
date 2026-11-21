//go:build windows || darwin || linux

package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"motty/internal/sftpclient"
)

// The standalone transfer window takes the same download path as the docked
// panel: a name already on this computer asks before anything is queued.
func TestSFTPWindowDownloadConflictKeepsBoth(t *testing.T) {
	state, w, _, host, localDir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if err := os.WriteFile(filepath.Join(localDir, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	index := sftpRowIndex(t, w.view, "notes.txt")
	w.view.selectRow(index, 0)
	w.queueSelection(false)
	if w.view.dialog != sftpDialogDownloadConflict {
		t.Fatalf("download dialog = %v, want the conflict prompt", w.view.dialog)
	}
	if len(w.view.transfers) != 0 {
		t.Fatalf("a transfer was queued before the user chose: %+v", w.view.transfers)
	}
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool {
		return len(w.view.transfers) == 1 && w.view.transfers[0].State == sftpclient.TransferCompleted
	})
	if content, err := os.ReadFile(filepath.Join(localDir, "notes.txt")); err != nil || string(content) != "mine" {
		t.Fatalf("keep both replaced the original: %q, %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(localDir, "notes (1).txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("numbered copy = %q, %v", content, err)
	}
}
