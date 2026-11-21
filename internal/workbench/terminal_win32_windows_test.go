//go:build windows

package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
	"motty/internal/completion"
	"motty/internal/localshell"
	"motty/internal/term"
)

func TestWin32GitBashCompletionThroughConPTY(t *testing.T) {
	var bash string
	for _, choice := range localshell.InstalledWindowsShells() {
		if choice.Name == "Git Bash" {
			bash = choice.Path
			break
		}
	}
	if bash == "" {
		t.Skip("Git Bash is not installed")
	}
	t.Setenv("HOME", t.TempDir())
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	state.localShell = bash
	state.Click(hitLocalTerminal)
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil || session.term.shell == nil {
		t.Fatal("Git Bash did not start")
	}
	t.Cleanup(func() { session.term.shell.Close() })
	deadline := time.Now().Add(waitBudget())
	for time.Now().Before(deadline) {
		state.Wake()
		if session.term.screen.Modes().Win32Input && session.term.screen.Cursor().X > 0 {
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !session.term.screen.Modes().Win32Input {
		t.Fatal("Git Bash did not request Win32 input records")
	}
	time.Sleep(100 * time.Millisecond)
	state.Wake()
	state.history.suggestOff = false
	state.history.inlineOff = true
	catalog, err := completion.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	state.completion.catalog = catalog
	for _, character := range "git s" {
		event := gfx.KeyEvent{Text: string(character), Typed: string(character), Win32: gfx.Win32Key{VirtualKey: uint16(unicode.ToUpper(character)), Unicode: uint16(character)}}
		key(state, 0, event)
	}
	localShellOutput(t, state, session, "git s")
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	if !ok || line != "git s" {
		t.Fatalf("Git Bash line = %q, %v", line, ok)
	}
	state.openCompletions()
	if !state.completion.list.active || len(state.completion.list.candidates) == 0 {
		t.Fatal("Git Bash did not open completion for the typed line")
	}
}

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

func TestWin32InputTypingAndCompletion(t *testing.T) {
	h := ghostWorld(t, "git checkout main")
	h.shell.say("\x1b[?9001h")
	h.state.Wake()
	h.prompt("$ ")

	session := &h.state.sessions[0]

	for _, ch := range "git " {
		press := gfx.KeyEvent{
			Key:   gfx.Key(ch),
			Text:  string(ch),
			Win32: gfx.Win32Key{VirtualKey: uint16(ch), Unicode: uint16(ch)},
		}
		key(h.state, 0, press)
		h.shell.say(string(ch))
		h.state.Wake()
	}
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	if !ok || line != "git " {
		t.Fatalf("recorder.Line = %q, %v; want %q, true", line, ok, "git ")
	}
	ghost, ghostOk := h.ghost()
	if !ghostOk || ghost != "checkout main" {
		t.Fatalf("ghost = %q, %v; want %q, true", ghost, ghostOk, "checkout main")
	}
	if !h.state.completionEligible(session) {
		t.Fatal("completionEligible is false after typing under Win32Input")
	}

	h.state.openCompletions()
	if !h.state.completion.list.active {
		t.Fatal("manual completion failed to open under Win32Input")
	}

	enter := gfx.KeyEvent{
		Key:   gfx.KeyEnter,
		Win32: gfx.Win32Key{VirtualKey: 13, ScanCode: 28, Unicode: 13},
	}
	// First Enter accepts the completion candidate
	key(h.state, 0, enter)
	h.state.Wake()
	if h.state.completion.list.active {
		t.Fatal("completion list remained active after accept")
	}

	// Second Enter submits the command to the shell
	key(h.state, 0, enter)
	h.prompt("$ ")

	// Check if the command was remembered in history
	if remembered := h.remembered(""); len(remembered) == 0 || remembered[0] != "git checkout main" {
		t.Fatalf("remembered commands = %v, want git checkout main", remembered)
	}

	// Type second command: "git c"
	for _, ch := range "git c" {
		press := gfx.KeyEvent{
			Key:   gfx.Key(ch),
			Text:  string(ch),
			Win32: gfx.Win32Key{VirtualKey: uint16(ch), Unicode: uint16(ch)},
		}
		key(h.state, 0, press)
		h.shell.say(string(ch))
		h.state.Wake()
	}
	line2, ok2 := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	if !ok2 || line2 != "git c" {
		t.Fatalf("line2 = %q, %v; want %q, true", line2, ok2, "git c")
	}
	ghost2, ghostOk2 := h.ghost()
	if !ghostOk2 || ghost2 != "heckout main" {
		t.Fatalf("ghost2 = %q, %v; want %q, true", ghost2, ghostOk2, "heckout main")
	}

	// Catalog static ghost without history
	h2 := ghostWorld(t) // empty history
	catalog, err := completion.Builtin()
	if err != nil {
		t.Fatal(err)
	}
	h2.state.completion.catalog = catalog
	h2.shell.say("\x1b[?9001h")
	h2.state.Wake()
	h2.prompt("$ ")
	for _, ch := range "git statu" {
		press := gfx.KeyEvent{
			Key:   gfx.Key(ch),
			Text:  string(ch),
			Win32: gfx.Win32Key{VirtualKey: uint16(ch), Unicode: uint16(ch)},
		}
		key(h2.state, 0, press)
		h2.shell.say(string(ch))
		h2.state.Wake()
	}
	ghostStatic, okStatic := h2.ghost()
	if !okStatic || ghostStatic != "s" {
		t.Fatalf("staticGhost with Win32Input on = %q, %v; want %q, true", ghostStatic, okStatic, "s")
	}
}

func TestWin32InputUnintegratedShellAutoListOpens(t *testing.T) {
	w := newPlainCompletionWorld(t)
	w.shell.onWrite = func(p []byte) {
		var vk, sc, uc, down, control, repeat uint16
		logical := p
		if _, err := fmt.Sscanf(string(p), "\x1b[%d;%d;%d;%d;%d;%d_", &vk, &sc, &uc, &down, &control, &repeat); err == nil {
			logical = term.Win32LogicalKey(gfx.KeyEvent{Release: down == 0, Win32: gfx.Win32Key{VirtualKey: vk, ScanCode: sc, Unicode: uc, ControlState: control, Repeat: repeat}})
		}
		w.shell.say(strings.ReplaceAll(string(logical), "\r", "\r\n"))
	}
	w.shell.say("\x1b[?9001h")
	w.state.Wake()
	w.clock += time.Second
	w.state.history.inlineOff = true
	for _, ch := range "git s" {
		press := gfx.KeyEvent{
			Key:   gfx.Key(ch),
			Text:  string(ch),
			Win32: gfx.Win32Key{VirtualKey: uint16(ch), Unicode: uint16(ch)},
		}
		key(w.state, 0, press)
		w.state.Wake()
		w.tick()
	}
	waitCompletionList(t, w)
	if !w.state.completion.list.auto {
		t.Fatal("the win32 input unintegrated shell did not open the automatic list")
	}
	if got := strings.Join(listTexts(w), ","); !strings.Contains(got, "stage") {
		t.Fatalf("list = %q, want git stage", got)
	}
}
