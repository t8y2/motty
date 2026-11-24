package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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

func (w *sftpWindow) targetSide(id gfx.ID) (bool, bool) {
	for side := range 2 {
		if _, ok := id.In(sftpWindowBreadcrumbs[side]); ok {
			return side == 0, true
		}
		if id == sftpWindowPathEdit[side] {
			return side == 0, true
		}
		if _, ok := id.In(sftpWindowRecentHits[side]); ok {
			return side == 0, true
		}
		if id == sftpWindowHistoryBack[side] || id == sftpWindowHistoryForward[side] {
			return side == 0, true
		}
	}
	switch id {
	case hitSFTPLocationProfiles, hitSFTPProfileQuick, hitSFTPBrowseSync, hitSFTPLocalPlaces, hitSFTPLocalStar:
		return true, true
	case hitSFTPBookmarkList, hitSFTPBookmarkToggle, hitSFTPNewLink, hitSFTPWindowUpload, hitSFTPWindowDownload:
		return false, true
	}
	if local, _, ok := w.rowTarget(id); ok {
		return local, true
	}
	for side := range 2 {
		if id == sftpWindowTreeToggle[side] {
			return side == 0, true
		}
		if _, ok := id.In(sftpWindowTreeRows[side]); ok {
			return side == 0, true
		}
		if _, ok := id.In(sftpWindowTreeDisclosures[side]); ok {
			return side == 0, true
		}
		if _, ok := id.In(sftpWindowSortHits[side]); ok {
			return side == 0, true
		}
		if _, ok := id.In(sftpWindowActions[side]); ok {
			return side == 0, true
		}
		for _, ids := range [][2]gfx.ID{sftpWindowPath, sftpWindowFilter, sftpWindowList, sftpWindowMore, sftpWindowFilterClear, sftpWindowFilterToggle, sftpWindowHidden, sftpWindowHeaderHidden, sftpWindowRefresh, sftpWindowUp, sftpWindowNewFile, sftpWindowNewFolder, sftpWindowTreeBar} {
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

func (w *sftpWindow) clickFilterControl(id gfx.ID) bool {
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

func (w *sftpWindow) filterEscape(event gfx.KeyEvent) (gfx.EventResult, bool) {
	for side, id := range sftpWindowFilter {
		folder := w.folder(side == 0)
		if event.Target != id || !folder.filterOpen {
			continue
		}
		result := gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}
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
				result.Focus = w.rowID(side, max(0, folder.selected))
			}
		}
		return result, true
	}
	return gfx.EventResult{}, false
}

// headerMenuRows is one pane's More menu as page menu rows: the history the pane
// can reach, the filters and hidden toggle, and the actions the bar could not fit.
func (w *sftpWindow) headerMenuRows(side int, paneWidth float32) menuRows {
	m, folder, local := w.owner.messages, w.folder(side == 0), side == 0
	tools := m.Tools
	var rows menuRows
	ready := w.owner.sftpNavigationReady(w.view, local)
	_, canBack := folder.historyTarget(-1)
	_, canForward := folder.historyTarget(1)
	rows.add(components.ContextMenuItem{ID: sftpWindowHistoryBack[side], Label: tools.FilesBack, Shortcut: "Alt+Left", Disabled: !ready || !canBack})
	rows.add(components.ContextMenuItem{ID: sftpWindowHistoryForward[side], Label: tools.FilesForward, Shortcut: "Alt+Right", Disabled: !ready || !canForward})
	if len(folder.history.recent) > 0 {
		rows.add(components.ContextMenuItem{Label: tools.FilesRecent, Disabled: true})
		for index, directory := range folder.history.recent {
			rows.add(components.ContextMenuItem{ID: sftpWindowRecentHits[side].At(index), Label: directory, Hint: directory, Disabled: !ready, Selected: directory == folder.path})
		}
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: sftpWindowFilterToggle[side], Label: tools.FilesFilterLabel, Icon: lucideIcons[iconSearch], Kind: components.ContextMenuCheckbox, Checked: folder.filterOpen || folder.filterInputEditor().String() != ""})
	hiddenIcon, hiddenLabel := uint8(iconEyeOff), tools.FilesShowHidden
	if folder.showHidden {
		hiddenIcon, hiddenLabel = iconEye, tools.FilesHideHidden
	}
	rows.add(components.ContextMenuItem{ID: sftpWindowHeaderHidden[side], Label: hiddenLabel, Icon: lucideIcons[hiddenIcon], Kind: components.ContextMenuCheckbox, Checked: folder.showHidden})
	if label, disabled := w.owner.sftpEditorItem(w.view, folder); label != "" {
		rows.add(components.ContextMenuItem{ID: sftpWindowHeaderEditor[side], Label: label, Icon: lucideIcons[iconExternal], Disabled: disabled})
	}
	busy := w.browse.pending != nil || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected())
	rows.add(components.ContextMenuItem{
		ID:    sftpWindowActions[side].At(sftpWindowActionIndex(hitSFTPCalculateDirectorySize)),
		Label: m.SFTPWindow.CalculateDirectorySize, Icon: lucideIcons[iconInfo], Disabled: folder.path == "" || !local && w.view.openFiles == nil,
	})
	rows.add(components.ContextMenuItem{ID: sftpWindowNewFile[side], Label: m.SFTPNewFile, Icon: lucideIcons[iconFile], Disabled: busy})
	rows.add(components.ContextMenuItem{ID: sftpWindowNewFolder[side], Label: m.SFTPNewDirectory, Icon: lucideIcons[iconFolderPlus], Disabled: busy})
	if !local {
		rows.add(components.ContextMenuItem{ID: hitSFTPNewLink, Label: tools.TipNewLink, Icon: lucideIcons[iconLink], Disabled: busy})
		narrow := paneWidth < 400
		if narrow {
			rows.add(components.ContextMenuItem{ID: hitSFTPWindowUpload, Label: m.SFTPWindow.Upload, Icon: lucideIcons[iconUpload], Disabled: !w.canTransfer(true)})
			rows.add(components.ContextMenuItem{ID: hitSFTPWindowDownload, Label: m.SFTPWindow.Download, Icon: lucideIcons[iconDownload], Disabled: !w.canTransfer(false)})
		}
	}
	return rows
}

// declareHeaderMenus declares each pane's open More menu over the page, so its
// backdrop covers the window bar too. The menu hangs off the More button's own
// box: the page places it from the rectangle the control is laid out at, not the
// one the declaration can read before layout.
func (w *sftpWindow) declareHeaderMenus(c ui.C, anchors *[2]ui.Box) {
	for side := range 2 {
		folder := w.folder(side == 0)
		if !folder.headerMenuOpen {
			continue
		}
		if !w.owner.sftpHeaderMenuAvailable(w.view) || !anchors[side].Valid() {
			folder.headerMenuOpen = false
			continue
		}
		rows := w.headerMenuRows(side, w.ui.panes[side].bounds.Width)
		w.owner.declareMenuOn(c, &folder.headerMenuOpen, anchors[side], &rows, func(id gfx.ID) {
			w.clickFilterControl(id)
		})
	}
}
