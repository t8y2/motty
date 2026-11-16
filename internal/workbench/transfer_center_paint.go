package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func (state *State) paintTransferBadge(canvas *gfx.Canvas) {
	if !state.toolbarVisible(toolbarWindow, hitTransferCenter) {
		return
	}
	center, box := &state.transfers, state.toolbarDockBounds(hitTransferCenter)
	if box.Width <= 0 {
		return
	}
	center.anchor = box
	if center.open {
		canvas.Rounded(gfx.Rect{X: box.X, Y: box.Y + box.Height - 2, Width: box.Width, Height: 2}, 1, state.pal.accent)
	}
	if center.active > 0 {
		width := max(float32(14), float32(len(center.badge))*6+6)
		badge := gfx.Rect{X: box.X + box.Width - width, Y: box.Y, Width: width, Height: 14}
		canvas.Rounded(badge, 7, state.pal.accent)
		text := badge
		text.X += (badge.Width - canvas.TextWidthWeight(center.badge, 10, gfx.WeightSemibold)) / 2
		canvas.Label(center.badge, text, gfx.TextStyle{Size: 10, Weight: gfx.WeightSemibold, Color: state.pal.onAccent})
	}
}

// transferSection is the group an SFTP window's queue card sits in: the
// failures that need a decision, the transfers moving bytes, the ones waiting
// or paused, and the finished ones.
func transferSection(snapshot sftpclient.TransferSnapshot) int {
	switch {
	case snapshot.State == sftpclient.TransferFailed, snapshot.State == sftpclient.TransferInterrupted:
		return 0
	case snapshot.State == sftpclient.TransferRunning:
		return 1
	case transferActive(snapshot):
		return 2
	default:
		return 3
	}
}

// appendTransferOrder keeps the SFTP window queue's failure-first order.
func appendTransferOrder(dst []*transferEntry, entries []transferEntry, keep func(*transferEntry) bool) []*transferEntry {
	for section := range 4 {
		for index := len(entries) - 1; index >= 0; index-- {
			entry := &entries[index]
			if keep(entry) && transferSection(entry.snapshot) == section {
				dst = append(dst, entry)
			}
		}
	}
	return dst
}

// declareTransferCard declares a card's contents. Its buttons and progress bar
// are Slots the body paints after the page.
func (state *State) declareTransferCard(c ui.C, element ui.Box, entry *transferEntry, card *transferCard) {
	pal, m := state.pal, state.messages.Transfers
	snapshot := entry.snapshot
	direction, connector := m.Download, m.From
	switch snapshot.Operation {
	case sftpclient.OperationUpload:
		direction, connector = m.Upload, m.To
	case sftpclient.OperationRemoteCopy:
		direction, connector = m.RemoteCopy, m.To
	}
	statusColor := pal.muted
	switch snapshot.State {
	case sftpclient.TransferRunning:
		statusColor = pal.accent
	case sftpclient.TransferCompleted:
		statusColor = pal.success
	case sftpclient.TransferFailed:
		statusColor = pal.danger
	case sftpclient.TransferInterrupted:
		statusColor = pal.warning
	}
	card.statusColor = statusColor
	statusHint := snapshot.Error
	if snapshot.State == sftpclient.TransferInterrupted {
		statusHint = m.InterruptedHint
	}
	element.Pad(10).Gap(3).Radius(radius.control).Border(1, pal.line).Background(pal.surface).Children(func() {
		ui.Row(c).Height(22).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
			ui.Text(c, direction).FontSize(metrics.FontSizeS).Color(pal.muted)
			ui.Text(c, entry.title).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1)
			ui.Text(c, connector).FontSize(metrics.FontSizeS).Color(pal.muted)
			ui.Text(c, entry.server).FontSize(metrics.FontSizeS).Color(pal.text).Ellipsis().MaxWidth(96)
			ui.Text(c, entry.address).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().MaxWidth(180)
			ui.Text(c, entry.status).FontSize(metrics.FontSizeS).Color(statusColor).HitWith(hitTransfersStatus.At(entry.id), entry.status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: statusHint})
		})
		card.bar = ui.Slot(c).FillWidth().Height(3).Shrink(0).MarginSides(2, 0, 2, 0)
		ui.Text(c, entry.paths).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(16).Shrink(0).HitWith(hitTransfersPaths.At(entry.id), entry.paths, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: entry.paths})
		ui.Row(c).Height(22).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
			ui.Text(c, entry.amount).FontSize(metrics.FontSizeS).Color(pal.text)
			ui.Text(c, entry.clock).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1)
			if transferActive(snapshot) && entry.owner != nil {
				ui.Text(c, entry.speed).FontSize(metrics.FontSizeS).Color(pal.accent)
				pause := state.messages.Tools.TransferPause
				if snapshot.State == sftpclient.TransferPaused {
					pause = state.messages.Tools.TransferResume
				}
				card.pause = pageButtonSlot(c, pause, true).Height(22)
				card.cancel = pageButtonSlot(c, state.messages.SFTPCancelTransfer, true).Height(22)
			} else {
				if snapshot.Retryable && entry.owner != nil {
					if entry.conflict {
						card.replace = pageButtonSlot(c, m.Replace, true).Height(22)
						card.resume = pageButtonSlot(c, m.Resume, true).Height(22)
						card.skip = pageButtonSlot(c, m.Skip, true).Height(22)
					} else {
						card.retry = pageButtonSlot(c, m.Retry, true).Height(22)
						if snapshot.Operation == sftpclient.OperationUpload || snapshot.Operation == sftpclient.OperationDownload {
							// Retry continues the kept partial; Replace starts the file again
							// for a source that changed since then.
							card.replace = pageButtonSlot(c, m.Replace, true).Height(22)
						}
					}
				}
				if transferCanReveal(snapshot) {
					id := hitTransfersReveal.At(entry.id)
					foreground, labelColor := pal.accent, pal.text
					if state.revealBusy {
						foreground, labelColor = pal.faint, pal.faint
					}
					ui.Row(c).Height(22).Shrink(0).PadX(6).Gap(4).Items(ui.Center).Radius(radius.control).Border(1, gfx.Color{}).
						Hover(ui.Look{}.Background(withAlpha(pal.accent, 0.08))).Focus(ui.Look{}.Border(pal.accent)).
						HitWith(id, m.ShowInFolder, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.ShowInFolder, Disabled: state.revealBusy}).Children(func() {
						ui.Icon(c, lucideIcons[iconFolderOpen]).Size(14, 14).Color(foreground)
						ui.Text(c, m.ShowInFolder).FontSize(metrics.FontSizeS).Color(labelColor)
					})
				}
				id := hitTransfersRemove.At(entry.id)
				ui.Row(c).Height(22).Shrink(0).PadX(6).Gap(4).Items(ui.Center).Radius(radius.control).Border(1, gfx.Color{}).
					Hover(ui.Look{}.Background(withAlpha(pal.danger, 0.08))).Focus(ui.Look{}.Border(pal.accent)).
					HitWith(id, m.Remove, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.Remove}).Children(func() {
					ui.Icon(c, lucideIcons[iconTrash]).Size(14, 14).Color(pal.danger)
					ui.Text(c, m.Remove).FontSize(metrics.FontSizeS).Color(pal.danger)
				})
			}
		})
	})
}

// declareTransferBatch declares the confirmation shown before a bulk action.
func (state *State) declareTransferBatchBody(c ui.C) {
	center, pal := &state.transfers, state.pal
	ui.Column(c).Grow(1).FillWidth().Gap(16).Children(func() {
		ui.Wrap(c, center.batch.title).FontSize(metrics.FontSizeL).Weight(gfx.WeightSemibold).Color(pal.text)
		ui.Wrap(c, center.batch.description).FontSize(metrics.FontSizeS).Color(pal.muted).
			HitWith(hitTransfersBulkDescription, center.batch.title, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: center.batch.description})
		ui.Spacer(c)
		ui.Row(c).Height(32).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			ui.Spacer(c)
			center.batchBody.cancel = pageButtonSlot(c, state.messages.Cancel, false)
			center.batchBody.confirm = pageButtonSlot(c, center.batch.action, false)
		})
	})
}

// paintTransferBatchControls paints the confirmation's buttons into the slots
// its page left.
func (state *State) paintTransferBatchControls(canvas *gfx.Canvas, theme gfx.Theme) {
	center := &state.transfers
	components.BeginButton(canvas, theme, hitTransfersBulkCancel, center.batchBody.cancel.Rect()).Secondary(state.messages.Cancel)
	components.BeginButton(canvas, theme, hitTransfersBulkConfirm, center.batchBody.confirm.Rect()).Destructive(center.batch.action)
}

// paintTransferCardsFor paints the progress bar and buttons of a set of cards.
func (state *State) paintTransferCardsFor(canvas *gfx.Canvas, theme gfx.Theme, cards []transferCard) {
	m, pal := state.messages.Transfers, state.pal
	for index := range cards {
		card := &cards[index]
		entry := card.entry
		if entry == nil {
			continue
		}
		if card.bar.Valid() {
			paintTransferProgress(canvas, card.bar.Rect(), transferFraction(entry.snapshot), card.statusColor, pal.line)
		}
		if card.pause.Valid() {
			pause := state.messages.Tools.TransferPause
			if entry.snapshot.State == sftpclient.TransferPaused {
				pause = state.messages.Tools.TransferResume
			}
			components.BeginButton(canvas, theme, hitTransfersPause.At(entry.id), card.pause.Rect()).Small().Height(22).Ghost(pause)
			components.BeginButton(canvas, theme, hitTransfersCancel.At(entry.id), card.cancel.Rect()).Small().Height(22).Ghost(state.messages.SFTPCancelTransfer)
		}
		if card.retry.Valid() {
			components.BeginButton(canvas, theme, hitTransfersRetry.At(entry.id), card.retry.Rect()).Small().Height(22).Ghost(m.Retry)
		}
		if card.replace.Valid() {
			components.BeginButton(canvas, theme, hitTransfersReplace.At(entry.id), card.replace.Rect()).Small().Height(22).Outline(m.Replace)
			if card.resume.Valid() {
				components.BeginButton(canvas, theme, hitTransfersResume.At(entry.id), card.resume.Rect()).Small().Height(22).Ghost(m.Resume)
			}
			if card.skip.Valid() {
				components.BeginButton(canvas, theme, hitTransfersSkip.At(entry.id), card.skip.Rect()).Small().Height(22).Ghost(m.Skip)
			}
		}
	}
}

// paintTransferEmpty paints the message shown when no transfer matches, in the
// room the List leaves.
func (state *State) paintTransferEmpty(canvas *gfx.Canvas, list gfx.Rect, theme gfx.Theme) {
	center, m := &state.transfers, state.messages.Transfers
	title, hint := m.EmptyFiltered, m.EmptyFilteredHint
	if !center.hasEntries {
		title, hint = m.Empty, m.EmptyHint
	}
	data := components.EmptyData{Title: title}
	if list.Height >= 136 {
		data.Description = hint
		data.Icon, data.Media = lucideIcons[iconTransfer], components.EmptyMediaIcon
	}
	canvas.Clip(list)
	components.Empty(canvas, theme, list, data)
	canvas.Unclip()
}

func paintTransferProgress(canvas *gfx.Canvas, box gfx.Rect, fraction float32, fill, background gfx.Color) {
	canvas.Rounded(box, 2, background)
	if fraction > 0 {
		box.Width *= min(fraction, 1)
		canvas.Rounded(box, 2, fill)
	}
}

func transferFraction(snapshot sftpclient.TransferSnapshot) float32 {
	if snapshot.State == sftpclient.TransferCompleted {
		return 1
	}
	if snapshot.BytesTotal <= 0 {
		return 0
	}
	return min(float32(0.99), max(0, float32(snapshot.BytesDone)/float32(snapshot.BytesTotal)))
}

func (state *State) transferCenterScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || !pointInRect(event.X, event.Y, center.surface) {
		return gfx.EventResult{}, false
	}
	if len(center.batch.targets) > 0 || !pointInRect(event.X, event.Y, center.list) {
		return gfx.EventResult{Handled: true}, true
	}
	before, step := center.scroll, event.StepY(36)
	center.scroll = min(max(center.scroll+step, 0), center.geometry.MaxOffsetY)
	if before != center.scroll {
		center.details.dismiss()
	}
	return gfx.EventResult{Handled: true, Repaint: before != center.scroll}, true
}

func (state *State) transferCenterPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || (event.Target != hitTransfersBar && !center.dragging) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary {
		center.dragging = true
		center.details.dismiss()
		center.dragOffset = center.geometry.VerticalThumb.Height / 2
		if pointInRect(event.X, event.Y, center.geometry.VerticalThumb) {
			center.dragOffset = event.Y - center.geometry.VerticalThumb.Y
		}
	}
	if center.dragging {
		center.scroll = center.geometry.VerticalOffsetForThumb(event.Y - center.dragOffset)
	}
	if event.Kind == gfx.PointerUp {
		center.dragging = false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) transferCenterKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		if !center.selectOpen.IsZero() {
			center.selectOpen = gfx.ID{}
		} else if len(center.batch.targets) > 0 {
			center.batch = transferBatch{}
		} else if center.details.showing != 0 || center.details.pinned != 0 {
			center.details.dismiss()
		} else {
			center.close()
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if !center.selectOpen.IsZero() {
		if result, handled := state.menuKey(event); handled {
			return result, true
		}
	}
	if (event.Target == hitTransfersHost || event.Target == hitTransfersDirection) && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		center.selectOpen = event.Target
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if len(center.batch.targets) > 0 {
		if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && (event.Target == hitTransfersBulkConfirm || event.Target == hitTransfersBulkCancel) {
			state.clickTransferCenter(event.Target)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	if event.Target == hitTransfersList {
		area := components.BeginScrollArea(nil, state.Theme(), center.list, gfx.Size{Height: float32(len(center.rows)) * transferRowHeight})
		if _, offset, ok := area.ScrollForKey(0, center.scroll, event.Key, 36); ok {
			center.scroll = offset
			center.details.dismiss()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && event.Target != hitTransfersList {
		if state.clickTransferCenter(event.Target) {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}
