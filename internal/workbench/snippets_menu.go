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
	open  bool
	pick  bool // lists the terminals to send to, instead of the row's actions
	index int
	at    gfx.Point
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
	state.snippetMenu = snippetMenu{open: true, index: index, at: gfx.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

func (state *State) declareSnippetMenu(c ui.C) {
	menu := &state.snippetMenu
	if !menu.open {
		return
	}
	if menu.index >= len(state.snippets.list) || state.modal != modalNone || state.activeSection != sectionSnippets {
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
		state.snippetActionRows(&rows)
	}
	state.declareRowsMenu(c, &menu.open, menu.at, hitSnippetMenuDismiss, &rows, state.chooseSnippetMenu)
}

// snippetActionRows is what a snippet row's menu offers.
func (state *State) snippetActionRows(rows *menuRows) {
	m := state.messages.Tools
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuSend, Label: m.SnippetSend, Icon: icon(iconPlay)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuUp, Label: m.SnippetMoveUp, Icon: icon(iconArrowUp), Disabled: state.snippetNeighbor(state.snippetMenu.index, -1) < 0})
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuDown, Label: m.SnippetMoveDown, Icon: icon(iconChevronDown), Disabled: state.snippetNeighbor(state.snippetMenu.index, 1) < 0})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuEdit, Label: m.SnippetEdit, Icon: icon(iconPencil)})
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
		state.sendSnippetFromPage(index, menu.at)
	case hitSnippetMenuEdit:
		state.openSnippetDialog(index)
	case hitSnippetMenuUp:
		state.moveSnippet(index, -1)
	case hitSnippetMenuDown:
		state.moveSnippet(index, 1)
	case hitSnippetMenuDelete:
		state.openSnippetDelete(index)
	}
}
