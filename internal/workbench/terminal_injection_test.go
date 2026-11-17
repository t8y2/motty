package workbench

import (
	"strings"
	"testing"

	"motty/internal/sshconn"
	"motty/internal/term"
)

// A settling injection hides the shell's output until the ready marker, keeps
// what was typed meanwhile in order, and then releases both. Window-thread
// state only; no connection is involved.
func TestInjectionHidesItsEchoAndReleasesQueuedInput(t *testing.T) {
	const token = "inject-token"
	screen := term.New(80, 24, 0)
	t0 := &terminal{screen: screen, folderToken: token}
	shell := &fakeShell{}
	t0.shell = shell
	session := &Session{term: t0}
	state := &State{}
	marker := sshconn.IntegrationReadyMarker(token)
	t0.injection = terminalInjection{phase: injectionSuppressing, marker: marker}

	if !t0.writeShellInput([]byte("ls\r")) {
		t.Fatal("input typed during the injection was refused")
	}
	if len(shell.input) != 0 {
		t.Fatalf("input reached the shell while the injection was settling: %q", shell.input)
	}
	if rest, applied := state.applyInjection(session, []byte("echo of the script\r\n")); applied || len(rest) != 0 {
		t.Fatalf("output before the marker was released: %q (applied %v)", rest, applied)
	}
	rest, applied := state.applyInjection(session, append([]byte("more\r\n"), append(marker, []byte("prompt$ ")...)...))
	if !applied || string(rest) != "prompt$ " {
		t.Fatalf("marker released %q (applied %v), want only what follows it", rest, applied)
	}
	if got := string(shell.input); got != "ls\r" {
		t.Fatalf("queued input = %q, want it written after the marker", got)
	}
	if t0.injection.phase != injectionDone {
		t.Fatalf("phase = %v, want done", t0.injection.phase)
	}
	if text := screen.Text(); strings.Contains(text, "echo of the script") {
		t.Fatalf("the script's echo reached the screen: %q", text)
	}
}

// A hidden window that never sees its marker must not keep the shell from
// taking input: it fails open and the shell runs without integration.
func TestInjectionFailsOpenWhenTheMarkerNeverComes(t *testing.T) {
	screen := term.New(80, 24, 0)
	t0 := &terminal{screen: screen, folderToken: "token", integrated: true}
	shell := &fakeShell{}
	t0.shell = shell
	session := &Session{term: t0}
	state := &State{}
	t0.injection = terminalInjection{
		phase:  injectionSuppressing,
		marker: sshconn.IntegrationReadyMarker("token"),
	}
	if !t0.writeShellInput([]byte("typed\r")) {
		t.Fatal("input typed during the injection was refused")
	}
	state.applyInjection(session, []byte(strings.Repeat("x", injectionBufferLimit+1)))
	if t0.injection.phase != injectionFailed {
		t.Fatalf("phase = %v, want failed", t0.injection.phase)
	}
	if t0.integrated {
		t.Fatal("a failed injection still claims integration")
	}
	if got := string(shell.input); got != "typed\r" {
		t.Fatalf("queued input = %q, want it released on failure", got)
	}
}

// The terminal never erases the first prompt line on its own: the shell's next
// prompt clears the line in the same write that draws it. A ready marker that
// arrives alone must leave the screen as it was, and the shell's prefix must
// replace the first prompt exactly once.
func TestTheShellReplacesTheFirstPromptLine(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	t0 := session.term
	const token = "blink-token"
	t0.folderToken = token
	marker := sshconn.IntegrationReadyMarker(token)

	shell.say("user@host:~$ ")
	state.Wake()
	t0.injection = terminalInjection{phase: injectionSuppressing, marker: marker}

	shell.say(string(marker))
	state.Wake()
	if count := strings.Count(screenText(state, 0), "user@host:~$"); count != 1 {
		t.Fatalf("the marker changed the screen (prompt shown %d times):\n%s", count, screenText(state, 0))
	}

	// The one-shot prefix the integration puts before that prompt.
	shell.say("\r\x1b[2Kuser@host:~$ ")
	state.Wake()
	if count := strings.Count(screenText(state, 0), "user@host:~$"); count != 1 {
		t.Fatalf("the redrawn prompt shows %d times, want exactly one:\n%s", count, screenText(state, 0))
	}
}
