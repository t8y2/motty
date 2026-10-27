package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/autofill"
	"motty/internal/cmdhistory"
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
	// zmodemTail is the last bytes of output, in case a transfer header is cut
	// between two reads.
	zmodemTail []byte

	told            announcement
	skippedEndpoint sshconn.Endpoint
	skippedRole     sshconn.EndpointRole
	skipsTold       int
	live            bool             // the shell has spoken, or ended: the screen is the page to show
	ended           bool             // the shell finished, or never started
	shellOpened     time.Duration    // when the shell was asked for, to give a silent one up
	opening         chan shellResult // a shell being opened in the background, until pump collects it
	blink           cursorBlink
	wheelX          float32 // horizontal wheel travel not yet worth a whole cell
	wheelY          float32 // vertical wheel travel not yet worth a whole cell

	// label is the accessible name of the terminal, rebuilt only when the
	// language or the address it is made from changes.
	label, labelFormat, labelAddress string

	// preedit is the input-method composition, shown at the cursor.
	preedit app.TextState
	input   app.TextInput // reused, so resolving the text input allocates nothing
	cache   rowCache
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
	// prediction draws typed text before the server echoes it; see terminalPrediction.
	prediction terminalPrediction
	// autofill is what the autofill rules need to know of this terminal.
	autofill autofill.Watcher
	// recorder decides which typed lines the command history may keep; it is
	// only fed while historyOn says the history is wanted for this terminal.
	recorder  cmdhistory.Recorder
	historyOn func() bool
	onCommand func(string)
	// takeOutput brings in what the shell has already echoed, so the screen is
	// read after the echo of the keys that came before an Enter.
	takeOutput func()

	// exec is the command the assistant is waiting on in this terminal.
	exec *aiExec
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
	return &terminal{screen: term.New(cols, rows, scrollback), find: newTerminalFind()}
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
	session.term.openLink = state.openTerminalLink
	session.term.clock = state.now
	state.attachHistory(session)
	state.stampTerminal(session.term)
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
	session.term.screen.SetCwdChange(func(cwd term.Cwd) {
		state.terminalDirectory(sessionID, cwd)
	})
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

func (t *terminal) write(p []byte) {
	t.selection.clear()
	t.prediction.reset()
	t.screen.Write(p)
}

// writeRemote commits the shell's output. Unlike write it first lets the
// prediction see the bytes, so an echo of what was typed confirms the overlay
// instead of dropping it.
func (t *terminal) writeRemote(p []byte, now time.Duration) {
	t.selection.clear()
	t.prediction.observe(p, now)
	t.screen.Write(p)
}

// send writes input to the shell. Typing returns the view to the live screen.
func (t *terminal) send(p []byte) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	if t.exec != nil && userInterrupts(p) {
		t.finishExec(false, false) // the user took the terminal back; the keys go on to the shell
	}
	t.selection.clear()
	t.screen.ScrollToBottom()
	t.autofill.Typed(p)
	if t.historyOn != nil && t.historyOn() {
		if t.takeOutput != nil && bytes.ContainsAny(p, "\r\n") {
			t.takeOutput()
		}
		if command, ok := t.recorder.Typed(p, cmdhistory.TermScreen{Term: t.screen}, t.recordingNow()); ok && t.onCommand != nil {
			t.onCommand(command)
		}
	}
	accepted := t.shell.Write(p)
	if accepted {
		t.recording.append(t.recordingNow(), terminalRecordingInput, p)
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
	return t.shell.Write(p)
}

// sendSecret writes an autofill answer to the shell. It is not typed input, so
// it is neither recorded nor mistaken for the user's own keys.
func (t *terminal) sendSecret(p []byte) bool {
	if t.shell == nil || len(p) == 0 {
		return false
	}
	return t.shell.Write(p)
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
	shell Shell
	err   error
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
	if session.host.ForwardX11 && session.host.isSSH() {
		environ := x11fwd.CurrentEnviron()
		config.X11 = &environ
	}
	if session.host.AgentForward && session.host.isSSH() {
		config.AgentForward = true
	}
	if _, slow := session.conn.(slowShellOpener); !slow {
		shell, err := session.conn.OpenShell(config)
		state.shellOpened(session, shell, err)
		return
	}
	result := make(chan shellResult, 1)
	t.opening = result
	conn, invalidate := session.conn, state.options.Invalidate
	go func() {
		shell, err := conn.OpenShell(config)
		result <- shellResult{shell, err}
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
		state.describe(session)
		return
	}
	t.shell = shell
	t.shellOpened = state.now()
	forwardNotes := 0
	if noter, ok := shell.(interface{ X11Note() string }); ok && session.host.ForwardX11 {
		if note := noter.X11Note(); note != "" {
			line := fmt.Sprintf(state.messages.Tools.X11UnavailableFmt, note)
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

// pumpBudget bounds how much output one Wake parses. A flood can leave a
// megabyte waiting, and parsing it in one go costs several milliseconds on the
// window thread; the rest stays queued and the next frame takes it. The shell
// ending, an exec capture and a caller without an Invalidator keep the old
// "all at once" behavior.
const pumpBudget = 128 << 10

// pump moves the shell's output into the screen and notices the shell's end.
func (state *State) pump(session *Session) {
	state.collectShell(session)
	t := session.term
	if t == nil || t.shell == nil {
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
	pending := false
	if len(data) > pumpBudget && !finished && t.exec == nil && state.options.Invalidate != nil {
		data, pending = data[:pumpBudget], true
	}
	wasExec := t.exec != nil
	if wasExec && len(data) > 0 {
		data = append(data[:0], t.feedExec(data)...)
	}
	if len(data) > 0 {
		data = state.scanTransfer(session, data)
	}
	if len(data) > 0 {
		t.recording.append(state.now(), terminalRecordingOutput, data)
		t.writeRemote(data, state.now())
		if wasExec {
			t.markAIBlock(t.exec == nil)
		}
		t.recorder.Output(state.now())
		if state.autofillRules.armed {
			t.autofill.Output(state.now())
		}
		state.goLive(session)
	}
	if state.autofillRules.armed {
		state.scanAutofill(session)
	}
	switch {
	case pending:
		// Move the unparsed suffix to the front of the buffer. The next frame
		// appends new output after it, so order is kept.
		copy(t.rx, t.rx[pumpBudget:])
		t.rx = t.rx[:len(t.rx)-pumpBudget]
		state.options.Invalidate()
	case !finished:
		t.rx = t.rx[:0]
		if cap(t.rx) > 1<<22 {
			t.rx = nil // do not keep a flood's buffer
		}
	default:
		t.rx = t.rx[:0]
	}
	if finished {
		state.shellEnded(session)
	}
}

// shellEnded tells the screen the session is over and lets the connection go.
func (state *State) shellEnded(session *Session) {
	t := session.term
	state.goLive(session) // whatever it said, and that it ended, is the terminal's to show
	err := t.shell.Err()
	t.shell.Close()
	t.shell = nil
	t.finishExec(false, false)
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
	if session.conn != nil {
		session.conn.Cancel()
	}
}

// terminalGeometry is where the terminal draws: the area below the window and
// info bars, and the cell grid that fits in it.
type terminalGeometry struct {
	area       ui.Rect
	left, top  float32 // where cell (0,0) sits
	cols, rows int
}

// terminalGeometry lays the terminal out for a window size and cell size.
func terminalLayout(size ui.Size, cellWidth, cellHeight float32, pad terminalPadding) terminalGeometry {
	return terminalLayoutWithInfoBar(size, cellWidth, cellHeight, infoBarHeight, pad)
}

func terminalLayoutWithInfoBar(size ui.Size, cellWidth, cellHeight, barHeight float32, pad terminalPadding) terminalGeometry {
	area := ui.Rect{Y: windowBarHeight + barHeight, Width: size.Width, Height: max(size.Height-windowBarHeight-barHeight, 0)}
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

// terminalInfoBarHeight is the height of a pane's bar. Active or not it is the
// same: the bar that changed height when focus moved made the terminals under
// it jump.
func (state *State) terminalInfoBarHeight(sessionID uint32) float32 { return infoBarHeight }

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
		return terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	}
	fraction, _, ok := state.paneFractionBounds(workspaceID, paneID)
	if !ok {
		return terminalLayout(state.viewport, state.cellWidth, state.cellHeight, state.padding())
	}
	bounds := pixelBounds(state.workspacePixelBoundsFor(workspaceID, state.viewport), fraction)
	geometry := terminalLayoutWithInfoBar(paneVirtualSize(bounds), state.cellWidth, state.cellHeight, state.terminalInfoBarHeight(sessionID), state.padding())
	origin := paneEmbedOrigin(bounds)
	geometry.area.X += origin.X
	geometry.area.Y += origin.Y
	geometry.left += origin.X
	geometry.top += origin.Y
	return geometry
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
func (state *State) Resize(size ui.Size) {
	state.viewport = size
	state.fitTerminals()
	state.fitSFTP(size)
}

// terminalSession finds the session whose terminal owns a hit.
func (state *State) terminalSession(hit ui.ID) *Session {
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
		return nil
	}
	if session.desk != nil {
		return state.desktopTextInput(session)
	}
	t := session.term
	geometry := state.layoutForSession(session.ID)
	cursor := t.screen.Cursor()
	if _, _, cells, ok := t.prediction.overlay(); ok {
		cursor.X += cells
	}
	cells := 1
	if t.preedit.Composing {
		cells += displayCells(t.preedit.Value)
	}
	t.input.Bounds = ui.Rect{
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
		t.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
			return state.terminalText(session.ID, event), nil
		}
	}
	return &t.input
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
		// Typed text is short; a stack buffer keeps this off the heap per key.
		var scratch [16]byte
		input := append(scratch[:0], event.Text...)
		if state.typed(session, input) {
			// The typed text is drawn at once, ahead of the server's echo; the
			// event asks for that frame, which committed terminal text otherwise
			// never gets.
			redraw = t.prediction.input(t.screen, input, state.now())
		}
	case app.TextPaste:
		redraw = t.prediction.reset()
		state.typed(session, term.EncodePaste(event.Text, t.screen.Modes().BracketedPaste))
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

// terminalKey handles a raw key for the terminal of session. Command chords
// drive the tabs; everything else becomes the bytes a program expects.
func (state *State) terminalKey(session *Session, event ui.KeyEvent) ui.EventResult {
	t := session.term
	handled := ui.EventResult{Handled: true}
	if event.Modifiers&ui.ModifierMeta != 0 && !event.Key.IsModifier() {
		return state.commandChord(session, event)
	}
	if event.Modifiers == ui.ModifierControl|ui.ModifierShift && event.Text == "c" {
		return state.copySelection(session)
	}
	if t.shell == nil {
		// Nothing is listening: Enter starts the session again.
		if event.Key == ui.KeyEnter && !event.Repeat && state.canReconnect(session) {
			state.reconnect(session)
			return ui.EventResult{Handled: true, Repaint: true}
		}
		return handled
	}
	if event.Modifiers&ui.ModifierShift != 0 && !t.screen.Modes().AltScreen {
		_, rows := t.screen.Size()
		switch event.Key {
		case ui.KeyPageUp:
			t.selection.clear()
			t.screen.Scroll(rows - 1)
			return ui.EventResult{Handled: true, Repaint: true}
		case ui.KeyPageDown:
			t.selection.clear()
			t.screen.Scroll(-(rows - 1))
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	modes := t.screen.Modes()
	if t.screen.KeyboardFlags() == 0 && !modes.Win32Input && state.ghostKey(session, event) {
		return handled
	}
	var bytes []byte
	if modes.Win32Input {
		bytes = term.EncodeWin32Key(event)
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

// commandChord handles Command+key while a terminal has the keyboard.
func (state *State) commandChord(session *Session, event ui.KeyEvent) ui.EventResult {
	result := ui.EventResult{Handled: true, Repaint: true}
	switch event.Text {
	case "c":
		return state.copySelection(session)
	case "w":
		state.closeActivePaneOrTab()
	case "t":
		state.activeSection = sectionHosts
		state.save()
	default:
		if len(event.Text) == 1 && event.Text[0] >= '1' && event.Text[0] <= '9' {
			index := int(event.Text[0] - '1')
			if index < len(state.sessions) {
				state.activeSessionID = state.sessions[index].ID
				state.save()
			}
			return result
		}
		return ui.EventResult{Handled: true}
	}
	return result
}

func (state *State) copySelection(session *Session) ui.EventResult {
	selection := session.term.selection
	if !selection.selected {
		return ui.EventResult{Handled: true}
	}
	return ui.EventResult{
		Handled:       true,
		ClipboardText: session.term.screen.SelectedText(selection.anchor, selection.focus),
	}
}

// Scroll turns the wheel into scrollback, or arrow keys on the alternate
// screen, where a pager or editor owns the display and has no scrollback.
func (state *State) Scroll(event ui.ScrollEvent) ui.EventResult {
	if state.modal == modalNewHost && state.editor.tagPicker.control.Open {
		p := &state.editor.tagPicker
		return ui.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, p.popup) && p.control.Wheel(event)}
	}
	if result, handled := state.transferCenterScroll(event); handled {
		return result
	}
	if result, handled := state.menuWheel(event); handled {
		return result
	}
	if state.modal == modalFontPicker {
		return state.scrollFontPicker(event)
	}
	if state.modal == modalCommandPalette {
		return state.scrollCommandPalette(event)
	}
	if state.modal == modalUpdate {
		return state.scrollUpdateNotes(event)
	}
	// Dialogs own the wheel even over their backdrop. Menus and scrollable
	// dialogs above have their own handlers; nothing behind them should move.
	if state.modal != modalNone || state.knownHosts.action != knownHostsActionNone ||
		(state.aiSettingsVisible() && state.ai.cfg.dialog != aiDialogNone) {
		return ui.EventResult{Handled: true}
	}
	if result, handled := state.tabStrip.scroll(event); handled {
		return result
	}
	if result, handled := state.aiScroll(event); handled {
		return result
	}
	// A wheel over a control of the files panel belongs to the panel's list. It
	// must not be treated as a pane event: that would shift its coordinates by a
	// pane's origin.
	if mount, ok := tree.MountOf(event.Target.Scope()); ok && mount == contextMount {
		if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.files != nil && state.activeSection == sectionTerminal && state.modal == modalNone {
			return state.scrollSFTP(session.files, event)
		}
		return ui.EventResult{}
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
	if session := state.terminalSession(event.Target); session != nil {
		_, origin, viewport, embedded := state.paneEventGeometry(session.ID, event.Viewport)
		if embedded {
			event.X -= origin.X
			event.Y -= origin.Y
			event.Viewport = viewport
		}
	}
	return state.scroll(event)
}

func (state *State) scroll(event ui.ScrollEvent) ui.EventResult {
	if state.activeSection == sectionSettings && state.modal == modalNone {
		return state.scrollSettings(event)
	}
	if state.activeSection == sectionHosts && state.modal == modalNone && state.hostsContent > 0 {
		return state.scrollHosts(event)
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if ok && session.files != nil && pointInRect(event.X, event.Y, state.contextRect) {
		return state.scrollSFTP(session.files, event)
	}
	if hovered := state.terminalSession(event.Target); hovered != nil {
		session, ok = hovered, true
	}
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.term == nil || state.cellWidth <= 0 || state.cellHeight <= 0 || state.showsProgress(session) {
		return ui.EventResult{}
	}
	if session.desk != nil {
		return state.desktopScroll(session, event)
	}
	t := session.term
	modes := t.screen.Modes()
	if modes.Mouse != term.MouseOff && event.Target == hitTerminalList.At(int(session.ID)) && event.Modifiers&ui.ModifierShift == 0 {
		return state.terminalMouseScroll(t, event)
	}
	if modes.AltScreen {
		if modes.Mouse != term.MouseOff || !modes.AlternateScroll {
			return ui.EventResult{Handled: true}
		}
	}
	t.wheelY += event.DeltaY / state.cellHeight
	lines := int(t.wheelY)
	if lines == 0 {
		return ui.EventResult{Handled: true}
	}
	t.wheelY -= float32(lines)
	// Positive deltas reveal content below, that is, newer output.
	if modes.AltScreen {
		key := ui.KeyDown
		if lines < 0 {
			key, lines = ui.KeyUp, -lines
		}
		for range min(lines, 5) {
			t.send(term.EncodeKey(key, 0, "", modes.AppCursorKeys))
		}
		return ui.EventResult{Handled: true}
	}
	t.selection.clear()
	if t.screen.Scroll(-lines) {
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{Handled: true}
}

func (state *State) terminalMouseScroll(terminal *terminal, event ui.ScrollEvent) ui.EventResult {
	position := state.terminalPositionAt(event.Viewport, event.X, event.Y)
	repaint := false
	terminal.wheelY += event.DeltaY / state.cellHeight
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

	terminal.wheelX += event.DeltaX / state.cellWidth
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
	return ui.EventResult{Handled: true, Repaint: repaint}
}

func (state *State) sessionByIDPointer(id uint32) (*Session, bool) {
	for index := range state.sessions {
		if state.sessions[index].ID == id {
			return &state.sessions[index], true
		}
	}
	return nil, false
}
