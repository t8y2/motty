package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func dropFixture(t *testing.T) (*State, *Session, *workbenchSFTPRemote, gfx.Rect, []string) {
	t.Helper()
	state, session, _, remote := openTestSFTP(t)
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
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
	at := gfx.FileDropEvent{X: panel.X + panel.Width/2, Y: panel.Y + panel.Height - 40, Paths: paths}
	at.Phase = gfx.FileDropOver
	if result := state.FileDrop(at); !result.Handled || !session.files.dropping {
		t.Fatalf("hover result %+v, dropping %v", result, session.files.dropping)
	}
	at.Phase = gfx.FileDropDrop
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
	event := gfx.FileDropEvent{
		X: panel.X + 40, Y: panel.Y + 60, Paths: paths[:1],
		Target: contextID(hitSFTPEntryList.At(row - session.files.first)),
	}
	event.Phase = gfx.FileDropOver
	state.FileDrop(event)
	if session.files.dropRow != row {
		t.Fatalf("hover row %d, want %d", session.files.dropRow, row)
	}
	event.Phase = gfx.FileDropDrop
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
	outside := gfx.FileDropEvent{X: panel.X - 50, Y: 4, Paths: paths}
	outside.Phase = gfx.FileDropOver
	if result := state.FileDrop(outside); result.Handled {
		t.Fatalf("the window bar accepted the hover: %+v", result)
	}
	outside.Phase = gfx.FileDropDrop
	if result := state.FileDrop(outside); result.Handled || len(session.files.transfers) != 0 {
		t.Fatalf("a drop outside the panel was taken: %+v, %d transfers", result, len(session.files.transfers))
	}

	over := gfx.FileDropEvent{X: panel.X + 40, Y: panel.Y + 60}
	over.Phase = gfx.FileDropOver
	state.FileDrop(over)
	if !session.files.dropping {
		t.Fatal("hover over the panel did not show the target")
	}
	state.FileDrop(gfx.FileDropEvent{Phase: gfx.FileDropLeave})
	if session.files.dropping {
		t.Fatal("leaving did not clear the target")
	}
}

func TestDropOnTheLocalPaneIsRefused(t *testing.T) {
	state, session, _, panel, paths := dropFixture(t)
	state.Click(hitSFTPPaneToggle)
	event := gfx.FileDropEvent{X: panel.X + 40, Y: panel.Y + 60, Paths: paths}
	event.Phase = gfx.FileDropDrop
	if result := state.FileDrop(event); result.Handled || len(session.files.transfers) != 0 {
		t.Fatalf("the local pane took a drop: %+v", result)
	}
}

func terminalPoint(panel gfx.Rect) (float32, float32) {
	if panel.X > 100 {
		return panel.X - 50, panel.Y + 60
	}
	return panel.X + panel.Width + 200, panel.Y + 60
}

func TestDroppedFilesOnTheTerminalUploadIntoTheShellsFolder(t *testing.T) {
	state, session, remote, panel, paths := dropFixture(t)
	session.term.screen.Write([]byte("\x1b]7;file:///home/test/docs\x07"))
	x, y := terminalPoint(panel)
	event := gfx.FileDropEvent{X: x, Y: y, Paths: paths[:1]}
	event.Phase = gfx.FileDropOver
	if result := state.FileDrop(event); !result.Handled || state.terminalDrop.directory != "/home/test/docs" {
		t.Fatalf("hover result %+v, target %+v", result, state.terminalDrop)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	event.Phase = gfx.FileDropDrop
	if result := state.FileDrop(event); !result.Handled {
		t.Fatalf("drop result %+v", result)
	}
	if state.terminalDrop.session != 0 {
		t.Fatal("the terminal's drop target stayed after the drop")
	}
	if state.transfers.open {
		t.Fatal("the transfer center opened over the terminal")
	}
	waitTransfersDone(t, state, session, 1)
	remote.mu.Lock()
	got := string(remote.files["/home/test/docs/dropped.txt"])
	remote.mu.Unlock()
	if got != "dropped" {
		t.Fatalf("the shell's folder holds %q", got)
	}
	want := fmt.Sprintf(state.messages.Tools.DropTerminalDoneFmt, 1, "/home/test/docs")
	if last := state.fileNotices.items[len(state.fileNotices.items)-1]; last.detail != want || last.kind != components.ToastSuccess {
		t.Fatalf("notices %+v, want a success saying %q", state.fileNotices.items, want)
	}
	if len(state.terminalUploads) != 0 {
		t.Fatal("a finished drop is still followed")
	}
}

func TestDroppedFilesThatCannotBeUploadedAreReportedOnTheTerminal(t *testing.T) {
	state, session, _, panel, paths := dropFixture(t)
	x, y := terminalPoint(panel)
	event := gfx.FileDropEvent{X: x, Y: y, Paths: paths[:1]}
	event.Phase = gfx.FileDropDrop
	state.FileDrop(event)
	waitTransfersDone(t, state, session, 1)
	state.FileDrop(event) // the same name is already there
	waitSFTP(t, state, session, func(*sftpView) bool { return len(state.terminalUploads) == 0 })
	last := state.fileNotices.items[len(state.fileNotices.items)-1]
	if last.kind != components.ToastError || !strings.Contains(last.detail, "/home/test") {
		t.Fatalf("notices %+v, want an error naming the folder", state.fileNotices.items)
	}
}

func TestDroppedFilesOnATerminalWithoutAReportGoToThePanelsFolder(t *testing.T) {
	state, session, remote, panel, paths := dropFixture(t)
	x, y := terminalPoint(panel)
	event := gfx.FileDropEvent{X: x, Y: y, Paths: paths[:1]}
	event.Phase = gfx.FileDropDrop
	if result := state.FileDrop(event); !result.Handled {
		t.Fatalf("drop result %+v", result)
	}
	waitTransfersDone(t, state, session, 1)
	remote.mu.Lock()
	_, there := remote.files["/home/test/dropped.txt"]
	remote.mu.Unlock()
	if !there {
		t.Fatal("the file did not arrive in the folder the panel shows")
	}
}

func TestDropOnATerminalWhoseFolderIsUnknownIsRefused(t *testing.T) {
	state, session, _, panel, paths := dropFixture(t)
	state.closeSFTP(session)
	x, y := terminalPoint(panel)
	event := gfx.FileDropEvent{X: x, Y: y, Paths: paths[:1]}
	event.Phase = gfx.FileDropOver
	if result := state.FileDrop(event); result.Handled {
		t.Fatalf("a terminal with no known folder accepted the hover: %+v", result)
	}
}
