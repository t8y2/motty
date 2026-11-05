package workbench

import (
	"errors"
	"os"
	"path"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/sftpclient"
)

const pastedImageHome = "/home/test"

var pastedPNG = []byte("\x89PNG\r\n\x1a\nnot really a picture")

func pasteImageRemote() *workbenchSFTPRemote {
	return &workbenchSFTPRemote{
		cwd:     pastedImageHome,
		entries: map[string][]os.FileInfo{pastedImageHome: {}},
		files:   map[string][]byte{},
	}
}

// pasteInto presses Paste in the only terminal and returns what the shell was
// typed, once something other than the old input arrived or the wait ran out.
func pasteInto(t *testing.T, state *State, shell *fakeShell, wait bool) string {
	t.Helper()
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.pasteIntoTerminal(session)
	typed := func() string {
		shell.mu.Lock()
		defer shell.mu.Unlock()
		return string(shell.input)
	}
	deadline := time.Now().Add(5 * time.Second)
	for wait && time.Now().Before(deadline) && typed() == "" {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	state.Wake()
	return typed()
}

func TestPastingAnImageUploadsItAndTypesItsPath(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	remote := pasteImageRemote()
	fake.mu.Lock()
	fake.sftp = remote
	fake.mu.Unlock()
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return pastedPNG, nil }

	typed := pasteInto(t, state, shell, true)
	directory := path.Join(pastedImageHome, pasteImageDir)
	if !strings.HasPrefix(typed, directory+"/paste-") || !strings.HasSuffix(typed, ".png ") {
		t.Fatalf("typed %q, want a path under %s and a space; status %q", typed, directory, state.status)
	}
	name := strings.TrimSuffix(typed, " ")
	remote.mu.Lock()
	content := remote.files[name]
	remote.mu.Unlock()
	if string(content) != string(pastedPNG) {
		t.Fatalf("uploaded %q to %s", content, name)
	}
	if !remote.closed {
		t.Fatal("the file client of the upload was left open")
	}
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if session.pastingImage || !strings.Contains(state.status, name) {
		t.Fatalf("pasting=%v status %q", session.pastingImage, state.status)
	}
}

func TestPastingTextNeverTouchesTheImage(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	fake.mu.Lock()
	fake.sftp = pasteImageRemote()
	fake.mu.Unlock()
	state.options.ClipboardText = func() (string, error) { return "echo hi", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) {
		t.Error("the clipboard image was read although it holds text")
		return pastedPNG, nil
	}
	if typed := pasteInto(t, state, shell, true); typed != "echo hi" {
		t.Fatalf("typed %q", typed)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.sftpOpens != 0 {
		t.Fatal("a text paste opened a file client")
	}
}

func TestPastingAnImageIsOffWhenTheSettingSaysSo(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	fake.mu.Lock()
	fake.sftp = pasteImageRemote()
	fake.mu.Unlock()
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) {
		t.Error("the clipboard image was read although pasting images is off")
		return pastedPNG, nil
	}
	state.Click(hitSettingsPasteImageToggle)
	if !state.pasteImageOff {
		t.Fatal("the switch did not turn pasting images off")
	}
	if typed := pasteInto(t, state, shell, false); typed != "" {
		t.Fatalf("typed %q", typed)
	}
}

func TestPastingAnImageThatFailsToUploadSaysWhy(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	fake.mu.Lock()
	fake.sftpErr = errors.New("permission denied")
	fake.mu.Unlock()
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return pastedPNG, nil }

	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.pasteIntoTerminal(session)
	deadline := time.Now().Add(5 * time.Second)
	for session.pastingImage && time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if !strings.Contains(state.status, "permission denied") {
		t.Fatalf("status %q", state.status)
	}
	shell.mu.Lock()
	defer shell.mu.Unlock()
	if len(shell.input) != 0 {
		t.Fatalf("typed %q after a failed upload", shell.input)
	}
}

func TestPastingAnImageThatIsTooLargeIsRefusedBeforeUploading(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return nil, app.ErrTooLarge }
	if typed := pasteInto(t, state, shell, false); typed != "" {
		t.Fatalf("typed %q", typed)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.sftpOpens != 0 || state.status != state.messages.Tools.PasteImageTooLarge {
		t.Fatalf("opens %d, status %q", fake.sftpOpens, state.status)
	}
}

func TestAnEmptyClipboardPastesNothing(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return nil, app.ErrNoImage }
	if typed := pasteInto(t, state, shell, false); typed != "" {
		t.Fatalf("typed %q", typed)
	}
	fake.mu.Lock()
	defer fake.mu.Unlock()
	if fake.sftpOpens != 0 {
		t.Fatal("an empty clipboard opened a file client")
	}
}

func TestPastingAnImageRemovesOnlyOldPastedImages(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	directory := path.Join(pastedImageHome, pasteImageDir)
	week := pasteImageKeep + time.Hour
	old := time.Now().Add(-week)
	fresh := time.Now().Add(-time.Hour)
	remote := pasteImageRemote()
	remote.entries[directory] = []os.FileInfo{
		workbenchFileInfo{name: "paste-old.png", size: 1, mode: 0o600, when: old},
		workbenchFileInfo{name: "paste-new.png", size: 1, mode: 0o600, when: fresh},
		workbenchFileInfo{name: "notes.txt", size: 1, mode: 0o600, when: old},
	}
	for _, name := range []string{"paste-old.png", "paste-new.png", "notes.txt"} {
		remote.files[path.Join(directory, name)] = []byte("x")
	}
	for _, parent := range []string{path.Join(pastedImageHome, ".cache"), path.Join(pastedImageHome, ".cache/motty")} {
		remote.entries[parent] = []os.FileInfo{}
	}
	fake.mu.Lock()
	fake.sftp = remote
	fake.mu.Unlock()
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return pastedPNG, nil }
	pasteInto(t, state, shell, true)

	remote.mu.Lock()
	defer remote.mu.Unlock()
	if _, kept := remote.files[path.Join(directory, "paste-old.png")]; kept {
		t.Fatal("a week-old pasted image was kept")
	}
	for _, name := range []string{"paste-new.png", "notes.txt"} {
		if _, kept := remote.files[path.Join(directory, name)]; !kept {
			t.Fatalf("%s was removed", name)
		}
	}
}

// A server with no SFTP gets the picture over SCP.
func TestPastingAnImageFallsBackToSCP(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	server := &scpServer{files: map[string]string{}}
	fake.mu.Lock()
	fake.sftpErr = errors.New("sftp: start subsystem: ssh: subsystem request failed")
	fake.scp = sftpclient.NewSCP(server.start)
	fake.mu.Unlock()
	state.options.ClipboardText = func() (string, error) { return "", nil }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return pastedPNG, nil }

	typed := pasteInto(t, state, shell, true)
	name := strings.TrimSuffix(typed, " ")
	server.mu.Lock()
	defer server.mu.Unlock()
	if !strings.HasPrefix(name, path.Join(pastedImageHome, pasteImageDir)+"/paste-") || server.files[name] != string(pastedPNG) {
		t.Fatalf("typed %q; server has %v", typed, server.files)
	}
}

func TestAPastedPathIsOneWordToAShell(t *testing.T) {
	if got, want := shellEscapePath("/home/a b/.cache/x(1).png"), `/home/a\ b/.cache/x\(1\).png`; got != want {
		t.Fatalf("escaped %q, want %q", got, want)
	}
	if got := shellEscapePath("/home/用户/a-b_c.png"); got != "/home/用户/a-b_c.png" {
		t.Fatalf("escaped %q", got)
	}
}

// OpenCode reads a clipboard picture itself, and only when a paste arrives with
// nothing in it: the terminal must send that paste when the clipboard has no text
// and Motty has no use for the picture (a local shell).
func TestAnImageOnlyClipboardIsAnEmptyPasteForPicturesTheProgramReadsItself(t *testing.T) {
	for name, text := range map[string]func() (string, error){
		"empty":  func() (string, error) { return "", nil },
		"errors": func() (string, error) { return "", errors.New("no text format") },
	} {
		t.Run(name, func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			state.pasteImageOff = true // the picture is not Motty's to upload
			state.options.ClipboardText = text
			state.options.ClipboardImagePNG = func() ([]byte, error) { return pastedPNG, nil }
			session, _ := state.sessionByIDPointer(state.activeSessionID)

			state.pasteIntoTerminal(session)
			if got := shell.typed(); got != "" {
				t.Fatalf("typed %q without bracketed paste: an empty paste is no event then", got)
			}
			shell.say("\x1b[?2004h")
			state.Wake()
			state.pasteIntoTerminal(session)
			if got := shell.typed(); got != "\x1b[200~\x1b[201~" {
				t.Fatalf("typed %q, want an empty bracketed paste", got)
			}
		})
	}
}

func TestAClipboardWithNeitherTextNorPictureSaysWhyOnAReadError(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[?2004h")
	state.Wake()
	state.options.ClipboardText = func() (string, error) { return "", errors.New("clipboard busy") }
	state.options.ClipboardImagePNG = func() ([]byte, error) { return nil, app.ErrNoImage }
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	state.pasteIntoTerminal(session)
	if got := shell.typed(); got != "" || !strings.Contains(state.status, "clipboard busy") {
		t.Fatalf("typed %q, status %q", got, state.status)
	}
}
