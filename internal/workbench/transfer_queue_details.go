package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/workbench/icons"
)

func (state *State) declareTransferDetailCard(c ui.C, details *transferDetails, trigger gfx.Scope, card, dismiss gfx.ID, allowed bool) {
	if !allowed {
		details.dismiss()
		return
	}
	canvas := c.Canvas()
	ref, anchor := details.current, details.currentBox
	if details.pinned == 0 {
		hover, now := details.hover, canvas.Now()
		if details.suppressed != hover.id {
			details.suppressed = 0
		}
		var candidate gfx.ID
		if hover.id != details.suppressed && canvas.Pressed().IsZero() && hover.id != 0 && details.hoverBox.Valid() {
			candidate = trigger.At(hover.id)
		}
		ready, wait := details.intent.Rest(candidate, time.Time{}.Add(now), transferDetailDelay)
		switch {
		case !ready.IsZero():
			ref, anchor = hover, details.hoverBox
			details.leftAt = 0
		case canvas.Hovered() == card && details.showing != 0:
			details.leftAt = 0
		case details.showing != 0 && canvas.Pressed().IsZero():
			if details.leftAt == 0 {
				details.leftAt = now + transferDetailLinger
			}
			if linger := details.leftAt - now; linger > 0 {
				if wait <= 0 || linger < wait {
					wait = linger
				}
			} else {
				ref, anchor = transferDetailRow{}, ui.Box{}
			}
		default:
			ref, anchor = transferDetailRow{}, ui.Box{}
		}
		if wait > 0 {
			canvas.RequestFrameIn(wait)
		}
	}
	if ref.id == 0 || ref.entry < 0 || ref.entry >= len(state.transfers.entries) ||
		state.transfers.entries[ref.entry].id != ref.id || !anchor.Valid() {
		details.pinned, details.showing, details.leftAt = 0, 0, 0
		details.open = false
		return
	}
	details.showing = ref.id
	details.open = true
	entry := &state.transfers.entries[ref.entry]
	width := min(492, c.Bounds().Width-24)
	spec := ui.PopoverSpec{
		Side: components.PopoverBottom, Align: ui.Start, Offset: 1, Width: width,
		DismissID: dismiss, KeepOpenOnAnchor: true,
	}
	if ui.PopoverWith(c, anchor, spec, &details.open, func() {
		state.declareTransferDetailBody(c, entry, card)
	}) {
		details.dismiss()
	}
}

func (state *State) declareTransferDetailBody(c ui.C, entry *transferEntry, card gfx.ID) {
	m, shell := state.messages.Transfers, state.messages.SFTPWindow
	theme := state.Theme()
	snapshot := entry.snapshot
	direction := transferDirectionOf(snapshot.Operation)
	from, to := shell.Local, shell.Remote
	if direction == transferDownload {
		from, to = shell.Remote, shell.Local
	} else if direction == transferRemoteCopy {
		from = shell.Remote
	}
	host := transferDetailHost(entry.server, entry.address)
	if direction == transferRemoteCopy {
		if source := transferDetailHost(entry.sourceServer, entry.sourceAddress); source != "" {
			host = source + " → " + host
		}
	}
	fields := [12]transferDetailField{}
	n := 0
	add := func(name, value string, icon gfx.Icon) {
		if value == "" {
			value = "—"
		}
		fields[n] = transferDetailField{name, value, icon}
		n++
	}
	fileIcon, folder := lucideIcons[iconFile], lucideIcons[iconFolderOpen]
	clock := lucideIcons[iconClock]
	add(m.FileName, entry.title, fileIcon)
	add(m.Direction, fmt.Sprintf("%s (%s → %s)", state.transferDirectionLabel(direction), from, to), lucideIcons[iconTransfer])
	add(m.Hosts, host, icons.Monitor)
	switch direction {
	case transferUpload:
		add(m.LocalPath, snapshot.Source, folder)
		add(m.RemotePath, snapshot.Destination, folder)
	case transferDownload:
		add(m.LocalPath, snapshot.Destination, folder)
		add(m.RemotePath, snapshot.Source, folder)
	case transferRemoteCopy:
		add(m.SourcePath, snapshot.Source, folder)
		add(m.TargetPath, snapshot.Destination, folder)
	}
	add(m.Size, entry.amount, fileIcon)
	if snapshot.StartedAt.IsZero() {
		add(m.QueuedAt, transferDetailTime(snapshot.QueuedAt), clock)
	} else {
		add(m.StartedAt, transferDetailTime(snapshot.StartedAt), clock)
	}
	add(m.Elapsed, entry.elapsed, clock)
	if snapshot.State == sftpclient.TransferRunning {
		add(m.Speed, entry.speed, gfx.Icon{})
	}
	if !snapshot.FinishedAt.IsZero() && !transferActive(snapshot) {
		add(m.FinishedAt, transferDetailTime(snapshot.FinishedAt), clock)
	}
	if snapshot.State == sftpclient.TransferFailed && snapshot.Error != "" {
		add(m.Error, snapshot.Error, icons.CircleAlert)
	}

	width := min(492, c.Bounds().Width-24)
	labelWidth := min(106, width*0.25)
	labelOffset := float32(22)
	size := theme.Metrics.FontSizeS
	lineHeight := size + 5
	// Leave at least one line for each field, and spend the free height on long
	// names and paths. Keep the complete text in the accessibility tree.
	extra := max(0, int((c.Bounds().Height-70-float32(n)*(lineHeight+5))/lineHeight))
	maxLines := 1 + min(extra, 5)
	fieldsScope := tree.Sub(card.Scope(), tree.Name("detail-field"))
	ui.Column(c).Width(width).Gap(5).
		HitWith(card, m.Details, gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true, SkipTab: true}).
		Children(func() {
			for index, field := range fields[:n] {
				ui.Row(c).Items(ui.Start).Gap(3).
					HitWith(fieldsScope.At(index), field.name, gfx.HitOptions{
						Role: gfx.RoleGroup, Description: field.value, SkipFocus: true, SkipTab: true,
					}).
					Children(func() {
						if field.icon.ViewBox > 0 {
							ui.Icon(c, field.icon).Size(14, 14).Color(theme.MutedForeground).Shrink(0).
								MarginTop((lineHeight - 14) / 2)
						}
						ui.Text(c, field.name).Width(labelWidth).Shrink(0).FontSize(size).
							Color(theme.MutedForeground).MarginLeft(labelOffset - 14)
						ui.Paragraph(c, field.value, maxLines).Grow(1).FontSize(size).Color(theme.PopoverForeground)
					})
			}
		})
}
