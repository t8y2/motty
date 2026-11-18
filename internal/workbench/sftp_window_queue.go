package workbench

import (
	"cmp"
	"slices"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

const sftpQueueHeaderHeight = float32(24)

func sftpQueueHeight(size gfx.Size, count int) float32 {
	visible := min(max(2, int(size.Height*0.28/transferRowHeight)), 5, count)
	return sftpQueueHeaderHeight + float32(visible)*transferRowHeight
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
		for i := range center.entries {
			e := &center.entries[i]
			if e.owner == w.view && !e.removed {
				view.queueEntries = append(view.queueEntries, i)
			}
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
	list := gfx.Rect{X: bounds.X, Y: bounds.Y + sftpQueueHeaderHeight, Width: bounds.Width, Height: max(bounds.Height-sftpQueueHeaderHeight, 0)}
	view.queueBounds = list
	view.queueDetails.beginPaint()
	contentHeight := float32(len(view.queueEntries)) * transferRowHeight
	view.queueOffset = max(0, min(view.queueOffset, contentHeight-list.Height))
	if list.Height <= 0 || list.Width <= 12 {
		view.queueDetails.dismiss()
		return
	}
	ui.Column(c).Absolute().Left(list.X - origin.X).Top(list.Y - origin.Y).
		Width(list.Width).Height(list.Height).Clip().Children(func() {
		width := list.Width - 12 // reserve the scrollbar's hit area
		first := int(view.queueOffset / transferRowHeight)
		for i := first; i < len(view.queueEntries); i++ {
			top := float32(i)*transferRowHeight - view.queueOffset
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

// queueDetailsAllowed reports whether the window's transient states leave the
// queue's pointer preview alone.
func (w *sftpWindow) queueDetailsAllowed() bool {
	view, state := &w.ui, w.owner
	return view.barDrag == 0 && !w.drag.armed && !w.drag.active && !view.splitting &&
		!view.menuOpen && !w.headerMenuOpen() && !view.profileQuickOpen &&
		!w.view.bookmarksOpen && !w.bookmark.open && !w.fileProperties.open &&
		w.view.dialog == sftpDialogNone && !w.closeConfirm && w.externalConflict == 0 &&
		!(w.window == nil && (state.transfers.open || !state.transferCenterAvailable()))
}

func (w *sftpWindow) paintQueue(canvas *gfx.Canvas, bounds, viewport gfx.Rect) {
	view, state := &w.ui, w.owner
	if bounds.Width <= 0 || bounds.Height <= sftpQueueHeaderHeight {
		view.queueBounds, view.queueOffset, view.queueBar = gfx.Rect{}, 0, components.ScrollAreaGeometry{}
		view.queueDetails.dismiss()
		return
	}
	theme, label := state.Theme(), state.messages.SFTPTransferQueue
	canvas.Label(label, gfx.Rect{X: bounds.X + 8, Y: bounds.Y, Width: bounds.Width - 16, Height: 20},
		gfx.TextStyle{Size: theme.Metrics.FontSizeS, Weight: gfx.WeightSemibold, Color: theme.Foreground})
	area := components.BeginScrollArea(canvas, theme, view.queueBounds, gfx.Size{Height: float32(len(view.queueEntries)) * transferRowHeight}).
		Offset(0, view.queueOffset).VerticalScrollbar(hitSFTPWindowQueueBar, label, "")
	view.queueBar = area.EndContent()

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
