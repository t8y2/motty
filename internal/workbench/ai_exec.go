package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"time"

	"motty/internal/ai/capture"
	"motty/internal/ai/gate"
	"motty/internal/ai/tools"
	"motty/internal/shellkind"
)

// aiExec is one command the assistant typed into a visible terminal and is
// waiting to see finish. A terminal has at most one.
type aiExec struct {
	t    *terminal
	tap  *capture.Tap
	done chan gate.ExecResult // buffered; receives exactly once
}

// maxExecOutput bounds the output kept for the model, whatever the terminal shows.
const maxExecOutput = 64 << 10

var (
	errNoSession  = errors.New("no such session; call list_sessions")
	errNoShell    = errors.New("the session has no open shell yet")
	errExecBusy   = errors.New("the terminal is already running another assistant command")
	errExecRefuse = errors.New("the terminal did not accept input")
)

// aiSession finds the session a tool call names: "" is the active one.
func (state *State) aiSession(handle string) (*Session, error) {
	if handle == "" {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
			return session, nil
		}
		for index := range state.sessions {
			if t := state.sessions[index].term; t != nil && t.shell != nil {
				return &state.sessions[index], nil
			}
		}
		return nil, errNoSession
	}
	id, err := strconv.ParseUint(handle, 10, 32)
	if err != nil {
		return nil, errNoSession
	}
	session, ok := state.sessionByIDPointer(uint32(id))
	if !ok {
		return nil, errNoSession
	}
	return session, nil
}

// shellKinder is a Shell that knows which family of shell it runs. Others, such
// as a test double, are treated as POSIX, which is how every shell was typed to
// before the family was known.
type shellKinder interface{ Kind() shellkind.Kind }

func (t *terminal) shellKind() shellkind.Kind {
	if k, ok := t.shell.(shellKinder); ok {
		return k.Kind()
	}
	return shellkind.Unknown
}

// beginExec types the command into the session's terminal. Window thread.
func (state *State) beginExec(args tools.TerminalExecArgs) (*aiExec, error) {
	session, err := state.aiSession(args.Session)
	if err != nil {
		return nil, err
	}
	t := session.term
	if t == nil || t.shell == nil || !t.live {
		return nil, errNoShell
	}
	if t.exec != nil {
		return nil, errExecBusy
	}
	kind := t.shellKind()
	nonce := capture.NewNonce()
	wrapped, err := capture.WrapFor(kind, args.Command, nonce)
	if err != nil {
		return nil, err
	}
	tap := capture.NewTap(nonce, maxExecOutput)
	tap.SetEcho(wrapped)
	// PowerShell's line editor redraws what is typed and asks the terminal where
	// its cursor is, and it waits for the answer. Holding that output back to swap
	// the wrapper for the plain command would withhold the question, so the user
	// sees what the shell echoes.
	if kind != shellkind.PowerShell {
		tap.SetDisplay(capture.DisplayCommandFor(kind, args.Command))
	}
	t.exec = &aiExec{t: t, tap: tap, done: make(chan gate.ExecResult, 1)}
	if t.aiRows == nil {
		t.aiRows = map[uint64]struct{}{}
	}
	// The prompt row is where the assistant's command will be shown, so mark it
	// before the shell echoes anything. A line ID survives scrolling.
	line := t.screen.Row(t.screen.Cursor().Y).ID
	t.aiStartID = line
	t.aiRows[line] = struct{}{}
	if !t.sendAssistant(wrapped) {
		delete(t.aiRows, line)
		t.exec = nil
		return nil, errExecRefuse
	}
	return t.exec, nil
}

// markAIBlock gives the assistant's command and its output rows the AI gutter
// mark. While the command runs, a line that is still being written is included;
// the final write ends before the shell prompt, which belongs to the next user
// action. Scrolling back leaves the visible rows untouched.
func (t *terminal) markAIBlock(final bool) {
	if t.aiRows == nil || t.aiStartID == 0 || t.screen.ViewOffset() > 0 {
		return
	}
	_, rows := t.screen.Size()
	cursor := t.screen.Cursor()
	end := cursor.Y
	if final && end > 0 {
		// The prompt is written before this method runs, in the same chunk or a
		// later one. Never leave its row marked as the assistant's command; the
		// next thing typed there is the user's.
		delete(t.aiRows, t.screen.Row(end).ID)
		end--
	}
	start := 0
	for row := 0; row < rows; row++ {
		if t.screen.Row(row).ID == t.aiStartID {
			start = row
			break
		}
	}
	for row := start; row <= end && row < rows; row++ {
		t.aiRows[t.screen.Row(row).ID] = struct{}{}
	}
}

// feedExec passes shell output through the running command's tap and returns
// what the terminal should show. When the command ended, it is finished.
func (t *terminal) feedExec(output []byte) []byte {
	display, done := t.exec.tap.Feed(output)
	if done {
		t.finishExec(false, false)
	}
	return display
}

// finishExec ends the running command: normally when its marker arrived, or
// because the wait timed out, was canceled, or the user pressed Ctrl-C. The
// command may still be running on the remote side then, so an interrupt is sent.
func (t *terminal) finishExec(timedOut, interrupt bool) {
	exec := t.exec
	if exec == nil {
		return
	}
	t.exec = nil
	if rest := exec.tap.Abort(); len(rest) > 0 {
		t.writeRemote(rest, t.recordingNow())
	}
	result := exec.tap.Result()
	if interrupt && t.shell != nil {
		t.sendProtocol([]byte{3})
	}
	exec.done <- gate.ExecResult{Output: result.Output, ExitCode: result.ExitCode, Truncated: result.Truncated, TimedOut: timedOut}
}

// userInterrupts reports whether keyboard input contains Ctrl-C, which ends the
// assistant's wait: the user is taking the terminal back.
func userInterrupts(p []byte) bool { return bytes.IndexByte(p, 3) >= 0 }

// aiExecWait waits for the command, giving up at the deadline or when cancel
// closes; it then interrupts the command, which stays visible in the terminal.
func (state *State) aiExecWait(exec *aiExec, timeout time.Duration, cancel <-chan struct{}) gate.ExecResult {
	timer := time.NewTimer(timeout)
	defer timer.Stop()
	abort := func() gate.ExecResult {
		finished := make(chan gate.ExecResult, 1)
		if !state.aiPost(func(*State) {
			if exec.t.exec == exec {
				exec.t.finishExec(true, true)
			}
			select {
			case r := <-exec.done:
				finished <- r
			default:
				finished <- gate.ExecResult{TimedOut: true}
			}
		}) {
			return gate.ExecResult{TimedOut: true}
		}
		select {
		case r := <-finished:
			return r
		case <-time.After(5 * time.Second):
			return gate.ExecResult{TimedOut: true}
		}
	}
	select {
	case r := <-exec.done:
		return r
	case <-timer.C:
		return abort()
	case <-cancel:
		return abort()
	}
}

func execTimeout(seconds int) time.Duration {
	if seconds <= 0 {
		seconds = tools.DefaultTimeoutSeconds
	}
	return time.Duration(seconds) * time.Second
}

func sessionHandle(id uint32) string { return fmt.Sprint(id) }
