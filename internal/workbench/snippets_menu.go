package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

var (
	hitSnippetMenuDismiss = tree.ID(snippetsHits, "snippet-menu-dismiss")
	hitSnippetMenuSend    = tree.ID(snippetsHits, "snippet-menu-send")
	hitSnippetMenuEdit    = tree.ID(snippetsHits, "snippet-menu-edit")
	hitSnippetMenuDelete  = tree.ID(snippetsHits, "snippet-menu-delete")
	hitSnippetMenuUp      = tree.ID(snippetsHits, "snippet-menu-up")
	hitSnippetMenuDown    = tree.ID(snippetsHits, "snippet-menu-down")
)

// snippetMenu is the right-click menu of a row of the Snippets page: which
// snippet, and where it was asked for. Closed, it costs nothing.
type snippetMenu struct {
	open    bool
	pick    bool // lists the terminals to send to, instead of the row's actions
	fromBar bool // opened from a chip of the command bar, not from the page
	index   int
	name    string // the snippet the menu was opened on, to notice a rebuilt list
	at      gfx.Point
}

// openSnippetMenuOn is called while painting a snippet row: when this paint is the
// event pass of a secondary press on that row it selects the row and opens its
// menu there.
func (state *State) openSnippetMenuOn(canvas *gfx.Canvas, id gfx.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary || state.modal != modalNone || state.activeSection != sectionSnippets {
		return
	}
	state.snippets.selected = index
	state.snippetMenu = snippetMenu{open: true, index: index, name: state.snippets.list[index].Name, at: gfx.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

// openSnippetBarMenuOn is the same for a chip of the command bar: a secondary
// press opens the snippet's own menu there, so a command added to the bar can
// still be edited or removed without leaving the terminal page.
func (state *State) openSnippetBarMenuOn(canvas *gfx.Canvas, id gfx.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != gfx.PointerDown || event.Button != gfx.PointerButtonSecondary || state.modal != modalNone {
		return
	}
	state.snippetMenu = snippetMenu{open: true, fromBar: true, index: index, name: state.snippets.list[index].Name, at: gfx.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

func (state *State) declareSnippetMenu(c ui.C) {
	state.declareSnippetFolderMenu(c)
	state.declareSnippetBlankMenu(c)
	menu := &state.snippetMenu
	if !menu.open {
		return
	}
	if menu.index < 0 || menu.index >= len(state.snippets.list) || state.snippets.list[menu.index].Name != menu.name || state.modal != modalNone ||
		(!menu.fromBar && state.activeSection != sectionSnippets) ||
		(menu.fromBar && (!state.terminalPage() || !state.commands.shown || !state.commandsBarAvailable())) {
		menu.open = false
		return
	}
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	if menu.pick {
		for i := range state.sessions {
			session := &state.sessions[i]
			if session.term == nil {
				continue
			}
			current := ""
			if session.ID == state.activeSessionID {
				current = m.SnippetSendCurrent
			}
			rows.add(components.ContextMenuItem{ID: hitSnippetPickList.At(int(session.ID)), Label: session.Title, Icon: icon(iconTerminal), Shortcut: current})
		}
	} else {
		state.snippetActionRows(&rows, !menu.fromBar)
	}
	state.declareRowsMenu(c, &menu.open, menu.at, hitSnippetMenuDismiss, &rows, state.chooseSnippetMenu)
}

// snippetActionRows is what a snippet row's menu offers. Moving reorders by the
// Snippets page's own filter and sort, so from the command bar only the actions
// that make sense without the page are offered.
func (state *State) snippetActionRows(rows *menuRows, moves bool) {
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuSend, Label: m.SnippetSend, Icon: icon(iconPlay)})
	if moves {
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitSnippetMenuUp, Label: m.SnippetMoveUp, Icon: icon(iconArrowUp), Disabled: state.snippetNeighbor(state.snippetMenu.index, -1) < 0})
		rows.add(components.ContextMenuItem{ID: hitSnippetMenuDown, Label: m.SnippetMoveDown, Icon: icon(iconChevronDown), Disabled: state.snippetNeighbor(state.snippetMenu.index, 1) < 0})
	}
	rows.separate()
	if !moves {
		rows.add(components.ContextMenuItem{ID: hitSnippetMenuEdit, Label: m.SnippetEdit, Icon: icon(iconPencil)})
	}
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuDelete, Label: m.SnippetDelete, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive})
}

// chooseSnippetMenu runs a choice of the menu, which closed with it.
func (state *State) chooseSnippetMenu(id gfx.ID) {
	menu := state.snippetMenu
	index := menu.index
	if id.Scope() == hitSnippetPickList {
		if session, ok := state.sessionByIDPointer(uint32(item(id))); ok && session.term != nil {
			state.sendSnippetPicked(index, session)
		}
		return
	}
	switch id {
	case hitSnippetMenuSend:
		if menu.fromBar {
			if state.sendSnippet(index) {
				state.returnKeyboardToTerminal()
			}
		} else {
			state.sendSnippetFromPage(index, menu.at)
		}
	case hitSnippetMenuEdit:
		state.focusInlineSnippet(index)
	case hitSnippetMenuUp:
		state.moveSnippet(index, -1)
	case hitSnippetMenuDown:
		state.moveSnippet(index, 1)
	case hitSnippetMenuDelete:
		state.openSnippetDelete(index)
	}
}
