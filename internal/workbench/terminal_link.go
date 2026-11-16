package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/term"
)

func (state *State) openTerminalLink(target string) {
	if err := state.options.OpenURL(target); err != nil {
		state.status = fmt.Sprintf(state.messages.StatusOpenURLFailedFmt, err)
	}
}

// terminalLinkOpens reports whether a pointer with these modifiers works on
// links: a click opens one only with the platform's shortcut modifier held
// (Command on macOS, Control elsewhere), unless the user chose plain clicks.
// Shift is the terminal's own selection and never works on links. Other
// modifiers stay free for what they may mean to a pointer later.
func (state *State) terminalLinkOpens(modifiers gfx.Modifiers) bool {
	if modifiers&gfx.ModifierShift != 0 {
		return false
	}
	return state.terminalLinkPlainClick || modifiers == keymap.Primary
}

// terminalLinks tracks the link under the pointer. Detection stays dormant
// until pointer interaction asks for it, and then looks at the soft-wrapped
// group under the pointer, not the whole scrollback: a hover over a busy
// terminal must not rescan thousands of lines per pointer event.
type terminalLinks struct {
	hovered  term.Link
	hasHover bool
	version  uint64 // the screen version the hover was found at
	// emptyLine remembers the line whose group had no link at version, so
	// moving the pointer along a linkless line does not scan it again.
	emptyLine int
	empty     bool
	pressed   term.Link
	pressing  bool
}

func (links *terminalLinks) hover(screen *term.Terminal, position term.BufferPosition) bool {
	version := screen.Version()
	if links.version == version {
		if links.hasHover && terminalLinkContains(links.hovered, position) {
			return false
		}
		if links.empty && links.emptyLine == position.Line {
			return false
		}
	}
	link, ok := screen.LinksAt(position)
	changed := ok != links.hasHover || link != links.hovered
	links.hovered, links.hasHover, links.version = link, ok, version
	links.empty, links.emptyLine = !ok, position.Line
	if changed {
		links.pressing = false
	}
	return changed
}

func (links *terminalLinks) press(screen *term.Terminal, position term.BufferPosition) bool {
	changed := links.hover(screen, position)
	links.pressing = links.hasHover
	if links.pressing {
		links.pressed = links.hovered
	} else {
		links.pressed = term.Link{}
	}
	return changed
}

func (links *terminalLinks) release(screen *term.Terminal, position term.BufferPosition) (string, bool, bool) {
	changed := links.hover(screen, position)
	pressed, pressing := links.pressed, links.pressing
	links.pressed = term.Link{}
	links.pressing = false
	if !pressing || !links.hasHover || links.hovered != pressed {
		return "", false, changed
	}
	return pressed.Target, true, changed
}

func (links *terminalLinks) clear() bool {
	changed := links.hasHover
	links.hovered, links.hasHover = term.Link{}, false
	links.empty, links.emptyLine = false, 0
	links.pressed = term.Link{}
	links.pressing = false
	return changed
}

func (links *terminalLinks) hoveredLink(screen *term.Terminal) (term.Link, bool) {
	if !links.hasHover || links.version != screen.Version() {
		return term.Link{}, false
	}
	return links.hovered, true
}

func terminalLinkContains(link term.Link, position term.BufferPosition) bool {
	return !terminalBufferPositionLess(position, link.Start) && terminalBufferPositionLess(position, link.End)
}

func terminalBufferPositionLess(left, right term.BufferPosition) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Column < right.Column
}

func terminalLinkPosition(screen *term.Terminal, position term.Position) term.BufferPosition {
	return term.BufferPosition{Line: screen.ViewLine(position.Row), Column: position.Column}
}
