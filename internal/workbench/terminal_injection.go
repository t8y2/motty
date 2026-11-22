package workbench

import (
	"bytes"
	"context"
	"time"

	"github.com/vivaui/viva/gfx"

	"motty/internal/shellkind"
	"motty/internal/sshconn"
)

// Shell integration over SSH is installed after the shell starts, not by
// starting the shell through a wrapper: OpenSSH prints the connection's login
// message (the MOTD, the last login) only to a session that is not a command,
// and only on the first session of the connection. See ADR 0025.
//
// The terminal therefore types a short, history-guarded script into the shell
// once it is quiet, hides everything the shell writes until the script's ready
// marker, and then shows the redrawn prompt. Input typed during that window is
// queued in order and released afterwards. A shell whose type cannot be
// learned, or whose script never reports, runs on without integration exactly
// as an unsupported shell always has.
const (
	// injectionQuiet is how long the shell must have been quiet before the
	// script is typed: the MOTD, the startup files and the first prompt have all
	// arrived by then, so hiding the script cannot hide any of them.
	injectionQuiet = 400 * time.Millisecond
	// injectionGrace gives a shell that has said nothing a chance to start
	// before the script is typed anyway.
	injectionGrace = 4 * time.Second
	// injectionTimeout bounds the hidden window. A shell that never prints the
	// marker (a busy startup, a replaced PROMPT_COMMAND) is released and runs
	// unintegrated.
	injectionTimeout = 8 * time.Second
	// injectionBufferLimit bounds what the hidden window keeps. The script's
	// echo is a few kilobytes; a host that floods the terminal while the marker
	// is outstanding is not allowed to grow this.
	injectionBufferLimit = 64 << 10
)

type injectionPhase uint8

const (
	injectionIdle injectionPhase = iota
	// injectionWaiting: the shell is open; its type is being probed and its
	// startup output is settling. Input is held.
	injectionWaiting
	// injectionSuppressing: the script was typed; its echo is being dropped
	// until the ready marker. Input is held.
	injectionSuppressing
	injectionDone
	injectionFailed
)

// shellProbe is what ProbeShell learned about the running shell, and the
// activation line its integration file was written for.
type shellProbe struct {
	path string
	kind shellkind.Kind
	line string
	err  error
}

// terminalInjection is one shell's integration installation.
type terminalInjection struct {
	phase injectionPhase
	// result is the off-thread shell probe, buffered so a result for a terminal
	// that was reset meanwhile can be dropped without blocking the prober.
	result chan shellProbe
	probe  shellProbe
	probed bool

	// token binds the ready marker, the command-start reports and the
	// activation line's echo tag to this shell.
	token string
	// directory is the folder the shell starts in, for a line that is built
	// when it is typed.
	directory string
	marker    []byte
	// echoTag identifies the activation line in the shell's output; it outlives
	// a failed window, because the line may be echoed long after the deadline.
	echoTag []byte
	// lateEcho marks a window reopened by a recognized echo, so its own failure
	// gives up instead of looking for an echo that will not come again.
	lateEcho bool
	buf      []byte
	queued   [][]byte

	since      time.Duration // when the shell opened
	lastOutput time.Duration // when it last wrote while this was waiting
	outputSeen bool
	started    time.Duration // when the script was typed
}

// holding reports whether input must wait for the injection to settle.
func (injection *terminalInjection) holding() bool {
	return injection.phase == injectionWaiting || injection.phase == injectionSuppressing
}

// queue keeps input in order until the shell can take it.
func (injection *terminalInjection) queue(p []byte) {
	injection.queued = append(injection.queued, append([]byte(nil), p...))
}

// startInjection probes the shell that just opened and prepares to install its
// integration. Window thread.
func (state *State) startInjection(session *Session, shell Shell) {
	t := session.term
	if t == nil || !t.integrated || session.isLocal() || session.host.mosh() {
		return
	}
	prober, ok := session.conn.(interface {
		ProbeShell(context.Context) (string, shellkind.Kind)
		UploadIntegration(context.Context, string, string, string) (string, error)
	})
	if !ok {
		return
	}
	if _, ok := shell.(*sshconn.Shell); !ok {
		return
	}
	token, directory := t.folderToken, session.startDirectory
	result := make(chan shellProbe, 1)
	t.injection = terminalInjection{phase: injectionWaiting, result: result, token: token, directory: directory, since: state.now()}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		path, kind := prober.ProbeShell(ctx)
		probe := shellProbe{path: path, kind: kind}
		probe.line, probe.err = activationLine(kind, path, token, directory, -1, func() (string, error) {
			return prober.UploadIntegration(ctx, path, token, directory)
		})
		result <- probe
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}()
}

// activationLine is the line that installs integration in a shell of this kind.
// A PowerShell takes the whole script in the line itself, since a script file
// would be subject to the host's execution policy; a Bash or Zsh reads a file
// that upload writes to the server. Neither is available for any other shell,
// which runs on without integration.
func activationLine(kind shellkind.Kind, path, token, directory string, row int, upload func() (string, error)) (string, error) {
	switch {
	case kind == shellkind.PowerShell:
		return sshconn.PowerShellActivation(token, directory, row), nil
	case path != "":
		return upload()
	}
	return "", nil
}

// advanceInjection moves one shell's injection forward on a Wake. Window thread.
func (state *State) advanceInjection(session *Session, now time.Duration) {
	t := session.term
	if t == nil {
		return
	}
	injection := &t.injection
	if injection.result != nil {
		select {
		case probe := <-injection.result:
			injection.result = nil
			injection.probe, injection.probed = probe, true
		default:
		}
	}
	switch injection.phase {
	case injectionWaiting:
		if !injection.probed {
			return
		}
		if injection.outputSeen {
			if now-injection.lastOutput < injectionQuiet {
				return
			}
		} else if now-injection.since < injectionGrace {
			return
		}
		state.beginInjection(session)
	case injectionSuppressing:
		if now-injection.started >= injectionTimeout {
			state.failInjection(session)
		}
	}
}

// beginInjection types the script into the running shell. Window thread.
func (state *State) beginInjection(session *Session) {
	t := session.term
	injection := &t.injection
	if injection.probe.line == "" || injection.probe.err != nil || t.shell == nil {
		state.failInjection(session)
		return
	}
	// A full-screen program owns the terminal (a multiplexer the login files
	// start, say): the line would be typed into that program, not the shell,
	// and its ready marker would never come back through it. Run unintegrated,
	// as for any shell that cannot be reached, instead of holding input for the
	// whole window.
	if t.screen.Modes().AltScreen {
		state.failInjection(session)
		return
	}
	if injection.probe.kind == shellkind.PowerShell {
		// The line is built now, not when the shell was probed: it names the
		// screen row the prompt is on, which the startup output has moved.
		injection.probe.line = sshconn.PowerShellActivation(injection.token, injection.directory, t.screen.Cursor().Y)
	}
	injection.marker = sshconn.IntegrationReadyMarker(injection.token)
	injection.echoTag = sshconn.IntegrationEchoTag(injection.token)
	injection.phase = injectionSuppressing
	injection.started = state.now()
	injection.buf = injection.buf[:0]
	if setter, ok := t.shell.(interface{ SetKind(shellkind.Kind) }); ok {
		setter.SetKind(injection.probe.kind)
	}
	// Typed directly: this is not the user's input and must not be recorded,
	// predicted or queued behind itself. The activation line is short enough
	// for the pty's canonical input buffer.
	t.shell.Write([]byte(injection.probe.line))
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

// applyInjection hides the shell's output until the ready marker arrives and
// returns what follows it. Window thread.
func (state *State) applyInjection(session *Session, data []byte) (rest []byte, applied bool) {
	t := session.term
	injection := &t.injection
	injection.buf = append(injection.buf, data...)
	if index := bytes.Index(injection.buf, injection.marker); index >= 0 {
		// Bash reports command-start support while the activation output is
		// still hidden. Retain that capability so the first prompt can own input.
		commandStart := sshconn.IntegrationCommandStartMarker(injection.token)
		if bytes.Contains(injection.buf[:index], commandStart) {
			t.commandReporting = true
		}
		rest = append([]byte(nil), injection.buf[index+len(injection.marker):]...)
		if injection.probe.kind == shellkind.PowerShell {
			// The console behind the pty draws the next prompt relative to
			// where it believes the cursor is: the start of the prompt's row,
			// since the script put it there. The screen never saw the line's
			// echo, so its own cursor is still at the end of the old prompt.
			// Moving it to that start and clearing the row makes the two agree.
			rest = append([]byte("\r\x1b[2K"), rest...)
		}
		injection.buf = injection.buf[:0]
		injection.phase = injectionDone
		// The echo arrived, however late: the shell is integrated after all.
		injection.echoTag = nil
		injection.lateEcho = false
		if !t.integrated {
			t.integrated = true
		}
		// The hidden echo ended the first prompt line, and the shell's next
		// prompt clears and redraws that line itself (the first-prompt prefix
		// the integration installs). Erasing here would paint a frame with the
		// line empty when the redrawn prompt arrives in a later write.
		t.flushInjectionInput()
		return rest, true
	}
	if len(injection.buf) > injectionBufferLimit {
		state.failInjection(session)
	}
	return nil, false
}

// catchLateEcho recognizes the activation line's echo in a failed window's
// output. A server that received the line after the window closed echoes it
// now; the line's tag, which a shell echoes as ordinary text, identifies it
// wherever the line editor drew it. Finding it reopens the window, because the
// shell is about to execute the line and print its ready marker: everything
// between the tag and the marker belongs to the installation, not to the user.
// A chunk without the tag is the user's output and passes unchanged.
func (state *State) catchLateEcho(session *Session, data []byte) (emit []byte, caught bool) {
	injection := &session.term.injection
	index := bytes.Index(data, injection.echoTag)
	if index < 0 {
		return data, false
	}
	// The echo stays out of the screen: only what preceded it is shown now.
	injection.buf = append(injection.buf[:0], data[index:]...)
	injection.phase = injectionSuppressing
	injection.lateEcho = true
	injection.started = state.now()
	return append([]byte(nil), data[:index]...), true
}

// failInjection releases a hidden window that will not finish. The shell keeps
// running without integration, as an unsupported shell does.
func (state *State) failInjection(session *Session) {
	t := session.term
	injection := &t.injection
	if !injection.holding() && injection.phase != injectionSuppressing {
		return
	}
	injection.phase = injectionFailed
	injection.buf = injection.buf[:0]
	// The first failure keeps looking for the activation line's echo: a link
	// that stalled past the deadline delivers it later. A window reopened by
	// that echo has seen it already, so its failure stops the search.
	if injection.lateEcho {
		injection.echoTag = nil
		injection.lateEcho = false
	}
	t.integrated = false
	t.flushInjectionInput()
}

// flushInjectionInput writes what was typed while the injection was settling.
func (t *terminal) flushInjectionInput() {
	queued := t.injection.queued
	t.injection.queued = nil
	for _, chunk := range queued {
		if t.shell == nil {
			return
		}
		t.writeShellInput(chunk)
	}
}

// requestInjectionFrames asks for the frame that advances a settling injection:
// the quiet deadline, the grace deadline, or the end of the hidden window.
func (state *State) requestInjectionFrames(canvas *gfx.Canvas) {
	now := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		t := session.term
		if t == nil {
			continue
		}
		injection := &t.injection
		switch injection.phase {
		case injectionWaiting:
			if !injection.probed {
				// The probe's own goroutine asks for a frame when it answers;
				// nothing here polls for it.
				continue
			}
			if injection.outputSeen {
				canvas.RequestFrameIn(max(injection.lastOutput+injectionQuiet-now, time.Millisecond))
			} else {
				canvas.RequestFrameIn(max(injection.since+injectionGrace-now, time.Millisecond))
			}
		case injectionSuppressing:
			canvas.RequestFrameIn(max(injection.started+injectionTimeout-now, time.Millisecond))
		}
	}
}
