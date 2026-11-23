package term

import (
	"strings"
	"testing"
)

func TestCursorPointDoesNotMoveWithTheView(t *testing.T) {
	screen := New(20, 5, 100)
	screen.Write([]byte(strings.Repeat("line\r\n", 10)))

	bottomPoint, ok := screen.CursorPoint()
	if !ok {
		t.Fatal("CursorPoint on the main screen is not ok")
	}
	if !screen.Scroll(3) {
		t.Fatal("the test terminal has no scrollback to scroll into")
	}
	scrolledPoint, ok := screen.CursorPoint()
	if !ok {
		t.Fatal("CursorPoint while scrolled is not ok")
	}
	if scrolledPoint != bottomPoint {
		t.Fatalf("CursorPoint moved with the view: %+v, want %+v", scrolledPoint, bottomPoint)
	}
	cursor := screen.Cursor()
	if got := screen.PointAt(Position{Row: cursor.Y, Column: cursor.X}); got != scrolledPoint {
		t.Fatalf("PointAt(Cursor) = %+v, want the same point as CursorPoint %+v", got, scrolledPoint)
	}
}

func TestInputStartIsThePromptEndPoint(t *testing.T) {
	screen := New(20, 5, 100)
	screen.SetPromptToken("token")
	screen.Write([]byte("\x1b]133;A;motty=token\x07$ \x1b]133;B;motty=token\x07"))

	start, ok := screen.InputStart()
	if !ok {
		t.Fatal("InputStart after OSC 133 B is not ok")
	}
	if start.Line != 1 || start.Column != 2 {
		t.Fatalf("InputStart = %+v, want line 1 column 2", start)
	}

	screen.Write([]byte("echo hi\r\n\x1b]133;C;motty=token\x07"))
	if _, ok := screen.InputStart(); ok {
		t.Fatal("InputStart survived OSC 133 C")
	}
}

func TestInputStartNeedsTheSessionToken(t *testing.T) {
	screen := New(20, 5, 100)
	screen.Write([]byte("\x1b]133;B\x07"))
	if _, ok := screen.InputStart(); ok {
		t.Fatal("an untokened prompt marker recorded an input start")
	}
}

func TestCursorPointIsUnavailableOnTheAlternateScreen(t *testing.T) {
	screen := New(20, 5, 100)
	screen.Write([]byte("\x1b[?1049h"))
	if _, ok := screen.CursorPoint(); ok {
		t.Fatal("CursorPoint is ok on the alternate screen")
	}
	if _, ok := screen.CurrentLogical(); ok {
		t.Fatal("CurrentLogical is ok on the alternate screen")
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

	count := screen.RetainedLines()
	var previous uint64
	for index := 0; index < count; index++ {
		logical := screen.RetainedLogical(index)
		if logical == 0 {
			t.Fatalf("RetainedLogical(%d) = 0", index)
		}
		if index > 0 && logical < previous {
			t.Fatalf("RetainedLogical(%d) = %d follows %d; logical order is not monotonic", index, logical, previous)
		}
		line := screen.RetainedLine(index)
		if got, ok := screen.LogicalAt(line); !ok || got != logical {
			t.Fatalf("LogicalAt(RetainedLine(%d)) = %d, %v; want %d, true", index, got, ok, logical)
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
			wantNext = screen.RetainedLine(index)
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

	point, ok := screen.CursorPoint()
	if !ok {
		t.Fatal("CursorPoint is not ok")
	}
	current, ok := screen.CurrentLogical()
	if !ok {
		t.Fatal("CurrentLogical is not ok at the live cursor")
	}
	want, wantOK := screen.LogicalAt(point.Line)
	if !wantOK || current != want {
		t.Fatalf("CurrentLogical = %d, %v; want LogicalAt(CursorPoint) = %d, %v", current, ok, want, wantOK)
	}
}

func TestLineIdentityRoundTripsThroughScrollbackAndReflow(t *testing.T) {
	screen := New(20, 5, 100)
	screen.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\n"))

	first, ok := screen.LineIDAt(1)
	if !ok {
		t.Fatal("LineIDAt(1) after the first line scrolled into history is not ok")
	}
	if line, ok := screen.LineOfID(first); !ok || line != 1 {
		t.Fatalf("LineOfID(first) = %d, %v; want 1, true", line, ok)
	}
	if !screen.Scroll(3) {
		t.Fatal("the test terminal has no scrollback to scroll into")
	}
	if line, ok := screen.LineOfID(first); !ok || line != 1 {
		t.Fatalf("LineOfID(first) while scrolled = %d, %v; want 1, true", line, ok)
	}

	screen.Resize(5, 5)
	if line, ok := screen.LineOfID(first); !ok || line != 1 {
		t.Fatalf("LineOfID(first) after reflow = %d, %v; want 1, true", line, ok)
	}
	if _, ok := screen.LineIDAt(1); !ok {
		t.Fatal("LineIDAt(1) after reflow is not ok")
	}
}

func TestLineIdentityStopsAtTheRetainedBuffer(t *testing.T) {
	screen := New(20, 5, 2)
	screen.Write([]byte(strings.Repeat("line\r\n", 10)))

	first, ok := screen.RetainedFrom()
	if !ok || first <= 1 {
		t.Fatalf("RetainedFrom = %d, %v; the test needs dropped lines", first, ok)
	}
	if _, ok := screen.LineIDAt(first - 1); ok {
		t.Fatal("LineIDAt returned a line before the retained buffer")
	}
	if _, ok := screen.LineOfID(0); ok {
		t.Fatal("LineOfID(0) is ok")
	}
}

func TestRetainedRowsCoverScrollbackAndScreen(t *testing.T) {
	screen := New(20, 5, 100)
	screen.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\r\nsix\r\n"))
	total := screen.RetainedLines()
	if total != 7 {
		t.Fatalf("RetainedLines = %d, want the 2 history lines plus the 5 screen rows", total)
	}
	first, ok := screen.RetainedFrom()
	if !ok || first != 1 {
		t.Fatalf("RetainedFrom = %d, %v; want 1, true", first, ok)
	}
	for index := 0; index < total; index++ {
		row, ok := screen.RetainedRow(index)
		if !ok || row.ID == 0 {
			t.Fatalf("RetainedRow(%d) has no identity", index)
		}
		if line := screen.RetainedLine(index); line != first+uint64(index) {
			t.Fatalf("RetainedLine(%d) = %d, want %d", index, line, first+uint64(index))
		}
	}
	if _, ok := screen.RetainedRow(-1); ok {
		t.Fatal("RetainedRow(-1) returned a row")
	}
	if _, ok := screen.RetainedRow(total); ok {
		t.Fatal("RetainedRow(total) returned a row")
	}
}
