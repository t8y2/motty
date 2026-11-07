package workbench

import (
	"github.com/vivaui/viva/components"
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
	at    ui.Point
}

// snippetMenuPointer closes an open menu on a secondary press outside it. Opening
// it is the paint of a row (openSnippetMenuOn), where the row is declared.
func (state *State) snippetMenuPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button == ui.PointerButtonNone || event.Button == ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	if state.snippetMenu.open && event.Target == hitSnippetMenuDismiss {
		state.snippetMenu.open = false
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// openSnippetMenuOn is called while painting a snippet row: when this paint is the
// event pass of a secondary press on that row it selects the row and opens its
// menu there.
func (state *State) openSnippetMenuOn(canvas *ui.Canvas, id ui.ID, index int) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != ui.PointerDown || event.Button != ui.PointerButtonSecondary || state.modal != modalNone || state.activeSection != sectionSnippets {
		return
	}
	state.snippets.selected = index
	state.snippetMenu = snippetMenu{open: true, index: index, at: ui.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

func (state *State) paintSnippetMenu(canvas *ui.Canvas, viewport ui.Rect) {
	menu := &state.snippetMenu
	if !menu.open {
		return
	}
	if menu.index >= len(state.snippets.list) || state.modal != modalNone || state.activeSection != sectionSnippets {
		menu.open = false
		return
	}
	m := state.messages.Tools
	icon := func(kind uint8) ui.Icon { return lucideIcons[kind] }
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
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, menu.at, width, height, 6)
	popup := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitSnippetMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitSnippetMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(popup.Bounds())
	rows.paint(&popup, nil)
	popup.End()
}

// snippetActionRows is what a snippet row's menu offers.
func (state *State) snippetActionRows(rows *menuRows) {
	m := state.messages.Tools
	icon := func(kind uint8) ui.Icon { return lucideIcons[kind] }
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuSend, Label: m.SnippetSend, Icon: icon(iconPlay)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuUp, Label: m.SnippetMoveUp, Icon: icon(iconArrowUp), Disabled: state.snippetNeighbor(state.snippetMenu.index, -1) < 0})
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuDown, Label: m.SnippetMoveDown, Icon: icon(iconChevronDown), Disabled: state.snippetNeighbor(state.snippetMenu.index, 1) < 0})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuEdit, Label: m.SnippetEdit, Icon: icon(iconPencil)})
	rows.add(components.ContextMenuItem{ID: hitSnippetMenuDelete, Label: m.SnippetDelete, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive})
}

// clickSnippetMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickSnippetMenu(id ui.ID) bool {
	if !state.snippetMenu.open {
		return false
	}
	menu := state.snippetMenu
	index := menu.index
	state.snippetMenu.open = false
	if id.Scope() == hitSnippetPickList {
		if session, ok := state.sessionByIDPointer(uint32(item(id))); ok && session.term != nil {
			state.sendSnippetPicked(index, session)
		}
		return true
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
		state.deleteSnippet(index)
	}
	return true
}
