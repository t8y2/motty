package workbench

import (
	"cmp"
	"fmt"
	"slices"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

const (
	sftpQueueHeaderHeight = float32(24)
	// sftpQueueConflictHeight is the recovery bar a window's queue shows above
	// its rows while transfers are waiting on a destination that exists.
	sftpQueueConflictHeight = float32(31)
)

func sftpQueueHeight(size gfx.Size, count, conflicts int) float32 {
	visible := min(max(2, int(size.Height*0.28/transferRowHeight)), 5, count)
	height := sftpQueueHeaderHeight + float32(visible)*transferRowHeight
	if conflicts > 0 {
		height += sftpQueueConflictHeight
	}
	return height
}

// Keep actionable conflicts at the top. The rest of the flat list shows
// running tasks, the FIFO waiting queue, then recent history.
func sftpQueuePriority(entry *transferEntry) (int, int) {
	if entry.conflict {
		return 0, transferFilterFailed
	}
	switch entry.snapshot.State {
	case sftpclient.TransferRunning:
		return 1, transferFilterRunning
	case sftpclient.TransferQueued, sftpclient.TransferPaused:
		return 2, transferFilterWaiting
	default:
		return 3, transferFilterEnded
	}
}

func compareSFTPQueueEntries(a, b *transferEntry) int {
	left, group := sftpQueuePriority(a)
	right, _ := sftpQueuePriority(b)
	if order := cmp.Compare(left, right); order != 0 {
		return order
	}
	return compareTransferOrder(group, a, b)
}

func (w *sftpWindow) queueCount() int {
	center, view := &w.owner.transfers, &w.ui
	if view.queueRevision != center.revision {
		view.queueEntries = view.queueEntries[:0]
		view.queueConflicts = 0
		for i := range center.entries {
			e := &center.entries[i]
			if e.owner == w.view && !e.removed {
				view.queueEntries = append(view.queueEntries, i)
				if e.conflict {
					view.queueConflicts++
				}
			}
		}
		if view.queueConflicts == 0 {
			view.queueBatch = transferBatch{}
		}
		slices.SortFunc(view.queueEntries, func(a, b int) int {
			return compareSFTPQueueEntries(&center.entries[a], &center.entries[b])
		})
		view.queueRevision = center.revision
	}
	return len(view.queueEntries)
}

func (w *sftpWindow) declareQueue(c ui.C, bounds, origin gfx.Rect) {
	view := &w.ui
	top := sftpQueueHeaderHeight
	if view.queueConflicts > 0 {
		w.declareQueueConflictBar(c, bounds, origin)
		top += sftpQueueConflictHeight
	}
	list := gfx.Rect{X: bounds.X, Y: bounds.Y + top, Width: bounds.Width, Height: max(bounds.Height-top, 0)}
	view.queueBounds = list
	view.queueDetails.beginPaint()
	contentHeight := float32(len(view.queueEntries)) * transferRowHeight
	if list.Height <= 0 || list.Width <= 12 {
		view.queueScroll = nil
		view.queueDetails.dismiss()
		return
	}
	area := ui.Scroll(c).Absolute().Left(list.X-origin.X).Top(list.Y-origin.Y).
		Width(list.Width).Height(list.Height).Clip().AlwaysScrollbars().
		ScrollbarLabels(w.owner.messages.SFTPTransferQueue, "")
	view.queueScroll = area.ScrollState()
	offset := float32(0)
	if view.queueScroll != nil {
		// Clamp before declaring the rows, as the painted queue did, so a list
		// that shrank does not ask for rows past its end.
		offset = min(max(view.queueScroll.Offset, 0), max(contentHeight-list.Height, 0))
		view.queueScroll.Offset = offset
	}
	area.Children(func() {
		// The slot gives the area its content height; the rows are placed by it.
		ui.Slot(c).Width(1).Height(contentHeight)
		width := list.Width - 12 // reserve the scrollbar's hit area
		first := int(offset / transferRowHeight)
		for i := first; i < len(view.queueEntries); i++ {
			top := float32(i)*transferRowHeight - offset
			if top >= list.Height {
				break
			}
			index := view.queueEntries[i]
			id := w.owner.transfers.entries[index].id
			row := ui.Row(c.KeyInt(id)).Absolute().Top(top).Left(0).
				Width(width).Height(transferRowHeight).PadX(8).Gap(9).Items(ui.Center)
			w.owner.declareTransferTaskRow(c, row, sftpWindowQueueRows.At(id), index, width, &view.queueDetails, true)
		}
		w.owner.declareTransferDetailCard(c, &view.queueDetails, sftpWindowQueueRows, hitSFTPQueueDetails, hitSFTPQueueDetailsDismiss, w.queueDetailsAllowed())
	})
}

// declareQueueConflictBar offers one choice for every conflict this window's
// queue holds, so a batch that hit an existing destination is not resolved one
// row at a time. Overwriting asks first, exactly as the transfer center does.
func (w *sftpWindow) declareQueueConflictBar(c ui.C, bounds, origin gfx.Rect) {
	view, m := &w.ui, w.owner.messages.Transfers
	ui.Row(c).Absolute().Left(bounds.X - origin.X).Top(bounds.Y - origin.Y + sftpQueueHeaderHeight).
		Width(bounds.Width).Height(sftpQueueConflictHeight).PadX(8).Gap(6).Items(ui.Center).Children(func() {
		if len(view.queueBatch.targets) > 0 {
			ui.Text(c, view.queueBatch.title).FontSize(w.owner.Theme().Metrics.FontSizeS).Color(w.owner.pal.muted).Ellipsis().Grow(1)
			w.declareQueueAction(c, hitSFTPQueueReplaceCancel, w.owner.messages.Cancel, false)
			w.declareQueueAction(c, hitSFTPQueueReplaceConfirm, view.queueBatch.action, true)
			return
		}
		ui.Text(c, fmt.Sprintf(m.ConflictCountFmt, view.queueConflicts)).FontSize(w.owner.Theme().Metrics.FontSizeS).Color(w.owner.pal.muted).Ellipsis().Grow(1)
		w.declareQueueAction(c, hitSFTPQueueReplaceAll, m.ReplaceAll, true)
		w.declareQueueAction(c, hitSFTPQueueSkipAll, m.SkipAll, false)
	})
}

func (w *sftpWindow) declareQueueAction(c ui.C, id gfx.ID, label string, danger bool) {
	pal := w.owner.pal
	ink, fill, edge := pal.text, gfx.Color{}, pal.line
	if danger {
		ink, fill, edge = pal.danger, withAlpha(pal.danger, 0.12), withAlpha(pal.danger, 0.6)
	}
	ui.ButtonBase(c, label).Height(21).Shrink(0).PadX(8).Radius(radius.control).Background(fill).Border(1, edge).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label}).Children(func() {
		ui.Text(c, label).FontSize(w.owner.Theme().Metrics.FontSizeS).Color(ink)
	})
}

// clickQueueConflict handles the window's bulk conflict choice. Confirm and
// cancel only make sense while a captured replace is pending; the skip path
// runs straight away, as the center's does.
func (w *sftpWindow) clickQueueConflict(id gfx.ID) bool {
	view := &w.ui
	switch id {
	case hitSFTPQueueReplaceCancel:
		view.queueBatch = transferBatch{}
		return true
	case hitSFTPQueueReplaceConfirm:
		batch := view.queueBatch
		view.queueBatch = transferBatch{}
		if len(batch.targets) > 0 {
			w.owner.dispatchTransferBatch(batch)
		}
		return true
	case hitSFTPQueueSkipAll:
		if batch := w.owner.captureViewTransferBatch(w.view, sftpclient.ExistingSkip); len(batch.targets) > 0 {
			w.owner.dispatchTransferBatch(batch)
		}
		return true
	case hitSFTPQueueReplaceAll:
		if batch := w.owner.captureViewTransferBatch(w.view, sftpclient.ExistingReplace); len(batch.targets) > 0 {
			view.queueBatch = batch
		}
		return true
	}
	return false
}

// queueDetailsAllowed reports whether the window's transient states leave the
// queue's pointer preview alone.
func (w *sftpWindow) queueDetailsAllowed() bool {
	view, state := &w.ui, w.owner
	return !w.drag.armed && !w.drag.active && !view.split.Dragging() &&
		(view.queueScroll == nil || !view.queueScroll.Dragging()) &&
		!view.menuOpen && !w.headerMenuOpen() && !view.profileQuickOpen &&
		!w.view.bookmarksOpen && !w.bookmark.open && !w.fileProperties.open &&
		w.view.dialog == sftpDialogNone && !w.closeConfirm && w.externalConflict == 0 &&
		!(w.window == nil && (state.transfers.open || !state.transferCenterAvailable()))
}

func (w *sftpWindow) paintQueue(canvas *gfx.Canvas, bounds, viewport gfx.Rect) {
	view, state := &w.ui, w.owner
	if bounds.Width <= 0 || bounds.Height <= sftpQueueHeaderHeight {
		view.queueBounds, view.queueScroll = gfx.Rect{}, nil
		view.queueDetails.dismiss()
		return
	}
	theme, label := state.Theme(), state.messages.SFTPTransferQueue
	canvas.Label(label, gfx.Rect{X: bounds.X + 8, Y: bounds.Y, Width: bounds.Width - 16, Height: 20},
		gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightSemibold, Color: theme.Foreground})

	if !w.queueDetailsAllowed() {
		view.queueDetails.dismiss()
	}
}

func (w *sftpWindow) clickQueueDetails(id gfx.ID) bool {
	details := &w.ui.queueDetails
	if id == hitSFTPQueueDetailsDismiss {
		details.dismiss()
		return true
	}
	if entry, ok := id.In(sftpWindowQueueRows); ok {
		if details.pinned == entry {
			details.dismiss()
		} else {
			details.pinned = entry
		}
		return true
	}
	return false
}
