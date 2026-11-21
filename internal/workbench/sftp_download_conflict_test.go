package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func openDownloadConflict(t *testing.T) (*State, *Session, *sftpView, string) {
	t.Helper()
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	directory := t.TempDir()
	if err := os.WriteFile(filepath.Join(directory, "notes.txt"), []byte("mine"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.sftpDownloadDirectory = directory
	return state, session, view, directory
}

func waitDownloadCompleted(t *testing.T, state *State, session *Session, count int) {
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

// A download whose local name is taken must ask before it starts a transfer.
// The old behavior queued the transfer and only reported the conflict in the
// transfer center, after the fact.
func TestSFTPDownloadExistingFileAsksBeforeTransfer(t *testing.T) {
	state, _, view, directory := openDownloadConflict(t)
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	if view.dialog != sftpDialogDownloadConflict {
		t.Fatalf("download dialog = %v, want the conflict prompt", view.dialog)
	}
	if view.downloadConflictCount != 1 || view.downloadConflictName != "notes.txt" {
		t.Fatalf("conflict description = %d %q", view.downloadConflictCount, view.downloadConflictName)
	}
	if len(view.transfers) != 0 {
		t.Fatalf("a transfer was queued before the user chose: %+v", view.transfers)
	}
	if content, err := os.ReadFile(filepath.Join(directory, "notes.txt")); err != nil || string(content) != "mine" {
		t.Fatalf("the existing file changed before the user chose: %q, %v", content, err)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if canvas.InitialFocus() != hitSFTPDialogSubmit {
		t.Fatalf("conflict dialog focus = %d, want the Keep Both button", canvas.InitialFocus())
	}
	if _, ok := hitByID(&canvas, hitSFTPDialogReplace); !ok {
		t.Fatal("the conflict dialog has no Replace button")
	}
}

func TestSFTPDownloadKeepBothNumbersTheCopy(t *testing.T) {
	state, session, view, directory := openDownloadConflict(t)
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	state.Click(hitSFTPDialogSubmit)
	waitDownloadCompleted(t, state, session, 1)
	if content, err := os.ReadFile(filepath.Join(directory, "notes.txt")); err != nil || string(content) != "mine" {
		t.Fatalf("keep both replaced the original: %q, %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(directory, "notes (1).txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("numbered copy = %q, %v", content, err)
	}
}

func TestSFTPDownloadReplaceOverwritesAfterChoice(t *testing.T) {
	state, session, view, directory := openDownloadConflict(t)
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	state.Click(hitSFTPDialogReplace)
	waitDownloadCompleted(t, state, session, 1)
	if content, err := os.ReadFile(filepath.Join(directory, "notes.txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("confirmed replacement = %q, %v", content, err)
	}
	if _, err := os.Stat(filepath.Join(directory, "notes (1).txt")); !os.IsNotExist(err) {
		t.Fatalf("replace left a numbered copy: %v", err)
	}
}

func TestSFTPDownloadConflictCancelQueuesNothing(t *testing.T) {
	state, session, view, directory := openDownloadConflict(t)
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	state.Click(hitSFTPDialogCancel)
	if view.dialog != sftpDialogNone || len(view.transfers) != 0 {
		t.Fatalf("cancel left dialog=%v transfers=%+v", view.dialog, view.transfers)
	}
	if content, err := os.ReadFile(filepath.Join(directory, "notes.txt")); err != nil || string(content) != "mine" {
		t.Fatalf("cancel changed the existing file: %q, %v", content, err)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
}

func TestSFTPDownloadKeepBothSkipsNamesAlreadyTaken(t *testing.T) {
	state, session, view, directory := openDownloadConflict(t)
	for _, name := range []string{"notes (1).txt", "notes (2).txt"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	state.Click(hitSFTPDialogSubmit)
	waitDownloadCompleted(t, state, session, 1)
	if content, err := os.ReadFile(filepath.Join(directory, "notes (3).txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("third numbered copy = %q, %v", content, err)
	}
}

func TestSFTPDownloadBatchAsksOnceAndKeepsBoth(t *testing.T) {
	state, session, view, directory := openDownloadConflict(t)
	view.selectRow(sftpRowIndex(t, view, "notes.txt"), 0)
	view.selectRow(sftpRowIndex(t, view, "docs"), gfx.ModifierControl)
	state.Click(hitSFTPDownload)
	if view.dialog != sftpDialogDownloadConflict || len(view.downloadPending) != 2 {
		t.Fatalf("batch conflict = dialog %v, pending %d", view.dialog, len(view.downloadPending))
	}
	state.Click(hitSFTPDialogSubmit)
	waitDownloadCompleted(t, state, session, 2)
	if content, err := os.ReadFile(filepath.Join(directory, "notes (1).txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("numbered file in batch = %q, %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(directory, "docs", "guide.md")); err != nil || string(content) != "guide\n" {
		entries, _ := os.ReadDir(directory)
		t.Fatalf("directory in batch = %q, %v; transfers=%+v entries=%v", content, err, view.transfers, entries)
	}
}

func TestSFTPPasteRemoteClipboardAsksAboutExistingName(t *testing.T) {
	state, session, view, directory := openDownloadConflict(t)
	clip := sftpClipboard{
		view: view, sourceDir: "/home/test",
		entries: []sftpclient.Entry{{Name: "notes.txt", Size: 11, Mode: 0o644}},
	}
	if !state.pasteRemoteClipboardToLocal(view, clip, directory) {
		t.Fatal("paste was not taken")
	}
	if view.dialog != sftpDialogDownloadConflict || len(view.downloadPending) != 1 {
		t.Fatalf("paste conflict = dialog %v, pending %d", view.dialog, len(view.downloadPending))
	}
	state.Click(hitSFTPDialogSubmit)
	waitDownloadCompleted(t, state, session, 1)
	if content, err := os.ReadFile(filepath.Join(directory, "notes (1).txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("pasted numbered copy = %q, %v", content, err)
	}
}

func TestNumberedDownloadPathKeepsExtensionAndDotfiles(t *testing.T) {
	directory := t.TempDir()
	taken := map[string]bool{}
	for _, name := range []string{"archive.tar.gz", ".bashrc"} {
		if err := os.WriteFile(filepath.Join(directory, name), []byte(name), 0o600); err != nil {
			t.Fatal(err)
		}
	}
	if err := os.Mkdir(filepath.Join(directory, "release.v1"), 0o700); err != nil {
		t.Fatal(err)
	}
	cases := []struct {
		name      string
		directory bool
		want      string
	}{
		{"archive.tar.gz", false, "archive.tar (1).gz"},
		{".bashrc", false, ".bashrc (1)"},
		{"release.v1", true, "release.v1 (1)"},
	}
	for _, test := range cases {
		target := filepath.Join(directory, test.name)
		if got := filepath.Base(numberedDownloadPath(target, test.directory, taken)); got != test.want {
			t.Errorf("numberedDownloadPath(%q, dir=%t) = %q, want %q", test.name, test.directory, got, test.want)
		}
	}
}
