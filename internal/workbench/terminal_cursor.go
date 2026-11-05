package workbench

// How thick the thin cursors draw: the bar and the underline. The block cursor
// fills its cell and does not follow this choice.
type cursorThickness uint8

const (
	cursorNormal cursorThickness = iota
	cursorThin
	cursorThick
)

// bar is the bar cursor's width in logical pixels.
func (mode cursorThickness) bar() float32 {
	switch mode {
	case cursorThin:
		return 2
	case cursorThick:
		return 5
	}
	return 3
}

// underline is the underline cursor's height in logical pixels.
func (mode cursorThickness) underline() float32 {
	switch mode {
	case cursorThin:
		return 1
	case cursorThick:
		return 3
	}
	return 2
}

func (mode cursorThickness) String() string {
	switch mode {
	case cursorThin:
		return "thin"
	case cursorThick:
		return "thick"
	}
	return "normal"
}

// cursorThicknessFromString reads the saved choice; anything else (a damaged
// file, an older build) is the default.
func cursorThicknessFromString(value string) cursorThickness {
	switch value {
	case "thin":
		return cursorThin
	case "thick":
		return cursorThick
	}
	return cursorNormal
}

func (state *State) setTerminalCursorThickness(mode cursorThickness) bool {
	if state.terminalCursorThickness == mode {
		return false
	}
	state.terminalCursorThickness = mode
	return true
}
