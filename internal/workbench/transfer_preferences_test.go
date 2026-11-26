package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestSFTPPanelAutomaticCardIgnoresHistoryFilters(t *testing.T) {
	for _, entry := range []string{"button", "drop", "paste"} {
		for _, preference := range []struct {
			name string
			off  bool
		}{{"on", false}, {"off", true}} {
			t.Run(entry+"/"+preference.name, func(t *testing.T) {
				state, session, _, remote := openTestSFTP(t)
				state.sftpTransferAutoOpenOff = preference.off
				center := &state.transfers
				center.filter = transferFilterEnded
				center.direction = transferDownload
				center.search.SetText("unmatched-history")
				state.updateTransferSearch()
				center.groups[transferFilterWaiting].collapsed = true
				local := filepath.Join(t.TempDir(), "auto-upload.txt")
				if err := os.WriteFile(local, []byte("uploaded"), 0o600); err != nil {
					t.Fatal(err)
				}
				switch entry {
				case "button":
					state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{local}, nil }
					state.Click(hitSFTPUpload)
				case "drop":
					var canvas gfx.Canvas
					state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
					panel := state.contextRect
					if result := state.FileDrop(gfx.FileDropEvent{
						X: panel.X + panel.Width/2, Y: panel.Y + panel.Height - 40,
						Phase: gfx.FileDropDrop, Paths: []string{local},
					}); !result.Handled {
						t.Fatal("the SFTP panel rejected the drop")
					}
				case "paste":
					state.options.ClipboardFiles = func() ([]string, error) { return []string{local}, nil }
					if !state.pasteSFTPClipboard(session.files) {
						t.Fatal("the SFTP panel rejected the paste")
					}
				}
				if center.open != !preference.off || center.compact != !preference.off {
					t.Fatalf("automatic card: open=%v compact=%v, setting off=%v", center.open, center.compact, preference.off)
				}
				if center.filter != transferFilterEnded || center.direction != transferDownload || center.query != "unmatched-history" || !center.groups[transferFilterWaiting].collapsed {
					t.Fatal("automatic display changed the full history filters")
				}
				waitTransfersDone(t, state, session, 1)
				remote.mu.Lock()
				contents := string(remote.files["/home/test/auto-upload.txt"])
				remote.mu.Unlock()
				if contents != "uploaded" {
					t.Fatalf("uploaded contents = %q", contents)
				}
			})
		}
	}
}

func TestTransferPreferencesPersistAndControlAutomaticOpening(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	if state.sftpClearUploads || state.sftpTransferAutoOpenOff {
		t.Fatal("new settings changed the existing defaults")
	}
	state.clickFilesSettings(hitSettingsTransferAutoOpen)
	state.clickFilesSettings(hitSettingsClearUploads)
	restored := newTestState(t, options)
	if !restored.sftpTransferAutoOpenOff || !restored.sftpClearUploads {
		t.Fatal("file transfer preferences did not survive a restart")
	}
	restored.openTransferCenterForQueued()
	if restored.transfers.open {
		t.Fatal("a new transfer opened the center with automatic display disabled")
	}
	restored.Click(hitTransferCenter)
	if !restored.transfers.open || restored.transfers.compact {
		t.Fatal("the toolbar cannot open the full center manually")
	}
	restored.Click(hitTransferCenter)
	restored.clickFilesSettings(hitSettingsTransferAutoOpen)
	restored.openTransferCenterForQueued()
	if !restored.transfers.open || !restored.transfers.compact {
		t.Fatal("reenabling automatic display did not open the progress card")
	}
}

func TestTransferAutomaticCleanupOnlyRemovesSuccessfulUploads(t *testing.T) {
	for _, enabled := range []bool{false, true} {
		t.Run(map[bool]string{false: "keep", true: "clear"}[enabled], func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			state.sftpClearUploads = enabled
			view, remote := bulkTestView(t, state)
			conflict, conflictPath := bulkConflict(t, state, view, remote, "existing", "keep")
			bulkSource(remote, "/download", "downloaded")
			downloadPath := filepath.Join(t.TempDir(), "download")
			download, ok := view.worker.QueueDownload("/download", downloadPath, false, sftpclient.ExistingFail)
			if !ok {
				t.Fatal("download rejected")
			}
			bulkAwait(t, state, view, download, sftpclient.TransferCompleted)
			source := filepath.Join(t.TempDir(), "upload.txt")
			if err := os.WriteFile(source, []byte("uploaded"), 0o600); err != nil {
				t.Fatal(err)
			}
			upload, ok := view.worker.QueueUpload(source, "/upload.txt", false, sftpclient.ExistingFail)
			if !ok {
				t.Fatal("upload rejected")
			}
			bulkAwait(t, state, view, upload, sftpclient.TransferCompleted)
			state.refreshSFTPTransfers(view)
			_, hasUpload := state.transfers.byTask[transferKey{view, upload}]
			if hasUpload == enabled {
				t.Fatalf("upload history exists=%v, cleanup=%v", hasUpload, enabled)
			}
			for _, id := range []uint64{conflict, download} {
				if _, ok := state.transfers.byTask[transferKey{view, id}]; !ok {
					t.Fatalf("cleanup removed task %d", id)
				}
			}
			if contents, err := os.ReadFile(source); err != nil || string(contents) != "uploaded" {
				t.Fatal("cleanup removed the source file")
			}
			if contents, _ := os.ReadFile(conflictPath); string(contents) != "keep" {
				t.Fatal("cleanup changed a conflicting file")
			}
			remote.mu.Lock()
			contents := string(remote.files["/upload.txt"])
			remote.mu.Unlock()
			if contents != "uploaded" {
				t.Fatalf("remote upload=%q", contents)
			}
		})
	}
}

func TestTransferPendingIsSeparateFromFailureAndLeavesTheFilterAfterRetry(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	view, remote := bulkTestView(t, state)
	id, _ := bulkConflict(t, state, view, remote, "exists", "old")
	failed, ok := view.worker.QueueDownload("/missing", filepath.Join(t.TempDir(), "missing"), false, sftpclient.ExistingFail)
	if !ok {
		t.Fatal("download rejected")
	}
	bulkAwait(t, state, view, failed, sftpclient.TransferFailed)
	center := &state.transfers
	if len(center.groups[transferFilterPending].members) != 1 || len(center.groups[transferFilterFailed].members) != 1 || center.failed != 1 || center.pending != 1 {
		t.Fatal("conflicts were counted as ordinary failures")
	}
	state.Click(hitTransfersFilter.At(transferFilterPending))
	entry := center.entries[center.byTask[transferKey{view, id}]]
	if entry.status != state.messages.Transfers.Pending || center.count != 1 {
		t.Fatal("pending task has the wrong status")
	}
	state.Click(hitTransfersReplace.At(entry.id))
	if center.filter != transferFilterAll {
		t.Fatal("retry remained hidden in the pending filter")
	}
	bulkAwait(t, state, view, id, sftpclient.TransferCompleted)
	if len(center.groups[transferFilterPending].members) != 0 || center.failed != 1 {
		t.Fatal("resolved conflict remains pending or changed a failure")
	}
}

func TestTerminalUploadWaitsForConflictResolutionBeforeReportingItsOutcome(t *testing.T) {
	_, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	view, remote := bulkTestView(t, state)
	id, _ := bulkConflict(t, state, view, remote, "existing", "old")
	state.terminalUploads = []terminalUploadBatch{{view: view, total: 1, pending: map[uint64]struct{}{id: {}}}}
	state.settleTerminalUploads(view)
	if len(state.terminalUploads) != 1 || state.terminalUploads[0].failed != 0 || len(state.terminalUploads[0].pending) != 1 {
		t.Fatal("the drop reported a pending conflict as a failed upload")
	}
	state.showTransferConflict(view)
	state.Click(hitTransferConflictReplaceAll)
	bulkAwait(t, state, view, id, sftpclient.TransferCompleted)
	if len(state.terminalUploads) != 0 {
		t.Fatal("the resolved drop did not settle")
	}
}
