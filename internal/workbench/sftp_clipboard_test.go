package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sftpclient"
)

// clipboardChord is the key event the configured action is bound to, aimed at
// target.
func clipboardChord(state *State, action shortcutAction, target gfx.ID) gfx.KeyEvent {
	chord := state.shortcuts.chords.Chord(action)
	return gfx.KeyEvent{Target: target, Modifiers: chord.Modifiers, Text: string(chord.Char)}
}

// folderRowIndex is the index of name among the folder's rows.
func folderRowIndex(t *testing.T, folder *sftpFolder, name string) int {
	t.Helper()
	for index := range folder.rows {
		if folder.rows[index].entry.Name == name {
			return index
		}
	}
	t.Fatalf("row %q not found in %+v", name, folder.rows)
	return -1
}

// The issue this covers: copy files in the system file manager, then paste them
// on the server side of the docked panel to upload them.
func TestSFTPPasteUploadsTheFilesOnTheSystemClipboard(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	dir := t.TempDir()
	file := filepath.Join(dir, "pasted.txt")
	if err := os.WriteFile(file, []byte("pasted"), 0o644); err != nil {
		t.Fatal(err)
	}
	folder := filepath.Join(dir, "tree")
	if err := os.Mkdir(folder, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(folder, "leaf.txt"), []byte("leaf"), 0o644); err != nil {
		t.Fatal(err)
	}
	state.options.ClipboardFiles = func() ([]string, error) { return []string{file, folder}, nil }
	view := session.files
	if view.local {
		t.Fatal("the docked panel starts on the server side")
	}
	paste := clipboardChord(state, shortcutPaste, contextID(hitSFTPEntryList.At(0)))
	if result := state.Shortcut(paste); !result.Handled {
		t.Fatalf("paste was not taken: %+v", result)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		if len(view.transfers) != 2 {
			return false
		}
		for _, transfer := range view.transfers {
			if transfer.State != sftpclient.TransferCompleted {
				return false
			}
		}
		return true
	})
	remote.mu.Lock()
	body, leaf := string(remote.files["/home/test/pasted.txt"]), string(remote.files["/home/test/tree/leaf.txt"])
	remote.mu.Unlock()
	if body != "pasted" || leaf != "leaf" {
		t.Fatalf("uploaded file %q, nested file %q", body, leaf)
	}
}

func TestSFTPCopyPutsTheLocalSelectionOnTheSystemClipboard(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	dir := localFixture(t)
	view := openLocalPane(t, state, session, dir)
	clickSFTPLocalRow(t, state, view, "report.txt")
	var copied []string
	state.options.SetClipboardFiles = func(paths []string) error {
		copied = append([]string(nil), paths...)
		return nil
	}
	copyKey := clipboardChord(state, shortcutCopy, contextID(hitSFTPEntryList.At(0)))
	if result := state.Shortcut(copyKey); !result.Handled {
		t.Fatalf("copy was not taken: %+v", result)
	}
	if len(copied) != 1 || copied[0] != filepath.Join(dir, "report.txt") {
		t.Fatalf("clipboard holds %q, want the selected local file", copied)
	}
}

func TestSFTPWindowPastesLocalFilesOntoTheServer(t *testing.T) {
	state, w, remote, host, localDir := dualSFTPFixture(t, 1)
	name := "中文上传.yml"
	local := w.folder(true)
	local.selectRow(folderRowIndex(t, local, name), 0)
	w.selectionChanged(true)
	var copied []string
	state.options.SetClipboardFiles = func(paths []string) error {
		copied = append([]string(nil), paths...)
		return nil
	}
	state.options.ClipboardFiles = func() ([]string, error) { return copied, nil }

	copyKey := gfx.KeyEvent{Target: dualRow(t, w, true, name), Modifiers: keymap.Primary, Text: "c"}
	if result := w.shortcut(copyKey); !result.Handled {
		t.Fatalf("copy was not taken: %+v", result)
	}
	if len(copied) != 1 || copied[0] != filepath.Join(localDir, name) {
		t.Fatalf("clipboard holds %q, want the selected local file", copied)
	}
	pasteKey := gfx.KeyEvent{Target: dualRow(t, w, false, "notes.txt"), Modifiers: keymap.Primary, Text: "v"}
	if result := w.shortcut(pasteKey); !result.Handled {
		t.Fatalf("paste was not taken: %+v", result)
	}
	dualWait(t, state, w, host.windows[0].view, func() bool {
		return len(w.view.transfers) == 1 && w.view.transfers[0].State == sftpclient.TransferCompleted
	})
	remote.mu.Lock()
	body := string(remote.files["/home/test/"+name])
	remote.mu.Unlock()
	if body != "name: 中文\n" {
		t.Fatalf("server file holds %q", body)
	}
}

func TestSFTPWindowPastesServerFilesOntoTheLocalSide(t *testing.T) {
	state, w, _, host, localDir := dualSFTPFixture(t, 1)
	remote := w.folder(false)
	remote.selectRow(folderRowIndex(t, remote, "notes.txt"), 0)
	w.selectionChanged(false)

	copyKey := gfx.KeyEvent{Target: dualRow(t, w, false, "notes.txt"), Modifiers: keymap.Primary, Text: "c"}
	if result := w.shortcut(copyKey); !result.Handled {
		t.Fatalf("copy was not taken: %+v", result)
	}
	if len(w.view.clipboard.entries) != 1 || w.view.clipboard.entries[0].Name != "notes.txt" {
		t.Fatalf("server clipboard holds %+v", w.view.clipboard.entries)
	}
	pasteKey := gfx.KeyEvent{Target: dualRow(t, w, true, "中文上传.yml"), Modifiers: keymap.Primary, Text: "v"}
	if result := w.shortcut(pasteKey); !result.Handled {
		t.Fatalf("paste was not taken: %+v", result)
	}
	dualWait(t, state, w, host.windows[0].view, func() bool {
		return len(w.view.transfers) == 1 && w.view.transfers[0].State == sftpclient.TransferCompleted
	})
	body, err := os.ReadFile(filepath.Join(localDir, "notes.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if string(body) != "hello world" {
		t.Fatalf("local file holds %q", body)
	}
}
