//go:build darwin || linux || windows

package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestSFTPFilterRemoteHeadlessFlow(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	viewState := session.files
	remote.mu.Lock()
	remote.entries[viewState.path] = append(remote.entries[viewState.path], workbenchFileInfo{name: ".env", mode: 0o600})
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.allRows) == 3 })
	state.Click(hitSFTPFilterToggle)
	replaceSFTPFilter(t, state, session, false, "NOTES")

	var fallback app.TextInput
	preview, err := headless.Open(app.Config{
		Title: "Motty SFTP P0", Width: 1400, Height: 900, Tree: Tree(),
		Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
		TextInput: func(context app.TextInputContext) *app.TextInput {
			if input := state.SFTPInput(context); input != nil {
				return input
			}
			if editor := state.EditorFor(context.ID); editor != nil {
				fallback = app.TextInput{Editor: editor}
				return &fallback
			}
			return nil
		},
	}, nil, headless.Options{Scale: 1})
	if err != nil {
		t.Fatal(err)
	}
	defer preview.Close()
	if err := preview.Wait(300 * time.Millisecond); err != nil {
		t.Fatal(err)
	}
	for _, id := range []gfx.ID{contextID(hitSFTPFilter), contextID(hitSFTPHiddenToggle), contextID(hitSFTPPathEdit)} {
		found := false
		for _, hit := range preview.Snapshot().Hits {
			found = found || hit.ID == id
		}
		if !found {
			t.Fatalf("missing SFTP P0 control %v", id)
		}
	}
	pathShortcut := "ctrl+l"
	if runtime.GOOS == "darwin" {
		pathShortcut = "cmd+l"
	}
	terminalID := state.activeTerminalFocusHit()
	terminalFocused := false
	for _, hit := range preview.Snapshot().Hits {
		if hit.ID == terminalID {
			if err := preview.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
				t.Fatal(err)
			}
			terminalFocused = true
			break
		}
	}
	if !terminalFocused {
		t.Fatal("missing terminal focus target")
	}
	terminalResult := state.Shortcut(gfx.KeyEvent{
		Target: terminalID, Modifiers: state.shortcuts.chords.Chord(shortcutFilesPath).Modifiers, Text: string(state.shortcuts.chords.Chord(shortcutFilesPath).Char),
	})
	if terminalResult.Handled {
		t.Fatal("path shortcut stole Ctrl/Cmd+L from the terminal")
	}
	var fileRow gfx.Hit
	for _, hit := range preview.Snapshot().Hits {
		if hit.ID == contextID(hitSFTPEntryList.At(0)) {
			fileRow = hit
			break
		}
	}
	if fileRow.ID.IsZero() {
		t.Fatal("missing file-list focus target")
	}
	if err := preview.Click(fileRow.Bounds.X+fileRow.Bounds.Width/2, fileRow.Bounds.Y+fileRow.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	if err := preview.Keys(pathShortcut); err != nil {
		t.Fatal(err)
	}
	if preview.Snapshot().Focused != contextID(hitSFTPPath) {
		t.Fatalf("path shortcut focused %v, want %v", preview.Snapshot().Focused, contextID(hitSFTPPath))
	}
	if err := preview.Type("x"); err != nil {
		t.Fatal(err)
	}
	if editor := state.EditorFor(hitSFTPPath); editor == nil || !strings.HasSuffix(editor.String(), "x") {
		t.Fatalf("typing after path shortcut did not reach path editor: %v", editor)
	}
	if dir := os.Getenv("MOTTY_SFTP_P0_SCREENSHOTS"); dir != "" {
		if err := os.MkdirAll(dir, 0o755); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(dir, "remote-filter-dark.png")); err != nil {
			t.Fatal(err)
		}
		state.chooseAppearanceMode(appearancePinnedLight)
		state.Click(hitSFTPFilterClear)
		if err := preview.Resize(1400, 900); err != nil {
			t.Fatal(err)
		}
		if err := preview.Wait(300 * time.Millisecond); err != nil {
			t.Fatal(err)
		}
		if err := preview.Screenshot(filepath.Join(dir, "remote-files-light.png")); err != nil {
			t.Fatal(err)
		}
	}
}
