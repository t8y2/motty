package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
	"motty/internal/workbench/icons"
)

var (
	hitTransfersDetails       = tree.Sub(transferCenterHits, tree.Name("details"))
	hitTransfersDetailCard    = tree.ID(transferCenterHits, "details-card")
	hitTransfersDetailDismiss = tree.ID(transferCenterHits, "details-dismiss")
)

const (
	transferDetailDelay  = 230 * time.Millisecond
	transferDetailLinger = 140 * time.Millisecond
)

type transferDetailRow struct {
	id, entry int
}

type transferDetails struct {
	hover, current              transferDetailRow
	pinned, showing, suppressed int
	leftAt                      time.Duration
	intent                      components.HoverIntent
	lines                       [12][]string
}

func (details *transferDetails) dismiss() {
	id := details.pinned
	if id == 0 {
		id = details.showing
	}
	if id != 0 {
		details.suppressed = id
	}
	details.pinned, details.showing, details.leftAt = 0, 0, 0
	details.intent.Reset()
}

func (row transferDetailRow) anchor(canvas *gfx.Canvas, entries []transferEntry, list gfx.Rect, trigger gfx.Scope) gfx.Rect {
	if row.id == 0 || row.entry < 0 || row.entry >= len(entries) || entries[row.entry].id != row.id {
		return gfx.Rect{}
	}
	bounds, _ := canvas.HitBounds(trigger.At(row.id))
	return intersectRect(bounds, list)
}

// Paint outside the List's clip, so the details can sit over the rows below or
// flip above a row near the bottom of the window.
func (state *State) paintTransferDetails(canvas *gfx.Canvas, viewport gfx.Rect) {
	center, details := &state.transfers, &state.transfers.details
	if !center.open || !state.transferCenterAvailable() || !center.selectOpen.IsZero() || center.dragging || len(center.batch.targets) > 0 {
		details.dismiss()
		return
	}
	state.paintTransferDetailCard(canvas, viewport, center.list, details, hitTransfersDetails, hitTransfersDetailCard, hitTransfersDetailDismiss)
}

func (state *State) paintTransferDetailCard(canvas *gfx.Canvas, viewport, list gfx.Rect, details *transferDetails, trigger gfx.Scope, card, dismiss gfx.ID) {
	entries := state.transfers.entries
	ref := details.current
	anchor := ref.anchor(canvas, entries, list, trigger)
	if details.pinned == 0 {
		hover, now := details.hover, canvas.Now()
		if details.suppressed != hover.id {
			details.suppressed = 0
		}
		var candidate gfx.ID
		hoverAnchor := hover.anchor(canvas, entries, list, trigger)
		if hover.id != details.suppressed && canvas.Pressed().IsZero() && hoverAnchor.Height > 0 {
			candidate = trigger.At(hover.id)
		}
		ready, wait := details.intent.Rest(candidate, time.Time{}.Add(now), transferDetailDelay)
		switch {
		case !ready.IsZero():
			ref, anchor = hover, hoverAnchor
			details.leftAt = 0
		case canvas.Hovered() == card && anchor.Height > 0:
			details.leftAt = 0
		case anchor.Height > 0 && canvas.Pressed().IsZero():
			if details.leftAt == 0 {
				details.leftAt = now + transferDetailLinger
			}
			if linger := details.leftAt - now; linger > 0 {
				if wait <= 0 || linger < wait {
					wait = linger
				}
			} else {
				anchor = gfx.Rect{}
			}
		default:
			anchor = gfx.Rect{}
		}
		if wait > 0 {
			canvas.RequestFrameIn(wait)
		}
	}
	if anchor.Width <= 0 || anchor.Height <= 0 {
		details.pinned, details.showing, details.leftAt = 0, 0, 0
		return
	}
	details.showing = ref.id
	if details.pinned == 0 {
		dismiss = gfx.ID{}
	}
	state.drawTransferDetails(canvas, viewport, anchor, &entries[ref.entry], &details.lines, card, dismiss)
}

type transferDetailField struct {
	name, value string
	icon        gfx.Icon
}

func (state *State) drawTransferDetails(canvas *gfx.Canvas, viewport, anchor gfx.Rect, entry *transferEntry, lines *[12][]string, id, dismiss gfx.ID) {
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

	width := min(492, viewport.Width-24)
	pad := theme.Metrics.PadSurface
	labelWidth := min(106, width*0.25)
	labelOffset, gap := float32(22), float32(5)
	size := theme.Metrics.FontSizeS
	lineHeight := size + 5
	valueOffset := labelOffset + labelWidth + 8
	valueWidth := max(1, width-2*pad-valueOffset)
	bodyHeight := float32(0)
	// Leave at least one line for each field, and spend the free height on
	// long names and paths. Keep the complete text in the accessibility tree.
	extra := max(0, int((viewport.Height-70-float32(n)*(lineHeight+gap))/lineHeight))
	for index, field := range fields[:n] {
		lines[index] = canvas.WrapLines(lines[index][:0], field.value, size, gfx.WeightRegular, valueWidth, 1+min(extra, 5))
		extra = max(0, extra-(len(lines[index])-1))
		bodyHeight += float32(len(lines[index]))*lineHeight + gap
	}
	card := components.BeginHoverCard(canvas, theme, viewport, anchor, components.HoverCardOptions{
		Layer: hoverCardLayer, ID: id, DismissID: dismiss,
		Side: components.HoverCardBottom, Align: components.HoverCardAlignStart,
		SideOffset: 3, AlignOffset: max(4, min(anchor.Width*0.55, 190)),
		ContentSize: gfx.Size{Width: width}, BodyHeight: bodyHeight,
	})
	layout := card.Begin(m.Details, "")
	canvas.Clip(layout.Body)
	y := layout.Body.Y
	for index, field := range fields[:n] {
		x := layout.Body.X
		if field.icon.ViewBox > 0 {
			canvas.DrawIcon(field.icon, gfx.Rect{X: x, Y: y + (lineHeight-14)/2, Width: 14, Height: 14}, theme.MutedForeground)
		}
		canvas.Label(field.name, gfx.Rect{X: x + labelOffset, Y: y, Width: labelWidth, Height: lineHeight},
			gfx.TextStyle{Size: size, Color: theme.MutedForeground})
		for line, value := range lines[index] {
			canvas.Label(value, gfx.Rect{X: x + valueOffset, Y: y + float32(line)*lineHeight, Width: valueWidth, Height: lineHeight},
				gfx.TextStyle{Size: size, Color: theme.PopoverForeground})
		}
		height := float32(len(lines[index])) * lineHeight
		canvas.ContainerWithOptions(gfx.Rect{X: x, Y: y, Width: layout.Body.Width, Height: height},
			gfx.RoleGroup, field.name, gfx.HitOptions{Description: field.value})
		y += height + gap
	}
	canvas.Unclip()
	card.End()
}

func transferDetailHost(server, address string) string {
	if address == "" || address == server {
		return server
	}
	if server == "" {
		return address
	}
	return server + " · " + address
}

func transferDetailTime(at time.Time) string {
	if at.IsZero() {
		return ""
	}
	return at.Local().Format("2006-01-02 15:04:05")
}
