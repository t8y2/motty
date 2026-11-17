package workbench

import "strconv"

// The margin between the terminal's grid and the edges of its pane.
type terminalPad uint8

const (
	padNormal terminalPad = iota
	padCompact
	padRoomy
)

// terminalPadding is the margin, on the sides and above and below the grid.
type terminalPadding struct {
	x, top, bottom float32
	// gutter is the width of the line-number/time strip left of the grid.
	gutter float32
}

func (pad terminalPad) padding() terminalPadding {
	switch pad {
	case padCompact:
		return terminalPadding{x: 6, top: 4, bottom: 4}
	case padRoomy:
		return terminalPadding{x: 24, top: 16, bottom: 16}
	}
	return terminalPadding{x: termPadX, top: termPadTop, bottom: termPadBottom}
}

func (pad terminalPad) String() string {
	switch pad {
	case padCompact:
		return "compact"
	case padRoomy:
		return "roomy"
	}
	return "normal"
}

func terminalPadFromString(value string) terminalPad {
	switch value {
	case "compact":
		return padCompact
	case "roomy":
		return padRoomy
	}
	return padNormal
}

func (state *State) setTerminalPadding(pad terminalPad) bool {
	if state.terminalPad == pad {
		return false
	}
	state.terminalPad = pad
	state.applyTerminalPadding()
	return true
}

// applyTerminalPadding applies the margin the page's choice already wrote.
func (state *State) applyTerminalPadding() {
	state.fitTerminals()
}

// padding is the margin the terminals are laid out with.
func (state *State) padding() terminalPadding {
	pad := state.terminalPad.padding()
	pad.gutter = state.gutterWidth()
	return pad
}

// fontSizeLabel is the terminal's font size as the settings page shows it. It is
// made again only when the size changes.
func (state *State) fontSizeLabel() string {
	if state.fontSizeShown != state.terminalFontSize || state.fontSizeText == "" {
		state.fontSizeShown = state.terminalFontSize
		state.fontSizeText = strconv.Itoa(int(state.terminalFontSize)) + " pt"
	}
	return state.fontSizeText
}
