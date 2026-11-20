package workbench

import (
	"strings"
	"testing"

	"motty/internal/ai/llm"
	"motty/internal/ai/tools"
	"motty/internal/shellkind"
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

// A connection that ends while the injection hides its output must not put
// that output on screen. The flush that keeps the shell's last words runs
// around the normal output path, and no ready marker can end the hidden window
// once the shell is gone, so what is still hidden is dropped there too.
func TestAConnectionLostDuringInjectionDoesNotShowItsEcho(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	session := &state.sessions[0]
	t0 := session.term
	const token = "lost-token"
	t0.folderToken = token
	t0.injection = terminalInjection{phase: injectionSuppressing, marker: sshconn.IntegrationReadyMarker(token)}

	shell.say("_motty_hist_on=; set +o history\r\n. '/tmp/motty-shell.x/init'\r\n")
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if text := screenText(state, 0); strings.Contains(text, "_motty_hist_on") {
		t.Fatalf("the hidden activation echo reached the screen:\n%s", text)
	}
}

// The same flush keeps what the shell said after a ready marker that did
// arrive: that output was never hidden and is the shell's last word.
func TestAConnectionLostAfterTheReadyMarkerKeepsWhatFollowsIt(t *testing.T) {
	state, _, fake, shell := liveSession(t)
	session := &state.sessions[0]
	t0 := session.term
	const token = "kept-token"
	t0.folderToken = token
	t0.injection = terminalInjection{phase: injectionSuppressing, marker: sshconn.IntegrationReadyMarker(token)}

	shell.say("echo of the activation\r\n" + string(sshconn.IntegrationReadyMarker(token)) + "user@host:~$ ")
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	text := screenText(state, 0)
	if strings.Contains(text, "echo of the activation") {
		t.Fatalf("the hidden activation echo reached the screen:\n%s", text)
	}
	if !strings.Contains(text, "user@host:~$") {
		t.Fatalf("the output after the ready marker was lost:\n%s", text)
	}
}

// A connection lost while the assistant's command runs ends that command: the
// next shell's output must not be read as the dead command's capture.
func TestAConnectionLostEndsTheAssistantCommand(t *testing.T) {
	state, shell := assistantTestSession(t, &scriptedModel{turn: func(int, llm.Request) []llm.Event { return textTurn("ok") }})
	session := &state.sessions[0]
	term := session.term
	term.shell = kindShell{shell, shellkind.POSIX}
	if _, err := state.beginExec(tools.TerminalExecArgs{Command: "hostname"}); err != nil {
		t.Fatalf("beginExec: %v", err)
	}
	fake, ok := session.conn.(*fakeConn)
	if !ok {
		t.Fatalf("session connection = %T, want *fakeConn", session.conn)
	}
	fake.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseClosed })
	state.Wake()
	if term.exec != nil {
		t.Fatal("the lost connection left the assistant's command running")
	}
}

// A server that delivers the activation line after the window's deadline
// echoes it later. The tag on the line identifies that echo wherever the line
// editor drew it: the window reopens until its ready marker, so the echo stays
// off the screen while the output around it is shown.
func TestALateEchoReopensTheWindowUntilItsMarker(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	t0 := session.term
	const token = "late-token"
	t0.folderToken = token
	t0.integrated = false
	t0.injection = terminalInjection{
		phase:   injectionFailed,
		token:   token,
		marker:  sshconn.IntegrationReadyMarker(token),
		echoTag: sshconn.IntegrationEchoTag(token),
		started: state.now(),
	}

	shell.say("before$ ")
	shell.say("user@host:~$  : motty-" + token + "; _motty_hist_on=; fi\r\n" +
		string(sshconn.IntegrationReadyMarker(token)) + "user@host:~$ ")
	state.Wake()
	text := screenText(state, 0)
	if !strings.Contains(text, "before$ ") {
		t.Fatalf("output before the echo was hidden:\n%s", text)
	}
	if strings.Contains(text, "_motty_hist_on") || strings.Contains(text, "motty-"+token) {
		t.Fatalf("the late echo reached the screen:\n%s", text)
	}
	if !strings.Contains(text, "user@host:~$ ") {
		t.Fatalf("output after the ready marker was lost:\n%s", text)
	}
	if t0.injection.phase != injectionDone || !t0.integrated {
		t.Fatalf("late echo left phase %v integrated %v", t0.injection.phase, t0.integrated)
	}
}

// The first failure keeps looking for the echo; a window reopened by one that
// then fails must not look again, or it would hide real output forever.
func TestAFailedWindowKeepsLookingForItsEchoOnlyOnce(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	t0 := session.term
	const token = "once-token"
	t0.folderToken = token
	t0.injection = terminalInjection{
		phase:   injectionSuppressing,
		token:   token,
		marker:  sshconn.IntegrationReadyMarker(token),
		echoTag: sshconn.IntegrationEchoTag(token),
		started: state.now(),
	}
	state.advanceInjection(session, state.now()+injectionTimeout)
	if t0.injection.phase != injectionFailed || len(t0.injection.echoTag) == 0 {
		t.Fatalf("the first failure left phase %v tag %q, want a failed window still looking",
			t0.injection.phase, t0.injection.echoTag)
	}

	shell.say("x : motty-" + token + "; still no marker\r\n")
	state.Wake()
	if t0.injection.phase != injectionSuppressing {
		t.Fatalf("the echo did not reopen the window: phase %v", t0.injection.phase)
	}
	state.advanceInjection(session, state.now()+injectionTimeout)
	if t0.injection.phase != injectionFailed || len(t0.injection.echoTag) != 0 {
		t.Fatalf("the reopened window left phase %v tag %q, want it to stop looking",
			t0.injection.phase, t0.injection.echoTag)
	}
	if text := screenText(state, 0); strings.Contains(text, "still no marker") {
		t.Fatalf("the reopened window's output was not released:\n%s", text)
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

// A program's query is answered when it is asked. The answer is not typing:
// held behind the injection window it would reach the shell as keystrokes long
// after the program stopped waiting (a multiplexer's startup queries, printed
// at the prompt).
func TestAnswersToQueriesAreNotHeldByTheInjection(t *testing.T) {
	t0 := &terminal{screen: term.New(80, 24, 0), folderToken: "token"}
	shell := &fakeShell{}
	t0.shell = shell
	t0.injection = terminalInjection{phase: injectionWaiting}

	t0.answerQuery([]byte("\x1b[?62;22c"))
	if got := string(shell.input); got != "\x1b[?62;22c" {
		t.Fatalf("answer written = %q, want it sent while the injection waits", got)
	}
	if len(t0.injection.queued) != 0 {
		t.Fatalf("the answer was queued behind the injection: %q", t0.injection.queued)
	}
	t0.writeShellInput([]byte("ls\r"))
	if len(t0.injection.queued) != 1 {
		t.Fatal("typing during the injection must still wait for it")
	}
}

// A full-screen program owns the terminal, so the line cannot be typed to a
// shell and its marker cannot come back: the shell runs unintegrated at once
// instead of holding input until the window times out.
func TestInjectionIsNotTypedIntoAFullScreenProgram(t *testing.T) {
	screen := term.New(80, 24, 0)
	screen.Write([]byte("\x1b[?1049h"))
	t0 := &terminal{screen: screen, folderToken: "token", integrated: true}
	shell := &fakeShell{}
	t0.shell = shell
	session := &Session{term: t0}
	state := &State{}
	t0.injection = terminalInjection{
		phase:  injectionWaiting,
		probed: true,
		probe:  shellProbe{path: "/bin/bash", kind: shellkind.POSIX, line: " : motty-token\n"},
	}
	t0.injection.queue([]byte("typed\r"))

	state.beginInjection(session)
	if t0.injection.phase != injectionFailed || t0.integrated {
		t.Fatalf("phase = %v integrated = %v, want a failed, unintegrated injection", t0.injection.phase, t0.integrated)
	}
	if got := string(shell.input); got != "typed\r" {
		t.Fatalf("shell received %q, want only the released input and no activation line", got)
	}
}
