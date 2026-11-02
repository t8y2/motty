//go:build windows

package workbench

import (
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
)

func TestCustomEditorPickerLaunchAndLocalFolder(t *testing.T) {
	options := freshOptions(t)
	executable := `C:\Tools\Editor With 空格.exe`
	options.OpenFiles = func(got app.OpenFileOptions) ([]string, error) {
		if len(got.Filters) != 1 || got.Filters[0].Pattern != "*.exe" {
			t.Fatalf("picker filters = %+v", got.Filters)
		}
		return []string{executable}, nil
	}
	state := newTestState(t, options)
	state.chooseCustomEditor()
	if state.defaultEditor != editorCustom || state.customEditorPath != executable {
		t.Fatalf("chosen editor = %q/%q", state.defaultEditor, state.customEditorPath)
	}

	launched := make(chan [2]string, 2)
	state.options.LaunchCustomEditor = func(exe, path string) error {
		launched <- [2]string{exe, path}
		return nil
	}
	localFile := `C:\Users\测试 User\notes.yml`
	if err := state.openWithConfiguredEditor(localFile); err != nil {
		t.Fatal(err)
	}
	if got := <-launched; got != [2]string{executable, localFile} {
		t.Fatalf("launch = %q", got)
	}

	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.local = true
	view.lfolder.path = `C:\Users\测试 User\Project With Spaces`
	state.defaultEditor = editorCustom
	state.customEditorPath = executable
	state.options.LaunchCustomEditor = func(exe, path string) error {
		launched <- [2]string{exe, path}
		return nil
	}
	state.openSFTPInEditor(view)
	select {
	case got := <-launched:
		if got != [2]string{executable, view.lfolder.path} {
			t.Fatalf("local folder launch = %q", got)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("the custom editor was not launched for a local folder")
	}

	state.customEditorPath = `C:\Tools\Editor.txt`
	if err := state.openWithConfiguredEditor(localFile); err == nil || !strings.Contains(err.Error(), state.messages.Tools.FilesCustomEditorInvalid) {
		t.Fatalf("invalid executable error = %v", err)
	}
}

func TestCustomEditorReceivesRemoteTemporaryFileAndSyncsBack(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	executable := `C:\Tools\Remote Editor.exe`
	state.defaultEditor = editorCustom
	state.customEditorPath = executable
	launched := make(chan [2]string, 1)
	state.options.LaunchCustomEditor = func(exe, path string) error {
		launched <- [2]string{exe, path}
		return nil
	}
	state.options.EditPollInterval = 5 * time.Millisecond

	clickSFTPRow(t, state, session.files, "notes.txt")
	state.Click(hitSFTPEditExternal)
	waitFor(t, state, func() bool { return len(state.edits.list) == 1 || session.files.errorText != "" })
	if len(state.edits.list) != 1 {
		t.Fatalf("no synchronized edit started: %q", session.files.errorText)
	}
	got := <-launched
	if got[0] != executable || got[1] != state.edits.list[0].local {
		t.Fatalf("custom editor launch = %q", got)
	}
	content, err := os.ReadFile(got[1])
	if err != nil || string(content) != "hello world" {
		t.Fatalf("downloaded temporary file = %q, %v", content, err)
	}
	if err := os.WriteFile(got[1], []byte("saved by custom editor\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool {
		remote.mu.Lock()
		defer remote.mu.Unlock()
		return string(remote.files["/home/test/notes.txt"]) == "saved by custom editor\n"
	})
}
