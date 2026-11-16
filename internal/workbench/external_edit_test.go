package workbench

import (
	"errors"
	"fmt"
	"os"
	"runtime"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
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
