package workbench

import (
	"fmt"
	"time"

	"motty/internal/sftpclient"
	"motty/internal/workbench/icons"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
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
	hover, current transferDetailRow
	// hoverBox and currentBox are the trigger boxes those rows were declared
	// with this paint. The card is a page Popover anchored on them.
	hoverBox, currentBox ui.Box
	// hoverWidth and currentWidth are the trigger widths the last paint laid
	// out. AlignOffset is read while the page is declared, before the frame's
	// layout, so it uses the widths a previous paint measured for those rows.
	hoverWidth, currentWidth float32
	pinned, showing          int
	suppressed               int
	leftAt                   time.Duration
	intent                   components.HoverIntent
	open                     bool
	selection                ui.TextSelectionState // what the user selected of the card's fields, to copy it
	// The body is a Draw slot that paints the fields in the old card's places.
	fields                        [12]transferDetailField
	fieldCount                    int
	lines                         [12][]string
	size, lineHeight              float32
	labelWidth, valueWidth        float32
	labelOffset, valueOffset, gap float32
	bodyHeight                    float32
	// slotHeight is the body slot's layout height: the title line and the
	// padding the page's header would have taken, without its border.
	slotHeight float32
	c          ui.C
	body       func()
	draw       func(*ui.DrawContext)
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
	details.open = false
	details.intent.Reset()
}

// beginPaint forgets the rows the last paint saw. pinned, showing and the
// hover intent stay: they are what the preview is open for.
func (details *transferDetails) beginPaint() {
	details.hover, details.current = transferDetailRow{}, transferDetailRow{}
	details.hoverBox, details.currentBox = ui.Box{}, ui.Box{}
}

// anchor is the rectangle the row's trigger took in this paint, clipped to the
// list. The old hover card hung off exactly this rectangle.
func (row transferDetailRow) anchor(canvas *gfx.Canvas, entries []transferEntry, list gfx.Rect, trigger gfx.Scope) gfx.Rect {
	if row.id == 0 || row.entry < 0 || row.entry >= len(entries) || entries[row.entry].id != row.id {
		return gfx.Rect{}
	}
	bounds, _ := canvas.HitBounds(trigger.At(row.id))
	return intersectRect(bounds, list)
}

// recordTransferDetailAnchors measures the rows the page declared this paint,
// after its layout. The card opens on the next paint, and AlignOffset follows
// the trigger width the row actually got.
func (state *State) recordTransferDetailAnchors(canvas *gfx.Canvas) {
	center, details := &state.transfers, &state.transfers.details
	if !center.open || !state.transferCenterAvailable() || len(center.batch.targets) > 0 {
		details.dismiss()
		details.hoverWidth, details.currentWidth = 0, 0
		return
	}
	entries := center.entries
	hit := details.hover.anchor(canvas, entries, center.list, hitTransfersDetails)
	details.hoverWidth = hit.Width
	details.currentWidth = details.current.anchor(canvas, entries, center.list, hitTransfersDetails).Width
}

// declareTransferCenterPreview declares the pointer preview of a transfer row
// outside the list's clip, as a page Popover anchored on the row's trigger.
func (state *State) declareTransferCenterPreview(c ui.C) {
	center, details := &state.transfers, &state.transfers.details
	if !center.open || !state.transferCenterAvailable() || len(center.batch.targets) > 0 {
		details.dismiss()
		return
	}
	canvas := c.Canvas()
	ref, anchor, width := details.current, details.currentBox, details.currentWidth
	if width <= 0 {
		// The clicked row was measured as the hovered one on the paint that
		// pinned it; its current width is measured after this frame's layout.
		width = details.hoverWidth
	}
	if details.pinned == 0 {
		hover, now := details.hover, canvas.Now()
		if details.suppressed != hover.id {
			details.suppressed = 0
		}
		var candidate gfx.ID
		if hover.id != details.suppressed && canvas.Pressed().IsZero() && hover.id != 0 && details.hoverBox.Valid() {
			candidate = hitTransfersDetails.At(hover.id)
		}
		ready, wait := details.intent.Rest(candidate, time.Time{}.Add(now), transferDetailDelay)
		switch {
		case !ready.IsZero():
			ref, anchor, width = hover, details.hoverBox, details.hoverWidth
			details.leftAt = 0
		case canvas.Hovered() == hitTransfersDetailCard && details.showing != 0:
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
				ref, anchor, width = transferDetailRow{}, ui.Box{}, 0
			}
		default:
			ref, anchor, width = transferDetailRow{}, ui.Box{}, 0
		}
		if wait > 0 {
			canvas.RequestFrameIn(wait)
		}
	}
	entries := center.entries
	if ref.id == 0 || ref.entry < 0 || ref.entry >= len(entries) ||
		entries[ref.entry].id != ref.id || !anchor.Valid() || width <= 0 {
		details.pinned, details.showing, details.leftAt = 0, 0, 0
		details.open = false
		return
	}
	details.showing = ref.id
	viewport := state.windowRect()
	state.prepareTransferDetailBody(canvas, &entries[ref.entry], viewport)
	details.open = true
	dismiss := gfx.ID{}
	if details.pinned != 0 {
		dismiss = hitTransfersDetailDismiss
	}
	details.c = c
	if details.body == nil {
		details.body = state.declareTransferCenterBody
	}
	if details.draw == nil {
		details.draw = state.drawTransferDetailBody
	}
	spec := ui.PopoverSpec{
		Side: components.PopoverBottom, Align: ui.Start, Offset: 3,
		AlignOffset: max(4, min(width*0.55, 190)),
		Width:       min(492, viewport.Width-24),
		// The old hover card was a popup surface, not a button-styled popover:
		// it kept the popup radius and had no press-outside target of its own.
		Radius:    state.Theme().Metrics.RadiusPopup(),
		NoDismiss: dismiss.IsZero(),
		DismissID: dismiss, KeepOpenOnAnchor: true,
	}
	if ui.Popover(c, anchor, &details.open, spec, details.body) {
		details.dismiss()
	}
}

// declareTransferCenterBody declares the card's one body slot, which paints the
// title and the fields itself: the old card put them at the surface's inner
// edge, while the page's border and header would inset them a pixel more.
func (state *State) declareTransferCenterBody() {
	details := &state.transfers.details
	ui.Slot(details.c).FillWidth().Height(details.slotHeight).Draw(details.draw)
}

type transferDetailField struct {
	name, value string
	icon        gfx.Icon
}

// prepareTransferDetailBody fills the fields, their wrapped lines and the
// geometry the body paints at, exactly as the old card measured them.
func (state *State) prepareTransferDetailBody(canvas *gfx.Canvas, entry *transferEntry, bounds gfx.Rect) {
	details := &state.transfers.details
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
	details.fieldCount = 0
	add := func(name, value string, icon gfx.Icon) {
		if value == "" {
			value = "—"
		}
		details.fields[details.fieldCount] = transferDetailField{name, value, icon}
		details.fieldCount++
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

	width := min(492, bounds.Width-24)
	pad := theme.Metrics.PadSurface
	details.labelWidth = min(106, width*0.25)
	details.labelOffset, details.gap = float32(22), float32(5)
	details.size = theme.Metrics.FontSizeS
	details.lineHeight = details.size + 5
	details.valueOffset = details.labelOffset + details.labelWidth + 8
	details.valueWidth = max(1, width-2*pad-details.valueOffset)
	details.bodyHeight = 0
	// Leave at least one line for each field, and spend the free height on
	// long names and paths. Keep the complete text in the accessibility tree.
	n := details.fieldCount
	extra := max(0, int((bounds.Height-70-float32(n)*(details.lineHeight+details.gap))/details.lineHeight))
	for index := 0; index < n; index++ {
		field := &details.fields[index]
		details.lines[index] = canvas.WrapLines(details.lines[index][:0], field.value,
			details.size, gfx.WeightRegular, details.valueWidth, 1+min(extra, 5))
		extra = max(0, extra-(len(details.lines[index])-1))
		details.bodyHeight += float32(len(details.lines[index]))*details.lineHeight + details.gap
	}
	// The single body slot is what the page measures; the surface's 1px border
	// takes two pixels of the height the old card's padding used.
	details.slotHeight = pad + (theme.Metrics.FontSizeM + 7) + details.bodyHeight - 2
}

// drawTransferDetailBody paints the fields into the card body the page placed.
func (state *State) drawTransferDetailBody(d *ui.DrawContext) {
	details := &state.transfers.details
	canvas, theme := d.Canvas, state.Theme()
	// The page's Popover lays its border inside the surface and reserves the
	// space, while the old card painted the same border within the surface and
	// inset its content by the padding alone. Expanding the slot by the border
	// reproduces the old content box, title and body exactly.
	const border = float32(1)
	pad := theme.Metrics.PadSurface
	titleLine := theme.Metrics.FontSizeM + 7
	content := d.Rect
	inner := gfx.Rect{X: content.X - border, Y: content.Y - border, Width: content.Width + 2*border, Height: content.Height + 2*border}
	// The old card was one group hit over its whole surface, so the pointer can
	// move off the row onto the preview and keep it open.
	card := gfx.Rect{
		X: content.X - border - pad, Y: content.Y - border - pad,
		Width: content.Width + 2*(border+pad), Height: content.Height + 2*(border+pad),
	}
	canvas.HitWithOptions(hitTransfersDetailCard, card, state.messages.Transfers.Details,
		gfx.HitOptions{Role: gfx.RoleGroup, SkipFocus: true})
	canvas.Label(state.messages.Transfers.Details,
		gfx.Rect{X: inner.X, Y: inner.Y, Width: inner.Width, Height: titleLine},
		gfx.TextStyle{Size: theme.Metrics.FontSizeM, Weight: gfx.WeightMedium, Color: theme.PopoverForeground})
	body := gfx.Rect{X: inner.X, Y: inner.Y + titleLine + pad, Width: inner.Width, Height: details.bodyHeight}
	canvas.Clip(body)
	y := body.Y
	for index := 0; index < details.fieldCount; index++ {
		field := &details.fields[index]
		x := body.X
		if field.icon.ViewBox > 0 {
			canvas.DrawIcon(field.icon, gfx.Rect{X: x, Y: y + (details.lineHeight-14)/2, Width: 14, Height: 14}, theme.MutedForeground)
		}
		canvas.Label(field.name, gfx.Rect{X: x + details.labelOffset, Y: y, Width: details.labelWidth, Height: details.lineHeight},
			gfx.TextStyle{Size: details.size, Color: theme.MutedForeground})
		for line, value := range details.lines[index] {
			canvas.Label(value, gfx.Rect{X: x + details.valueOffset, Y: y + float32(line)*details.lineHeight, Width: details.valueWidth, Height: details.lineHeight},
				gfx.TextStyle{Size: details.size, Color: theme.PopoverForeground})
		}
		height := float32(len(details.lines[index])) * details.lineHeight
		canvas.ContainerWithOptions(gfx.Rect{X: x, Y: y, Width: body.Width, Height: height},
			gfx.RoleGroup, field.name, gfx.HitOptions{Description: field.value})
		y += height + details.gap
	}
	canvas.Unclip()
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
