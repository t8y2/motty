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

// declareTransferCenter declares the transfer popover in the workbench page: the
// page's Popover gives it the same 640x680 panel and header the hand-painted
// popover had, and its body is a List of the cards.
func (state *State) declareTransferCenter(c ui.C) {
	center := &state.transfers
	if !center.open || !state.transferCenterAvailable() {
		return
	}
	viewport := c.Bounds()
	anchorRect := center.anchor
	if anchorRect.Width <= 0 {
		anchorRect = gfx.Rect{X: max(viewport.Width-16, 0), Y: windowBarHeight, Width: 1, Height: 1}
	}
	theme := state.Theme()
	m := state.messages.Transfers
	width := min(640, viewport.Width-24)
	total := min(680, viewport.Height-windowBarHeight-16)
	pad, gap := theme.Metrics.PadSurface, theme.Metrics.GapItem
	center.headerLines = c.Canvas().WrapLines(center.headerLines[:0], m.Description, theme.Metrics.FontSizeM, gfx.WeightRegular, width-2*pad, 1<<20)
	center.headerHeight = theme.Metrics.FontSizeM + 7
	if m.Description != "" {
		center.headerHeight += 4 + float32(len(center.headerLines))*(theme.Metrics.FontSizeM+7)
	}
	// The page Popover gives its header a bottom margin of PadSurface-GapItem,
	// so the header-to-body space is the old PadSurface. The body keeps the old
	// rectangle; only the panel's own padding and the header take height.
	center.headerMargin = max(pad-gap, 0)
	center.bodyHeight = max(total-2*pad-center.headerHeight-gap-center.headerMargin, 0)
	initialFocus := hitTransfersList
	if len(center.batch.targets) > 0 {
		initialFocus = hitTransfersBulkCancel
	}
	anchor := ui.Slot(c).Absolute().Left(anchorRect.X).Top(anchorRect.Y).Width(max(anchorRect.Width, 1)).Height(max(anchorRect.Height, 1))
	spec := ui.PopoverSpec{
		Scope: hitTransfersScope, DismissID: hitTransfersDismiss, InitialFocusID: initialFocus,
		Side: components.PopoverBottom, Align: ui.End, Offset: 8, Width: width,
		Title: m.Title, Description: m.Description, TitleWeight: gfx.WeightBold,
	}
	dismissed := ui.PopoverWith(c, anchor, spec, &center.open, func() {
		state.declareTransferPopoverBody(c, width, center.bodyHeight)
	})
	if dismissed {
		center.dragging, center.hostOpen, center.batch = false, false, transferBatch{}
	}
}

// declareTransferPopoverBody declares the popover's body: the cards or the bulk
// confirmation, the summary and filters over them, and the close control in the
// header.
func (state *State) declareTransferPopoverBody(c ui.C, width, height float32) {
	center := &state.transfers
	m, pal, theme := state.messages.Transfers, state.pal, *c.Theme()
	innerWidth := width - 2*theme.Metrics.PadSurface
	center.listWidth = innerWidth
	body := ui.Column(c).FillWidth().Height(height)
	body.Children(func() {
		// The close control sits in the header's top right, outside the body.
		state.declareIconButton(c, hitTransfersClose, iconClose, m.Close, false, iconButtonQuiet).
			Absolute().Right(0).Top(-center.headerHeight-theme.Metrics.GapItem-center.headerMargin-2).Size(24, 24)
		if len(center.batch.targets) > 0 {
			state.declareTransferBatchBody(c)
			return
		}
		column := ui.Column(c).Fill().Gap(10)
		column.Children(func() {
			ui.Column(c).Height(48).Shrink(0).PadX(12).Center().Radius(radius.control).Background(pal.accentSoft).Children(func() {
				ui.Text(c, center.summary).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).LineHeight(22)
				center.body.summaryBar = ui.Slot(c).Height(3).Width(max(0, innerWidth-24))
			})
			ui.Row(c).Height(28).Shrink(0).Gap(4).Items(ui.Center).Children(func() {
				for index, label := range [4]string{m.All, m.Active, m.Ended, m.Failed} {
					center.body.filter[index] = pageButtonSlot(c, label, true)
				}
			})
			ui.Row(c).Height(28).Shrink(0).Gap(4).Items(ui.Center).Children(func() {
				center.body.hostBox = ui.Slot(c).Grow(1).MinWidth(100).Height(28)
				center.body.cancelAll = pageButtonSlot(c, center.cancelLabel, true)
				center.body.clear = pageButtonSlot(c, center.clearLabel, true)
			})
			ui.Text(c, center.scopeLabel).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Height(16).Shrink(0).
				HitWith(hitTransfersScopeLabel, center.scopeLabel, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: center.scopeLabel})
			ui.Row(c).Height(28).Shrink(0).Gap(4).Items(ui.Center).Children(func() {
				center.body.retryFailed = pageButtonSlot(c, center.retryLabel, true)
				ui.Spacer(c)
				ui.Text(c, center.conflictLabel).FontSize(metrics.FontSizeS).Color(pal.muted)
				center.body.skip = pageButtonSlot(c, m.Skip, true)
				center.body.replace = pageButtonSlot(c, m.Replace, true)
				center.body.resume = pageButtonSlot(c, m.Resume, true)
			})
			// The List is the body's flexible child: a scroll area is what gives way
			// in a column, so it takes the room the rows above leave and scrolls the
			// rest instead of painting its whole height past the panel.
			state.prepareTransferCards()
			center.listBox = ui.List(c, &center.listState, center.count, center.rowDecl).Grow(1).Label(m.Title).
				HitWith(hitTransfersList, m.Title, gfx.HitOptions{Role: gfx.RoleGroup})
			center.body.list = center.listBox
			if scroll := center.listBox.ScrollState(); scroll != nil && center.scroll != center.scrollSynced {
				scroll.ScrollTo(center.scroll)
				center.scrollSynced = center.scroll
				// The List places its rows from the offset it is declared at, so the
				// paint that moved the offset still shows the old one: ask for the
				// paint that follows, as a List asking for a scroll it applied does.
				c.Canvas().RequestFrame()
			}
		})
	})
	body.DrawOver(center.bodyDraw)
}

// prepareTransferCards fills the visible entries in the order the popover shows
// them and the scratch each card's row uses.
func (state *State) prepareTransferCards() {
	center := &state.transfers
	center.visible = center.visible[:0]
	for pass := 0; pass < 2; pass++ {
		for index := len(center.entries) - 1; index >= 0; index-- {
			entry := &center.entries[index]
			if !center.shows(entry) || (pass == 0) != transferActive(entry.snapshot) {
				continue
			}
			center.visible = append(center.visible, entry)
		}
	}
	center.count = len(center.visible)
	if cap(center.cards) < len(center.visible) {
		center.cards = make([]transferCard, len(center.visible))
	} else {
		center.cards = center.cards[:len(center.visible)]
		for index := range center.cards {
			center.cards[index] = transferCard{}
		}
	}
	center.handlers(state)
}

// declareTransferCardRow declares one card as a List row. Only the rows the List
// shows are declared.
func (state *State) declareTransferCardRow(row ui.ListRow) {
	center := &state.transfers
	if row.Index < 0 || row.Index >= len(center.visible) {
		return
	}
	entry := center.visible[row.Index]
	card := &center.cards[row.Index]
	*card = transferCard{entry: entry}
	row.Element.Width(center.listWidth-12).Height(transferCardHeight-8).MarginSides(0, 0, 8, 0)
	// A List row is a Row; the card itself is a Column inside it.
	column := ui.Column(row.C).Fill()
	row.Element.Children(func() { state.declareTransferCard(row.C, column, entry, card) })
}

// declareTransferCard declares a card's contents. Its buttons and progress bar
// are Slots the body paints after the page.
func (state *State) declareTransferCard(c ui.C, element ui.Box, entry *transferEntry, card *transferCard) {
	pal, m := state.pal, state.messages.Transfers
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
	card.statusColor = statusColor
	element.Pad(10).Gap(3).Radius(radius.control).Border(1, pal.line).Background(pal.surface).Children(func() {
		ui.Row(c).Height(22).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
			ui.Text(c, direction).FontSize(metrics.FontSizeS).Color(pal.muted)
			ui.Text(c, entry.title).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().Grow(1)
			ui.Text(c, connector).FontSize(metrics.FontSizeS).Color(pal.muted)
			ui.Text(c, entry.server).FontSize(metrics.FontSizeS).Color(pal.text).Ellipsis().MaxWidth(96)
			ui.Text(c, entry.address).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().MaxWidth(180)
			ui.Text(c, entry.status).FontSize(metrics.FontSizeS).Color(statusColor).HitWith(hitTransfersStatus.At(entry.id), entry.status, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: snapshot.Error})
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
	ui.Column(c).Fill().Gap(16).Children(func() {
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

// paintTransferCenterBody paints what the popover body's page only lays out.
func (state *State) paintTransferCenterBody(d *ui.DrawContext) {
	center, m, pal, theme := &state.transfers, state.messages.Transfers, state.pal, *d.Theme
	canvas := d.Canvas
	pad, gap := theme.Metrics.PadSurface, theme.Metrics.GapItem
	center.surface = gfx.Rect{
		X: d.Rect.X - pad, Y: d.Rect.Y - center.headerHeight - gap - center.headerMargin - pad,
		Width: d.Rect.Width + 2*pad, Height: d.Rect.Height + center.headerHeight + gap + center.headerMargin + 2*pad,
	}
	if len(center.batch.targets) > 0 {
		state.paintTransferBatchControls(canvas, theme)
		return
	}
	center.list = center.body.list.Rect()
	if scroll := center.listBox.ScrollState(); scroll != nil {
		center.scroll = scroll.Offset
		center.scrollSynced = scroll.Offset
	}
	paintTransferProgress(canvas, center.body.summaryBar.Rect(), center.fraction, pal.accent, pal.line)
	state.paintTransferBodyControls(canvas, theme)
	state.paintTransferCards(canvas, theme)
	if center.count == 0 {
		state.paintTransferEmpty(canvas, center.list, theme)
	}
	area := components.BeginScrollArea(canvas, theme, center.list, gfx.Size{Height: float32(center.count) * transferCardHeight}).
		Offset(0, center.scroll).VerticalScrollbar(hitTransfersBar, m.Title, "")
	center.geometry = area.EndContent()
	center.hostBounds = center.body.hostBox.Rect()
	state.paintTransferHosts(canvas)
}

// paintTransferBodyControls paints the body's summary progress and buttons into
// the slots its page left.
func (state *State) paintTransferBodyControls(canvas *gfx.Canvas, theme gfx.Theme) {
	center, m := &state.transfers, state.messages.Transfers
	body := &center.body
	for index, label := range [4]string{m.All, m.Active, m.Ended, m.Failed} {
		button := components.BeginButton(canvas, theme, hitTransfersFilter.At(index), body.filter[index].Rect()).Small()
		if center.filter == index {
			button.Secondary(label)
		} else {
			button.Ghost(label)
		}
	}
	cancelAll := components.BeginButton(canvas, theme, hitTransfersCancelAll, body.cancelAll.Rect()).Small()
	if center.visibleActive == 0 {
		cancelAll = cancelAll.Disabled()
	}
	cancelAll.Ghost(center.cancelLabel)
	clear := components.BeginButton(canvas, theme, hitTransfersClear, body.clear.Rect()).Small()
	if center.visibleEnded == 0 {
		clear = clear.Disabled()
	}
	clear.Ghost(center.clearLabel)
	retry := components.BeginButton(canvas, theme, hitTransfersRetryFailed, body.retryFailed.Rect()).Small()
	if center.retryCount == 0 {
		retry = retry.Disabled()
	}
	retry.Ghost(center.retryLabel)
	disabled := center.conflictCount == 0
	skip := components.BeginButton(canvas, theme, hitTransfersSkipVisible, body.skip.Rect()).Small()
	if disabled {
		skip = skip.Disabled()
	}
	skip.Ghost(m.Skip)
	replace := components.BeginButton(canvas, theme, hitTransfersReplaceVisible, body.replace.Rect()).Small()
	if disabled {
		replace = replace.Disabled()
	}
	replace.Outline(m.Replace)
	resume := components.BeginButton(canvas, theme, hitTransfersResumeVisible, body.resume.Rect()).Small()
	if disabled {
		resume = resume.Disabled()
	}
	resume.Ghost(m.Resume)
}

// paintTransferBatchControls paints the confirmation's buttons into the slots
// its page left.
func (state *State) paintTransferBatchControls(canvas *gfx.Canvas, theme gfx.Theme) {
	center := &state.transfers
	components.BeginButton(canvas, theme, hitTransfersBulkCancel, center.batchBody.cancel.Rect()).Secondary(state.messages.Cancel)
	components.BeginButton(canvas, theme, hitTransfersBulkConfirm, center.batchBody.confirm.Rect()).Destructive(center.batch.action)
}

// paintTransferCards paints the progress bar and buttons of every card row the
// List declared.
func (state *State) paintTransferCards(canvas *gfx.Canvas, theme gfx.Theme) {
	state.paintTransferCardsFor(canvas, theme, state.transfers.cards)
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
			components.BeginButton(canvas, theme, hitTransfersResume.At(entry.id), card.resume.Rect()).Small().Height(22).Ghost(m.Resume)
			components.BeginButton(canvas, theme, hitTransfersSkip.At(entry.id), card.skip.Rect()).Small().Height(22).Ghost(m.Skip)
		}
	}
}

// paintTransferEmpty paints the message shown when no transfer matches, in the
// room the List leaves.
func (state *State) paintTransferEmpty(canvas *gfx.Canvas, list gfx.Rect, theme gfx.Theme) {
	center, m, pal := &state.transfers, state.messages.Transfers, state.pal
	empty := m.EmptyFiltered
	if len(center.entries) == 0 {
		empty = m.Empty
	}
	line := theme.Metrics.FontSizeS + 7
	height := 28 + 12 + line
	top := list.Y + (list.Height-height)/2
	canvas.DrawIcon(lucideIcons[iconTransfer], gfx.Rect{X: list.X + (list.Width-28)/2, Y: top, Width: 28, Height: 28}, pal.muted)
	canvas.LabelCenter(empty, gfx.Rect{X: list.X, Y: top + 28 + 12, Width: list.Width, Height: line}, gfx.TextStyle{Size: theme.Metrics.FontSizeS, Color: pal.muted})
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
