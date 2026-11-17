//go:build darwin || linux || windows

package workbench

import (
	"fmt"
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

func TestSFTPPanelBottomFolderKeepsItsPositionAfterSelection(t *testing.T) {
	for _, height := range []int{600, 900} {
		t.Run(fmt.Sprint(height), func(t *testing.T) {
			state, session, _, remote := openTestSFTP(t)
			view := session.files
			remote.mu.Lock()
			entries := make([]os.FileInfo, 80)
			for i := range entries {
				name := fmt.Sprintf("folder-%03d", i)
				entries[i] = workbenchFileInfo{name: name, mode: os.ModeDir | 0o755}
				remote.entries[view.path+"/"+name] = nil
			}
			remote.entries[view.path] = entries
			remote.mu.Unlock()
			state.Click(hitSFTPRefresh)
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && len(v.rows) == 80 })
			preview, err := headless.Open(app.Config{
				Title: "SFTP bottom folder", Width: 1400, Height: height, Tree: Tree(),
				Paint: state.Paint, OnWake: state.Wake, OnPointer: state.Pointer,
				OnKey: state.Key, OnShortcut: state.Shortcut, OnScroll: state.Scroll, OnResize: state.Resize,
			}, nil, headless.Options{Scale: 1})
			if err != nil {
				t.Fatal(err)
			}
			defer preview.Close()
			if err := preview.Driver().Pump(); err != nil {
				t.Fatal(err)
			}
			list := state.chrome.sftpPanel.listSlot.Rect()
			for range 100 {
				if err := preview.Scroll(gfx.ScrollEvent{X: list.X + 24, Y: list.Y + 24, DeltaY: 60, Precise: true}); err != nil {
					t.Fatal(err)
				}
			}
			last := sftpRowIndex(t, view, "folder-079")
			rowID := sftpListRowID(t, view.active(), last)
			findRow := func() gfx.Rect {
				t.Helper()
				for _, hit := range preview.Snapshot().Hits {
					if hit.ID == rowID {
						return hit.Bounds
					}
				}
				t.Fatal("last folder is not visible")
				return gfx.Rect{}
			}
			before := findRow()
			x, y := before.X+before.Width/2, before.Y+before.Height/2
			if err := preview.Click(x, y); err != nil {
				t.Fatal(err)
			}
			if err := preview.Driver().Pump(); err != nil {
				t.Fatal(err)
			}
			if count, _ := selectedSFTP(view); count != 1 || !view.rows[last].selected {
				t.Fatalf("click did not select the last folder: count=%d selected=%d last=%d row=%+v list=%+v", count, view.selected, last, before, list)
			}
			if after := state.chrome.sftpPanel.listSlot.Rect(); after != list {
				t.Fatalf("selection changed list bounds: before=%+v after=%+v", list, after)
			}
			if after := findRow(); after != before {
				t.Fatalf("selection moved last folder: before=%+v after=%+v", before, after)
			}
			if err := preview.DoubleClick(x, y); err != nil {
				t.Fatal(err)
			}
			waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && strings.HasSuffix(v.path, "/folder-079") })
		})
	}
}

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
		if hit.ID == sftpListRowID(t, viewState.active(), 0) {
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
