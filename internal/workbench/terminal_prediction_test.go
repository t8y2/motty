package workbench

import (
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"motty/internal/term"
)

func TestTerminalPredictionNeedsVerifiedEchoAndReconcilesRemoteOutput(t *testing.T) {
	screen := term.New(80, 24, 0)
	var prediction terminalPrediction
	now := time.Second

	for _, input := range []string{"a", "b", "c"} {
		prediction.input(screen, []byte(input), now)
		if _, _, _, visible := prediction.overlay(); visible {
			t.Fatalf("unverified input %q became visible", input)
		}
		prediction.observe([]byte(input), now)
		screen.Write([]byte(input))
		now += time.Millisecond
	}

	prediction.input(screen, []byte("世界x"), now)
	text, _, cells, visible := prediction.overlay()
	if !visible || text != "世界x" || cells != 5 {
		t.Fatalf("verified prediction = %q, %d cells, visible=%v", text, cells, visible)
	}

	encoded := []byte("世")
	prediction.observe(encoded[:1], now)
	if text, _, _, _ := prediction.overlay(); text != "世界x" {
		t.Fatalf("partial UTF-8 confirmation left %q", text)
	}
	prediction.observe(encoded[1:], now)
	screen.Write(encoded)
	if text, _, cells, _ := prediction.overlay(); text != "界x" || cells != 3 {
		t.Fatalf("confirmed first rune left %q, %d cells", text, cells)
	}

	if !prediction.observe([]byte("!"), now) {
		t.Fatal("a mismatch did not report the visible overlay removal")
	}
	if _, _, _, visible := prediction.overlay(); visible {
		t.Fatal("a remote mismatch left predicted text visible")
	}
}

func TestTerminalPredictionTrustExpiresWithoutBackgroundWork(t *testing.T) {
	screen := term.New(80, 24, 0)
	var prediction terminalPrediction
	for _, input := range []byte("abc") {
		prediction.input(screen, []byte{input}, 0)
		prediction.observe([]byte{input}, 0)
		screen.Write([]byte{input})
	}
	prediction.input(screen, []byte("visible"), terminalPredictionTrust+time.Nanosecond)
	if _, _, _, visible := prediction.overlay(); visible {
		t.Fatal("expired echo trust still predicted input")
	}
	if prediction.phase != terminalPredictionProbe {
		t.Fatalf("expired phase = %v, want probe", prediction.phase)
	}
}

func TestWorkbenchPaintsOnlyVerifiedPredictionsAndDropsThemAtSensitiveContext(t *testing.T) {
	state, _, _, shell := liveSession(t)
	session := &state.sessions[0]

	for _, input := range []string{"a", "b", "c"} {
		state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: input})
		if _, texts := paintedTexts(state); containsExact(texts, input) {
			t.Fatalf("probe %q was painted before remote confirmation: %q", input, texts)
		}
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
		foundPrediction = len(runs) > 0 && runs[0].Underline && runs[0].Color == state.pal.muted
	}
	if !foundPrediction {
		t.Fatal("prediction is not visually distinguished as remote-unconfirmed")
	}

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
		var prediction terminalPrediction
		prediction.input(screen, []byte("abc"), 0)
		prediction.observe([]byte("abc"), 0)
		prediction.input(screen, []byte("deploy"), 0)
		prediction.observe([]byte("deploy"), 0)
	}
}
