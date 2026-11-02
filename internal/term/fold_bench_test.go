package term

import (
	"fmt"
	"strings"
	"testing"
)

// blockHistory is a terminal holding about tens of thousands of lines of history made of many
// short commands, with some of their blocks folded.
func blockHistory(b *testing.B, folded int) *Terminal {
	b.Helper()
	screen := New(80, 40, 100_000)
	screen.SetFolding(true)
	var out strings.Builder
	for i := range 2000 {
		fmt.Fprintf(&out, "\x1b]133;A\x07$ \x1b]133;B\x07cmd %d\r\n\x1b]133;C\x07", i)
		for j := range 49 {
			fmt.Fprintf(&out, "output %d of %d\r\n", j, i)
		}
	}
	out.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07")
	screen.Write([]byte(out.String()))
	if screen.Scrollback() < 40_000 {
		b.Fatalf("history holds %d lines", screen.Scrollback())
	}
	// Fold blocks while looking at them in the history.
	screen.Scroll(screen.Scrollback() / 2)
	for done, row := 0, 0; done < folded && row < 40; row++ {
		if screen.FoldAt(row).Anchor {
			screen.ToggleFold(row)
			done++
		}
	}
	return screen
}

// BenchmarkViewRows is what a change of the buffer costs the view of a terminal
// holding tens of thousands of lines of history: the view is rebuilt on the first read after
// each Write.
func BenchmarkViewRows(b *testing.B) {
	for _, tc := range []struct {
		name   string
		folded int
		middle bool // look at the middle of the history, not the live screen
	}{
		{"live/no folds", 0, false},
		{"live/folds in history", 5, false},
		{"history/no folds", 0, true},
		{"history/5 folded", 5, true},
	} {
		b.Run(tc.name, func(b *testing.B) {
			screen := blockHistory(b, tc.folded)
			if tc.middle {
				screen.Scroll(screen.Scrollback()/2 - screen.ViewOffset())
			} else {
				screen.ScrollToBottom()
			}
			b.ReportAllocs()
			b.ResetTimer()
			for range b.N {
				screen.version++
				screen.viewRows()
			}
		})
	}
}

// BenchmarkLongOutputBlock is the worst case for finding where the view starts
// inside a block: one command that printed 1e5 lines, viewed from its middle.
func BenchmarkLongOutputBlock(b *testing.B) {
	screen := New(80, 40, 100_000)
	screen.SetFolding(true)
	var out strings.Builder
	out.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07seq\r\n\x1b]133;C\x07")
	for i := range 99_000 {
		fmt.Fprintf(&out, "%d\r\n", i)
	}
	out.WriteString("\x1b]133;A\x07$ \x1b]133;B\x07")
	screen.Write([]byte(out.String()))
	screen.Scroll(screen.Scrollback() / 2)
	b.ReportAllocs()
	b.ResetTimer()
	for range b.N {
		screen.version++
		screen.viewRows()
	}
}
