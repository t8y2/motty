package workbench

import "motty/internal/term"

// terminalIntense is how bold text is told apart from the rest, the choice
// Windows Terminal calls intenseTextStyle. Programs mark a directory, an
// address or a heading with bold and one of the eight base colors (ls writes
// 01;34), and terminals have long drawn that in the bright variant of the color
// (xterm's boldColors, iTerm2's "Brighten bold text") and, usually, in a heavier
// face too. It is a property of the terminal, not of a color scheme, so every
// scheme gets it.
type terminalIntense uint8

const (
	// intenseBoth draws bold text in the bright color and a bold face.
	intenseBoth terminalIntense = iota
	// intenseBright only brightens the color, as Windows Terminal does.
	intenseBright
	// intenseBold only changes the face.
	intenseBold
	// intensePlain leaves bold text as it is.
	intensePlain
)

// defaultTerminalIntense keeps the bold face every earlier release drew and adds
// the bright color the other terminals have, so nothing a program emphasized
// loses its emphasis.
const defaultTerminalIntense = intenseBoth

func (style terminalIntense) String() string {
	switch style {
	case intenseBright:
		return "bright"
	case intenseBold:
		return "bold"
	case intensePlain:
		return "plain"
	}
	return "both"
}

func terminalIntenseFromString(value string) terminalIntense {
	switch value {
	case "both":
		return intenseBoth
	case "bright":
		return intenseBright
	case "bold":
		return intenseBold
	case "plain":
		return intensePlain
	}
	// Unset means the state predates the choice: follow the default.
	return defaultTerminalIntense
}

func (style terminalIntense) brightens() bool { return style == intenseBoth || style == intenseBright }
func (style terminalIntense) embolden() bool  { return style == intenseBoth || style == intenseBold }

// brighten maps one of the eight base palette colors to its bright variant. The
// 256-color cube and true color are exactly what the program asked for and are
// left alone.
func brighten(color term.Color) term.Color {
	if color.Kind() == term.PaletteColor && color.Index() < 8 {
		return term.Indexed(color.Index() + 8)
	}
	return color
}

func (state *State) setTerminalIntense(style terminalIntense) bool {
	if state.terminalIntense == style {
		return false
	}
	state.terminalIntense = style
	state.rebuildTerminalLook()
	return true
}
