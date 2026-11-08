package rdp

import (
	"context"
	"crypto/rand"
	"crypto/sha256"
	"encoding/hex"
	"io/fs"
	"os"
	"path/filepath"
	"sort"
	"strings"
	"testing"
	"time"

	"motty/internal/desktop"
)

// TestLiveFilesCopiedHereArePastedInExplorer is the end-to-end check of file
// transfer through the clipboard, against a real Windows server. It runs on that
// server (see tools/live/rdp_live.sh), so the files it copies are read by the
// remote Explorer from the very machine that pastes them, and the test can
// compare what arrived:
//
//	MOTTY_RDP_PASTE_DIR  an empty folder the signed-in account may write to
//
// It signs in, copies a tree of files (a multi-chunk file, a folder with a
// nested one, a name outside ASCII), opens that folder in Explorer, pastes, and
// compares every file by SHA-256 and the folders by name.
func TestLiveFilesCopiedHereArePastedInExplorer(t *testing.T) {
	config := liveEnv(t)
	dest := os.Getenv("MOTTY_RDP_PASTE_DIR")
	if dest == "" {
		t.Skip("MOTTY_RDP_PASTE_DIR not set")
	}
	opts := desktop.Options{
		Username: config.user, Domain: config.domain, Password: config.password, Clipboard: true,
		VerifyCertificate: func(string, []byte) error { return nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session, err := Open(ctx, direct, config.address, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer session.Close()

	view := &screen{}
	save := func(name string) {
		if config.png != "" {
			view.savePNG(t, config.png+"-"+name+".png")
		}
	}
	if !pump(t, session, view, 40*time.Second, func() bool { return view.colors(16) >= 4 }) {
		t.Fatalf("no picture: %d colours", view.colors(16))
	}
	liveAnswerFirstPrompt(t, session, view, config, save)
	if !pump(t, session, view, 120*time.Second, func() bool { return view.colors(400) >= 400 }) {
		t.Fatalf("no desktop: %d colours", view.colors(400))
	}
	pump(t, session, view, 5*time.Second, func() bool { return false })
	width, height := session.Size()
	save("files-desktop")

	source := t.TempDir()
	liveWriteFile(t, filepath.Join(source, "hello 中文.txt"), 1234)
	liveWriteFile(t, filepath.Join(source, "big.bin"), 3<<20+17)
	liveWriteFile(t, filepath.Join(source, "folder", "inner.txt"), 77)
	liveWriteFile(t, filepath.Join(source, "folder", "deeper", "x.bin"), 200<<10)
	want := digests(t, source)
	offered := []string{
		filepath.Join(source, "hello 中文.txt"), filepath.Join(source, "big.bin"), filepath.Join(source, "folder"),
	}
	offer, err := session.SetClipboardFiles(offered)
	if err != nil {
		t.Fatalf("offer: %v (%+v)", err, offer)
	}
	t.Logf("offered %d files and %d folders, %d bytes", offer.Files, offer.Folders, offer.Bytes)
	if offer.Files != 4 || offer.Folders != 2 || offer.Skipped != 0 {
		t.Fatalf("offer = %+v", offer)
	}
	pump(t, session, view, 2*time.Second, func() bool { return false })

	// Win+R, the folder, Enter: an Explorer window on the destination.
	liveChord(t, session, desktop.KeyMeta, desktop.KeyEvent{Rune: 'r'})
	pump(t, session, view, 3*time.Second, func() bool { return false })
	liveType(t, session, "explorer "+dest)
	pump(t, session, view, time.Second, func() bool { return false })
	save("files-run")
	pressNamed(t, session, "enter")
	pump(t, session, view, 8*time.Second, func() bool { return false })
	// Maximised, with the pointer in the empty folder view, so the keyboard is in
	// the list and Ctrl+V pastes there and not in the address bar.
	liveChord(t, session, desktop.KeyMeta, desktop.KeyEvent{Key: desktop.KeyUp})
	pump(t, session, view, 2*time.Second, func() bool { return false })
	for _, buttons := range []desktop.Buttons{0, desktop.ButtonLeft, 0} {
		if err := session.SendPointer(width/2, height/2, buttons); err != nil {
			t.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	save("files-explorer")
	liveChord(t, session, desktop.KeyControl, desktop.KeyEvent{Rune: 'v'})

	var got map[string]string
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		pump(t, session, view, 2*time.Second, func() bool { return false })
		got = digests(t, dest)
		if len(got) >= len(want) {
			break
		}
	}
	save("files-pasted")
	if len(got) != len(want) {
		t.Fatalf("pasted %d entries %v, want %d %v", len(got), names(got), len(want), names(want))
	}
	for name, sum := range want {
		if got[name] != sum {
			t.Errorf("%s: pasted %q, want %q", name, got[name], sum)
		}
	}
	t.Logf("pasted %d entries, all identical", len(got))
}

// digests maps every entry under root, by its slash-separated relative path, to
// the SHA-256 of a file or "dir" for a folder.
func digests(t *testing.T, root string) map[string]string {
	t.Helper()
	result := map[string]string{}
	err := filepath.WalkDir(root, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || path == root {
			return nil
		}
		rel, _ := filepath.Rel(root, path)
		rel = filepath.ToSlash(rel)
		if entry.IsDir() {
			result[rel] = "dir"
			return nil
		}
		data, err := os.ReadFile(path)
		if err != nil {
			result[rel] = "unreadable: " + err.Error()
			return nil
		}
		sum := sha256.Sum256(data)
		result[rel] = hex.EncodeToString(sum[:])
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	return result
}

func names(m map[string]string) string {
	list := make([]string, 0, len(m))
	for name := range m {
		list = append(list, name)
	}
	sort.Strings(list)
	return strings.Join(list, ", ")
}

// liveWriteFile writes size random bytes at path, making its folder.
func liveWriteFile(t *testing.T, path string, size int) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	data := make([]byte, size)
	_, _ = rand.Read(data)
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
}

// liveSendKey sends one event and leaves the remote time to see it.
func liveSendKey(t *testing.T, session *Session, event desktop.KeyEvent) {
	t.Helper()
	if err := session.SendKey(event); err != nil {
		t.Fatal(err)
	}
	time.Sleep(25 * time.Millisecond)
}

// liveChord presses modifier and key together and releases both.
func liveChord(t *testing.T, session *Session, modifier desktop.Key, key desktop.KeyEvent) {
	t.Helper()
	liveSendKey(t, session, desktop.KeyEvent{Key: modifier, Down: true})
	key.Down = true
	liveSendKey(t, session, key)
	key.Down = false
	liveSendKey(t, session, key)
	liveSendKey(t, session, desktop.KeyEvent{Key: modifier, Down: false})
}

// liveType types text as key events.
func liveType(t *testing.T, session *Session, text string) {
	t.Helper()
	for _, r := range text {
		liveSendKey(t, session, desktop.KeyEvent{Rune: r, Down: true})
		liveSendKey(t, session, desktop.KeyEvent{Rune: r, Down: false})
	}
}

// TestLiveFilesCopiedOnTheServerArriveHere is the end-to-end check of the other
// direction: files copied on the remote system are read by this computer into a
// directory of its own and their bytes are compared by SHA-256. It runs on the
// Windows test machine, like the paste test above. PowerShell's Set-Clipboard
// puts the same CF_HDROP file list on the session clipboard that Explorer's
// Copy does; the test types it into the remote session, so the clipboard
// travels through the real RDP clipboard channel.
func TestLiveFilesCopiedOnTheServerArriveHere(t *testing.T) {
	config := liveEnv(t)
	opts := desktop.Options{
		Username: config.user, Domain: config.domain, Password: config.password, Clipboard: true,
		VerifyCertificate: func(string, []byte) error { return nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Minute)
	defer cancel()
	session, err := Open(ctx, direct, config.address, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer session.Close()

	view := &screen{}
	save := func(name string) {
		if config.png != "" {
			view.savePNG(t, config.png+"-"+name+".png")
		}
	}
	if !pump(t, session, view, 40*time.Second, func() bool { return view.colors(16) >= 4 }) {
		t.Fatalf("no picture: %d colours", view.colors(16))
	}
	liveAnswerFirstPrompt(t, session, view, config, save)
	if !pump(t, session, view, 120*time.Second, func() bool { return view.colors(400) >= 400 }) {
		t.Fatalf("no desktop: %d colours", view.colors(400))
	}
	pump(t, session, view, 5*time.Second, func() bool { return false })
	save("reverse-desktop")

	// Keep the paths short: the Run dialog accepts at most 259 characters, and
	// the command that sets the clipboard has to name all three top-level entries.
	source := filepath.Join(os.Getenv("USERPROFILE"), "Desktop", "rdp")
	if err := os.RemoveAll(source); err != nil {
		t.Fatal(err)
	}
	defer os.RemoveAll(source)
	liveWriteFile(t, filepath.Join(source, "中.txt"), 1234)
	liveWriteFile(t, filepath.Join(source, "b.bin"), 3<<20+17)
	liveWriteFile(t, filepath.Join(source, "f", "inner.txt"), 77)
	liveWriteFile(t, filepath.Join(source, "f", "deeper", "x.bin"), 200<<10)
	want := digests(t, source)

	// Put the three top-level entries on the remote session's clipboard, as
	// Explorer's Copy does.
	paths := []string{
		filepath.Join(source, "中.txt"), filepath.Join(source, "b.bin"), filepath.Join(source, "f"),
	}
	command := `powershell -c "Set-Clipboard -Path ` + strings.Join(paths, ",") + `"`
	liveChord(t, session, desktop.KeyMeta, desktop.KeyEvent{Rune: 'r'})
	pump(t, session, view, 2*time.Second, func() bool { return false })
	liveType(t, session, command)
	pump(t, session, view, time.Second, func() bool { return false })
	save("reverse-command")
	pressNamed(t, session, "enter")

	var received desktop.ReceivedFiles
	deadline := time.Now().Add(90 * time.Second)
	for time.Now().Before(deadline) {
		pump(t, session, view, 2*time.Second, func() bool { return false })
		if files, ok := session.RemoteFiles(); ok {
			received = files
			break
		}
	}
	if received.Paths == nil && received.Err == nil {
		save("reverse-timeout")
		if text, ok := session.RemoteClipboard(); ok {
			t.Logf("remote clipboard text: %q", text)
		}
		t.Logf("session ended: %v", session.Err())
		t.Fatal("the remote files never arrived")
	}
	if received.Err != nil {
		t.Fatalf("receive: %v", received.Err)
	}
	root := filepath.Dir(received.Paths[0])
	for _, path := range received.Paths {
		if filepath.Dir(path) != root {
			t.Fatalf("top-level entries in different directories: %q", received.Paths)
		}
	}
	defer os.RemoveAll(root)
	if received.Files != 4 || received.Folders != 2 || received.Skipped != 0 {
		t.Fatalf("received %+v", received)
	}
	got := digests(t, root)
	if len(got) != len(want) {
		t.Fatalf("received %d entries %v, want %d %v", len(got), names(got), len(want), names(want))
	}
	for name, sum := range want {
		if got[name] != sum {
			t.Errorf("%s: received %q, want %q", name, got[name], sum)
		}
	}
	t.Logf("received %d entries, all identical", len(got))
}

// TestLiveTextCopiedOnTheServerArrivesHere checks the server-to-this-computer
// direction of the clipboard with text first: text is the simplest format, so a
// failure here says the channel or its format list is wrong rather than file
// transfer. The text is typed into the Run dialog, selected and copied without
// closing the dialog.
func TestLiveTextCopiedOnTheServerArrivesHere(t *testing.T) {
	config := liveEnv(t)
	opts := desktop.Options{
		Username: config.user, Domain: config.domain, Password: config.password, Clipboard: true,
		VerifyCertificate: func(string, []byte) error { return nil },
	}
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	session, err := Open(ctx, direct, config.address, opts)
	if err != nil {
		t.Fatalf("open: %v", err)
	}
	defer session.Close()

	view := &screen{}
	if !pump(t, session, view, 40*time.Second, func() bool { return view.colors(16) >= 4 }) {
		t.Fatalf("no picture: %d colours", view.colors(16))
	}
	liveAnswerFirstPrompt(t, session, view, config, nil)
	if !pump(t, session, view, 120*time.Second, func() bool { return view.colors(400) >= 400 }) {
		t.Fatalf("no desktop: %d colours", view.colors(400))
	}
	pump(t, session, view, 3*time.Second, func() bool { return false })

	want := "motty-clipboard-text"
	liveChord(t, session, desktop.KeyMeta, desktop.KeyEvent{Rune: 'r'})
	pump(t, session, view, 2*time.Second, func() bool { return false })
	liveType(t, session, want)
	pump(t, session, view, time.Second, func() bool { return false })
	liveChord(t, session, desktop.KeyControl, desktop.KeyEvent{Rune: 'a'})
	// The clipboard as it was before the copy is not this test's answer; drop
	// whatever update arrived while the session was starting.
	for {
		if _, ok := session.RemoteClipboard(); !ok {
			break
		}
	}
	liveChord(t, session, desktop.KeyControl, desktop.KeyEvent{Rune: 'c'})

	var got string
	deadline := time.Now().Add(20 * time.Second)
	for time.Now().Before(deadline) {
		pump(t, session, view, time.Second, func() bool { return false })
		if text, ok := session.RemoteClipboard(); ok && text == want {
			got = text
			break
		}
	}
	pressNamed(t, session, "esc")
	if got != want {
		t.Fatalf("remote clipboard text = %q, want %q", got, want)
	}
}
