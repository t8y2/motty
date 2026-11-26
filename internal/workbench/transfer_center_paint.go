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

// declareTransferBatchDialog declares the alert dialog asked before a bulk
// overwrite or resume, over whatever surface raised it, so the transfers it
// concerns stay visible behind it. Cancel has the initial focus because the
// action replaces files that exist.
func (state *State) declareTransferBatchDialog(c ui.C, batch *transferBatch, cancel, confirm gfx.ID) {
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: batch.title, Description: batch.description, Width: 512,
		Layer: dialogLayer + 1, Scope: dialogScope, CloseID: cancel,
		InitialFocusID: cancel, NoClose: true,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: cancel, Label: state.messages.Cancel, Variant: components.ButtonOutline},
			components.ButtonAction{ID: confirm, Label: batch.action, Variant: components.ButtonDestructive},
		)
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
	if view := state.transferConflictView(); view != nil {
		if result, handled := state.transferConflictKey(view, event); handled {
			return result, true
		}
		return gfx.EventResult{}, false
	}
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || event.Release {
		return gfx.EventResult{}, false
	}
	if event.Key == gfx.KeyEscape {
		if len(center.batch.targets) > 0 {
			center.batch = transferBatch{}
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		// A pinned preview is the topmost transient surface: it closes before
		// the center, as the page's choice lists do before both.
		if center.details.pinned != 0 {
			center.details.dismiss()
			return gfx.EventResult{Handled: true, Repaint: true}, true
		}
		// The page's popovers own the rest: the open choice list first, then
		// the center itself.
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
