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

func (state *State) paintTransferCenter(canvas *gfx.Canvas) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() {
		return
	}
	m, pal := state.messages.Transfers, state.pal
	viewport := state.windowRect()
	anchor := gfx.Rect{X: max(viewport.Width-16, 0), Y: windowBarHeight, Width: 1, Height: 1}
	if state.toolbarVisible(toolbarWindow, hitTransferCenter) {
		if button := state.toolbarDockBounds(hitTransferCenter); button.Width > 0 {
			anchor = button
		}
	}
	initialFocus := hitTransfersList
	if len(center.batch.targets) > 0 {
		initialFocus = hitTransfersBulkCancel
	}
	popover := components.BeginPopover(canvas, state.Theme(), viewport, anchor, components.PopoverOptions{
		Layer: dialogLayer, Scope: hitTransfersScope, DismissID: hitTransfersDismiss,
		AnchorDismiss: true,
		Side:          components.PopoverBottom, Align: components.PopoverAlignEnd, SideOffset: 8,
		ContentSize:    gfx.Size{Width: min(640, viewport.Width-24), Height: min(680, viewport.Height-windowBarHeight-16)},
		InitialFocusID: initialFocus,
		TitleWeight:    gfx.WeightBold,
	})
	layout := popover.Begin(m.Title, m.Description)
	center.surface = layout.Content
	closeBox := gfx.Rect{X: layout.Header.X + layout.Header.Width - 24, Y: layout.Header.Y - 2, Width: 24, Height: 24}
	state.paintIconButton(canvas, hitTransfersClose, iconClose, closeBox, m.Close, false, iconButtonQuiet, pal.surface)
	body := layout.Body
	if len(center.batch.targets) > 0 {
		state.paintTransferBatch(canvas, body)
		popover.End()
		return
	}
	if center.summary == "" {
		state.describeTransferSummary()
	}
	var list, summaryBar, hostBox ui.Box
	f := center.view.Begin(canvas, state.Theme(), body)
	f.Column().Fill().Gap(10).Children(func() {
		f.Column().Height(48).Shrink(0).PadX(12).Center().Radius(radius.control).Background(pal.accentSoft).Children(func() {
			f.Text(center.summary).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(22)
			summaryBar = f.Slot().Height(3).Width(max(0, body.Width-24))
		})
		f.Row().Height(28).Shrink(0).Gap(4).Items(ui.Center).Children(func() {
			labels := [4]string{m.All, m.Active, m.Ended, m.Failed}
			for index, label := range labels {
				button := f.Button(hitTransfersFilter.At(index), label).Small().Ghost()
				if center.filter == index {
					button.Secondary()
				}
			}
		})
		f.Row().Height(28).Shrink(0).Gap(4).Items(ui.Center).Children(func() {
			hostBox = f.Slot().Grow(1).MinWidth(100).Height(28)
			f.Button(hitTransfersCancelAll, center.cancelLabel).Small().Ghost().Disabled(center.visibleActive == 0)
			f.Button(hitTransfersClear, center.clearLabel).Small().Ghost().Disabled(center.visibleEnded == 0)
		})
		f.Text(center.scopeLabel).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(16).Shrink(0).
			HitWith(hitTransfersScopeLabel, center.scopeLabel, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: center.scopeLabel})
		f.Row().Height(28).Shrink(0).Gap(4).Items(ui.Center).Children(func() {
			f.Button(hitTransfersRetryFailed, center.retryLabel).Small().Ghost().Disabled(center.retryCount == 0)
			f.Spacer()
			f.Text(center.conflictLabel).FontSize(metrics.FontSizeS).Color(pal.muted)
			f.Button(hitTransfersSkipVisible, m.Skip).Small().Ghost().Disabled(center.conflictCount == 0)
			f.Button(hitTransfersReplaceVisible, m.Replace).Small().Outline().Disabled(center.conflictCount == 0)
			f.Button(hitTransfersResumeVisible, m.Resume).Small().Ghost().Disabled(center.conflictCount == 0)
		})
		list = f.Slot().Grow(1)
	})
	f.End()
	paintTransferProgress(canvas, summaryBar.Rect(), center.fraction, pal.accent, pal.line)
	center.list = list.Rect()
	area := components.BeginScrollArea(canvas, state.Theme(), center.list, gfx.Size{Height: float32(center.count) * transferCardHeight}).
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
			box := gfx.Rect{X: geometry.Content.X, Y: geometry.Content.Y + float32(row)*transferCardHeight, Width: geometry.Viewport.Width - 12, Height: transferCardHeight - 8}
			row++
			if box.Y+box.Height < center.list.Y || box.Y > center.list.Y+center.list.Height {
				continue
			}
			state.paintTransferCard(canvas, box, entry)
		}
	}
	area.EndContent()
	center.hostBounds = hostBox.Rect()
	state.paintTransferHosts(canvas)
	popover.End()
}

func (state *State) paintTransferHosts(canvas *gfx.Canvas) {
	center, m := &state.transfers, state.messages.Transfers
	bounds := center.hostBounds
	viewport := state.windowRect()
	height := min(float32(8+(len(center.hosts)+1)*32), min(264, viewport.Height-32))
	content := gfx.Rect{X: bounds.X, Y: bounds.Y + bounds.Height + 4, Width: max(bounds.Width, min(400, viewport.Width-32)), Height: height}
	content.X = min(content.X, viewport.Width-content.Width-12)
	if content.Y+height > viewport.Height-12 {
		content.Y = max(12, bounds.Y-height-4)
	}
	opening := center.hostOpen && state.popups.owner != hitTransfersHostDismiss
	scroll := state.selectScroll(center.hostOpen, hitTransfersHostDismiss)
	if opening {
		selected := 0
		for index, host := range center.hosts {
			if sameTransferHost(center.host, host) {
				selected = index + 1
				break
			}
		}
		scroll.Offset = max(0, min(float32(selected)*32, float32((len(center.hosts)+1)*32)+8-height))
		canvas.SetInitialFocus(hitTransfersHostItems.At(selected))
	}
	control := components.BeginSelect(canvas, state.Theme(), bounds, components.SelectOptions{
		TriggerID: hitTransfersHost, Label: m.Hosts, Value: center.hostLabel, Hint: center.hostLabel,
		Open: center.hostOpen, Content: content, Size: components.SelectSizeSmall,
		Layer: dialogLayer + 1, Scope: hitTransfersHostDismiss, DismissID: hitTransfersHostDismiss,
		Viewport: viewport, Scroll: scroll,
	})
	defer control.End()
	if !center.hostOpen {
		return
	}
	state.menuPlaced(control.ContentBounds())
	control.AddItem(components.SelectItem{ID: hitTransfersHostItems.At(0), Label: m.AllHosts, Selected: center.host == nil})
	for index, host := range center.hosts {
		control.AddItem(components.SelectItem{ID: hitTransfersHostItems.At(index + 1), Label: host.label, Hint: host.label, Selected: sameTransferHost(center.host, host)})
	}
}

func (state *State) paintTransferBatch(canvas *gfx.Canvas, bounds gfx.Rect) {
	center, pal := &state.transfers, state.pal
	f := center.view.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().Gap(16).Children(func() {
		f.Wrap(center.batch.title).FontSize(metrics.FontSizeL).Weight(gfx.WeightSemibold).Color(pal.text)
		f.Wrap(center.batch.description).FontSize(metrics.FontSizeS).Color(pal.muted).
			HitWith(hitTransfersBulkDescription, center.batch.title, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: center.batch.description})
		f.Spacer()
		f.Row().Height(32).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			f.Spacer()
			f.Button(hitTransfersBulkCancel, state.messages.Cancel).Secondary()
			f.Button(hitTransfersBulkConfirm, center.batch.action).Destructive()
		})
	})
	f.End()
}

func paintTransferProgress(canvas *gfx.Canvas, box gfx.Rect, fraction float32, fill, background gfx.Color) {
	canvas.Rounded(box, 2, background)
	if fraction > 0 {
		box.Width *= min(fraction, 1)
		canvas.Rounded(box, 2, fill)
	}
}

func (state *State) paintTransferCard(canvas *gfx.Canvas, box gfx.Rect, entry *transferEntry) {
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
	var bar ui.Box
	f := center.row.Begin(canvas, state.Theme(), box)
	f.Column().Fill().Pad(10).Gap(3).Radius(radius.control).Border(1, pal.line).Background(pal.surface).Children(func() {
		f.Row().Height(22).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
			f.Text(direction).FontSize(metrics.FontSizeS).Color(pal.muted)
			f.Text(entry.title).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1)
			f.Text(connector).FontSize(metrics.FontSizeS).Color(pal.muted)
			f.Text(entry.server).FontSize(metrics.FontSizeS).Color(pal.text).Ellipsis().MaxWidth(96)
			f.Text(entry.address).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().MaxWidth(180)
			f.Text(entry.status).FontSize(metrics.FontSizeS).Color(statusColor).HitWith(hitTransfersStatus.At(entry.id), entry.status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: snapshot.Error})
		})
		bar = f.Slot().Height(3).Shrink(0).MarginSides(2, 0, 2, 0)
		f.Text(entry.paths).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(16).Shrink(0).HitWith(hitTransfersPaths.At(entry.id), entry.paths, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: entry.paths})
		f.Row().Height(22).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
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
				if transferCanReveal(snapshot) {
					id := hitTransfersReveal.At(entry.id)
					foreground, labelColor := pal.accent, pal.text
					if state.revealBusy {
						foreground, labelColor = pal.faint, pal.faint
					}
					f.Row().Height(22).Shrink(0).PadX(6).Gap(4).Items(ui.Center).Radius(radius.control).Border(1, gfx.Color{}).
						Hover(ui.Look{}.Background(withAlpha(pal.accent, 0.08))).Focus(ui.Look{}.Border(pal.accent)).
						HitWith(id, m.ShowInFolder, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.ShowInFolder, Disabled: state.revealBusy}).Children(func() {
						f.Icon(lucideIcons[iconFolderOpen]).Size(14, 14).Color(foreground)
						f.Text(m.ShowInFolder).FontSize(metrics.FontSizeS).Color(labelColor)
					})
				}
				id := hitTransfersRemove.At(entry.id)
				f.Row().Height(22).Shrink(0).PadX(6).Gap(4).Items(ui.Center).Radius(radius.control).Border(1, gfx.Color{}).
					Hover(ui.Look{}.Background(withAlpha(pal.danger, 0.08))).Focus(ui.Look{}.Border(pal.accent)).
					HitWith(id, m.Remove, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.Remove}).Children(func() {
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

func (state *State) transferCenterScroll(event gfx.ScrollEvent) (gfx.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || !pointInRect(event.X, event.Y, center.surface) {
		return gfx.EventResult{}, false
	}
	before, step := center.scroll, event.StepY(36)
	center.scroll = min(max(center.scroll+step, 0), center.geometry.MaxOffsetY)
	return gfx.EventResult{Handled: true, Repaint: before != center.scroll}, true
}

func (state *State) transferCenterPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() || (event.Target != hitTransfersBar && !center.dragging) {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary {
		center.dragging = true
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
		if center.hostOpen {
			center.hostOpen = false
		} else if len(center.batch.targets) > 0 {
			center.batch = transferBatch{}
		} else {
			center.open, center.dragging = false, false
		}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if center.hostOpen {
		if result, handled := state.menuKey(event); handled {
			return result, true
		}
	}
	if event.Target == hitTransfersHost && (event.Key == gfx.KeyDown || event.Key == gfx.KeyUp) {
		center.hostOpen = true
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
		area := components.BeginScrollArea(nil, state.Theme(), center.list, gfx.Size{Height: float32(center.count) * transferCardHeight})
		if _, offset, ok := area.ScrollForKey(0, center.scroll, event.Key, 36); ok {
			center.scroll = offset
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
