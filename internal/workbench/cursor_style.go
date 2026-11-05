package workbench

import (
	"motty/internal/term"
)

// terminalCursorStyle is the shape of the terminal's cursor before a program
// asks for one with DECSCUSR. A program's own choice still wins.
type terminalCursorStyle uint8

const (
	cursorBlock terminalCursorStyle = iota
	cursorBar
	cursorUnderline
)

func (style terminalCursorStyle) String() string {
	switch style {
	case cursorBar:
		return "bar"
	case cursorUnderline:
		return "underline"
	default:
		return "block"
	}
}

func terminalCursorStyleFromString(value string) terminalCursorStyle {
	switch value {
	case "bar":
		return cursorBar
	case "underline":
		return cursorUnderline
	default:
		return cursorBlock
	}
}

// termCursorStyle maps the choice to what the terminal emulator draws.
func (style terminalCursorStyle) termCursorStyle() term.CursorStyle {
	switch style {
	case cursorBar:
		return term.CursorBar
	case cursorUnderline:
		return term.CursorUnderline
	default:
		return term.CursorBlock
	}
}

// setTerminalCursorStyle applies the choice to the sessions already open; the
// ones opened later take it at creation (see connection.go).
func (state *State) setTerminalCursorStyle(style terminalCursorStyle) bool {
	if state.terminalCursorStyle == style {
		return false
	}
	state.terminalCursorStyle = style
	state.applyTerminalCursorStyle()
	return true
}

func (state *State) applyTerminalCursorStyle() {
	for index := range state.sessions {
		if t := state.sessions[index].term; t != nil {
			t.screen.SetDefaultCursor(state.terminalCursorStyle.termCursorStyle())
		}
	}
}
