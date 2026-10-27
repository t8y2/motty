package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

// sftpMenuHits is the place of the controls this file paints.
var sftpMenuHits = tree.Feature("sftp-menu")

// The file panel's right-click menu. Each row stands for a button of the bar under
// the list and runs the same action; it has its own identity because both are on
// the screen at once.
var (
	hitSFTPMenuDismiss      = tree.ID(sftpMenuHits, "sftp-menu-dismiss")
	hitSFTPMenuOpen         = tree.ID(sftpMenuHits, "sftp-menu-open")
	hitSFTPMenuDownload     = tree.ID(sftpMenuHits, "sftp-menu-download")
	hitSFTPMenuDownloadHere = tree.ID(sftpMenuHits, "sftp-menu-download-here")
	hitSFTPMenuSend         = tree.ID(sftpMenuHits, "sftp-menu-send")
	hitSFTPMenuInsertPath   = tree.ID(sftpMenuHits, "sftp-menu-insert-path")
	hitSFTPMenuEdit         = tree.ID(sftpMenuHits, "sftp-menu-edit")
	hitSFTPMenuRename       = tree.ID(sftpMenuHits, "sftp-menu-rename")
	hitSFTPMenuChmod        = tree.ID(sftpMenuHits, "sftp-menu-chmod")
	hitSFTPMenuExtract      = tree.ID(sftpMenuHits, "sftp-menu-extract")
	hitSFTPMenuDelete       = tree.ID(sftpMenuHits, "sftp-menu-delete")
)

// sftpMenuAction is the button of the bar that a menu row stands for.
func sftpMenuAction(id ui.ID) (ui.ID, bool) {
	switch id {
	case hitSFTPMenuOpen:
		return hitSFTPOpenEntry, true
	case hitSFTPMenuDownload:
		return hitSFTPDownload, true
	case hitSFTPMenuDownloadHere:
		return hitSFTPDownloadHere, true
	case hitSFTPMenuSend:
		return hitSFTPSend, true
	case hitSFTPMenuInsertPath:
		return hitSFTPInsertPath, true
	case hitSFTPMenuEdit:
		return hitSFTPEditExternal, true
	case hitSFTPMenuRename:
		return hitSFTPRename, true
	case hitSFTPMenuChmod:
		return hitSFTPChmod, true
	case hitSFTPMenuExtract:
		return hitSFTPExtract, true
	case hitSFTPMenuDelete:
		return hitSFTPDelete, true
	}
	return ui.ID{}, false
}

// sftpMenu is the right-click menu of the file list: which session's panel it was
// asked in, and where. Closed, it costs nothing.
type sftpMenu struct {
	open    bool
	session uint32
	at      ui.Point
}

// sftpMenuDismissPointer closes an open menu on a secondary press outside it; the
// backdrop swallows the press, and only a primary one is also a click.
func (state *State) sftpMenuDismissPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button == ui.PointerButtonNone || event.Button == ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	if state.sftpMenu.open && event.Target == hitSFTPMenuDismiss {
		state.sftpMenu.open = false
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

// sftpMenuPointer opens the menu on a secondary press on a row of the file list,
// selecting that row first unless it is already part of the selection.
func (state *State) sftpMenuPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Kind != ui.PointerDown || event.Button != ui.PointerButtonSecondary || event.Target.Scope() != hitSFTPEntryList {
		return ui.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil || session.files.editPath != "" {
		return ui.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	index := folder.first + item(event.Target)
	if view.dialog != sftpDialogNone || index >= len(folder.rows) || view.busy && !view.local {
		return ui.EventResult{Handled: true}, true
	}
	if !folder.rows[index].selected {
		folder.selectRow(index, 0)
		view.status = fmt.Sprintf(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID, at: state.terminalMenu.pointer}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

// sftpMenuView is the panel an open menu acts on, if it is still there to act on.
func (state *State) sftpMenuView() (*Session, *sftpView, bool) {
	if !state.sftpMenu.open || state.activeSection != sectionTerminal || state.modal != modalNone || state.sftpMenu.session != state.activeSessionID {
		return nil, nil, false
	}
	session, ok := state.sessionByIDPointer(state.sftpMenu.session)
	if !ok || session.files == nil || session.files.dialog != sftpDialogNone || session.files.editPath != "" {
		return nil, nil, false
	}
	return session, session.files, true
}

func (state *State) paintSFTPMenu(canvas *ui.Canvas, viewport ui.Rect) {
	session, view, ok := state.sftpMenuView()
	if !ok {
		state.sftpMenu.open = false
		return
	}
	messages := state.messages
	count, selected := selectedSFTP(view)
	one, local := count == 1, view.local
	busy := view.busy && !local
	icon := func(kind uint8) ui.Icon { return lucideIcons[kind] }
	var rows menuRows
	if one {
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuOpen, Label: messages.SFTPOpen, Icon: icon(iconFolderOpen), Disabled: busy})
		rows.separate()
	}
	switch {
	case view.localOnly:
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuInsertPath, Label: messages.SFTPInsertPath, Icon: icon(iconTerminal), Disabled: session.term == nil || session.term.shell == nil})
	case local:
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuSend, Label: messages.Tools.SendToServer,
			Description: view.path, Value: view.path, Hint: view.path, Icon: icon(iconUpload), Disabled: view.path == ""})
	default:
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuDownload, Label: messages.SFTPDownload, Icon: icon(iconDownload), Disabled: busy})
		if view.lfolder.path != "" {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuDownloadHere, Label: messages.Tools.DownloadHere, Icon: icon(iconFileDown), Disabled: busy})
		}
	}
	if one && !local {
		if !selected.IsDir() {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuEdit, Label: messages.Tools.ExtEditButton, Icon: icon(iconExternal), Disabled: busy})
		}
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuRename, Label: messages.SFTPRename, Icon: icon(iconPencil), Disabled: busy})
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuChmod, Label: messages.SFTPPermissions, Icon: icon(iconLock), Disabled: busy})
		if _, archive := state.selectedArchive(session, view); archive {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuExtract, Label: messages.Tools.Extract, Icon: icon(iconArchive), Disabled: busy})
		}
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuDelete, Label: messages.SFTPDelete, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive, Disabled: busy})
	}

	width, height := rows.size(canvas, state.contextMenuTheme())
	// Long server paths should not stretch the menu across the whole window.
	// Viva ellipsizes the second line; its full value remains in the item's hint.
	width = min(width, 480)
	bounds := state.popupAt(canvas, viewport, state.sftpMenu.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitSFTPMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitSFTPMenuDismiss),
		RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

// clickSFTPMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickSFTPMenu(id ui.ID) bool {
	if !state.sftpMenu.open {
		return false
	}
	state.sftpMenu.open = false
	if action, ok := sftpMenuAction(id); ok {
		state.clickSFTP(action)
	}
	return true
}
