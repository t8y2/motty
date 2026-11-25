package workbench

import (
	"fmt"
	"math"
	"path"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/keymap"
	"motty/internal/sftpclient"
)

type sftpWindowDrag struct {
	armed, active, fromLocal bool
	start, at                gfx.Point
	sourceDir                string
	entries                  []sftpclient.Entry
	// screen and outside place the pointer among this process's windows; target
	// is where the release would land, and the highlights name what is lit.
	screen         gfx.Point
	outside        bool
	target         sftpDragTarget
	highlighted    *sftpWindow
	highlightedTab *State
}

// sftpDragTarget is where a file drag would land: a pane of this or another
// server window, or the tab of another server. A file tab names a state
// and a session; the target folder is read when the release happens.
type sftpDragTarget struct {
	kind  uint8 // sftpDragNone, sftpDragPeer, sftpDragTab, sftpDragMove
	peer  *sftpWindow
	local bool // the target pane is this computer's
	dir   string
	row   int
	move  sftpMoveTarget
	state *State
	tab   uint32
}

const (
	sftpDragNone uint8 = iota
	sftpDragPeer
	sftpDragTab
	sftpDragMove
)

func (w *sftpWindow) cancelDrag() {
	w.clearDragHighlights()
	w.drag = sftpWindowDrag{}
	pending := w.deferredListings
	w.deferredListings = [2]func(){}
	for _, apply := range pending {
		if apply != nil && !w.closed {
			apply()
		}
	}
}

// clearDragHighlights takes the drop picture off every window it lit.
func (w *sftpWindow) clearDragHighlights() {
	if peer := w.drag.highlighted; peer != nil {
		peer.foreign = sftpForeignDrag{}
		peer.invalidate()
	}
	if state := w.drag.highlightedTab; state != nil && state.foreignFileTab != 0 {
		state.foreignFileTab = 0
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}
	w.drag.highlighted, w.drag.highlightedTab = nil, nil
}

// trackFileDrag resolves where a file drag would land: a file tab of any main
// window, this window's own panes, or another server window under the pointer.
func (w *sftpWindow) trackFileDrag(event gfx.PointerEvent) {
	w.drag.screen = gfx.Point{X: event.ScreenX, Y: event.ScreenY}
	w.drag.at = gfx.Point{X: event.X, Y: event.Y}
	w.drag.outside = event.X < 0 || event.Y < 0 || event.X >= event.Viewport.Width || event.Y >= event.Viewport.Height
	// A tab's strip is in the same window as its panes; a native window's is
	// somewhere else, and only a pointer outside its content can be over it.
	if w.window == nil || w.drag.outside {
		if state, tab, ok := w.owner.fileDragTabAt(w.drag.screen, w); ok {
			w.setDragPeerHighlight(nil, false, false, "", -1)
			w.setDragTabHighlight(state, tab)
			w.drag.target = sftpDragTarget{kind: sftpDragTab, state: state, tab: tab}
			return
		}
	}
	w.setDragTabHighlight(nil, 0)
	if !w.drag.outside || event.Viewport.Width <= 0 {
		if local, target, row, valid := w.dropTarget(event.X, event.Y, w.drag.fromLocal); valid {
			w.setDragPeerHighlight(nil, false, false, "", -1)
			w.drag.target = sftpDragTarget{kind: sftpDragPeer, peer: w, local: local, dir: target, row: row}
			return
		}
		if move := w.sameSideMoveTarget(event.X, event.Y); move.kind != sftpMoveTargetNone {
			w.setDragPeerHighlight(nil, false, false, "", -1)
			w.drag.target = sftpDragTarget{kind: sftpDragMove, peer: w, local: w.drag.fromLocal, dir: move.dir, row: move.row, move: move}
			return
		}
	}
	if !w.drag.outside {
		w.setDragPeerHighlight(nil, false, false, "", -1)
		w.drag.target = sftpDragTarget{}
		return
	}
	if peer := w.owner.fileDropWindowAt(w.drag.screen, w); peer != nil {
		if frame, ok := peer.frame(); ok {
			if local, target, row, valid := peer.dropTargetAny(w.drag.screen.X-frame.X, w.drag.screen.Y-frame.Y); valid && !(w.drag.fromLocal && local) {
				w.setDragPeerHighlight(peer, w.drag.fromLocal, local, target, row)
				w.drag.target = sftpDragTarget{kind: sftpDragPeer, peer: peer, local: local, dir: target, row: row}
				return
			}
		}
	}
	// The pointer is outside this window and over no other Motty window: the
	// drag leaves the application, and the desktop takes it from here. Only a
	// held move starts it; the release that follows is the desktop's.
	if event.Kind == gfx.PointerMove && !w.owner.ownWindowAt(w.drag.screen, w) {
		entries := w.drag.entries
		directory := w.drag.sourceDir
		fromLocal := w.drag.fromLocal
		w.cancelDrag()
		w.owner.dragFilesOut(w, w.view, entries, directory, fromLocal)
		return
	}
	w.setDragPeerHighlight(nil, false, false, "", -1)
	w.drag.target = sftpDragTarget{}
}

// setDragPeerHighlight lights the pane of the window a drop would land in. The
// source window paints its own pane itself.
func (w *sftpWindow) setDragPeerHighlight(peer *sftpWindow, fromLocal, local bool, target string, row int) {
	if w.drag.highlighted != peer {
		if old := w.drag.highlighted; old != nil {
			old.foreign = sftpForeignDrag{}
			old.invalidate()
		}
		w.drag.highlighted = peer
	}
	if peer == nil || peer == w {
		return
	}
	peer.foreign = sftpForeignDrag{active: true, fromLocal: fromLocal, local: local, target: target, row: row}
	peer.invalidate()
}

// setDragTabHighlight lights the file tab another window's drag would fill.
func (w *sftpWindow) setDragTabHighlight(state *State, tab uint32) {
	if w.drag.highlightedTab == state && (state == nil || state.foreignFileTab == tab) {
		return
	}
	if old := w.drag.highlightedTab; old != nil {
		old.foreignFileTab = 0
		if old.options.Invalidate != nil {
			old.options.Invalidate()
		}
	}
	w.drag.highlightedTab = nil
	if state == nil || tab == 0 {
		return
	}
	state.foreignFileTab = tab
	w.drag.highlightedTab = state
	if state.options.Invalidate != nil {
		state.options.Invalidate()
	}
}

// dropFileDrag queues what the release would move: a same-window drop keeps its
// old path; a peer pane or another server's tab decides the direction by the
// two sides.
func (w *sftpWindow) dropFileDrag() {
	target := w.drag.target
	switch target.kind {
	case sftpDragPeer:
		if target.peer == w {
			w.queueEntries(w.drag.fromLocal, w.drag.entries, w.drag.sourceDir, target.dir)
			return
		}
		w.dropOnPeer(target)
	case sftpDragMove:
		if target.local {
			w.owner.moveLocalEntries(w.view, w.drag.sourceDir, w.drag.entries, target.dir)
		} else {
			w.owner.moveSFTPEntries(w.view, w.drag.sourceDir, w.drag.entries, target.dir)
		}
	case sftpDragTab:
		w.dropOnTab(target)
	}
}

func (w *sftpWindow) dropOnPeer(target sftpDragTarget) {
	peer := target.peer
	switch {
	case w.drag.fromLocal && !target.local:
		peer.queueEntries(true, w.drag.entries, w.drag.sourceDir, target.dir)
	case !w.drag.fromLocal && target.local:
		w.queueEntries(false, w.drag.entries, w.drag.sourceDir, target.dir)
		peer.localRefresh = true
	case !w.drag.fromLocal && !target.local:
		queued, rejected := peer.owner.queueRemoteCopies(peer.view, copySourceFromView(w.view), w.drag.sourceDir, target.dir, w.drag.entries)
		w.reportRemoteCopy(queued, rejected, target.dir)
	}
}

func (w *sftpWindow) dropOnTab(target sftpDragTarget) {
	if target.state == nil {
		return
	}
	targetWindow := target.state.sftpWindows[target.tab]
	if targetWindow == nil || targetWindow.closed || targetWindow.view.worker == nil {
		return
	}
	dir := targetWindow.folder(false).path
	if dir == "" {
		return
	}
	if w.drag.fromLocal {
		targetWindow.queueEntries(true, w.drag.entries, w.drag.sourceDir, dir)
		return
	}
	queued, rejected := target.state.queueRemoteCopies(targetWindow.view, copySourceFromView(w.view), w.drag.sourceDir, dir, w.drag.entries)
	w.reportRemoteCopy(queued, rejected, dir)
}

func (w *sftpWindow) reportRemoteCopy(queued, rejected int, target string) {
	if queued > 0 {
		w.notices.show(w.owner.messages.SFTPWindow.Title,
			fmt.Sprintf(w.owner.messages.SFTPCopiedCountFmt, queued, target), components.ToastSuccess)
	}
	if rejected > 0 {
		w.notices.show(w.owner.messages.SFTPWindow.Title,
			fmt.Sprintf(w.owner.messages.SFTPWindow.QueueRejectedFmt, rejected), components.ToastWarning)
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
	if view.panelDrag.state.Dragging() {
		view.deferredPanelListings[sideIndex(local)] = apply
		return true
	}
	return false
}

func (w *sftpWindow) rowTarget(id gfx.ID) (bool, int, bool) {
	for side := range 2 {
		if scope := w.folder(side == 0).listRowsScope; !scope.IsZero() {
			if index, ok := id.In(scope); ok {
				return side == 0, index, true
			}
		}
	}
	// The scopes the rows had before they were a List still name them: the
	// tests, and the window's own focus results that predate the List, speak in
	// them.
	for side, scope := range sftpWindowRows {
		if index, ok := id.In(scope); ok {
			return side == 0, index, true
		}
	}
	return false, 0, false
}

// rowID is the identity of row index on a side: the List's, once it has
// declared its rows, else the scope the rows had before it did.
func (w *sftpWindow) rowID(side, index int) gfx.ID {
	if scope := w.folder(side == 0).listRowsScope; !scope.IsZero() {
		return scope.At(index)
	}
	return sftpWindowRows[side].At(index)
}

// listBlocked reports a layer above the file lists that owns the keyboard or
// the pointer, so a row key must not reach the folder behind it.
func (w *sftpWindow) listBlocked() bool {
	return w.view.worker == nil || w.fileProperties.open || w.bookmark.open || w.closeConfirm || w.externalConflict != 0 ||
		w.view.dialog != sftpDialogNone || w.ui.menuOpen || w.view.bookmarksOpen || w.headerMenuOpen()
}

// listTypeahead moves to the row whose name begins with the letters typed. The
// window page's List has its own typeahead and sees the key first; this remains
// for the window's own shortcut route, which is reached when a pane's List has
// not been painted yet.
func (w *sftpWindow) listTypeahead(event gfx.KeyEvent) (gfx.EventResult, bool) {
	text := event.Text
	if text == "" && event.Key == gfx.KeySpace {
		text = " "
	}
	if text == "" || w.listBlocked() {
		return gfx.EventResult{}, false
	}
	for side := range 2 {
		folder := w.folder(side == 0)
		scope := folder.listRowsScope
		if scope.IsZero() || len(folder.rows) == 0 {
			continue
		}
		if _, inside := event.Target.In(scope); !inside {
			continue
		}
		rows := components.MenuTypeaheadRows{
			N:     len(folder.rows),
			ID:    func(i int) gfx.ID { return scope.At(i) },
			Label: func(i int) string { return folder.rows[i].entry.Name },
		}
		id, ok := w.ui.typeahead[side].TypeRows(text, time.Now(), rows, event.Target)
		if !ok {
			return gfx.EventResult{}, false
		}
		index, ok := id.In(scope)
		if !ok || index < 0 || index >= len(folder.rows) {
			return gfx.EventResult{}, false
		}
		folder.selectRow(index, 0)
		folder.list.Select(index)
		folder.list.ScrollIntoView(index)
		w.selectionChanged(side == 0)
		return gfx.EventResult{Handled: true, Repaint: true, Focus: id, FocusVisible: true}, true
	}
	return gfx.EventResult{}, false
}

// listOffset is the rows' offset for side, handing the List an offset the model
// (a history restore, a filter, a sort, a test) wrote since the last paint.
func (w *sftpWindow) listOffset(side int) float32 {
	if w.ui.offset[side] != w.ui.offsetSynced[side] {
		return w.setListOffset(side, w.ui.offset[side])
	}
	return w.ui.offset[side]
}

// setListOffset moves the List to offset and mirrors it into the fields the
// drop target, the model and the tests read between paints.
func (w *sftpWindow) setListOffset(side int, offset float32) float32 {
	folder := w.folder(side == 0)
	offset = max(offset, 0)
	if scroll := folder.listScroll; scroll != nil {
		if room := scroll.Max(); room > 0 && offset > room {
			offset = room
		}
		scroll.ScrollTo(offset)
		offset = scroll.Offset
	}
	w.ui.offset[side], w.ui.offsetSynced[side] = offset, offset
	folder.scrollOffset = offset
	folder.first = max(0, int(offset/sftpRowHeight))
	return offset
}

// Pointer capture retains the source hit during a drag. Resolve destinations
// from current coordinates, never that captured hit or the active terminal tab.
func (w *sftpWindow) dropTarget(x, y float32, fromLocal bool) (local bool, target string, row int, valid bool) {
	target, row, valid = w.dropTargetSide(!fromLocal, x, y)
	return !fromLocal, target, row, valid
}

// dropTargetAny is the destination from another window's drag: either pane of
// this window, whichever the pointer is over, since the source pane may be a
// server as well.
func (w *sftpWindow) dropTargetAny(x, y float32) (local bool, target string, row int, valid bool) {
	for side := range 2 {
		local = side == 0
		if target, row, valid = w.dropTargetSide(local, x, y); valid {
			return
		}
	}
	return false, "", -1, false
}

// dropTargetSide is the folder one pane offers a drop at.
func (w *sftpWindow) dropTargetSide(local bool, x, y float32) (target string, row int, valid bool) {
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

// sameSideMoveTarget resolves a folder destination on the source side of a
// file drag: a subfolder row in the list, a directory tree node, or an ancestor
// breadcrumb.
func (w *sftpWindow) sameSideMoveTarget(x, y float32) sftpMoveTarget {
	if !w.drag.active || len(w.drag.entries) == 0 {
		return sftpMoveTarget{}
	}
	local := w.drag.fromLocal
	if w.navigationBusy() || w.bookmark.open || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 || w.view.bookmarksOpen || w.ui.menuOpen || w.headerMenuOpen() {
		return sftpMoveTarget{}
	}
	if local && w.view.localBusy {
		return sftpMoveTarget{}
	}
	if !local && (!w.connected() || w.view.busy) {
		return sftpMoveTarget{}
	}
	side := sideIndex(local)
	folder := w.folder(local)
	if folder.path == "" {
		return sftpMoveTarget{}
	}
	// 1. Subfolder row in the same pane's file list.
	if pointInRect(x, y, w.ui.list[side]) {
		index := int((y - w.ui.list[side].Y + w.ui.offset[side]) / sftpRowHeight)
		if index >= 0 && index < len(folder.rows) {
			entry := folder.rows[index].entry
			if entry.IsDir() && !entry.LinkBroken {
				targetDir := joinSFTPPath(local, folder.path, entry.Name)
				if validFileMoveTarget(local, w.drag.sourceDir, w.drag.entries, targetDir) {
					return sftpMoveTarget{
						kind: sftpMoveTargetRow,
						dir:  targetDir,
						row:  index,
						rect: gfx.Rect{
							X:      w.ui.list[side].X,
							Y:      w.ui.list[side].Y + float32(index)*sftpRowHeight - w.ui.offset[side],
							Width:  w.ui.list[side].Width,
							Height: sftpRowHeight,
						},
						clip: w.ui.list[side],
					}
				}
			}
		}
		return sftpMoveTarget{}
	}
	// 2. Directory tree node on the same side.
	tree := &w.directoryTree[side]
	if tree.enabled && tree.bounds.Width > 0 && tree.bounds.Height > 0 && pointInRect(x, y, tree.bounds) {
		index := int((y - tree.bounds.Y + tree.scrollOffset) / sftpDirectoryTreeRowHeight)
		if index >= 0 && index < len(tree.visible) {
			node := tree.visible[index].node
			if node != nil && validFileMoveTarget(local, w.drag.sourceDir, w.drag.entries, node.path) {
				return sftpMoveTarget{
					kind: sftpMoveTargetTree,
					dir:  node.path,
					row:  index,
					rect: gfx.Rect{
						X:      tree.bounds.X,
						Y:      tree.bounds.Y + float32(index)*sftpDirectoryTreeRowHeight - tree.scrollOffset,
						Width:  tree.bounds.Width,
						Height: sftpDirectoryTreeRowHeight,
					},
					clip: tree.bounds,
				}
			}
		}
		return sftpMoveTarget{}
	}
	// 3. Ancestor breadcrumb in the same pane's address bar.
	return sftpBreadcrumbMoveTarget(folder, local, w.drag.sourceDir, w.drag.entries, x, y)
}

// pointer sends a pointer event to the window's hand-painted chrome: the split,
// the scroll bars, the row drags and drops, and the popups. The panes' Lists are
// part of the window page now, so their clicks complete there first.
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
		// The dialog's lists, scrollbars and menus are page controls; only the
		// double click that opens a profile is the window's.
		if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && event.ClickCount == 2 {
			if index, ok := event.Target.In(hitSFTPProfileItems); ok {
				profiles := w.visibleProfiles()
				if index < len(profiles) {
					w.selectProfile(profiles[index])
					w.openProfileDraft()
					return handled
				}
			}
		}
		return gfx.EventResult{}
	}
	if w.view.bookmarksOpen {
		// The places menu is a page menu; the page owns its pointer.
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary {
		for side, id := range sftpWindowPathEdit {
			if event.Target == id {
				return w.startPathEditing(side)
			}
		}
	}
	// A double click on the divider returns the panes to half each, as the
	// painted Resizable did.
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary &&
		event.Target == hitSFTPWindowSplit && event.ClickCount >= 2 {
		span := max(w.ui.body.Width-sftpWindowSplitExtent, 0)
		w.ui.split.Ratio = .5
		w.ui.split.Pos = span / 2
		return handled
	}
	if result, handled := w.columnResizePointer(event); handled {
		return result
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
				w.drag.entries = w.drag.entries[:0]
				for _, row := range w.folder(w.drag.fromLocal).rows {
					if row.selected {
						w.drag.entries = append(w.drag.entries, row.entry)
					}
				}
				if len(w.drag.entries) == 0 {
					w.cancelDrag()
					return handled
				}
				w.drag.active = true
			}
			if w.drag.active {
				w.trackFileDrag(event)
			}
			return handled
		case gfx.PointerUp:
			if w.drag.active {
				w.trackFileDrag(event)
				w.dropFileDrag()
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
				w.folder(side == 0).clearSelection()
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
	canDrag := (!local && w.connected() && !w.view.busy) || (local && !w.view.localBusy && w.pendingLocal == "")
	if canDrag {
		w.drag = sftpWindowDrag{armed: true, fromLocal: local, start: gfx.Point{X: event.X, Y: event.Y}, at: gfx.Point{X: event.X, Y: event.Y}, sourceDir: folder.path}
	}
	return handled
}

func (w *sftpWindow) click(id gfx.ID) {
	defer w.invalidate()
	if w.view.worker == nil {
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
	if id == hitSFTPWindowMenuDismiss {
		w.ui.menuOpen = false
		return
	}
	if w.ui.menuOpen {
		w.ui.menuOpen = false
		w.clickFileMenuAction(id)
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
	if w.clickQueueConflict(id) {
		return
	}
	if w.clickQueueDetails(id) {
		return
	}
	if action, ok := tree.Local(sftpWindowQueueActions, id); ok {
		w.owner.clickTransferTask(action, w.view)
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
		if index, ok := id.In(sftpWindowBreadcrumbs[side]); ok {
			if folder := w.folder(local); index >= 0 && index < len(folder.crumbs) {
				w.navigate(local, folder.crumbs[index].path)
			}
			return
		}
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
		case sftpWindowNewFile[side]:
			if w.browse.pending != nil || local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected()) {
				return
			}
			w.activeLocal, w.view.local = local, local
			w.cancelDrag()
			w.owner.openSFTPDialog(w.view, sftpDialogCreateFile)
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
		w.owner.refreshLocalDirectory(w.view)
	} else if !w.view.busy && w.connected() {
		w.view.forgetListings()
		w.owner.listSFTP(w.view, w.view.path)
	}
}

// Only deliberate returns to the manager refresh it; wakes and paints do not
// poll. Leave interactions and in-flight navigation in charge of their rows.
func (w *sftpWindow) activate() {
	if w.closed || !w.connected() || w.listBlocked() || w.navigationBusy() || w.drag.armed || w.drag.active || w.foreign.active ||
		w.view.localBusy || w.view.editPath != "" || w.view.pathEditing || w.view.lfolder.pathEditing || w.ui.profileQuickOpen {
		return
	}
	w.refresh(true)
	if w.connected() && w.view.path != "" {
		w.view.forgetListings()
		w.owner.refreshSFTPDirectory(w.view, w.view.path)
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
	if context.ID == hitSFTPInlineRename && w.view.rename != nil {
		return &w.view.rename.input
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

func (w *sftpWindow) startPathEditing(side int) gfx.EventResult {
	folder := w.folder(side == 0)
	if !w.owner.sftpPathVisible(folder) {
		folder.resetPathInput()
	}
	folder.pathEditing = true
	w.activeLocal, w.view.local = side == 0, side == 0
	w.cancelDrag()
	return gfx.EventResult{Handled: true, Repaint: true, Focus: sftpWindowPath[side], FocusVisible: true}
}

// moveSplitter moves the pane divider by the arrow keys, as a focused Resizable
// did: five percent of the group a press, Home and End to the limits. The share
// is kept so a window resize keeps the proportion.
func (w *sftpWindow) moveSplitter(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Target != hitSFTPWindowSplit || event.Release {
		return gfx.EventResult{}, false
	}
	width := w.ui.body.Width
	least := float32(220)
	most := max(width-sftpWindowSplitExtent-least, least)
	span := max(width-sftpWindowSplitExtent, 1)
	step := span * .05
	pos := w.paneSplitWidth(width)
	switch event.Key {
	case gfx.KeyLeft, gfx.KeyUp:
		pos -= step
	case gfx.KeyRight, gfx.KeyDown:
		pos += step
	case gfx.KeyHome:
		pos = least
	case gfx.KeyEnd:
		pos = most
	default:
		return gfx.EventResult{}, false
	}
	pos = min(max(pos, least), most)
	w.ui.split.Pos = pos
	if span := width - sftpWindowSplitExtent; span > 0 {
		w.ui.split.Ratio = pos / span
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (w *sftpWindow) key(event gfx.KeyEvent) gfx.EventResult {
	if event.Release {
		return gfx.EventResult{}
	}
	if result := w.owner.sftpRenameKey(w.view, event); result.Handled {
		return result
	}
	// The List submits Enter to the window; opening the row is the window's.
	// Take it before the List's key route so a folder is entered once, not
	// twice.
	if !event.Repeat && event.Key == gfx.KeyEnter && event.Modifiers == 0 && !w.listBlocked() {
		if local, index, ok := w.rowTarget(event.Target); ok {
			w.openEntry(local, index)
			return gfx.EventResult{Handled: true, Repaint: true}
		}
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
		if w.bookmark.scopeOpen && event.Key == gfx.KeyEscape {
			// The open scope menu's focus trap owns Escape: let it fall through so
			// the runtime dismisses the list and leaves the editor open.
			return gfx.EventResult{}
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
				if scroll := w.bookmark.scroll; scroll != nil {
					top, bottom := float32(next)*52, float32(next+1)*52
					if top < scroll.Offset {
						scroll.ScrollTo(top)
					} else if bottom > scroll.Offset+scroll.Viewport {
						scroll.ScrollTo(bottom - scroll.Viewport)
					}
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
	if result, handled := w.owner.sftpOwnershipKey(w.view, event); handled {
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
		} else if len(w.ui.queueBatch.targets) > 0 {
			w.ui.queueBatch = transferBatch{}
		} else if w.ui.queueDetails.pinned != 0 {
			w.ui.queueDetails.dismiss()
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
						if !w.owner.sftpPathInput {
							folder.pathEditing = false
							handled.Focus, handled.FocusVisible = sftpWindowPathEdit[side], true
						}
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
	if w.headerMenuOpen() || w.ui.menuOpen {
		// The page owns the keys of its open menus.
		return gfx.EventResult{}
	}
	if w.view.bookmarksOpen {
		// The places menu is a page menu; the page owns its keys.
		return gfx.EventResult{}
	}
	if w.fileProperties.open || w.closeConfirm || w.externalConflict != 0 || w.view.dialog != sftpDialogNone {
		return gfx.EventResult{}
	}
	if !event.Repeat && event.Modifiers == 0 && (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) {
		for side, id := range sftpWindowPathEdit {
			if event.Target == id {
				return w.startPathEditing(side)
			}
		}
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
	// The splitter is the page's ui.Splitter; its keyboard moves take the same
	// keys a Resizable took.
	if result, ok := w.moveSplitter(event); ok {
		return result
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
	if row && event.Key != gfx.KeyTab {
		// Tabbing through rows to reach another control does not move the
		// side the toolbar actions apply to.
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
		if !w.activeLocal && !w.listBlocked() && !event.Repeat && event.Modifiers == 0 {
			w.view.local = false
			if w.owner.beginSFTPRename(w.view) {
				return handled
			}
		}
	case gfx.KeyDelete:
		if w.activeLocal {
			if !w.view.localBusy && w.pendingLocal == "" && w.browse.pending == nil {
				w.cancelDrag()
				w.view.local = true
				w.owner.openSFTPDialog(w.view, sftpDialogDelete)
				return handled
			}
			return handled
		}
		if !w.view.busy && w.connected() {
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
		handled.Focus = w.rowID(side, next)
		handled.FocusVisible = true
		return handled
	}
	return gfx.EventResult{}
}

func (w *sftpWindow) shortcut(event gfx.KeyEvent) gfx.EventResult {
	if event.Release || event.Target == hitSFTPInlineRename {
		return gfx.EventResult{}
	}
	// A printable key while a List row has the keyboard is the List's typeahead.
	// The window owns OnShortcut, so it feeds the same session a Menu uses.
	if event.Modifiers&^gfx.ModifierShift == 0 {
		if result, ok := w.listTypeahead(event); ok {
			return result
		}
	}
	if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta) == 0 {
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
	// Copy and paste are application actions with bindings the settings page
	// owns, so the window follows the chord the user kept or chose.
	if chord, ok := keymap.FromEvent(event); ok {
		switch w.owner.shortcuts.actionFor(chord) {
		case shortcutCopy:
			if event.Repeat {
				return handled
			}
			if _, err := w.owner.copySFTPSelection(w.view); err != nil {
				w.notices.show(w.owner.messages.SFTPWindow.Title, err.Error(), components.ToastError)
			}
			return handled
		case shortcutPaste:
			if event.Repeat {
				return handled
			}
			w.pasteClipboard()
			return handled
		}
	}
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
		folder.listNeedsSeed = true
		w.selectionChanged(w.activeLocal)
		return handled
	case "l":
		side := sideIndex(w.activeLocal)
		handled = w.startPathEditing(side)
		folder := w.folder(w.activeLocal)
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(folder.pathInput())
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
		// The dialog's profile list is a page ScrollArea and its scope choice a
		// page menu; the page owns the wheel over both.
		if result := w.bookmarkPage().Wheel(event); result.Handled {
			return result
		}
		return gfx.EventResult{Handled: true}
	}
	if w.ui.profileQuickOpen || w.headerMenuOpen() || w.ui.menuOpen {
		// The page owns the wheel over its open menus (they are modal there).
		return gfx.EventResult{}
	}
	if w.view.bookmarksOpen {
		// The places menu is a page menu; the page owns its wheel.
		return gfx.EventResult{}
	}
	if w.fileProperties.open || w.view.dialog != sftpDialogNone || w.bookmark.open || w.closeConfirm || w.externalConflict != 0 {
		return gfx.EventResult{Handled: true}
	}
	for side, bounds := range w.ui.list {
		if pointInRect(event.X, event.Y, bounds) {
			before := w.listOffset(side)
			folder := w.folder(side == 0)
			if event.Precise {
				// A trackpad moves exactly what it reports, as the side panel
				// does; the List keeps the offset and its scrollbar follows.
				folder.wheel = 0
				w.setListOffset(side, before+event.DeltaY)
			} else {
				// A notch is whole rows, accumulated as the side panel does, so
				// both lists move alike for the same wheel.
				scrollFolder(folder, event)
				w.setListOffset(side, float32(folder.first)*sftpRowHeight)
			}
			return gfx.EventResult{Handled: true, Repaint: before != w.ui.offset[side]}
		}
	}
	// The transfer queue is a page ScrollArea now: a wheel over it is the page's.
	return gfx.EventResult{}
}

// scrollWindow gives the wheel to the window's own surfaces first (its lists move
// whole rows, and its dialogs keep the wheel to themselves), then to the page's
// scroll areas.
func (w *sftpWindow) scrollWindow(event gfx.ScrollEvent) gfx.EventResult {
	if result := w.scroll(event); result.Handled {
		return result
	}
	return w.windowPage().Wheel(event)
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
	w.queueLocalPaths(event.Paths, target)
	return gfx.EventResult{Handled: true, Repaint: true}
}

// clickFileMenuAction runs a choice of the file list's context menu. The page menu
// has already closed it, so the choice is taken by identity alone.
func (w *sftpWindow) clickFileMenuAction(id gfx.ID) {
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
	if id == hitSFTPMenuCopyFiles {
		if _, err := w.owner.copySFTPSelection(w.view); err != nil {
			w.notices.show(w.owner.messages.SFTPWindow.Title, err.Error(), components.ToastError)
		}
	}
	if id == hitSFTPMenuPasteFiles {
		w.pasteClipboard()
	}
}
