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
			ui.Button(c, state.messages.Cancel).Secondary().Hit(hitTransfersBulkCancel, state.messages.Cancel)
			ui.Button(c, center.batch.action).Destructive().Hit(hitTransfersBulkConfirm, center.batch.action)
		})
	})
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

func (state *State) transferCenterKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		if len(center.batch.targets) > 0 {
			center.batch = transferBatch{}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		// The page's popovers own Escape here: the open choice list first, then
		// the details card, then the center itself.
		return gfx.EventResult{}, false
	}
	if len(center.batch.targets) > 0 {
		if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && (event.Target == hitTransfersBulkConfirm || event.Target == hitTransfersBulkCancel) {
			state.clickTransferCenter(event.Target)
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		return gfx.EventResult{}, false
	}
	if event.Target == hitTransfersList && state.scrollTransferList(event.Key) {
		center.details.dismiss()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if (event.Key == gfx.KeyEnter || event.Key == gfx.KeySpace) && !event.Repeat && event.Target != hitTransfersList {
		if state.clickTransferCenter(event.Target) {
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return gfx.EventResult{}, false
}

// scrollTransferList moves the List to where the key asks: the keys the list
// itself takes once a row has the focus, for the list's own stop at the top of
// the tab order.
func (state *State) scrollTransferList(key gfx.Key) bool {
	center := &state.transfers
	if !center.listBox.Valid() {
		return false
	}
	scroll := center.listBox.ScrollState()
	step := float32(36)
	page := max(scroll.Viewport-step, step)
	target := scroll.Offset
	switch key {
	case gfx.KeyUp:
		target -= step
	case gfx.KeyDown:
		target += step
	case gfx.KeyPageUp:
		target -= page
	case gfx.KeyPageDown:
		target += page
	case gfx.KeyHome:
		target = 0
	case gfx.KeyEnd:
		target = scroll.Max()
	default:
		return false
	}
	target = min(max(target, 0), scroll.Max())
	if target != scroll.Offset {
		scroll.ScrollTo(target)
		center.scroll, center.scrollSynced = target, target
	}
	return true
}
