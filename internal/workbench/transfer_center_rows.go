package workbench

import (
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/workbench/icons"
)

func (state *State) declareTransferRow(row ui.ListRow) {
	center, pal, c := &state.transfers, state.pal, row.C
	item := center.rows[row.Index]
	width := max(center.listWidth-10, 0) // leave the scrollbar clear of row actions
	row.Element.Width(width).Height(transferRowHeight).PadX(8).Gap(9).Items(ui.Center)
	if item.entry < 0 {
		state.declareTransferGroup(row, item.group)
		return
	}
	entry := &center.entries[item.entry]
	id := hitTransfersDetails.At(entry.id)
	canvas := c.Canvas()
	hover := canvas.Hovered() == row.ID || canvas.Hovered() == id
	if hovered, ok := canvas.Hovered().In(hitTransfersStatus); ok && hovered == entry.id {
		hover = true
	}
	details := &center.details
	if hover || details.showing == entry.id || details.pinned == entry.id {
		row.Element.Background(withAlpha(state.transferBlue(), 0.14)).Radius(radius.control)
	}
	row.Element.Children(func() {
		name := ui.Row(c).Grow(1).MinWidth(32).Height(30).Gap(8).Items(ui.Center).Radius(radius.control).
			Focus(ui.Look{}.Ring(1.5, withAlpha(pal.accent, 0.55))).
			HitWith(id, entry.title, gfx.HitOptions{Role: gfx.RoleButton, Description: state.messages.Transfers.Details})
		name.Children(func() {
			glyph, mono := entry.fileIcon, entry.monoIcon
			if glyph == nil {
				glyph, mono = &icons.File, true
			}
			tint := pal.text
			if mono {
				tint = pal.muted
			}
			ui.Icon(c, *glyph).Size(20, 20).Color(tint).Shrink(0)
			ui.Text(c, entry.title).Color(pal.text).Weight(gfx.WeightSemibold).FontSize(metrics.FontSizeM).Ellipsis().Grow(1).MinWidth(0)
		})
		ref := transferDetailRow{entry: item.entry, id: entry.id, box: name}
		if hover {
			details.hover = ref
		}
		if entry.id == details.pinned || details.pinned == 0 && entry.id == details.showing {
			details.current = ref
		}
		direction := transferDirectionOf(entry.snapshot.Operation)
		state.declareTransferDirection(c, direction, width < 630)
		if width >= 690 {
			amount := entry.size
			if item.group == transferFilterRunning || entry.snapshot.State == sftpclient.TransferPaused {
				amount = entry.amount
			}
			ui.Text(c, amount).FontSize(metrics.FontSizeS).Color(pal.text).Width(128).Shrink(0).Ellipsis()
		}
		state.declareTransferResult(c, entry, width)
		state.declareTransferRowActions(c, entry, width >= 970)
		ui.Slot(c).Absolute().Bottom(0).Left(0).Right(0).Height(1).Background(withAlpha(pal.line, 0.6))
	})
}

func (state *State) declareTransferGroup(row ui.ListRow, group int) {
	pal, c := state.pal, row.C
	section := &state.transfers.groups[group]
	glyph, tint := state.transferGroupLook(group)
	chevron := lucideIcons[iconChevronDown]
	if section.collapsed {
		chevron = lucideIcons[iconChevronRight]
	}
	row.Element.Radius(radius.control).Hover(ui.Look{}.Background(withAlpha(pal.surfaceHot, 0.5))).
		Focus(ui.Look{}.Ring(1.5, withAlpha(pal.accent, 0.55))).
		HitWith(hitTransfersGroup.At(group), section.label, gfx.HitOptions{Role: gfx.RoleButton, Expanded: !section.collapsed}).
		Children(func() {
			ui.Icon(c, chevron).Size(14, 14).Color(pal.muted)
			ui.Icon(c, glyph).Size(16, 16).Color(tint)
			ui.Text(c, section.label).FontSize(metrics.FontSizeM).Weight(gfx.WeightSemibold).Color(pal.text)
		})
}

func (state *State) transferGroupLook(group int) (gfx.Icon, gfx.Color) {
	pal := state.pal
	switch group {
	case transferFilterRunning:
		return lucideIcons[iconActivity], state.transferBlue()
	case transferFilterEnded:
		return icons.CircleCheck, pal.success
	case transferFilterFailed:
		return icons.CircleAlert, pal.danger
	default:
		return lucideIcons[iconClock], pal.muted
	}
}

func (state *State) declareTransferDirection(c ui.C, direction transferDirection, compact bool) {
	pal := state.pal
	glyph, ink := icons.ArrowUp, state.transferBlue()
	switch direction {
	case transferDownload:
		glyph, ink = icons.ArrowDown, pal.success
	case transferRemoteCopy:
		glyph, ink = icons.Copy, pal.muted
	}
	width := float32(90)
	if compact {
		width = 25
	}
	ui.Row(c).Width(width).Shrink(0).Items(ui.Center).Children(func() {
		ui.Row(c).PadX(5).Gap(4).Height(21).Items(ui.Center).Radius(radius.control).Background(withAlpha(ink, 0.15)).Children(func() {
			ui.Icon(c, glyph).Size(13, 13).Color(ink).Shrink(0)
			if !compact {
				ui.Text(c, state.transferDirectionLabel(direction)).FontSize(metrics.FontSizeS).Color(ink).Ellipsis()
			}
		})
	})
}

func (state *State) declareTransferResult(c ui.C, entry *transferEntry, width float32) {
	pal := state.pal
	snapshot := entry.snapshot
	statusHint := snapshot.Error
	if snapshot.State == sftpclient.TransferInterrupted {
		statusHint = state.messages.Transfers.InterruptedHint
	}
	rowWidth := min(375, width*0.35)
	if width < 630 {
		rowWidth = min(140, width*0.28)
	}
	ui.Row(c).Width(rowWidth).Height(26).Gap(8).Items(ui.Center).Shrink(0).MinWidth(0).
		HitWith(hitTransfersStatus.At(entry.id), entry.outcome, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: statusHint}).
		Children(func() {
			if snapshot.State == sftpclient.TransferRunning {
				state.declareTransferProgress(c, transferFraction(snapshot), 7, 12)
				ui.Text(c, entry.percent).FontSize(metrics.FontSizeS).Weight(gfx.WeightSemibold).Color(pal.text).Width(32).TextEnd().Shrink(0)
				if width >= 810 {
					ui.Text(c, entry.speed).FontSize(metrics.FontSizeS).Color(pal.text).Width(88).TextEnd().Shrink(0).Ellipsis()
				}
				if width >= 970 {
					status := entry.eta
					if status == "" {
						status = entry.status
					}
					ui.Text(c, status).FontSize(metrics.FontSizeS).Color(pal.muted).Width(91).TextEnd().Shrink(0).Ellipsis()
				}
				return
			}
			glyph, tint := state.transferGroupLook(transferGroupOf(snapshot.State))
			if snapshot.State == sftpclient.TransferCanceled {
				glyph, tint = icons.X, pal.muted
			}
			ui.Icon(c, glyph).Size(15, 15).Color(tint).Shrink(0)
			ui.Text(c, entry.outcome).FontSize(metrics.FontSizeS).Color(pal.text).Ellipsis().Grow(1).MinWidth(0)
		})
}

func (state *State) declareTransferRowActions(c ui.C, entry *transferEntry, wide bool) {
	m, snapshot := state.messages.Transfers, entry.snapshot
	ui.Row(c).Width(104).Height(28).Gap(2).Items(ui.Center).Shrink(0).Children(func() {
		ui.Spacer(c)
		if transferActive(snapshot) {
			if entry.owner != nil {
				glyph, label := uint8(iconPause), state.messages.Tools.TransferPause
				if snapshot.State == sftpclient.TransferPaused {
					glyph, label = iconPlay, state.messages.Tools.TransferResume
				}
				state.declareIconButton(c, hitTransfersPause.At(entry.id), glyph, label, false, iconButtonQuiet).Size(24, 26)
				state.declareIconButton(c, hitTransfersCancel.At(entry.id), iconClose, state.messages.SFTPCancelTransfer, false, iconButtonQuiet).Size(24, 26)
			}
			return
		}
		if snapshot.Retryable && entry.owner != nil {
			if entry.conflict {
				state.declareIconButton(c, hitTransfersSkip.At(entry.id), iconClose, m.Skip, false, iconButtonQuiet).Size(24, 26)
				state.declareIconButton(c, hitTransfersReplace.At(entry.id), iconCopy, m.Replace, false, iconButtonDanger).Size(24, 26)
				state.declareIconButton(c, hitTransfersResume.At(entry.id), iconPlay, m.Resume, false, iconButtonQuiet).Size(24, 26)
			} else if wide {
				state.declareTransferAction(c, hitTransfersRetry.At(entry.id), iconRefresh, m.Retry, false, false, false)
			} else {
				state.declareIconButton(c, hitTransfersRetry.At(entry.id), iconRefresh, m.Retry, false, iconButtonQuiet).Size(24, 26)
			}
		}
		if transferCanReveal(snapshot) {
			state.declareIconButton(c, hitTransfersReveal.At(entry.id), iconFolderOpen, m.ShowInFolder, state.revealBusy, iconButtonQuiet).Size(24, 26)
		}
		state.declareIconButton(c, hitTransfersRemove.At(entry.id), iconTrash, m.Remove, false, iconButtonQuiet).Size(24, 26)
	})
}
