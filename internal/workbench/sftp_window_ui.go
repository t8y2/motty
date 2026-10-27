package workbench

import (
	"fmt"
	"math"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
)

var sftpWindowHits = tree.Feature("sftp-window")
var (
	hitSFTPDetach             = tree.ID(sftpHits, "detach")
	sftpWindowSides           = tree.Sub(sftpWindowHits, tree.Name("sides"))
	sftpWindowLocal           = tree.Sub(sftpWindowSides, tree.Name("local"))
	sftpWindowRemote          = tree.Sub(sftpWindowSides, tree.Name("remote"))
	sftpWindowRows            = [2]ui.Scope{tree.Sub(sftpWindowLocal, tree.Name("rows")), tree.Sub(sftpWindowRemote, tree.Name("rows"))}
	sftpWindowPath            = [2]ui.ID{tree.ID(sftpWindowLocal, "path"), tree.ID(sftpWindowRemote, "path")}
	sftpWindowUp              = [2]ui.ID{tree.ID(sftpWindowLocal, "up"), tree.ID(sftpWindowRemote, "up")}
	sftpWindowRefresh         = [2]ui.ID{tree.ID(sftpWindowLocal, "refresh"), tree.ID(sftpWindowRemote, "refresh")}
	sftpWindowNewFolder       = [2]ui.ID{tree.ID(sftpWindowLocal, "new-folder"), tree.ID(sftpWindowRemote, "new-folder")}
	sftpWindowBar             = [2]ui.ID{tree.ID(sftpWindowLocal, "scroll"), tree.ID(sftpWindowRemote, "scroll")}
	hitSFTPWindowSplit        = tree.ID(sftpWindowHits, "split")
	hitSFTPWindowUpload       = tree.ID(sftpWindowHits, "upload")
	hitSFTPWindowDownload     = tree.ID(sftpWindowHits, "download")
	hitSFTPWindowCloseCancel  = tree.ID(sftpWindowHits, "close-cancel")
	hitSFTPWindowCloseConfirm = tree.ID(sftpWindowHits, "close-confirm")
	hitSFTPWindowMenuDismiss  = tree.ID(sftpWindowHits, "menu-dismiss")
	hitSFTPWindowMenuOpen     = tree.ID(sftpWindowHits, "menu-open")
	hitSFTPWindowMenuTransfer = tree.ID(sftpWindowHits, "menu-transfer")
	hitSFTPWindowMenuCopy     = tree.ID(sftpWindowHits, "menu-copy")
	hitSFTPWindowQueueBar     = tree.ID(sftpWindowHits, "queue-scroll")
	sftpWindowQueueRows       = tree.Sub(sftpWindowHits, tree.Name("queue-rows"))
)

type sftpWindowUI struct {
	frame       flex.View
	panes       [2]flex.View
	rows        [2]flex.View
	size        ui.Size
	list        [2]ui.Rect
	offset      [2]float32
	bars        [2]components.ScrollAreaGeometry
	barDrag     int // side + 1, zero when idle
	barGrab     float32
	fraction    float32
	split       components.Resizable
	splitting   bool
	tooltip     components.HoverTooltip
	menuOpen    bool
	menuAt      ui.Point
	queueBounds ui.Rect
	queueOffset float32
	queueBar    components.ScrollAreaGeometry
}

func sideIndex(local bool) int {
	if local {
		return 0
	}
	return 1
}

func (w *sftpWindow) paint(canvas *ui.Canvas, size ui.Size) {
	canvas.SetTree(&tree)
	if id, ok := canvas.ClickTarget(); ok {
		canvas.AfterPaint(func() { w.click(id) })
	}
	w.ui.size = size
	theme, m := w.owner.Theme(), w.owner.messages
	canvas.Fill(ui.Rect{Width: size.Width, Height: size.Height}, theme.Background)
	var body, queue flex.Box
	f := w.ui.frame.Begin(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Pad(12).Gap(8).Children(func() {
		f.Row().Height(32).Gap(8).Items(flex.Center).Children(func() {
			f.Text(w.session.Title + " · " + w.session.Address).FontSize(18).Bold().Color(theme.Foreground).Ellipsis().Grow(1)
			f.Button(hitSFTPWindowDownload, m.SFTPWindow.Download).Small().Outline().Disabled(!w.canTransfer(false))
			f.Button(hitSFTPWindowUpload, m.SFTPWindow.Upload).Small().Disabled(!w.canTransfer(true))
		})
		body = f.Slot().Grow(1)
		if len(w.view.transferRows) > 0 {
			queue = f.Slot().Height(min(202, max(86, size.Height*.25))).Shrink(0)
		}
		status := w.view.status
		if w.view.errorText != "" {
			status = w.view.errorText
		}
		if !w.connected() {
			status = m.SFTPWindow.Disconnected
		}
		if status == "" {
			status = m.SFTPWindow.DragHint
		}
		f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
	})
	f.End()
	w.ui.split = components.BeginResizable(canvas, theme, hitSFTPWindowSplit, body.Rect(), w.ui.fraction).LimitsPx(220, 220)
	panels := w.ui.split.Paint(m.SFTPWindow.Title)
	w.paintPane(canvas, panels.First, true)
	w.paintPane(canvas, panels.Second, false)
	if len(w.view.transferRows) > 0 {
		w.paintQueue(canvas, queue.Rect())
	}
	if w.view.dialog != sftpDialogNone {
		w.owner.paintSFTPDialog(canvas, ui.Rect{Width: size.Width, Height: size.Height}, w.view)
	}
	if w.closeConfirm {
		components.BeginAlertDialog(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height}, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowCloseConfirm, CancelID: hitSFTPWindowCloseCancel, InitialFocusID: hitSFTPWindowCloseCancel, ActionVariant: components.ButtonDestructive}).Paint(m.SFTPWindow.CloseTitle, m.SFTPWindow.CloseDescription, m.SFTPWindow.CloseTransfers, m.Cancel)
	}
	if w.ui.menuOpen {
		w.paintMenu(canvas)
	}
	if w.drag.active {
		w.paintDrag(canvas)
	}
	w.notices.paint(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height})
	w.ui.tooltip.Paint(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height}, components.TooltipOptions{Layer: tooltipLayer, SideOffset: tooltipGap}, components.HoverTooltipDelay)
}

func (w *sftpWindow) canTransfer(local bool) bool {
	if !w.connected() || w.view.worker == nil || w.view.busy || w.pendingLocal != "" || w.view.dialog != sftpDialogNone || w.closeConfirm || w.view.path == "" || w.view.lfolder.path == "" {
		return false
	}
	for _, row := range w.folder(local).rows {
		if row.selected {
			return true
		}
	}
	return false
}

func (w *sftpWindow) paintPane(canvas *ui.Canvas, bounds ui.Rect, local bool) {
	side := sideIndex(local)
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	var address, list flex.Box
	compact := bounds.Height < 230
	var buttons [3]sftpIconButton
	buttonCount := 0
	f := w.ui.panes[side].Begin(canvas, theme, bounds)
	icon := func(id ui.ID, glyph uint8, hint string, disabled bool) {
		buttons[buttonCount] = sftpIconButton{box: f.Slot().Size(28, 28), id: id, icon: glyph, hint: hint, disabled: disabled, style: iconButtonQuiet}
		buttonCount++
	}
	f.Column().Fill().Pad(8).Gap(6).Children(func() {
		f.Row().Height(28).Gap(4).Items(flex.Center).Children(func() {
			title := m.SFTPWindow.Remote
			if local {
				title = m.SFTPWindow.Local
			}
			f.Text(title).Bold().Color(theme.Foreground).Ellipsis().Grow(1)
			icon(sftpWindowUp[side], iconArrowUp, m.SFTPUp, folder.path == "" || !local && (w.view.busy || !w.connected()))
			icon(sftpWindowRefresh[side], iconRefresh, m.SFTPRefresh, !local && (w.view.busy || !w.connected()))
			icon(sftpWindowNewFolder[side], iconFolderPlus, m.SFTPNewDirectory, local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected()))
		})
		address = f.Slot().Height(32)
		if !compact {
			f.Row().Height(20).Gap(8).PadX(8).Items(flex.Center).Children(func() {
				f.Text(m.SFTPWindow.Name).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Grow(1)
				f.Text(m.SFTPWindow.Size).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Width(62).TextEnd()
				if bounds.Width >= 420 {
					f.Text(m.SFTPWindow.Modified).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Width(118)
				}
			})
		}
		list = f.Slot().Grow(1)
		count := 0
		for _, row := range folder.rows {
			if row.selected {
				count++
			}
		}
		status := fmt.Sprintf(m.SFTPItemsFmt, len(folder.rows))
		if count > 0 {
			status += " · " + fmt.Sprintf(m.SFTPWindow.SelectedFmt, count)
		}
		if !compact {
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
		}
	})
	f.End()
	w.owner.paintSFTPIconButtons(canvas, buttons[:buttonCount])
	w.ui.list[side] = list.Rect()
	value := w.paths[side].String()
	components.PaintInput(canvas, theme, address.Rect(), components.InputSnapshot{ID: sftpWindowPath[side], Label: m.SFTPWindow.Path, Value: value, Hint: folder.path, Focused: canvas.Focused() == sftpWindowPath[side], PlatformEditing: canvas.Focused() == sftpWindowPath[side]})
	rowsBounds := list.Rect()
	canvas.RoundedBorder(rowsBounds, 6, 1, theme.Border, theme.Background)
	folder.visibleRows = max(1, int(rowsBounds.Height/sftpRowHeight))
	area := components.BeginScrollArea(canvas, theme, rowsBounds, ui.Size{Width: rowsBounds.Width, Height: float32(len(folder.rows)) * sftpRowHeight}).Offset(0, w.ui.offset[side]).VerticalScrollbar(sftpWindowBar[side], m.SFTPFiles, "")
	geometry := area.BeginContent()
	w.ui.bars[side] = geometry
	w.ui.offset[side] = geometry.OffsetY
	folder.first = int(geometry.OffsetY / sftpRowHeight)
	last := min(len(folder.rows), folder.first+int(math.Ceil(float64(rowsBounds.Height/sftpRowHeight)))+1)
	r := w.ui.rows[side].Begin(canvas, theme, ui.Rect{X: rowsBounds.X + 2, Y: rowsBounds.Y - float32(math.Mod(float64(geometry.OffsetY), float64(sftpRowHeight))), Width: max(rowsBounds.Width-14, 0), Height: float32(last-folder.first) * sftpRowHeight})
	r.Column().FillWidth().Children(func() {
		for index := folder.first; index < last; index++ {
			w.paintRow(r, &folder.rows[index], sftpWindowRows[side].At(index), bounds.Width >= 420)
		}
	})
	r.End()
	if len(folder.rows) == 0 {
		label := m.SFTPEmpty
		if !local && w.view.busy {
			label = m.SFTPLoading
		}
		canvas.LabelCenter(label, rowsBounds, ui.TextStyle{Size: theme.Metrics.FontSizeM, Color: theme.MutedForeground})
	}
	area.EndContent()
}

func (w *sftpWindow) paintRow(f *flex.Frame, row *sftpRow, id ui.ID, showDate bool) {
	pal, canvas := w.owner.pal, f.Canvas()
	background := pal.workspace
	if row.selected {
		background = pal.accentSoft
	} else if canvas.Hovered() == id {
		background = pal.surfaceHot
	}
	color := pal.text
	if row.entry.LinkBroken {
		color = pal.danger
	}
	tint := ui.Color{Alpha: 1}
	if row.mono {
		tint = pal.muted
	}
	box := f.Row().Height(sftpRowHeight).Shrink(0).Gap(8).PadX(8).Items(flex.Center).Background(background).Radius(4).HitWith(id, row.entry.Name, ui.HitOptions{Role: ui.RoleButton, Selected: row.selected, Hint: row.entry.Name})
	if canvas.FocusVisible() == id {
		box.Border(1, pal.accent)
	}
	box.Children(func() {
		if row.icon != nil {
			f.Icon(*row.icon).Color(tint)
		}
		f.Text(row.entry.Name).Color(color).Ellipsis().Grow(1)
		f.Text(row.cells[2]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(62).TextEnd()
		if showDate {
			f.Text(row.cells[3]).FontSize(metrics.FontSizeS).Color(pal.muted).Width(118)
		}
	})
}

func (w *sftpWindow) paintQueue(canvas *ui.Canvas, bounds ui.Rect) {
	theme, m := w.owner.Theme(), w.owner.messages
	canvas.RoundedBorder(bounds, 6, 1, theme.Border, theme.Muted)
	canvas.Label(m.SFTPTransferQueue, ui.Rect{X: bounds.X + 8, Y: bounds.Y + 4, Width: bounds.Width - 16, Height: 20}, ui.TextStyle{Size: theme.Metrics.FontSizeS, Weight: ui.WeightSemibold, Color: theme.Foreground})
	w.ui.queueBounds = ui.Rect{X: bounds.X + 4, Y: bounds.Y + 24, Width: max(bounds.Width-8, 0), Height: max(bounds.Height-28, 0)}
	area := components.BeginScrollArea(canvas, theme, w.ui.queueBounds, ui.Size{Width: w.ui.queueBounds.Width, Height: float32(len(w.view.transferRows)) * 42}).Offset(0, w.ui.queueOffset).VerticalScrollbar(hitSFTPWindowQueueBar, m.SFTPTransferQueue, "")
	w.ui.queueBar = area.BeginContent()
	w.ui.queueOffset = w.ui.queueBar.OffsetY
	first := int(w.ui.queueOffset / 42)
	last := min(len(w.view.transferRows), first+int(math.Ceil(float64(w.ui.queueBounds.Height/42)))+1)
	for index := first; index < last; index++ {
		row := &w.view.transferRows[index]
		top := w.ui.queueBounds.Y + float32(index)*42 - w.ui.queueOffset
		canvas.HitWithOptions(sftpWindowQueueRows.At(index), ui.Rect{X: bounds.X + 4, Y: top, Width: max(bounds.Width-20, 0), Height: 40}, row.title, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Hint: row.snapshot.Source + " → " + row.snapshot.Destination})
		status := row.status + " · " + row.snapshot.Destination
		if row.snapshot.Operation == sftpclient.OperationDownload {
			status = "↓ " + status
		} else {
			status = "↑ " + status
		}
		textBounds := ui.Rect{X: bounds.X + 8, Y: top, Width: max(bounds.Width-230, 20), Height: 18}
		canvas.Label(row.title, textBounds, ui.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.Foreground})
		canvas.Label(status, ui.Rect{X: textBounds.X, Y: top + 18, Width: textBounds.Width, Height: 18}, ui.TextStyle{Size: theme.Metrics.FontSizeS, Color: theme.MutedForeground})
		actions := components.BeginButtonRow(canvas, theme, ui.Rect{X: bounds.X + bounds.Width - 228, Y: top + 2, Width: 208, Height: 30}).Size(components.ButtonSizeSmall)
		switch row.snapshot.State {
		case sftpclient.TransferQueued, sftpclient.TransferRunning, sftpclient.TransferPaused:
			label := m.Tools.TransferPause
			if row.snapshot.State == sftpclient.TransferPaused {
				label = m.Tools.TransferResume
			}
			actions.Paint(components.ButtonAction{ID: hitSFTPTransferPauseList.At(index), Label: label, Variant: components.ButtonGhost}, components.ButtonAction{ID: hitSFTPTransferCancelList.At(index), Label: m.SFTPCancelTransfer, Variant: components.ButtonOutline})
		case sftpclient.TransferFailed, sftpclient.TransferCanceled:
			if row.snapshot.Retryable {
				actions.Paint(components.ButtonAction{ID: hitSFTPTransferRetryList.At(index), Label: m.SFTPRetryTransfer, Variant: components.ButtonOutline})
			}
		}
		if row.snapshot.BytesTotal > 0 {
			fraction := min(float32(row.snapshot.BytesDone)/float32(row.snapshot.BytesTotal), 1)
			canvas.Fill(ui.Rect{X: bounds.X + 8, Y: top + 39, Width: max(bounds.Width-26, 0) * fraction, Height: 2}, theme.Primary)
		}
	}
	area.EndContent()
}

func (w *sftpWindow) paintMenu(canvas *ui.Canvas) {
	m := w.owner.messages
	transfer := m.SFTPWindow.Download
	if w.activeLocal {
		transfer = m.SFTPWindow.Upload
	}
	menu := components.BeginContextMenu(canvas, w.owner.Theme(), ui.Rect{X: w.ui.menuAt.X, Y: w.ui.menuAt.Y, Width: 240, Height: 3*32 + 8}, components.ContextMenuOptions{Layer: popupLayer, Scope: hitSFTPWindowMenuDismiss, DismissID: hitSFTPWindowMenuDismiss, Viewport: ui.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height}})
	menu.AddItem(components.ContextMenuItem{ID: hitSFTPWindowMenuOpen, Label: m.SFTPOpen})
	menu.AddItem(components.ContextMenuItem{ID: hitSFTPWindowMenuTransfer, Label: transfer, Disabled: !w.canTransfer(w.activeLocal)})
	menu.AddItem(components.ContextMenuItem{ID: hitSFTPWindowMenuCopy, Label: m.SFTPWindow.CopyPath})
	menu.End()
}

func (w *sftpWindow) paintDrag(canvas *ui.Canvas) {
	local, target, row, valid := w.dropTarget(w.drag.at.X, w.drag.at.Y, w.drag.fromLocal)
	if valid {
		bounds := w.ui.list[sideIndex(local)]
		if row >= 0 {
			bounds.Y += float32(row)*sftpRowHeight - w.ui.offset[sideIndex(local)]
			bounds.Height = sftpRowHeight
		}
		canvas.Clip(w.ui.list[sideIndex(local)])
		canvas.RoundedBorder(bounds, 6, 2, w.owner.Theme().Primary, withAlpha(w.owner.Theme().Primary, .12).Over(w.owner.Theme().Background))
		canvas.Unclip()
	}
	label := w.owner.messages.SFTPWindow.DragHint
	if valid {
		label = w.owner.messages.SFTPWindow.DropDownload
		if w.drag.fromLocal {
			label = w.owner.messages.SFTPWindow.DropUpload
		}
		label += " " + target
		if len(w.drag.entries) > 0 {
			label += fmt.Sprintf(" · %d", len(w.drag.entries))
		}
	}
	canvas.PushLayer(tooltipLayer)
	bounds := ui.Rect{X: min(max(w.drag.at.X+12, 8), max(w.ui.size.Width-328, 8)), Y: min(max(w.drag.at.Y+16, 8), max(w.ui.size.Height-40, 8)), Width: min(320, max(w.ui.size.Width-16, 0)), Height: 32}
	canvas.Rounded(bounds, 6, w.owner.Theme().Foreground)
	canvas.Label(strings.TrimSpace(label), ui.Rect{X: bounds.X + 8, Y: bounds.Y, Width: bounds.Width - 16, Height: bounds.Height}, ui.TextStyle{Size: metrics.FontSizeS, Color: w.owner.Theme().Background})
	canvas.PopLayer()
}
