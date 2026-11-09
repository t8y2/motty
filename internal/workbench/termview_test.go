package workbench

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/highlight"
	"motty/internal/term"
)

func TestResizeReusesRowsWhoseContentStillFits(t *testing.T) {
	for _, rules := range [][]highlight.Rule{nil, highlight.Builtin()} {
		screen := term.New(40, 3, 10)
		screen.Write([]byte("error: 你好 é"))
		look := darkPalette
		look.highlight = highlight.Compile(rules)
		var cache rowCache
		cache.begin()
		cache.row(screen.Row(0), 40, -1, cellSpan{}, &look, false)
		for _, cols := range []int{80, 60, 30, 40} {
			screen.Resize(cols, 3)
			cache.begin()
			entry := cache.row(screen.Row(0), cols, -1, cellSpan{}, &look, false)
			var fresh rowCache
			fresh.begin()
			want := fresh.row(screen.Row(0), cols, -1, cellSpan{}, &look, false)
			if entry.text != want.text || !reflect.DeepEqual(entry.runs, want.runs) || !reflect.DeepEqual(entry.spans, want.spans) {
				t.Fatalf("at width %d, cached row %+v disagrees with fresh row %+v", cols, entry, want)
			}
		}
		if cache.builds != 1 {
			t.Fatalf("resizing blank padding rebuilt the same row %d times", cache.builds)
		}
	}
}

func TestResizeReevaluatesHighlightsThatMatchTrailingPadding(t *testing.T) {
	screen := term.New(8, 2, 0)
	screen.Write([]byte("error"))
	look := darkPalette
	look.highlight = highlight.Compile([]highlight.Rule{{Pattern: `error {3}$`, Regex: true, Slot: highlight.Red}})
	var cache rowCache
	for _, cols := range []int{8, 10, 8} {
		screen.Resize(cols, 2)
		cache.begin()
		entry := cache.row(screen.Row(0), cols, -1, cellSpan{}, &look, false)
		want := look.termFG
		if cols == 8 {
			want = look.highlightColor(highlight.Red, false)
		}
		if len(entry.runs) == 0 || entry.runs[0].Color != want {
			t.Fatalf("at width %d, runs = %+v, want first word in %v", cols, entry.runs, want)
		}
	}
}

func TestResizeClipsCachedAlternateScreenCellsAndBackgrounds(t *testing.T) {
	for _, text := range []string{"abc界XYZ", "abc\x1b[44m     ", "abc\x1b[4m     ", "abcdéXYZ"} {
		t.Run(text, func(t *testing.T) {
			screen := term.New(8, 2, 0)
			screen.Write([]byte("\x1b[?1049h" + text))
			var cache rowCache
			for _, cols := range []int{8, 4, 12} {
				screen.Resize(cols, 2)
				cache.begin()
				entry := cache.row(screen.Row(0), cols, -1, cellSpan{}, &darkPalette, false)
				var fresh rowCache
				fresh.begin()
				want := fresh.row(screen.Row(0), cols, -1, cellSpan{}, &darkPalette, false)
				if entry.text != want.text || !reflect.DeepEqual(entry.runs, want.runs) || !reflect.DeepEqual(entry.spans, want.spans) {
					t.Fatalf("at width %d, cached row %+v disagrees with fresh row %+v", cols, entry, want)
				}
			}
		})
	}
}

func TestResizeRevealsCachedHistoryBackground(t *testing.T) {
	for _, from := range []int{0, 5} {
		t.Run(fmt.Sprintf("from=%d", from), func(t *testing.T) {
			screen := term.New(8, 2, 20)
			fmt.Fprintf(screen, "\x1b[%dG\x1b[44m\x1b[K\x1b[0m\r\nx\r\ny", from+1)
			screen.Resize(4, 2)
			screen.Scroll(1)
			var cache rowCache
			cache.begin()
			before := screen.Row(0)
			cache.row(before, 4, -1, cellSpan{}, &darkPalette, false)
			screen.Resize(8, 2)
			line := screen.Row(0)
			if line.ID != before.ID || line.Version != before.Version {
				t.Fatal("history padding unexpectedly reflowed or changed its version")
			}
			cache.begin()
			entry := cache.row(line, 8, -1, cellSpan{}, &darkPalette, false)
			if len(entry.spans) != 1 || entry.spans[0].from != from || entry.spans[0].to != 8 {
				t.Fatalf("revealed history background = %+v, want [%d,8)", entry.spans, from)
			}
		})
	}
}

func TestClippedWideHistoryRowMatchesFreshCache(t *testing.T) {
	for _, prefix := range []string{"", "\x1b[44m"} {
		t.Run(prefix, func(t *testing.T) {
			screen := term.New(8, 2, 20)
			screen.Write([]byte(prefix + "abc\u754cXYZ\r\nnext"))
			screen.Resize(4, 2)
			var cache rowCache
			cache.begin()
			cache.row(screen.Row(0), 4, -1, cellSpan{}, &darkPalette, false)
			screen.Write([]byte("\r\nx"))
			screen.Scroll(1)
			cache.begin()
			cached := cache.row(screen.Row(0), 4, -1, cellSpan{}, &darkPalette, false)
			var fresh rowCache
			fresh.begin()
			rebuilt := fresh.row(screen.Row(0), 4, -1, cellSpan{}, &darkPalette, false)
			if cached.text != rebuilt.text || !reflect.DeepEqual(cached.runs, rebuilt.runs) || !reflect.DeepEqual(cached.spans, rebuilt.spans) {
				t.Fatalf("cache disagrees with fresh rendering: cached=%+v fresh=%+v", cached, rebuilt)
			}
		})
	}
}

// build prepares row 0 of a screen holding input.
func build(t testing.TB, input string, cursor int) *rowEntry {
	t.Helper()
	screen := term.New(40, 3, 10)
	screen.Write([]byte(input))
	var cache rowCache
	cache.begin()
	return cache.row(screen.Row(0), 40, cursor, cellSpan{}, &darkPalette, false)
}

// An input method's composition is drawn over the cells it covers, and a
// rectangle painted there does not hide text already in them: a program that
// shows placeholder text at its cursor would show through the composition.
func TestCoveredCellsHoldNoText(t *testing.T) {
	screen := term.New(40, 3, 10)
	screen.Write([]byte("Ask Codex"))
	var cache rowCache
	cache.begin()
	entry := cache.row(screen.Row(0), 40, -1, cellSpan{from: 0, to: 2}, &darkPalette, false)
	if entry.text != "k Codex" || entry.runs[0].Column != 2 {
		t.Fatalf("text = %q runs = %+v: the covered cells must not be drawn", entry.text, entry.runs)
	}
	cache.begin()
	if again := cache.row(screen.Row(0), 40, -1, cellSpan{}, &darkPalette, false); again.text != "Ask Codex" {
		t.Fatalf("text = %q: a line is prepared again when the cover goes away", again.text)
	}
}

func TestRowsBecomeRunsOnlyForWhatIsVisible(t *testing.T) {
	entry := build(t, "  \x1b[31mred\x1b[0m  plain   ", -1)
	if entry.text != "red  plain" {
		t.Fatalf("text = %q: leading blanks need no text, trailing ones are dropped", entry.text)
	}
	if len(entry.runs) != 2 {
		t.Fatalf("runs = %+v", entry.runs)
	}
	red, plain := entry.runs[0], entry.runs[1]
	if red.Column != 2 || red.Bytes != 5 || red.Cells != 5 || red.Color != gfx.RGB(240, 120, 120) {
		t.Errorf("red run = %+v: it starts at its cell and swallows the blanks that follow", red)
	}
	if plain.Column != 7 || plain.Bytes != 5 || plain.Color != darkPalette.text {
		t.Errorf("plain run = %+v", plain)
	}
	if len(entry.spans) != 0 {
		t.Errorf("a row without backgrounds has spans: %+v", entry.spans)
	}
}

func TestEveryNonASCIICharacterIsItsOwnRunOnItsCell(t *testing.T) {
	entry := build(t, "ab你好é", -1)
	if entry.text != "ab你好é" {
		t.Fatalf("text = %q", entry.text)
	}
	want := []gfx.TextRun{
		{Column: 0, Bytes: 2, Cells: 2},
		{Column: 2, Bytes: 3, Cells: 2},
		{Column: 4, Bytes: 3, Cells: 2},
		{Column: 6, Bytes: 2, Cells: 1},
	}
	if len(entry.runs) != len(want) {
		t.Fatalf("runs = %+v", entry.runs)
	}
	for index, run := range want {
		got := entry.runs[index]
		if got.Column != run.Column || got.Bytes != run.Bytes || got.Cells != run.Cells {
			t.Errorf("run %d = %+v, want column %d bytes %d cells %d", index, got, run.Column, run.Bytes, run.Cells)
		}
	}
}

func TestCombiningMarksStayWithTheirCharacter(t *testing.T) {
	entry := build(t, "xéy", -1)
	if entry.text != "xéy" || len(entry.runs) != 3 || entry.runs[1].Bytes != 3 || entry.runs[1].Cells != 1 || entry.runs[2].Column != 2 {
		t.Fatalf("text %q runs %+v", entry.text, entry.runs)
	}
}

func TestAttributesBecomeRunStyleAndBackgrounds(t *testing.T) {
	entry := build(t, "\x1b[1mB\x1b[3mI\x1b[4mU\x1b[9mS\x1b[0m \x1b[44m  \x1b[0m\x1b[7mR\x1b[0m\x1b[2mD", -1)
	if len(entry.runs) < 5 {
		t.Fatalf("runs = %+v", entry.runs)
	}
	if entry.runs[0].Weight != gfx.WeightBold || entry.runs[0].Italic {
		t.Errorf("B = %+v", entry.runs[0])
	}
	if !entry.runs[1].Italic || entry.runs[1].Weight != gfx.WeightBold {
		t.Errorf("I = %+v", entry.runs[1])
	}
	if !entry.runs[2].Underline || !entry.runs[3].Strike {
		t.Errorf("U/S = %+v %+v", entry.runs[2], entry.runs[3])
	}
	// Decorated runs do not stretch over the blanks after them.
	if entry.runs[3].Cells != 1 {
		t.Errorf("the strike-through run spans %d cells", entry.runs[3].Cells)
	}
	// A blue background over two blanks, and an inverse character.
	if len(entry.spans) != 2 {
		t.Fatalf("spans = %+v", entry.spans)
	}
	if blue := entry.spans[0]; blue.from != 5 || blue.to != 7 || blue.color != gfx.RGB(110, 160, 240) {
		t.Errorf("blue span = %+v", blue)
	}
	if inverse := entry.spans[1]; inverse.from != 7 || inverse.to != 8 || inverse.color != darkPalette.text {
		t.Errorf("inverse span = %+v", inverse)
	}
	dim := entry.runs[len(entry.runs)-1]
	if dim.Color.Alpha >= 1 {
		t.Errorf("dim text is fully opaque: %+v", dim)
	}
}

func TestTrueAndIndexedColorsResolveAgainstThePalette(t *testing.T) {
	entry := build(t, "\x1b[38;2;10;20;30ma\x1b[38;5;208mb\x1b[38;5;240mc\x1b[91md", -1)
	colors := []gfx.Color{gfx.RGB(10, 20, 30), gfx.RGB(255, 135, 0), gfx.RGB(88, 88, 88), gfx.RGB(255, 150, 150)}
	for index, want := range colors {
		if got := entry.runs[index].Color; got != want {
			t.Errorf("run %d color = %v, want %v", index, got, want)
		}
	}
}

func TestTheCursorIsABlockOverItsCellEvenPastTheText(t *testing.T) {
	inside := build(t, "abc", 1)
	if len(inside.spans) != 1 || inside.spans[0].from != 1 || inside.spans[0].to != 2 || inside.spans[0].color != darkPalette.cursor {
		t.Fatalf("spans = %+v", inside.spans)
	}
	if inside.runs[1].Color != darkPalette.terminal {
		t.Errorf("the character under the cursor is drawn in %v, want the background color", inside.runs[1].Color)
	}
	past := build(t, "abc", 10)
	if len(past.spans) != 1 || past.spans[0].from != 10 || past.spans[0].to != 11 {
		t.Fatalf("a cursor beyond the text: %+v", past.spans)
	}
	wide := build(t, "世x", 1) // the cursor on the tail of a wide character covers the whole character
	if len(wide.spans) != 1 || wide.spans[0].from != 0 || wide.spans[0].to != 2 {
		t.Fatalf("wide cursor spans = %+v", wide.spans)
	}
}

func TestControlCharactersAndHiddenTextAreNotDrawn(t *testing.T) {
	entry := build(t, "a\x1b[8msecret\x1b[0mb", -1)
	if strings.Contains(entry.text, "secret") || !strings.HasPrefix(entry.text, "a") || !strings.HasSuffix(entry.text, "b") {
		t.Fatalf("text = %q", entry.text)
	}
}

func TestScrolledLinesAreFoundAgainNotRebuilt(t *testing.T) {
	screen := term.New(40, 10, 100)
	for line := 0; line < 10; line++ {
		fmt.Fprintf(screen, "\x1b[3%dmline %d\x1b[0m\r\n", line%7, line)
	}
	var cache rowCache
	paint := func() {
		cache.begin()
		for row := 0; row < 10; row++ {
			cache.row(screen.Row(row), 40, -1, cellSpan{}, &darkPalette, false)
		}
		cache.end(10)
	}
	paint()
	built := cache.builds
	paint()
	if cache.builds != built {
		t.Fatalf("an unchanged screen rebuilt %d lines", cache.builds-built)
	}
	// One new line scrolls everything up one row: only the new line is prepared.
	screen.Write([]byte("fresh\r\n"))
	paint()
	if cache.builds-built > 2 {
		t.Fatalf("scrolling one line rebuilt %d lines", cache.builds-built)
	}
	// The cache stays about one screen long however much output went by.
	for line := 0; line < 5000; line++ {
		fmt.Fprintf(screen, "output %d\r\n", line)
		if line%10 == 0 {
			paint()
		}
	}
	paint()
	if len(cache.entries) > 40 {
		t.Fatalf("the cache holds %d lines for a 10-line screen", len(cache.entries))
	}
}

func TestChangingThePaletteRebuildsRows(t *testing.T) {
	screen := term.New(20, 2, 0)
	screen.Write([]byte("\x1b[32mok"))
	var cache rowCache
	cache.begin()
	dark := cache.row(screen.Row(0), 20, -1, cellSpan{}, &darkPalette, false).runs[0].Color
	cache.begin()
	light := cache.row(screen.Row(0), 20, -1, cellSpan{}, &lightPalette, false).runs[0].Color
	if dark == light {
		t.Fatal("the same cached row served both palettes")
	}
}

func BenchmarkPaintTerminalUnchanged(b *testing.B) {
	state, _, _, shell := liveSession(&testing.T{})
	for line := 0; line < 60; line++ {
		shell.say(fmt.Sprintf("\x1b[32mline %d\x1b[0m some \x1b[1;34mcolored\x1b[0m output with 你好 mixed text\r\n", line))
	}
	state.Wake()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	canvas.Reset()
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func BenchmarkPaintTerminalSelectionUnchanged(b *testing.B) {
	state, _, _, shell := liveSession(&testing.T{})
	for line := 0; line < 60; line++ {
		shell.say(fmt.Sprintf("line %d colored output with 你好 mixed text\r\n", line))
	}
	state.Wake()
	state.sessions[0].term.selection.selectRange(
		state.sessions[0].term.screen.PointAt(term.Position{Row: 5, Column: 3}),
		state.sessions[0].term.screen.PointAt(term.Position{Row: 30, Column: 70}),
	)
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	canvas.Reset()
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// Every line changes, as when a program floods the screen: the cost of
// preparing a whole screen of colored lines.
func BenchmarkPaintTerminalFlood(b *testing.B) {
	state, _, _, shell := liveSession(&testing.T{})
	state.Wake()
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	chunk := make([]byte, 0, 1<<14)
	for line := 0; line < 60; line++ {
		chunk = fmt.Appendf(chunk, "\x1b[32m%06d\x1b[0m the quick \x1b[1;34mbrown\x1b[0m fox jumps over \x1b[31mthe\x1b[0m lazy dog 0123456789 abcdefghijklmnop\r\n", line)
	}
	b.ReportAllocs()
	number := 0
	for b.Loop() {
		number++
		shell.say(string(chunk))
		state.Wake()
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	_ = number
}

func TestASCIICellRunsAskForLigatureShaping(t *testing.T) {
	entry := build(t, "-> x", -1)
	if len(entry.runs) != 1 || !entry.runs[0].Ligatures {
		t.Fatalf("runs = %+v, want one ligature-capable run", entry.runs)
	}
	entry = build(t, "a中b", -1)
	for _, run := range entry.runs {
		if run.Ligatures {
			t.Fatalf("non-ASCII run asked for ligatures: %+v", entry.runs)
		}
	}
	entry = build(t, "->", 1) // the cursor splits the pair
	for _, run := range entry.runs {
		if run.Ligatures {
			t.Fatalf("a run split by the cursor asked for ligatures: %+v", entry.runs)
		}
	}
}

// ls writes a directory as 01;34: bold and the base blue. Terminals draw that in
// the bright blue (Windows Terminal's default, xterm, iTerm2), which is what
// issue 305 compared against. Colors the program picked exactly stay as they are.
func TestBoldBaseColorsAreDrawnBright(t *testing.T) {
	input := "\x1b[1;34mA\x1b[0m\x1b[34mB\x1b[0m\x1b[1;94mC\x1b[0m\x1b[1;38;5;4mD\x1b[0m\x1b[1;38;2;0;0;200mE"
	// Adjacent characters in one style share a run, so look a character up by its column.
	colors := func(intense terminalIntense) (runs []gfx.TextRun) {
		pal := darkPalette
		pal.intense = intense
		screen := term.New(40, 3, 10)
		screen.Write([]byte(input))
		var cache rowCache
		cache.begin()
		for _, run := range cache.row(screen.Row(0), 40, -1, cellSpan{}, &pal, false).runs {
			for cell := int32(0); cell < run.Cells; cell++ {
				runs = append(runs, run)
			}
		}
		return runs
	}
	rgb := func(index int) gfx.Color {
		c := darkPalette.ansi[index]
		return gfx.RGB(c[0], c[1], c[2])
	}
	runs := colors(intenseBright)
	want := []gfx.Color{rgb(12), rgb(4), rgb(12), rgb(12), gfx.RGB(0, 0, 200)}
	for index, color := range want {
		if runs[index].Color != color {
			t.Errorf("bright: run %d color %+v, want %+v", index, runs[index].Color, color)
		}
	}
	if runs[0].Weight == gfx.WeightBold {
		t.Errorf("bright only: the face stays regular, got weight %v", runs[0].Weight)
	}
	if runs := colors(intenseBoth); runs[0].Color != rgb(12) || runs[0].Weight != gfx.WeightBold {
		t.Errorf("both: %+v", runs[0])
	}
	if runs := colors(intenseBold); runs[0].Color != rgb(4) || runs[0].Weight != gfx.WeightBold {
		t.Errorf("bold: %+v", runs[0])
	}
	if runs := colors(intensePlain); runs[0].Color != rgb(4) || runs[0].Weight == gfx.WeightBold {
		t.Errorf("plain: %+v", runs[0])
	}
}

// A Nerd Font icon is one cell wide and its ink is wider (t8y2/motty#503). The renderer lets the
// ink show over the blank cell after it, but only a cell that belongs to no run: a blank folded
// into the icon's run, or into the run after it, would be that run's cell and the ink would be cut.
func TestTheBlankAfterAnIconBelongsToNoRun(t *testing.T) {
	const debian, python = "\uf306", "\ue73c"
	entry := build(t, debian+" root "+python+" v3", -1)
	owner := make([]int, 40)
	for index := range owner {
		owner[index] = -1
	}
	for index, run := range entry.runs {
		for cell := run.Column; cell < run.Column+run.Cells; cell++ {
			if owner[cell] >= 0 {
				t.Fatalf("cell %d belongs to runs %d and %d: %+v", cell, owner[cell], index, entry.runs)
			}
			owner[cell] = index
		}
	}
	for _, icon := range []int{0, 7} { // the cells of the two icons
		if got := entry.runs[owner[icon]]; got.Cells != 1 || got.Bytes != 3 {
			t.Errorf("the icon in cell %d is run %+v, want one cell and its own run", icon, got)
		}
		if owner[icon+1] != -1 {
			t.Errorf("the cell after the icon in cell %d belongs to run %+v, want no run", icon, entry.runs[owner[icon+1]])
		}
	}
}
