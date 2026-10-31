package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
)

func (w *sftpWindow) headerMenu() (int, bool) {
	for side := range 2 {
		if w.folder(side == 0).headerMenuOpen {
			return side, true
		}
	}
	return 0, false
}
func (w *sftpWindow) headerMenuOpen() bool { _, ok := w.headerMenu(); return ok }
func (w *sftpWindow) closeHeaderMenu() {
	w.view.lfolder.headerMenuOpen, w.view.sftpFolder.headerMenuOpen = false, false
}

func (w *sftpWindow) targetSide(id ui.ID) (bool, bool) {
	for side := range 2 {
		if _, ok := id.In(sftpWindowRecentHits[side]); ok {
			return side == 0, true
		}
		if id == sftpWindowHistoryBack[side] || id == sftpWindowHistoryForward[side] {
			return side == 0, true
		}
	}
	switch id {
	case hitSFTPLocationProfiles, hitSFTPBrowseSync, hitSFTPLocalPlaces, hitSFTPLocalStar:
		return true, true
	case hitSFTPBookmarkList, hitSFTPBookmarkToggle, hitSFTPNewLink, hitSFTPWindowUpload, hitSFTPWindowDownload:
		return false, true
	}
	if local, _, ok := w.rowTarget(id); ok {
		return local, true
	}
	for side := range 2 {
		if _, ok := id.In(sftpWindowSortHits[side]); ok {
			return side == 0, true
		}
		if _, ok := id.In(sftpWindowActions[side]); ok {
			return side == 0, true
		}
		for _, ids := range [][2]ui.ID{sftpWindowPath, sftpWindowFilter, sftpWindowList, sftpWindowMore, sftpWindowFilterClear, sftpWindowFilterToggle, sftpWindowHidden, sftpWindowHeaderHidden, sftpWindowRefresh, sftpWindowUp, sftpWindowNewFolder, sftpWindowBar} {
			if id == ids[side] {
				return side == 0, true
			}
		}
	}
	return false, false
}

func (w *sftpWindow) applyFolderFilter(local bool) {
	w.cancelDrag()
	folder := w.folder(local)
	folder.rememberSelection()
	w.owner.applySFTPFilter(w.view, folder, false)
	w.ui.offset[sideIndex(local)] = float32(folder.first) * sftpRowHeight
}

func (w *sftpWindow) clickFilterControl(id ui.ID) bool {
	if local, ok := w.targetSide(id); ok {
		w.activeLocal, w.view.local = local, local
	}
	if id == hitSFTPWindowHeaderDismiss {
		w.closeHeaderMenu()
		return true
	}
	if w.headerMenuOpen() {
		w.closeHeaderMenu()
	}
	for side := range 2 {
		folder, local := w.folder(side == 0), side == 0
		if w.owner.clickSFTPHistory(w.view, local, id, sftpWindowHistoryBack[side], sftpWindowHistoryForward[side], sftpWindowRecentHits[side]) {
			return true
		}
		switch id {
		case sftpWindowMore[side]:
			w.cancelDrag()
			w.activeLocal, w.view.local = local, local
			folder.headerMenuOpen = true
			w.ui.headerScroll = components.MenuScroll{}
			return true
		case sftpWindowFilterToggle[side]:
			w.activeLocal, w.view.local = local, local
			folder.filterOpen = !folder.filterOpen
			return true
		case sftpWindowHeaderEditor[side]:
			w.activeLocal, w.view.local = local, local
			w.owner.openSFTPInEditor(w.view)
			return true
		case sftpWindowHidden[side], sftpWindowHeaderHidden[side]:
			w.activeLocal, w.view.local = local, local
			folder.showHidden = !folder.showHidden
			w.applyFolderFilter(local)
			return true
		case sftpWindowFilterClear[side]:
			w.activeLocal, w.view.local = local, local
			folder.clearFilter()
			w.applyFolderFilter(local)
			return true
		}
	}
	return false
}

func (w *sftpWindow) filterEscape(event ui.KeyEvent) (ui.EventResult, bool) {
	for side, id := range sftpWindowFilter {
		folder := w.folder(side == 0)
		if event.Target != id || !folder.filterOpen {
			continue
		}
		result := ui.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}
		if _, composing := folder.filterInputEditor().Composition(); composing || event.Repeat {
			return result, true
		}
		if folder.filterInputEditor().String() != "" {
			folder.clearFilter()
			w.applyFolderFilter(side == 0)
		} else {
			folder.filterOpen = false
			result.Focus = sftpWindowList[side]
			if len(folder.rows) > 0 {
				result.Focus = sftpWindowRows[side].At(max(0, folder.selected))
			}
		}
		return result, true
	}
	return ui.EventResult{}, false
}

func (w *sftpWindow) paintHeaderMenu(canvas *ui.Canvas, side int) {
	m, folder, local := w.owner.messages, w.folder(side == 0), side == 0
	anchor := w.ui.headerAnchor[side]
	count := 6 + len(folder.history.recent)
	if label, _ := w.owner.sftpEditorItem(w.view, folder); label != "" {
		count++
	}
	if !local {
		count++
	}
	narrow := w.ui.list[side].Width < 400
	if !local && narrow {
		count += 2
	}
	bounds := ui.Rect{X: anchor.X + anchor.Width - 224, Y: anchor.Y + anchor.Height + 4, Width: 224, Height: float32(count*32 + 8)}
	menu := components.BeginDropdownMenu(canvas, w.owner.Theme(), bounds, components.DropdownMenuOptions{Layer: popupLayer, Scope: hitSFTPWindowHeaderDismiss, DismissID: hitSFTPWindowHeaderDismiss, Viewport: ui.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height}, Scroll: &w.ui.headerScroll})
	w.owner.paintSFTPHistoryMenu(&menu, w.view, local, sftpWindowHistoryBack[side], sftpWindowHistoryForward[side], sftpWindowRecentHits[side])
	menu.AddItem(components.DropdownMenuItem{ID: sftpWindowFilterToggle[side], Label: m.Tools.FilesFilterLabel, Icon: lucideIcons[iconSearch], Selected: folder.filterOpen || folder.filterInputEditor().String() != ""})
	hiddenIcon, hiddenLabel := uint8(iconEyeOff), m.Tools.FilesShowHidden
	if folder.showHidden {
		hiddenIcon, hiddenLabel = iconEye, m.Tools.FilesHideHidden
	}
	menu.AddItem(components.DropdownMenuItem{ID: sftpWindowHeaderHidden[side], Label: hiddenLabel, Icon: lucideIcons[hiddenIcon], Selected: folder.showHidden})
	if label, disabled := w.owner.sftpEditorItem(w.view, folder); label != "" {
		menu.AddItem(components.DropdownMenuItem{ID: sftpWindowHeaderEditor[side], Label: label, Icon: lucideIcons[iconExternal], Disabled: disabled})
	}
	busy := w.browse.pending != nil || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected())
	menu.AddItem(components.DropdownMenuItem{ID: sftpWindowNewFolder[side], Label: m.SFTPNewDirectory, Icon: lucideIcons[iconFolderPlus], Disabled: busy})
	if !local {
		menu.AddItem(components.DropdownMenuItem{ID: hitSFTPNewLink, Label: m.Tools.TipNewLink, Icon: lucideIcons[iconLink], Disabled: busy})
		if narrow {
			menu.AddItem(components.DropdownMenuItem{ID: hitSFTPWindowUpload, Label: m.SFTPWindow.Upload, Icon: lucideIcons[iconUpload], Disabled: !w.canTransfer(true)})
			menu.AddItem(components.DropdownMenuItem{ID: hitSFTPWindowDownload, Label: m.SFTPWindow.Download, Icon: lucideIcons[iconDownload], Disabled: !w.canTransfer(false)})
		}
	}
	menu.End()
}
