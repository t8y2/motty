package workbench

import (
	"math"
	"strconv"
	"time"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
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
	gutterNumberChars = 4 // up to 9999, then the low digits
	gutterTimeChars   = 8 // HH:MM:SS
	gutterMarkChars   = 1 // the cell the fold box and the wrap corner take
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
// a cell to set the number apart from it, the number, and at the strip's right
// the cell the fold box and the wrap corner take.
func (gutter terminalGutter) chars() int {
	n := 0
	if gutter.times() {
		n += gutterTimeChars
		if gutter.numbers() {
			n++ // a cell between the two
		}
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
// starts at the strip's left edge, the number follows it a cell later, and the
// cell before the rule carries the mark: the box that folds a command's output
// away, the line that shows which rows a block owns, or the corner that ties a
// wrapped row to the line above it.
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
	mark := geometry.left - gutterRuleGap - float32(gutterMarkChars)*state.cellWidth
	if cached.line != "" {
		// The number keeps the column it has before the mark cell whether or not
		// the row carries a time: a row not written yet must not shift it.
		canvas.LabelAligned(cached.line, ui.Rect{
			X:      mark - float32(gutterNumberChars)*state.cellWidth,
			Y:      y,
			Width:  float32(gutterNumberChars) * state.cellWidth,
			Height: state.cellHeight,
		}, number, ui.AlignRight)
	}
	state.paintFoldMark(canvas, t, row, mark, y, t.screen.FoldAt(row), t.screen.ContinuesAbove(row),
		number.Color, withAlpha(written.Color, written.Color.Alpha*0.45))
}

// paintFoldMark draws the mark cell of one row: a box with a minus that folds a
// command's output away (a plus once it is folded), the line that shows the rows
// of a block while it is open, or the corner that ties a wrapped row to the line
// it continues. They are drawn rather than set in glyphs, because the strip must
// not depend on the terminal's font holding a fold or wrap arrow.
func (state *State) paintFoldMark(canvas *ui.Canvas, t *terminal, row int, x, y float32, info term.FoldInfo, continues bool, strong, quiet ui.Color) {
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	if cellWidth <= 0 || cellHeight <= 0 {
		return
	}
	stroke := max(float32(1), snap(cellWidth/8))
	middle := snap(x + cellWidth/2)
	switch {
	case info.Anchor:
		box := snap(cellWidth - 2)
		left, top := snap(x+1), snap(y+(cellHeight-box)/2)
		canvas.Fill(ui.Rect{X: left, Y: top, Width: box, Height: stroke}, strong)
		canvas.Fill(ui.Rect{X: left, Y: top + box - stroke, Width: box, Height: stroke}, strong)
		canvas.Fill(ui.Rect{X: left, Y: top, Width: stroke, Height: box}, strong)
		canvas.Fill(ui.Rect{X: left + box - stroke, Y: top, Width: stroke, Height: box}, strong)
		canvas.Fill(ui.Rect{X: left + 2*stroke, Y: snap(y + cellHeight/2), Width: box - 4*stroke, Height: stroke}, strong)
		if info.Folded {
			// A plus says the block can be opened again.
			canvas.Fill(ui.Rect{X: middle, Y: top + 2*stroke, Width: stroke, Height: box - 4*stroke}, strong)
		} else if t.screen.FoldAt(row + 1).Inside {
			// The line the block hangs from.
			canvas.Fill(ui.Rect{X: middle, Y: top + box, Width: stroke, Height: y + cellHeight - (top + box)}, quiet)
		}
	case info.Inside:
		canvas.Fill(ui.Rect{X: middle, Y: y, Width: stroke, Height: cellHeight}, quiet)
		if !t.screen.FoldAt(row + 1).Inside {
			// The foot closes the block's line under its last row.
			canvas.Fill(ui.Rect{X: middle, Y: y + cellHeight - stroke, Width: snap(cellWidth * 0.6), Height: stroke}, quiet)
		}
	case continues:
		paintWrapMark(canvas, x, y, cellWidth, cellHeight, quiet)
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

// terminalFoldToggle folds or opens the command block whose box a press hit, and
// reports whether the press was one. The box sits in the gutter, outside the
// grid, so a press there is neither a selection nor the application's.
func (state *State) terminalFoldToggle(session *Session, event ui.PointerEvent) bool {
	if state.terminalGutter == gutterOff || event.Button != ui.PointerButtonPrimary {
		return false
	}
	if state.cellWidth <= 0 || state.cellHeight <= 0 {
		return false
	}
	geometry := terminalLayoutWithInfoBar(event.Viewport, state.cellWidth, state.cellHeight, state.terminalInfoBarHeight(session.ID), state.padding())
	box := geometry.left - gutterRuleGap - float32(gutterMarkChars)*state.cellWidth
	if event.X < box || event.X >= box+state.cellWidth {
		return false
	}
	row := int(math.Floor(float64((event.Y - geometry.top) / state.cellHeight)))
	return session.term.screen.ToggleFold(row)
}

// formatGutter is the two texts of a gutter: the line number and the time.
func formatGutter(mode terminalGutter, number uint64, written int64) (line, at string) {
	if mode.numbers() {
		var scratch [gutterNumberChars]byte
		line = string(strconv.AppendUint(scratch[:0], number%10000, 10))
	}
	if mode.times() && written != 0 {
		var scratch [gutterTimeChars]byte
		at = string(time.Unix(written, 0).AppendFormat(scratch[:0], "15:04:05"))
	}
	return line, at
}
