package workbench

import (
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
)

// terminalGutter is what the strip at the left of the terminal shows for each
// line: its number, the time its content was written, both, or nothing. The
// strip belongs to logical lines: a soft-wrapped run paints its number and
// time once, on the row that begins it, so a resize that rewraps the text into
// more or fewer rows skips the extra rows and moves no number or time around
// it. A program that repaints a line in place (top, a progress bar) moves that
// line's time forward with every write.
//
// The gutter takes its width from the terminal's columns, auto-sizing its
// line-number block per terminal from gutterMinNumberChars up to
// gutterMaxNumberChars as the line count crosses powers of ten. The grid, the
// pty size the remote is told, selection, links, find and mouse coordinates all
// start at geometry.left, so moving that edge keeps every one of them consistent
// with no overlay and no second coordinate system. The columns stay reserved on
// the alternate screen too, where the strip is left empty: a full-screen program
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
	gutterMinNumberChars = 4 // 1..9999 without resizing: an ordinary session never crosses it
	gutterMaxNumberChars = 6 // up to 999999, then the low digits
	gutterTimeChars      = 8 // HH:MM:SS
	gutterRuleGap        = float32(16)
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

// gutterNumberDigits is the cell count needed to show line, clamped to the
// gutter's minimum and maximum digit width. Lines past 999999 wrap to their
// low six digits, so the column stays at gutterMaxNumberChars once reached.
func gutterNumberDigits(line uint64) int {
	digits := 1
	for ; line >= 10 && digits < gutterMaxNumberChars; line /= 10 {
		digits++
	}
	return max(digits, gutterMinNumberChars)
}

// gutterNumberChars is the digit columns reserved for t's line numbers. It
// grows monotonically at powers of ten up to gutterMaxNumberChars and never
// advances from the alternate screen or mid-synchronized update, so full-screen
// programs, cursor moves and scrolling history never resize the PTY.
func (t *terminal) gutterNumberChars() int {
	if t == nil {
		return gutterMinNumberChars
	}
	chars := max(t.gutterDigits, gutterMinNumberChars)
	if t.screen == nil || t.screen.Modes().AltScreen || t.sync != nil {
		return chars
	}
	if needed := gutterNumberDigits(t.cursorLine()); needed > chars {
		chars = needed
		t.gutterDigits = chars
	}
	return chars
}

// chars is the gutter's text width in cells for a number column of numberChars digits.
func (gutter terminalGutter) chars(numberChars int) int {
	n := 0
	if gutter.numbers() {
		n += numberChars
	}
	if gutter.times() {
		n += gutterTimeChars
	}
	if gutter.numbers() && gutter.times() {
		n++ // a cell between the two
	}
	return n
}

// gutterWidthFor is the pixels the gutter takes from the left of t's grid.
func (state *State) gutterWidthFor(t *terminal) float32 {
	if state.terminalGutter == gutterOff || state.cellWidth <= 0 {
		return 0
	}
	numberChars := 0
	if state.terminalGutter.numbers() {
		numberChars = t.gutterNumberChars()
	}
	chars := state.terminalGutter.chars(numberChars)
	if chars == 0 {
		return 0
	}
	return snap(float32(chars)*state.cellWidth) + gutterRuleGap
}

// gutterWidth is the pixels the gutter takes for a default (minimum-width) terminal.
func (state *State) gutterWidth() float32 {
	return state.gutterWidthFor(nil)
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
	state.applyTerminalGutter()
	return true
}

// applyTerminalGutter applies the strip the page's choice already wrote to the
// terminals that are open and refits them to its width.
func (state *State) applyTerminalGutter() {
	for index := range state.sessions {
		if t := state.sessions[index].term; t != nil {
			state.stampTerminal(t)
		}
	}
	state.fitTerminals()
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
func gutterStyles(size float32, face gfx.MonoFace, pal *palette) (number, written gfx.TextStyle) {
	cyan := gfx.RGB(pal.ansi[14][0], pal.ansi[14][1], pal.ansi[14][2])
	// Cyan thinned over a light background loses contrast fast, so a scheme with
	// dark text gets a firmer tone.
	numberAlpha, writtenAlpha := float32(0.85), float32(0.65)
	if fg := pal.termFG; fg.Red+fg.Green+fg.Blue < 1.5 {
		numberAlpha, writtenAlpha = 1, 0.85
	}
	number = gfx.TextStyle{Size: size, Color: withAlpha(cyan, numberAlpha), Mono: true, MonoFace: face}
	written = number
	written.Color = withAlpha(cyan, writtenAlpha)
	return number, written
}

// paintGutter draws the strip for row. entry is the row's cache entry. The
// number is right-aligned against the rule's side of its block so the digits
// line up whatever their count; the time starts after it.
// A row that continues the line above it shows nothing: the line keeps the number
// and time of the row that begins it, so rewrapping leaves the strip as it was.
func (state *State) paintGutter(canvas *gfx.Canvas, entry *rowEntry, t *terminal, geometry terminalGeometry, row int, y float32, number, written gfx.TextStyle) {
	if !t.gutterHeads[row] {
		return
	}

	mode := state.terminalGutter
	line := t.screen.Row(row)
	lineNumber := t.gutterNumbers[row]
	cached := &entry.gutter
	if cached.mode != mode || cached.number != lineNumber || cached.at != line.Written || (cached.line == "" && cached.written == "") {
		cached.number, cached.at, cached.mode = lineNumber, line.Written, mode
		cached.line, cached.written = formatGutter(mode, lineNumber, line.Written)
	}
	numberChars := t.gutterNumberChars()
	left := geometry.left - state.gutterWidthFor(t)
	if cached.line != "" {
		width := float32(numberChars) * state.cellWidth
		canvas.LabelAligned(cached.line, gfx.Rect{X: left, Y: y, Width: width, Height: state.cellHeight}, number, gfx.AlignRight)
		left += width + state.cellWidth
	}
	if cached.written != "" {
		canvas.Label(cached.written, gfx.Rect{X: left, Y: y, Width: float32(gutterTimeChars)*state.cellWidth + 1, Height: state.cellHeight}, written)
	}
}

// formatGutter is the two texts of a gutter: the line number and the time.
func formatGutter(mode terminalGutter, number uint64, written int64) (line, at string) {
	if mode.numbers() {
		var scratch [gutterMaxNumberChars]byte
		line = string(strconv.AppendUint(scratch[:0], number%1000000, 10))
	}
	if mode.times() && written != 0 {
		var scratch [gutterTimeChars]byte
		at = string(time.Unix(written, 0).AppendFormat(scratch[:0], "15:04:05"))
	}
	return line, at
}
