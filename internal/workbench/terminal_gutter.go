package workbench

import (
	"strconv"
	"time"

	"github.com/vivaui/viva/gfx"
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

// chars is the gutter's text width in cells.
func (gutter terminalGutter) chars() int {
	n := 0
	if gutter.numbers() {
		n += gutterNumberChars
	}
	if gutter.times() {
		n += gutterTimeChars
	}
	if n == gutterNumberChars+gutterTimeChars {
		n++ // a cell between the two
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
func (state *State) paintGutter(canvas *gfx.Canvas, entry *rowEntry, t *terminal, geometry terminalGeometry, row int, y float32, number, written gfx.TextStyle) {
	mode := state.terminalGutter
	line := t.screen.Row(row)
	lineNumber := t.screen.LineNumber(row)
	cached := &entry.gutter
	if cached.mode != mode || cached.number != lineNumber || cached.at != line.Written || (cached.line == "" && cached.written == "") {
		cached.number, cached.at, cached.mode = lineNumber, line.Written, mode
		cached.line, cached.written = formatGutter(mode, lineNumber, line.Written)
	}
	left := geometry.left - state.gutterWidth()
	if cached.line != "" {
		width := float32(gutterNumberChars) * state.cellWidth
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
		var scratch [gutterNumberChars]byte
		line = string(strconv.AppendUint(scratch[:0], number%1000000, 10))
	}
	if mode.times() && written != 0 {
		var scratch [gutterTimeChars]byte
		at = string(time.Unix(written, 0).AppendFormat(scratch[:0], "15:04:05"))
	}
	return line, at
}
