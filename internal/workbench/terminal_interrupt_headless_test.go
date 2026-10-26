//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"net"
	"os"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
)

// Exercise OnShortcut before OnKey, as native windows do. Calling Key alone
// misses the copy shortcut that used to swallow Ctrl+C without a selection.
func TestTerminalInterruptPassesTheCopyShortcut(t *testing.T) {
	for _, kitty := range []bool{false, true} {
		t.Run(fmt.Sprintf("kitty=%v", kitty), func(t *testing.T) {
			state, _, _, shell := liveSession(t)
			// Also exercise the Windows/Linux copy binding when run on macOS.
			state.shortcuts.chords[shortcutCopy] = shortcutChord{Modifiers: ui.ModifierControl, Key: 'c'}
			if kitty {
				shell.say("\x1b[=1u")
				state.Wake()
			}
			session, err := headless.Open(app.Config{
				Title: "terminal interrupt", Width: 1400, Height: 900,
				Paint: state.Paint, TextInput: state.TerminalInput,
				OnPointer: state.Pointer, OnKey: state.Key,
				OnShortcut: state.Shortcut, OnResize: state.Resize,
			}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)); err != nil {
				t.Fatal(err)
			}
			shell.input = nil
			if err := session.Keys("ctrl+c"); err != nil {
				t.Fatal(err)
			}
			want := "\x03"
			if kitty {
				want = "\x1b[99;5u"
			}
			if got := string(shell.input); got != want {
				t.Fatalf("Ctrl+C = %q, want %q", got, want)
			}
			// Copying selected terminal output must still stay local.
			state.sessions[0].term.selection.selected = true
			state.sessions[0].term.selection.anchor = term.Position{}
			state.sessions[0].term.selection.focus = term.Position{Column: 1}
			shell.input = nil
			if err := session.Keys("ctrl+c"); err != nil {
				t.Fatal(err)
			}
			if len(shell.input) != 0 {
				t.Fatalf("copy reached shell: %q", shell.input)
			}
		})
	}
}

// Opt in with comma-separated host:port endpoints and a test identity. This
// uses the product's SSH/PTTY transport and shortcut routing against real sshd.
func TestTerminalInterruptSSH(t *testing.T) {
	endpoints := os.Getenv("MOTTY_TEST_SSH_ENDPOINTS")
	if endpoints == "" {
		t.Skip("set MOTTY_TEST_SSH_ENDPOINTS to run against test sshd")
	}
	for _, endpoint := range strings.Split(endpoints, ",") {
		t.Run(endpoint, func(t *testing.T) {
			host, portText, err := net.SplitHostPort(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				t.Fatal(err)
			}
			wake := make(chan struct{}, 1)
			notify := func() {
				select {
				case wake <- struct{}{}:
				default:
				}
			}
			conn := sshconn.Dial(sshconn.Config{Host: host, Port: uint16(port), User: os.Getenv("MOTTY_TEST_SSH_USER"), IdentityFiles: []string{os.Getenv("MOTTY_TEST_SSH_KEY")}, KnownHostsPath: os.Getenv("MOTTY_TEST_SSH_KNOWN_HOSTS"), DialTimeout: 5 * time.Second, Notify: notify})
			defer conn.Close()
			deadline := time.After(15 * time.Second)
			for conn.Status().Phase != sshconn.PhaseConnected {
				select {
				case <-wake:
				case <-deadline:
					t.Fatalf("SSH did not connect: %+v", conn.Status())
				}
			}
			remote, err := conn.OpenShell(sshconn.ShellConfig{Cols: 80, Rows: 24, Notify: notify})
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			until := func(want string) {
				t.Helper()
				var output []byte
				deadline := time.After(10 * time.Second)
				for {
					output = remote.Drain(output)
					if strings.Contains(string(output), want) {
						return
					}
					select {
					case <-wake:
					case <-deadline:
						t.Fatalf("waiting for %q, got %q", want, output)
					}
				}
			}
			state, _, _, _ := liveSession(t)
			state.sessions[0].term.shell = remote
			state.shortcuts.chords[shortcutCopy] = shortcutChord{Modifiers: ui.ModifierControl, Key: 'c'}
			session, err := headless.Open(app.Config{Title: "SSH interrupt", Width: 1400, Height: 900, Paint: state.Paint, TextInput: state.TerminalInput, OnPointer: state.Pointer, OnKey: state.Key, OnShortcut: state.Shortcut, OnResize: state.Resize}, nil, headless.Options{})
			if err != nil {
				t.Fatal(err)
			}
			defer session.Close()
			if err := session.ClickLabel(fmt.Sprintf(state.messages.LabelTerminalFmt, state.sessions[0].Address)); err != nil {
				t.Fatal(err)
			}
			remote.Write([]byte("stty -echo; printf '\\nINTERRUPT-%s\\n' READY; tail -f /dev/null\r"))
			until("INTERRUPT-READY")
			if err := session.Keys("ctrl+c"); err != nil {
				t.Fatal(err)
			}
			remote.Write([]byte("printf '\\nINTERRUPT-%s\\n' PASSED\r"))
			until("INTERRUPT-PASSED")
		})
	}
}
