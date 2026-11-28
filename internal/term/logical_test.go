package term

import (
	"strings"
	"testing"
)

// The logical identity is additive: the emulator never reads it, so these tests
// also pin the physical behaviour it must leave alone — row ids, row text, the
// cursor and prompt handling.

func TestSoftWrappedRowsShareOneLogical(t *testing.T) {
	screen := New(5, 4, 100)
	feed(screen, "abcdefghij")

	first, ok := screen.LogicalAt(1)
	if !ok {
		t.Fatal("LogicalAt(1) on a soft-wrapped line is not ok")
	}
	second, ok := screen.LogicalAt(2)
	if !ok || second != first {
		t.Fatalf("second wrapped row logical = %d, %v; want %d, true", second, ok, first)
	}
	if screen.Row(0).ID == screen.Row(1).ID {
		t.Fatal("the wrapped rows share a physical identity")
	}
	if !screen.Row(0).Wrapped || screen.Row(1).Wrapped {
		t.Fatal("the wrapped rows are not marked as one line")
	}
}

func TestSoftWrapAcrossAScrollSharesOneLogical(t *testing.T) {
	screen := New(5, 2, 100)
	feed(screen, "abcdefghijk")
	if screen.Scrollback() != 1 {
		t.Fatalf("scrollback = %d, want the wrapped row", screen.Scrollback())
	}
	logical, ok := screen.LogicalAt(1)
	if !ok {
		t.Fatal("LogicalAt(1) is not ok")
	}
	for index := 0; index < screen.bufferLineCount(); index++ {
		if got := screen.RetainedLogical(index); got != logical {
			t.Fatalf("row %d logical = %d, want the wrapped line %d", index, got, logical)
		}
	}
}

func TestHardNewlineStartsANewLogical(t *testing.T) {
	screen := New(10, 4, 100)
	feed(screen, "one\r\ntwo")

	first, ok := screen.LogicalAt(1)
	if !ok {
		t.Fatal("LogicalAt(1) is not ok")
	}
	second, ok := screen.LogicalAt(2)
	if !ok {
		t.Fatal("LogicalAt(2) is not ok")
	}
	if first == second {
		t.Fatalf("a hard newline kept logical %d", first)
	}

	wrapped := New(5, 4, 100)
	feed(wrapped, "abcdefghij\r\nk")
	continued, ok := wrapped.LogicalAt(2)
	if !ok {
		t.Fatal("LogicalAt(2) after a soft wrap is not ok")
	}
	next, ok := wrapped.LogicalAt(3)
	if !ok {
		t.Fatal("LogicalAt(3) after a hard newline is not ok")
	}
	if continued != wrapped.RetainedLogical(1) || next == continued {
		t.Fatalf("soft wrap logical = %d, hard newline logical = %d; want a shared then a new value", continued, next)
	}
}

func TestReflowPreservesTheLogicalOfTheSameText(t *testing.T) {
	screen := New(10, 5, 100)
	feed(screen, "abcdefghijklmno\r")
	logical, ok := screen.LogicalAt(1)
	if !ok {
		t.Fatal("LogicalAt(1) is not ok")
	}
	if second, ok := screen.LogicalAt(2); !ok || second != logical {
		t.Fatalf("row two logical = %d, %v; want %d, true", second, ok, logical)
	}
	if got := string(screen.AppendRetainedText(nil)); got != "abcdefghijklmno" {
		t.Fatalf("retained text = %q", got)
	}

	for _, width := range []int{5, 10, 5, 20, 10} {
		screen.Resize(width, 5)
		line, ok := screen.FirstLineOfLogical(logical)
		if !ok {
			t.Fatalf("width %d: the logical line vanished", width)
		}
		if line != 1 {
			t.Fatalf("width %d: the logical line starts at %d, want 1", width, line)
		}
		base, ok := screen.RetainedFrom()
		if !ok {
			t.Fatalf("width %d: the retained buffer is empty", width)
		}
		rows := 0
		for index := int(line - base); index < screen.bufferLineCount(); index++ {
			if screen.RetainedLogical(index) != logical {
				break
			}
			rows++
		}
		if want := (len("abcdefghijklmno") + width - 1) / width; rows != want {
			t.Fatalf("width %d: logical line spans %d rows, want %d", width, rows, want)
		}
		if got := string(screen.AppendRetainedText(nil)); got != "abcdefghijklmno" {
			t.Fatalf("width %d: retained text = %q", width, got)
		}
	}
}

func TestRecycleStartsNewLogicalWhileReflowKeepsIt(t *testing.T) {
	screen := New(10, 4, 100)
	recycled := screen.main.lines[2]
	old := recycled.logical
	if old == 0 {
		t.Fatal("a fresh row has no logical identity")
	}
	screen.recycle(recycled)
	if recycled.logical <= old {
		t.Fatalf("recycled logical = %d, want a value above %d", recycled.logical, old)
	}
	if _, ok := screen.FirstLineOfLogical(old); ok {
		t.Fatalf("recycle reused live logical %d", old)
	}

	reflowed := New(10, 5, 100)
	feed(reflowed, strings.Repeat("x", 15))
	before, ok := reflowed.LogicalAt(1)
	if !ok {
		t.Fatal("LogicalAt(1) before a reflow is not ok")
	}
	reflowed.Resize(5, 5)
	after, ok := reflowed.LogicalAt(1)
	if !ok || after != before {
		t.Fatalf("reflow logical = %d, %v; want %d, true", after, ok, before)
	}
	reflowed.Resize(20, 5)
	if got, ok := reflowed.LogicalAt(1); !ok || got != before {
		t.Fatalf("second reflow logical = %d, %v; want %d, true", got, ok, before)
	}
}

func TestReflowEpochTracksOnlyRealResizes(t *testing.T) {
	screen := New(10, 4, 100)
	epoch := screen.ReflowEpoch()
	feed(screen, "hello\r\nworld")
	if got := screen.ReflowEpoch(); got != epoch {
		t.Fatalf("writing changed the reflow epoch from %d to %d", epoch, got)
	}
	screen.Resize(10, 4)
	if got := screen.ReflowEpoch(); got != epoch {
		t.Fatalf("a no-op resize changed the reflow epoch from %d to %d", epoch, got)
	}

	screen.Resize(12, 4)
	first := screen.ReflowEpoch()
	if first == epoch {
		t.Fatalf("a width change kept the reflow epoch at %d", epoch)
	}
	screen.Resize(12, 6)
	second := screen.ReflowEpoch()
	if second == first {
		t.Fatalf("a row change kept the reflow epoch at %d", first)
	}
}

func TestLogicalAccessorsRoundTripRetainedRows(t *testing.T) {
	screen := New(5, 4, 100)
	feed(screen, "abcdefghij\r\nnext\r\nmore\r\nlast")
	if got := screen.Scrollback(); got != 1 {
		t.Fatalf("scrollback = %d, want the wrapped first line plus one history line", got)
	}
	base, ok := screen.RetainedFrom()
	if !ok {
		t.Fatal("the retained buffer is empty")
	}
	shared, ok := screen.LogicalAt(base)
	if !ok {
		t.Fatalf("LogicalAt(%d) is not ok", base)
	}
	if got, ok := screen.LogicalAt(base + 1); !ok || got != shared {
		t.Fatalf("LogicalAt(%d) = %d, %v; want the soft-wrapped logical %d", base+1, got, ok, shared)
	}

	count := screen.bufferLineCount()
	var previous uint64
	for index := 0; index < count; index++ {
		logical := screen.RetainedLogical(index)
		if logical == 0 {
			t.Fatalf("RetainedLogical(%d) = 0", index)
		}
		if index > 0 && logical < previous {
			t.Fatalf("RetainedLogical(%d) = %d follows %d; logical order is not monotonic", index, logical, previous)
		}
		if got, ok := screen.LogicalAt(base + uint64(index)); !ok || got != logical {
			t.Fatalf("LogicalAt(%d) = %d, %v; want %d, true", base+uint64(index), got, ok, logical)
		}
		previous = logical
	}

	if line, ok := screen.FirstLineOfLogical(shared); !ok || line != base {
		t.Fatalf("FirstLineOfLogical(%d) = %d, %v; want %d, true", shared, line, ok, base)
	}
	if line, ok := screen.FirstLineAtOrAfterLogical(shared); !ok || line != base {
		t.Fatalf("FirstLineAtOrAfterLogical(%d) = %d, %v; want %d, true", shared, line, ok, base)
	}
	wantNext := uint64(0)
	for index := 0; index < count; index++ {
		if screen.RetainedLogical(index) > shared {
			wantNext = base + uint64(index)
			break
		}
	}
	if wantNext == 0 {
		t.Fatal("the test buffer has no logical line after the wrapped one")
	}
	if line, ok := screen.FirstLineAtOrAfterLogical(shared + 1); !ok || line != wantNext {
		t.Fatalf("FirstLineAtOrAfterLogical(%d) = %d, %v; want %d, true", shared+1, line, ok, wantNext)
	}
	if _, ok := screen.FirstLineOfLogical(0); ok {
		t.Fatal("FirstLineOfLogical(0) is ok")
	}
	if _, ok := screen.FirstLineAtOrAfterLogical(previous + 1); ok {
		t.Fatal("FirstLineAtOrAfterLogical past the newest line is ok")
	}
	if got := screen.RetainedLogical(-1); got != 0 {
		t.Fatalf("RetainedLogical(-1) = %d", got)
	}
	if got := screen.RetainedLogical(count); got != 0 {
		t.Fatalf("RetainedLogical(count) = %d", got)
	}
	if _, ok := screen.LogicalAt(base - 1); ok {
		t.Fatal("LogicalAt returned a line before the retained buffer")
	}
	if _, ok := screen.LogicalAt(base + uint64(count)); ok {
		t.Fatal("LogicalAt returned a line past the retained buffer")
	}

	current, ok := screen.CurrentLogical()
	if !ok {
		t.Fatal("CurrentLogical is not ok at the live cursor")
	}
	want, wantOK := screen.LogicalAt(screen.LineNumber(screen.Cursor().Y))
	if !wantOK || current != want {
		t.Fatalf("CurrentLogical = %d, %v; want LogicalAt(cursor line) = %d, %v", current, ok, want, wantOK)
	}
}

func TestLogicalIdentityLeavesPhysicalRowsUnchanged(t *testing.T) {
	screen := New(10, 4, 100)
	screen.SetPromptToken("tok")
	feed(screen, "\x1b]133;A;motty=tok\x07$ \x1b]133;B;motty=tok\x07")
	if !screen.AtPrompt() {
		t.Fatal("the prompt marker no longer keeps AtPrompt set")
	}

	feed(screen, "hello\r\nworld")
	if got := screen.RowText(0); got != "$ hello" {
		t.Fatalf("row text = %q, want %q", got, "$ hello")
	}
	if got := screen.RowText(1); got != "world" {
		t.Fatalf("row text = %q, want %q", got, "world")
	}
	if cursor := screen.Cursor(); cursor.X != 5 || cursor.Y != 1 {
		t.Fatalf("cursor = (%d,%d), want (5,1)", cursor.X, cursor.Y)
	}
	if screen.Row(0).ID == 0 || screen.Row(0).ID == screen.Row(1).ID {
		t.Fatalf("physical row ids = %d, %d; want two distinct non-zero ids", screen.Row(0).ID, screen.Row(1).ID)
	}
	if id, ok := screen.LineID(screen.LineNumber(0)); !ok || id != screen.Row(0).ID {
		t.Fatalf("LineID(LineNumber(0)) = %d, %v; want row 0's id %d", id, ok, screen.Row(0).ID)
	}

	seen := make(map[uint64]bool, screen.bufferLineCount())
	for index := 0; index < screen.bufferLineCount(); index++ {
		id := screen.bufferLine(index).id
		if id == 0 || seen[id] {
			t.Fatalf("retained row %d has id %d; want every physical id distinct and non-zero", index, id)
		}
		seen[id] = true
	}

	feed(screen, "\x1b]133;C;motty=tok\x07")
	if screen.AtPrompt() {
		t.Fatal("the command marker no longer clears AtPrompt")
	}
}
