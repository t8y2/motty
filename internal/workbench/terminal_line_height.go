package workbench

import "github.com/vivaui/viva/gfx"

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
		return
	}
	state.cellWidth, state.cellHeight = width, height
	state.fitTerminals()
}
