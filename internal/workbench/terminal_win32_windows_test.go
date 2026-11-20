//go:build windows

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

func TestWin32InputModeSendsTheConsolePacket(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[?9001h")
	state.Wake()
	shell.input = nil

	key(state, 0, gfx.KeyEvent{Win32: gfx.Win32Key{VirtualKey: 'A', ScanCode: 0x1e, Unicode: 'a', Repeat: 2}})
	if got, want := string(shell.input), "\x1b[65;30;97;1;0;2_"; got != want {
		t.Fatalf("win32 packet = %q, want %q", got, want)
	}
	hit := terminalHit(state, 0)
	if input := state.TerminalInput(app.TextInputContext{ID: hit, Terminal: true}); input == nil || !input.KeyReleases {
		t.Fatalf("terminal input does not ask for raw keys: %+v", input)
	}
}

// A local console shell asks for win32 input records through ConPTY, which is
// how Git Bash and Cygwin receive their keys on Windows. That is a transport,
// not a claim on the terminal's own suggestion key: Right at a live suggestion
// still accepts it, the badge still names the key, and the program is not sent
// the release of a press the terminal consumed.
func TestSuggestionRightIsAcceptedWhileWin32InputIsOn(t *testing.T) {
	h := ghostWorld(t, "git checkout main")
	h.prompt("$ ")
	h.type_("git ch")
	h.shell.say("\x1b[?9001h")
	h.state.Wake()
	if !h.state.sessions[0].term.screen.Modes().Win32Input {
		t.Fatal("win32 input mode was not set")
	}
	if rest, ok := h.ghost(); !ok || rest != "eckout main" {
		t.Fatalf("suggestion = %q, %v", rest, ok)
	}
	if _, _, ok := paintGhostGrid(t, h.state, &h.state.sessions[0]); !ok {
		t.Fatal("the accept badge is missing while win32 input is on")
	}
	before := len(h.shell.input)
	press := gfx.KeyEvent{Key: gfx.KeyRight, Win32: gfx.Win32Key{VirtualKey: 39, ScanCode: 0x4d}}
	h.key(press)
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the shell got %q, want the rest of the command and no key packet", got)
	}
	h.key(gfx.KeyEvent{Key: gfx.KeyRight, Release: true, Win32: gfx.Win32Key{VirtualKey: 39, ScanCode: 0x4d}})
	if got := string(h.shell.input[before:]); got != "eckout main" {
		t.Fatalf("the release of the accepted press reached the shell: %q", got)
	}
}

// ConPTY asks a local shell for win32 input records, so a lone modifier and
// every release arrive as packets. They carry key state, not typing: pressing
// Control on the way to Control+Insert must not clear the selection the copy
// is about to take, and letting go of a key must not move the view.
func TestBareModifierInWin32InputModeKeepsTheSelectionForCopy(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[?9001h")
	state.Wake()
	shell.say("alpha beta\r\n")
	state.Wake()
	session := &state.sessions[0]
	session.term.selection.selectRange(term.Point{Line: 0, Column: 2}, term.Point{Line: 0, Column: 6})

	control := gfx.KeyEvent{
		Key: gfx.KeyControlLeft, Modifiers: gfx.ModifierControl,
		Win32: gfx.Win32Key{VirtualKey: 17, ScanCode: 0x1d},
	}
	before := len(shell.input)
	key(state, 0, control)
	release := control
	release.Release = true
	key(state, 0, release)
	if len(shell.input) == before {
		t.Fatal("the shell did not receive the modifier")
	}
	if !session.term.selection.selected {
		t.Fatal("a lone Control press or release cleared the selection")
	}

	insert := gfx.KeyEvent{
		Key: gfx.KeyInsert, Modifiers: gfx.ModifierControl,
		Win32: gfx.Win32Key{VirtualKey: 45, ScanCode: 0x52},
	}
	if copied := key(state, 0, insert).ClipboardText; copied != "pha b" {
		t.Fatalf("Ctrl+Insert copied %q, want %q", copied, "pha b")
	}
}
