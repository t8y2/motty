package workbench

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"motty/internal/sftpclient"
)

type openedPaths struct {
	paths []string
	err   error
}

func externalEditSetup(t *testing.T) (*State, *Session, *workbenchSFTPRemote, *openedPaths) {
	t.Helper()
	state, session, _, remote := openTestSFTP(t)
	opened := &openedPaths{}
	state.options.OpenPath = func(path string) error {
		opened.paths = append(opened.paths, path)
		return opened.err
	}
	state.options.EditPollInterval = 5 * time.Millisecond
	return state, session, remote, opened
}

func startExternalEditOf(t *testing.T, state *State, session *Session, name string) *externalEdit {
	t.Helper()
	clickSFTPRow(t, state, session.files, name)
	state.Click(hitSFTPEditExternal)
	waitFor(t, state, func() bool { return len(state.edits.list) == 1 || session.files.errorText != "" })
	if len(state.edits.list) != 1 {
		t.Fatalf("no edit started: %q", session.files.errorText)
	}
	return &state.edits.list[0]
}

func remoteText(remote *workbenchSFTPRemote, name string) string {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	return string(remote.files[name])
}

func TestEditingInTheSystemEditorSendsEverySaveBack(t *testing.T) {
	state, session, remote, opened := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	if len(opened.paths) != 1 || opened.paths[0] != edit.local || !strings.HasSuffix(edit.local, "notes.txt") {
		t.Fatalf("opened %v for %q", opened.paths, edit.local)
	}
	data, err := os.ReadFile(edit.local)
	if err != nil || string(data) != "hello world" {
		t.Fatalf("local copy %q, %v", data, err)
	}
	// Windows reports no Unix permission bits; its folders are private to the
	// profile that made them.
	if info, _ := os.Stat(edit.dir); info == nil || runtime.GOOS != "windows" && info.Mode().Perm()&0o077 != 0 {
		t.Fatalf("the folder is not private: %v", info)
	}

	// Looking at an unchanged file sends nothing.
	time.Sleep(30 * time.Millisecond)
	state.Wake()
	if remoteText(remote, "/home/test/notes.txt") != "hello world" || session.files.status == fmt.Sprintf(state.messages.Tools.ExtEditSentFmt, "notes.txt") {
		t.Fatal("an untouched file was sent")
	}

	if err := os.WriteFile(edit.local, []byte("edited elsewhere\n中文\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool {
		return remoteText(remote, "/home/test/notes.txt") == "edited elsewhere\n中文\n" && !state.edits.list[0].saving
	})
	if session.files.status != fmt.Sprintf(state.messages.Tools.ExtEditSentFmt, "notes.txt") {
		t.Fatalf("status %q", session.files.status)
	}
	if len(state.fileNotices.items) == 0 {
		t.Fatal("sending an edit back did not report success")
	}
	notice := state.fileNotices.items[len(state.fileNotices.items)-1]
	if notice.title != "notes.txt" || notice.detail != fmt.Sprintf(state.messages.Tools.ExtEditSentFmt, "notes.txt") || notice.kind != components.ToastSuccess {
		t.Fatalf("send notice = %+v", notice)
	}

	// A second save goes on the identity the first one left, with no conflict.
	if err := os.WriteFile(edit.local, []byte("second save\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return remoteText(remote, "/home/test/notes.txt") == "second save\n" })
	if state.modal != modalNone {
		t.Fatalf("modal %d", state.modal)
	}
}

func TestAFileThatChangedOnTheServerIsNotReplacedUnasked(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	remote.mu.Lock()
	remote.files["/home/test/notes.txt"] = []byte("someone else\n")
	remote.mu.Unlock()
	if err := os.WriteFile(edit.local, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return state.modal == modalForm })
	if state.form.title != state.messages.Tools.ExtEditConflictTitle || remoteText(remote, "/home/test/notes.txt") != "someone else\n" {
		t.Fatalf("form %q, remote %q", state.form.title, remoteText(remote, "/home/test/notes.txt"))
	}

	// Keeping: nothing is sent, and the next save asks again.
	state.Click(hitFormCancel)
	time.Sleep(30 * time.Millisecond)
	state.Wake()
	if state.modal != modalNone || remoteText(remote, "/home/test/notes.txt") != "someone else\n" {
		t.Fatalf("keeping: modal %d, remote %q", state.modal, remoteText(remote, "/home/test/notes.txt"))
	}
	if err := os.WriteFile(edit.local, []byte("mine again\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return state.modal == modalForm })

	// Replacing: the server's file is read again for its identity, then sent over.
	state.Click(hitFormSubmit)
	waitFor(t, state, func() bool { return remoteText(remote, "/home/test/notes.txt") == "mine again\n" })
}

func TestAFileTheSystemWouldRunIsNotOpenedAsAnEdit(t *testing.T) {
	state, session, _, opened := externalEditSetup(t)
	opened.err = fmt.Errorf("%w: notes.txt", app.ErrExecutablePath)
	clickSFTPRow(t, state, session.files, "notes.txt")
	state.Click(hitSFTPEditExternal)
	waitFor(t, state, func() bool { return session.files.errorText != "" })
	if session.files.errorText != state.messages.Tools.ExtEditRefused || len(state.edits.list) != 0 || state.edits.watching {
		t.Fatalf("error %q, %d edits, watching %v", session.files.errorText, len(state.edits.list), state.edits.watching)
	}
	if len(opened.paths) != 1 {
		t.Fatalf("opened %v", opened.paths)
	}
	if _, err := os.Stat(opened.paths[0]); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the temporary copy of a refused file stays: %v", err)
	}
}

func TestContentThatIsNotTextIsNotSentBack(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	if err := os.WriteFile(edit.local, []byte{0xff, 0xfe, 0x00, 'x'}, 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return session.files.errorText != "" })
	if remoteText(remote, "/home/test/notes.txt") != "hello world" || !strings.Contains(session.files.errorText, "UTF-8") {
		t.Fatalf("remote %q, error %q", remoteText(remote, "/home/test/notes.txt"), session.files.errorText)
	}
}

func TestClosingTheSessionRemovesTheTemporaryCopyAndStopsLooking(t *testing.T) {
	state, session, _, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	dir := edit.dir
	if !state.edits.watching {
		t.Fatal("nothing is watching the open edit")
	}
	state.closeSession(session.ID)
	if _, err := os.Stat(dir); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("the folder stays: %v", err)
	}
	if len(state.edits.list) != 0 || state.edits.watching {
		t.Fatalf("%d edits, watching %v", len(state.edits.list), state.edits.watching)
	}
}

func TestAnEditOfTheSameFileIsNotOpenedTwice(t *testing.T) {
	state, session, _, opened := externalEditSetup(t)
	startExternalEditOf(t, state, session, "notes.txt")
	state.Click(hitSFTPEditExternal)
	if len(state.edits.list) != 1 || len(opened.paths) != 2 || opened.paths[0] != opened.paths[1] {
		t.Fatalf("%d edits, opened %v", len(state.edits.list), opened.paths)
	}
}

func TestLocalNamesKeepTheExtensionAndDropTheDangerousCharacters(t *testing.T) {
	for remote, want := range map[string]string{
		"/etc/nginx/nginx.conf": "nginx.conf",
		"/tmp/ok name.sh":       "ok name.sh",
	} {
		if got := safeLocalName(remote); got != want {
			t.Errorf("%q -> %q, want %q", remote, got, want)
		}
	}
	// A name that had to change keeps its extension last and a digest of the
	// original, so a:b and a*b cannot land on one local name.
	dangerous := safeLocalName("/tmp/a:b*c?.txt")
	if strings.ContainsAny(dangerous, `<>:"/\|?*`) || !strings.HasSuffix(dangerous, ".txt") || !strings.Contains(dangerous, "~") {
		t.Errorf("dangerous name = %q", dangerous)
	}
	if safeLocalName("/tmp/a:b.txt") == safeLocalName("/tmp/a*b.txt") {
		t.Error("distinct remote names share one local name")
	}
	if got := safeLocalName("/tmp/.."); !strings.HasPrefix(got, "file") {
		t.Errorf("parent name -> %q", got)
	}
}

// The system editor's copy is the only thing a save touches, so a save that did
// not reach the server must be said out loud and must not be replaced by asking
// for the file again: what the editor showed would then quietly become stale.
func TestASaveThatCannotBeSentIsReportedAndItsCopyIsKept(t *testing.T) {
	state, session, remote, opened := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	refuseReplacing(t, remote, "/home/test/notes.txt")
	if err := os.WriteFile(edit.local, []byte("not sent\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool {
		return !state.edits.list[0].saving && session.files.errorText != ""
	})
	if got := remoteText(remote, "/home/test/notes.txt"); got != "hello world" {
		t.Fatalf("the server was changed by a failed save: %q", got)
	}
	if !strings.Contains(session.files.errorText, "notes.txt") {
		t.Fatalf("the failure did not name the file: %q", session.files.errorText)
	}
	if len(state.fileNotices.items) == 0 {
		t.Fatal("a save that did not reach the server said nothing")
	}
	notice := state.fileNotices.items[len(state.fileNotices.items)-1]
	if notice.detail != session.files.errorText || notice.kind != components.ToastError {
		t.Fatalf("failure notice = %+v, panel said %q", notice, session.files.errorText)
	}

	// Asking for the file again warns that the copy here is ahead of the
	// server, and still opens that copy: the editor has the user's text.
	state.Click(hitSFTPEditExternal)
	want := fmt.Sprintf(state.messages.Tools.ExtEditUnsentFmt, "notes.txt")
	if session.files.errorText != want {
		t.Fatalf("reopening an unsent copy said %q, want %q", session.files.errorText, want)
	}
	if len(opened.paths) != 2 || opened.paths[1] != edit.local {
		t.Fatalf("reopened %v", opened.paths)
	}

	// Once a save does reach the server the copy is no longer unsent.
	allowReplacing(remote)
	if err := os.WriteFile(edit.local, []byte("second save\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool {
		return remoteText(remote, "/home/test/notes.txt") == "second save\n" && !state.edits.list[0].saving
	})
	if externalEditHeldLocally(&state.edits.list[0]) {
		t.Fatal("a save that reached the server left the copy ahead of it")
	}
	state.Click(hitSFTPEditExternal)
	if session.files.errorText != "" {
		t.Fatalf("a copy the server has was still warned about: %q", session.files.errorText)
	}
}

// A connection that ends with the save in flight publishes no answer, so the
// edit would wait for it forever and every later save would be skipped.
func TestASaveWhoseWorkerEndedIsSentAgain(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	edit.saving, edit.worker = true, deadSFTPWorker()
	if err := os.WriteFile(edit.local, []byte("sent after the worker ended\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool {
		e := &state.edits.list[0]
		return remoteText(remote, "/home/test/notes.txt") == "sent after the worker ended\n" && !e.saving && e.worker == nil
	})
	if externalEditHeldLocally(&state.edits.list[0]) {
		t.Fatal("the retried send left the copy ahead of the server")
	}
}

// A read whose worker died is answered nowhere either; without dropping it no
// later edit of the run could start.
func TestAnOpenWhoseWorkerEndedDoesNotBlockTheNextOne(t *testing.T) {
	state, session, _, _ := externalEditSetup(t)
	state.edits.opening.active = true
	state.edits.opening.session = session.ID
	state.edits.opening.remote = "/home/test/notes.txt"
	state.edits.opening.worker = deadSFTPWorker()
	startExternalEditOf(t, state, session, "notes.txt")
	if len(state.edits.list) != 1 {
		t.Fatalf("%d edits", len(state.edits.list))
	}
}

// A file the user kept over a conflict is still only in the local copy, and
// asking for it again says so instead of passing that copy off as the server's.
func TestKeepingTheLocalVersionLeavesTheCopyAheadOfTheServer(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	remote.mu.Lock()
	remote.files["/home/test/notes.txt"] = []byte("someone else\n")
	remote.mu.Unlock()
	if err := os.WriteFile(edit.local, []byte("mine\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return state.modal == modalForm })
	state.Click(hitFormCancel)
	if !externalEditHeldLocally(&state.edits.list[0]) {
		t.Fatal("keeping the local version left the copy looking like the server's")
	}
	if got := remoteText(remote, "/home/test/notes.txt"); got != "someone else\n" {
		t.Fatalf("keeping the local version wrote the server: %q", got)
	}
	state.Click(hitSFTPEditExternal)
	want := fmt.Sprintf(state.messages.Tools.ExtEditUnsentFmt, "notes.txt")
	if session.files.errorText != want {
		t.Fatalf("reopening the kept copy said %q, want %q", session.files.errorText, want)
	}
}

func deadSFTPWorker() *sftpclient.Worker {
	worker := sftpclient.NewWorker(func() (sftpclient.Remote, error) {
		return nil, errors.New("gone")
	}, func() {})
	worker.Close()
	<-worker.Done()
	return worker
}

// refuseReplacing makes the server refuse every write onto one file, the way a
// full disk or a filesystem that rejects the temporary sibling does, so the save
// has to give up instead of falling back to writing the file itself.
func refuseReplacing(t *testing.T, remote *workbenchSFTPRemote, remotePath string) {
	t.Helper()
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.replaceErr == nil {
		remote.replaceErr = map[string]error{}
	}
	remote.replaceErr[remotePath] = errors.New("no space left on device")
}

func allowReplacing(remote *workbenchSFTPRemote) {
	remote.mu.Lock()
	remote.replaceErr = nil
	remote.mu.Unlock()
}

func TestEditingSameSizeAndTimestampSendsSaveBack(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	info, err := os.Stat(edit.local)
	if err != nil {
		t.Fatal(err)
	}
	originalTime := info.ModTime()

	// Replace "hello world" (11 bytes) with "HELLO WORLD" (11 bytes, same size)
	// and restore original timestamp to simulate filesystem timestamp granularity.
	if err := os.WriteFile(edit.local, []byte("HELLO WORLD"), 0o600); err != nil {
		t.Fatal(err)
	}
	_ = os.Chtimes(edit.local, originalTime, originalTime)

	waitFor(t, state, func() bool {
		return remoteText(remote, "/home/test/notes.txt") == "HELLO WORLD" && !state.edits.list[0].saving
	})
}

func TestRevertingWhileAnExternalSaveIsInFlightStillSendsTheRevert(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	remote.mu.Lock()
	locked := true
	defer func() {
		if locked {
			remote.mu.Unlock()
		}
	}()
	if err := os.WriteFile(edit.local, []byte("pending change"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return edit.saving })
	if err := os.WriteFile(edit.local, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	original := sha256.Sum256([]byte("hello world"))
	waitFor(t, state, func() bool { return edit.seenSum == original && edit.dirty })
	remote.mu.Unlock()
	locked = false
	waitFor(t, state, func() bool {
		return remoteText(remote, "/home/test/notes.txt") == "hello world" && !edit.saving && edit.synced == original
	})
}

func TestExternalEditRetriesAFailedSaveWithoutAnotherEdit(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	refuseReplacing(t, remote, "/home/test/notes.txt")
	if err := os.WriteFile(edit.local, []byte("retry this save"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return !edit.saving && edit.retryWait > 0 })
	allowReplacing(remote)
	waitFor(t, state, func() bool {
		return remoteText(remote, "/home/test/notes.txt") == "retry this save" && !edit.saving
	})
}

func TestAnUncertainSaveDoesNotLoseARevertedLocalCopy(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	remote.mu.Lock()
	remote.files["/home/test/notes.txt"] = []byte("save completed without a reply")
	remote.mu.Unlock()
	edit.saving = true
	edit.sending = sha256.Sum256([]byte("save completed without a reply"))
	state.externalEditResult(session, session.files, sftpclient.Result{
		Operation: sftpclient.OperationSaveText,
		Path:      "/home/test/notes.txt",
		Err:       errors.New("connection interrupted before the reply"),
	})
	waitFor(t, state, func() bool { return state.modal == modalForm })
	if got := remoteText(remote, "/home/test/notes.txt"); got != "save completed without a reply" {
		t.Fatalf("an uncertain save silently replaced the server: %q", got)
	}
	local, err := os.ReadFile(edit.local)
	if err != nil || string(local) != "hello world" {
		t.Fatalf("the local version was lost: %q, %v", local, err)
	}
}

func TestKeepingARevertedCopyStillReportsTheServerConflict(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	remote.mu.Lock()
	remote.files["/home/test/notes.txt"] = []byte("changed by another user")
	remote.mu.Unlock()
	if err := os.WriteFile(edit.local, []byte("my change"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return state.modal == modalForm })
	if err := os.WriteFile(edit.local, []byte("hello world"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.Click(hitFormCancel)
	if !externalEditHeldLocally(edit) {
		t.Fatal("the kept local version was mistaken for the changed server version")
	}
	if got := remoteText(remote, "/home/test/notes.txt"); got != "changed by another user" {
		t.Fatalf("keeping a local copy changed the server: %q", got)
	}
}

func TestAnOldExternalEditWatcherCannotStopItsReplacement(t *testing.T) {
	state, session, _, _ := externalEditSetup(t)
	startExternalEditOf(t, state, session, "notes.txt")
	old := state.edits.stop
	state.stopWatchingExternalEdits()
	state.watchExternalEdits()
	current := state.edits.stop
	state.finishExternalEditWatch(old)
	state.stopWatchingExternalEdits()
	select {
	case <-current:
	default:
		t.Fatal("the replacement watcher lost its stop signal")
	}
}

func TestOversizedExternalEditIsReportedAndCanBeCorrected(t *testing.T) {
	state, session, remote, _ := externalEditSetup(t)
	edit := startExternalEditOf(t, state, session, "notes.txt")
	if err := os.WriteFile(edit.local, []byte(strings.Repeat("x", sftpclient.MaxEditFileBytes+1)), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return edit.badFile && session.files.errorText != "" })
	if got := remoteText(remote, "/home/test/notes.txt"); got != "hello world" {
		t.Fatalf("oversized edit changed the server: %q", got)
	}
	if err := os.WriteFile(edit.local, []byte("fixed"), 0o600); err != nil {
		t.Fatal(err)
	}
	waitFor(t, state, func() bool { return remoteText(remote, "/home/test/notes.txt") == "fixed" })
}
