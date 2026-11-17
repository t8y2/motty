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
