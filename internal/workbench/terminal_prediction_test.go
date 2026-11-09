package workbench

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// echoed plays what a server does with typed text: it arrives after delay, is
// drawn on the emulator and then judged against the prediction.
func echoed(screen *term.Terminal, prediction *terminalPrediction, output string, now time.Duration) bool {
	screen.Write([]byte(output))
	return prediction.reconcile(screen, now)
}

// slowLink replaces the workbench clock with one the test moves, so that echoes
// can take as long as a long link makes them. The workbench draws no guess where
// the echo beats the next frame, which a test running in microseconds would be.
func slowLink(state *State) (advance func(time.Duration)) {
	now := state.now()
	state.options.Now = func() time.Duration { return now }
	return func(delay time.Duration) { now += delay }
}

func visibleAt(prediction *terminalPrediction, now time.Duration) (string, bool) {
	text, _, _, visible := prediction.overlay(now)
	return text, visible
}

func TestTerminalPredictionNeedsVerifiedEchoAndReconcilesRemoteOutput(t *testing.T) {
	screen := term.New(80, 24, 0)
	var prediction terminalPrediction
	now := time.Second

	for _, input := range []string{"a", "b", "c"} {
		prediction.input(screen, []byte(input), now, false)
		if _, visible := visibleAt(&prediction, now); visible {
			t.Fatalf("unverified input %q became visible", input)
		}
		echoed(screen, &prediction, input, now)
		now += time.Millisecond
	}

	prediction.input(screen, []byte("世界x"), now, false)
	text, visible := visibleAt(&prediction, now)
	if _, _, cells, _ := prediction.overlay(now); !visible || text != "世界x" || cells != 5 {
		t.Fatalf("verified prediction = %q, %d cells, visible=%v", text, cells, visible)
	}

	encoded := []byte("世")
	echoed(screen, &prediction, string(encoded[:1]), now)
	if text, _ := visibleAt(&prediction, now); text != "世界x" {
		t.Fatalf("partial UTF-8 confirmation left %q", text)
	}
	echoed(screen, &prediction, string(encoded[1:]), now)
	if text, _ := visibleAt(&prediction, now); text != "界x" {
		t.Fatalf("confirmed first rune left %q", text)
	}
	if _, _, cells, _ := prediction.overlay(now); cells != 3 {
		t.Fatalf("confirmed first rune left %d cells", cells)
	}

	if !echoed(screen, &prediction, "!", now) {
		t.Fatal("a mismatch did not report the visible overlay removal")
	}
	if _, visible := visibleAt(&prediction, now); visible {
		t.Fatal("a remote mismatch left predicted text visible")
	}
}

func TestTerminalPredictionTrustExpiresWithoutBackgroundWork(t *testing.T) {
	screen := term.New(80, 24, 0)
	var prediction terminalPrediction
	for _, input := range []byte("abc") {
		prediction.input(screen, []byte{input}, 0, false)
		echoed(screen, &prediction, string(input), 0)
	}
	prediction.input(screen, []byte("visible"), terminalPredictionTrust+time.Nanosecond, false)
	if _, visible := visibleAt(&prediction, terminalPredictionTrust+time.Nanosecond); visible {
		t.Fatal("expired echo trust still predicted input")
	}
	if prediction.phase != terminalPredictionProbe {
		t.Fatalf("expired phase = %v, want probe", prediction.phase)
	}
}

// Shells that color the line as it is typed, or draw a suggestion after the
// cursor, do not echo the typed bytes as they are. What counts is what is left
// in the cells before the cursor.
func TestTerminalPredictionConfirmsEchoThatColorsOrRedrawsTheLine(t *testing.T) {
	screen := term.New(80, 24, 0)
	screen.Write([]byte("$ "))
	var prediction terminalPrediction
	now := time.Second
	typed := ""
	const suggestion = " rest of a history line"
	for _, r := range "abc" {
		prediction.input(screen, []byte(string(r)), now, false)
		typed += string(r)
		// The whole line is drawn again, colored, with a suggestion behind it.
		line := fmt.Sprintf("\r\x1b[2K$ \x1b[32m%s\x1b[90m%s\x1b[39m\x1b[%dD", typed, suggestion, len(suggestion))
		echoed(screen, &prediction, line, now+60*time.Millisecond)
		now += 150 * time.Millisecond
	}
	prediction.input(screen, []byte("d"), now, false)
	if text, visible := visibleAt(&prediction, now); !visible || text != "d" {
		t.Fatalf("after three colored echoes the guess = %q (visible %v)", text, visible)
	}
}

// Typing faster than the round trip keeps text pending whenever an echo comes
// back, which is exactly when a guess matters most.
func TestTerminalPredictionActivatesWhileTypingFasterThanTheEchoReturns(t *testing.T) {
	screen := term.New(80, 24, 0)
	var prediction terminalPrediction
	const echo, gap = 200 * time.Millisecond, 80 * time.Millisecond
	type event struct {
		at     time.Duration
		typed  string
		output string
	}
	var events []event
	for i, r := range "abcdefgh" {
		at := time.Duration(i) * gap
		events = append(events, event{at: at, typed: string(r)}, event{at: at + echo, output: string(r)})
	}
	for i := 1; i < len(events); i++ { // stable by time: typed before output at a tie
		for j := i; j > 0 && events[j].at < events[j-1].at; j-- {
			events[j], events[j-1] = events[j-1], events[j]
		}
	}
	shown := 0
	for _, e := range events {
		if e.typed != "" {
			prediction.input(screen, []byte(e.typed), e.at, false)
			if _, visible := visibleAt(&prediction, e.at); visible {
				shown++
			}
		} else {
			echoed(screen, &prediction, e.output, e.at)
		}
	}
	if shown == 0 {
		t.Fatal("continuous typing over a slow link never produced a guess")
	}
}

func TestTerminalPredictionProofShowsTheFirstCharacterAndOnlyOnThatLine(t *testing.T) {
	screen := term.New(80, 24, 0)
	screen.Write([]byte("$ "))
	var prediction terminalPrediction
	now := time.Second

	prediction.input(screen, []byte("l"), now, false)
	if _, visible := visibleAt(&prediction, now); visible {
		t.Fatal("a first character without proof was shown")
	}
	prediction.reset()

	prediction.input(screen, []byte("l"), now, true)
	if text, visible := visibleAt(&prediction, now); !visible || text != "l" {
		t.Fatalf("with a prompt proof the first character = %q (visible %v)", text, visible)
	}

	// The server answers with something else: the guess goes, and this line is
	// not trusted again until the shell draws a prompt.
	echoed(screen, &prediction, "\r\nsurprise", now+50*time.Millisecond)
	if _, visible := visibleAt(&prediction, now); visible {
		t.Fatal("a mismatch left the guess visible")
	}
	prediction.input(screen, []byte("m"), now+100*time.Millisecond, true)
	if _, visible := visibleAt(&prediction, now); visible {
		t.Fatal("a failed line was trusted again before the next prompt")
	}
	prediction.reset()
	prediction.newPrompt()
	screen.Write([]byte("\r\n$ "))
	prediction.input(screen, []byte("n"), now+200*time.Millisecond, true)
	if _, visible := visibleAt(&prediction, now+200*time.Millisecond); !visible {
		t.Fatal("the next prompt did not restore the proof")
	}
}

// A server that never echoes (a vi-mode prompt in command mode, a program that
// reads keys silently) must not leave text on screen.
func TestTerminalPredictionTakesBackAGuessNobodyEchoes(t *testing.T) {
	screen := term.New(80, 24, 0)
	screen.Write([]byte("$ "))
	var prediction terminalPrediction
	now := time.Second
	prediction.input(screen, []byte("h"), now, true)
	remaining, ok := prediction.expiry(now)
	if !ok || remaining <= 0 {
		t.Fatalf("a shown guess has no deadline: %v %v", remaining, ok)
	}
	if _, visible := visibleAt(&prediction, now+remaining+time.Millisecond); visible {
		t.Fatal("a guess nobody echoed outlived its deadline")
	}
	prediction.input(screen, []byte("j"), now+remaining+time.Millisecond, true)
	if _, visible := visibleAt(&prediction, now+remaining+time.Millisecond); visible {
		t.Fatal("a line whose guess went unanswered was trusted again")
	}
}

// Where the echo returns before the next frame there is nothing to gain, so no
// text is drawn; where it takes longer, it is.
func TestTerminalPredictionStaysHiddenOnLinksFasterThanAFrame(t *testing.T) {
	for _, testCase := range []struct {
		name string
		echo time.Duration
		want bool
	}{
		{"fast link", 5 * time.Millisecond, false},
		{"slow link", 80 * time.Millisecond, true},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			screen := term.New(80, 24, 0)
			screen.Write([]byte("$ "))
			var prediction terminalPrediction
			now := time.Second
			for _, r := range "ab" {
				prediction.input(screen, []byte(string(r)), now, true)
				echoed(screen, &prediction, string(r), now+testCase.echo)
				now += time.Second / 5
			}
			prediction.input(screen, []byte("c"), now, true)
			if _, visible := visibleAt(&prediction, now); visible != testCase.want {
				t.Fatalf("visible = %v after echoes of %v, want %v", visible, testCase.echo, testCase.want)
			}
		})
	}
}

// A link whose echo delay hovers at an edge must not make the guess flicker.
func TestTerminalPredictionShowsByTheLinkWithHysteresis(t *testing.T) {
	var prediction terminalPrediction
	for _, testCase := range []struct {
		echo  time.Duration
		shown bool
	}{
		{10 * time.Millisecond, false},
		{25 * time.Millisecond, false}, // between the edges: no change yet
		{40 * time.Millisecond, true},
		{25 * time.Millisecond, true}, // between the edges: stays shown
		{15 * time.Millisecond, false},
	} {
		prediction.echo = testCase.echo
		prediction.judgeLink()
		if prediction.shown != testCase.shown {
			t.Fatalf("echo %v: shown=%v, want %v", testCase.echo, prediction.shown, testCase.shown)
		}
	}
}

// Output that arrives when nothing was typed, such as the next prompt, ends
// the trust the echoes earned.
func TestTerminalPredictionDropsTrustOnUnsolicitedOutput(t *testing.T) {
	screen := term.New(80, 24, 0)
	var prediction terminalPrediction
	now := time.Second
	for _, r := range "abc" {
		prediction.input(screen, []byte(string(r)), now, false)
		echoed(screen, &prediction, string(r), now)
	}
	echoed(screen, &prediction, "\r\nPassword: ", now)
	prediction.input(screen, []byte("secret"), now, false)
	if _, visible := visibleAt(&prediction, now); visible {
		t.Fatal("text typed at a prompt that followed unsolicited output was shown")
	}
}

func TestWorkbenchPaintsOnlyVerifiedPredictionsAndDropsThemAtSensitiveContext(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	advance := slowLink(state)

	for _, input := range []string{"a", "b", "c"} {
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: input})
		if _, texts := paintedTexts(state); containsExact(texts, input) {
			t.Fatalf("probe %q was painted before remote confirmation: %q", input, texts)
		}
		advance(80 * time.Millisecond)
		shell.say(input)
		state.Wake()
	}

	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "deploy"})
	if strings.Contains(screenText(state, 0), "deploy") {
		t.Fatal("prediction was committed into the terminal emulator")
	}
	canvas, texts := paintedTexts(state)
	if !containsExact(texts, "deploy") {
		t.Fatalf("verified prediction was not painted: %q; phase=%v length=%d confirmed=%d now=%v trust=%v", texts,
			session.term.prediction.phase, session.term.prediction.length, session.term.prediction.confirmed,
			state.now(), session.term.prediction.trustedUntil)
	}
	foundPrediction := false
	for _, text := range canvas.Texts() {
		if text.Value != "deploy" {
			continue
		}
		runs := canvas.TextRuns(text)
		// The guess keeps the terminal's own color and carries no mark, so the
		// echo that replaces it changes nothing a reader could see crawl across
		// the line.
		foundPrediction = len(runs) > 0 && !runs[0].Underline && runs[0].Color == state.termPal.termFG
	}
	if !foundPrediction {
		t.Fatal("prediction is not drawn in the terminal's own color, unmarked")
	}

	advance(80 * time.Millisecond)
	shell.say("de")
	state.Wake()
	if _, texts = paintedTexts(state); !containsExact(texts, "ploy") {
		t.Fatalf("partial confirmation did not shrink the overlay: %q", texts)
	}
	if !strings.Contains(screenText(state, 0), "abcde") {
		t.Fatalf("confirmed output did not reach the real screen: %q", screenText(state, 0))
	}

	// A prompt or any unrelated output invalidates the echo context. Text typed
	// after that point remains invisible until the remote proves echo again,
	// which is what keeps password input from being exposed locally.
	shell.say("\r\nPassword: ")
	state.Wake()
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "secret"})
	if _, texts = paintedTexts(state); containsExact(texts, "secret") {
		t.Fatalf("unconfirmed sensitive input was painted: %q", texts)
	}
}

// The shell's own prompt marker is a proof that typing is at an input line, so
// the first character is shown at once; Enter, Tab and the like end the proof,
// and a password asked for after a command is never shown.
func TestWorkbenchShowsTheFirstCharacterAtAProvenPromptOnly(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	typed := func(text string) []string {
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: text})
		_, texts := paintedTexts(state)
		return texts
	}

	if texts := typed("x"); containsExact(texts, "x") {
		t.Fatal("a character was shown at a prompt that proved nothing")
	}
	shell.say("\r\n" + folderSyncMarker(session, "A") + "$ " + folderSyncMarker(session, "B"))
	state.Wake()
	if texts := typed("l"); !containsExact(texts, "l") {
		t.Fatalf("the first character at a proven prompt was not shown: %q", texts)
	}

	// Tab leaves the line the shell drew: nothing more is guessed on it.
	key(state, 0, gfx.KeyEvent{Key: gfx.KeyTab})
	shell.say("\r\n" + folderSyncMarker(session, "A") + "$ ls" + folderSyncMarker(session, "B"))
	state.Wake()
	key(state, 0, gfx.KeyEvent{Key: gfx.KeyTab})
	if texts := typed("s"); containsExact(texts, "s") {
		t.Fatal("a character was shown after Tab ended the proof")
	}

	// A command starts, then asks for a password: the proof is gone with the
	// command, and the echo-less answer is never drawn.
	shell.say("\r\n" + folderSyncMarker(session, "A") + "$ " + folderSyncMarker(session, "B"))
	state.Wake()
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "sudo ls"})
	key(state, 0, gfx.KeyEvent{Key: gfx.KeyEnter})
	shell.say(folderSyncMarker(session, "C") + "\r\n[sudo] password for me: ")
	state.Wake()
	if texts := typed("hunter2"); containsExact(texts, "hunter2") {
		t.Fatalf("a password was shown: %q", texts)
	}
}

// A block cursor is an inverted cell of the grid, not a shape drawn over it, so
// while a guess is on screen the caret must sit after the typed text rather than
// the whole cursor disappearing until the server catches up.
func TestWorkbenchKeepsTheBlockCursorPastTheTypedTextAheadOfTheEcho(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]
	advance := slowLink(state)
	for _, input := range []string{"a", "b", "c"} {
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: input})
		advance(80 * time.Millisecond)
		shell.say(input)
		state.Wake()
	}
	state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "deploy"})
	guess, _, cells, predicting := session.term.prediction.overlay(state.now())
	if !predicting {
		t.Fatalf("the guess is not on screen: %q", guess)
	}
	cursor := session.term.presentedCursor()
	caretX := cursor.X + cells

	var canvas gfx.Canvas
	hit := terminalHit(state, 0)
	canvas.SetInteraction(hit, gfx.ID{}, hit, true)
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	geometry := state.layout()

	// The cell under a point, as the terminal's own column arithmetic finds it.
	cellAt := func(x, y float32) (int, int) {
		return int((x - geometry.left) / state.cellWidth), int((y - geometry.top) / state.cellHeight)
	}
	// The cursor cell is the one filled with the cursor color: a block cursor has
	// no rectangle of its own, its cell is inverted instead.
	cursorCell := func(column, row int) bool {
		centerX := geometry.left + (float32(column)+0.5)*state.cellWidth
		centerY := geometry.top + (float32(row)+0.5)*state.cellHeight
		for _, rectangle := range canvas.Rectangles() {
			bounds := rectangle.Bounds
			if rectangle.Color != state.termPal.cursor || bounds.Height < state.cellHeight {
				continue
			}
			if centerX >= bounds.X && centerX < bounds.X+bounds.Width &&
				centerY >= bounds.Y && centerY < bounds.Y+bounds.Height {
				return true
			}
		}
		return false
	}

	if !cursorCell(caretX, cursor.Y) {
		column, row := cellAt(geometry.left+(float32(caretX)+0.5)*state.cellWidth, geometry.top+(float32(cursor.Y)+0.5)*state.cellHeight)
		t.Fatalf("the block cursor is not at the caret (%d,%d), the cell after %q, in %d cells", column, row, guess, cells)
	}
	if cursorCell(cursor.X, cursor.Y) {
		t.Fatalf("the cursor stayed on the emulator's column %d under the typed text", cursor.X)
	}
}

func containsExact(values []string, want string) bool {
	for _, value := range values {
		if value == want {
			return true
		}
	}
	return false
}

func BenchmarkTerminalPredictionRoundTrip(b *testing.B) {
	screen := term.New(120, 40, 0)
	b.ReportAllocs()
	for b.Loop() {
		screen.Write([]byte("\r\n$ "))
		var prediction terminalPrediction
		prediction.input(screen, []byte("abc"), 0, false)
		echoed(screen, &prediction, "abc", 0)
		prediction.input(screen, []byte("deploy"), 0, false)
		echoed(screen, &prediction, "deploy", 0)
	}
}
