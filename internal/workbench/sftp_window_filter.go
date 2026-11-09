package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
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
	if _, ok := id.In(hitSFTPProfileHoverItems); ok {
		return true, true
	}
	for side := range 2 {
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
		for _, ids := range [][2]gfx.ID{sftpWindowPath, sftpWindowFilter, sftpWindowList, sftpWindowMore, sftpWindowFilterClear, sftpWindowFilterToggle, sftpWindowHidden, sftpWindowHeaderHidden, sftpWindowRefresh, sftpWindowUp, sftpWindowNewFolder, sftpWindowBar, sftpWindowTreeBar} {
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
				result.Focus = sftpWindowRows[side].At(max(0, folder.selected))
			}
		}
		return result, true
	}
	return gfx.EventResult{}, false
}

// paintHeaderMenu paints one pane's More menu. origin and mount place it: a file
// tab paints over the page, so its menu has to as well — the page clips whatever
// an embedded canvas contributes, which would cut the dismiss backdrop down to
// the tab and leave the window bar unreachable. A standalone transfer window
// passes the zero point and scope, since its canvas is the whole window.
func (w *sftpWindow) paintHeaderMenu(canvas *gfx.Canvas, side int, origin gfx.Point, mount gfx.Scope, viewport gfx.Rect, scroll *components.MenuScroll) {
	m, folder, local := w.owner.messages, w.folder(side == 0), side == 0
	id := func(local gfx.ID) gfx.ID { return tree.MountID(mount, local) }
	route := func(local gfx.Scope) gfx.Scope { return tree.Mount(mount, local) }
	anchor := w.ui.headerAnchor[side]
	anchor.X, anchor.Y = anchor.X+origin.X, anchor.Y+origin.Y
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
	bounds := gfx.Rect{X: anchor.X + anchor.Width - 224, Y: anchor.Y + anchor.Height + 4, Width: 224, Height: float32(count*32 + 8)}
	dismiss := id(hitSFTPWindowHeaderDismiss)
	if !mount.IsZero() {
		// Painted over the page: the menu takes the page's popup state, so the
		// wheel, the keyboard and Escape reach it as they reach any other menu.
		scroll = w.owner.menuScrollFor(dismiss)
	}
	menu := components.BeginDropdownMenu(canvas, w.owner.Theme(), bounds, components.DropdownMenuOptions{Layer: popupLayer, Scope: dismiss, DismissID: dismiss, Viewport: viewport, Scroll: scroll})
	if !mount.IsZero() {
		w.owner.menuPlaced(menu.Bounds())
	}
	w.owner.paintSFTPHistoryMenu(&menu, w.view, local, id(sftpWindowHistoryBack[side]), id(sftpWindowHistoryForward[side]), route(sftpWindowRecentHits[side]))
	menu.AddItem(components.DropdownMenuItem{ID: id(sftpWindowFilterToggle[side]), Label: m.Tools.FilesFilterLabel, Icon: lucideIcons[iconSearch], Selected: folder.filterOpen || folder.filterInputEditor().String() != ""})
	hiddenIcon, hiddenLabel := uint8(iconEyeOff), m.Tools.FilesShowHidden
	if folder.showHidden {
		hiddenIcon, hiddenLabel = iconEye, m.Tools.FilesHideHidden
	}
	menu.AddItem(components.DropdownMenuItem{ID: id(sftpWindowHeaderHidden[side]), Label: hiddenLabel, Icon: lucideIcons[hiddenIcon], Selected: folder.showHidden})
	if label, disabled := w.owner.sftpEditorItem(w.view, folder); label != "" {
		menu.AddItem(components.DropdownMenuItem{ID: id(sftpWindowHeaderEditor[side]), Label: label, Icon: lucideIcons[iconExternal], Disabled: disabled})
	}
	busy := w.browse.pending != nil || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected())
	menu.AddItem(components.DropdownMenuItem{ID: id(sftpWindowNewFolder[side]), Label: m.SFTPNewDirectory, Icon: lucideIcons[iconFolderPlus], Disabled: busy})
	if !local {
		menu.AddItem(components.DropdownMenuItem{ID: id(hitSFTPNewLink), Label: m.Tools.TipNewLink, Icon: lucideIcons[iconLink], Disabled: busy})
		if narrow {
			menu.AddItem(components.DropdownMenuItem{ID: id(hitSFTPWindowUpload), Label: m.SFTPWindow.Upload, Icon: lucideIcons[iconUpload], Disabled: !w.canTransfer(true)})
			menu.AddItem(components.DropdownMenuItem{ID: id(hitSFTPWindowDownload), Label: m.SFTPWindow.Download, Icon: lucideIcons[iconDownload], Disabled: !w.canTransfer(false)})
		}
	}
	menu.End()
}
