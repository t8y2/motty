//go:build darwin || linux || windows

package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sftpclient"
)

func TestSFTPInlineRenameThroughTheFileLists(t *testing.T) {
	for _, mode := range []string{"sidebar", "window", "tab"} {
		t.Run(mode, func(t *testing.T) {
			var state *State
			var view *sftpView
			var remote *workbenchSFTPRemote
			var window *sftpWindow
			var session *Session
			var preview *headless.Session
			switch mode {
			case "sidebar":
				state, session, _, remote = openTestSFTP(t)
				view = session.files
			case "window":
				var host *sftpNativeHost
				state, window, remote, host, _ = dualSFTPFixture(t, 1.5)
				view, preview = window.view, host.windows[0].view
			case "tab":
				state, _, window, remote, _ = openSFTPTabFixture(t)
				view = window.view
			}
			if preview == nil {
				var err error
				preview, err = headless.Open(state.WindowConfig(app.Config{
					Title: "Inline rename", Width: 1400, Height: 850, Tree: Tree(),
					Paint: state.Paint, OnKey: state.Key, OnPointer: state.Pointer,
					OnShortcut: state.Shortcut, OnResize: state.Resize, OnWake: state.Wake,
				}), nil, headless.Options{Scale: 1.5})
				if err != nil {
					t.Fatal(err)
				}
				defer preview.Close()
			}
			key := func(key gfx.Key) {
				t.Helper()
				if err := preview.Driver().Key(key, 0); err != nil {
					t.Fatal(err)
				}
			}
			rest := func() {
				t.Helper()
				if err := preview.Rest(); err != nil {
					t.Fatal(err)
				}
			}
			rowID := func(name string) gfx.ID {
				t.Helper()
				if window != nil {
					return dualRow(t, window, false, name)
				}
				index := sftpRowNameIndex(view.rows, name)
				return sftpListRowID(t, &view.sftpFolder, index)
			}
			begin := func() {
				t.Helper()
				dualClickID(t, preview, rowID("notes.txt"))
				key(gfx.KeyF2)
				rest()
				if view.dialog != sftpDialogNone || view.rename == nil || preview.Snapshot().Focused != hitSFTPInlineRename {
					t.Fatalf("did not focus the inline editor: focus=%v edit=%t", preview.Snapshot().Focused, view.rename != nil)
				}
				field, row := dualHit(t, preview, hitSFTPInlineRename), dualHit(t, preview, rowID("notes.txt"))
				if field.X <= row.X || field.Y < row.Y || field.Y+field.Height > row.Y+row.Height {
					t.Fatalf("input is outside the file name row: %v within %v", field, row)
				}
			}
			wait := func(done func() bool) {
				t.Helper()
				if window == nil {
					waitSFTP(t, state, session, func(*sftpView) bool { return done() })
					rest()
				} else {
					dualWait(t, state, window, preview, done)
				}
			}
			rest()

			begin()
			if err := preview.Type("改名"); err != nil {
				t.Fatal(err)
			}
			if got := view.rename.editor.String(); got != "改名.txt" {
				t.Fatalf("typing should replace only the name stem: %q", got)
			}
			key(gfx.KeyEscape)
			if view.rename != nil || view.busy || sftpRowNameIndex(view.rows, "notes.txt") < 0 {
				t.Fatal("Escape did not discard the edit")
			}

			begin()
			if err := preview.Type("discard"); err != nil {
				t.Fatal(err)
			}
			dualClickID(t, preview, rowID("docs"))
			rest()
			if view.rename != nil || view.busy {
				t.Fatal("clicking another row submitted or kept an inactive draft")
			}

			begin()
			remote.mu.Lock()
			remote.renameErrAt = map[string]error{"/home/test/notes.txt": errors.New("rename refused by server")}
			remote.mu.Unlock()
			if err := preview.Type("denied"); err != nil {
				t.Fatal(err)
			}
			key(gfx.KeyEnter)
			wait(func() bool { return !view.busy && strings.Contains(view.errorText, "rename refused by server") })
			if sftpRowNameIndex(view.rows, "notes.txt") < 0 {
				t.Fatal("server error removed the original row")
			}

			begin()
			remote.mu.Lock()
			remote.renameErrAt = nil
			remote.mu.Unlock()
			if err := preview.Compose("改名"); err != nil {
				t.Fatal(err)
			}
			key(gfx.KeyEnter)
			if view.rename == nil || view.busy {
				t.Fatal("Enter submitted the name before the input method committed it")
			}
			view.rename.editor.Unmark()
			if got := view.rename.editor.String(); got != "改名.txt" {
				t.Fatalf("input method changed the extension or left the old stem: %q", got)
			}
			key(gfx.KeyEnter)
			wait(func() bool { return !view.busy && sftpRowNameIndex(view.rows, "改名.txt") >= 0 })
			if count, entry := selectedSFTP(view); count != 1 || entry.Name != "改名.txt" {
				t.Fatalf("the renamed file was not kept selected: %d, %q", count, entry.Name)
			}
			if view.operation != sftpclient.OperationList || view.rename != nil {
				t.Fatal("rename did not refresh the list and end the edit")
			}
			toasts := state.fileNotices.items
			if window != nil {
				toasts = window.notices.items
			}
			if len(toasts) < 2 || toasts[len(toasts)-2].kind != components.ToastError ||
				toasts[len(toasts)-1].kind != components.ToastSuccess {
				t.Fatalf("expected a failure followed by a success toast: %+v", toasts)
			}
		})
	}
}
