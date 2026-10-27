package workbench

import (
	"context"
	"errors"
	"os"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
)

func TestRemoteFolderContextMenuCompressesOnTheServerAndRefreshes(t *testing.T) {
	state, session, fake, remote := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "docs")
	state.sftpMenu = sftpMenu{open: true, session: session.ID, at: ui.Point{X: 80, Y: 140}}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitSFTPMenuCompress); !ok {
		t.Fatal("remote folder has no Compress menu item")
	}
	state.Click(hitSFTPMenuCompress)
	if view.dialog != sftpDialogCompress || view.dialogEditor.String() != "docs.tar.gz" {
		t.Fatalf("compression dialog = %v, name = %q", view.dialog, view.dialogEditor.String())
	}
	*state.EditorFor(hitSFTPDialogField) = *text.NewEditor("bad/name.zip")
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogCompress || view.dialogError == "" {
		t.Fatal("invalid archive filename was accepted")
	}
	var command string
	fake.mu.Lock()
	fake.runCommand = func(_ context.Context, cmd string, _ int) ([]byte, error) {
		command = cmd
		remote.mu.Lock()
		remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{name: "docs.zip", size: 100, mode: 0o644, when: time.Now()})
		remote.mu.Unlock()
		return nil, nil
	}
	fake.mu.Unlock()
	*state.EditorFor(hitSFTPDialogField) = *text.NewEditor("docs.zip")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && !view.extracting && len(view.rows) == 3 })
	if !strings.Contains(command, "zip -qr") || !strings.Contains(command, "'/home/test/docs.zip'") || !strings.Contains(command, "-- 'docs'") {
		t.Fatalf("remote command = %s", command)
	}
	if len(view.transfers) != 0 || view.errorText != "" || !strings.Contains(view.status, "docs.zip") {
		t.Fatalf("compression unexpectedly transferred a file or failed: %q", view.status)
	}
}

func TestArchiveFailuresUseGlobalLocalizedNotices(t *testing.T) {
	for _, compress := range []bool{false, true} {
		for _, test := range []struct {
			output, want string
			err          error
		}{
			{archiveMissingTool + "unzip\n", "服务器未安装 unzip", errors.New("exit 127")},
			{archiveExists + "\n", "/home/test/destination", errors.New("exit 17")},
			{"bad CRC in archive\n", "bad CRC", errors.New("exit 2")},
			{"", "permission denied", os.ErrPermission},
			{"", "已取消", context.Canceled},
		} {
			t.Run(test.want, func(t *testing.T) {
				state, session, _, _ := openTestSFTP(t)
				state.setLanguage(i18n.Chinese)
				view := session.files
				view.busy, view.extracting, view.archiveSequence = true, true, 1
				state.finishSFTPArchive(session.ID, view, 1, view.path, "中文.zip", "destination", compress, []byte(test.output), test.err)
				if view.errorText != "" || view.busy || view.extracting || len(state.fileNotices.items) != 1 {
					t.Fatalf("archive error remained inline or did not settle: %q", view.errorText)
				}
				note := state.fileNotices.items[0]
				if !strings.Contains(note.detail, test.want) || !strings.Contains(note.detail, "/home/test/中文.zip") {
					t.Fatalf("global notice = %+v", note)
				}
				if test.err != context.Canceled && note.kind != components.ToastError {
					t.Fatal("failure is not an error toast")
				}
			})
		}
	}
}

func TestArchiveCompletionCannotChangeAReopenedPanelOrANewerTask(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	old := session.files
	old.busy, old.extracting, old.archiveSequence = true, true, 1
	replacement := &sftpView{busy: true, extracting: true, archiveSequence: 2}
	session.files = replacement
	state.finishSFTPArchive(session.ID, old, 1, "/home/test", "old.zip", "old", false, nil, errors.New("old error"))
	state.finishSFTPArchive(session.ID, replacement, 1, "/home/test", "old.zip", "old", false, nil, errors.New("old error"))
	if !replacement.busy || !replacement.extracting || len(state.fileNotices.items) != 0 {
		t.Fatal("an old completion changed a new panel or task")
	}
	session.files = old // the fixture owns and closes its worker
}

func TestArchiveCommandKeepsThePanelBusyDuringUnrelatedWorkerReplies(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.entries[view.path] = append(remote.entries[view.path], workbenchFileInfo{name: "new.txt", mode: 0o644})
	remote.mu.Unlock()
	view.busy, view.extracting = true, true
	if !view.worker.List(view.path) {
		t.Fatal("list was not queued")
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return len(view.rows) == 3 })
	if !view.busy {
		t.Fatal("a worker reply unlocked the panel during an archive command")
	}
	view.busy, view.extracting = false, false
}

func TestArchiveDialogIMEConfirmationDoesNotSubmit(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPCompress)
	view.dialogEditor.SetMarkedText("中文", text.Range{Start: 0, End: 2})
	state.Key(ui.KeyEvent{Target: contextID(hitSFTPDialogField), Key: ui.KeyEnter})
	if view.dialog != sftpDialogCompress || view.dialogError != "" || view.extracting {
		t.Fatal("confirming IME composition submitted the archive dialog")
	}
}

func TestArchiveCompletionTravelsWithItsPanelToAnotherWindow(t *testing.T) {
	source, sourceSession, fake, _ := openTestSFTP(t)
	target, targetSession, _, _ := openTestSFTP(t)
	started, release := make(chan struct{}), make(chan struct{})
	fake.mu.Lock()
	fake.runCommand = func(context.Context, string, int) ([]byte, error) {
		close(started)
		<-release
		return []byte(archiveMissingTool + "zip"), errors.New("exit 127")
	}
	fake.mu.Unlock()
	view := sourceSession.files
	source.startSFTPArchive(sourceSession, view, view.path, "docs", "docs.zip", "server command", true)
	<-started
	target.closeSFTP(targetSession)
	sourceSession.files = nil
	targetSession.files = view
	close(release)
	waitSFTP(t, target, targetSession, func(view *sftpView) bool { return !view.extracting })
	if len(source.fileNotices.items) != 0 || len(target.fileNotices.items) != 1 || view.busy {
		t.Fatal("archive completion did not reach the panel's current window")
	}
}
