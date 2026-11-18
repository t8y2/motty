package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

const transferRowHeight = float32(38)

func (state *State) declareTransferCenter(c ui.C) {
	center := &state.transfers
	center.details.beginPaint()
	if !center.open || !state.transferCenterAvailable() {
		return
	}
	center.handlers(state)
	viewport, anchor := c.Bounds(), center.anchor
	if anchor.Width <= 0 {
		anchor = gfx.Rect{X: max(viewport.Width-16, 0), Y: windowBarHeight, Width: 1, Height: 1}
	}
	width := max(0, min(1180, viewport.Width-24))
	padding := c.Theme().Metrics.PadSurface
	// Size the panel for the side it opens on, including its padding. A panel
	// taller than the room below the toolbar would clamp back over its toggle.
	const gap, margin = float32(8), float32(8)
	room := viewport.Y + viewport.Height - margin - anchor.Y - anchor.Height - gap
	side := components.PopoverBottom
	if above := anchor.Y - gap - (viewport.Y + margin); room < 760+2*padding && above > room {
		room, side = above, components.PopoverTop
	}
	height := max(0, min(760, room-2*padding))
	center.listWidth = max(0, width-2*padding)
	initialFocus := hitTransfersList
	if len(center.batch.targets) > 0 {
		initialFocus = hitTransfersBulkCancel
	}
	origin := ui.Slot(c).Absolute().Left(anchor.X).Top(anchor.Y).Width(max(anchor.Width, 1)).Height(max(anchor.Height, 1))
	spec := ui.PopoverSpec{
		Scope: hitTransfersScope, DismissID: hitTransfersDismiss, InitialFocusID: initialFocus,
		Side: side, Align: ui.End, Offset: gap, Width: width,
	}
	if ui.PopoverWith(c, origin, spec, &center.open, func() { state.declareTransferBody(c, height) }) {
		center.close()
	}
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
		if len(center.batch.targets) > 0 {
			state.declareTransferBatchBody(c)
			return
		}
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
			Label(m.Title).Scrollbars().HitWith(hitTransfersList, m.Title, gfx.HitOptions{Role: gfx.RoleGroup})
		center.body.list = center.listBox
		if scroll := center.listBox.ScrollState(); scroll != nil && center.scroll != center.scrollSynced {
			scroll.ScrollTo(center.scroll)
			center.scrollSynced = center.scroll
			// The List declared its visible rows at its previous offset.
			c.Canvas().RequestFrame()
		}
		state.declareTransferDetails(c)
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
		ui.Row(c).PadX(5).Height(17).Center().Shrink(0).Radius(8.5).Background(withAlpha(ink, 0.13)).Children(func() {
			ui.Text(c, center.groups[index].count).FontSize(metrics.FontSizeS - 1).Color(ink)
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
			}).Small().Width(min(172, width*0.23)).Label(m.Hosts).Hint(center.hostLabel).
				Placeholder(center.hostLabel)
			if host.Changed() {
				center.scroll = 0
				center.details.dismiss()
				state.describeTransferScope()
			}
			direction := ui.Select(c, &center.direction, transferDirections[:], state.transferDirectionLabel).
				Small().Width(min(134, width*0.23)).Label(m.Direction).
				Hint(state.transferDirectionLabel(center.direction))
			if direction.Changed() {
				center.scroll = 0
				center.details.dismiss()
				state.describeTransferScope()
			}
			input := ui.TextFieldID(c, hitTransfersSearch, &center.search, m.Search, m.Search).
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
	if len(center.batch.targets) > 0 {
		center.list = gfx.Rect{}
		return
	}
	center.list = center.body.list.Rect()
	if scroll := center.listBox.ScrollState(); scroll != nil {
		// The List owns its offset; remember where the user left it so a reset
		// (a filter change or a new upload) can put it back at the top.
		center.scrollSynced = scroll.Offset
	}
	if center.count == 0 && center.list.Height >= 30 {
		state.paintTransferEmpty(d.Canvas, center.list, theme)
	}
}
