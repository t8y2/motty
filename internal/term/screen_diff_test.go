package term

import (
	"slices"
	"strings"
	"testing"
)

// assertScreensEqual proves the display the diff was applied to shows the
// target: every cell and mark, the cursor, the modes and the title.
func assertScreensEqual(t *testing.T, got, want *Terminal) {
	t.Helper()
	gotCols, gotRows := got.Size()
	wantCols, wantRows := want.Size()
	if gotCols != wantCols || gotRows != wantRows {
		t.Fatalf("size %dx%d, want %dx%d", gotCols, gotRows, wantCols, wantRows)
	}
	for y := 0; y < wantRows; y++ {
		gotRow, wantRow := got.Row(y), want.Row(y)
		if !slices.Equal(gotRow.Cells, wantRow.Cells) {
			t.Fatalf("row %d cells differ:\n got: %q\nwant: %q", y, got.RowText(y), want.RowText(y))
		}
		if !slices.Equal(gotRow.Marks, wantRow.Marks) {
			t.Fatalf("row %d marks differ: %v, want %v", y, gotRow.Marks, wantRow.Marks)
		}
	}
	if got.Cursor() != want.Cursor() {
		t.Fatalf("cursor %+v, want %+v", got.Cursor(), want.Cursor())
	}
	if got.Modes() != want.Modes() {
		t.Fatalf("modes %+v, want %+v", got.Modes(), want.Modes())
	}
	if got.Title() != want.Title() {
		t.Fatalf("title %q, want %q", got.Title(), want.Title())
	}
}

// applyDiff returns a terminal that showed from and then the diff's bytes.
func applyDiff(from *Terminal, diff []byte) *Terminal {
	display := from.Clone()
	display.Write(diff)
	return display
}

func TestScreenDiffTurnsOneScreenIntoAnother(t *testing.T) {
	from := New(40, 8, 0)
	from.Write([]byte("hello \x1b[1;31mred\x1b[0m\r\nworld \x1b[38;5;200mindexed\x1b[0m\r\n"))
	from.Write([]byte("\x1b[32m你好 wide\x1b[0m e\xcc\x81\r\n"))
	to := from.Clone()
	to.Write([]byte("\x1b[2;2Hchanged"))
	to.Write([]byte("\r\nnew line"))
	to.Write([]byte("\x1b[?2004h\x1b[?1h\x1b[?25l\x1b[?1002h\x1b[?1006h"))
	to.Write([]byte("\x1b]0;title\x07"))

	diff := ScreenDiff(from, to)
	if strings.Contains(string(diff), "\x1b[2J") {
		t.Fatalf("a screen with content in place was repainted in full: %q", diff)
	}
	assertScreensEqual(t, applyDiff(from, diff), to)
}

func TestScreenDiffRepaintsWhatTheOwnerAlreadyDrew(t *testing.T) {
	display := New(20, 5, 0)
	display.Write([]byte("connecting...\r\n"))
	to := New(20, 5, 0)
	to.Write([]byte("prompt $ ls\r\nfile.txt\r\n"))

	diff := ScreenDiff(nil, to)
	if !strings.Contains(string(diff), "\x1b[2J") {
		t.Fatalf("the first frame did not clear the owner's text: %q", diff)
	}
	display.Write(diff)
	assertScreensEqual(t, display, to)
}

// A screen that moved up is advanced with newlines, not repainted cell by cell,
// so the lines that left the top reach the terminal's scrollback.
func TestScreenDiffAdvancesScrollback(t *testing.T) {
	from := New(24, 4, 0)
	from.Write([]byte("one\r\ntwo\r\nthree\r\nfour"))
	display := New(24, 4, 8)
	display.Write(ScreenDiff(nil, from))
	assertScreensEqual(t, display, from)

	to := from.Clone()
	// The cursor is on the last row; a newline scrolls the whole screen.
	to.Write([]byte("\r\nfive"))

	display.Write(ScreenDiff(from, to))
	assertScreensEqual(t, display, to)
	if !display.Scroll(1) {
		t.Fatal("the scrolled-off line did not enter the scrollback")
	}
	defer display.Scroll(-1)
	if got := strings.TrimRight(display.RowText(0), " "); got != "one" {
		t.Fatalf("scrollback top = %q, want %q", got, "one")
	}
}

func TestScreenDiffHandlesResize(t *testing.T) {
	from := New(30, 6, 0)
	from.Write([]byte("before resize\r\nsecond line"))
	to := from.Clone()
	to.Resize(16, 3)
	to.Write([]byte("\x1b[2;1Hafter resize"))

	// The owner resizes its own terminal; the diff paints the new-size screen.
	display := from.Clone()
	display.Resize(16, 3)
	display.Write(ScreenDiff(from, to))
	assertScreensEqual(t, display, to)
}

func TestScreenDiffLetsAProgramStyleTheCursor(t *testing.T) {
	from := New(10, 3, 0)
	to := from.Clone()
	to.Write([]byte("\x1b[4 q"))
	display := applyDiff(from, ScreenDiff(from, to))
	assertScreensEqual(t, display, to)
	if got := display.Cursor(); got.Style != CursorUnderline || !got.Steady {
		t.Fatalf("cursor %+v, want a steady underline", got)
	}
}

func TestCloneIsIndependent(t *testing.T) {
	original := New(12, 4, 0)
	original.Write([]byte("\x1b]0;first\x07one two\r\nthree"))
	clone := original.Clone()

	original.Write([]byte("\x1b[1;1HX\x1b]0;second\x07"))
	if got := clone.RowText(0); got != "one two" {
		t.Fatalf("clone row changed with the original: %q", got)
	}
	if clone.Title() != "first" {
		t.Fatalf("clone title = %q, want %q", clone.Title(), "first")
	}

	clone.Write([]byte("\x1b[2;1HY"))
	if got := original.RowText(1); got != "three" {
		t.Fatalf("original row changed with the clone: %q", got)
	}
}

// A parser in the middle of an escape sequence is part of the state: the clone
// must continue the sequence from where the original stopped.
func TestCloneKeepsAPartialSequence(t *testing.T) {
	original := New(20, 3, 0)
	original.Write([]byte("before \x1b[38;5;"))
	clone := original.Clone()
	original.Write([]byte("200mafter"))
	clone.Write([]byte("200mafter"))
	assertScreensEqual(t, clone, original)
}

// A screen that scrolled while the cursor was not on its last row uses a
// scrolling region rather than the whole screen's carriage return and newlines.
func TestScreenDiffScrollsARegionAwayFromTheBottom(t *testing.T) {
	from := New(20, 5, 0)
	from.Write([]byte("one\r\ntwo\r\nthree\r\nfour\r\nfive\x1b[2;1H"))
	to := from.Clone()
	// Move to the last row, scroll the screen one line, and put the cursor back
	// on row 2 so the fast whole-screen path cannot be used.
	to.Write([]byte("\x1b[5;1H\n\x1b[2;6Hsix"))
	display := New(20, 5, 4)
	display.Write(ScreenDiff(nil, from))

	display.Write(ScreenDiff(from, to))
	assertScreensEqual(t, display, to)
	if !display.Scroll(1) {
		t.Fatal("the scrolled-off line did not enter the scrollback")
	}
	defer display.Scroll(-1)
	if got := strings.TrimRight(display.RowText(0), " "); got != "one" {
		t.Fatalf("scrollback top = %q, want %q", got, "one")
	}
}

// Cells with one rendition are written as one run, so text a reader or a test
// searches for stays contiguous.
func TestScreenDiffWritesARunContiguously(t *testing.T) {
	from := New(40, 3, 0)
	to := from.Clone()
	to.Write([]byte("\x1b[1;31mred red red\x1b[0m plain text"))
	diff := ScreenDiff(from, to)
	if !strings.Contains(string(diff), "red red red") || !strings.Contains(string(diff), "plain text") {
		t.Fatalf("styled runs are split per cell: %q", diff)
	}
}

func BenchmarkScreenDiffOneCharacter(b *testing.B) {
	from := New(80, 24, 0)
	from.Write([]byte("prompt $ some command output with \x1b[1;32mcolor\x1b[0m\r\n"))
	to := from.Clone()
	to.Write([]byte("x"))
	b.ReportAllocs()
	for i := 0; i < b.N; i++ {
		_ = ScreenDiff(from, to)
	}
}

// A history ring is allocated with its unused slots nil; cloning one must keep
// those slots and copy the lines it holds.
func TestCloneCopiesAHistoryRing(t *testing.T) {
	original := New(12, 2, 5)
	original.Write([]byte("one\r\ntwo\r\nthree\r\nfour"))
	clone := original.Clone()
	if !clone.Scroll(1) {
		t.Fatal("the clone has no history")
	}
	if got := strings.TrimRight(clone.RowText(0), " "); got == "" {
		t.Fatalf("clone history top is empty")
	}
	original.Scroll(-original.ViewOffset())
	clone.Scroll(-clone.ViewOffset())
	assertScreensEqual(t, clone, original)
}
