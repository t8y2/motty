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

func TestInjectionRevealsOnlyTheSettledPrompt(t *testing.T) {
	const token = "inject-token"
	screen := term.New(80, 24, 0)
	t0 := &terminal{screen: screen, folderToken: token}
	session := &Session{term: t0}
	state := &State{}
	t0.injection = terminalInjection{phase: injectionWaiting}

	state.applyOutput(session, []byte("Last login: today\r\nprompt$ "), false)
	if t0.live {
		t.Fatal("the prompt that injection will redraw was revealed")
	}
	if text := screen.Text(); !strings.Contains(text, "Last login: today") || !strings.Contains(text, "prompt$") {
		t.Fatalf("startup output was not parsed while hidden: %q", text)
	}

	t0.injection.phase = injectionSuppressing
	t0.injection.marker = sshconn.IntegrationReadyMarker(token)
	state.applyOutput(session, []byte("echo of the script\r\n"), false)
	if t0.live {
		t.Fatal("the activation echo revealed the terminal")
	}
	output := append(t0.injection.marker, []byte("prompt$ ")...)
	state.applyOutput(session, output, false)
	if !t0.live {
		t.Fatal("the settled prompt was not revealed")
	}
	if text := screen.Text(); strings.Count(text, "prompt$") != 1 || strings.Contains(text, "echo of the script") {
		t.Fatalf("settled screen = %q, want one prompt and no activation echo", text)
	}
}

func TestInjectionFailureRevealsStartupOutput(t *testing.T) {
	screen := term.New(80, 24, 0)
	t0 := &terminal{screen: screen, integrated: true}
	session := &Session{term: t0}
	state := &State{}
	t0.injection = terminalInjection{phase: injectionWaiting}

	state.applyOutput(session, []byte("prompt$ "), false)
	if t0.live {
		t.Fatal("the unsettled prompt was revealed")
	}
	state.failInjection(session)
	if !t0.live {
		t.Fatal("a failed injection hid the usable shell")
	}
	if text := screen.Text(); !strings.Contains(text, "prompt$") {
		t.Fatalf("startup output was lost on failure: %q", text)
	}
}
