package workbench

import (
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/highlight"
	"motty/internal/term"
)

func TestWrappedHighlightKeepsLongURLContext(t *testing.T) {
	terminal := newTerminalWithScrollback(10, 3, 256)
	terminal.screen.Write([]byte("https://" + strings.Repeat("1234567890", 90)))
	look := darkPalette
	cyan := highlight.RGB(0, 208, 208)
	look.highlight = highlight.Compile([]highlight.Rule{
		{Pattern: `https://[^\s]+`, Regex: true, Color: cyan},
		{Pattern: `\d+`, Regex: true, Color: highlight.Red},
	})
	terminal.cache.begin()
	terminal.prepareLogicalLines()
	logical := terminal.presentedLogicalLine(0)
	if len(logical) <= 64 {
		t.Fatalf("expected the whole long logical line, got %d rows", len(logical))
	}
	entry := terminal.cache.rowWithLogical(terminal.presentedRow(0), 10, -1, cellSpan{}, &look, false, logical, false)
	if len(entry.runs) == 0 || entry.runs[0].Color != gfx.RGB(0, 208, 208) {
		t.Fatalf("URL suffix was not colored as a URL: %+v", entry.runs)
	}
	warm := func() {
		terminal.cache.begin()
		terminal.prepareLogicalLines()
		for row := 0; row < 3; row++ {
			terminal.cache.rowWithLogical(terminal.presentedRow(row), 10, -1, cellSpan{}, &look, false, terminal.presentedLogicalLine(row), terminal.logicalView[row].suppressed)
		}
		terminal.cache.end(3)
	}
	warm()
	if allocations := testing.AllocsPerRun(10, warm); allocations != 0 {
		t.Fatalf("steady wrapped-line paint preparation allocated %v times", allocations)
	}
}

func TestWrappedHighlightIncludesCombiningMarksAndWrittenSpaces(t *testing.T) {
	for _, test := range []struct {
		name, text, pattern string
		cols                int
		firstColumn         int
	}{
		{"combining mark", "12e\u0301X", "e\u0301X", 3, 2},
		{"written space", "abc def", "abc def", 4, 0},
		{"wide wrap", "AB 界", "AB 界", 4, 0},
	} {
		t.Run(test.name, func(t *testing.T) {
			screen := term.New(test.cols, 3, 0)
			screen.Write([]byte(test.text))
			logical := screen.LogicalLineRows(0)
			if len(logical) != 2 {
				t.Fatalf("logical rows = %d, want two", len(logical))
			}
			var cache rowCache
			cache.begin()
			rules := highlight.Compile([]highlight.Rule{{Pattern: test.pattern, Color: highlight.Red}})
			group := cache.logicalLine(logical, test.cols, 0, rules)
			if len(group.cells) == 0 || group.cells[test.firstColumn] != highlight.Red || group.cells[test.cols] != highlight.Red {
				t.Fatalf("cross-row match lost text: cells=%v", group.cells)
			}
		})
	}
}

func TestWrappedHighlightSurvivesSynchronizedUpdateOutsideViewport(t *testing.T) {
	terminal := newTerminalWithScrollback(10, 3, 256)
	terminal.screen.Write([]byte("https://" + strings.Repeat("1234567890", 80)))
	if !terminal.screen.Scroll(20) {
		t.Fatal("could not scroll into the logical line")
	}
	terminal.screen.SetSyncHold(terminal.holdSync)
	terminal.screen.Write([]byte("\x1b[?2026h\x1b[2J\x1b[Hupdated"))
	if terminal.sync == nil || terminal.sync.visible == 0 || len(terminal.sync.lines) <= terminal.sync.visible+terminal.sync.rows {
		t.Fatal("the frozen frame did not retain context on both sides of the viewport")
	}
	look := darkPalette
	look.highlight = highlight.Compile([]highlight.Rule{
		{Pattern: `https://[^\s]+`, Regex: true, Color: highlight.Cyan},
		{Pattern: `\d+`, Regex: true, Color: highlight.Red},
	})
	terminal.cache.begin()
	terminal.prepareLogicalLines()
	entry := terminal.cache.rowWithLogical(terminal.presentedRow(0), 10, -1, cellSpan{}, &look, false, terminal.presentedLogicalLine(0), terminal.logicalView[0].suppressed)
	if len(entry.runs) == 0 || entry.runs[0].Color != look.highlightColor(highlight.Cyan, false) {
		t.Fatalf("the frozen URL lost its highlight: %+v", entry.runs)
	}
	terminal.holdSync(false)
	terminal.holdSync(true)
	terminal.holdSync(false)
	if allocations := testing.AllocsPerRun(10, func() {
		terminal.holdSync(true)
		terminal.holdSync(false)
	}); allocations != 0 {
		t.Fatalf("a warm snapshot of a wrapped line allocated %v times", allocations)
	}
}

func BenchmarkWrappedHighlightSteadyPaint(b *testing.B) {
	terminal := newTerminalWithScrollback(10, 3, 256)
	terminal.screen.Write([]byte("https://" + strings.Repeat("1234567890", 90)))
	look := darkPalette
	look.highlight = highlight.Compile([]highlight.Rule{{Pattern: `https://[^\s]+`, Regex: true, Color: highlight.Cyan}})
	draw := func() {
		terminal.cache.begin()
		terminal.prepareLogicalLines()
		for row := 0; row < 3; row++ {
			terminal.cache.rowWithLogical(terminal.presentedRow(row), 10, -1, cellSpan{}, &look, false, terminal.presentedLogicalLine(row), terminal.logicalView[row].suppressed)
		}
		terminal.cache.end(3)
	}
	draw()
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		draw()
	}
}
