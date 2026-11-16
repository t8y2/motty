package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/term"
)

func TestFolderFollowRemembersReportsBeforePanelOpening(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	state.closeSFTP(session)
	shell := fake.shell(t)
	shell.say("\x1b]7;file://box/home/test/docs\x07")
	state.Wake()
	state.openSFTP(session)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && view.path == "/home/test/docs" && len(view.rows) == 1
	})
}

func TestFolderFollowReopeningUsesTheLastReportedDirectory(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	fake.shell(t).say("\x1b]7;file://box/home/test/docs\x07")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	state.closeSFTP(session)
	state.openSFTP(session)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
}

func TestFolderFollowCoalescesBlockedReports(t *testing.T) {
	for _, blocker := range []string{"busy", "path-editing", "path-input-draft", "dialog", "file-editing", "local-pane"} {
		t.Run(blocker, func(t *testing.T) {
			state, session, fake, remote := openTestSFTP(t)
			view := session.files
			switch blocker {
			case "busy":
				view.busy = true
			case "path-editing":
				view.pathEditing = true
			case "path-input-draft":
				state.sftpPathInput = true
				*view.pathInput() = *text.NewEditor("/unfinished")
			case "dialog":
				view.dialog = sftpDialogCreateDirectory
			case "file-editing":
				view.editPath = "/home/test/notes.txt"
			case "local-pane":
				view.local = true
			}
			fake.shell(t).say("\x1b]7;file://box/skip\x07\x1b]7;file://box/home/test/docs\x07")
			state.Wake()
			if view.path != "/home/test" {
				t.Fatalf("navigation interrupted %s: %q", blocker, view.path)
			}
			if blocker != "busy" && view.busy {
				t.Fatalf("a listing started while %s was protected", blocker)
			}
			view.busy, view.pathEditing, view.local = false, false, false
			view.dialog, view.editPath = sftpDialogNone, ""
			view.resetPathInput()
			waitSFTP(t, state, session, func(view *sftpView) bool {
				return !view.busy && view.path == "/home/test/docs" && len(view.rows) == 1
			})
			if remote.readCount("/skip") != 0 {
				t.Fatal("an obsolete report was listed")
			}
			state.openSFTPDirectory(view, "/home/test")
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
			state.Wake()
			if view.path != "/home/test" {
				t.Fatal("a consumed report kept snapping the panel back")
			}
		})
	}
}

func TestFolderFollowOffAndUnknownSessionDoNotMovePanel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.sftpFollowOff = true
	state.terminalDirectory(session.ID, term.Cwd{Path: "/home/test/docs"})
	state.terminalDirectory(session.ID+1000, term.Cwd{Path: "/home/test/docs"})
	state.Wake()
	if session.files.path != "/home/test" {
		t.Fatal("disabled or unknown-session report moved the panel")
	}
}
