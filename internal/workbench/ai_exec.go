package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"strconv"
	"strings"
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
	job  *aiJob               // set for a background command nobody is waiting on
	// A device console has no completion marker. The prompt seen before the
	// command is the signal: once it is drawn again on a later line, the command
	// has returned.
	consolePrompt string
	consoleRow    uint64
}

// execEnd is why a command stopped being the one the assistant waits on.
type execEnd uint8

const (
	endMarker  execEnd = iota // the command finished and printed its marker
	endUser                   // the user pressed Ctrl-C and took the terminal back
	endShell                  // the shell ended
	endTimeout                // the wait ran out or was canceled
	endStop                   // the assistant asked for the command to be stopped
)

// maxExecOutput bounds the output kept for the model, whatever the terminal shows.
const maxExecOutput = 64 << 10

var (
	errNoSession  = errors.New("no such session; call list_sessions")
	errNoShell    = errors.New("the session has no open shell yet")
	errExecBusy   = errors.New("the terminal is already running another assistant command")
	errExecRefuse = errors.New("the terminal did not accept input")
	// errNoConsolePrompt means there is no prompt to wait for. Typing a command
	// then would have no completion signal.
	errNoConsolePrompt = errors.New("the device console is not showing a prompt; read the terminal and wait for its prompt, then try again")
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
	nonce := ""
	consolePrompt := ""
	if kind == shellkind.Console {
		// Read the prompt from the live screen. This is what a person types at,
		// and it is also the only completion signal a device gives.
		t.screen.ScrollToBottom()
		consolePrompt = t.consolePrompt()
		if consolePrompt == "" {
			return nil, errNoConsolePrompt
		}
	} else {
		nonce = capture.NewNonce()
	}
	wrapped, err := capture.WrapFor(kind, args.Command, nonce)
	if err != nil {
		return nil, err
	}
	tap := capture.NewTap(nonce, maxExecOutput)
	if kind == shellkind.Console {
		tap = capture.NewConsoleTap(maxExecOutput)
	}
	switch {
	case args.Background:
		tap.RetainWindow(jobRetainWindow)
	case args.RetainOutput:
		tap.Retain(gate.MaxRetainedOutput)
	}
	tap.SetEcho(wrapped)
	// PowerShell's line editor redraws what is typed and asks the terminal where
	// its cursor is, and it waits for the answer. Holding that output back to swap
	// the wrapper for the plain command would withhold the question, so the user
	// sees what the shell echoes.
	if kind != shellkind.PowerShell && kind != shellkind.Console {
		tap.SetDisplay(capture.DisplayCommandFor(kind, args.Command))
	}
	// The prompt row is where the assistant's command will be shown, so mark it
	// before the shell echoes anything. A line ID survives scrolling.
	line := t.screen.Row(t.screen.Cursor().Y).ID
	t.exec = &aiExec{
		t: t, tap: tap, done: make(chan gate.ExecResult, 1),
		consolePrompt: consolePrompt, consoleRow: line,
	}
	if t.aiRows == nil {
		t.aiRows = map[uint64]struct{}{}
	}
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
		t.finishExec(endMarker)
	}
	return display
}

// consolePrompt returns the text from the start of the cursor's line to the
// cursor, which is the prompt a device console is waiting at.
func (t *terminal) consolePrompt() string {
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if cursor.Y < 0 || cursor.Y >= rows || cursor.X <= 0 {
		return ""
	}
	text, ok := t.screen.AppendCellText(nil, cursor.Y, 0, cursor.X)
	if !ok {
		return ""
	}
	return strings.TrimRight(string(text), " ")
}

// consoleReturnedToPrompt reports whether the running console command has come
// back to the prompt it started from. The start row is excluded: it still shows
// that prompt until the device echoes the command, and accepting it then would
// finish before the command ran.
func (t *terminal) consoleReturnedToPrompt(exec *aiExec) bool {
	cursor := t.screen.Cursor()
	_, rows := t.screen.Size()
	if cursor.Y < 0 || cursor.Y >= rows || t.screen.Row(cursor.Y).ID == exec.consoleRow {
		return false
	}
	return t.screen.RowText(cursor.Y) == exec.consolePrompt
}

// finishConsoleExec ends a device-console command once its prompt is back. It
// has no exit status, so the result says so rather than inventing one.
func (t *terminal) finishConsoleExec() {
	exec := t.exec
	if exec == nil || exec.consolePrompt == "" || !t.consoleReturnedToPrompt(exec) {
		return
	}
	exec.tap.FinishConsole(exec.consolePrompt, -1)
	t.finishExec(endMarker)
}

// finishExec ends the running command: normally when its marker arrived, or
// because the wait timed out, was canceled, or the user pressed Ctrl-C. The
// command may still be running on the remote side then, so an interrupt is sent.
func (t *terminal) finishExec(end execEnd) {
	timedOut, interrupt := end == endTimeout, end == endTimeout || end == endStop
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
	if exec.job != nil {
		exec.job.finish(end, result.ExitCode)
	}
	exec.done <- gate.ExecResult{Output: result.Output, ExitCode: result.ExitCode, Truncated: result.Truncated, TimedOut: timedOut, Retained: result.Retained, RetainLost: result.RetainLost}
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
				exec.t.finishExec(endTimeout)
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
