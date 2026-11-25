package workbench

import (
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const (
	transferRowHeight   = float32(38)
	transferCenterGap   = float32(8)
	transferCenterInset = float32(12)
)

func (state *State) declareTransferCenter(c ui.C) {
	center := &state.transfers
	center.details.beginPaint()
	if !center.open || !state.transferCenterAvailable() {
		return
	}
	center.handlers(state)
	viewport := c.Bounds()
	anchor := center.anchor
	width, preferredHeight := max(0, min(1180, viewport.Width-24)), float32(760)
	roomArea, margin := viewport, transferCenterGap
	if center.compact {
		area := state.transferCenterArea()
		anchor = gfx.Rect{
			X: area.X + area.Width - transferCenterInset - 1, Y: area.Y + transferCenterInset,
			Width: 1, Height: 1,
		}
		width, preferredHeight = max(0, min(360, area.Width-2*transferCenterInset)), 148
		if center.active == 0 {
			preferredHeight = 108
		}
		roomArea, margin = area, transferCenterInset
	} else if anchor.Width <= 0 {
		anchor = gfx.Rect{X: max(viewport.Width-16, 0), Y: windowBarHeight, Width: 1, Height: 1}
	}
	padding := c.Theme().Metrics.PadSurface
	// Only the compact notice hangs from the terminal content. The full center
	// keeps its toolbar anchor and original viewport sizing.
	room := roomArea.Y + roomArea.Height - margin - anchor.Y - anchor.Height - transferCenterGap
	side := components.PopoverBottom
	if above := anchor.Y - transferCenterGap - (roomArea.Y + margin); room < preferredHeight+2*padding && above > room {
		room, side = above, components.PopoverTop
	}
	height := max(0, min(preferredHeight, room-2*padding))
	center.listWidth = max(0, width-2*padding)
	origin := ui.Slot(c).Absolute().Left(anchor.X).Top(anchor.Y).Width(max(anchor.Width, 1)).Height(max(anchor.Height, 1))
	spec := ui.PopoverSpec{
		Scope: hitTransfersScope, DismissID: hitTransfersDismiss,
		Side: side, Align: ui.End, Offset: transferCenterGap, Width: width,
	}
	body := func() { state.declareTransferBody(c, height) }
	if center.compact {
		body = func() { state.declareTransferCompact(c, height) }
	} else {
		spec.InitialFocusID = hitTransfersList
	}
	if ui.Popover(c, origin, &center.open, spec, body) {
		center.close()
		return
	}
	if len(center.batch.targets) > 0 {
		state.declareTransferBatchDialog(c, &center.batch, hitTransfersBulkCancel, hitTransfersBulkConfirm)
	}
}

// transferCenterArea is the content rectangle the progress notice floats in.
// On the terminal page it excludes the pane header, side panels and assistant;
// file-manager tabs and section pages use their own visible content instead.
func (state *State) transferCenterArea() gfx.Rect {
	switch {
	case state.terminalPage():
		return state.workspacePixelBounds(state.viewport)
	case state.activeSFTPWindow() != nil:
		return state.fileManagerPixelBounds(state.viewport)
	default:
		if area := state.chrome.workspaceSlot.Rect(); area.Width > 0 && area.Height > 0 {
			return area
		}
	}
	area := state.windowRect()
	area.Y += windowBarHeight
	area.Height = max(0, area.Height-windowBarHeight)
	return area
}

// declareTransferCompact keeps a queued transfer visible without covering the
// workspace with the full history. The body is one button; closing and expanding
// stay separate so the card remains usable with the keyboard.
func (state *State) declareTransferCompact(c ui.C, height float32) {
	center, m, pal := &state.transfers, state.messages.Transfers, state.pal
	overview := center.overview
	status, detail, ink := center.summary, overview.amount, pal.text
	if center.active == 0 {
		status, detail = m.Idle, ""
		outcomes := []struct {
			label string
			count int
		}{
			{m.Failed, center.failed},
			{m.Interrupted, center.interrupted},
			{m.Canceled, center.canceled},
			{m.Completed, center.completed},
		}
		parts := make([]string, 0, len(outcomes))
		for _, outcome := range outcomes {
			if outcome.count > 0 {
				parts = append(parts, outcome.label+" · "+strconv.Itoa(outcome.count))
			}
		}
		known := center.failed + center.interrupted + center.canceled + center.completed
		if known < center.ended {
			parts = append(parts, m.Ended+" · "+strconv.Itoa(center.ended-known))
		}
		if len(parts) > 0 {
			status = strings.Join(parts, " · ")
		}
		switch {
		case center.failed > 0:
			ink = pal.danger
		case center.interrupted > 0:
			ink = pal.warning
		case center.completed > 0:
			ink = pal.success
		case center.canceled > 0:
			ink = pal.muted
		}
	} else if detail == "" {
		detail = overview.title
	}
	padding := c.Theme().Metrics.PadSurface
	center.list = gfx.Rect{}
	ui.Column(c).FillWidth().Height(height).Gap(8).Children(func() {
		ui.Row(c).Height(28).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			ui.Icon(c, lucideIcons[iconTransfer]).Size(16, 16).Color(state.transferBlue()).Shrink(0)
			ui.Text(c, m.Title).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Shrink(0)
			ui.Spacer(c)
			state.declareIconButton(c, hitTransfersClose, iconClose, m.Close, false, iconButtonQuiet).Size(26, 26)
		})
		ui.Column(c).FillWidth().Grow(1).MinHeight(0).Pad(10).Gap(6).Radius(radius.surface).
			Border(1, pal.line).Background(withAlpha(pal.surfaceHot, 0.28)).
			Hover(ui.Look{}.Background(withAlpha(pal.surfaceHot, 0.5).Over(pal.surface))).
			Focus(ui.Look{}.Ring(1.5, pal.accent)).
			HitWith(hitTransfersExpand, m.Details, gfx.HitOptions{Role: gfx.RoleButton, Hint: m.Details}).Children(func() {
			ui.Row(c).Height(22).Shrink(0).FillWidth().Gap(6).Items(ui.Center).Children(func() {
				ui.Text(c, status).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(ink).Grow(1).MinWidth(0).Ellipsis()
				if overview.amount != "" && center.active > 0 {
					ui.Text(c, overview.percent).FontSize(metrics.FontSizeM).Weight(gfx.WeightBold).Color(ink).Shrink(0)
				}
			})
			if detail != "" {
				ui.Text(c, detail).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().FillWidth()
			}
			if overview.amount != "" && center.active > 0 {
				state.declareTransferProgress(c, overview.fraction, 6, 40)
			}
			ui.Spacer(c)
			ui.Row(c).Height(18).Shrink(0).FillWidth().Gap(4).Items(ui.Center).Children(func() {
				ui.Text(c, m.Details).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Grow(1)
				ui.Icon(c, lucideIcons[iconChevronRight]).Size(14, 14).Color(pal.muted).Shrink(0)
			})
		})
	}).DrawOver(func(d *ui.DrawContext) {
		center.surface = gfx.Rect{X: d.Rect.X - padding, Y: d.Rect.Y - padding, Width: d.Rect.Width + 2*padding, Height: d.Rect.Height + 2*padding}
	})
}

func (state *State) declareTransferBody(c ui.C, height float32) {
	center, m, pal := &state.transfers, state.messages.Transfers, state.pal
	width := center.listWidth
	ui.Column(c).FillWidth().Height(height).Gap(10).Children(func() {
		ui.Row(c).Height(30).Shrink(0).Gap(10).Items(ui.Center).Children(func() {
			ui.Icon(c, lucideIcons[iconTransfer]).Size(18, 18).Color(state.transferBlue()).Shrink(0)
			ui.Text(c, m.Title).FontSize(metrics.FontSizeM + 4).Weight(gfx.WeightBold).Color(pal.text).Shrink(0)
			if width >= 530 {
				ui.Text(c, m.Description).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().Grow(1)
			} else {
				ui.Spacer(c)
			}
			state.declareIconButton(c, hitTransfersClose, iconClose, m.Close, false, iconButtonQuiet).Size(28, 28)
		})
		state.declareTransferOverview(c, width)
		ui.Column(c).FillWidth().Shrink(0).Gap(4).Children(func() {
			ui.Row(c).Height(29).FillWidth().Gap(4).Items(ui.Center).Children(func() {
				labels := state.transferFilterLabels()
				state.declareTransferTab(c, transferFilterAll, labels[transferFilterAll])
				for _, group := range transferGroups {
					state.declareTransferTab(c, group, labels[group])
				}
			})
			ui.Divider(c).Height(1).FillWidth()
		})
		state.declareTransferFilters(c, width)
		state.declareTransferRecovery(c)

		center.listBox = ui.List(c, &center.listState, len(center.rows), center.rowDecl).Grow(1).MinHeight(0).FillWidth().
			Label(m.Title).AlwaysScrollbars().ScrollbarSize(10).HitWith(hitTransfersList, m.Title, gfx.HitOptions{Role: gfx.RoleGroup})
		center.body.list = center.listBox
		if scroll := center.listBox.ScrollState(); scroll != nil && center.scroll != center.scrollSynced {
			scroll.ScrollTo(center.scroll)
			center.scrollSynced = center.scroll
			// The List declared its visible rows at its previous offset.
			c.Canvas().RequestFrame()
		}
		state.declareTransferCenterPreview(c)
	}).DrawOver(center.bodyDraw)
}

func (state *State) declareTransferOverview(c ui.C, width float32) {
	center, pal := &state.transfers, state.pal
	overview := center.overview
	ui.Row(c).Height(58).Shrink(0).FillWidth().PadX(12).Gap(12).Items(ui.Center).
		Radius(radius.surface).Border(1, pal.line).Background(withAlpha(pal.surfaceHot, 0.28)).Children(func() {
		label := ui.Column(c).MinWidth(0).Gap(1)
		if overview.amount != "" {
			label.Width(min(width*0.36, 164))
		} else {
			label.Grow(1)
		}
		label.Children(func() {
			ui.Text(c, overview.title).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis().FillWidth()
			if overview.amount != "" {
				ui.Text(c, overview.amount).FontSize(metrics.FontSizeS).Color(pal.muted).Ellipsis().FillWidth()
			}
		})
		if overview.amount != "" {
			state.declareTransferProgress(c, overview.fraction, 8, 22)
			ui.Text(c, overview.percent).FontSize(metrics.FontSizeM).Color(pal.text).Weight(gfx.WeightBold).Width(36).TextEnd().Shrink(0)
		}
		if width >= 725 && overview.rate != "" {
			ui.Slot(c).Size(1, 26).Background(pal.line).Shrink(0)
			ui.Text(c, overview.rate).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text).
				Width(98).Shrink(0).Ellipsis()
		}
		pause := uint8(iconPause)
		if center.pausable == 0 && center.paused > 0 {
			pause = iconPlay
		}
		if width < 510 {
			state.declareIconButton(c, hitTransfersPauseAll, pause, center.pauseLabel, center.visibleActive == 0, iconButtonQuiet).Size(28, 28)
			state.declareIconButton(c, hitTransfersCancelAll, iconStop, center.cancelLabel, center.visibleActive == 0, iconButtonDanger).Size(28, 28)
		} else {
			state.declareTransferAction(c, hitTransfersPauseAll, pause, center.pauseLabel, center.visibleActive == 0, false, true)
			state.declareTransferAction(c, hitTransfersCancelAll, iconStop, center.cancelLabel, center.visibleActive == 0, true, true)
		}
	})
}

func (state *State) declareTransferTab(c ui.C, index int, label string) {
	center, pal := &state.transfers, state.pal
	selected := center.filter == index
	ink, background := pal.muted, gfx.Color{}
	if selected {
		ink, background = gfx.RGB(255, 255, 255), gfx.RGB(26, 89, 178)
	}
	ui.Row(c).MinWidth(0).MaxWidth(126).Grow(1).Height(28).PadX(8).Gap(6).Center().
		Radius(radius.control).Background(background).
		Hover(ui.Look{}.Background(withAlpha(pal.surfaceHot, 0.22).Over(background))).
		Focus(ui.Look{}.Ring(1.5, pal.accent)).
		HitWith(hitTransfersFilter.At(index), label, gfx.HitOptions{
			Role: gfx.RoleTab, Selected: selected, Value: center.groups[index].count,
		}).Children(func() {
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(ink).MinWidth(0).Ellipsis()
		ui.Badge(c, center.groups[index].count).Height(17).PadX(5).PadY(0).Items(ui.Center).Shrink(0).Radius(8.5).
			Background(withAlpha(ink, 0.13)).Children(func() {
			ui.Text(c, center.groups[index].count).FontSize(metrics.FontSizeS - 1).Weight(gfx.WeightRegular).Color(ink)
		})
	})
}

// Reuse the palette's selection blue to distinguish work in progress from
// waiting, completion and failure on both themes.
func (state *State) transferBlue() gfx.Color {
	blue := state.pal.ansi[4]
	return gfx.RGB(blue[0], blue[1], blue[2])
}

func (state *State) declareTransferProgress(c ui.C, fraction, height, minWidth float32) {
	ui.Row(c).Height(height).MinWidth(minWidth).Grow(1).Basis(0).Radius(height / 2).Background(state.pal.line).Children(func() {
		if fraction > 0 {
			ui.Slot(c).WidthPercent(100 * fraction).Height(height).Radius(height / 2).Background(state.transferBlue())
		}
	})
}

func (state *State) declareTransferFilters(c ui.C, width float32) {
	center, m := &state.transfers, state.messages.Transfers
	ui.Row(c).Height(30).Shrink(0).FillWidth().Gap(8).Items(ui.Center).
		HitWith(hitTransfersScopeLabel, center.scopeLabel, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, Hint: center.scopeLabel}).
		Children(func() {
			host := ui.Select(c, &center.host, center.hostChoices, func(host *transferHost) string {
				if host == nil {
					return m.AllHosts
				}
				return host.label
			}).Small().Width(min(172, width*0.23)).Height(30).Label(m.Hosts).Hint(center.hostLabel).
				Placeholder(center.hostLabel)
			if host.Changed() {
				center.scroll = 0
				center.details.dismiss()
				state.describeTransferScope()
			}
			direction := ui.Select(c, &center.direction, transferDirections[:], state.transferDirectionLabel).
				Small().Width(min(134, width*0.23)).Height(30).Label(m.Direction).
				Hint(state.transferDirectionLabel(center.direction))
			if direction.Changed() {
				center.scroll = 0
				center.details.dismiss()
				state.describeTransferScope()
			}
			input := ui.TextField(c, &center.search, m.Search, m.Search, ui.WithID(hitTransfersSearch)).
				Icon(lucideIcons[iconSearch]).FontSize(metrics.FontSizeS).Height(30).MinWidth(52).MaxWidth(340).Grow(5)
			if input.Changed() {
				state.updateTransferSearch()
			}
			if input.Submitted() {
				c.Canvas().RequestFocus(hitTransfersList)
			}
			ui.Spacer(c)
			if width < 510 {
				state.declareIconButton(c, hitTransfersClear, iconTrash, center.clearLabel, center.visibleEnded == 0, iconButtonQuiet).Size(28, 28)
			} else {
				state.declareTransferAction(c, hitTransfersClear, iconTrash, center.clearLabel, center.visibleEnded == 0, false, false)
			}
		})
}

func (state *State) declareTransferRecovery(c ui.C) {
	center, m, pal := &state.transfers, state.messages.Transfers, state.pal
	if center.retryCount == 0 && center.conflictCount == 0 {
		return
	}
	ui.Row(c).Height(29).Shrink(0).FillWidth().Gap(4).Items(ui.Center).Children(func() {
		if center.retryCount > 0 {
			state.declareTransferAction(c, hitTransfersRetryFailed, iconRefresh, center.retryLabel, false, false, false)
		}
		if center.conflictCount > 0 {
			ui.Spacer(c)
			ui.Text(c, center.conflictLabel).Color(pal.muted).FontSize(metrics.FontSizeS).Ellipsis().MinWidth(0)
			state.declareTransferAction(c, hitTransfersSkipVisible, iconClose, m.Skip, false, false, false)
			state.declareTransferAction(c, hitTransfersReplaceVisible, iconCopy, m.Replace, false, true, true)
			state.declareTransferAction(c, hitTransfersResumeVisible, iconPlay, m.Resume, false, false, false)
		}
	})
}

func (state *State) declareTransferAction(c ui.C, id gfx.ID, icon uint8, label string, disabled, danger, boxed bool) {
	pal := state.pal
	ink, fill, edge := pal.text, gfx.Color{}, gfx.Color{}
	if boxed {
		fill, edge = pal.surfaceHot, pal.line
	}
	if danger {
		ink, fill, edge = pal.danger, withAlpha(pal.danger, 0.12), withAlpha(pal.danger, 0.6)
	}
	if disabled {
		ink, fill, edge = pal.faint, withAlpha(fill, fill.Alpha*0.45), withAlpha(edge, edge.Alpha*0.45)
	}
	ui.ButtonBase(c, label).Height(29).Shrink(0).PadX(8).Gap(6).Radius(radius.control).Background(fill).Border(1, edge).
		Hover(ui.Look{}.Background(withAlpha(pal.surfaceHot, 0.4).Over(fill))).Focus(ui.Look{}.Ring(1.5, pal.accent)).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Hint: label, Disabled: disabled}).Children(func() {
		ui.Icon(c, lucideIcons[icon]).Size(14, 14).Color(ink)
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(ink)
	})
}

func (state *State) paintTransferCenterBody(d *ui.DrawContext) {
	center, theme := &state.transfers, *d.Theme
	pad := theme.Metrics.PadSurface
	center.surface = gfx.Rect{X: d.Rect.X - pad, Y: d.Rect.Y - pad, Width: d.Rect.Width + 2*pad, Height: d.Rect.Height + 2*pad}
	center.list = center.body.list.Rect()
	if scroll := center.listBox.ScrollState(); scroll != nil {
		// The List owns its offset while the user moves it; adopt that offset so
		// the next paint does not scroll back to where the application last put
		// it. A reset (a filter change or a new upload) still goes through
		// center.scroll.
		center.scroll, center.scrollSynced = scroll.Offset, scroll.Offset
	}
	if center.count == 0 && center.list.Height >= 30 {
		state.paintTransferEmpty(d.Canvas, center.list, theme)
	}
}
