package mosh

import (
	"strings"
	"testing"

	"motty/internal/term"
)

func hostMessage(t *testing.T, instructions ...HostInstruction) []byte {
	t.Helper()
	return MarshalHostMessage(instructions)
}

// The state a diffs are applied to is the state its reference names, and the
// delivered update turns the previous delivery into the new one.
func TestScreenStateAppliesDiffsAgainstTheirReference(t *testing.T) {
	initial := newScreenState(20, 4)
	first := initial.clone()
	ack, ok := first.apply(hostMessage(t, HostInstruction{Hoststring: []byte("ab"), EchoAckNum: 7}))
	if !ok || ack != 7 {
		t.Fatalf("apply = %d, %v; want the echo ack 7", ack, ok)
	}
	// A second state from the same initial reference sees only "ab" and adds
	// "cd": its screen is "abcd".
	second := initial.clone()
	if _, ok := second.apply(hostMessage(t, HostInstruction{Hoststring: []byte("abcd")})); !ok {
		t.Fatal("second state rejected")
	}
	update := second.diffFrom(first)
	// The owner already shows the first state; the update turns it into the
	// second.
	display := term.New(20, 4, 0)
	display.Write(first.diffFrom(nil))
	display.Write(update)
	if got := strings.TrimSpace(display.RowText(0)); got != "abcd" {
		t.Fatalf("displayed %q, want %q", got, "abcd")
	}
	// Only the cells the first state did not already show may be written.
	if strings.Contains(string(update), "abab") {
		t.Fatalf("the update repeated what was already displayed: %q", update)
	}
}

func TestScreenStateCarriesBellsAndClipboard(t *testing.T) {
	initial := newScreenState(20, 4)
	next := initial.clone()
	if _, ok := next.apply(hostMessage(t,
		HostInstruction{Hoststring: []byte("\a\x1b]52;c;aGVsbG8=\a")},
		HostInstruction{EchoAckNum: 3},
	)); !ok {
		t.Fatal("apply rejected the message")
	}
	update := next.diffFrom(initial)
	if !strings.Contains(string(update), "\a") {
		t.Fatalf("update has no bell: %q", update)
	}
	if !strings.Contains(string(update), "\x1b]52;c;aGVsbG8=\a") {
		t.Fatalf("update has no clipboard write: %q", update)
	}
	// The same state delivered twice changes nothing.
	if update := next.diffFrom(next.clone()); len(update) != 0 {
		t.Fatalf("an unchanged state produced %q", update)
	}
}

func TestScreenStateFirstFrameRepaints(t *testing.T) {
	initial := newScreenState(20, 4)
	state := initial.clone()
	state.apply(hostMessage(t, HostInstruction{Hoststring: []byte("prompt $")}))
	update := state.diffFrom(nil)
	if !strings.HasPrefix(string(update), "\x1b[r\x1b[0m\x1b[H\x1b[2J") {
		t.Fatalf("first frame does not clear the screen: %q", update)
	}
}

func TestScreenStateResizes(t *testing.T) {
	state := newScreenState(20, 4)
	next := state.clone()
	if _, ok := next.apply(hostMessage(t, HostInstruction{Width: 10, Height: 2})); !ok {
		t.Fatal("resize rejected")
	}
	cols, rows := next.(*screenState).screen.Size()
	if cols != 10 || rows != 2 {
		t.Fatalf("size %dx%d, want 10x2", cols, rows)
	}
	display := term.New(20, 4, 0)
	display.Write(next.diffFrom(state))
	if cols, rows := display.Size(); cols != 20 || rows != 4 {
		t.Fatalf("the owner is resized by the owner, not by the diff: %dx%d", cols, rows)
	}
}

func benchmarkScreen(b *testing.B) *screenState {
	b.Helper()
	state := newScreenState(80, 24)
	for y := 0; y < 24; y++ {
		state.apply(MarshalHostMessage([]HostInstruction{{Hoststring: []byte("prompt $ some command output with \x1b[1;32mcolor\x1b[0m\r\n")}}))
	}
	return state
}

func BenchmarkScreenStateClone(b *testing.B) {
	state := benchmarkScreen(b)
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = state.clone()
	}
}

// One received state: the C server's common case, a small diff applied to a
// clone of the state it names.
func BenchmarkScreenStateApplyOneLine(b *testing.B) {
	state := benchmarkScreen(b)
	message := MarshalHostMessage([]HostInstruction{{Hoststring: []byte("x")}})
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		next := state.clone()
		next.apply(message)
	}
}

func BenchmarkScreenStateDiffOneLine(b *testing.B) {
	state := benchmarkScreen(b)
	next := state.clone()
	next.apply(MarshalHostMessage([]HostInstruction{{Hoststring: []byte("x")}}))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = next.diffFrom(state)
	}
}
