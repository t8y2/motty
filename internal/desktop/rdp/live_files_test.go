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
	if !pump(t, session, view, 120*time.Second, func() bool { return view.colors(400) >= 400 }) {
		t.Fatalf("no desktop: %d colours", view.colors(400))
	}
	pump(t, session, view, 5*time.Second, func() bool { return false })
	width, height := session.Size()
	save := func(name string) {
		if config.png != "" {
			view.savePNG(t, config.png+"-"+name+".png")
		}
	}
	save("files-desktop")

	source := t.TempDir()
	write := func(rel string, size int) {
		path := filepath.Join(source, filepath.FromSlash(rel))
		if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
			t.Fatal(err)
		}
		data := make([]byte, size)
		_, _ = rand.Read(data)
		if err := os.WriteFile(path, data, 0o600); err != nil {
			t.Fatal(err)
		}
	}
	write("hello 中文.txt", 1234)
	write("big.bin", 3<<20+17)
	write("folder/inner.txt", 77)
	write("folder/deeper/x.bin", 200<<10)
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

	send := func(event desktop.KeyEvent) {
		t.Helper()
		if err := session.SendKey(event); err != nil {
			t.Fatal(err)
		}
		time.Sleep(25 * time.Millisecond)
	}
	chord := func(modifier desktop.Key, key desktop.KeyEvent) {
		send(desktop.KeyEvent{Key: modifier, Down: true})
		key.Down = true
		send(key)
		key.Down = false
		send(key)
		send(desktop.KeyEvent{Key: modifier, Down: false})
	}
	// Win+R, the folder, Enter: an Explorer window on the destination.
	chord(desktop.KeyMeta, desktop.KeyEvent{Rune: 'r'})
	pump(t, session, view, 3*time.Second, func() bool { return false })
	for _, r := range "explorer " + dest {
		send(desktop.KeyEvent{Rune: r, Down: true})
		send(desktop.KeyEvent{Rune: r, Down: false})
	}
	pump(t, session, view, time.Second, func() bool { return false })
	save("files-run")
	pressNamed(t, session, "enter")
	pump(t, session, view, 8*time.Second, func() bool { return false })
	// Maximised, with the pointer in the empty folder view, so the keyboard is in
	// the list and Ctrl+V pastes there and not in the address bar.
	chord(desktop.KeyMeta, desktop.KeyEvent{Key: desktop.KeyUp})
	pump(t, session, view, 2*time.Second, func() bool { return false })
	for _, buttons := range []desktop.Buttons{0, desktop.ButtonLeft, 0} {
		if err := session.SendPointer(width/2, height/2, buttons); err != nil {
			t.Fatal(err)
		}
		time.Sleep(150 * time.Millisecond)
	}
	save("files-explorer")
	chord(desktop.KeyControl, desktop.KeyEvent{Rune: 'v'})

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
