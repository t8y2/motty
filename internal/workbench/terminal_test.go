package workbench

import (
	"errors"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
	"motty/internal/term"
	layoutcore "motty/internal/workspace"
)

// liveSession opens the selected host and takes its connection to the point
// where the shell runs, as a server that accepted the credentials would.
func liveSession(t testing.TB) (*State, *world, *fakeConn, *fakeShell) {
	t.Helper()
	world, options := newWorld(t)
	options.Invalidate = func() {}
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.ServerVersion = "SSH-2.0-Test"
		s.Method = "password"
	})
	state.Wake()
	shell := fake.shell(t)
	shell.say("\x1b[0m") // the shell speaks, and the terminal takes the window from the connection page
	state.Wake()
	return state, world, fake, shell
}

func terminalHit(state *State, index int) ui.ID {
	return hitTerminalList.At(int(state.sessions[index].ID))
}

func key(state *State, index int, event ui.KeyEvent) ui.EventResult {
	event.Target = terminalHit(state, index)
	return state.Key(event)
}

func screenText(state *State, index int) string { return state.sessions[index].term.screen.Text() }

func TestAConnectedSessionOpensAShellTheSizeOfTheTerminal(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	geometry := state.layout()
	if len(fake.configs) != 1 || fake.configs[0].Cols != geometry.cols || fake.configs[0].Rows != geometry.rows {
		t.Fatalf("shell config = %+v, want %dx%d", fake.configs, geometry.cols, geometry.rows)
	}
	if geometry.cols < 100 || geometry.rows < 30 {
		t.Fatalf("a 1400x900 window fits only %dx%d cells", geometry.cols, geometry.rows)
	}
	if fake.configs[0].Notify == nil {
		t.Fatal("the shell cannot wake the window")
	}
	screen := screenText(state, 0)
	for _, hidden := range []string{"Waiting for the server", "Connected to", "Server:", "Authenticated with"} {
		if strings.Contains(screen, hidden) {
			t.Errorf("the terminal still contains local connection status %q:\n%s", hidden, screen)
		}
	}
	// Waking again changes nothing and opens no second shell.
	state.Wake()
	if len(fake.shells) != 1 {
		t.Fatalf("shells = %d", len(fake.shells))
	}
}

func TestShellOutputReachesTheScreenWithItsColors(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[01;32mdeploy@prod\x1b[00m:\x1b[01;34m~\x1b[00m$ ls\r\n\x1b[31mred.txt\x1b[0m  plain.txt\r\n")
	state.Wake()
	if !strings.Contains(screenText(state, 0), "deploy@prod:~$ ls\nred.txt  plain.txt") {
		t.Fatalf("screen:\n%s", screenText(state, 0))
	}
	canvas, _ := paintedTexts(state)
	var prompt, files ui.Text
	for _, text := range canvas.Texts() {
		switch {
		case strings.HasPrefix(text.Value, "deploy@prod"):
			prompt = text
		case strings.HasPrefix(text.Value, "red.txt"):
			files = text
		}
	}
	if !prompt.Style.Mono || prompt.RunCount < 3 {
		t.Fatalf("the prompt is not a multi-run fixed-pitch line: %+v", prompt)
	}
	runs := canvas.TextRuns(prompt)
	if runs[0].Color != ui.RGB(98, 206, 136) || runs[0].Weight != ui.WeightBold || runs[0].Column != 0 {
		t.Errorf("first prompt run = %+v, want bold green at column 0", runs[0])
	}
	fileRuns := canvas.TextRuns(files)
	if len(fileRuns) != 2 || fileRuns[0].Color != ui.RGB(240, 120, 120) || fileRuns[0].Bytes != 9 ||
		fileRuns[1].Column != 9 || fileRuns[1].Color != state.pal.text {
		t.Errorf("file runs = %+v, want red \"red.txt\" (and the blanks after it) then plain at column 9", fileRuns)
	}
}

func TestTheWindowSizeDecidesTheTerminalSizeOfEverySession(t *testing.T) {
	state, world, _, shell := liveSession(t)
	if !state.splitActivePane(layoutcore.Horizontal) {
		t.Fatal("split right")
	}
	state.Click(hitPaneChooseHost)
	state.Click(hitHostList.At(2))
	connect(t, state, world) // a second session, still connecting
	state.Resize(ui.Size{Width: 1000, Height: 700})
	geometry := state.layout()
	if geometry.cols >= 150 {
		t.Fatalf("1000 points fit %d columns", geometry.cols)
	}
	for index := range state.sessions {
		if cols, rows := state.sessions[index].term.screen.Size(); cols != geometry.cols || rows != geometry.rows {
			t.Errorf("session %d is %dx%d, want %dx%d", index, cols, rows, geometry.cols, geometry.rows)
		}
	}
	if len(shell.resizes) != 2 || shell.resizes[1] != [2]int{geometry.cols, geometry.rows} {
		t.Fatalf("the pty was told %v, want pane creation then %dx%d", shell.resizes, geometry.cols, geometry.rows)
	}
	state.Resize(ui.Size{Width: 1000, Height: 700})
	if len(shell.resizes) != 2 {
		t.Fatal("an unchanged size resized the pty again")
	}
}

func TestRawKeysBecomeTheBytesAProgramExpects(t *testing.T) {
	state, _, _, shell := liveSession(t)
	for _, event := range []ui.KeyEvent{
		{Key: ui.KeyEnter}, {Key: ui.KeyUp}, {Key: ui.KeyTab}, {Key: ui.KeyEscape}, {Key: ui.KeyBackspace},
		{Modifiers: ui.ModifierControl, Text: "c"}, {Key: ui.KeyLeft, Modifiers: ui.ModifierAlt}, {Key: ui.KeyF5},
		{Key: ui.KeyPageUp}, {Key: ui.KeyDelete},
	} {
		if result := key(state, 0, event); !result.Handled {
			t.Fatalf("%+v was not taken", event)
		}
	}
	if got, want := shell.typed(), "\r\x1b[A\t\x1b\x7f\x03\x1b[1;3D\x1b[15~\x1b[5~\x1b[3~"; got != want {
		t.Fatalf("sent %q, want %q", got, want)
	}

	// An application that asked for application cursor keys gets SS3 arrows.
	shell.say("\x1b[?1h")
	state.Wake()
	before := len(shell.typed())
	key(state, 0, ui.KeyEvent{Key: ui.KeyUp})
	if got := shell.typed()[before:]; got != "\x1bOA" {
		t.Fatalf("application-mode Up = %q", got)
	}
	// Command chords are the application's, not the program's.
	before = len(shell.typed())
	key(state, 0, ui.KeyEvent{Modifiers: ui.ModifierMeta, Text: "c"})
	if len(shell.typed()) != before {
		t.Fatal("a Command chord reached the program")
	}
}

func TestTextPasteAndInputMethodsReachTheProgram(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	context := app.TextInputContext{ID: terminalHit(state, 0), Terminal: true}
	input := state.TerminalInput(context)
	if input == nil || input.OnEvent == nil {
		t.Fatal("no text input for the focused terminal")
	}
	if input.FontSize != state.terminalFontSize || input.Bounds.Height != state.cellHeight {
		t.Fatalf("input = %+v", input)
	}
	if state.TerminalInput(app.TextInputContext{ID: hitTerminalList.At(77777), Terminal: true}) != nil {
		t.Fatal("a text input for a terminal that does not exist")
	}

	// The preedit surface sits on the cursor cell.
	shell.say("abc")
	state.Wake()
	cursor := state.sessions[0].term.screen.Cursor()
	moved := state.TerminalInput(context)
	geometry := state.layout()
	wantX := geometry.left + float32(cursor.X)*state.cellWidth
	if moved.Bounds.X != wantX || moved.Bounds.Y != geometry.top+float32(cursor.Y)*state.cellHeight {
		t.Fatalf("input at %+v, want x=%v for cursor (%d,%d)", moved.Bounds, wantX, cursor.X, cursor.Y)
	}

	next, err := input.OnEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "ni", MarkedSelection: app.TextRange{Start: 2, End: 2}})
	if err != nil || !next.Composing || next.Value != "ni" || next.Composition != (app.TextRange{End: 2}) {
		t.Fatalf("composition = %+v, %v", next, err)
	}
	if shell.typed() != "" {
		t.Fatalf("a composition leaked to the program: %q", shell.typed())
	}
	next, _ = input.OnEvent(app.TextEvent{Kind: app.TextInsert, Text: "你"})
	if next.Composing || next.Value != "" || shell.typed() != "你" {
		t.Fatalf("commit left %+v with %q sent", next, shell.typed())
	}
	input.OnEvent(app.TextEvent{Kind: app.TextSetMarked, Text: "x", MarkedSelection: app.TextRange{Start: 1, End: 1}})
	if next, _ = input.OnEvent(app.TextEvent{Kind: app.TextUnmark}); next.Composing {
		t.Fatal("unmarking left the composition")
	}

	// A paste is wrapped only when the program asked for bracketed paste.
	input.OnEvent(app.TextEvent{Kind: app.TextPaste, Text: "a\nb"})
	if got := shell.typed(); got != "你a\rb" {
		t.Fatalf("plain paste sent %q", got)
	}
	shell.say("\x1b[?2004h")
	state.Wake()
	input.OnEvent(app.TextEvent{Kind: app.TextPaste, Text: "ls\n"})
	if got := shell.typed(); !strings.HasSuffix(got, "\x1b[200~ls\r\x1b[201~") {
		t.Fatalf("bracketed paste sent %q", got)
	}
}

func TestTheProgramsAnswersGoBackToIt(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[c\x1b[6n")
	state.Wake()
	got := shell.typed()
	if !strings.HasPrefix(got, "\x1b[?62;22c\x1b[") || !strings.HasSuffix(got, "R") {
		t.Fatalf("answers = %q", got)
	}
}

func TestAnEndedSessionSaysSoAndEnterConnectsAgain(t *testing.T) {
	state, world, fake, shell := liveSession(t)
	shell.say("bye\r\n")
	shell.end(&exitStatus{3})
	state.Wake()
	screen := screenText(state, 0)
	if !strings.Contains(screen, "bye") || !strings.Contains(screen, "[Session ended with exit status 3]") ||
		!strings.Contains(screen, "Press Enter to connect again.") {
		t.Fatalf("screen:\n%s", screen)
	}
	if !shell.closed || fake.canceled == 0 || state.sessions[0].term.shell != nil {
		t.Fatalf("the ended shell was not released: closed=%v canceled=%d", shell.closed, fake.canceled)
	}
	if status, _ := state.sessionStatus(&state.sessions[0]); status != state.messages.HostStateIdle {
		t.Fatalf("status = %q", status)
	}

	// Typing at a dead session does nothing; Enter dials the same host in the same tab.
	key(state, 0, ui.KeyEvent{Key: ui.KeyUp})
	if len(world.conns) != 1 {
		t.Fatal("a key redialed")
	}
	if result := key(state, 0, ui.KeyEvent{Key: ui.KeyEnter}); !result.Handled || !result.Repaint {
		t.Fatalf("Enter result = %+v", result)
	}
	if len(world.conns) != 2 || len(state.sessions) != 1 {
		t.Fatalf("conns=%d sessions=%d, want a second connection in the same tab", len(world.conns), len(state.sessions))
	}
	screen = screenText(state, 0)
	if !strings.Contains(screen, "bye") || strings.Contains(screen, "Waiting for the server") {
		t.Fatalf("the tab did not keep its history clean while starting a new attempt:\n%s", screen)
	}
}

// exitStatus is a stand-in for *ssh.ExitError.
type exitStatus struct{ code int }

func (e *exitStatus) Error() string   { return "exit status" }
func (e *exitStatus) ExitStatus() int { return e.code }

func TestAFailedConnectionStaysOnScreenAndOffersAnotherTry(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseFailed; s.Err = errors.New("connection refused") })
	state.Wake()
	screen := screenText(state, 0)
	if !strings.Contains(screen, "Connection failed: connection refused") || !strings.Contains(screen, "Press Enter to connect again.") {
		t.Fatalf("screen:\n%s", screen)
	}
	if len(fake.shells) != 0 {
		t.Fatal("a failed connection opened a shell")
	}
	key(state, 0, ui.KeyEvent{Key: ui.KeyEnter})
	if len(world.conns) != 2 {
		t.Fatal("Enter did not dial again")
	}
}

func TestAShellThatCannotStartIsReportedOnTheScreen(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)
	fake.shellErr = errors.New("pty refused")
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	if !strings.Contains(screenText(state, 0), "Could not start a shell: pty refused") || state.sessions[0].tone != toneFail {
		t.Fatalf("screen:\n%s", screenText(state, 0))
	}
}

func TestClosingATabReleasesItsConnectionAndPicksANeighbour(t *testing.T) {
	state, world, fake, shell := liveSession(t)
	state.Click(hitHosts)
	state.Click(hitHostList.At(2))
	connect(t, state, world)
	state.Click(hitHosts)
	state.Click(hitHostList.At(3))
	connect(t, state, world)
	if len(state.sessions) != 3 {
		t.Fatalf("sessions = %d", len(state.sessions))
	}
	ids := []uint32{state.sessions[0].ID, state.sessions[1].ID, state.sessions[2].ID}

	// Closing the first tab, which is not active, keeps the active one.
	state.Click(hitTabCloseList.At(int(ids[0])))
	if !shell.closed || fake.canceled == 0 {
		t.Fatal("the closed tab's shell and connection were not released")
	}
	if len(state.sessions) != 2 || state.activeSessionID != ids[2] || len(state.tabOptions) != 2 {
		t.Fatalf("sessions=%d active=%d tabs=%d", len(state.sessions), state.activeSessionID, len(state.tabOptions))
	}
	// Closing the active one moves to the tab that took its place.
	state.Click(hitTabCloseList.At(int(ids[2])))
	if state.activeSessionID != ids[1] || state.activeSection != sectionTerminal {
		t.Fatalf("active=%d section=%d", state.activeSessionID, state.activeSection)
	}
	state.Click(hitTabCloseList.At(int(ids[1])))
	if len(state.sessions) != 0 || state.activeSection != sectionHosts || state.activeSessionID != 0 {
		t.Fatalf("closing the last tab left sessions=%d section=%d", len(state.sessions), state.activeSection)
	}
	state.Click(hitTabCloseList.At(999)) // unknown: ignored
}

func TestCommandChordsDriveTheTabs(t *testing.T) {
	state, world, _, _ := liveSession(t)
	state.Click(hitHosts)
	state.Click(hitHostList.At(2))
	connect(t, state, world)
	first, second := state.sessions[0].ID, state.sessions[1].ID

	key(state, 1, ui.KeyEvent{Modifiers: ui.ModifierMeta, Text: "1"})
	if state.activeSessionID != first {
		t.Fatal("Command+1 did not select the first tab")
	}
	key(state, 0, ui.KeyEvent{Modifiers: ui.ModifierMeta, Text: "2"})
	if state.activeSessionID != second {
		t.Fatal("Command+2 did not select the second tab")
	}
	key(state, 1, ui.KeyEvent{Modifiers: ui.ModifierMeta, Text: "9"}) // no ninth tab
	if state.activeSessionID != second {
		t.Fatal("a missing tab changed the selection")
	}
	key(state, 1, ui.KeyEvent{Modifiers: ui.ModifierMeta, Text: "t"})
	if state.activeSection != sectionHosts {
		t.Fatal("Command+T did not open the host list")
	}
	state.activeSection = sectionTerminal
	key(state, 1, ui.KeyEvent{Modifiers: ui.ModifierMeta, Text: "w"})
	if len(state.sessions) != 1 || state.activeSessionID != first {
		t.Fatalf("Command+W left sessions=%d active=%d", len(state.sessions), state.activeSessionID)
	}
}

func TestTheWheelScrollsBackAndTypingReturnsToTheBottom(t *testing.T) {
	state, _, _, shell := liveSession(t)
	var output strings.Builder
	for line := 1; line <= 200; line++ {
		output.WriteString("line ")
		output.WriteString(strings.Repeat("x", line%7))
		output.WriteString("\r\n")
	}
	shell.say(output.String())
	state.Wake()
	t0 := state.sessions[0].term
	if t0.screen.Scrollback() < 100 {
		t.Fatalf("scrollback = %d", t0.screen.Scrollback())
	}

	// A notch of three lines upward reveals older output: negative deltas.
	result := state.Scroll(ui.ScrollEvent{DeltaY: -3 * state.cellHeight, Precise: true})
	if !result.Handled || !result.Repaint || t0.screen.ViewOffset() != 3 {
		t.Fatalf("scroll up: %+v, offset %d", result, t0.screen.ViewOffset())
	}
	// Sub-line travel accumulates instead of being lost.
	for range 4 {
		state.Scroll(ui.ScrollEvent{DeltaY: state.cellHeight / 4, Precise: true})
	}
	if t0.screen.ViewOffset() != 2 {
		t.Fatalf("after a line of fractional travel the offset is %d, want 2", t0.screen.ViewOffset())
	}
	key(state, 0, ui.KeyEvent{Key: ui.KeyEnter})
	if t0.screen.ViewOffset() != 0 {
		t.Fatal("typing did not return to the bottom")
	}

	// Shift+PageUp pages through history without touching the program.
	typed := len(shell.typed())
	key(state, 0, ui.KeyEvent{Key: ui.KeyPageUp, Modifiers: ui.ModifierShift})
	_, rows := t0.screen.Size()
	if t0.screen.ViewOffset() != rows-1 || len(shell.typed()) != typed {
		t.Fatalf("Shift+PageUp: offset %d, sent %q", t0.screen.ViewOffset(), shell.typed()[typed:])
	}

	// On the alternate screen the wheel is arrow keys only when the program
	// explicitly asks for alternate scroll mode.
	t0.screen.ScrollToBottom()
	shell.say("\x1b[?1049h")
	state.Wake()
	typed = len(shell.typed())
	state.Scroll(ui.ScrollEvent{DeltaY: 2 * state.cellHeight, Precise: true})
	if got := shell.typed()[typed:]; got != "" {
		t.Fatalf("wheel without alternate-scroll mode sent %q", got)
	}
	shell.say("\x1b[?1007h")
	state.Wake()
	typed = len(shell.typed())
	state.Scroll(ui.ScrollEvent{DeltaY: 2 * state.cellHeight, Precise: true})
	if got := shell.typed()[typed:]; got != "\x1b[B\x1b[B" {
		t.Fatalf("wheel on the alternate screen sent %q", got)
	}
	// The wheel does not reach the terminal off the terminal page: the host list
	// may take it, but the terminal's view stays where it was.
	state.Click(hitHosts)
	offset, typed := t0.screen.ViewOffset(), len(shell.typed())
	state.Scroll(ui.ScrollEvent{DeltaY: 100})
	if t0.screen.ViewOffset() != offset || len(shell.typed()) != typed {
		t.Fatal("a wheel over the host list moved or typed into the terminal")
	}
}

func TestTheTerminalHasTheKeyboardWhenItAppears(t *testing.T) {
	state, _, _, _ := liveSession(t)
	canvas, _ := paintedTexts(state)
	hit, ok := hitByID(canvas, terminalHit(state, 0))
	if !ok || hit.Role != ui.RoleTerminal || !hit.Autofocus || !hit.Focusable {
		t.Fatalf("terminal hit = %+v, %v", hit, ok)
	}
	if hit.Label != "Terminal deploy@prod.example.com:22" {
		t.Fatalf("label = %q", hit.Label)
	}
	geometry := state.layout()
	if hit.Bounds != geometry.area || hit.Bounds.X != state.workspacePixelBounds(state.viewport).X || hit.Bounds.X+hit.Bounds.Width != 1400 || hit.Bounds.Y+hit.Bounds.Height != 900 {
		t.Fatalf("the terminal %+v does not fill the window below the bars", hit.Bounds)
	}
	// Window-bar buttons leave the keyboard where it is.
	for _, id := range []ui.ID{hitHome, hitNewTab, hitLanguage, hitTheme, hitWindowSettings, hitTabCloseList.At(int(state.sessions[0].ID))} {
		bar, ok := hitByID(canvas, id)
		if !ok || bar.Focusable {
			t.Errorf("hit %d = %+v, %v; want a mouse-only button", id, bar, ok)
		}
	}
}

func TestInfoBarSaysNothingWhileSimplyConnected(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	_, texts := paintedTexts(state)
	if contains(texts, "deploy@prod.example.com:22") || contains(texts, "Connected") {
		t.Fatalf("a plain connection repeats itself in the bar: %q", texts)
	}
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseClosed })
	state.Wake()
	_, texts = paintedTexts(state)
	if !contains(texts, "Not connected") {
		t.Fatalf("a closed session reads %q", texts)
	}
}

var _ = term.New

func TestAnIdleConnectedTerminalAsksForNoFrames(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	fake := connect(t, state, world)

	// While the connection is being made its page and the tab's light animate,
	// at their own pace rather than the display's.
	canvas, _ := paintedTexts(state)
	if delay, ok := canvas.NextFrame(); !ok || delay < min(pulseStep, connectionSignalStep) {
		t.Fatalf("a connecting session asks for its next frame after %v, %v; want a paced request", delay, ok)
	}
	fake.set(func(s *sshconn.Status) { s.Phase = sshconn.PhaseConnected })
	state.Wake()
	fake.shell(t).say("$ ")
	state.Wake()

	// Once connected, nothing in the window moves: no spinner, no cursor blink.
	canvas, _ = paintedTexts(state)
	if delay, ok := canvas.NextFrame(); ok {
		t.Fatalf("an idle connected terminal keeps asking for frames (next in %v)", delay)
	}
}

func TestALargeFloodIsParsedOverSeveralWakes(t *testing.T) {
	state, _, _, shell := liveSession(t)
	invalidations := 0
	state.options.Invalidate = func() { invalidations++ }
	// The escape sequence starts exactly on the budget boundary, so the parser
	// has to carry its state into the next pump.
	payload := strings.Repeat("a", pumpBudget-1) + "\x1b[31mred\x1b[0m\r\n"
	shell.say(payload)
	state.Wake()
	session := &state.sessions[0]
	if session.term.rx == nil || len(session.term.rx) == 0 {
		t.Fatal("the whole flood was parsed in one Wake")
	}
	if invalidations == 0 {
		t.Fatal("no continuation frame was requested")
	}
	for attempts := 0; len(session.term.rx) > 0 && attempts < 100; attempts++ {
		state.Wake()
	}
	if len(session.term.rx) != 0 {
		t.Fatalf("%d bytes still queued after 100 Wakes", len(session.term.rx))
	}
	if text := session.term.screen.Text(); !strings.Contains(text, "red") {
		t.Fatalf("the split escape sequence did not reach the screen: %q", text)
	}
}

func TestALargeFloodIsParsedInOneWakeWithoutAnInvalidator(t *testing.T) {
	state, _, _, shell := liveSession(t)
	state.options.Invalidate = nil
	shell.say(strings.Repeat("x", 2*pumpBudget))
	state.Wake()
	if session := &state.sessions[0]; len(session.term.rx) != 0 {
		t.Fatalf("%d bytes remained without an Invalidator", len(session.term.rx))
	}
}
