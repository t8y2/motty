package workbench

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
)

func TestLocalSFTPContextMenuOpensOriginalFileWithoutRemoteEditing(t *testing.T) {
	for _, localOnly := range []bool{false, true} {
		t.Run(map[bool]string{false: "ssh-panel", true: "local-terminal"}[localOnly], func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			root := localFixture(t)
			view := openLocalPane(t, state, session, root)
			view.localOnly = localOnly
			clickSFTPLocalRow(t, state, view, "report.txt")
			opened := ""
			state.options.OpenPath = func(location string) error { opened = location; return nil }
			state.sftpMenu = sftpMenu{open: true, session: session.ID, at: gfx.Point{X: 300, Y: 200}}
			canvas, _ := paintedTexts(state)
			if hit, ok := hitByID(canvas, hitSFTPMenuEdit); !ok || hit.Disabled {
				t.Fatal("local editor menu action is missing or disabled")
			}
			state.Click(hitSFTPMenuEdit)
			if opened != filepath.Join(root, "report.txt") || state.edits.opening.active || len(state.edits.list) != 0 || len(view.worker.Transfers(nil)) != 0 {
				t.Fatal("local editor did not open the original file directly")
			}
		})
	}
}

func TestLocalSFTPEditorFailureUsesGlobalToast(t *testing.T) {
	for _, vanished := range []bool{false, true} {
		t.Run(map[bool]string{false: "system-error", true: "removed-file"}[vanished], func(t *testing.T) {
			state, session, _, _ := openTestSFTP(t)
			root := localFixture(t)
			view := openLocalPane(t, state, session, root)
			clickSFTPLocalRow(t, state, view, "report.txt")
			calls := 0
			state.options.OpenPath = func(string) error { calls++; return errors.New("editor unavailable") }
			if vanished {
				if err := os.Remove(filepath.Join(root, "report.txt")); err != nil {
					t.Fatal(err)
				}
			}
			state.Click(hitSFTPEditExternal)
			if view.errorText != "" || len(state.fileNotices.items) != 1 || state.fileNotices.items[0].kind != components.ToastError || vanished && calls != 0 {
				t.Fatal("local editor failure did not remain a global notice")
			}
		})
	}
}

func TestLocalSFTPExecutableRefusalUsesLocalizedGlobalNotice(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	root := localFixture(t)
	view := openLocalPane(t, state, session, root)
	clickSFTPLocalRow(t, state, view, "report.txt")
	state.options.OpenPath = func(string) error { return app.ErrExecutablePath }
	state.Click(hitSFTPEditExternal)
	if len(state.fileNotices.items) != 1 ||
		state.fileNotices.items[0].detail != state.messages.Tools.ExtEditRefused ||
		state.fileNotices.items[0].kind != components.ToastError {
		t.Fatal("executable refusal was not shown as a localized global notice")
	}
}

func TestLocalSFTPDirectoryAndMultipleSelectionHaveNoEditorAction(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openLocalPane(t, state, session, localFixture(t))
	for _, names := range [][]string{{"photos"}, {"report.txt", "Zeta.txt"}} {
		for i := range view.lfolder.rows {
			view.lfolder.rows[i].selected = false
		}
		for _, name := range names {
			clickSFTPLocalRow(t, state, view, name)
		}
		state.sftpMenu = sftpMenu{open: true, session: session.ID, at: gfx.Point{X: 300, Y: 200}}
		canvas, _ := paintedTexts(state)
		if _, ok := hitByID(canvas, hitSFTPMenuEdit); ok {
			t.Fatal("directory or multi-selection exposed an editor action")
		}
		state.sftpMenu.open = false
	}
}

func TestLocalSFTPSpecialFileHasNoEditorAction(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openLocalPane(t, state, session, localFixture(t))
	for i := range view.lfolder.rows {
		view.lfolder.rows[i].selected = view.lfolder.rows[i].entry.Name == "report.txt"
		if view.lfolder.rows[i].selected {
			view.lfolder.rows[i].entry.Mode = os.ModeNamedPipe | 0o600
		}
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID, at: gfx.Point{X: 300, Y: 200}}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitSFTPMenuEdit); ok {
		t.Fatal("a special local file exposed an editor action")
	}
}
