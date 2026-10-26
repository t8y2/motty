package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func dropFixture(t *testing.T) (*State, *Session, *workbenchSFTPRemote, ui.Rect, []string) {
	t.Helper()
	state, session, _, remote := openTestSFTP(t)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	panel := state.contextRect
	if panel.Width == 0 {
		t.Fatal("the paint reported no files panel")
	}
	dir := t.TempDir()
	file := filepath.Join(dir, "dropped.txt")
	folder := filepath.Join(dir, "tree")
	os.WriteFile(file, []byte("dropped"), 0o644)
	os.Mkdir(folder, 0o755)
	os.WriteFile(filepath.Join(folder, "leaf.txt"), []byte("leaf"), 0o644)
	return state, session, remote, panel, []string{file, folder}
}

func waitTransfersDone(t *testing.T, state *State, session *Session, count int) {
	t.Helper()
	waitSFTP(t, state, session, func(view *sftpView) bool {
		if len(view.transfers) != count {
			return false
		}
		for _, transfer := range view.transfers {
			if transfer.State != sftpclient.TransferCompleted {
				return false
			}
		}
		return true
	})
}

func TestDroppedFilesUploadIntoTheFolderShown(t *testing.T) {
	state, session, remote, panel, paths := dropFixture(t)
	at := ui.FileDropEvent{X: panel.X + panel.Width/2, Y: panel.Y + panel.Height - 40, Paths: paths}
	at.Phase = ui.FileDropOver
	if result := state.FileDrop(at); !result.Handled || !session.files.dropping {
		t.Fatalf("hover result %+v, dropping %v", result, session.files.dropping)
	}
	at.Phase = ui.FileDropDrop
	if result := state.FileDrop(at); !result.Handled {
		t.Fatalf("drop result %+v", result)
	}
	if session.files.dropping {
		t.Fatal("the drop target highlight stayed after the drop")
	}
	waitTransfersDone(t, state, session, 2)
	remote.mu.Lock()
	file, leaf := string(remote.files["/home/test/dropped.txt"]), string(remote.files["/home/test/tree/leaf.txt"])
	remote.mu.Unlock()
	if file != "dropped" || leaf != "leaf" {
		t.Fatalf("remote has file %q, leaf %q", file, leaf)
	}
}

func TestDroppedFilesOnAFolderRowGoIntoThatFolder(t *testing.T) {
	state, session, remote, panel, paths := dropFixture(t)
	row := -1
	for index, candidate := range session.files.rows {
		if candidate.entry.Name == "docs" {
			row = index
		}
	}
	if row < 0 {
		t.Fatal("no docs row")
	}
	event := ui.FileDropEvent{
		X: panel.X + 40, Y: panel.Y + 60, Paths: paths[:1],
		Target: contextID(hitSFTPEntryList.At(row - session.files.first)),
	}
	event.Phase = ui.FileDropOver
	state.FileDrop(event)
	if session.files.dropRow != row {
		t.Fatalf("hover row %d, want %d", session.files.dropRow, row)
	}
	event.Phase = ui.FileDropDrop
	state.FileDrop(event)
	waitTransfersDone(t, state, session, 1)
	remote.mu.Lock()
	_, there := remote.files["/home/test/docs/dropped.txt"]
	remote.mu.Unlock()
	if !there {
		t.Fatal("the file did not arrive in docs")
	}
}

func TestDropIsRefusedOutsideThePanelAndLeaveClearsTheHighlight(t *testing.T) {
	state, session, _, panel, paths := dropFixture(t)
	outside := ui.FileDropEvent{X: panel.X - 50, Y: panel.Y + 60, Paths: paths}
	outside.Phase = ui.FileDropOver
	if result := state.FileDrop(outside); result.Handled {
		t.Fatalf("a remote session's terminal accepted the hover: %+v", result)
	}
	outside.Phase = ui.FileDropDrop
	if result := state.FileDrop(outside); result.Handled || len(session.files.transfers) != 0 {
		t.Fatalf("a drop outside the panel was taken: %+v, %d transfers", result, len(session.files.transfers))
	}

	over := ui.FileDropEvent{X: panel.X + 40, Y: panel.Y + 60}
	over.Phase = ui.FileDropOver
	state.FileDrop(over)
	if !session.files.dropping {
		t.Fatal("hover over the panel did not show the target")
	}
	state.FileDrop(ui.FileDropEvent{Phase: ui.FileDropLeave})
	if session.files.dropping {
		t.Fatal("leaving did not clear the target")
	}
}

func TestDropOnTheLocalPaneIsRefused(t *testing.T) {
	state, session, _, panel, paths := dropFixture(t)
	state.Click(hitSFTPPaneToggle)
	event := ui.FileDropEvent{X: panel.X + 40, Y: panel.Y + 60, Paths: paths}
	event.Phase = ui.FileDropDrop
	if result := state.FileDrop(event); result.Handled || len(session.files.transfers) != 0 {
		t.Fatalf("the local pane took a drop: %+v", result)
	}
}
