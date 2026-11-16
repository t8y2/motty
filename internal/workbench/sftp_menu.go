package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	hitSFTPMenuMove         = tree.ID(sftpMenuHits, "sftp-menu-move")
	hitSFTPMenuChmod        = tree.ID(sftpMenuHits, "sftp-menu-chmod")
	hitSFTPMenuChown        = tree.ID(sftpMenuHits, "sftp-menu-chown")
	hitSFTPMenuExtract      = tree.ID(sftpMenuHits, "sftp-menu-extract")
	hitSFTPMenuCompress     = tree.ID(sftpMenuHits, "sftp-menu-compress")
	hitSFTPMenuDelete       = tree.ID(sftpMenuHits, "sftp-menu-delete")
	hitSFTPMenuSudo         = tree.ID(sftpMenuHits, "sftp-menu-sudo")
	hitSFTPMenuCopyFiles    = tree.ID(sftpMenuHits, "sftp-menu-copy-files")
	hitSFTPMenuPasteFiles   = tree.ID(sftpMenuHits, "sftp-menu-paste-files")
)

// sftpMenuAction is the button of the bar that a menu row stands for.
func sftpMenuAction(id gfx.ID) (gfx.ID, bool) {
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
	case hitSFTPMenuMove:
		return hitSFTPMove, true
	case hitSFTPMenuChmod:
		return hitSFTPChmod, true
	case hitSFTPMenuChown:
		return hitSFTPChown, true
	case hitSFTPMenuExtract:
		return hitSFTPExtract, true
	case hitSFTPMenuCompress:
		return hitSFTPCompress, true
	case hitSFTPMenuDelete:
		return hitSFTPDelete, true
	case hitSFTPMenuSudo:
		return hitSFTPMenuSudo, true
	case hitSFTPMenuCopyFiles:
		return hitSFTPCopyFiles, true
	case hitSFTPMenuPasteFiles:
		return hitSFTPPasteFiles, true
	case hitSFTPNewDirectory, hitSFTPUpload, hitSFTPUploadDirectory, hitSFTPNewLink,
		hitSFTPCopyPath, hitSFTPProperties, hitSFTPCopyFiles, hitSFTPPasteFiles:
		return id, true
	}
	return gfx.ID{}, false
}

// sftpMenu is the right-click menu of the file list: which session's panel it was
// asked in, and where. Closed, it costs nothing.
type sftpMenu struct {
	open    bool
	session uint32
	at      gfx.Point
}

// sftpMenuDismissPointer closes an open menu on a secondary press outside it; the
// backdrop swallows the press, and only a primary one is also a click.
func (state *State) sftpMenuDismissPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown || event.Button == gfx.PointerButtonNone || event.Button == gfx.PointerButtonPrimary {
		return gfx.EventResult{}, false
	}
	if state.sftpMenu.open && event.Target == hitSFTPMenuDismiss {
		state.sftpMenu.open = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// sftpMenuPointer opens the file list menu on a secondary press. On the
// background the menu acts on the current directory.
func (state *State) sftpMenuPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if event.Kind != gfx.PointerDown {
		return gfx.EventResult{}, false
	}
	background := event.Target == hitSFTPListBackground
	if background && event.Button == gfx.PointerButtonPrimary {
		// A press on the list background still takes focus out of the path field.
		return gfx.EventResult{Handled: true, Repaint: true, Blur: true}, true
	}
	if event.Button != gfx.PointerButtonSecondary {
		return gfx.EventResult{}, false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || state.activeSection != sectionTerminal || state.modal != modalNone || session.files == nil || session.files.editPath != "" {
		return gfx.EventResult{}, false
	}
	view := session.files
	folder := view.active()
	index, listRow := folder.listRow(event.Target)
	if !listRow && !background {
		if event.Target.Scope() != hitSFTPEntryList {
			return gfx.EventResult{}, false
		}
		index = folder.first + item(event.Target)
	}
	if view.dialog != sftpDialogNone || !background && index >= len(folder.shownRows()) || view.busy && !view.local {
		return gfx.EventResult{Handled: true}, true
	}
	if folder.searching() {
		if background {
			return gfx.EventResult{}, false
		}
		// The actions in the menu are the folder's; a result is not in it.
		folder.search.selectRow(index)
		folder.listNeedsSeed = true
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if background {
		folder.clearSelection()
		view.setItemCountStatus(state.messages.SFTPItemsFmt, 0)
	} else if !folder.rows[index].selected {
		folder.selectRow(index, 0)
		view.setItemCountStatus(state.messages.SFTPItemsFmt, selectedSFTPCount(view))
	}
	state.sftpMenu = sftpMenu{open: true, session: session.ID, at: state.terminalMenu.pointer}
	return gfx.EventResult{Handled: true, Repaint: true}, true
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

func (state *State) paintSFTPMenu(canvas *gfx.Canvas, viewport gfx.Rect) {
	session, view, ok := state.sftpMenuView()
	if !ok {
		state.sftpMenu.open = false
		return
	}
	rows := state.sftpMenuRows(session, view)
	width, height := rows.size(canvas, state.contextMenuTheme())
	width = min(width, 480)
	bounds := state.popupAt(canvas, viewport, state.sftpMenu.at, width, height, 6)
	menu := components.BeginContextMenu(canvas, state.contextMenuTheme(), bounds, components.ContextMenuOptions{
		Layer: dialogLayer, Scope: dialogScope, DismissID: hitSFTPMenuDismiss, Viewport: viewport, Scroll: state.menuScrollFor(hitSFTPMenuDismiss), RowHeight: contextMenuRow,
	})
	state.menuPlaced(menu.Bounds())
	rows.paint(&menu, nil)
	menu.End()
}

func (state *State) sftpMenuRows(session *Session, view *sftpView) menuRows {
	messages := state.messages
	count, selected := selectedSFTP(view)
	one, local := count == 1, view.local
	busy := view.busy && !local || local && view.localBusy
	icon := func(kind uint8) gfx.Icon { return lucideIcons[kind] }
	var rows menuRows
	if one {
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuOpen, Label: messages.SFTPOpen, Icon: icon(iconFolderOpen), Disabled: busy})
		if local && selected.Mode.IsRegular() {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuEdit, Label: messages.Tools.ExtEditButton, Icon: icon(iconExternal), Disabled: busy})
		}
		rows.separate()
	}
	if count > 0 {
		switch {
		case view.localOnly:
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuInsertPath, Label: messages.SFTPInsertPath, Icon: icon(iconTerminal), Disabled: session.term == nil || session.term.shell == nil || busy})
		case local:
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuSend, Label: messages.Tools.SendToServer,
				Description: view.path, Value: view.path, Hint: view.path, Icon: icon(iconUpload), Disabled: view.path == "" || busy})
		default:
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuDownload, Label: messages.SFTPDownload, Icon: icon(iconDownload), Disabled: busy})
			if view.lfolder.path != "" {
				rows.add(components.ContextMenuItem{ID: hitSFTPMenuDownloadHere, Label: messages.Tools.DownloadHere, Icon: icon(iconFileDown), Disabled: busy})
			}
		}
	}
	if !local && count > 0 {
		if one && !selected.IsDir() {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuEdit, Label: messages.Tools.ExtEditButton, Icon: icon(iconExternal), Disabled: busy})
		}
		rows.separate()
		if one {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuRename, Label: messages.SFTPRename, Icon: icon(iconPencil), Disabled: busy})
		}
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuMove, Label: messages.SFTPMove, Icon: icon(iconFolderOpen), Disabled: busy})
	}
	if one && !local {
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuChmod, Label: messages.SFTPPermissions, Icon: icon(iconLock), Disabled: busy})
		if sftpOwnershipAvailable(session, view) {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuChown, Label: messages.SFTPOwnership, Icon: icon(iconWrench), Disabled: busy})
		}
		if _, archive := state.selectedArchive(session, view); archive {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuExtract, Label: messages.Tools.Extract, Icon: icon(iconArchive), Disabled: busy})
		}
		if state.canCompressSFTP(session, view) {
			rows.add(components.ContextMenuItem{ID: hitSFTPMenuCompress, Label: messages.Tools.Compress, Icon: icon(iconArchive), Disabled: busy})
		}
		rows.separate()
	}
	if count > 0 {
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuDelete, Label: messages.SFTPDelete, Icon: icon(iconTrash), Variant: components.ContextMenuItemDestructive, Disabled: busy})
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSFTPNewDirectory, Label: messages.SFTPNewDirectory, Icon: icon(iconFolderPlus), Disabled: busy})
	if !local {
		rows.add(components.ContextMenuItem{ID: hitSFTPUpload, Label: messages.SFTPUpload, Icon: icon(iconUpload), Disabled: busy})
		rows.add(components.ContextMenuItem{ID: hitSFTPUploadDirectory, Label: messages.SFTPUploadDirectory, Icon: icon(iconFolderUp), Disabled: busy})
		rows.add(components.ContextMenuItem{ID: hitSFTPNewLink, Label: messages.Tools.TipNewLink, Icon: icon(iconLink), Disabled: busy})
	}
	if count > 0 {
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuCopyFiles, Label: messages.SFTPWindow.CopyFiles, Icon: icon(iconCopy), Disabled: busy || view.active().searching()})
	}
	// A local side pastes the server files copied last, from any server; the
	// server side pastes them too, or the files the system clipboard holds,
	// which is only known once the paste runs.
	paste := view.worker != nil
	if local {
		paste = !state.sftpRemoteClipboard().empty()
	}
	rows.add(components.ContextMenuItem{ID: hitSFTPMenuPasteFiles, Label: messages.SFTPWindow.PasteFiles, Icon: icon(iconPaste), Disabled: busy || view.busy || view.worker == nil || !paste || view.active().searching()})
	if count > 0 {
		rows.add(components.ContextMenuItem{ID: hitSFTPCopyPath, Label: messages.SFTPWindow.CopyPath, Icon: icon(iconCopy)})
	}
	if one {
		rows.add(components.ContextMenuItem{ID: hitSFTPProperties, Label: messages.SFTPWindow.PropertiesTitle, Icon: icon(iconInfo)})
	}
	if sudoSFTPAvailable(session, view) {
		label := messages.SFTPSudoEnable
		if view.sudo {
			label = messages.SFTPSudoDisable
		}
		rows.separate()
		rows.add(components.ContextMenuItem{ID: hitSFTPMenuSudo, Label: label, Icon: icon(iconLock)})
	}

	return rows
}

// clickSFTPMenu runs a menu choice. A press anywhere else only closes the menu.
func (state *State) clickSFTPMenu(id gfx.ID) bool {
	if !state.sftpMenu.open {
		return false
	}
	state.sftpMenu.open = false
	if action, ok := sftpMenuAction(id); ok {
		state.clickSFTP(action)
	}
	return true
}
