package workbench

import (
	"math"

	"github.com/vivaui/viva/gfx"

	"motty/internal/termsize"
)

// The spacing between terminal rows: how tall each line is drawn relative to the
// font. Compact packs more onto the screen, roomy gives the text breathing room.
type lineHeight uint8

const (
	lineHeightNormal lineHeight = iota
	lineHeightCompact
	lineHeightRoomy
)

// factor is the multiplier a row's height is drawn at on top of the font's cell.
// Normal is exactly the font's own line box so the default looks as it always
// has; the other two step away from it.
func (mode lineHeight) factor() float32 {
	switch mode {
	case lineHeightCompact:
		return lineHeightCompactFactor
	case lineHeightRoomy:
		return lineHeightRoomyFactor
	}
	return lineHeightNormalFactor
}

func (mode lineHeight) String() string {
	switch mode {
	case lineHeightCompact:
		return "compact"
	case lineHeightRoomy:
		return "roomy"
	}
	return "normal"
}

func lineHeightFromString(value string) lineHeight {
	switch value {
	case "compact":
		return lineHeightCompact
	case "roomy":
		return lineHeightRoomy
	}
	return lineHeightNormal
}

// normalizeTerminalLineHeight keeps a saved or requested spacing one of the
// three the interface offers; anything else (a damaged file) is the default.
func normalizeTerminalLineHeight(value lineHeight) lineHeight {
	switch value {
	case lineHeightCompact, lineHeightNormal, lineHeightRoomy:
		return value
	}
	return lineHeightNormal
}

var (
	lineHeightNormalFactor  float32 = 1.0
	lineHeightCompactFactor float32 = 0.9
	lineHeightRoomyFactor   float32 = 1.25
)

// setTerminalLineHeight is the user choosing denser or airier rows; the next
// paint measures the cell with the new factor and every pane refits.
func (state *State) setTerminalLineHeight(mode lineHeight) bool {
	mode = normalizeTerminalLineHeight(mode)
	if state.terminalLineHeight == mode {
		return false
	}
	state.terminalLineHeight = mode
	return true
}

// measureTerminalCell is the terminal's cell at the current font, size and line
// spacing, measured on the window's own canvas: its text engine knows the advance
// and the line box of the font that draws the terminal, so the columns and rows are
// the ones that font gives. Until the first paint the cell is the reference estimate
// the state was made with. A changed cell refits every terminal, whichever setting
// (font, size, spacing, display scale, installed fonts) moved it.
func (state *State) measureTerminalCell(canvas *gfx.Canvas) {
	width, height := canvas.MonoCellSize(state.terminalFontSize, state.terminalFontFace)
	height *= state.terminalLineHeight.factor()
	if width == state.cellWidth && height == state.cellHeight {
		state.syncCellPixels()
		return
	}
	state.cellWidth, state.cellHeight = width, height
	state.fitTerminals()
	state.syncCellPixels()
}

// cellPixels is one character cell in device pixels: the cell the font measured,
// in the units a pty's window size and the terminal's pixel size reports use. A
// HiDPI display draws each logical pixel as more than one, and a program sizing
// an image by the cell is asking about the screen it is shown on, not about the
// layout's units.
func (state *State) cellPixels() (width, height int) {
	scale := state.pixelScale
	if scale <= 0 {
		scale = 1
	}
	return int(math.Round(float64(state.cellWidth) * float64(scale))),
		int(math.Round(float64(state.cellHeight) * float64(scale)))
}

// terminalSize is the size of a cols×rows grid at the current cell.
func (state *State) terminalSize(cols, rows int) termsize.Size {
	width, height := state.cellPixels()
	return termsize.Size{Cols: cols, Rows: rows, PixelWidth: cols * width, PixelHeight: rows * height}
}

// syncCellPixels hands the cell in device pixels to every screen, whose size
// reports answer from it, and re-tells every shell whose text area in pixels
// moved. A font, line-spacing or display-scale change moves the pixels even when
// the grid stays the same, and a program that lays images out by the cell would
// otherwise keep the old one until the next window resize.
func (state *State) syncCellPixels() {
	width, height := state.cellPixels()
	if width == state.cellPixelWidth && height == state.cellPixelHeight {
		return
	}
	state.cellPixelWidth, state.cellPixelHeight = width, height
	for index := range state.sessions {
		state.retellPixels(state.sessions[index].term)
	}
	if login := state.ai.login; login != nil {
		state.retellPixels(login.term)
	}
}

// retellPixels gives one terminal the current cell and tells its shell the text
// area that cell gives. A shell already waiting to hear a size is left to
// settle, which takes the pixels as they are then.
func (state *State) retellPixels(t *terminal) {
	if t == nil || t.screen == nil {
		return
	}
	t.screen.SetCellPixels(state.cellPixelWidth, state.cellPixelHeight)
	if t.shell == nil || t.ended || t.shellWaiting {
		return
	}
	if size := state.terminalSize(t.screen.Size()); size != t.shellSize {
		state.tellShellSize(t, size)
	}
}
