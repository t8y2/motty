package workbench

import (
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
	// gutterNumberChars is the least width of the number column. A terminal
	// that has written more lines than it holds widens the column, so a number
	// is never shown cut to its low digits.
	gutterNumberChars = 6
	gutterTimeChars   = 8 // HH:MM:SS
	gutterRuleGap     = float32(16)
	// gutterMarkWidthCells is the width of the column the fold box and the wrap
	// corner take, in cells. A cell and a half holds a box the size of a
	// character with a pixel of air around it, which is what makes it read as a
	// control rather than a stray dot.
	gutterMarkWidthCells = 1.5
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

// chars is the gutter's text width in cells, the mark column aside: the time at
// the strip's left edge, a cell to set the number apart from it, the number in
// digits cells, and a cell that keeps the mark column off the text.
func (gutter terminalGutter) chars(digits int) int {
	if gutter == gutterOff {
		return 0
	}
	n := 1 // the cell before the mark column
	if gutter.times() {
		n += gutterTimeChars
		if gutter.numbers() {
			n++ // and the one between the time and the number
		}
	}
	if gutter.numbers() {
		n += digits
	}
	return n
}

// gutterNumberDigits is the width of the number column in cells: the least, or
// what the line numbers written so far need.
func (state *State) gutterNumberDigits() int { return max(state.gutterDigits, gutterNumberChars) }

// growGutterNumbers widens the number column when the terminal has written a
// line whose number no longer fits, which takes a million lines; it never
// narrows again, so the grid is not resized by output scrolling back and forth
// across a power of ten. It reports whether the gutter changed width.
func (state *State) growGutterNumbers(session *Session) bool {
	t := session.term
	if t == nil || !state.terminalGutter.numbers() || t.presentedAltScreen() {
		return false
	}
	_, rows := t.screen.Size()
	digits := len(strconv.FormatUint(t.screen.LineNumber(rows-1), 10))
	if digits <= state.gutterNumberDigits() {
		return false
	}
	state.gutterDigits = digits
	state.fitTerminals()
	return true
}

// gutterMarkWidth is the width of the column the fold box and the wrap corner
// take. It is not a whole number of cells: the box is drawn the size of a
// character, and the column only has to hold it with a little room beside the
// number.
func (state *State) gutterMarkWidth() float32 {
	if state.cellWidth <= 0 {
		return 0
	}
	return snap(state.cellWidth * gutterMarkWidthCells)
}

// gutterWidth is the pixels the gutter takes from the left of the grid.
func (state *State) gutterWidth() float32 {
	if state.terminalGutter == gutterOff || state.cellWidth <= 0 {
		return 0
	}
	chars := float32(state.terminalGutter.chars(state.gutterNumberDigits())) * state.cellWidth
	return snap(chars) + state.gutterMarkWidth() + gutterRuleGap
}

func (state *State) wallSeconds() int64 {
	if state.wallClock != nil {
		return state.wallClock()
	}
	return time.Now().Unix()
}

// stampTerminal applies the gutter to a terminal: line timestamps on or off, and
// the fold boxes that live in the gutter. Without the gutter nothing is folded,
// whatever was folded before, so no output is left hidden with no box to open
// it; the choices stay on their lines and come back with the gutter. Off, the
// emulator records no times.
func (state *State) stampTerminal(t *terminal) {
	t.screen.SetFolding(state.terminalGutter != gutterOff)
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
func (state *State) paintGutter(canvas *ui.Canvas, entry *rowEntry, session *Session, geometry terminalGeometry, row int, y float32, number, written ui.TextStyle) {
	t := session.term
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
	mark := geometry.left - gutterRuleGap - state.gutterMarkWidth()
	if cached.line != "" {
		// The number keeps the column it has before the mark column whether or
		// not the row carries a time: a row not written yet must not shift it.
		canvas.LabelAligned(cached.line, ui.Rect{
			X:      mark - state.cellWidth - float32(state.gutterNumberDigits())*state.cellWidth,
			Y:      y,
			Width:  float32(state.gutterNumberDigits()) * state.cellWidth,
			Height: state.cellHeight,
		}, number, ui.AlignRight)
	}
	state.paintFoldMark(canvas, session, row, mark, y, t.screen.FoldAt(row), t.screen.FoldAt(row+1).Inside,
		t.screen.ContinuesAbove(row), number.Color, withAlpha(written.Color, written.Color.Alpha*0.45))
}

// paintFoldMark draws the mark column of one row: a box with a minus that folds
// a command's output away (a plus once it is folded), the line that shows the
// rows of a block while it is open, and the corner that ties a wrapped row to
// the line it continues, which a row inside a block can need as well as any
// other. They are drawn rather than set in glyphs, because the strip must not
// depend on the terminal's font holding a fold or wrap arrow. The box is also a
// control: it is remembered here and made a button once the terminal's own
// region has been declared, so that it sits above it (see declareFoldBoxes).
func (state *State) paintFoldMark(canvas *ui.Canvas, session *Session, row int, x, y float32, info term.FoldInfo, nextInside, continues bool, strong, quiet ui.Color) {
	cellWidth, cellHeight := state.cellWidth, state.cellHeight
	if cellWidth <= 0 || cellHeight <= 0 {
		return
	}
	width := state.gutterMarkWidth()
	stroke := max(float32(1), snap(cellWidth/8))
	middle := snap(x + width/2)
	if info.Anchor {
		t := session.term
		bounds := ui.Rect{X: x, Y: y, Width: width, Height: cellHeight}
		t.foldBoxes = append(t.foldBoxes, foldBox{row: row, bounds: bounds, folded: info.Folded})
		if canvas.Hovered() == hitTerminalFold.At(foldHitIndex(session.ID, row)) {
			canvas.Fill(bounds, withAlpha(strong, 0.2))
		}
		// A box the size of a character, with a pixel of air around it.
		box := snap(cellWidth + 2)
		left, top := snap(x+(width-box)/2), snap(y+(cellHeight-box)/2)
		canvas.Fill(ui.Rect{X: left, Y: top, Width: box, Height: stroke}, strong)
		canvas.Fill(ui.Rect{X: left, Y: top + box - stroke, Width: box, Height: stroke}, strong)
		canvas.Fill(ui.Rect{X: left, Y: top, Width: stroke, Height: box}, strong)
		canvas.Fill(ui.Rect{X: left + box - stroke, Y: top, Width: stroke, Height: box}, strong)
		inset := snap(box / 4)
		canvas.Fill(ui.Rect{X: left + inset, Y: snap(y + cellHeight/2), Width: box - 2*inset, Height: stroke}, strong)
		if info.Folded {
			// A plus says the block can be opened again.
			canvas.Fill(ui.Rect{X: middle, Y: top + inset, Width: stroke, Height: box - 2*inset}, strong)
		} else if nextInside {
			// The line the block hangs from.
			canvas.Fill(ui.Rect{X: middle, Y: top + box, Width: stroke, Height: y + cellHeight - (top + box)}, quiet)
		}
	} else if info.Inside {
		canvas.Fill(ui.Rect{X: middle, Y: y, Width: stroke, Height: cellHeight}, quiet)
		if !nextInside {
			// The foot closes the block's line under its last row.
			canvas.Fill(ui.Rect{X: middle, Y: y + cellHeight - stroke, Width: snap(width/2 - 1), Height: stroke}, quiet)
		}
	}
	if continues && !info.Anchor {
		paintWrapMark(canvas, x, y, width, cellHeight, quiet)
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
		var scratch [20]byte
		line = string(strconv.AppendUint(scratch[:0], number, 10))
	}
	if mode.times() && written != 0 {
		var scratch [gutterTimeChars]byte
		at = string(time.Unix(written, 0).AppendFormat(scratch[:0], "15:04:05"))
	}
	return line, at
}

// hitTerminalFold is the box on a command's row that folds its output. Its item
// number holds the session and the view row.
var hitTerminalFold = tree.Sub(shellHits, tree.Name("terminal-fold"))

// foldHitRows is the number of rows an item number reserves for one session: the
// terminal has at most 500.
const foldHitRows = 512

func foldHitIndex(session uint32, row int) int { return int(session)*foldHitRows + row }

// foldBox is a box the gutter painted this frame.
type foldBox struct {
	row    int
	bounds ui.Rect
	folded bool
}

// declareFoldBoxes makes the boxes the gutter painted into buttons, after the
// terminal's own region so that a press on one is the button's and not the
// terminal's: it is neither a selection nor a mouse report for the program, and
// the pointer shows a hand over it. Assistive technology finds a button that
// says whether the output is folded. The box takes no keyboard focus, which
// stays with the terminal.
func (state *State) declareFoldBoxes(canvas *ui.Canvas, session *Session) {
	t := session.term
	for _, box := range t.foldBoxes {
		label := state.messages.Tools.FoldOutput
		if box.folded {
			label = state.messages.Tools.UnfoldOutput
		}
		canvas.HitWithOptions(hitTerminalFold.At(foldHitIndex(session.ID, box.row)), box.bounds, label, ui.HitOptions{
			Role: ui.RoleButton, Cursor: ui.PointerCursorHand, SkipFocus: true, SkipTab: true,
			Expandable: true, Expanded: !box.folded,
		})
	}
	t.foldBoxes = t.foldBoxes[:0]
}

// foldBoxOf finds the session and view row a fold button belongs to.
func (state *State) foldBoxOf(hit ui.ID) (*Session, int, bool) {
	index, ok := hit.In(hitTerminalFold)
	if !ok {
		return nil, 0, false
	}
	for i := range state.sessions {
		if state.sessions[i].ID == uint32(index/foldHitRows) && state.sessions[i].term != nil {
			return &state.sessions[i], index % foldHitRows, true
		}
	}
	return nil, 0, false
}

// terminalFoldPointer folds or opens a block when its box is pressed. A double
// click is one press too many for a control that toggles, so only the first
// press of a sequence counts; the rest of the gesture belongs to the box, not
// to the terminal behind it.
func (state *State) terminalFoldPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	session, row, ok := state.foldBoxOf(event.Target)
	if !ok {
		return ui.EventResult{}, false
	}
	if event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{Handled: event.Kind != ui.PointerExit}, true
	}
	repaint := event.ClickCount <= 1 && session.term.screen.ToggleFold(row)
	return ui.EventResult{Handled: true, Repaint: repaint}, true
}

// The palette is the keyboard's way to a fold: the boxes are for the pointer and
// the assistive tools that press buttons, and the terminal owns every key.
var (
	hitFoldLastOutput  = tree.ID(shellHits, "terminal-fold-last")
	hitUnfoldAllOutput = tree.ID(shellHits, "terminal-unfold-all")
)

// foldPaletteEntries lists the palette's fold commands, which exist while the
// gutter that carries the folds does.
func (state *State) foldPaletteEntries(palette *commandPalette) {
	if state.terminalGutter == gutterOff {
		return
	}
	palette.add(state.messages.Tools.FoldLastOutput, hitFoldLastOutput)
	palette.add(state.messages.Tools.UnfoldAllOutput, hitUnfoldAllOutput)
}

// foldClick is a click on a fold box that did not come from the pointer's press:
// an assistive tool pressing the button.
func (state *State) foldClick(id ui.ID) {
	if session, row, ok := state.foldBoxOf(id); ok {
		session.term.screen.ToggleFold(row)
	}
}

// foldCommand runs a fold command of the palette on the active terminal.
func (state *State) foldCommand(id ui.ID) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.term == nil {
		return
	}
	switch id {
	case hitFoldLastOutput:
		session.term.screen.ToggleLastFold()
	case hitUnfoldAllOutput:
		session.term.screen.UnfoldAll()
	}
}
