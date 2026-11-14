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

	marker []byte
	buf    []byte
	queued [][]byte

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
	t.injection = terminalInjection{phase: injectionWaiting, result: result, since: state.now()}
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 4*time.Second)
		defer cancel()
		path, kind := prober.ProbeShell(ctx)
		probe := shellProbe{path: path, kind: kind}
		if path != "" {
			probe.line, probe.err = prober.UploadIntegration(ctx, path, token, directory)
		}
		result <- probe
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}()
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
	injection.marker = sshconn.IntegrationReadyMarker(t.folderToken)
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
		rest = append([]byte(nil), injection.buf[index+len(injection.marker):]...)
		injection.buf = injection.buf[:0]
		injection.phase = injectionDone
		// The echo that was hidden included the newline that ended the first
		// prompt line. Erase that line so the redrawn prompt takes its place.
		t.writeScreen([]byte("\r\x1b[2K"))
		t.flushInjectionInput()
		return rest, true
	}
	if len(injection.buf) > injectionBufferLimit {
		state.failInjection(session)
	}
	return nil, false
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
