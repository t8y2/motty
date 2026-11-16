//go:build windows

package localshell

import (
	"bytes"
	"encoding/hex"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/sys/windows"
	"motty/internal/term"
)

// resetBundledConPTY forgets the loaded DLL so a test can change where it looks.
func resetBundledConPTY() {
	bundledOnce = sync.Once{}
	bundled = nil
}

// conptyTree points a test at the repository's shipped conpty tree.
func conptyTree(t *testing.T) string {
	t.Helper()
	arch := map[string]string{"amd64": "x64", "arm64": "arm64"}[runtime.GOARCH]
	if arch == "" {
		t.Skip("no bundled ConPTY for this architecture")
	}
	root, err := filepath.Abs("conpty")
	if err != nil {
		t.Fatal(err)
	}
	if _, err := os.Stat(filepath.Join(root, arch, "conpty.dll")); err != nil {
		t.Skipf("the conpty tree is not beside the test: %v", err)
	}
	previous := conptyDirectoryOverride
	conptyDirectoryOverride = root
	resetBundledConPTY()
	t.Cleanup(func() {
		conptyDirectoryOverride = previous
		resetBundledConPTY()
	})
	return root
}

// TestBundledConPTYProbeChild is the program the parent test runs inside the
// pseudo console. It asks for the default colors, then writes a synchronized
// update and waits for the terminal's replies.
func TestBundledConPTYProbeChild(t *testing.T) {
	if os.Getenv("MOTTY_CONPTY_PROBE_CHILD") != "1" {
		t.Skip("conpty probe child only")
	}
	input := windows.Handle(os.Stdin.Fd())
	if err := windows.SetConsoleMode(input, windows.ENABLE_VIRTUAL_TERMINAL_INPUT|windows.ENABLE_EXTENDED_FLAGS); err != nil {
		fmt.Printf("MOTTY-CONPTY-CHILD-ERROR:set console mode: %v\n", err)
		os.Exit(1)
	}
	sequence := "\x1b]10;?\x1b\\\x1b]11;?\x1b\\\x1b[?2026$p" +
		"\x1b[?2026hmotty-conpty-sync-marker\r\n\x1b[?2026l"
	if _, err := os.Stdout.WriteString(sequence); err != nil {
		fmt.Printf("MOTTY-CONPTY-CHILD-ERROR:write: %v\n", err)
		os.Exit(1)
	}

	replies := make(chan []byte)
	go func() {
		buffer := make([]byte, 256)
		for {
			n, err := os.Stdin.Read(buffer)
			if n > 0 {
				replies <- append([]byte(nil), buffer[:n]...)
			}
			if err != nil {
				close(replies)
				return
			}
		}
	}()
	var received []byte
	deadline := time.After(8 * time.Second)
probe:
	for !(bytes.Contains(received, []byte("\x1b]10;")) && bytes.Contains(received, []byte("\x1b]11;"))) {
		select {
		case chunk, ok := <-replies:
			if !ok {
				break probe
			}
			received = append(received, chunk...)
		case <-deadline:
			break probe
		}
	}
	fmt.Printf("MOTTY-CONPTY-REPLIES:%s\n", hex.EncodeToString(received))
	// The test framework's process is not the application; ending it here keeps
	// its leak checker out of the parent's measurement.
	os.Exit(0)
}

func TestBundledConPTYDeliversColorQueriesAndKeepsSyncBlocksAtomic(t *testing.T) {
	conptyTree(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell, wakes := startWindows(t, Config{Cols: 100, Rows: 30, Env: []string{"MOTTY_CONPTY_PROBE_CHILD=1"}})
	screen := term.New(100, 30, 0)
	screen.SetDefaultColors([3]uint8{0xaa, 0xbb, 0xcc}, [3]uint8{1, 2, 3})
	replies := 0
	screen.SetReply(func(reply []byte) {
		replies++
		shell.Write(append([]byte(nil), reply...))
	})

	command := windows.ComposeCommandLine([]string{executable, "-test.run=^TestBundledConPTYProbeChild$"})
	if !shell.Write([]byte(command + "\r")) {
		t.Fatal("the shell refused the probe command")
	}

	var raw []byte
	deadline := time.After(20 * time.Second)
	wait := func(done func(string) bool) string {
		t.Helper()
		for {
			if chunk := shell.Drain(nil); len(chunk) > 0 {
				raw = append(raw, chunk...)
				screen.Write(chunk)
			}
			if done(string(raw)) {
				return string(raw)
			}
			select {
			case <-wakes:
			case <-deadline:
				t.Fatalf("the probe did not report back; output so far %q", raw)
			}
		}
	}
	stream := wait(func(stream string) bool { return strings.Contains(stream, "MOTTY-CONPTY-REPLIES:") })
	// Let any paint the shell queued after the child exit arrive, so the sync
	// position is measured on the whole response, not a prefix.
	time.Sleep(300 * time.Millisecond)
	if chunk := shell.Drain(nil); len(chunk) > 0 {
		raw = append(raw, chunk...)
		screen.Write(chunk)
	}
	stream = string(raw)

	marker := "MOTTY-CONPTY-REPLIES:"
	start := strings.Index(stream, marker)
	if start < 0 {
		t.Fatalf("the child's reply line is missing: %q", stream)
	}
	end := strings.IndexAny(stream[start+len(marker):], "\r\n")
	if end < 0 {
		t.Fatalf("the child's reply line is missing: %q", stream)
	}
	encoded := stream[start+len(marker) : start+len(marker)+end]
	received, err := hex.DecodeString(encoded)
	if err != nil {
		t.Fatalf("the child's reply line is not hex (%q): %v", encoded, err)
	}
	for _, want := range []string{"\x1b]10;rgb:aaaa/bbbb/cccc\x1b\\", "\x1b]11;rgb:0101/0202/0303\x1b\\"} {
		if !bytes.Contains(received, []byte(want)) {
			t.Errorf("the child did not receive %q; got %q", want, received)
		}
	}
	if replies < 2 {
		t.Errorf("the terminal answered %d queries", replies)
	}

	begin := strings.Index(stream, "\x1b[?2026h")
	finish := strings.Index(stream, "\x1b[?2026l")
	block := strings.Index(stream, "motty-conpty-sync-marker")
	if begin < 0 || finish < 0 || block < 0 {
		t.Fatalf("the synchronized block is incomplete: begin=%d finish=%d marker=%d", begin, finish, block)
	}
	if block < begin || block > finish {
		t.Errorf("the block's content reached the terminal outside DECSET 2026: begin=%d marker=%d finish=%d", begin, block, finish)
	}
	if again := strings.Index(stream[finish:], "motty-conpty-sync-marker"); again >= 0 {
		t.Errorf("the content was painted again after the synchronized update ended")
	}
}

func TestBundledConPTYIsUnavailableWithoutItsTree(t *testing.T) {
	previous := conptyDirectoryOverride
	conptyDirectoryOverride = t.TempDir()
	resetBundledConPTY()
	t.Cleanup(func() {
		conptyDirectoryOverride = previous
		resetBundledConPTY()
	})
	if bundledConPTY() != nil {
		t.Fatal("a missing conpty tree was treated as the bundled backend")
	}
}

// TestBundledConPTYProbeMouseChild asks for mouse reporting the way a
// full-screen program does; the parent test reads the request back.
func TestBundledConPTYProbeMouseChild(t *testing.T) {
	if os.Getenv("MOTTY_CONPTY_MOUSE_CHILD") != "1" {
		t.Skip("conpty mouse probe child only")
	}
	if _, err := os.Stdout.WriteString("\x1b[?1000h\x1b[?1002h\x1b[?1006hMOTTY-CONPTY-MOUSE\r\n"); err != nil {
		fmt.Printf("MOTTY-CONPTY-CHILD-ERROR:write: %v\n", err)
		os.Exit(1)
	}
	os.Exit(0)
}

// TestBundledConPTYForwardsMouseReporting keeps a program's mouse modes reaching
// the terminal: the inbox ConPTY consumes them, and a full-screen program's
// wheel then falls back to arrow keys (issue #425). The bundled one forwards them.
func TestBundledConPTYForwardsMouseReporting(t *testing.T) {
	conptyTree(t)
	executable, err := os.Executable()
	if err != nil {
		t.Fatal(err)
	}
	shell, wakes := startWindows(t, Config{Cols: 100, Rows: 30, Env: []string{"MOTTY_CONPTY_MOUSE_CHILD=1"}})
	screen := term.New(100, 30, 0)

	command := windows.ComposeCommandLine([]string{executable, "-test.run=^TestBundledConPTYProbeMouseChild$"})
	if !shell.Write([]byte(command + "\r")) {
		t.Fatal("the shell refused the probe command")
	}

	var raw []byte
	deadline := time.After(20 * time.Second)
	for !bytes.Contains(raw, []byte("MOTTY-CONPTY-MOUSE")) {
		if chunk := shell.Drain(nil); len(chunk) > 0 {
			raw = append(raw, chunk...)
			screen.Write(chunk)
		}
		select {
		case <-wakes:
		case <-deadline:
			t.Fatalf("the probe did not report back; output so far %q", raw)
		}
	}
	for _, sequence := range []string{"\x1b[?1000h", "\x1b[?1002h", "\x1b[?1006h"} {
		if !bytes.Contains(raw, []byte(sequence)) {
			t.Errorf("ConPTY swallowed %q", sequence)
		}
	}
	if modes := screen.Modes(); modes.Mouse != term.MouseDrag || modes.MouseEncoding != term.MouseEncodingSGR {
		t.Errorf("the terminal learned mouse=%v encoding=%v", modes.Mouse, modes.MouseEncoding)
	}
}
