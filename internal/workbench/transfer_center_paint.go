package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

func (state *State) paintTransferBadge(canvas *ui.Canvas) {
	if !state.toolbarVisible(toolbarWindow, hitTransferCenter) {
		return
	}
	center, box := &state.transfers, state.chrome.bar.transfers.Rect()
	if box.Width <= 0 {
		return
	}
	if center.open {
		canvas.Rounded(ui.Rect{X: box.X, Y: box.Y + box.Height - 2, Width: box.Width, Height: 2}, 1, state.pal.accent)
	}
	if center.active > 0 {
		width := max(float32(14), float32(len(center.badge))*6+6)
		badge := ui.Rect{X: box.X + box.Width - width, Y: box.Y, Width: width, Height: 14}
		canvas.Rounded(badge, 7, state.pal.accent)
		text := badge
		text.X += (badge.Width - canvas.TextWidthWeight(center.badge, 10, ui.WeightSemibold)) / 2
		canvas.Label(center.badge, text, ui.TextStyle{Size: 10, Weight: ui.WeightSemibold, Color: state.pal.onAccent})
	}
}

func (state *State) paintTransferCenter(canvas *ui.Canvas) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() {
		return
	}
	m, pal := state.messages.Transfers, state.pal
	viewport := state.windowRect()
	anchor := ui.Rect{X: max(viewport.Width-16, 0), Y: windowBarHeight, Width: 1, Height: 1}
	if state.toolbarVisible(toolbarWindow, hitTransferCenter) {
		anchor = state.chrome.bar.transfers.Rect()
	}
	popover := components.BeginPopover(canvas, state.Theme(), viewport, anchor, components.PopoverOptions{
		Layer: dialogLayer, Scope: hitTransfersScope, DismissID: hitTransfersDismiss,
		AnchorDismiss: true,
		Side:          components.PopoverBottom, Align: components.PopoverAlignEnd, SideOffset: 8,
		ContentSize:    ui.Size{Width: min(640, viewport.Width-24), Height: min(680, viewport.Height-windowBarHeight-16)},
		InitialFocusID: hitTransfersList,
		TitleWeight:    ui.WeightBold,
	})
	layout := popover.Begin(m.Title, m.Description)
	center.surface = layout.Content
	closeBox := ui.Rect{X: layout.Header.X + layout.Header.Width - 24, Y: layout.Header.Y - 2, Width: 24, Height: 24}
	state.paintIconButton(canvas, hitTransfersClose, iconClose, closeBox, m.Close, false, iconButtonQuiet, pal.surface)
	body := layout.Body
	if center.summary == "" {
		state.describeTransferSummary()
	}
	var list, summaryBar flex.Box
	f := center.view.Begin(canvas, state.Theme(), body)
	f.Column().Fill().Gap(10).Children(func() {
		f.Column().Height(48).Shrink(0).PadX(12).Center().Radius(radius.control).Background(pal.accentSoft).Children(func() {
			f.Text(center.summary).FontSize(metrics.FontSizeS).Weight(ui.WeightMedium).Color(pal.text).LineHeight(22)
			summaryBar = f.Slot().Height(3).Width(max(0, body.Width-24))
		})
		f.Row().Height(28).Shrink(0).Gap(4).Items(flex.Center).Children(func() {
			labels := [3]string{m.All, m.Active, m.Ended}
			for index, label := range labels {
				button := f.Button(hitTransfersFilter.At(index), label).Small().Ghost()
				if center.filter == index {
					button.Secondary()
				}
			}
			f.Spacer()
			f.Button(hitTransfersCancelAll, m.CancelAll).Small().Ghost().Disabled(center.active == 0)
			f.Button(hitTransfersClear, m.Clear).Small().Ghost().Disabled(center.ended == 0)
		})
		list = f.Slot().Grow(1)
	})
	f.End()
	paintTransferProgress(canvas, summaryBar.Rect(), center.fraction, pal.accent, pal.line)
	center.list = list.Rect()
	center.count = 0
	for index := range center.entries {
		if center.shows(&center.entries[index]) {
			center.count++
		}
	}
	area := components.BeginScrollArea(canvas, state.Theme(), center.list, ui.Size{Height: float32(center.count) * transferCardHeight}).
		Offset(0, center.scroll).Viewport(hitTransfersList, m.Title).VerticalScrollbar(hitTransfersBar, m.Title, "")
	geometry := area.BeginContent()
	center.scroll, center.geometry = geometry.OffsetY, geometry
	if center.count == 0 {
		empty := m.EmptyFiltered
		if len(center.entries) == 0 {
			empty = m.Empty
		}
		f := center.row.Begin(canvas, state.Theme(), center.list)
		f.Column().Fill().Center().Gap(12).Children(func() {
			f.Icon(lucideIcons[iconTransfer]).Size(28, 28).Color(pal.muted)
			f.Text(empty).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
		f.End()
	}
	row := 0
	// Active tasks precede finished history, with newest first in each group.
	for pass := 0; pass < 2; pass++ {
		for index := len(center.entries) - 1; index >= 0; index-- {
			entry := &center.entries[index]
			if !center.shows(entry) || (pass == 0) != transferActive(entry.snapshot) {
				continue
			}
			box := ui.Rect{X: geometry.Content.X, Y: geometry.Content.Y + float32(row)*transferCardHeight, Width: geometry.Viewport.Width - 12, Height: transferCardHeight - 8}
			row++
			if box.Y+box.Height < center.list.Y || box.Y > center.list.Y+center.list.Height {
				continue
			}
			state.paintTransferCard(canvas, box, entry)
		}
	}
	area.EndContent()
	popover.End()
}

func paintTransferProgress(canvas *ui.Canvas, box ui.Rect, fraction float32, fill, background ui.Color) {
	canvas.Rounded(box, 2, background)
	if fraction > 0 {
		box.Width *= min(fraction, 1)
		canvas.Rounded(box, 2, fill)
	}
}

func (state *State) paintTransferCard(canvas *ui.Canvas, box ui.Rect, entry *transferEntry) {
	pal, m, center := state.pal, state.messages.Transfers, &state.transfers
	snapshot := entry.snapshot
	direction, connector := m.Download, m.From
	if snapshot.Operation == sftpclient.OperationUpload {
		direction, connector = m.Upload, m.To
	}
	statusColor := pal.muted
	switch snapshot.State {
	case sftpclient.TransferRunning:
		statusColor = pal.accent
	case sftpclient.TransferCompleted:
		statusColor = pal.success
	case sftpclient.TransferFailed:
		statusColor = pal.danger
	}
	var bar flex.Box
	f := center.row.Begin(canvas, state.Theme(), box)
	f.Column().Fill().Pad(10).Gap(3).Radius(radius.control).Border(1, pal.line).Background(pal.surface).Children(func() {
		f.Row().Height(22).Shrink(0).Gap(6).Items(flex.Center).Children(func() {
			f.Text(direction).FontSize(metrics.FontSizeS).Color(pal.muted)
			f.Text(entry.title).FontSize(metrics.FontSizeS).Weight(ui.WeightSemibold).Color(pal.text).Ellipsis().Grow(1)
			f.Text(connector).FontSize(metrics.FontSizeS).Color(pal.muted)
			f.Text(entry.server).FontSize(metrics.FontSizeS).Color(pal.text).Ellipsis().MaxWidth(96)
			f.Text(entry.address).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().MaxWidth(180)
			f.Text(entry.status).FontSize(metrics.FontSizeS).Color(statusColor).HitWith(hitTransfersStatus.At(entry.id), entry.status, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Hint: snapshot.Error})
		})
		bar = f.Slot().Height(3).Shrink(0).MarginSides(2, 0, 2, 0)
		f.Text(entry.paths).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(16).Shrink(0).HitWith(hitTransfersPaths.At(entry.id), entry.paths, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Hint: entry.paths})
		f.Row().Height(22).Shrink(0).Gap(6).Items(flex.Center).Children(func() {
			f.Text(entry.amount).FontSize(metrics.FontSizeS).Color(pal.text)
			f.Text(entry.clock).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1)
			if transferActive(snapshot) && entry.owner != nil {
				f.Text(entry.speed).FontSize(metrics.FontSizeS).Color(pal.accent)
				pause := state.messages.Tools.TransferPause
				if snapshot.State == sftpclient.TransferPaused {
					pause = state.messages.Tools.TransferResume
				}
				f.Button(hitTransfersPause.At(entry.id), pause).Small().Ghost().Height(22)
				f.Button(hitTransfersCancel.At(entry.id), state.messages.SFTPCancelTransfer).Small().Ghost().Height(22)
			} else {
				if snapshot.Retryable && entry.owner != nil {
					if entry.conflict {
						f.Button(hitTransfersReplace.At(entry.id), m.Replace).Small().Outline().Height(22)
						f.Button(hitTransfersResume.At(entry.id), m.Resume).Small().Ghost().Height(22)
						f.Button(hitTransfersSkip.At(entry.id), m.Skip).Small().Ghost().Height(22)
					} else {
						f.Button(hitTransfersRetry.At(entry.id), m.Retry).Small().Ghost().Height(22)
					}
				}
				id := hitTransfersRemove.At(entry.id)
				button := f.Row().Height(22).Shrink(0).PadX(6).Gap(4).Items(flex.Center).Radius(radius.control).
					HoverBackground(withAlpha(pal.danger, 0.08)).HitWith(id, m.Remove, ui.HitOptions{Role: ui.RoleButton, Hint: m.Remove})
				if canvas.FocusVisible() == id {
					button.Border(1, pal.accent)
				}
				button.Children(func() {
					f.Icon(lucideIcons[iconTrash]).Size(14, 14).Color(pal.danger)
					f.Text(m.Remove).FontSize(metrics.FontSizeS).Color(pal.danger)
				})
			}
		})
	})
	f.End()
	paintTransferProgress(canvas, bar.Rect(), transferFraction(snapshot), statusColor, pal.line)
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

func (state *State) transferCenterScroll(event ui.ScrollEvent) (ui.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || !pointInRect(event.X, event.Y, center.surface) {
		return ui.EventResult{}, false
	}
	before, step := center.scroll, event.DeltaY
	if !event.Precise && step != 0 && step > -36 && step < 36 {
		if step < 0 {
			step = -36
		} else {
			step = 36
		}
	}
	center.scroll = min(max(center.scroll+step, 0), center.geometry.MaxOffsetY)
	return ui.EventResult{Handled: true, Repaint: before != center.scroll}, true
}

func (state *State) transferCenterPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || (event.Target != hitTransfersBar && !center.dragging) {
		return ui.EventResult{}, false
	}
	if event.Kind == ui.PointerDown && event.Button == ui.PointerButtonPrimary {
		center.dragging = true
		center.dragOffset = center.geometry.VerticalThumb.Height / 2
		if pointInRect(event.X, event.Y, center.geometry.VerticalThumb) {
			center.dragOffset = event.Y - center.geometry.VerticalThumb.Y
		}
	}
	if center.dragging {
		center.scroll = center.geometry.VerticalOffsetForThumb(event.Y - center.dragOffset)
	}
	if event.Kind == ui.PointerUp {
		center.dragging = false
	}
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) transferCenterKey(event ui.KeyEvent) (ui.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || event.Release {
		return ui.EventResult{}, false
	}
	if event.Key == ui.KeyEscape {
		center.open, center.dragging = false, false
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Target == hitTransfersList {
		area := components.BeginScrollArea(nil, state.Theme(), center.list, ui.Size{Height: float32(center.count) * transferCardHeight})
		if _, offset, ok := area.ScrollForKey(0, center.scroll, event.Key, 36); ok {
			center.scroll = offset
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	if (event.Key == ui.KeyEnter || event.Key == ui.KeySpace) && !event.Repeat && event.Target != hitTransfersList {
		if state.clickTransferCenter(event.Target) {
			return ui.EventResult{Handled: true, Repaint: true}, true
		}
	}
	return ui.EventResult{}, false
}
