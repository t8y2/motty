package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// hostMenuHits is the place of the controls this file paints.
var hostMenuHits = tree.Feature("host-menu")

// The host menu's hit identities, beside the tab menu's.
var (
	hitHostMenuDismiss = tree.ID(hostMenuHits, "host-menu-dismiss")
	hitHostMenuConnect = tree.ID(hostMenuHits, "host-menu-connect")
	hitHostMenuSFTP    = tree.ID(hostMenuHits, "host-menu-sftp")
	hitHostMenuEdit    = tree.ID(hostMenuHits, "host-menu-edit")
	hitHostMenuCopy    = tree.ID(hostMenuHits, "host-menu-copy")
	hitHostMenuMove    = tree.ID(hostMenuHits, "host-menu-move")
	hitHostMenuDelete  = tree.ID(hostMenuHits, "host-menu-delete")
)

// hostMenu is the right-click menu of a host, in the list or the cards: which
// host, and where it was asked for. Closed, it costs nothing.
type hostMenu struct {
	open    bool
	host    uint32
	checked bool
	at      ui.Point
}

// groupMenuAnchor is where the Move group picker opens: under the button of the
// action row over the list, or at the point a host's own menu was opened at,
// where the picker takes that menu's place.
type groupMenuAnchor struct {
	button ui.Rect  // the action row's button
	point  ui.Point // the point of the host menu that handed over
	menu   bool     // that menu asked, so the point counts, not the button
}

// hostMenuPointer closes an open menu on a secondary press outside it. Opening
// it is the paint of a host row (openHostMenuOn), where the row is declared.
func (state *State) hostMenuPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button == ui.PointerButtonNone || event.Button == ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	if state.hostMenu.open && event.Target == hitHostMenuDismiss {
		state.hostMenu.open = false
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// openHostOn is called while painting a host row: when this paint is the event
// pass of a double click on it, the host is connected. Consuming the press keeps
// its release from clicking whatever the new page put under the pointer.
func (state *State) openHostOn(canvas *ui.Canvas, id ui.ID, host Host) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary || event.ClickCount != 2 || state.modal != modalNone || state.activeSection != sectionHosts {
		return
	}
	state.selectedHostID = host.ID
	state.newHostSession(host)
	canvas.Consume()
}

// openHostMenuOn is called while painting a host row: when this paint is the event
// pass of a secondary press on that row it opens the host's menu there. A right
// click never checks a host: an unchecked host is the menu's temporary target,
// while a checked host keeps the checked set as the target of group actions.
func (state *State) openHostMenuOn(canvas *ui.Canvas, id ui.ID, host Host) {
	event, ok := canvas.PointerOn(id)
	if !ok || event.Kind != ui.PointerDown || event.Button != ui.PointerButtonSecondary || state.modal != modalNone || state.activeSection != sectionHosts {
		return
	}
	state.selectedHostID = host.ID
	state.hostMenu = hostMenu{open: true, host: host.ID, checked: state.hostSelected(host.ID), at: ui.Point{X: event.X, Y: event.Y}}
	canvas.Consume()
}

// hostRowEvents is what a host row does with the pointer events aimed at it.
func (state *State) hostRowEvents(canvas *ui.Canvas, id ui.ID, host Host) {
	state.openHostMenuOn(canvas, id, host)
	state.openHostOn(canvas, id, host)
}

func (state *State) paintHostMenu(canvas *ui.Canvas, viewport ui.Rect) {
	if !state.hostMenu.open {
		return
	}
	if _, ok := state.hostByID(state.hostMenu.host); !ok || state.modal != modalNone {
		state.hostMenu.open = false
		return
	}
	messages := state.messages
	several := state.hostMenu.checked && len(state.hostSelections) > 1
	icon := func(kind uint8) ui.Icon { return lucideIcons[kind] }
	var rows menuRows
	rows.add(components.ContextMenuItem{ID: hitHostMenuConnect, Label: messages.Connect, Icon: icon(iconPlug), Disabled: several})
	if host, ok := state.hostByID(state.hostMenu.host); ok && host.isSSH() {
		rows.add(components.ContextMenuItem{ID: hitHostMenuSFTP, Label: messages.SFTPWindow.Open, Icon: icon(iconExternal), Disabled: several || state.options.OpenContentWindow == nil})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitHostMenuEdit, Label: messages.EditHost, Icon: icon(iconPencil), Disabled: several})
	rows.add(components.ContextMenuItem{ID: hitHostMenuCopy, Label: messages.CopyHost, Icon: icon(iconCopy), Disabled: several})
	rows.add(components.ContextMenuItem{ID: hitHostMenuMove, Label: messages.MoveHostGroup, Icon: icon(iconFolderOpen)})
	rows.add(components.ContextMenuItem{ID: hitHostMenuDelete, Label: messages.DeleteHost, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive})
	width, height := rows.size(canvas, state.contextMenuTheme())
	bounds := state.popupAt(canvas, viewport, state.hostMenu.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitHostMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitHostMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

// clickHostMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickHostMenu(id ui.ID) bool {
	if !state.hostMenu.open {
		return false
	}
	hostID := state.hostMenu.host
	state.hostMenu.open = false
	host, ok := state.hostByID(hostID)
	if !ok {
		return true
	}
	switch id {
	case hitHostMenuConnect:
		state.selectedHostID = host.ID
		state.newHostSession(host)
	case hitHostMenuSFTP:
		state.openHostSFTPWindow(host)
	case hitHostMenuEdit:
		state.openEditHostID(host.ID)
	case hitHostMenuCopy:
		state.openCopyHostID(host.ID)
	case hitHostMenuMove:
		state.captureHostMenuActionIDs(host.ID)
		// The picker replaces this menu in place: it opens where the menu did,
		// not under the action row the host list may not even be showing.
		state.groupMenu = groupMenuAnchor{point: state.hostMenu.at, menu: true}
		state.hostGroupMenu = len(state.hostActionIDs) > 0
	case hitHostMenuDelete:
		state.captureHostMenuActionIDs(host.ID)
		state.openDeleteCapturedHosts()
	}
	return true
}

func (state *State) captureHostMenuActionIDs(hostID uint32) {
	if state.hostMenu.checked {
		state.captureHostActionIDs()
		return
	}
	state.hostActionIDs = state.hostActionIDs[:0]
	if _, ok := state.hostByID(hostID); ok {
		state.hostActionIDs = append(state.hostActionIDs, hostID)
	}
}
