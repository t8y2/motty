package workbench

import (
	"bytes"
	"crypto/rand"
	"errors"
	"fmt"
	"log"
	"runtime"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/autofill"
	"motty/internal/cmdhistory"
	"motty/internal/i18n"
	"motty/internal/sessionlog"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
	"motty/internal/x11fwd"
)

// Shell is what the workbench needs from a remote shell. *sshconn.Shell
// satisfies it; tests stand in for the network with their own.
type Shell interface {
	// Drain appends the output received since the last call to dst.
	Drain(dst []byte) []byte
	// Write queues input for the shell and reports whether it was accepted.
	Write(p []byte) bool
	// Resize tells the remote pty the terminal size in cells.
	Resize(cols, rows int)
	// Done is closed once the shell has ended and all its output is drainable.
	Done() <-chan struct{}
	// Err says why the shell ended; nil for a clean exit.
	Err() error
	Close()
}

// scrollbackLines is how much history each terminal keeps above its screen.
const scrollbackLines = 5000

// announcement tracks local terminal messages so each is written once however
// often the connection reports its status.
type announcement uint8

const (
	toldNothing announcement = iota
	toldBanner
	toldEnded
)

// terminal is one session's screen and the shell behind it. Everything here is
// touched only on the window thread: output arrives through Drain, input leaves
// through Write, and neither blocks.
type terminal struct {
	screen *term.Terminal
	shell  Shell
	rx     []byte // scratch for draining output
	// pumpBudget is the window-thread time a Wake may spend parsing this
	// terminal's output; see pumpBudget.
	pumpBudget time.Duration
	// zmodemTail is the last bytes of output, in case a transfer header is cut
	// between two reads.
	zmodemTail []byte

	told announcement
	// noShellTold records that a keystroke into a tab with no shell has already
	// been explained, so the status line is not rewritten on every key.
	noShellTold           bool
	skippedEndpoint       sshconn.Endpoint
	skippedRole           sshconn.EndpointRole
	skipsTold             int
	live                  bool             // the shell has spoken, or ended: the screen is the page to show
	inputSent             bool             // the user, or an autofill answer, wrote to the shell: it did not end on its own
	ended                 bool             // the shell finished, or never started
	shellOpened           time.Duration    // when the shell was asked for, to give a silent one up
	opening               chan shellResult // a shell being opened in the background, until pump collects it
	followPath            string           // latest shell directory awaiting a safe attached file panel
	reportedHome          string           // home reported by this shell, in the same path form as cwd
	reportedDirectory     term.Cwd         // last report accepted from this shell, rather than a running program
	promptReady           bool
	promptStarted         bool
	promptDirty           bool
	promptVersion         uint64 // invalidates a reverse navigation while its directory is being listed
	folderToken           string
	folderIntegrationSeen bool
	commandReporting      bool
	submittedLines        int
	blink                 cursorBlink
	wheelX                float32             // horizontal wheel travel not yet worth a whole cell
	wheelY                float32             // vertical wheel travel not yet worth a whole cell
	railScroll            float32             // horizontal or vertical tool-rail offset
	railViewport          gfx.Rect            // last painted, scrollable tool viewport
	railBounds            gfx.Rect            // full painted rail, including its gutter
	railContentExtent     float32             // full length of the ordered tool rail
	railIDs               [maxBarTools]gfx.ID // ordered controls for roving keyboard focus
	railEnabled           [maxBarTools]bool   // disabled actions stay visible but leave the keyboard order
	railCount             int
	barDragging           bool          // the scrollbar thumb is held
	barGrab               float32       // where on the thumb it was taken
	barOffset             int           // view offset the scrollbar last saw, to notice the view moving
	barSince              time.Duration // when the scrollbar was last lit
	barLit                bool          // the scrollbar is lit or still lingering

	// label is the accessible name of the terminal, rebuilt only when the
	// language or the address it is made from changes.
	label, labelFormat, labelAddress string

	// preedit is the input-method composition, shown at the cursor.
	preedit    app.TextState
	inputGuard terminalInputGuard
	input      app.TextInput // reused, so resolving the text input allocates nothing
	cache      rowCache
	// rowIDs are the visible rows' line identities, kept between paints so an
	// image can find its row without scanning the whole scrollback.
	rowIDs    []uint64
	selection terminalSelection
	find      terminalFind
	links     terminalLinks
	openLink  func(string)
	clock     func() time.Duration
	recording terminalRecording
	// sync holds the screen as it looked when DECSET 2026 began. While it is
	// set the grid paints the held screen, not the live one, so a program's
	// full redraw appears in one frame.
	sync    *terminalSync
	syncBuf terminalSync
	syncAt  time.Duration
	// login is the command this shell still has to be given after login; see
	// login_command.go. integrated says the shell was started with Motty's
	// prompt reporting, which will announce when it is ready.
	login      loginCommand
	integrated bool
	// prediction draws typed text before the server echoes it; see terminalPrediction.
	prediction terminalPrediction
	// inputLine is set between the end of this shell's prompt and the next
	// command start or control key: the cursor is on the line being typed, which
	// is what lets typed text be shown before its echo (see terminalPrediction).
	inputLine bool
	// autofill is what the autofill rules need to know of this terminal.
	autofill autofill.Watcher
	// passwordHint is the sudo prompt line the saved password is on offer at: set
	// while that line is still under the cursor and nothing was sent since.
	passwordHint string
	// recorder decides which typed lines the command history may keep; it is
	// only fed while historyOn says the history is wanted for this terminal.
	recorder  cmdhistory.Recorder
	historyOn func() bool
	onCommand func(string)
	// takeOutput brings in what the shell has already echoed, so the screen is
	// read after the echo of the keys that came before an Enter. It parses one
	// slice at most: a key is never made to wait for a backlog, whose echo of it
	// would queue behind the backlog anyway, and a frame takes the rest.
	takeOutput func()
	// completionFeed says whether the recorder must follow typing for command
	// completion even when the history itself is not wanted: the built-in
	// catalog is public data and works with learning off.
	completionFeed func() bool
	// completionInput runs after the shell accepted input, so an open list
	// closes on the first typed byte instead of offering a stale line.
	completionInput   func()
	suggestionPending *suggestionEcho

	// exec is the command the assistant is waiting on in this terminal.
	exec *aiExec
	// promptWait is closed when the shell next shows a prompt, for a stop that
	// reports whether the terminal came back.
	promptWait chan struct{}
	// aiRows holds the screen line IDs whose command was started by the
	// assistant. The line ID survives scrolling and resizing, so its gutter mark
	// stays with the command in scrollback.
	aiRows    map[uint64]struct{}
	aiStartID uint64

	mouseReport        [64]byte
	mouseButton        term.MouseEventButton
	mousePosition      term.Position
	mousePositionValid bool
}

// accessibleName is the terminal's name for assistive technology.
func (t *terminal) accessibleName(format, address string) string {
	if t.label == "" || t.labelFormat != format || t.labelAddress != address {
		t.label, t.labelFormat, t.labelAddress = fmt.Sprintf(format, address), format, address
	}
	return t.label
}

// terminalSync is the screen as it looked when a synchronized update began:
// the visible rows, the cursor and which buffer was active. The cells and marks
// are copies, because the live screen changes in place while the block runs.
type terminalSync struct {
	lines  []term.Row
	cursor term.Cursor
	cols   int
	alt    bool
}

// terminalSyncTimeout ends a block whose program never sent ESU, so a crash
// cannot leave the display frozen forever.
const terminalSyncTimeout = time.Second

func newTerminal(cols, rows int) *terminal {
	return newTerminalWithScrollback(cols, rows, scrollbackLines)
}

func newTerminalWithScrollback(cols, rows, scrollback int) *terminal {
	return &terminal{screen: term.New(cols, rows, scrollback), find: newTerminalFind(), pumpBudget: pumpBudget}
}

// holdSync takes or releases the presentation snapshot at a DECSET 2026
// boundary. It runs during Terminal.Write, before the bytes after the boundary
// are parsed, so the snapshot is the screen the program is replacing.
func (t *terminal) holdSync(on bool) {
	if !on {
		t.sync = nil
		return
	}
	if t.sync != nil {
		return
	}
	cols, rows := t.screen.Size()
	held := &t.syncBuf
	held.cols, held.cursor, held.alt = cols, t.screen.Cursor(), t.screen.Modes().AltScreen
	if cap(held.lines) < rows {
		held.lines = make([]term.Row, rows)
	} else {
		held.lines = held.lines[:rows]
	}
	for row := range held.lines {
		line := t.screen.Row(row)
		cells := held.lines[row].Cells[:0]
		line.Cells = append(cells, line.Cells...)
		marks := held.lines[row].Marks[:0]
		line.Marks = append(marks, line.Marks...)
		held.lines[row] = line
	}
	t.sync = held
	t.syncAt = t.recordingNow()
}

// expireSync releases a snapshot whose block outlived the timeout.
func (t *terminal) expireSync() {
	if t.sync == nil || t.clock == nil {
		return
	}
	if t.recordingNow()-t.syncAt >= terminalSyncTimeout {
		t.sync = nil
	}
}

func (t *terminal) presentedSize() (cols, rows int) {
	if t.sync != nil {
		return t.sync.cols, len(t.sync.lines)
	}
	return t.screen.Size()
}

func (t *terminal) presentedCursor() term.Cursor {
	if t.sync != nil {
		return t.sync.cursor
	}
	return t.screen.Cursor()
}

func (t *terminal) presentedAltScreen() bool {
	if t.sync != nil {
		return t.sync.alt
	}
	return t.screen.Modes().AltScreen
}

func (t *terminal) presentedRow(row int) term.Row {
	if t.sync != nil {
		if row < 0 || row >= len(t.sync.lines) {
			return term.Row{}
		}
		return t.sync.lines[row]
	}
	return t.screen.Row(row)
}

// syncRemaining is how long the held frame may still be shown.
func (t *terminal) syncRemaining() time.Duration {
	if t.sync == nil || t.clock == nil {
		return 0
	}
	return max(terminalSyncTimeout-(t.recordingNow()-t.syncAt), 0)
}

func (state *State) configureTerminal(session *Session) {
	session.outputAlert.commandRunning = false
	session.outputAlert.completionKnown = false
	session.outputAlert.promptSeen = false
	if session.term.folderToken == "" {
		session.term.resetFolderSync()
	}
	session.term.screen.SetPromptToken(session.term.folderToken)
	session.term.screen.SetDefaultCursor(state.terminalCursorStyle.termCursorStyle())
	session.term.openLink = state.openTerminalLink
	session.term.clock = state.now
	// An image decoded off the window thread asks for the frame that shows it.
	session.term.screen.SetImageWake(state.options.Invalidate)
	state.attachHistory(session)
	state.attachCompletion(session)
	state.stampTerminal(session.term)
	state.reportTerminalColors(session.term)
	sessionID := session.ID
	session.term.screen.SetSyncHold(func(on bool) {
		if current, ok := state.sessionByIDPointer(sessionID); ok && current.term != nil {
			current.term.holdSync(on)
		}
	})
	session.term.screen.SetClipboardWrite(func(write term.ClipboardWrite) {
		state.remoteClipboardWrite(sessionID, write)
	})
	session.term.screen.SetNotify(func(note term.Notification) {
		state.terminalNotification(sessionID, note)
	})
	session.term.screen.SetBell(func() { state.terminalBell(sessionID) })
	session.term.screen.SetCwdReport(func(cwd term.Cwd) {
		state.recordTerminalDirectory(sessionID, cwd, false)
	})
	session.term.screen.SetHomeDirectory(func(home string) {
		if current, ok := state.sessionByIDPointer(sessionID); ok && current.term != nil && current.containerCommand == "" && current.term.promptStarted {
			if current.isLocal() && !validLocalDirectory(home) || !current.isLocal() && !validSyncDirectory(home) {
				return
			}
			current.term.reportedHome = home
		}
	})
	session.term.screen.SetPromptStart(func() {
		if current, ok := state.sessionByIDPointer(sessionID); ok && current.term != nil {
			current.term.inputGuard.start()
			current.term.promptVersion++
			current.term.folderIntegrationSeen = true
			current.term.promptStarted = true
			current.term.promptReady = false
			current.term.inputLine = false
			current.outputAlert.promptSeen = true
		}
	})
	session.term.screen.SetPromptReady(func() {
		if current, ok := state.sessionByIDPointer(sessionID); ok && current.term != nil {
			t := current.term
			t.promptReady = t.promptStarted && !t.promptDirty && t.commandReporting && t.submittedLines == 0
			t.inputGuard.active = t.promptReady
			if t.promptReady {
				t.releasePromptWait()
			}
			t.inputLine = true
			t.prediction.newPrompt()
		}
	})
	session.term.screen.SetCommandFinished(func(code int) {
		if current, ok := state.sessionByIDPointer(sessionID); ok {
			state.finishBackgroundCommand(current, code)
		}
	})
	session.term.screen.SetCommandStart(func() {
		if current, ok := state.sessionByIDPointer(sessionID); ok && current.term != nil {
			t := current.term
			current.outputAlert.commandRunning = current.outputAlert.promptSeen
			t.commandReporting = true
			t.inputGuard.start()
			t.promptReady, t.promptStarted = false, false
			t.inputLine = false
			t.promptVersion++
			if t.submittedLines > 0 {
				t.submittedLines--
			}
		}
	})
	session.term.screen.SetInputEnvironment(func(environment term.InputEnvironment) {
		state.receiveInputEnvironment(sessionID, environment)
	})
}

func (t *terminal) resetFolderSync() {
	t.inputGuard = terminalInputGuard{}
	t.suggestionPending = nil
	t.folderToken = rand.Text()
	t.screen.SetPromptToken(t.folderToken)
	t.promptReady, t.promptStarted, t.promptDirty = false, false, false
	t.inputLine = false
	t.folderIntegrationSeen, t.commandReporting = false, false
	t.reportedDirectory = term.Cwd{}
	t.reportedHome = ""
	t.submittedLines, t.followPath = 0, ""
	t.promptVersion++
}

// Local messages are drawn with the terminal's own colors, so a connection's
// story looks like the program output that follows it.
const (
	sgrMuted = "\x1b[90m"
	sgrOK    = "\x1b[32m"
	sgrFail  = "\x1b[31m"
	sgrWarn  = "\x1b[33m"
	sgrReset = "\x1b[0m"
)

// say writes one line of local output.
func (t *terminal) say(style, line string) {
	t.write([]byte(style + line + sgrReset + "\r\n"))
}

// clearScreen empties the terminal: the scrollback and every row above the line
// the cursor is on, which becomes the first row. The selection goes with the
// rows it pointed at, and an open find rescans the rows that are left. Nothing
// is sent to the shell, whose prompt line survives, so the line editor keeps
// writing where it expects. The assistant's marks go with the rows it ran in:
// a row that was erased is not the assistant's command when it is written again.
func (t *terminal) clearScreen(messages i18n.Messages) {
	if !t.screen.ClearScreen() {
		return
	}
	t.selection.clear()
	t.find.refreshIfChanged(t.screen, messages, 0)
	kept := t.screen.Row(0).ID
	if t.aiStartID != kept {
		t.aiStartID = 0
	}
	for id := range t.aiRows {
		if id != kept {
			delete(t.aiRows, id)
		}
	}
}

func (t *terminal) write(p []byte) {
	t.selection.clear()
	t.prediction.reset()
	t.screen.Write(p)
}

// writeRemote commits the shell's output. Unlike write it then lets the
// prediction compare the screen with what was typed, so an echo of it confirms
// the overlay instead of dropping it.
func (t *terminal) writeRemote(p []byte, now time.Duration) {
	t.selection.clear()
	t.screen.Write(p)
	t.prediction.reconcile(t.screen, now)
	if t.inputGuard.active && cmdhistory.PasswordPrompt(t.screen.RowText(t.screen.Cursor().Y)) {
		t.inputGuard.start()
	}
}

// send writes input to the shell. Typing returns the view to the live screen.
func (t *terminal) send(p []byte) bool {
	return t.sendInput(p, true)
}

// A confirmed input snapshot has already drained and validated its owner.
// Consuming another transition after that check would invalidate the decision.
func (t *terminal) sendInput(p []byte, drain bool) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	if drain && t.takeOutput != nil {
		t.takeOutput()
		if t.shell == nil {
			return false
		}
	}
	if t.exec != nil && userInterrupts(p) {
		t.finishExec(endUser) // the user took the terminal back; the keys go on to the shell
	}
	t.selection.clear()
	t.screen.ScrollToBottom()
	t.autofill.Typed(p)
	feed := t.historyOn != nil && t.historyOn()
	if !feed && t.completionFeed != nil {
		feed = t.completionFeed()
	}
	if feed {
		if drain && t.takeOutput != nil && bytes.ContainsAny(p, "\r\n") {
			t.takeOutput()
		}
		if command, ok := t.recorder.Typed(p, cmdhistory.TermScreen{Term: t.screen}, t.recordingNow()); ok && t.onCommand != nil {
			t.onCommand(command)
		}
	}
	accepted := t.shell.Write(p)
	if accepted {
		t.inputSent = true
		t.noteShellInput(p)
		if bytes.ContainsAny(p, "\r\n\x03\x04") {
			t.suggestionPending = nil
		}
		t.recording.append(t.recordingNow(), terminalRecordingInput, p)
		if t.completionInput != nil {
			t.completionInput()
		}
	}
	return accepted
}

// sendAssistant types the assistant's capture wrapper into the shell. The
// wrapper is not the user's command: keep it out of command history, terminal
// recording and autofill, and let the capture Tap show the logical command
// instead. The remote shell still sees and can record the wrapper; the shell's
// own history is outside this process's control.
func (t *terminal) sendAssistant(p []byte) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	t.selection.clear()
	t.screen.ScrollToBottom()
	accepted := t.shell.Write(p)
	t.inputSent = t.inputSent || accepted
	if accepted {
		t.noteShellInput(p)
	}
	return accepted
}

// sendSecret writes an autofill answer to the shell. It is not typed input, so
// it is neither recorded nor mistaken for the user's own keys.
func (t *terminal) sendSecret(p []byte) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	accepted := t.shell.Write(p)
	t.inputSent = t.inputSent || accepted
	if accepted {
		t.noteShellInput(p)
	}
	return accepted
}

func (t *terminal) noteShellInput(p []byte) {
	t.passwordHint = ""
	if bytes.ContainsFunc(p, func(r rune) bool { return r < 0x20 || r == 0x7f }) {
		t.inputLine = false // Enter, Tab, an arrow or Ctrl-R: the line is no longer what was typed
	}
	t.inputGuard.observe(p)
	atPrompt := t.promptReady
	t.promptReady, t.promptStarted = false, false
	t.promptVersion++
	// A subsequent prompt must not discard text queued after the last Enter.
	// Cursor movement/deletion is conservative too: only submit/interrupt clears
	// the dirty line; a mere PS1 redraw never authorizes an automatic command.
	for i, b := range p {
		switch b {
		case '\r', '\n':
			if b == '\n' && i > 0 && p[i-1] == '\r' {
				continue
			}
			if t.promptDirty || !atPrompt {
				t.submittedLines++
			}
			t.promptDirty, atPrompt = false, false
		case 3:
			t.promptDirty, t.submittedLines = false, 0
		default:
			t.promptDirty = true
		}
	}
}

func (t *terminal) sendProtocol(p []byte) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	accepted := t.shell.Write(p)
	if accepted {
		t.recording.append(t.recordingNow(), terminalRecordingInput, p)
	}
	return accepted
}

func (t *terminal) recordingNow() time.Duration {
	if t.clock == nil {
		return 0
	}
	return t.clock()
}

// shellResult is the outcome of a shell opened away from the window's thread.
type shellResult struct {
	shell      Shell
	err        error
	cols, rows int // the PTY size requested before opening began
}

// openShell starts the remote shell once the connection is up. Over SSH that
// takes several round trips, so it runs on its own goroutine and pump collects
// the result: the window keeps painting while the server answers.
func (state *State) openShell(session *Session) {
	t := session.term
	if t.shell != nil || t.opening != nil || t.ended || session.conn == nil {
		return
	}
	cols, rows := t.screen.Size()
	config := sshconn.ShellConfig{Cols: cols, Rows: rows, Notify: state.notifyFor(session), Command: session.containerCommand}
	// Local directory restoration needs reports even with file-panel following
	// off. SSH enables integration for either file-panel following or path tabs.
	config.IntegrateShell = session.isLocal() || (session.host.isSSH() && session.host.System != sshconn.SystemWindows &&
		session.containerCommand == "" && (!state.sftpFollowOff || state.sftpTerminalFollow || state.tabTitleMode == tabTitleDirectory))
	config.IntegrationToken = t.folderToken
	if !session.isLocal() {
		config.StartDirectory = session.startDirectory
	}
	t.integrated = config.IntegrateShell
	if session.host.ForwardX11 && session.host.isSSH() {
		environ := x11fwd.CurrentEnviron()
		config.X11 = &environ
	}
	if session.host.AgentForward && session.host.isSSH() {
		config.AgentForward = true
	}
	if opener, ok := session.conn.(slowShellOpener); !ok || !opener.opensShellSlowly() {
		shell, err := session.conn.OpenShell(config)
		state.shellOpened(session, shell, err)
		return
	}
	result := make(chan shellResult, 1)
	t.opening = result
	conn, invalidate := session.conn, state.options.Invalidate
	go func() {
		shell, err := conn.OpenShell(config)
		result <- shellResult{shell: shell, err: err, cols: config.Cols, rows: config.Rows}
		if invalidate != nil {
			invalidate()
		}
	}()
}

// collectShell takes the shell opened in the background, once it is there.
func (state *State) collectShell(session *Session) {
	t := session.term
	if t == nil || t.opening == nil {
		return
	}
	select {
	case result := <-t.opening:
		t.opening = nil
		if result.err == nil {
			// Layout may have changed while OpenShell waited for the server.
			// fitTerminal already resized the local screen without a shell, so
			// its size comparison cannot detect the stale PTY after attachment.
			cols, rows := t.screen.Size()
			if cols != result.cols || rows != result.rows {
				result.shell.Resize(cols, rows)
			}
		}
		state.shellOpened(session, result.shell, result.err)
	default:
	}
}

// releaseShell closes a shell. Closing an SSH shell writes to the network, which
// waits as long as a dead connection does, so that is left to its own goroutine.
func releaseShell(shell Shell) {
	if _, remote := shell.(*sshconn.Shell); remote {
		go shell.Close()
		return
	}
	shell.Close()
}

// abandonShell lets go of a shell still being opened: it is closed when it
// arrives, since nothing is left to take it.
func (t *terminal) abandonShell() {
	if t == nil || t.opening == nil {
		return
	}
	result := t.opening
	t.opening = nil
	go func() {
		if opened := <-result; opened.shell != nil {
			opened.shell.Close()
		}
	}()
}

// shellOpened is what becomes of a session once its shell is, or is not, open.
func (state *State) shellOpened(session *Session, shell Shell, err error) {
	t := session.term
	if err != nil && session.shared && session.containerCommand == "" {
		// A server limits how many shells one connection may carry. Where the
		// borrowed connection is full, connect separately instead of failing.
		t.say(sgrWarn, fmt.Sprintf(state.messages.ConnShareRefusedFmt, err))
		session.avoidShare = true
		state.startConnectionMode(session, session.host, false)
		return
	}
	if err != nil {
		t.ended = true
		t.say(sgrFail, fmt.Sprintf(state.messages.ConnShellFailedFmt, err))
		session.tone = toneFail
		session.err, session.shellFailed = err, true
		session.finished = state.now()
		// The transport is up but no usable shell came out of it: the session
		// never became usable, and the journal says so without the error text.
		state.submitSessionLogEvent(session, sessionlog.EventShellFailed, 0)
		state.finishSessionLog(session, sessionlog.StatusFailed, sessionlog.ReasonShellFailed)
		state.describe(session)
		return
	}
	if session.isLocal() {
		state.rememberRecentConnection(session)
	}
	t.shell = shell
	t.shellOpened = state.now()
	t.noShellTold = false
	// A connection that drops takes the running program's teardown with it, so
	// the modes it had armed on this screen (the alternate screen, mouse
	// reporting, the kitty keyboard flags) are still on when the next shell
	// arrives. That shell is a process which never asked for any of them, so the
	// screen is handed over clean. Unlike the prompt marker this needs no shell
	// integration and no prompt: a session that hands the terminal straight to a
	// full-screen program is covered too.
	t.screen.ReclaimApplicationModes()
	state.submitSessionLogEvent(session, sessionlog.EventShellOpened, 0)
	session.logStatus = sessionlog.StatusActive
	forwardNotes := 0
	if reporter, ok := shell.(interface{ X11Err() error }); ok && session.host.ForwardX11 {
		if err := reporter.X11Err(); err != nil {
			line := fmt.Sprintf(state.messages.Tools.X11UnavailableFmt, state.x11Reason(err, runtime.GOOS))
			t.say(sgrWarn, line)
			forwardNotes++
			if forwardNotes == 1 {
				state.status = line
			}
		}
	}
	if noter, ok := shell.(interface{ AgentForwardNote() string }); ok && session.host.AgentForward {
		if note := noter.AgentForwardNote(); note != "" {
			line := fmt.Sprintf(state.messages.Tools.AgentForwardUnavailableFmt, note)
			t.say(sgrWarn, line)
			forwardNotes++
			if forwardNotes == 1 {
				state.status = line
			}
		}
	}
	t.screen.SetReply(func(reply []byte) {
		t.sendProtocol(reply)
	})
	if session.host.isSSH() && session.containerCommand == "" && session.host.LoginCommand != "" {
		t.login.arm(session.host.LoginCommand)
	}
	state.describe(session)
}

func (session *Session) shellPending() bool {
	return session.phase == sshconn.PhaseConnected && !session.host.isDesktop() &&
		session.term != nil && !session.term.live && !session.term.ended
}

// goLive marks that a shell has spoken, so the terminal replaces the
// connection page for good (until the tab connects again).
func (state *State) goLive(session *Session) {
	if t := session.term; !t.live {
		t.live = true
		session.finished = state.now()
	}
}

// A Wake parses output for at most pumpBudget of window-thread time, in slices
// of pumpSlice bytes, and leaves the rest queued for the next frame. Parsing
// costs from a few hundred to a few thousand nanoseconds per hundred bytes
// depending on what the bytes are, so a byte limit is either far under what a
// frame can afford or far over it; the time a frame can spare is what is
// bounded. The budget is half a frame at the 120 Hz wake spacing, which leaves
// the rest to input, painting and everything else on the thread. A slice is
// always parsed, so a Wake makes progress however slow the thread is. The shell
// ending, an exec capture and a caller without an Invalidator keep the old
// "all at once" behavior.
const (
	pumpBudget = time.Second / 240
	pumpSlice  = 32 << 10
)

// pump moves the shell's output into the screen and notices the shell's end.
func (state *State) pump(session *Session) {
	if t := session.term; t != nil {
		state.pumpFor(session, t.pumpBudget)
	}
}

// pumpFor is pump with the time it may spend parsing; see pumpBudget.
func (state *State) pumpFor(session *Session, budget time.Duration) {
	state.collectShell(session)
	t := session.term
	if t == nil {
		return
	}
	// Inline images finish decoding in the background; the ones that are ready
	// land here, in the order their bytes arrived.
	t.screen.ApplyImages()
	if t.shell == nil {
		return
	}
	t.rx = t.shell.Drain(t.rx) // keep what an earlier frame could not parse
	finished := false
	select {
	case <-t.shell.Done():
		finished = true
		t.rx = t.shell.Drain(t.rx) // output that arrived with the end
	default:
	}
	data := t.rx
	wasExec := t.exec != nil
	sliced := !finished && !wasExec && state.options.Invalidate != nil
	started := time.Now()
	consumed := 0
	for consumed < len(data) {
		chunk := data[consumed:]
		if sliced && len(chunk) > pumpSlice {
			// The capacity is cut too: the stages below may append to the chunk
			// and must not write over the output behind it.
			chunk = chunk[:pumpSlice:pumpSlice]
		}
		consumed += len(chunk)
		state.applyOutput(session, chunk, wasExec)
		if !sliced || time.Since(started) >= budget {
			break
		}
	}
	if state.autofillRules.armed {
		state.scanAutofill(session)
	}
	switch {
	case consumed < len(data):
		// Move the unparsed suffix to the front of the buffer. The next frame
		// appends new output after it, so order is kept.
		copy(t.rx, t.rx[consumed:])
		t.rx = t.rx[:len(t.rx)-consumed]
		state.options.Invalidate()
	case !finished:
		t.rx = t.rx[:0]
		if cap(t.rx) > 1<<22 {
			t.rx = nil // do not keep a flood's buffer
		}
	default:
		t.rx = t.rx[:0]
	}
	if !finished && len(t.rx) == 0 {
		state.reconcileSuggestionEcho(session)
	}
	if !finished && len(t.rx) == 0 && state.completion.list.active && state.completion.list.sessionID == session.ID {
		state.completionRefresh(session)
	}
	if finished {
		state.shellEnded(session)
	}
}

// applyOutput feeds one slice of the shell's output to the transfer scanner,
// the screen and whatever watches the output.
func (state *State) applyOutput(session *Session, data []byte, wasExec bool) {
	t := session.term
	if wasExec && len(data) > 0 {
		data = append(data[:0], t.feedExec(data)...)
	}
	if len(data) > 0 {
		data = state.scanTransfer(session, data)
	}
	if len(data) == 0 {
		return
	}
	state.receiveBackgroundOutput(session)
	t.recording.append(state.now(), terminalRecordingOutput, data)
	t.writeRemote(data, state.now())
	if wasExec {
		t.finishConsoleExec()
		t.markAIBlock(t.exec == nil)
	}
	t.recorder.Output(state.now())
	if state.autofillRules.armed {
		t.autofill.Output(state.now())
	}
	state.notePasswordPrompt(session)
	t.login.output(state.now())
	state.goLive(session)
}

// shellEnded tells the screen the session is over and lets the connection go.
func (state *State) shellEnded(session *Session) {
	session.outputAlert.commandRunning = false
	if state.completion.list.active && state.completion.list.sessionID == session.ID {
		state.closeCompletionList()
	}
	t := session.term
	state.goLive(session) // whatever it said, and that it ended, is the terminal's to show
	err := t.shell.Err()
	t.shell.Close()
	t.shell = nil
	t.finishExec(endShell)
	t.releasePromptWait()
	t.recording.stop()
	t.ended = true
	t.prediction.reset()
	t.preedit = app.TextState{}
	if t.told < toldEnded {
		t.told = toldEnded
		m := state.messages
		t.write([]byte("\r\n"))
		line := m.SessionEnded
		var exit interface{ ExitStatus() int }
		if errors.As(err, &exit) && exit.ExitStatus() != 0 {
			line = fmt.Sprintf(m.SessionEndedStatusFmt, exit.ExitStatus())
		}
		t.say(sgrMuted, line)
		t.say(sgrMuted, m.SessionReconnectHint)
	}
	session.tone = toneBusy
	session.busy = false
	state.stopRemoteStats(session)
	switch {
	case session.hasServer() && !t.inputSent && session.phase == sshconn.PhaseConnected:
		// The server ended a shell nobody had written to: an account that only
		// allows SFTP, a refusal printed and exit 1. The connection is sound and
		// stays, as when the server refuses the shell outright; the tab closes
		// it, or Enter dials again.
	case state.connectionInUse(session):
		session.keptAfterShell = true
	case session.conn != nil:
		session.conn.Cancel()
	}
}

// connectionInUse reports whether something other than the shell still works
// over the session's connection: its file panel, which may hold transfers, or a
// detached file window. Like OpenSSH, which waits for its other channels, the
// connection stays up for them.
func (state *State) connectionInUse(session *Session) bool {
	if session.files != nil {
		return true
	}
	w := state.sftpWindows[session.ID]
	return w != nil && !w.closed
}

// releaseKeptConnection closes a connection that outlived its shell once the
// last thing using it has gone.
func (state *State) releaseKeptConnection(session *Session) {
	if session == nil || !session.keptAfterShell || state.connectionInUse(session) {
		return
	}
	session.keptAfterShell = false
	if session.conn != nil {
		session.conn.Cancel()
	}
}

// terminalGeometry is where the terminal draws: the area below the window and
// info bars, and the cell grid that fits in it.
type terminalGeometry struct {
	area       gfx.Rect
	left, top  float32 // where cell (0,0) sits
	cols, rows int
}

// terminalGeometry lays the terminal out for a window size and cell size.
func terminalLayout(size gfx.Size, cellWidth, cellHeight float32, pad terminalPadding) terminalGeometry {
	return terminalLayoutWithInfoBar(size, cellWidth, cellHeight, infoBarHeight, pad)
}

func terminalLayoutWithInfoBar(size gfx.Size, cellWidth, cellHeight, barHeight float32, pad terminalPadding) terminalGeometry {
	return terminalLayoutWithChrome(size, cellWidth, cellHeight, terminalChrome{top: barHeight}, pad)
}

type terminalChrome struct {
	top float32
}

func terminalLayoutWithChrome(size gfx.Size, cellWidth, cellHeight float32, chrome terminalChrome, pad terminalPadding) terminalGeometry {
	area := gfx.Rect{
		Y:      windowBarHeight + chrome.top,
		Width:  size.Width,
		Height: max(size.Height-windowBarHeight-chrome.top, 0),
	}
	geometry := terminalGeometry{area: area, left: pad.x + pad.gutter, top: area.Y + pad.top}
	if cellWidth > 0 && cellHeight > 0 {
		geometry.cols = max(int((area.Width-2*pad.x-pad.gutter)/cellWidth), minTerminalCols)
		geometry.rows = max(int((area.Height-pad.top-pad.bottom)/cellHeight), minTerminalRows)
	}
	return geometry
}

const (
	minTerminalCols = 20
	minTerminalRows = 5
)

func (state *State) terminalChromeForSession(sessionID uint32) (terminalChrome, bool) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok {
		return terminalChrome{}, false
	}
	inactive := state.paneIsInactive(sessionID)
	finding := !inactive && session.term != nil && session.term.find.open
	inSplit := state.paneInSplit(sessionID)
	var chrome terminalChrome
	// Every pane in a split keeps the same top chrome whether it owns focus or
	// not. The active pane leaves that strip blank, but reserving it here keeps
	// switching panes from resizing either PTY or clearing a selection. The
	// shared rail keeps its width in find mode: the search bar takes this strip
	// and nothing else.
	if inSplit || finding {
		chrome.top = infoBarHeight
	}
	if state.terminalInputNoticeVisibleInWorkspace(session) && (inSplit || !finding) {
		chrome.top += inputNoticeHeight
	}
	return chrome, true
}

// terminalInputNoticeVisibleInWorkspace keeps a split's top chrome equal on
// every pane while any pane shows the locale warning. The warning belongs to
// one shell, but resizing only its pane would change that PTY and its selection
// each time focus moved. The notice itself is still painted only on its owner.
func (state *State) terminalInputNoticeVisibleInWorkspace(session *Session) bool {
	if state.terminalInputNoticeVisible(session) {
		return true
	}
	workspaceID, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if !ok {
		return false
	}
	for index := range state.sessions {
		other := &state.sessions[index]
		owner, _, owned := state.deck.PaneForSession(layoutcore.SessionID(other.ID))
		if owned && owner == workspaceID && state.terminalInputNoticeVisible(other) {
			return true
		}
	}
	return false
}

// terminalToolbarSession reports the active terminal whose eventual return to
// the terminal page needs the shared rail's width. It intentionally does not
// depend on activeSection: background terminals must already be fitted to the
// rail while settings or another page covers them.
func (state *State) terminalToolbarSession() (*Session, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil || session.restored || session.desk != nil {
		return nil, false
	}
	return session, true
}

// terminalSideRailSession reports the terminal whose shared rail is visible.
// Connection, restored and desktop pages use their own chrome. Find mode keeps
// the rail: its bar floats over the pane rather than taking the rail's room, so
// searching never resizes a PTY or clears a selection.
func (state *State) terminalSideRailSession() (*Session, bool) {
	if state.activeSection != sectionTerminal {
		return nil, false
	}
	session, ok := state.terminalToolbarSession()
	if !ok || state.showsProgress(session) {
		return nil, false
	}
	return session, true
}

// paneIsInactive reports a pane that shares its workspace without being the
// active one, which keeps only a compact identity in its bar.
func (state *State) paneIsInactive(sessionID uint32) bool {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	if !ok {
		return false
	}
	layout, ok := state.deck.Layout(workspaceID)
	return ok && layout.Len() >= 2 && state.deck.ActivePane(workspaceID) != paneID
}

// layout is the terminal geometry for the window as last laid out.
func (state *State) layout() terminalGeometry {
	return state.layoutForSession(state.activeSessionID)
}

func (state *State) layoutForSession(sessionID uint32) terminalGeometry {
	workspaceID, paneID, ok := state.deck.PaneForSession(layoutcore.SessionID(sessionID))
	if !ok {
		return state.terminalLayoutForEvent(sessionID, state.viewport)
	}
	fraction, _, ok := state.paneFractionBounds(workspaceID, paneID)
	if !ok {
		return state.terminalLayoutForEvent(sessionID, state.viewport)
	}
	bounds := pixelBounds(state.workspacePixelBoundsFor(workspaceID, state.viewport), fraction)
	geometry := state.terminalLayoutForEvent(sessionID, paneVirtualSize(bounds))
	origin := paneEmbedOrigin(bounds)
	geometry.area.X += origin.X
	geometry.area.Y += origin.Y
	geometry.left += origin.X
	geometry.top += origin.Y
	return geometry
}

// terminalLayoutForEvent lays out a terminal using the pane-local viewport and
// coordinates viva supplies after decoding an embedded hit.
func (state *State) terminalLayoutForEvent(sessionID uint32, viewport gfx.Size) terminalGeometry {
	chrome, sessionOK := state.terminalChromeForSession(sessionID)
	if !sessionOK {
		chrome = terminalChrome{top: infoBarHeight}
	}
	return terminalLayoutWithChrome(viewport, state.cellWidth, state.cellHeight, chrome, state.padding())
}

// fitTerminals gives every session's screen and pty the size the window now
// affords. Background tabs resize too, so switching to one finds it already
// the right size.
func (state *State) fitTerminals() {
	for index := range state.sessions {
		state.fitTerminal(&state.sessions[index])
	}
}

func (state *State) fitWorkspaceTerminals(workspaceID layoutcore.WorkspaceID) {
	for index := range state.sessions {
		owner, _, ok := state.deck.PaneForSession(layoutcore.SessionID(state.sessions[index].ID))
		if !ok || owner != workspaceID {
			continue
		}
		state.fitTerminal(&state.sessions[index])
	}
}

func (state *State) fitTerminal(session *Session) {
	t := session.term
	if t == nil {
		return
	}
	geometry := state.layoutForSession(session.ID)
	cols, rows := t.screen.Size()
	if cols == geometry.cols && rows == geometry.rows {
		return
	}
	t.prediction.reset()
	t.sync = nil
	t.screen.Resize(geometry.cols, geometry.rows)
	t.selection.clear()
	t.find.refreshIfChanged(t.screen, state.messages, 0)
	if t.shell != nil {
		t.shell.Resize(geometry.cols, geometry.rows)
	}
}

// Resize is the window-size callback: the terminals follow the window.
func (state *State) Resize(size gfx.Size) {
	state.viewport = size
	state.fitTerminals()
	state.fitSFTP(size)
	// Both panels stay open when the window docks them, but a window that grew
	// too narrow for both must not leave the assistant under the monitor.
	state.reconcileAIPanelWithMonitor(size)
}

// terminalSession finds the session whose terminal owns a hit.
func (state *State) terminalSession(hit gfx.ID) *Session {
	if hit.Scope() != hitTerminalList {
		return nil
	}
	for index := range state.sessions {
		if state.sessions[index].ID == uint32(item(hit)) {
			return &state.sessions[index]
		}
	}
	return nil
}

// TerminalInput resolves the native text surface for a focused terminal: a
// preedit surface at the cursor cell where input methods compose, and the
// receiver of committed text and pastes. The returned value is reused.
func (state *State) TerminalInput(context app.TextInputContext) *app.TextInput {
	if context.ID == hitAICLILoginTerminal {
		return state.aiCLILoginInput(context)
	}
	session, local, embedded := state.decodePaneHit(context.ID)
	if embedded {
		context.ID = local
	}
	if session == nil {
		session = state.terminalSession(context.ID)
	}
	if session == nil || session.term == nil || state.activeSessionID != 0 && session.ID != state.activeSessionID {
		// Typing goes nowhere from here, and the page cannot show it: the text
		// system simply gets no surface. Saying why, once per cause rather than
		// once per paint, is what turns "the keyboard stopped working" in a bug
		// report into a diagnosis. The reason is a fixed string, so nothing that
		// could identify a host or an account reaches the log.
		state.noteTerminalInputGap(session)
		return nil
	}
	if session.desk != nil {
		return state.desktopTextInput(session)
	}
	t := session.term
	geometry := state.layoutForSession(session.ID)
	cursor := t.screen.Cursor()
	if _, _, cells, ok := t.prediction.overlay(state.now()); ok {
		cursor.X += cells
	}
	cells := 1
	if t.preedit.Composing {
		cells += displayCells(t.preedit.Value)
	}
	t.input.Bounds = gfx.Rect{
		X:     geometry.left + float32(cursor.X)*state.cellWidth,
		Y:     geometry.top + float32(cursor.Y)*state.cellHeight,
		Width: float32(cells) * state.cellWidth, Height: state.cellHeight,
	}
	t.input.FontSize = state.terminalFontSize
	t.input.MonoFace = state.terminalFontFace
	t.input.Color = state.termPal.termFG
	t.input.State = t.preedit
	t.input.AltSendsEscape = state.terminalOption == terminalOptionMeta
	// A program that asked for kitty keyboard enhancements needs presses,
	// releases and raw printable keys, which Viva only delivers when the
	// terminal asks for key releases.
	t.input.KeyReleases = t.screen.KeyboardFlags() != 0 || t.screen.Modes().Win32Input
	if t.input.OnEvent == nil {
		// Closing or moving a tab shifts the session slice. Keep the identity,
		// not a pointer to a slot that may be cleared or hold another session.
		sessionID := session.ID
		t.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
			return state.terminalText(sessionID, event), nil
		}
	}
	return &t.input
}

// noteTerminalInputGap records why a terminal the shell cannot reach got no
// text surface. It is called from the paint path, so it reports a given cause
// once: the same gap repainted every frame would flood the log, and the
// diagnostics report keeps only the last few hundred lines. The key is the
// reason and the session it concerns, both stable across frames; the hit that
// asked is not, because the pane it names is re-derived on every paint.
func (state *State) noteTerminalInputGap(session *Session) {
	var note string
	var id uint32
	switch {
	case session == nil:
		note = "no session for the focused terminal"
	case session.term == nil:
		note, id = "session has no terminal", session.ID
	case session.ID != state.activeSessionID:
		note, id = "session is not the active one", session.ID
	default:
		return
	}
	key := fmt.Sprintf("%s %d", note, id)
	if key == state.inputGapNote {
		return
	}
	state.inputGapNote = key
	log.Printf("motty: terminal input has no surface: %s", note)
}

// terminalText applies a text-system event to a session's terminal.
func (state *State) terminalText(sessionID uint32, event app.TextEvent) app.TextState {
	var session *Session
	for index := range state.sessions {
		if state.sessions[index].ID == sessionID {
			session = &state.sessions[index]
		}
	}
	if session == nil || session.term == nil {
		return app.TextState{}
	}
	t := session.term
	redraw := false
	switch event.Kind {
	case app.TextInsert:
		t.preedit = app.TextState{}
		if state.modal == modalTerminalInput || state.guardTerminalText(session, event) {
			return app.TextState{Redraw: true}
		}
		// Typed text is short; a stack buffer keeps this off the heap per key.
		var scratch [16]byte
		input := append(scratch[:0], event.Text...)
		if state.typed(session, input) {
			// The typed text is drawn at once, ahead of the server's echo; the
			// event asks for that frame, which committed terminal text otherwise
			// never gets.
			redraw = t.prediction.input(t.screen, input, state.now(), t.inputLine && !session.isLocal())
		}
	case app.TextPaste:
		if state.modal == modalTerminalInput || state.guardTerminalText(session, event) {
			return app.TextState{Redraw: true}
		}
		redraw = t.prediction.reset()
		state.typed(session, term.EncodePaste(event.Text, t.screen.Modes().BracketedPaste))
	case app.TextPasteNoText:
		if state.modal != modalTerminalInput && state.pasteClipboardImage(session) {
			return app.TextState{}
		}
		if state.modal != modalTerminalInput && t.screen.Modes().BracketedPaste {
			state.typed(session, term.EncodePaste("", true))
		}
	case app.TextSetMarked:
		redraw = t.prediction.reset()
		runes := utf8.RuneCountInString(event.Text)
		t.preedit = app.TextState{
			Value: event.Text, Selection: event.MarkedSelection,
			Composition: app.TextRange{End: runes}, Composing: runes > 0,
		}
	case app.TextUnmark:
		redraw = t.prediction.reset()
		t.preedit = app.TextState{}
	}
	result := t.preedit
	result.Redraw = redraw
	return result
}

// displayCells is how many terminal cells text occupies.
func displayCells(value string) int {
	cells := 0
	for _, r := range value {
		cells += term.Width(r)
	}
	return cells
}

// terminalKey encodes keys left unclaimed by the application shortcut routes.
// Unclaimed Command chords are forwarded after kitty keyboard negotiation;
// macOS Command line edits keep their existing behavior.
func (state *State) terminalKey(session *Session, event gfx.KeyEvent) gfx.EventResult {
	t := session.term
	handled := gfx.EventResult{Handled: true}
	if t.preedit.Composing && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !t.screen.Modes().AltScreen {
		return handled
	}
	if event.Modifiers&gfx.ModifierMeta != 0 && !event.Key.IsModifier() {
		if letter, ok := term.CommandLineEdit(event.Key, event.Modifiers); ok && keymap.Primary == gfx.ModifierMeta {
			event.Key, event.Modifiers, event.Text, event.Typed = gfx.KeyUnknown, gfx.ModifierControl, letter, ""
		} else if t.screen.KeyboardFlags() == 0 || state.shortcutBound(event) {
			// A chord the application has bound belongs to it even where the
			// action does not apply: the router has already declined it for this
			// context, and sending it on would hand the program a Super key it
			// never asked for.
			return handled
		}
	}
	if event.Modifiers == gfx.ModifierControl|gfx.ModifierShift && event.Text == "c" {
		return state.copySelection(session)
	}
	if t.shell == nil {
		// Nothing is listening: Enter starts the session again.
		if event.Key == gfx.KeyEnter && !event.Repeat && state.canReconnect(session) {
			state.reconnect(session)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		// Every other key is swallowed, which looks exactly like a frozen window.
		// A shell that ended has already said so on the screen, so the status is
		// only needed when that message never ran: a tab restored locally, or a
		// shell that never opened.
		if t.told < toldEnded && !event.Release {
			state.status = state.messages.SessionNoShellInput
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return handled
	}
	if event.Modifiers&gfx.ModifierShift != 0 && event.Modifiers&gfx.ModifierMeta == 0 && !t.screen.Modes().AltScreen {
		_, rows := t.screen.Size()
		switch event.Key {
		case gfx.KeyPageUp:
			t.screen.Scroll(rows - 1)
			return gfx.EventResult{Handled: true, Repaint: true}
		case gfx.KeyPageDown:
			t.screen.Scroll(-(rows - 1))
			return gfx.EventResult{Handled: true, Repaint: true}
		}
	}
	modes := t.screen.Modes()
	if t.screen.KeyboardFlags() == 0 && !modes.Win32Input && (state.passwordHintKey(session, event) || state.ghostKey(session, event)) {
		return handled
	}
	var bytes []byte
	if modes.Win32Input {
		bytes = term.EncodeWin32Key(event)
	} else if runtime.GOOS == "darwin" && t.screen.KeyboardFlags() == 0 && !modes.AltScreen &&
		event.Modifiers == gfx.ModifierAlt && !event.Release {
		// Match macOS Terminal's word movement when the shell owns the key.
		// Alternate-screen programs and negotiated keyboard modes keep their keys.
		//
		// This stays in the application on purpose: which bytes a Mac viewer
		// sends for Option+arrow is terminal-emulation behaviour, which belongs
		// here rather than in the framework's key encoder. Viva keeps encoding
		// what the protocol defines; this is the platform convention layered on
		// top of a shell that negotiated nothing.
		//
		// Modifiers must be exactly Alt: Shift+Option is macOS's "select a whole
		// word", a different intent that the default encoding serves, so it is
		// deliberately left to fall through.
		switch event.Key {
		case gfx.KeyRight:
			bytes = []byte("\x1bf")
		case gfx.KeyLeft:
			bytes = []byte("\x1bb")
		}
	}
	if len(bytes) == 0 {
		bytes = term.EncodeKeyEvent(event, t.screen.KeyboardFlags(), modes.AppCursorKeys)
	}
	if len(bytes) > 0 {
		scrolled := t.screen.ViewOffset() > 0
		predictionChanged := t.prediction.reset()
		state.typed(session, bytes)
		handled.Repaint = scrolled || predictionChanged
	}
	return handled
}

// terminalToolbarKey keeps arrow navigation inside the visible tool rail.
// Paint scrolls the rail to the new focus on the following frame, which makes
// controls outside a short pane reachable without sending escape sequences to
// the shell.
func (state *State) terminalToolbarKey(session *Session, event gfx.KeyEvent) (gfx.EventResult, bool) {
	if session == nil || session.term == nil || session.ID != state.activeSessionID ||
		event.Modifiers != 0 || event.Release || session.term.railCount == 0 {
		return gfx.EventResult{}, false
	}
	t := session.term
	index := -1
	for at := 0; at < t.railCount; at++ {
		if t.railIDs[at] == event.Target {
			index = at
			break
		}
	}
	if index < 0 {
		return gfx.EventResult{}, false
	}
	next := index
	switch {
	case event.Key == gfx.KeyUp:
		for at := index - 1; at >= 0; at-- {
			if t.railEnabled[at] {
				next = at
				break
			}
		}
	case event.Key == gfx.KeyDown:
		for at := index + 1; at < t.railCount; at++ {
			if t.railEnabled[at] {
				next = at
				break
			}
		}
	case event.Key == gfx.KeyHome:
		for at := 0; at < t.railCount; at++ {
			if t.railEnabled[at] {
				next = at
				break
			}
		}
	case event.Key == gfx.KeyEnd:
		for at := t.railCount - 1; at >= 0; at-- {
			if t.railEnabled[at] {
				next = at
				break
			}
		}
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: next != index, Focus: t.railIDs[next], FocusVisible: true}, true
}

// shortcutBound reports whether the shortcut table binds the chord of event,
// whether or not its action applies to the focused target.
func (state *State) shortcutBound(event gfx.KeyEvent) bool {
	chord, ok := keymap.FromEvent(event)
	return ok && state.shortcutActionFor(chord) != shortcutNone
}

func (state *State) copySelection(session *Session) gfx.EventResult {
	selection := session.term.selection
	if !selection.selected {
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{
		Handled:       true,
		ClipboardText: selection.text(session.term.screen),
	}
}

// Scroll turns the wheel into scrollback, or arrow keys on the alternate
// screen, where a pager or editor owns the display and has no scrollback.
func (state *State) Scroll(event gfx.ScrollEvent) gfx.EventResult {
	if state.noteExit != nil {
		return gfx.EventResult{Handled: true}
	}
	if result := state.notesScroll(event); result.Handled {
		return result
	}
	if state.modal == modalFileProperties {
		return gfx.EventResult{Handled: true}
	}
	if state.modal == modalNone {
		if result, handled := state.workspaceArrangementScroll(event); handled {
			return result
		}
	}
	if state.modal == modalLocalProxy {
		return state.scrollLocalProxy(event)
	}
	if result := state.dashboardScroll(event); result.Handled {
		return result
	}
	if result := state.workspaceDraftScroll(event); result.Handled {
		return result
	}
	if result := state.scrollWorkspaceLibrary(event); result.Handled {
		return result
	}
	if result, handled := state.devEditorScroll(event); handled {
		return result
	}
	if state.chipMenu.open {
		// The menu is modal: only its own panels scroll.
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, state.chipMenu.popup) && state.chipMenu.tree.Wheel(event)}
	}
	if result := state.scrollProxyDialog(event); result.Handled {
		return result
	}
	if result := state.scrollForwardDialog(event); result.Handled {
		return result
	}
	if state.modal == modalNewHost && state.editor.groupPicker.control.Open {
		p := &state.editor.groupPicker
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, p.popup) && p.control.Wheel(event)}
	}
	if state.modal == modalToolbarSettings {
		return state.toolbarSettingsScroll(event)
	}
	if state.modal == modalNewHost && state.editor.tagPicker.control.Open {
		p := &state.editor.tagPicker
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, p.popup) && p.control.Wheel(event)}
	}
	if result, handled := state.remoteProcessWheel(event); handled {
		return result
	}
	if result, handled := state.transferCenterScroll(event); handled {
		return result
	}
	if result, handled := state.menuWheel(event); handled {
		return result
	}
	if result := state.toolbarDockScroll(event); result.Handled {
		return result
	}
	if state.modal == modalNewHost {
		return state.scrollHostForm(event)
	}
	if state.modal == modalFontPicker {
		return state.scrollFontPicker(event)
	}
	if state.modal == modalCommandPalette {
		return state.scrollCommandPalette(event)
	}
	if state.modal == modalForm {
		return state.scrollForm(event)
	}
	if state.modal == modalUpdate {
		return state.scrollUpdateNotes(event)
	}
	// Dialogs own the wheel even over their backdrop. Menus and scrollable
	// dialogs above have their own handlers; nothing behind them should move.
	if state.aiSettingsVisible() && state.ai.cfg.dialog == aiDialogImport && state.ai.cfg.importStep == aiImportPreview {
		// The import preview is one list, not pages: the wheel scrolls it while
		// the settings page behind the dialog stays still.
		return state.settingsRow.Wheel(event)
	}
	if state.modal != modalNone || state.knownHosts.action != knownHostsActionNone ||
		(state.aiSettingsVisible() && state.ai.cfg.dialog != aiDialogNone) {
		return gfx.EventResult{Handled: true}
	}
	// The assistant may be a drawer over a file tab. Give its painted panel the
	// wheel first; the file page only sees gestures outside that panel.
	if result, handled := state.aiScroll(event); handled {
		return result
	}
	tabScroll := &state.tabStrip
	if state.tabOrientation == tabsVertical && len(state.sftpTabs) > 0 &&
		pointInRect(event.X, event.Y, state.fileTabStrip.area) {
		tabScroll = &state.fileTabStrip
	}
	if result, handled := tabScroll.scroll(event); handled {
		return result
	}
	if state.fileTabVerticalRailAt(event.X, event.Y) {
		return gfx.EventResult{Handled: true}
	}
	if result, handled := state.sftpTabScroll(event); handled {
		return result
	}
	if state.tabOrientation == tabsVertical && pointInRect(event.X, event.Y, state.verticalTabs.bounds) {
		return gfx.EventResult{Handled: true}
	}
	if result, handled := state.scrollMonitor(event); handled {
		return result
	}
	// A wheel over a control of the files panel belongs to the panel's list. It
	// must not be treated as a pane event: that would shift its coordinates by a
	// pane's origin.
	if mount, ok := tree.MountOf(event.Target.Scope()); ok && mount == contextMount {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.files != nil && state.activeSection == sectionTerminal && state.modal == modalNone {
			return state.scrollSFTP(session.files, event)
		}
		return gfx.EventResult{}
	}
	// Empty space of the files panel names no control, so the panel is told
	// apart by where the pointer is. That is a window position: once a pane
	// shifts the event into its own coordinates, it would also match the part
	// of the terminal that lies as far from the pane's edge as the panel is wide.
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.files != nil &&
		state.activeSection == sectionTerminal && pointInRect(event.X, event.Y, state.contextRect) {
		return state.scrollSFTP(session.files, event)
	}
	if session, local, ok := state.decodePaneHit(event.Target); ok {
		// The wheel scrolls the pane under the pointer and leaves the active
		// pane where it is: activation is for a press.
		_, origin, viewport, embedded := state.paneEventGeometry(session.ID, event.Viewport)
		if embedded {
			event.X -= origin.X
			event.Y -= origin.Y
			event.Viewport = viewport
		}
		event.Target = local
		return state.scroll(event)
	}
	if session := state.terminalOrBarSession(event.Target); session != nil {
		_, origin, viewport, embedded := state.paneEventGeometry(session.ID, event.Viewport)
		if embedded {
			event.X -= origin.X
			event.Y -= origin.Y
			event.Viewport = viewport
		}
	}
	return state.scroll(event)
}

func (state *State) scroll(event gfx.ScrollEvent) gfx.EventResult {
	if state.activeSection == sectionSettings && state.modal == modalNone {
		return state.scrollSettings(event)
	}
	if state.activeSection == sectionHosts && state.modal == modalNone && state.hostsContent > 0 {
		return state.scrollHosts(event)
	}
	if state.activeSection == sectionSessionLogs && state.modal == modalNone {
		return state.scrollSessionLogs(event)
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if hovered := state.terminalOrBarSession(event.Target); hovered != nil {
		session, ok = hovered, true
	}
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.term == nil || state.cellWidth <= 0 || state.cellHeight <= 0 || state.showsProgress(session) {
		return gfx.EventResult{}
	}
	if result, handled := state.scrollTerminalToolbar(session, event); handled {
		return result
	}
	if session.desk != nil {
		return state.desktopScroll(session, event)
	}
	t := session.term
	modes := t.screen.Modes()
	if modes.Mouse != term.MouseOff && event.Target == hitTerminalList.At(int(session.ID)) && event.Modifiers&gfx.ModifierShift == 0 {
		return state.terminalMouseScroll(session, event)
	}
	if modes.AltScreen {
		if modes.Mouse != term.MouseOff || !modes.AlternateScroll {
			return gfx.EventResult{Handled: true}
		}
	}
	t.wheelY += event.StepY(state.cellHeight) / state.cellHeight
	lines := int(t.wheelY)
	if lines == 0 {
		return gfx.EventResult{Handled: true}
	}
	t.wheelY -= float32(lines)
	// Positive deltas reveal content below, that is, newer output.
	if modes.AltScreen {
		key := gfx.KeyDown
		if lines < 0 {
			key, lines = gfx.KeyUp, -lines
		}
		for range min(lines, 5) {
			t.send(term.EncodeKey(key, 0, "", modes.AppCursorKeys))
		}
		return gfx.EventResult{Handled: true}
	}
	if t.screen.Scroll(-lines) {
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{Handled: true}
}

func (state *State) terminalMouseScroll(session *Session, event gfx.ScrollEvent) gfx.EventResult {
	terminal := session.term
	position := state.terminalPositionAt(session, event.Viewport, event.X, event.Y)
	repaint := false
	terminal.wheelY += event.StepY(state.cellHeight) / state.cellHeight
	vertical := int(terminal.wheelY)
	terminal.wheelY -= float32(vertical)
	button := term.MouseWheelDown
	if vertical < 0 {
		button, vertical = term.MouseWheelUp, -vertical
	}
	for range min(vertical, 5) {
		repaint = terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseWheel, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		}) || repaint
	}

	terminal.wheelX += event.StepX(state.cellWidth) / state.cellWidth
	horizontal := int(terminal.wheelX)
	terminal.wheelX -= float32(horizontal)
	button = term.MouseWheelRight
	if horizontal < 0 {
		button, horizontal = term.MouseWheelLeft, -horizontal
	}
	for range min(horizontal, 5) {
		repaint = terminal.sendMouse(term.MouseEvent{
			Kind: term.MouseWheel, Button: button, Modifiers: terminalMouseModifiers(event.Modifiers),
			Column: position.Column, Row: position.Row,
		}) || repaint
	}
	return gfx.EventResult{Handled: true, Repaint: repaint}
}

func (state *State) sessionByIDPointer(id uint32) (*Session, bool) {
	for index := range state.sessions {
		if state.sessions[index].ID == id {
			return &state.sessions[index], true
		}
	}
	return nil, false
}
