package workbench

import (
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

type sftpWindowDrag struct {
	armed, active, fromLocal bool
	start, at                gfx.Point
	sourceDir                string
	entries                  []sftpclient.Entry
}

func (w *sftpWindow) cancelDrag() {
	w.drag = sftpWindowDrag{}
	pending := w.deferredListings
	w.deferredListings = [2]func(){}
	for _, apply := range pending {
		if apply != nil && !w.closed {
			apply()
		}
	}
}

// Keep the visible source and destination rows stable until a drag finishes.
// A transfer refresh or metadata prefetch must not move another folder under
// the pointer between the highlighted destination and the release event.
func (view *sftpView) deferDraggedListing(local bool, apply func()) bool {
	if w := view.detached; w != nil && (w.drag.armed || w.drag.active) {
		w.deferredListings[sideIndex(local)] = apply
		return true
	}
	return false
}

func (w *sftpWindow) rowTarget(id gfx.ID) (bool, int, bool) {
	for side, scope := range sftpWindowRows {
		if index, ok := id.In(scope); ok {
			return side == 0, index, true
		}
	}
	return false, 0, false
}

// Pointer capture retains the source hit during a drag. Resolve destinations
// from current coordinates, never that captured hit or the active terminal tab.
func (w *sftpWindow) dropTarget(x, y float32, fromLocal bool) (local bool, target string, row int, valid bool) {
	local = !fromLocal
	side := sideIndex(local)
	row = -1
	if !w.connected() || w.navigationBusy() || w.bookmark.open || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 || w.view.bookmarksOpen || w.ui.menuOpen || w.headerMenuOpen() || !pointInRect(x, y, w.ui.list[side]) {
		return
	}
	folder := w.folder(local)
	if folder.path == "" {
		return
	}
	target = folder.path
	index := int((y - w.ui.list[side].Y + w.ui.offset[side]) / sftpRowHeight)
	if index >= 0 && index < len(folder.rows) && folder.rows[index].entry.IsDir() && !folder.rows[index].entry.LinkBroken {
		row = index
		if local {
			target = filepath.Join(target, folder.rows[index].entry.Name)
		} else {
			target = path.Join(target, folder.rows[index].entry.Name)
		}
	}
	valid = true
	return
}

func (w *sftpWindow) pointer(event gfx.PointerEvent) gfx.EventResult {
	if w.view.worker == nil {
		return gfx.EventResult{}
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if w.fileProperties.open {
		// Let the dialog's own controls complete their normal press/release
		// click sequence. Every other pointer event belongs to the modal layer.
		if event.Target == hitFilePropertiesClose || event.Target == hitFilePropertiesDismiss {
			return gfx.EventResult{}
		}
		return handled
	}
	if w.closeConfirm || w.externalConflict != 0 || w.view.dialog != sftpDialogNone {
		return gfx.EventResult{}
	}
	if w.bookmark.open {
		return w.profilePointer(event)
	}
	if w.ui.profileQuickOpen {
		if w.ui.profileQuickScroll.Drag(event) {
			return handled
		}
		if event.Kind == gfx.PointerDown && event.Target == hitSFTPProfileQuickDismiss {
			w.closeProfileQuickMenu()
			return handled
		}
		return gfx.EventResult{}
	}
	if w.headerMenuOpen() {
		if w.ui.headerScroll.Drag(event) {
			return handled
		}
		if event.Kind == gfx.PointerDown && event.Target == hitSFTPWindowHeaderDismiss {
			w.closeHeaderMenu()
			return handled
		}
		return gfx.EventResult{}
	}
	if w.ui.menuOpen {
		if w.ui.menuScroll.Drag(event) {
			return handled
		}
		if event.Kind == gfx.PointerDown && event.Target == hitSFTPWindowMenuDismiss {
			w.ui.menuOpen = false
			return handled
		}
		return gfx.EventResult{}
	}
	if w.view.bookmarksOpen {
		wasDragging := w.view.places.scroll.Dragging()
		changed := w.view.places.scroll.Drag(event)
		if wasDragging || changed || w.view.places.scroll.Dragging() {
			return gfx.EventResult{Handled: true, Repaint: changed}
		}
		if event.Kind == gfx.PointerDown && event.Target == hitSFTPBookmarkDismiss {
			w.view.bookmarksOpen = false
			return handled
		}
		return gfx.EventResult{}
	}
	if w.ui.splitting {
		if event.Kind == gfx.PointerMove {
			w.ui.fraction = w.ui.split.FractionAt(event.X)
		} else if event.Kind == gfx.PointerUp || event.Kind == gfx.PointerExit {
			w.ui.splitting = false
		}
		return handled
	}
	if event.Kind == gfx.PointerDown && event.Target == hitSFTPWindowSplit {
		if fraction, ok := w.ui.split.FractionForClick(event.ClickCount); ok {
			w.ui.fraction = fraction
		} else {
			w.ui.splitting = true
		}
		return handled
	}
	if result, ok := w.directoryTreePointer(event); ok {
		return result
	}
	if w.ui.barDrag > 0 {
		side := w.ui.barDrag - 1
		if event.Kind == gfx.PointerMove {
			if !event.Captured {
				return gfx.EventResult{Handled: true}
			}
			var before, after float32
			if side == 2 {
				before = w.ui.queueOffset
				w.ui.queueOffset = w.ui.queueBar.VerticalOffsetForThumb(event.Y - w.ui.barGrab)
				after = w.ui.queueOffset
			} else {
				before = w.ui.offset[side]
				w.ui.offset[side] = w.ui.bars[side].VerticalOffsetForThumb(event.Y - w.ui.barGrab)
				after = w.ui.offset[side]
			}
			return gfx.EventResult{Handled: true, Repaint: before != after}
		} else if event.Kind == gfx.PointerUp {
			w.ui.barDrag = 0
		} else if event.Kind == gfx.PointerExit && !event.Captured {
			w.ui.barDrag = 0
		}
		return gfx.EventResult{Handled: true}
	}
	if event.Kind == gfx.PointerDown && event.Target == hitSFTPWindowQueueBar {
		geometry := w.ui.queueBar
		before := w.ui.queueOffset
		w.ui.barGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			w.ui.barGrab = event.Y - geometry.VerticalThumb.Y
		}
		w.ui.barDrag = 3
		w.ui.queueOffset = geometry.VerticalOffsetForThumb(event.Y - w.ui.barGrab)
		return gfx.EventResult{Handled: true, Repaint: before != w.ui.queueOffset}
	}
	for side, id := range sftpWindowBar {
		if event.Kind == gfx.PointerDown && event.Target == id {
			geometry := w.ui.bars[side]
			before := w.ui.offset[side]
			w.ui.barGrab = geometry.VerticalThumb.Height / 2
			if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
				w.ui.barGrab = event.Y - geometry.VerticalThumb.Y
			}
			w.ui.barDrag = side + 1
			w.ui.offset[side] = geometry.VerticalOffsetForThumb(event.Y - w.ui.barGrab)
			return gfx.EventResult{Handled: true, Repaint: before != w.ui.offset[side]}
		}
	}
	if w.drag.armed || w.drag.active {
		w.drag.at = gfx.Point{X: event.X, Y: event.Y}
		if (event.Kind == gfx.PointerDown || event.Kind == gfx.PointerUp) && event.Button != gfx.PointerButtonPrimary {
			w.cancelDrag()
			return handled
		}
		switch event.Kind {
		case gfx.PointerMove:
			if !w.drag.active && math.Hypot(float64(event.X-w.drag.start.X), float64(event.Y-w.drag.start.Y)) >= 6 {
				w.drag.active = true
				for _, row := range w.folder(w.drag.fromLocal).rows {
					if row.selected {
						w.drag.entries = append(w.drag.entries, row.entry)
					}
				}
			}
			return handled
		case gfx.PointerUp:
			if w.drag.active {
				_, target, _, valid := w.dropTarget(event.X, event.Y, w.drag.fromLocal)
				if valid {
					w.queueEntries(w.drag.fromLocal, w.drag.entries, w.drag.sourceDir, target)
				}
			}
			w.cancelDrag()
			return handled
		case gfx.PointerExit:
			if !event.Captured {
				w.cancelDrag()
			}
			return handled
		}
	}
	if event.Kind != gfx.PointerDown {
		return gfx.EventResult{}
	}
	local, index, ok := w.rowTarget(event.Target)
	if !ok {
		for side, id := range sftpWindowList {
			if event.Target == id && event.Button == gfx.PointerButtonSecondary {
				w.cancelDrag()
				w.activeLocal, w.view.local = side == 0, side == 0
				folder := w.folder(side == 0)
				for i := range folder.rows {
					folder.rows[i].selected = false
				}
				folder.selected = -1
				w.selectionChanged(side == 0)
				w.ui.menuOpen, w.ui.menuAt = true, gfx.Point{X: event.X, Y: event.Y}
				return handled
			}
		}
		return gfx.EventResult{}
	}
	folder := w.folder(local)
	if index < 0 || index >= len(folder.rows) || (!local && w.view.busy) {
		return handled
	}
	w.selectionChanged(local)
	w.activeLocal, w.view.local = local, local
	handled.Focus = event.Target
	if event.Button == gfx.PointerButtonSecondary {
		w.cancelDrag()
		if !folder.rows[index].selected {
			folder.selectRow(index, 0)
		}
		w.ui.menuOpen = true
		w.ui.menuAt = gfx.Point{X: event.X, Y: event.Y}
		return handled
	}
	if event.Button != gfx.PointerButtonPrimary {
		return gfx.EventResult{}
	}
	if event.ClickCount >= 2 {
		w.openEntry(local, index)
		return handled
	}
	if !folder.rows[index].selected || event.Modifiers != 0 {
		folder.selectRow(index, event.Modifiers)
	}
	if w.connected() && !w.view.busy && w.pendingLocal == "" {
		w.drag = sftpWindowDrag{armed: true, fromLocal: local, start: gfx.Point{X: event.X, Y: event.Y}, at: gfx.Point{X: event.X, Y: event.Y}, sourceDir: folder.path}
	}
	return handled
}

func (w *sftpWindow) click(id gfx.ID) {
	defer w.invalidate()
	if w.view.worker == nil {
		switch id {
		case hitSFTPWindowConnectCancel:
			w.closeHostNow()
		case hitSFTPWindowConnectRetry:
			if w.session.failed() || w.session.phase == sshconn.PhaseClosed {
				w.connect()
			}
		case hitSFTPWindowAuthenticate:
			w.owner.raiseSFTPAuthentication(&w.session)
		}
		return
	}
	if w.clickFileProperties(id) {
		return
	}
	if w.notices.click(id) {
		return
	}
	if w.closeConfirm {
		if id == hitSFTPWindowCloseCancel {
			w.closeConfirm = false
			w.allowClose = nil
			w.afterClosed = nil
		}
		if id == hitSFTPWindowCloseConfirm {
			allow := w.allowClose
			w.allowClose = nil
			if allow != nil {
				allow()
			}
		}
		return
	}
	if w.bookmark.open {
		w.clickBookmarkEditor(id)
		return
	}
	if w.view.dialog != sftpDialogNone {
		w.owner.clickSFTPDialog(w.view, id)
		return
	}
	if w.externalConflict != 0 {
		if id == hitSFTPWindowExternalReplace {
			w.owner.replaceOverConflict(w.externalConflict)
			w.externalConflict = 0
		}
		if id == hitSFTPWindowExternalKeep {
			w.owner.keepAfterConflict(w.externalConflict)
			w.externalConflict = 0
		}
		return
	}
	if w.clickProfileQuick(id) {
		return
	}
	if w.clickFilterControl(id) {
		return
	}
	for side := range 2 {
		if id == sftpWindowTreeToggle[side] {
			w.activeLocal, w.view.local = side == 0, side == 0
			w.toggleDirectoryTree(side == 0)
			return
		}
	}
	if w.clickDirectoryTree(id) {
		return
	}
	if id == hitSFTPWindowMenuDismiss {
		w.ui.menuOpen = false
		return
	}
	if w.ui.menuOpen {
		w.ui.menuOpen = false
		if id == hitSFTPMenuSudo {
			w.owner.toggleSudoSFTP(&w.session, w.view)
			return
		}
		if index, ok := id.In(sftpWindowMenuActions); ok && index < len(sftpWindowActionIDs) {
			w.fileAction(w.activeLocal, sftpWindowActionIDs[index])
			return
		}
		if id == hitSFTPWindowMenuCopy {
			if err := w.owner.copySFTPPaths(w.folder(w.activeLocal), w.activeLocal); err != nil {
				w.notices.show(w.owner.messages.SFTPWindow.Title, err.Error(), components.ToastError)
			}
		}
		return
	}
	if w.view.bookmarksOpen {
		w.clickDirectoryPlace(id)
		return
	}
	if id == hitSFTPWindowSudoMode {
		w.owner.toggleSudoSFTP(&w.session, w.view)
		return
	}
	for side := range 2 {
		if column, ok := id.In(sftpWindowSortHits[side]); ok && column < 3 {
			w.cancelDrag()
			w.activeLocal, w.view.local = side == 0, side == 0
			order := &w.ui.sort[side]
			order.toggle(column)
			w.sortFolder(side == 0)
			w.ui.offset[side] = 0
			return
		}
		if action, ok := id.In(sftpWindowActions[side]); ok && action < len(sftpWindowActionIDs) {
			w.fileAction(side == 0, sftpWindowActionIDs[action])
			return
		}
	}
	if w.owner.clickTransferTask(id) {
		w.invalidate()
		return
	}
	if id == hitSFTPBookmarkList {
		w.openDirectoryPlaces(false)
		return
	}
	if id == hitSFTPLocalPlaces {
		w.openDirectoryPlaces(true)
		return
	}
	if id == hitSFTPLocalStar {
		w.toggleDirectoryFavorite(true)
		return
	}
	if id == hitSFTPLocationProfiles {
		w.editBookmark(nil)
		return
	}
	if id == hitSFTPBookmarkToggle {
		w.toggleDirectoryFavorite(false)
		return
	}
	if id == hitSFTPBrowseSync {
		w.toggleBrowseSync()
		return
	}
	if id == hitSFTPNewLink {
		w.fileAction(false, id)
		return
	}
	for side := range sftpWindowPath {
		local := side == 0
		switch id {
		case sftpWindowRefresh[side]:
			w.refresh(local)
			return
		case sftpWindowUp[side]:
			parent := path.Dir(w.folder(local).path)
			if local {
				parent = filepath.Dir(w.folder(local).path)
			}
			w.navigate(local, parent)
			return
		case sftpWindowNewFolder[side]:
			if w.browse.pending != nil || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected()) {
				return
			}
			w.activeLocal, w.view.local = local, local
			w.cancelDrag()
			w.owner.openSFTPDialog(w.view, sftpDialogCreateDirectory)
			return
		}
	}
	switch {
	case id == hitSFTPWindowUpload:
		w.queueSelection(true)
	case id == hitSFTPWindowDownload:
		w.queueSelection(false)
	}
}

func (w *sftpWindow) refresh(local bool) {
	if w.browse.pending != nil || w.bookmark.open {
		return
	}
	if local {
		w.owner.listLocal(w.view, w.view.lfolder.path)
	} else if !w.view.busy && w.connected() {
		w.view.forgetListings()
		w.owner.listSFTP(w.view, w.view.path)
	}
}

func (w *sftpWindow) textInput(context app.TextInputContext) *app.TextInput {
	if w.fileProperties.open || w.closeConfirm || w.externalConflict != 0 {
		return nil
	}
	if w.bookmark.open {
		if index, ok := context.ID.In(hitSFTPBookmarkFields); ok && index < len(w.bookmark.fields) {
			w.input = app.TextInput{Editor: &w.bookmark.fields[index]}
			return &w.input
		}
		return nil
	}
	if w.view.dialog != sftpDialogNone {
		if context.ID == hitSFTPDialogField {
			w.input = app.TextInput{Editor: &w.view.dialogEditor}
			return &w.input
		}
		if context.ID == hitSFTPDialogField2 {
			w.input = app.TextInput{Editor: &w.view.dialogEditor2}
			return &w.input
		}
		return nil
	}
	for side, id := range sftpWindowFilter {
		folder := w.folder(side == 0)
		if context.ID == id && folder.filterOpen && !w.headerMenuOpen() && !w.ui.menuOpen && !w.view.bookmarksOpen {
			w.activeLocal, w.view.local = side == 0, side == 0
			input := &folder.filterInput
			input.Editor = nil
			input.State = app.TextStateOf(folder.filterInputEditor())
			input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
				before := folder.filterInputEditor().String()
				w.cancelDrag()
				next := w.owner.applySFTPFilterText(w.view, folder, event)
				if next.Value != before {
					w.ui.offset[side] = float32(folder.first) * sftpRowHeight
				}
				return next, nil
			}
			return input
		}
	}
	for side, id := range sftpWindowPath {
		if context.ID == id {
			w.activeLocal, w.view.local = side == 0, side == 0
			w.input = app.TextInput{Editor: w.folder(side == 0).pathInput()}
			return &w.input
		}
	}
	return nil
}

func (w *sftpWindow) key(event gfx.KeyEvent) gfx.EventResult {
	if event.Release {
		return gfx.EventResult{}
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	if w.view.worker == nil {
		if event.Key == gfx.KeyEscape && !event.Repeat {
			w.requestHostClose()
			return handled
		}
		return gfx.EventResult{}
	}
	if result, handled := w.filePropertiesKey(event); handled {
		return result
	}
	if w.bookmark.open && !w.closeConfirm {
		if result, handled := w.profileScopeKey(event); handled {
			return result
		}
		if event.Target == hitSFTPBookmarkSync && !event.Repeat && event.Modifiers == 0 && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
			w.clickBookmarkEditor(hitSFTPBookmarkSync)
			return handled
		}
		if index, ok := event.Target.In(hitSFTPBookmarkFields); ok && index < len(w.bookmark.fields) {
			if _, composing := w.bookmark.fields[index].Composition(); composing {
				return gfx.EventResult{}
			}
		}
		if !event.Repeat && event.Modifiers == 0 {
			switch event.Key {
			case gfx.KeyEscape:
				w.bookmark.open = false
				return handled
			case gfx.KeyEnter:
				if event.Target == hitSFTPBookmarkFields.At(0) || event.Target == hitSFTPBookmarkSave {
					w.saveBookmark()
					return handled
				}
				if _, input := event.Target.In(hitSFTPBookmarkFields); input || event.Target == hitSFTPProfileOpen {
					w.openProfileDraft()
					return handled
				}
				if index, row := event.Target.In(hitSFTPProfileItems); row {
					profiles := w.visibleProfiles()
					if index < len(profiles) {
						w.selectProfile(profiles[index])
						w.openProfileDraft()
					}
					return handled
				}
			}
		}
		if index, row := event.Target.In(hitSFTPProfileItems); row && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp || event.Key == gfx.KeyHome || event.Key == gfx.KeyEnd) {
			profiles := w.visibleProfiles()
			next := index
			switch event.Key {
			case gfx.KeyDown:
				next++
			case gfx.KeyUp:
				next--
			case gfx.KeyHome:
				next = 0
			case gfx.KeyEnd:
				next = len(profiles) - 1
			}
			if len(profiles) > 0 {
				next = max(0, min(next, len(profiles)-1))
				w.selectProfile(profiles[next])
				w.bookmark.offset = max(0, min(w.bookmark.offset, float32(next)*52))
				if bottom := float32(next+1) * 52; bottom > w.bookmark.offset+w.bookmark.list.Height {
					w.bookmark.offset = bottom - w.bookmark.list.Height
				}
				handled.Focus, handled.FocusVisible = hitSFTPProfileItems.At(next), true
			}
			return handled
		}
		return gfx.EventResult{}
	}
	if result, handled := w.profileQuickKey(event); handled {
		return result
	}
	if event.Key == gfx.KeyEscape {
		if w.closeConfirm {
			w.click(hitSFTPWindowCloseCancel)
		} else if w.externalConflict != 0 {
			w.click(hitSFTPWindowExternalKeep)
		} else if w.view.dialog != sftpDialogNone {
			w.owner.closeSFTPDialog(w.view)
		} else if w.headerMenuOpen() {
			w.closeHeaderMenu()
		} else if result, ok := w.filterEscape(event); ok {
			return result
		} else {
			w.cancelDrag()
			w.ui.menuOpen = false
			w.view.bookmarksOpen = false
			for side, id := range sftpWindowPath {
				if event.Target == id {
					folder := w.folder(side == 0)
					if _, composing := folder.pathInput().Composition(); !composing && !event.Repeat {
						if side == 0 && folder.navigation.id != 0 {
							w.owner.cancelSFTPLocalNavigation(w.view)
						}
						if side == 1 && w.view.pathRequest != "" {
							w.owner.cancelSFTPNavigation(w.view)
						}
						folder.resetPathInput()
					}
				}
			}
		}
		return handled
	}
	if w.view.dialog != sftpDialogNone && event.Key == gfx.KeyEnter && (event.Target == hitSFTPDialogField || event.Target == hitSFTPDialogField2 || event.Target == hitSFTPDialogSubmit) {
		w.owner.submitSFTPDialog(w.view)
		return handled
	}
	if w.headerMenuOpen() {
		if next, ok := w.ui.headerScroll.Navigate(event.Key, event.Target, true); ok {
			handled.Focus, handled.FocusVisible = next, true
			return handled
		}
		return gfx.EventResult{}
	}
	if w.ui.menuOpen || w.view.bookmarksOpen {
		scroll := &w.ui.menuScroll
		if w.view.bookmarksOpen {
			scroll = &w.view.places.scroll
		}
		if next, ok := scroll.Navigate(event.Key, event.Target, true); ok {
			handled.Focus, handled.FocusVisible = next, true
			return handled
		}
		return gfx.EventResult{}
	}
	if w.fileProperties.open || w.closeConfirm || w.externalConflict != 0 || w.view.dialog != sftpDialogNone {
		return gfx.EventResult{}
	}
	if result, handled := w.directoryTreeKey(event); handled {
		return result
	}
	if delta, ok := sftpHistoryChord(event); ok {
		isText := false
		for side := range 2 {
			isText = isText || event.Target == sftpWindowPath[side] || event.Target == sftpWindowFilter[side]
		}
		if local, fileControl := w.targetSide(event.Target); fileControl && !isText {
			w.owner.navigateSFTPHistory(w.view, local, delta)
			return handled
		}
	}
	for _, id := range sftpWindowFilter {
		if event.Target == id {
			return gfx.EventResult{}
		}
	}
	if event.Target == hitSFTPWindowSplit {
		if fraction, ok := w.ui.split.FractionForKey(event.Key); ok {
			w.ui.fraction = fraction
			return handled
		}
	}
	for side, id := range sftpWindowPath {
		if event.Target == id {
			if event.Key == gfx.KeyEnter {
				if _, composing := w.folder(side == 0).pathInput().Composition(); !composing && !event.Repeat && event.Modifiers == 0 && (side == 0 || !w.view.busy) {
					w.cancelDrag()
					w.activeLocal, w.view.local = side == 0, side == 0
					w.owner.submitSFTPPath(w.view)
				}
				return handled
			}
			return gfx.EventResult{}
		}
	}
	local, index, row := w.rowTarget(event.Target)
	if row {
		w.activeLocal, w.view.local = local, local
	}
	folder := w.folder(w.activeLocal)
	switch event.Key {
	case gfx.KeyF5:
		w.queueSelection(w.activeLocal)
		return handled
	case gfx.KeyEnter:
		if row {
			w.openEntry(local, index)
			return handled
		}
	case gfx.KeyF2:
		if !w.activeLocal && !w.view.busy && w.connected() {
			w.cancelDrag()
			w.view.local = false
			w.owner.openSFTPDialog(w.view, sftpDialogRename)
			return handled
		}
	case gfx.KeyDelete:
		if !w.activeLocal && !w.view.busy && w.connected() {
			w.cancelDrag()
			w.view.local = false
			w.owner.openSFTPDialog(w.view, sftpDialogDelete)
			return handled
		}
	case gfx.KeyBackspace:
		parent := path.Dir(folder.path)
		if w.activeLocal {
			parent = filepath.Dir(folder.path)
		}
		w.navigate(w.activeLocal, parent)
		return handled
	case gfx.KeyUp, gfx.KeyDown, gfx.KeyHome, gfx.KeyEnd, gfx.KeyPageUp, gfx.KeyPageDown:
		if len(folder.rows) == 0 {
			return handled
		}
		next := folder.selected
		switch event.Key {
		case gfx.KeyUp:
			next--
		case gfx.KeyDown:
			next++
		case gfx.KeyHome:
			next = 0
		case gfx.KeyEnd:
			next = len(folder.rows) - 1
		case gfx.KeyPageUp:
			next -= folder.visibleRows
		case gfx.KeyPageDown:
			next += folder.visibleRows
		}
		next = max(0, min(next, len(folder.rows)-1))
		folder.selectRow(next, event.Modifiers&gfx.ModifierShift)
		w.selectionChanged(w.activeLocal)
		side := sideIndex(w.activeLocal)
		top, bottom := float32(next)*sftpRowHeight, float32(next+1)*sftpRowHeight
		w.ui.offset[side] = max(0, min(w.ui.offset[side], top))
		if bottom > w.ui.offset[side]+w.ui.list[side].Height {
			w.ui.offset[side] = bottom - w.ui.list[side].Height
		}
		handled.Focus = sftpWindowRows[side].At(next)
		handled.FocusVisible = true
		return handled
	}
	return gfx.EventResult{}
}

func (w *sftpWindow) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 {
		return gfx.EventResult{}
	}
	if w.view.worker == nil && strings.ToLower(event.Text) != "w" {
		return gfx.EventResult{}
	}
	for _, id := range sftpWindowPath {
		if event.Target == id && strings.ToLower(event.Text) != "l" && strings.ToLower(event.Text) != "w" && strings.ToLower(event.Text) != "f" && strings.ToLower(event.Text) != "b" && strings.ToLower(event.Text) != "o" {
			return gfx.EventResult{}
		}
	}
	if w.fileProperties.open || w.view.dialog != sftpDialogNone || w.bookmark.open || w.closeConfirm || w.externalConflict != 0 || w.ui.menuOpen || w.view.bookmarksOpen || w.headerMenuOpen() {
		return gfx.EventResult{}
	}
	for _, id := range sftpWindowFilter {
		if event.Target == id && strings.ToLower(event.Text) != "f" && strings.ToLower(event.Text) != "l" && strings.ToLower(event.Text) != "w" {
			return gfx.EventResult{}
		}
	}
	if local, ok := w.targetSide(event.Target); ok {
		w.activeLocal, w.view.local = local, local
	}
	handled := gfx.EventResult{Handled: true, Repaint: true}
	switch strings.ToLower(event.Text) {
	case "f":
		if event.Modifiers != keymap.Primary {
			return gfx.EventResult{}
		}
		w.cancelDrag()
		w.folder(w.activeLocal).filterOpen = true
		handled.Focus, handled.FocusVisible = sftpWindowFilter[sideIndex(w.activeLocal)], true
		return handled
	case "o":
		w.editBookmark(nil)
		return handled
	case "b":
		if event.Modifiers&(gfx.ModifierAlt|gfx.ModifierShift) == 0 && !w.navigationBusy() {
			w.editBookmark(w.currentBookmark())
			return handled
		}
	case "a":
		folder := w.folder(w.activeLocal)
		for index := range folder.rows {
			folder.rows[index].selected = true
		}
		w.selectionChanged(w.activeLocal)
		return handled
	case "l":
		side := sideIndex(w.activeLocal)
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(w.folder(w.activeLocal).pathInput())
		handled.Focus = sftpWindowPath[side]
		return handled
	case "r":
		w.refresh(w.activeLocal)
		return handled
	case "w":
		w.requestHostClose()
		return handled
	}
	return gfx.EventResult{}
}

func (w *sftpWindow) scroll(event gfx.ScrollEvent) gfx.EventResult {
	if w.bookmark.open {
		b := &w.bookmark
		if b.scopeOpen {
			return gfx.EventResult{Handled: true, Repaint: b.scopeMenu.Wheel(event)}
		}
		if pointInRect(event.X, event.Y, b.list) {
			before := b.offset
			b.offset = max(0, min(b.area.MaxOffsetY, b.offset+event.DeltaY))
			return gfx.EventResult{Handled: true, Repaint: before != b.offset}
		}
		return gfx.EventResult{Handled: true}
	}
	if w.ui.profileQuickOpen {
		return gfx.EventResult{Handled: true, Repaint: w.ui.profileQuickScroll.Wheel(event)}
	}
	if w.headerMenuOpen() {
		return gfx.EventResult{Handled: true, Repaint: w.ui.headerScroll.Wheel(event)}
	}
	if w.ui.menuOpen {
		return gfx.EventResult{Handled: true, Repaint: w.ui.menuScroll.Wheel(event)}
	}
	if w.view.bookmarksOpen {
		return gfx.EventResult{Handled: true, Repaint: w.view.places.scroll.Wheel(event)}
	}
	if w.fileProperties.open || w.view.dialog != sftpDialogNone || w.bookmark.open || w.closeConfirm || w.externalConflict != 0 {
		return gfx.EventResult{Handled: true}
	}
	if result, handled := w.directoryTreeScroll(event); handled {
		return result
	}
	for side, bounds := range w.ui.list {
		if pointInRect(event.X, event.Y, bounds) {
			before := w.ui.offset[side]
			if event.Precise {
				w.folder(side == 0).wheel = 0
				w.ui.offset[side] = max(0, min(w.ui.bars[side].MaxOffsetY, before+event.DeltaY))
			} else {
				folder := w.folder(side == 0)
				scrollFolder(folder, event)
				w.ui.offset[side] = max(0, min(w.ui.bars[side].MaxOffsetY, float32(folder.first)*sftpRowHeight))
			}
			return gfx.EventResult{Handled: true, Repaint: before != w.ui.offset[side]}
		}
	}
	if pointInRect(event.X, event.Y, w.ui.queueBounds) {
		before := w.ui.queueOffset
		w.ui.queueOffset = max(0, min(w.ui.queueBar.MaxOffsetY, w.ui.queueOffset+event.DeltaY))
		return gfx.EventResult{Handled: true, Repaint: before != w.ui.queueOffset}
	}
	return gfx.EventResult{}
}

func (w *sftpWindow) fileDrop(event gfx.FileDropEvent) gfx.EventResult {
	if event.Phase == gfx.FileDropLeave {
		w.cancelDrag()
		return gfx.EventResult{Repaint: true}
	}
	if w.fileProperties.open {
		w.cancelDrag()
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	_, target, _, valid := w.dropTarget(event.X, event.Y, true)
	if !valid {
		w.cancelDrag()
		return gfx.EventResult{Repaint: true}
	}
	if event.Phase == gfx.FileDropOver {
		w.drag = sftpWindowDrag{active: true, fromLocal: true, at: gfx.Point{X: event.X, Y: event.Y}}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	w.cancelDrag()
	for _, location := range event.Paths {
		info, err := os.Lstat(location)
		if err != nil {
			w.notices.show(w.owner.messages.SFTPWindow.Title, err.Error(), components.ToastError)
			continue
		}
		w.queueEntries(true, []sftpclient.Entry{{Name: filepath.Base(location), Mode: info.Mode()}}, filepath.Dir(location), target)
	}
	return gfx.EventResult{Handled: true, Repaint: true}
}
