package workbench

import (
	"strconv"
	"time"

	"github.com/vivaui/viva/ui"
)

// terminalGutter is what the strip at the left of the terminal shows for each
// line: its number, the time it was written, both, or nothing.
//
// The gutter takes its width from the terminal's columns. The grid, the pty size
// the remote is told, selection, links, find and mouse coordinates all start at
// geometry.left, so moving that edge keeps every one of them consistent with
// no overlay and no second coordinate system. The columns stay reserved on the
// alternate screen too, where the strip is left empty: a full-screen program
// then owns every cell it was told about, and entering or leaving it never
// changes the size the remote sees.
type terminalGutter uint8

const (
	gutterOff terminalGutter = iota
	gutterNumbers
	gutterTime
	gutterBoth
)

const (
	gutterNumberChars = 6 // up to 999999, then the low digits
	gutterTimeChars   = 8 // HH:MM:SS
	gutterMarkChars   = 1 // the cell the wrap mark takes
	gutterRuleGap     = float32(16)
)

func (gutter terminalGutter) String() string {
	switch gutter {
	case gutterNumbers:
		return "numbers"
	case gutterTime:
		return "time"
	case gutterBoth:
		return "both"
	}
	return "off"
}

func terminalGutterFromString(value string) terminalGutter {
	switch value {
	case "numbers":
		return gutterNumbers
	case "time":
		return gutterTime
	case "both":
		return gutterBoth
	}
	return gutterOff
}

func (gutter terminalGutter) numbers() bool { return gutter == gutterNumbers || gutter == gutterBoth }
func (gutter terminalGutter) times() bool   { return gutter == gutterTime || gutter == gutterBoth }

// chars is the gutter's text width in cells: the time at the strip's left edge,
// the cell the wrap mark takes, then the number against the rule.
func (gutter terminalGutter) chars() int {
	n := 0
	if gutter.times() {
		n += gutterTimeChars
	}
	if gutter.numbers() {
		n += gutterNumberChars
	}
	if n > 0 {
		n += gutterMarkChars
	}
	return n
}

// gutterWidth is the pixels the gutter takes from the left of the grid.
func (state *State) gutterWidth() float32 {
	chars := state.terminalGutter.chars()
	if chars == 0 || state.cellWidth <= 0 {
		return 0
	}
	return snap(float32(chars)*state.cellWidth) + gutterRuleGap
}

func (state *State) wallSeconds() int64 {
	if state.wallClock != nil {
		return state.wallClock()
	}
	return time.Now().Unix()
}

// stampTerminal turns line timestamps on or off for a terminal. Off, the
// emulator records nothing.
func (state *State) stampTerminal(t *terminal) {
	if state.terminalGutter.times() {
		t.screen.SetClock(state.wallSeconds)
	} else {
		t.screen.SetClock(nil)
	}
}

func (state *State) setTerminalGutter(gutter terminalGutter) bool {
	if state.terminalGutter == gutter {
		return false
	}
	state.terminalGutter = gutter
	for index := range state.sessions {
		if t := state.sessions[index].term; t != nil {
			state.stampTerminal(t)
		}
	}
	state.fitTerminals()
	return true
}

// gutterEntry caches the text of one row's gutter so a repaint of unchanged
// rows formats nothing.
type gutterEntry struct {
	number uint64
	at     int64
	mode   terminalGutter
	// line is the number as digits, written is the time of day; either is empty
	// when the mode leaves it out or the row has not been written.
	line    string
	written string
}

// gutterStyles are the two tones of the strip. Both take the scheme's bright
// cyan, so the strip reads as part of the terminal's palette and follows the
// scheme, with the time a step quieter than the line number. Mono matters: a plain label
// is set in the proportional face, where digits differ in width and the time
// column wobbles; MonoFace alone only picks which fixed-pitch font Mono uses.
func gutterStyles(size float32, face ui.MonoFace, pal *palette) (number, written ui.TextStyle) {
	cyan := ui.RGB(pal.ansi[14][0], pal.ansi[14][1], pal.ansi[14][2])
	// Cyan thinned over a light background loses contrast fast, so a scheme with
	// dark text gets a firmer tone.
	numberAlpha, writtenAlpha := float32(0.85), float32(0.65)
	if fg := pal.termFG; fg.Red+fg.Green+fg.Blue < 1.5 {
		numberAlpha, writtenAlpha = 1, 0.85
	}
	number = ui.TextStyle{Size: size, Color: withAlpha(cyan, numberAlpha), Mono: true, MonoFace: face}
	written = number
	written.Color = withAlpha(cyan, writtenAlpha)
	return number, written
}

// paintGutter draws the strip for row. entry is the row's cache entry. The time
// starts at the strip's left edge and the number ends against the rule: the
// number is right-aligned in its block so the digits line up whatever their
// count, and the time sits ahead of it, where the eye finds it first. A row that
// continues a wrapped line is tied to it by a mark in the cell between the two.
func (state *State) paintGutter(canvas *ui.Canvas, entry *rowEntry, t *terminal, geometry terminalGeometry, row int, y float32, number, written ui.TextStyle) {
	mode := state.terminalGutter
	line := t.screen.Row(row)
	lineNumber := t.screen.LineNumber(row)
	cached := &entry.gutter
	if cached.mode != mode || cached.number != lineNumber || cached.at != line.Written || (cached.line == "" && cached.written == "") {
		cached.number, cached.at, cached.mode = lineNumber, line.Written, mode
		cached.line, cached.written = formatGutter(mode, lineNumber, line.Written)
	}
	left := geometry.left - state.gutterWidth()
	if cached.written != "" {
		canvas.Label(cached.written, ui.Rect{X: left, Y: y, Width: float32(gutterTimeChars)*state.cellWidth + 1, Height: state.cellHeight}, written)
	}
	mark := left
	if mode.times() {
		mark += float32(gutterTimeChars) * state.cellWidth
	}
	if cached.line != "" {
		// The number keeps the column it has beside the rule whether or not the
		// row carries a time: a row not written yet must not shift it.
		canvas.LabelAligned(cached.line, ui.Rect{
			X:      mark + float32(gutterMarkChars)*state.cellWidth,
			Y:      y,
			Width:  float32(gutterNumberChars) * state.cellWidth,
			Height: state.cellHeight,
		}, number, ui.AlignRight)
	}
	if t.screen.ContinuesAbove(row) {
		paintWrapMark(canvas, mark, y, state.cellWidth, state.cellHeight, written.Color)
	}
}

// paintWrapMark draws the corner that ties a row to the line it continues: a
// stroke down the cell's left turning right along its middle, so a wrapped block
// reads as one line whose tail runs under it. It is drawn rather than set in a
// glyph, because the strip must not depend on the terminal's font holding a wrap
// arrow.
func paintWrapMark(canvas *ui.Canvas, x, y, cellWidth, cellHeight float32, color ui.Color) {
	stroke := max(float32(1), snap(cellWidth/8))
	top := snap(y + cellHeight*0.2)
	middle := snap(y + cellHeight*0.58)
	left := snap(x + cellWidth*0.25)
	right := snap(x + cellWidth*0.8)
	canvas.Fill(ui.Rect{X: left, Y: top, Width: stroke, Height: middle - top}, color)
	canvas.Fill(ui.Rect{X: left, Y: middle - stroke, Width: right - left, Height: stroke}, color)
}

// formatGutter is the two texts of a gutter: the line number and the time.
func formatGutter(mode terminalGutter, number uint64, written int64) (line, at string) {
	if mode.numbers() {
		var scratch [gutterNumberChars]byte
		line = string(strconv.AppendUint(scratch[:0], number%1000000, 10))
	}
	if mode.times() && written != 0 {
		var scratch [gutterTimeChars]byte
		at = string(time.Unix(written, 0).AppendFormat(scratch[:0], "15:04:05"))
	}
	return line, at
}
