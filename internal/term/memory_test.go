package term

import (
	"runtime"
	"strings"
	"testing"
	"weak"
)

// collected reports whether ref became unreachable. Collection needs more than
// one cycle when a value was still on a stack in an earlier cycle.
func collected[T any](ref weak.Pointer[T]) bool {
	for range 5 {
		runtime.GC()
		if ref.Value() == nil {
			return true
		}
	}
	return false
}

// Growing the window pulls history lines back onto the screen. The line that
// leaves the history ring used to stay referenced by the ring's backing array,
// so a temporary tall window held a whole scrollback's worth of cells that were
// no longer history.
func TestGrowingTheWindowReleasesPoppedHistory(t *testing.T) {
	term := New(40, 5, 100)
	for range 200 {
		term.Write([]byte("a line of output\r\n"))
	}
	newest := term.histAt(term.histCount - 1)
	ref := weak.Make(newest)
	newest = nil
	term.Resize(40, 100)
	if !collected(ref) {
		t.Fatal("a line popped from history is still reachable from the terminal")
	}
	runtime.KeepAlive(term)
}

// A line limit is not a memory limit: at 500 columns the same 5000 lines cost
// several times what they do at 120. The byte budget must give the oldest
// lines back instead of letting a wide terminal grow without bound.
func TestWideHistoryStaysWithinItsByteBudget(t *testing.T) {
	term := New(500, 24, DefaultScrollback)
	term.histBudget = 64 << 10
	line := []byte(strings.Repeat("x", 500) + "\r\n")
	for range 200 {
		term.Write(line)
	}
	checkInvariants(t, term)
	if term.histBytes > term.historyBudget() {
		t.Fatalf("history holds %d bytes, budget %d", term.histBytes, term.historyBudget())
	}
	if lines := term.Scrollback(); lines == 0 || lines >= DefaultScrollback {
		t.Fatalf("scrollback = %d, want some lines but fewer than the line limit", lines)
	}
	// Raising the line limit must not undo the byte bound.
	term.SetScrollbackLimit(2 * DefaultScrollback)
	if term.histBytes > term.historyBudget() || term.Scrollback() > 2*DefaultScrollback {
		t.Fatalf("after raising the limit: %d lines, %d bytes (budget %d)", term.Scrollback(), term.histBytes, term.historyBudget())
	}
}

// Wide desktop terminals (such as 1080p/2K/4K maximized windows) must still
// retain the full configured scrollback count instead of silently dropping
// thousands of lines to a fixed 16 MiB cap.
func TestWideDesktopTerminalsKeepFullConfiguredScrollback(t *testing.T) {
	for _, tc := range []struct {
		name  string
		cols  int
		limit int
	}{
		{"5000_at_240", 240, DefaultScrollback},
		{"5000_at_384", 384, DefaultScrollback},
		{"5000_at_500", 500, DefaultScrollback},
		{"10000_at_240", 240, 10000},
		{"10000_at_384", 384, 10000},
	} {
		t.Run(tc.name, func(t *testing.T) {
			terminal := New(tc.cols, 24, tc.limit)
			line := []byte(strings.Repeat("x", tc.cols) + "\r\n")
			for range tc.limit + 40 {
				terminal.Write(line)
			}
			checkInvariants(t, terminal)
			if got := terminal.Scrollback(); got != tc.limit {
				t.Fatalf("scrollback = %d, want %d (bytes=%d budget=%d)", got, tc.limit, terminal.histBytes, terminal.historyBudget())
			}
			if !terminal.Scroll(tc.limit) || terminal.ViewOffset() != tc.limit {
				t.Fatalf("view offset = %d, want %d", terminal.ViewOffset(), tc.limit)
			}
		})
	}
}

// Choosing a larger scrollback must not silently keep a fraction of it. The
// default budget for the largest offered choices has to cover their full line
// count at the widths that budget represents, or the setting would repeat the
// premature eviction of #1693.
func TestLargeScrollbackChoicesReserveTheirFullLineCount(t *testing.T) {
	for _, limit := range []int{50000, 100000} {
		for _, cols := range []int{240, 600} {
			terminal := New(cols, 24, limit)
			line := []byte(strings.Repeat("x", cols) + "\r\n")
			for range 25 {
				terminal.Write(line)
			}
			if terminal.Scrollback() == 0 {
				t.Fatalf("%d lines at %d columns: no history line to measure", limit, cols)
			}
			cost := lineHistoryBytes(terminal.histAt(0))
			if budget := terminal.historyBudget(); budget < cost*limit {
				t.Fatalf("%d lines at %d columns need %d bytes, budget is %d", limit, cols, cost*limit, budget)
			}
		}
	}
}

// The largest offered choice retains its full count on a normal-width
// terminal, not merely a budget that would allow it.
func TestLargestScrollbackRetainsItsFullLineCount(t *testing.T) {
	terminal := New(120, 24, 100000)
	line := []byte(strings.Repeat("x", 120) + "\r\n")
	for range 100040 {
		terminal.Write(line)
	}
	if got := terminal.Scrollback(); got != 100000 {
		t.Fatalf("scrollback = %d, want 100000 (bytes=%d budget=%d)", got, terminal.histBytes, terminal.historyBudget())
	}
}

// Raising the scrollback limit on an existing terminal with the default budget
// scales its default byte budget too, so changing Settings from 5000 to 10000
// actually lets open wide terminals retain 10000 lines.
func TestSetScrollbackLimitScalesDefaultByteBudget(t *testing.T) {
	terminal := New(240, 24, DefaultScrollback)
	terminal.SetScrollbackLimit(10000)
	line := []byte(strings.Repeat("x", 240) + "\r\n")
	for range 10040 {
		terminal.Write(line)
	}
	checkInvariants(t, terminal)
	if got := terminal.Scrollback(); got != 10000 {
		t.Fatalf("scrollback = %d, want 10000 (bytes=%d budget=%d)", got, terminal.histBytes, terminal.historyBudget())
	}
}

// Widening a terminal reflows wrapped history lines into fewer, wider rows. A
// reflow must not leave those rows holding the capacity they were stitched from,
// which bills the byte budget for cells nobody wrote and evicts lines that fit.
func TestReflowWideningDoesNotInflateHistoryCapacityOrEvictLines(t *testing.T) {
	widen := func(budget int) *Terminal {
		terminal := New(120, 40, DefaultScrollback)
		terminal.histBudget = budget
		line := []byte(strings.Repeat("x", 125) + "\r\n")
		for range 3000 {
			terminal.Write(line)
		}
		terminal.Resize(240, 40)
		checkInvariants(t, terminal)
		return terminal
	}
	// A budget that fits the lines as written; the same history under a budget
	// nothing reaches shows how many lines the line limit alone keeps.
	tight, unlimited := widen(10<<20), widen(1<<30)
	if got, want := tight.Scrollback(), unlimited.Scrollback(); got != want {
		t.Fatalf("a budget that fits the lines kept %d of the %d the line limit allows (bytes=%d budget=%d)", got, want, tight.histBytes, tight.historyBudget())
	}
	if tight.histBytes != tight.historyBytes() {
		t.Fatalf("histBytes = %d, counted %d", tight.histBytes, tight.historyBytes())
	}
	for i := range tight.histCount {
		l := tight.histAt(i)
		if historyCellCapExcess(cap(l.cells), len(l.cells)) {
			t.Fatalf("history line %d keeps capacity %d for %d cells", i, cap(l.cells), len(l.cells))
		}
	}
}

// Hard-ended lines padded with plain spaces must not bill the full terminal
// width to the history budget, while styled spaces, marked spaces, linked
// spaces and trailing spaces on soft-wrapped lines are preserved.
func TestHistoryTrimsPlainTrailingSpacesOnlyWhenSafe(t *testing.T) {
	terminal := New(40, 2, 100)
	// 1. Plain space-padded line -> trimmed to "hello" (5 cells).
	terminal.Write([]byte("hello" + strings.Repeat(" ", 30) + "\r\n"))
	// 2. Background-colored trailing spaces -> preserved.
	terminal.Write([]byte("bg\x1b[44m   \x1b[0m\r\n"))
	// 3. Underlined trailing spaces -> preserved.
	terminal.Write([]byte("ul\x1b[4m   \x1b[0m\r\n"))
	// 4. Combining mark on trailing space -> preserved.
	terminal.Write([]byte("mk \u0301\r\n"))
	// 5. OSC 8 hyperlink on trailing spaces -> preserved.
	terminal.Write([]byte("\x1b]8;;https://example.test\x07lk  \x1b]8;;\x07\r\n"))
	// 6. Soft-wrapped line ending in spaces -> preserved across reflow.
	terminal.Write([]byte(strings.Repeat("a", 38) + "  " + "bb\r\n"))
	// Push all of the above into scrollback.
	terminal.Write([]byte("\r\n\r\n"))
	checkInvariants(t, terminal)

	if got := len(terminal.histAt(0).cells); got != 5 {
		t.Fatalf("plain space-padded line kept %d cells, want 5", got)
	}
	if got := len(terminal.histAt(1).cells); got != 5 {
		t.Fatalf("background-colored trailing spaces kept %d cells, want 5", got)
	}
	if got := len(terminal.histAt(2).cells); got != 5 {
		t.Fatalf("underlined trailing spaces kept %d cells, want 5", got)
	}
	if got := len(terminal.histAt(3).cells); got != 3 || len(terminal.histAt(3).marks) != 1 {
		t.Fatalf("marked trailing space kept %d cells and %d marks, want 3 and 1", len(terminal.histAt(3).cells), len(terminal.histAt(3).marks))
	}
	if got := len(terminal.histAt(4).cells); got != 4 {
		t.Fatalf("linked trailing spaces kept %d cells, want 4", got)
	}
	if got := len(terminal.histAt(5).cells); got != 40 || !terminal.histAt(5).wrapped {
		t.Fatalf("soft-wrapped line kept %d cells (wrapped=%v), want 40 wrapped", got, terminal.histAt(5).wrapped)
	}
	terminal.Resize(80, 2)
	checkInvariants(t, terminal)
	wantJoined := strings.Repeat("a", 38) + "  bb"
	if got := terminal.histAt(5).text(80); got != wantJoined {
		t.Fatalf("reflowed wrapped line = %q, want %q", got, wantJoined)
	}
}

// Reusing a wide history line for shorter output must shed its oversized cell
// and mark backing arrays instead of billing old peak width forever.
func TestHistoryRecyclingReleasesOversizedCapacity(t *testing.T) {
	terminal := New(400, 4, 20)
	wide := []byte("a\u0301" + strings.Repeat("w", 398) + "\r\n")
	for range 30 {
		terminal.Write(wide)
	}
	wideBytes := terminal.histBytes
	short := []byte("short\r\n")
	for range 30 {
		terminal.Write(short)
	}
	checkInvariants(t, terminal)
	if terminal.histBytes >= wideBytes/4 {
		t.Fatalf("histBytes after short lines = %d, still close to wide %d", terminal.histBytes, wideBytes)
	}
	if terminal.histBytes != terminal.historyBytes() {
		t.Fatalf("histBytes = %d, counted %d", terminal.histBytes, terminal.historyBytes())
	}
}
