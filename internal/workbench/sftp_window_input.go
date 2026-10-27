package workbench

import (
	"math"
	"os"
	"path"
	"path/filepath"
	"strings"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

type sftpWindowDrag struct {
	armed, active, fromLocal bool
	start, at                ui.Point
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

func (w *sftpWindow) rowTarget(id ui.ID) (bool, int, bool) {
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
	if !w.connected() || w.view.busy || w.pendingLocal != "" || w.view.dialog != sftpDialogNone || w.closeConfirm || !pointInRect(x, y, w.ui.list[side]) {
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

func (w *sftpWindow) pointer(event ui.PointerEvent) ui.EventResult {
	handled := ui.EventResult{Handled: true, Repaint: true}
	if w.closeConfirm || w.view.dialog != sftpDialogNone {
		return ui.EventResult{}
	}
	if w.ui.menuOpen {
		if event.Kind == ui.PointerDown && event.Target == hitSFTPWindowMenuDismiss {
			w.ui.menuOpen = false
			return handled
		}
		return ui.EventResult{}
	}
	if w.ui.splitting {
		if event.Kind == ui.PointerMove {
			w.ui.fraction = w.ui.split.FractionAt(event.X)
		} else if event.Kind == ui.PointerUp || event.Kind == ui.PointerExit {
			w.ui.splitting = false
		}
		return handled
	}
	if event.Kind == ui.PointerDown && event.Target == hitSFTPWindowSplit {
		if fraction, ok := w.ui.split.FractionForClick(event.ClickCount); ok {
			w.ui.fraction = fraction
		} else {
			w.ui.splitting = true
		}
		return handled
	}
	if w.ui.barDrag > 0 {
		side := w.ui.barDrag - 1
		if event.Kind == ui.PointerMove {
			if side == 2 {
				w.ui.queueOffset = w.ui.queueBar.VerticalOffsetForThumb(event.Y - w.ui.barGrab)
			} else {
				w.ui.offset[side] = w.ui.bars[side].VerticalOffsetForThumb(event.Y - w.ui.barGrab)
			}
		} else if event.Kind == ui.PointerUp || event.Kind == ui.PointerExit {
			w.ui.barDrag = 0
		}
		return handled
	}
	if event.Kind == ui.PointerDown && event.Target == hitSFTPWindowQueueBar {
		geometry := w.ui.queueBar
		w.ui.barGrab = geometry.VerticalThumb.Height / 2
		if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
			w.ui.barGrab = event.Y - geometry.VerticalThumb.Y
		}
		w.ui.barDrag = 3
		w.ui.queueOffset = geometry.VerticalOffsetForThumb(event.Y - w.ui.barGrab)
		return handled
	}
	for side, id := range sftpWindowBar {
		if event.Kind == ui.PointerDown && event.Target == id {
			geometry := w.ui.bars[side]
			w.ui.barGrab = geometry.VerticalThumb.Height / 2
			if event.Y >= geometry.VerticalThumb.Y && event.Y < geometry.VerticalThumb.Y+geometry.VerticalThumb.Height {
				w.ui.barGrab = event.Y - geometry.VerticalThumb.Y
			}
			w.ui.barDrag = side + 1
			w.ui.offset[side] = geometry.VerticalOffsetForThumb(event.Y - w.ui.barGrab)
			return handled
		}
	}
	if w.drag.armed || w.drag.active {
		w.drag.at = ui.Point{X: event.X, Y: event.Y}
		switch event.Kind {
		case ui.PointerMove:
			if !w.drag.active && math.Hypot(float64(event.X-w.drag.start.X), float64(event.Y-w.drag.start.Y)) >= 6 {
				w.drag.active = true
				for _, row := range w.folder(w.drag.fromLocal).rows {
					if row.selected {
						w.drag.entries = append(w.drag.entries, row.entry)
					}
				}
			}
			return handled
		case ui.PointerUp:
			if w.drag.active {
				_, target, _, valid := w.dropTarget(event.X, event.Y, w.drag.fromLocal)
				if valid {
					w.queueEntries(w.drag.fromLocal, w.drag.entries, w.drag.sourceDir, target)
				}
			}
			w.cancelDrag()
			return handled
		case ui.PointerExit:
			if !event.Captured {
				w.cancelDrag()
			}
			return handled
		}
	}
	if event.Kind != ui.PointerDown {
		return ui.EventResult{}
	}
	local, index, ok := w.rowTarget(event.Target)
	if !ok {
		return ui.EventResult{}
	}
	folder := w.folder(local)
	if index < 0 || index >= len(folder.rows) || (!local && w.view.busy) {
		return handled
	}
	w.activeLocal, w.view.local = local, local
	handled.Focus = event.Target
	if event.Button == ui.PointerButtonSecondary {
		w.cancelDrag()
		if !folder.rows[index].selected {
			folder.selectRow(index, 0)
		}
		w.ui.menuOpen = true
		w.ui.menuAt = ui.Point{X: event.X, Y: event.Y}
		return handled
	}
	if event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}
	}
	if event.ClickCount >= 2 {
		w.openEntry(local, index)
		return handled
	}
	if !folder.rows[index].selected || event.Modifiers != 0 {
		folder.selectRow(index, event.Modifiers)
	}
	if w.connected() && !w.view.busy && w.pendingLocal == "" {
		w.drag = sftpWindowDrag{armed: true, fromLocal: local, start: ui.Point{X: event.X, Y: event.Y}, at: ui.Point{X: event.X, Y: event.Y}, sourceDir: folder.path}
	}
	return handled
}

func (w *sftpWindow) click(id ui.ID) {
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
	if w.view.dialog != sftpDialogNone {
		if w.view.dialog == sftpDialogOverwrite && (id == hitSFTPDialogSubmit || id == hitSFTPDialogSkip) {
			for _, transfer := range w.view.transfers {
				if transfer.ID == w.view.dialogTransferID && transfer.Operation == sftpclient.OperationDownload {
					w.localRefresh = true
				}
			}
		}
		w.owner.clickSFTPDialog(w.view, id)
		return
	}
	if id == hitSFTPWindowMenuDismiss {
		w.ui.menuOpen = false
		return
	}
	if w.ui.menuOpen {
		w.ui.menuOpen = false
		switch id {
		case hitSFTPWindowMenuOpen:
			w.openEntry(w.activeLocal, w.folder(w.activeLocal).selected)
		case hitSFTPWindowMenuTransfer:
			w.queueSelection(w.activeLocal)
		case hitSFTPWindowMenuCopy:
			folder := w.folder(w.activeLocal)
			var names []string
			for _, row := range folder.rows {
				if row.selected {
					location := path.Join(folder.path, row.entry.Name)
					if w.activeLocal {
						location = filepath.Join(folder.path, row.entry.Name)
					}
					names = append(names, location)
				}
			}
			if err := w.owner.options.SetClipboardText(strings.Join(names, "\n")); err != nil {
				w.notices.show(w.owner.messages.SFTPWindow.Title, err.Error(), components.ToastError)
			}
		}
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
			if local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected()) {
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
	case id.Scope() == hitSFTPTransferPauseList:
		index, ok := id.In(hitSFTPTransferPauseList)
		if ok && index < len(w.view.transferRows) {
			transfer := w.view.transferRows[index].snapshot
			if transfer.State == sftpclient.TransferPaused {
				w.view.worker.Resume(transfer.ID)
			} else {
				w.view.worker.Pause(transfer.ID)
			}
		}
	case id.Scope() == hitSFTPTransferCancelList:
		index, ok := id.In(hitSFTPTransferCancelList)
		if ok && index < len(w.view.transferRows) {
			w.view.worker.CancelTransfer(w.view.transferRows[index].snapshot.ID)
		}
	case id.Scope() == hitSFTPTransferRetryList:
		index, ok := id.In(hitSFTPTransferRetryList)
		if ok {
			w.owner.retrySFTPTransfer(w.view, index)
		}
	}
}

func (w *sftpWindow) refresh(local bool) {
	if local {
		w.owner.listLocal(w.view, w.view.lfolder.path)
	} else if !w.view.busy && w.connected() {
		w.view.forgetListings()
		w.owner.listSFTP(w.view, w.view.path)
	}
}

func (w *sftpWindow) textInput(context app.TextInputContext) *app.TextInput {
	if w.closeConfirm {
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
	for side, id := range sftpWindowPath {
		if context.ID == id {
			w.activeLocal, w.view.local = side == 0, side == 0
			w.input = app.TextInput{Editor: &w.paths[side]}
			return &w.input
		}
	}
	return nil
}

func (w *sftpWindow) key(event ui.KeyEvent) ui.EventResult {
	if event.Release {
		return ui.EventResult{}
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	if event.Key == ui.KeyEscape {
		if w.closeConfirm {
			w.click(hitSFTPWindowCloseCancel)
		} else if w.view.dialog != sftpDialogNone {
			w.owner.closeSFTPDialog(w.view)
		} else {
			w.cancelDrag()
			w.ui.menuOpen = false
			for side, id := range sftpWindowPath {
				if event.Target == id {
					app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&w.paths[side])
					w.paths[side].Insert(w.folder(side == 0).path)
				}
			}
		}
		return handled
	}
	if w.closeConfirm || w.view.dialog != sftpDialogNone || w.ui.menuOpen {
		return ui.EventResult{}
	}
	if event.Target == hitSFTPWindowSplit {
		if fraction, ok := w.ui.split.FractionForKey(event.Key); ok {
			w.ui.fraction = fraction
			return handled
		}
	}
	for side, id := range sftpWindowPath {
		if event.Target == id {
			if event.Key == ui.KeyEnter {
				location := w.paths[side].String()
				if strings.TrimSpace(location) != "" {
					if side == 0 && !filepath.IsAbs(location) {
						location = filepath.Join(w.view.lfolder.path, location)
					} else if side == 1 {
						location = sftpRemotePath(w.view.path, location)
					}
					w.navigate(side == 0, location)
				}
				return handled
			}
			return ui.EventResult{}
		}
	}
	local, index, row := w.rowTarget(event.Target)
	if row {
		w.activeLocal, w.view.local = local, local
	}
	folder := w.folder(w.activeLocal)
	switch event.Key {
	case ui.KeyF5:
		w.queueSelection(w.activeLocal)
		return handled
	case ui.KeyEnter:
		if row {
			w.openEntry(local, index)
			return handled
		}
	case ui.KeyF2:
		if !w.activeLocal && !w.view.busy && w.connected() {
			w.cancelDrag()
			w.view.local = false
			w.owner.openSFTPDialog(w.view, sftpDialogRename)
			return handled
		}
	case ui.KeyDelete:
		if !w.activeLocal && !w.view.busy && w.connected() {
			w.cancelDrag()
			w.view.local = false
			w.owner.openSFTPDialog(w.view, sftpDialogDelete)
			return handled
		}
	case ui.KeyBackspace:
		parent := path.Dir(folder.path)
		if w.activeLocal {
			parent = filepath.Dir(folder.path)
		}
		w.navigate(w.activeLocal, parent)
		return handled
	case ui.KeyUp, ui.KeyDown, ui.KeyHome, ui.KeyEnd, ui.KeyPageUp, ui.KeyPageDown:
		if len(folder.rows) == 0 {
			return handled
		}
		next := folder.selected
		switch event.Key {
		case ui.KeyUp:
			next--
		case ui.KeyDown:
			next++
		case ui.KeyHome:
			next = 0
		case ui.KeyEnd:
			next = len(folder.rows) - 1
		case ui.KeyPageUp:
			next -= folder.visibleRows
		case ui.KeyPageDown:
			next += folder.visibleRows
		}
		next = max(0, min(next, len(folder.rows)-1))
		folder.selectRow(next, event.Modifiers&ui.ModifierShift)
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
	return ui.EventResult{}
}

func (w *sftpWindow) shortcut(event ui.KeyEvent) ui.EventResult {
	if event.Release || event.Modifiers&(ui.ModifierControl|ui.ModifierMeta) == 0 {
		return ui.EventResult{}
	}
	for _, id := range sftpWindowPath {
		if event.Target == id && strings.ToLower(event.Text) != "l" && strings.ToLower(event.Text) != "w" {
			return ui.EventResult{}
		}
	}
	if w.view.dialog != sftpDialogNone || w.closeConfirm || w.ui.menuOpen {
		return ui.EventResult{}
	}
	handled := ui.EventResult{Handled: true, Repaint: true}
	switch strings.ToLower(event.Text) {
	case "a":
		folder := w.folder(w.activeLocal)
		for index := range folder.rows {
			folder.rows[index].selected = true
		}
		return handled
	case "l":
		side := sideIndex(w.activeLocal)
		app.TextEvent{Kind: app.TextCommandEvent, Command: app.TextSelectAll}.Apply(&w.paths[side])
		handled.Focus = sftpWindowPath[side]
		return handled
	case "r":
		w.refresh(w.activeLocal)
		return handled
	case "w":
		w.window.RequestClose()
		return handled
	}
	return ui.EventResult{}
}

func (w *sftpWindow) scroll(event ui.ScrollEvent) ui.EventResult {
	if w.view.dialog != sftpDialogNone || w.closeConfirm {
		return ui.EventResult{Handled: true}
	}
	for side, bounds := range w.ui.list {
		if pointInRect(event.X, event.Y, bounds) {
			w.ui.offset[side] = max(0, min(w.ui.bars[side].MaxOffsetY, w.ui.offset[side]+event.DeltaY))
			return ui.EventResult{Handled: true, Repaint: true}
		}
	}
	if pointInRect(event.X, event.Y, w.ui.queueBounds) {
		w.ui.queueOffset = max(0, min(w.ui.queueBar.MaxOffsetY, w.ui.queueOffset+event.DeltaY))
		return ui.EventResult{Handled: true, Repaint: true}
	}
	return ui.EventResult{}
}

func (w *sftpWindow) fileDrop(event ui.FileDropEvent) ui.EventResult {
	if event.Phase == ui.FileDropLeave {
		w.cancelDrag()
		return ui.EventResult{Repaint: true}
	}
	_, target, _, valid := w.dropTarget(event.X, event.Y, true)
	if !valid {
		w.cancelDrag()
		return ui.EventResult{Repaint: true}
	}
	if event.Phase == ui.FileDropOver {
		w.drag = sftpWindowDrag{active: true, fromLocal: true, at: ui.Point{X: event.X, Y: event.Y}}
		return ui.EventResult{Handled: true, Repaint: true}
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
	return ui.EventResult{Handled: true, Repaint: true}
}
