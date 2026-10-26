package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabMenuHits is the place of the controls this file paints.
var tabMenuHits = tree.Feature("tab-menu")

// The tab menu's hit identities: a block of their own, beside the terminal menu's.
var (
	hitTabMenuDismiss     = tree.ID(tabMenuHits, "tab-menu-dismiss")
	hitTabMenuClose       = tree.ID(tabMenuHits, "tab-menu-close")
	hitTabMenuCloseOthers = tree.ID(tabMenuHits, "tab-menu-close-others")
	hitTabMenuCloseRight  = tree.ID(tabMenuHits, "tab-menu-close-right")
	hitTabMenuReconnect   = tree.ID(tabMenuHits, "tab-menu-reconnect")
	hitTabMenuRename      = tree.ID(tabMenuHits, "tab-menu-rename")
)

// The rename dialog's identities, after the menu's.
var (
	hitTabRenameDismiss = tree.ID(tabMenuHits, "tab-rename-dismiss")
	hitTabRenameClose   = tree.ID(tabMenuHits, "tab-rename-close")
	hitTabRenameCancel  = tree.ID(tabMenuHits, "tab-rename-cancel")
	hitTabRenameSubmit  = tree.ID(tabMenuHits, "tab-rename-submit")
	hitTabRenameField   = tree.ID(tabMenuHits, "tab-rename-field")
)

// The confirmation before closing several tabs at once.
var (
	hitTabsCloseDismiss = tree.ID(tabMenuHits, "tabs-close-dismiss")
	hitTabsCloseClose   = tree.ID(tabMenuHits, "tabs-close-close")
	hitTabsCloseCancel  = tree.ID(tabMenuHits, "tabs-close-cancel")
	hitTabsCloseConfirm = tree.ID(tabMenuHits, "tabs-close-confirm")
)

// tabMenu is the right-click menu of a tab: which tab, and where it was asked for.
// Closed, it costs nothing.
type tabMenu struct {
	open      bool
	workspace layoutcore.WorkspaceID
	at        ui.Point
}

// tabMenuPointer closes an open menu on a secondary press outside it (the
// backdrop swallows the press; only a primary one is also a click). Opening it is
// the paint of a tab (openTabMenuOn).
func (state *State) tabMenuPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button == ui.PointerButtonNone || event.Button == ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	if state.tabMenu.open && event.Target == hitTabMenuDismiss {
		state.tabMenu.open = false
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// openTabMenuOn is called while painting a tab and its close button: when this
// paint is the event pass of a secondary press on either, it opens the tab's menu.
func (state *State) openTabMenuOn(canvas *ui.Canvas, workspace layoutcore.WorkspaceID, tab, closeButton ui.ID) {
	event, ok := canvas.PointerOn(tab)
	if !ok {
		event, ok = canvas.PointerOn(closeButton)
	}
	if !ok || event.Kind != ui.PointerDown || event.Button != ui.PointerButtonSecondary || state.modal != modalNone {
		return
	}
	if _, ok := state.workspaceTab(workspace); !ok {
		return
	}
	state.tabMenu = tabMenu{open: true, workspace: workspace, at: ui.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

// workspaceTab finds a tab by workspace, with its position among the tabs.
func (state *State) workspaceTab(id layoutcore.WorkspaceID) (int, bool) {
	for index, tab := range state.workspaceTabs {
		if tab.ID == id {
			return index, true
		}
	}
	return 0, false
}

// tabMenuSession is the session shown in the menu's tab, if it has one.
func (state *State) tabMenuSession() (*Session, bool) {
	paneID := state.deck.ActivePane(state.tabMenu.workspace)
	pane, ok := state.deck.Pane(state.tabMenu.workspace, paneID)
	if !ok || pane.SessionID == 0 {
		return nil, false
	}
	return state.sessionByIDPointer(uint32(pane.SessionID))
}

func (state *State) paintTabMenu(canvas *ui.Canvas, viewport ui.Rect) {
	if !state.tabMenu.open {
		return
	}
	index, ok := state.workspaceTab(state.tabMenu.workspace)
	if !ok {
		state.tabMenu.open = false
		return
	}
	messages := state.messages
	session, hasSession := state.tabMenuSession()
	reconnect := hasSession && state.canReconnect(session)
	var keys [6]string
	closeKeys := state.shortcuts.chords[shortcutCloseTab].keys(keys[:0])
	icon := func(kind uint8) ui.Icon { return lucideIcons[kind] }
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitTabMenuRename, Label: messages.MenuRenameTab, Icon: icon(iconPencil)})
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitTabMenuClose, Label: messages.LabelCloseTab, Icon: icon(iconClose), Keys: closeKeys})
	rows.add(components.ContextMenuItem{ID: hitTabMenuCloseOthers, Label: messages.MenuCloseOtherTabs, Icon: icon(iconCloseOthers), Disabled: len(state.workspaceTabs) < 2})
	rows.add(components.ContextMenuItem{ID: hitTabMenuCloseRight, Label: messages.MenuCloseTabsToRight, Icon: icon(iconCloseRight), Disabled: index == len(state.workspaceTabs)-1})
	if reconnect {
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitTabMenuReconnect, Label: messages.SessionReconnect, Icon: icon(iconRefresh)})
	}
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, state.tabMenu.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitTabMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitTabMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

// clickTabMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickTabMenu(id ui.ID) bool {
	if !state.tabMenu.open {
		return false
	}
	workspace := state.tabMenu.workspace
	state.tabMenu.open = false
	index, ok := state.workspaceTab(workspace)
	if !ok {
		return true
	}
	switch id {
	case hitTabMenuRename:
		state.openTabRename(workspace)
	case hitTabMenuClose:
		state.closeWorkspace(workspace)
	case hitTabMenuCloseOthers:
		state.askCloseTabs(workspace, func(position int) bool { return position != index })
	case hitTabMenuCloseRight:
		state.askCloseTabs(workspace, func(position int) bool { return position > index })
	case hitTabMenuReconnect:
		if session, ok := state.tabMenuSessionFor(workspace); ok && state.canReconnect(session) {
			state.activateWorkspace(workspace)
			state.reconnect(session)
			state.save()
		}
	}
	return true
}

func (state *State) tabMenuSessionFor(workspace layoutcore.WorkspaceID) (*Session, bool) {
	state.tabMenu.workspace = workspace
	return state.tabMenuSession()
}

// closeTabs closes the tabs, by position, that pick selects, and keeps the tab
// the menu was opened on showing: closing its neighbours must not leave the user
// looking at a different one.
func (state *State) closeTabs(keep layoutcore.WorkspaceID, pick func(position int) bool) {
	var doomed [maxRestoredSessions]layoutcore.WorkspaceID
	count := 0
	for position, tab := range state.workspaceTabs {
		if pick(position) && count < len(doomed) {
			doomed[count] = tab.ID
			count++
		}
	}
	if count == 0 {
		return
	}
	state.activateWorkspace(keep)
	for _, id := range doomed[:count] {
		state.closeWorkspace(id)
	}
	if state.deck.ActiveWorkspace() != keep {
		state.activateWorkspace(keep)
	}
	state.save()
}
