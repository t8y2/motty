package workbench

import (
	"bytes"

	"github.com/vivaui/viva/ui"
	"motty/internal/term"
)

// A click on the line being typed moves the shell's cursor there, as Warp and
// iTerm2 (with Option) do. The terminal cannot place another program's
// cursor, so this sends cursor keys and lets the shell's line editor act on
// them; see term.Terminal.InputMove for when that is sound. Programs that took
// the mouse already had the click, and a press that drags is a selection, not a click.

// terminalInputClick reports whether event asks for the cursor move.
func terminalInputClick(event ui.PointerEvent) bool {
	return event.Kind == ui.PointerUp && event.Button == ui.PointerButtonPrimary &&
		(event.Modifiers == 0 || event.Modifiers == ui.ModifierAlt)
}

// moveInputCursor sends the cursor keys that take the shell's cursor to
// position. It reports whether the click was used, which includes a click on
// the cursor itself.
func (state *State) moveInputCursor(session *Session, position term.Position) bool {
	terminal := session.term
	// The shell must have reported its prompt and be waiting at it: Windows
	// shells mark the prompt but not command starts, so without the C marker a
	// running program would receive the keys.
	if terminal.shell == nil || terminal.ended || !terminal.commandReporting || terminal.screen.Modes().AltScreen {
		return false
	}
	// Output still in flight can move the cursor the distance is measured from.
	if terminal.takeOutput != nil {
		terminal.takeOutput()
	}
	moves, ok := terminal.screen.InputMove(position)
	if !ok {
		return false
	}
	if moves == 0 {
		return true
	}
	key := ui.KeyRight
	if moves < 0 {
		key, moves = ui.KeyLeft, -moves
	}
	press := term.EncodeKey(key, 0, "", terminal.screen.Modes().AppCursorKeys)
	terminal.send(bytes.Repeat(press, moves))
	return true
}
