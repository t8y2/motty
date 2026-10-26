package workbench

import (
	"fmt"
	"strings"
	"testing"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

// build prepares row 0 of a screen holding input.
func build(t testing.TB, input string, cursor int) *rowEntry {
	t.Helper()
	screen := term.New(40, 3, 10)
	screen.Write([]byte(input))
	var cache rowCache
	cache.begin()
	return cache.row(screen.Row(0), 40, cursor, &darkPalette, false)
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
	if red.Column != 2 || red.Bytes != 5 || red.Cells != 5 || red.Color != ui.RGB(240, 120, 120) {
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
	want := []ui.TextRun{
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
	if entry.runs[0].Weight != ui.WeightBold || entry.runs[0].Italic {
		t.Errorf("B = %+v", entry.runs[0])
	}
	if !entry.runs[1].Italic || entry.runs[1].Weight != ui.WeightBold {
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
	if blue := entry.spans[0]; blue.from != 5 || blue.to != 7 || blue.color != ui.RGB(110, 160, 240) {
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
	colors := []ui.Color{ui.RGB(10, 20, 30), ui.RGB(255, 135, 0), ui.RGB(88, 88, 88), ui.RGB(255, 150, 150)}
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
			cache.row(screen.Row(row), 40, -1, &darkPalette, false)
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
	dark := cache.row(screen.Row(0), 20, -1, &darkPalette, false).runs[0].Color
	cache.begin()
	light := cache.row(screen.Row(0), 20, -1, &lightPalette, false).runs[0].Color
	if dark == light {
		t.Fatal("the same cached row served both palettes")
	}
}

func TestPaintingAnUnchangedTerminalAllocatesNothing(t *testing.T) {
	state, _, _, shell := liveSession(t)
	shell.say("\x1b[32muser@host\x1b[0m:~$ ls\r\nfile1  \x1b[34mdir\x1b[0m  你好\r\n")
	state.Wake()
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	paint := func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
	paint()
	paint()
	if allocs := testing.AllocsPerRun(50, paint); allocs != 0 {
		t.Errorf("painting an unchanged terminal allocates %v times", allocs)
	}
	state.sessions[0].term.selection.selectRange(
		term.Position{Row: 0, Column: 0},
		term.Position{Row: 1, Column: 20},
	)
	paint()
	if allocs := testing.AllocsPerRun(50, paint); allocs != 0 {
		t.Errorf("painting an unchanged selection allocates %v times", allocs)
	}
}

func BenchmarkPaintTerminalUnchanged(b *testing.B) {
	state, _, _, shell := liveSession(&testing.T{})
	for line := 0; line < 60; line++ {
		shell.say(fmt.Sprintf("\x1b[32mline %d\x1b[0m some \x1b[1;34mcolored\x1b[0m output with 你好 mixed text\r\n", line))
	}
	state.Wake()
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
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
		term.Position{Row: 5, Column: 3},
		term.Position{Row: 30, Column: 70},
	)
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
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
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
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
