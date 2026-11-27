package term

import (
	"math/rand"
	"reflect"
	"strings"
	"testing"
)

func TestLinesKeepTheirWriteTimeIntoScrollback(t *testing.T) {
	screen := New(10, 3, 100)
	now := int64(0)
	screen.SetClock(func() int64 { return now })

	for i, text := range []string{"one\r\n", "two\r\n", "three\r\n", "four\r\n", "five"} {
		now = int64(100 + i)
		screen.Write([]byte(text))
	}
	// "one" and "two" scrolled off; the view shows three, four, five.
	if screen.Scrollback() != 2 {
		t.Fatalf("scrollback = %d, want 2", screen.Scrollback())
	}
	if got := screen.Row(0).Written; got != 102 {
		t.Fatalf("row 0 written = %d, want 102", got)
	}
	screen.Scroll(2)
	for i, want := range []int64{100, 101, 102} {
		if got := screen.Row(i).Written; got != want {
			t.Fatalf("history view row %d written = %d, want %d (%q)", i, got, want, screen.RowText(i))
		}
		if got := screen.LineNumber(i); got != uint64(i+1) {
			t.Fatalf("row %d number = %d, want %d", i, got, i+1)
		}
	}
}

// A full-screen program on the main screen (top does not use the alternate screen) repaints its rows in place.
// The time must follow the content, not the row's first appearance.
func TestRewritingALineMovesItsWriteTime(t *testing.T) {
	screen := New(10, 3, 10)
	now := int64(100)
	screen.SetClock(func() int64 { return now })
	screen.Write([]byte("one\r\ntwo\r\n"))

	now = 200
	screen.Write([]byte("\x1b[HONE"))
	if got := screen.Row(0).Written; got != 200 {
		t.Fatalf("rewritten row written = %d, want 200", got)
	}
	if got := screen.Row(1).Written; got != 100 {
		t.Fatalf("untouched row written = %d, want 100", got)
	}
}

func TestErasingALineMovesItsWriteTime(t *testing.T) {
	screen := New(10, 3, 10)
	now := int64(100)
	screen.SetClock(func() int64 { return now })
	screen.Write([]byte("one\r\n"))

	now = 200
	screen.Write([]byte("\x1b[H\x1b[K"))
	if got := screen.Row(0).Written; got != 200 {
		t.Fatalf("erased row written = %d, want 200", got)
	}
}

func TestErasingAnUnwrittenLineLeavesItUnstamped(t *testing.T) {
	screen := New(10, 3, 10)
	screen.SetClock(func() int64 { return 200 })
	screen.Write([]byte("\x1b[H\x1b[2J"))
	if got := screen.Row(0).Written; got != 0 {
		t.Fatalf("unwritten row written = %d, want 0", got)
	}
}

// The clock is asked once per Write, not once per line or character: the write
// paths stamp every run of text they print.
func TestClockIsAskedOncePerWrite(t *testing.T) {
	screen := New(20, 4, 10)
	calls := 0
	screen.SetClock(func() int64 { calls++; return 100 })
	screen.Write([]byte("one\r\ntwo\r\nthree\x1b[H\x1b[K"))
	if calls != 1 {
		t.Fatalf("clock calls = %d for one write, want 1", calls)
	}
}

func TestLineNumbersSurviveDroppedHistory(t *testing.T) {
	screen := New(10, 3, 2)
	for range 20 {
		screen.Write([]byte("x\r\n"))
	}
	// 20 lines were written, the screen holds the last 2 plus the blank cursor row.
	if got := screen.LineNumber(2); got != 21 {
		t.Fatalf("cursor row number = %d, want 21", got)
	}
	screen.Scroll(2)
	if got := screen.LineNumber(0); got != 17 {
		t.Fatalf("oldest retained line number = %d, want 17", got)
	}
}

func TestNoClockRecordsNothing(t *testing.T) {
	screen := New(10, 3, 10)
	screen.Write([]byte("hello\r\n"))
	if got := screen.Row(0).Written; got != 0 {
		t.Fatalf("written = %d without a clock", got)
	}
}

// The gutter numbers logical lines: a soft-wrapped run is one line, numbered
// on the row that begins it, and a resize that rewraps it into more or fewer
// rows moves no number.
func TestLogicalLineNumbersSkipWrappedRows(t *testing.T) {
	screen := New(10, 4, 20)
	screen.Write([]byte("0123456789ABCDEF\r\nsecond\r\nthird"))
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if !reflect.DeepEqual(numbers, []uint64{1, 1, 2, 3}) {
		t.Fatalf("numbers = %v, want [1 1 2 3]", numbers)
	}
	if !reflect.DeepEqual(heads, []bool{true, false, true, true}) {
		t.Fatalf("heads = %v, want [true false true true]", heads)
	}
}

func TestResizeKeepsLogicalLineNumbers(t *testing.T) {
	screen := New(12, 4, 20)
	screen.Write([]byte("0123456789ABCDEF\r\nsecond\r\nthird"))
	before, beforeHeads := screen.LogicalLineNumbers(nil, nil)
	if !reflect.DeepEqual(before, []uint64{1, 1, 2, 3}) {
		t.Fatalf("numbers = %v, want [1 1 2 3]", before)
	}
	if !reflect.DeepEqual(beforeHeads, []bool{true, false, true, true}) {
		t.Fatalf("heads = %v, want [true false true true]", beforeHeads)
	}

	// Narrowing rewraps the first line into four rows. The view follows the
	// cursor, so it is the line's tail that leaves the screen; scrolled to the
	// oldest retained line, every visible row of the wrapped line carries the
	// line's number and only its first row begins it.
	screen.Resize(5, 4)
	screen.Scroll(100)
	narrow, narrowHeads := screen.LogicalLineNumbers(before, beforeHeads)
	if !reflect.DeepEqual(narrow, []uint64{1, 1, 1, 1}) {
		t.Fatalf("numbers at width 5 = %v, want [1 1 1 1]", narrow)
	}
	if !reflect.DeepEqual(narrowHeads, []bool{true, false, false, false}) {
		t.Fatalf("heads at width 5 = %v, want only the first row to begin the line", narrowHeads)
	}
	screen.Scroll(-2)
	if middle, middleHeads := screen.LogicalLineNumbers(narrow, narrowHeads); !reflect.DeepEqual(middle, []uint64{1, 1, 2, 2}) || !reflect.DeepEqual(middleHeads, []bool{false, false, true, false}) {
		t.Fatalf("numbers across the boundary = %v with heads %v, want [1 1 2 2] with [false false true false]", middle, middleHeads)
	}
	screen.ScrollToBottom()
	if bottom, _ := screen.LogicalLineNumbers(narrow, narrowHeads); !reflect.DeepEqual(bottom, []uint64{2, 2, 3, 3}) {
		t.Fatalf("numbers at the cursor = %v, want [2 2 3 3]", bottom)
	}

	screen.Resize(12, 4)
	wide, wideHeads := screen.LogicalLineNumbers(narrow, narrowHeads)
	if !reflect.DeepEqual(wide, before) || !reflect.DeepEqual(wideHeads, beforeHeads) {
		t.Fatalf("numbers after widening = %v with heads %v, want %v with heads %v", wide, wideHeads, before, beforeHeads)
	}
}

func TestResizeKeepsWrittenTimes(t *testing.T) {
	screen := New(12, 4, 20)
	now := int64(100)
	screen.SetClock(func() int64 { return now })
	screen.Write([]byte("0123456789ABCDEF\r\nsecond"))

	now = 200
	screen.Resize(5, 4)
	for row := range 4 {
		if got := screen.Row(row).Written; got != 100 {
			t.Fatalf("row %d written = %d after the rewrap, want 100", row, got)
		}
	}

	now = 300
	screen.Resize(12, 4)
	// The fourth row was never written to, before or after.
	for row, want := range []int64{100, 100, 100, 0} {
		if got := screen.Row(row).Written; got != want {
			t.Fatalf("row %d written = %d after rejoining, want %d", row, got, want)
		}
	}
}

// A shell redraws its prompt after a resize. The display copy the resize leaves
// in place keeps the time the prompt appeared at, so the resize itself does not
// stamp it; the shell's redraw is a write, so it takes the redraw's time.
func TestResizeRedrawStampsThePromptTime(t *testing.T) {
	screen := New(10, 3, 10)
	now := int64(100)
	screen.SetClock(func() int64 { return now })
	screen.SetPromptToken("t")
	screen.Write([]byte("\x1b]133;A;motty=t\x07$ "))
	if got := screen.Row(0).Written; got != 100 {
		t.Fatalf("prompt written = %d, want 100", got)
	}

	now = 200
	screen.Resize(4, 3)
	if got := screen.Row(0).Written; got != 100 {
		t.Fatalf("prompt copy written = %d after the resize, want 100", got)
	}

	now = 300
	screen.Write([]byte("\x1b[H$ >"))
	if got := screen.Row(0).Written; got != 300 {
		t.Fatalf("prompt written = %d after the shell's redraw, want 300", got)
	}
}

// Numbers keep counting past dropped history, wrapped lines included: the two
// dropped rows here are a line's head and one of its continuations, so an
// undercount would move every number after them.
func TestLogicalLineNumbersKeepCountingPastDroppedHistory(t *testing.T) {
	screen := New(8, 3, 3)
	screen.Write([]byte(strings.Repeat("a", 17) + "\r\none\r\ntwo\r\nthree\r\nfour\r\nfive"))
	// Line 1 wraps over three rows. The ring dropped its first two rows;
	// the screen shows lines 4 to 6.
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if want := []uint64{4, 5, 6}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers at the cursor = %v, want %v", numbers, want)
	}
	if want := []bool{true, true, true}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads at the cursor = %v, want %v", heads, want)
	}

	// Scrolled to the oldest retained row:
	// it carries the dropped line's number and begins nothing of its own.
	screen.Scroll(100)
	numbers, heads = screen.LogicalLineNumbers(numbers, heads)
	if want := []uint64{1, 2, 3}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers at the oldest history = %v, want %v", numbers, want)
	}
	if want := []bool{false, true, true}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads at the oldest history = %v, want %v", heads, want)
	}

	screen.Scroll(-1)
	if numbers, _ = screen.LogicalLineNumbers(numbers, heads); !reflect.DeepEqual(numbers, []uint64{2, 3, 4}) {
		t.Fatalf("numbers across the boundary = %v, want [2 3 4]", numbers)
	}
}

// Clearing the scrollback drops lines too.
// The rows left on the screen keep their numbers, and new output keeps counting from them.
func TestLogicalLineNumbersKeepCountingPastClearedHistory(t *testing.T) {
	screen := New(8, 3, 3)
	screen.Write([]byte(strings.Repeat("a", 17) + "\r\none\r\ntwo\r\nthree\r\nfour\r\nfive"))
	screen.ClearScrollback()
	// Lines 1 to 3 left with the history; the screen still shows lines 4 to 6.
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if want := []uint64{4, 5, 6}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers after the clear = %v, want %v", numbers, want)
	}
	if want := []bool{true, true, true}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads after the clear = %v, want %v", heads, want)
	}

	screen.Write([]byte("\r\nsix"))
	if numbers, _ = screen.LogicalLineNumbers(numbers, heads); !reflect.DeepEqual(numbers, []uint64{5, 6, 7}) {
		t.Fatalf("numbers after new output = %v, want [5 6 7]", numbers)
	}
}

// A reflow can leave more rows than the history holds, so refillHistory drops the oldest ones.
// Here it drops line 1 entirely; the lines after it must keep their numbers.
func TestLogicalLineNumbersKeepCountingPastReflowedHistory(t *testing.T) {
	screen := New(10, 2, 2)
	screen.Write([]byte("0123456789abc\r\nsecond\r\nthird"))
	// Line 1 wraps over two history rows, then "second" and "third" are on the screen.
	numbers, _ := screen.LogicalLineNumbers(nil, nil)
	if want := []uint64{2, 3}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers before the resize = %v, want %v", numbers, want)
	}

	// Narrowing rewraps line 1 into four rows, which the two-line history drops;
	// the two rows kept are line 2, and line 3 stays on the screen.
	screen.Resize(4, 2)
	if got := screen.Scrollback(); got != 2 {
		t.Fatalf("scrollback = %d after the reflow, want 2", got)
	}
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if want := []uint64{3, 3}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers after the reflow = %v, want %v", numbers, want)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads after the reflow = %v, want %v", heads, want)
	}

	// The oldest retained rows are line 2's: dropping line 1's rows, head included, must not shift them.
	screen.Scroll(100)
	numbers, heads = screen.LogicalLineNumbers(numbers, heads)
	if want := []uint64{2, 2}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers in the reflowed history = %v, want %v", numbers, want)
	}
	if want := []bool{true, false}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads in the reflowed history = %v, want %v", heads, want)
	}

	screen.Scroll(-1)
	numbers, heads = screen.LogicalLineNumbers(numbers, heads)
	if want := []uint64{2, 3}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers across the boundary = %v, want %v", numbers, want)
	}
	if want := []bool{false, true}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads across the boundary = %v, want %v", heads, want)
	}
}

// Lowering the scrollback limit drops lines too; the rows that stay keep the numbers they had.
func TestLogicalLineNumbersKeepCountingWhenTheScrollbackLimitShrinks(t *testing.T) {
	screen := New(8, 3, 3)
	screen.Write([]byte(strings.Repeat("a", 17) + "\r\none\r\ntwo\r\nthree\r\nfour\r\nfive"))
	screen.SetScrollbackLimit(1)
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if want := []uint64{4, 5, 6}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers after shrinking the limit = %v, want %v", numbers, want)
	}
	if want := []bool{true, true, true}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads after shrinking the limit = %v, want %v", heads, want)
	}

	screen.Scroll(100)
	if numbers, _ = screen.LogicalLineNumbers(numbers, heads); !reflect.DeepEqual(numbers, []uint64{3, 4, 5}) {
		t.Fatalf("numbers in the shortened history = %v, want [3 4 5]", numbers)
	}
}

// With no scrollback every line is dropped as it leaves the screen;
// the numbers must still count a wrapped line once.
func TestLogicalLineNumbersKeepCountingWithoutScrollback(t *testing.T) {
	screen := New(8, 3, 0)
	screen.Write([]byte(strings.Repeat("a", 17) + "\r\none\r\ntwo"))
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if want := []uint64{1, 2, 3}; !reflect.DeepEqual(numbers, want) {
		t.Fatalf("numbers = %v, want %v", numbers, want)
	}
	if want := []bool{false, true, true}; !reflect.DeepEqual(heads, want) {
		t.Fatalf("heads = %v, want %v", heads, want)
	}
}

// slowLogicalLineNumbers counts the buffer from the start, the way the gutter did before LogicalLineNumbers cached a row.
// It is the reference the cache must keep agreeing with as output, scrolling and resizing move the buffer.
func slowLogicalLineNumbers(t *Terminal) (numbers []uint64, heads []bool) {
	numbers, heads = make([]uint64, t.rows), make([]bool, t.rows)
	if t.onAlt {
		for row := range numbers {
			numbers[row], heads[row] = t.LineNumber(row), true
		}
		return numbers, heads
	}
	count := t.scrolled - uint64(t.histCount) - t.droppedContinuations
	top := t.histCount - t.view
	for i := 0; i <= top+t.rows-1; i++ {
		head := i == 0 && !t.droppedWrapped || i > 0 && !t.bufferLine(i-1).wrapped
		if head {
			count++
		}
		if row := i - top; row >= 0 {
			numbers[row], heads[row] = count, head
		}
	}
	return numbers, heads
}

// The cached row number is an optimization:
// it must agree with counting the buffer from the start whatever moved it between frames.
func TestLogicalLineNumbersMatchACountFromTheStart(t *testing.T) {
	rng := rand.New(rand.NewSource(1))
	output := []string{
		"short\r\n",
		strings.Repeat("x", 37) + "\r\n",
		strings.Repeat("y", 53),
		"\x1b[2J",
		"\x1b[3J",
		"line\r\n",
		"\x1b[H",
		"\x1b[2;1H",
		"\x1b[?1049h",
		"\x1b[?1049l",
	}
	screen := New(12, 4, 6)
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	for step := 0; step < 400; step++ {
		switch choice := rng.Intn(11); {
		case choice < 5:
			screen.Write([]byte(output[rng.Intn(len(output))]))
		case choice == 5:
			screen.Scroll(rng.Intn(5) - 2)
		case choice == 6:
			screen.ScrollToBottom()
		case choice == 7:
			screen.Resize(4+rng.Intn(20), 1+rng.Intn(6))
		case choice == 8:
			if rng.Intn(3) == 0 {
				screen.ClearScrollback()
			}
		case choice == 9:
			if rng.Intn(3) == 0 {
				screen.SetScrollbackLimit(rng.Intn(8))
			}
		case choice == 10:
			if rng.Intn(4) == 0 {
				screen.Reset()
			}
		}
		numbers, heads = screen.LogicalLineNumbers(numbers, heads)
		wantNumbers, wantHeads := slowLogicalLineNumbers(screen)
		if !reflect.DeepEqual(numbers, wantNumbers) || !reflect.DeepEqual(heads, wantHeads) {
			t.Fatalf("step %d: numbers %v heads %v, want %v %v", step, numbers, heads, wantNumbers, wantHeads)
		}
	}
}

// The gutter numbers every visible row on every frame.
// With the scrollback full, a frame must cost the rows it shows, not the lines kept:
// both the live screen and a view scrolled back into the history are measured.
func BenchmarkLogicalLineNumbersLargeScrollback(b *testing.B) {
	screen := New(120, 40, DefaultScrollback)
	for line := 0; line < DefaultScrollback+40; line++ {
		screen.Write([]byte(itoa(line) + " output line\r\n"))
	}
	numbers, heads := make([]uint64, 40), make([]bool, 40)
	b.Run("live", func(b *testing.B) {
		b.ReportAllocs()
		for b.Loop() {
			numbers, heads = screen.LogicalLineNumbers(numbers, heads)
		}
	})
	b.Run("scrolled", func(b *testing.B) {
		screen.Scroll(DefaultScrollback)
		b.ReportAllocs()
		for b.Loop() {
			numbers, heads = screen.LogicalLineNumbers(numbers, heads)
		}
	})
}

// Clearing the screen drops the history, and the row the cursor stands on
// becomes the first row. It begins a new line even when the history's last row
// had soft-wrapped into it, so it keeps its number and time.
func TestClearScreenAfterAWrappedHistoryBoundaryKeepsThePromptNumbered(t *testing.T) {
	screen := New(10, 4, 20)
	screen.Write([]byte(strings.Repeat("x", 55) + "\r\nprompt$ "))
	if !screen.ClearScreen() {
		t.Fatal("ClearScreen did nothing on the main screen")
	}
	numbers, heads := screen.LogicalLineNumbers(nil, nil)
	if !heads[0] {
		t.Fatalf("the prompt row lost its line number: numbers %v, heads %v", numbers, heads)
	}
	if numbers[0] != 2 {
		t.Fatalf("prompt number = %d, want 2 (the cleared line was number 1)", numbers[0])
	}
}
