package workbench

import (
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/sftpclient"
	"motty/internal/term"
)

// A file-transfer window opens its own view, separate from the docked panel.
// The terminal's folder report must move that window too, not just the docked
// panel it may no longer even have.
func TestFolderFollowMovesADetachedFileWindow(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	// A detached window opens with its local pane active; its view.local is
	// true until the user picks the remote side. The folder must still move.
	windowView := &sftpView{worker: session.files.worker, local: true}
	windowView.remember("/home/test/docs", []sftpclient.Entry{{Name: "docs", Mode: 0o755}})
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	session.files = nil
	if !state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("the detached file window was not offered the folder")
	}
	if windowView.path != "/home/test/docs" {
		t.Fatalf("the detached window stayed at %q", windowView.path)
	}
}

// A panel that is busy keeps the report, so it does not lose the folder just
// because another panel could take it.
func TestFolderFollowKeepsTheReportWhileAWindowIsBusy(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	docked := session.files
	docked.remember("/home/test/docs", []sftpclient.Entry{{Name: "docs", Mode: 0o755}})
	docked.path = "/home/test/docs"
	windowView := &sftpView{worker: session.files.worker, busy: true}
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	if state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("a busy window dropped the report")
	}
}

// While a panel is busy the report is kept; once the folder has been taken, a
// later pump must not drag the panel back after the user browsed away.
func TestFolderFollowDoesNotDragAPanelBackAfterTheUserBrowses(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	session.term.followSeq = 7
	view := session.files
	view.remember("/home/test/docs", []sftpclient.Entry{{Name: "docs", Mode: 0o755}})
	view.path = "/home/test/docs"
	if !state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("the panel did not take the folder")
	}
	view.path = "/etc"
	if !state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("the kept report was not settled")
	}
	if view.path != "/etc" {
		t.Fatalf("the panel was dragged back to %q", view.path)
	}
}

// When the user is editing a path in the detached window, that panel is theirs;
// the report is settled rather than kept, so finishing the edit does not drag
// them off to wherever the terminal was.
func TestFolderFollowSettlesTheReportWhileTheUserEditsAWindowPath(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	session.term.followSeq = 9
	windowView := &sftpView{worker: session.files.worker}
	windowView.remember("/home/test", []sftpclient.Entry{{Name: "test", Mode: 0o755}})
	windowView.pathEditing = true
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	session.files = nil
	if state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("a panel the user is editing took the folder")
	}
	windowView.pathEditing = false
	windowView.path = "/etc"
	if !state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("the settled report was kept")
	}
	if windowView.path != "/etc" {
		t.Fatalf("the window was dragged back to %q", windowView.path)
	}
}

// A drag in progress is short-lived: the report waits for it to finish rather
// than navigating the window out from under the file being dragged.
func TestFolderFollowKeepsTheReportWhileAWindowDrags(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	windowView := &sftpView{worker: session.files.worker}
	windowView.remember("/home/test/docs", []sftpclient.Entry{{Name: "docs", Mode: 0o755}})
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	window.drag = sftpWindowDrag{armed: true}
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	session.files = nil
	if state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("a drag released the report")
	}
	window.drag = sftpWindowDrag{}
	if !state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("the report was dropped after the drag")
	}
	if windowView.path != "/home/test/docs" {
		t.Fatalf("the window stayed at %q", windowView.path)
	}
}

// Another window's file drag hovering over this window's pane is just as
// short-lived; do not navigate the pane out from under the drop.
func TestFolderFollowKeepsTheReportWhileAFileIsDraggedOverTheWindow(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	windowView := &sftpView{worker: session.files.worker}
	windowView.remember("/home/test/docs", []sftpclient.Entry{{Name: "docs", Mode: 0o755}})
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	window.foreign = sftpForeignDrag{active: true, target: "/home/test/docs"}
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	session.files = nil
	if state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("a hovered drop released the report")
	}
	window.foreign = sftpForeignDrag{}
	if !state.followSFTPViews(session, "/home/test/docs") {
		t.Fatal("the report was dropped after the drop")
	}
}

// With the local pane active, the user editing that path is still the user
// working in the window; the report must wait rather than reset their draft.
func TestFolderFollowWaitsWhileTheLocalPanePathIsBeingEdited(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	windowView := &sftpView{worker: session.files.worker, local: true}
	windowView.remember("/srv/proj", []sftpclient.Entry{{Name: "proj", Mode: 0o755}})
	windowView.lfolder.pathEditing = true
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	session.files = nil
	if state.followSFTPViews(session, "/srv/proj/sub") {
		t.Fatal("a path being edited in the local pane took the folder")
	}
}

// A report that arrives while the panel is still loading the previous folder
// must not be mistaken for the user walking away when that listing lands.
func TestFolderFollowDoesNotDropAReportFromALoadingPanel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.remember("/home/old", []sftpclient.Entry{{Name: "old", Mode: 0o755}})
	view.path = "/home/old"
	view.busy = true
	view.navigation = sftpNavigation{id: 3, path: "/home/A"}
	session.term.followSeq = 2
	if state.followSFTPViews(session, "/home/B") {
		t.Fatal("a loading panel released the report")
	}
	view.busy = false
	view.navigation = sftpNavigation{}
	view.path = "/home/A"
	if !state.followSFTPViews(session, "/home/B") {
		t.Fatal("the report was dropped as if the user had moved away")
	}
	if view.followSeq != 2 {
		t.Fatal("the report was not taken")
	}
}

// Following a detached window must go through its own navigation, so a paired
// dual-pane sync moves both sides (and can stop with its warning) rather than
// leaving the two panes on unrelated folders.
func TestFolderFollowKeepsADetachedWindowsPairedPanesInStep(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	windowView := &sftpView{worker: session.files.worker}
	windowView.remember("/srv/proj", []sftpclient.Entry{{Name: "proj", Mode: 0o755}})
	windowView.lfolder.path = "/home/u/proj"
	window := &sftpWindow{owner: state, session: *session, view: windowView}
	windowView.detached = window
	window.browse.enabled = true
	window.browse.roots = [2]string{"/home/u/proj", "/srv/proj"}
	state.sftpWindows = map[uint32]*sftpWindow{session.ID: window}
	session.files = nil
	if !state.followSFTPViews(session, "/srv/proj/sub") {
		t.Fatal("the detached window did not take the folder")
	}
	if window.browse.pending == nil {
		t.Fatal("the paired panes were not moved together")
	}
	if window.browse.pending.paths[1] != "/srv/proj/sub" || window.browse.pending.paths[0] != filepath.Clean("/home/u/proj/sub") {
		t.Fatalf("paired folders = %v", window.browse.pending.paths)
	}
}

// A listing that fails or is cancelled leaves the panel where it was; the
// folder report must still be applied rather than mistaken for the user moving.
func TestFolderFollowRetriesAReportWhenTheListingDidNotLand(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.remember("/home/old", []sftpclient.Entry{{Name: "old", Mode: 0o755}})
	view.path = "/home/old"
	view.busy = true
	view.navigation = sftpNavigation{id: 4, path: "/home/A"}
	session.term.followSeq = 2
	if state.followSFTPViews(session, "/home/B") {
		t.Fatal("a loading panel released the report")
	}
	// The listing failed: not busy, path still at the folder it showed.
	view.busy = false
	view.navigation = sftpNavigation{}
	if !state.followSFTPViews(session, "/home/B") {
		t.Fatal("the report was dropped after a failed listing")
	}
	if view.followSeq != 2 {
		t.Fatal("the report was not retried")
	}
}

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
