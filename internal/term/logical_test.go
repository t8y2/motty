package term

import (
	"strings"
	"testing"
)

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
	for index := 1; index < screen.RetainedLines(); index++ {
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
	if continued != wrapped.RetainedLogical(0) || next == continued {
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
		rows := 0
		for index := int(line - screen.RetainedLine(0)); index < screen.RetainedLines(); index++ {
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
