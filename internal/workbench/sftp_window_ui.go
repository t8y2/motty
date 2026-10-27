package workbench

import (
	"fmt"
	"math"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
)

var sftpWindowHits = tree.Feature("sftp-window")
var (
	hitSFTPDetach                = tree.ID(sftpHits, "detach")
	sftpWindowSides              = tree.Sub(sftpWindowHits, tree.Name("sides"))
	sftpWindowLocal              = tree.Sub(sftpWindowSides, tree.Name("local"))
	sftpWindowRemote             = tree.Sub(sftpWindowSides, tree.Name("remote"))
	sftpWindowRows               = [2]ui.Scope{tree.Sub(sftpWindowLocal, tree.Name("rows")), tree.Sub(sftpWindowRemote, tree.Name("rows"))}
	sftpWindowPath               = [2]ui.ID{tree.ID(sftpWindowLocal, "path"), tree.ID(sftpWindowRemote, "path")}
	sftpWindowList               = [2]ui.ID{tree.ID(sftpWindowLocal, "list"), tree.ID(sftpWindowRemote, "list")}
	sftpWindowSortHits           = [2]ui.Scope{tree.Sub(sftpWindowLocal, tree.Name("sort")), tree.Sub(sftpWindowRemote, tree.Name("sort"))}
	sftpWindowActions            = [2]ui.Scope{tree.Sub(sftpWindowLocal, tree.Name("actions")), tree.Sub(sftpWindowRemote, tree.Name("actions"))}
	sftpWindowMenuActions        = tree.Sub(sftpWindowHits, tree.Name("menu-actions"))
	sftpWindowUp                 = [2]ui.ID{tree.ID(sftpWindowLocal, "up"), tree.ID(sftpWindowRemote, "up")}
	sftpWindowRefresh            = [2]ui.ID{tree.ID(sftpWindowLocal, "refresh"), tree.ID(sftpWindowRemote, "refresh")}
	sftpWindowNewFolder          = [2]ui.ID{tree.ID(sftpWindowLocal, "new-folder"), tree.ID(sftpWindowRemote, "new-folder")}
	sftpWindowBar                = [2]ui.ID{tree.ID(sftpWindowLocal, "scroll"), tree.ID(sftpWindowRemote, "scroll")}
	hitSFTPWindowSplit           = tree.ID(sftpWindowHits, "split")
	hitSFTPWindowUpload          = tree.ID(sftpWindowHits, "upload")
	hitSFTPWindowDownload        = tree.ID(sftpWindowHits, "download")
	hitSFTPWindowCloseCancel     = tree.ID(sftpWindowHits, "close-cancel")
	hitSFTPWindowCloseConfirm    = tree.ID(sftpWindowHits, "close-confirm")
	hitSFTPWindowExternalReplace = tree.ID(sftpWindowHits, "external-replace")
	hitSFTPWindowExternalKeep    = tree.ID(sftpWindowHits, "external-keep")
	hitSFTPWindowMenuDismiss     = tree.ID(sftpWindowHits, "menu-dismiss")
	hitSFTPWindowMenuCopy        = tree.ID(sftpWindowHits, "menu-copy")
	hitSFTPWindowQueueBar        = tree.ID(sftpWindowHits, "queue-scroll")
	hitSFTPWindowConnectCancel   = tree.ID(sftpWindowHits, "connect-cancel")
	hitSFTPWindowConnectRetry    = tree.ID(sftpWindowHits, "connect-retry")
	hitSFTPWindowAuthenticate    = tree.ID(sftpWindowHits, "authenticate")
	sftpWindowQueueRows          = tree.Sub(sftpWindowHits, tree.Name("queue-rows"))
)

type sftpWindowUI struct {
	frame          flex.View
	panes          [2]flex.View
	rows           [2]flex.View
	size           ui.Size
	list           [2]ui.Rect
	offset         [2]float32
	bars           [2]components.ScrollAreaGeometry
	barDrag        int // 1: local, 2: remote, 3: queue; zero when idle
	barGrab        float32
	fraction       float32
	split          components.Resizable
	splitting      bool
	tooltip        components.HoverTooltip
	menuOpen       bool
	menuAt         ui.Point
	queueBounds    ui.Rect
	queueOffset    float32
	queueBar       components.ScrollAreaGeometry
	sort           [2]sftpWindowSort
	menuScroll     components.MenuScroll
	bookmarkScroll components.MenuScroll
	bookmarkAnchor ui.Rect
	selection      [2]sftpWindowSelection
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
	if w.view.worker == nil {
		w.paintConnecting(canvas, size)
		return
	}
	var body, queue flex.Box
	f := w.ui.frame.Begin(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height})
	f.Column().Fill().Pad(12).Gap(8).Children(func() {
		body = f.Slot().Grow(1)
		if w.queueCount() > 0 {
			queue = f.Slot().Height(min(244, max(108, size.Height*.28))).Shrink(0)
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
	if w.queueCount() > 0 {
		w.paintQueue(canvas, queue.Rect())
	} else {
		w.ui.queueBounds = ui.Rect{}
		w.ui.queueOffset = 0
	}
	if w.view.bookmarksOpen {
		w.paintBookmarks(canvas)
	}
	if w.view.dialog != sftpDialogNone {
		w.owner.paintSFTPDialog(canvas, ui.Rect{Width: size.Width, Height: size.Height}, w.view)
	}
	if w.closeConfirm {
		components.BeginAlertDialog(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height}, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowCloseConfirm, CancelID: hitSFTPWindowCloseCancel, InitialFocusID: hitSFTPWindowCloseCancel, ActionVariant: components.ButtonDestructive}).Paint(m.SFTPWindow.CloseTitle, m.SFTPWindow.CloseDescription, m.SFTPWindow.CloseTransfers, m.Cancel)
	}
	if w.externalConflict != 0 && !w.closeConfirm && w.view.dialog == sftpDialogNone {
		if edit := w.owner.editByID(w.externalConflict); edit != nil {
			m := w.owner.messages.Tools
			components.BeginAlertDialog(canvas, theme, ui.Rect{Width: size.Width, Height: size.Height}, components.AlertDialogOptions{Layer: dialogLayer + 1, Scope: dialogScope, ActionID: hitSFTPWindowExternalReplace, CancelID: hitSFTPWindowExternalKeep, InitialFocusID: hitSFTPWindowExternalKeep, ActionVariant: components.ButtonDestructive}).Paint(m.ExtEditConflictTitle, fmt.Sprintf(m.ExtEditConflictDescriptionFmt, edit.name), m.ExtEditReplace, w.owner.messages.Cancel)
		}
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
	if !w.connected() || w.view.worker == nil || w.view.busy || w.pendingLocal != "" || w.view.dialog != sftpDialogNone || w.closeConfirm || w.externalConflict != 0 || w.view.path == "" || w.view.lfolder.path == "" {
		return false
	}
	return w.selection(local).count > 0
}

func (w *sftpWindow) paintPane(canvas *ui.Canvas, bounds ui.Rect, local bool) {
	side := sideIndex(local)
	folder := w.folder(local)
	theme, m := w.owner.Theme(), w.owner.messages
	var address, list flex.Box
	compact := bounds.Height < 230
	var buttons [18]sftpIconButton
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
			if !local {
				icon(hitSFTPWindowUpload, iconUpload, m.SFTPWindow.Upload, !w.canTransfer(true))
				icon(hitSFTPWindowDownload, iconDownload, m.SFTPWindow.Download, !w.canTransfer(false))
			}
			icon(sftpWindowUp[side], iconArrowUp, m.SFTPUp, folder.path == "" || !local && (w.view.busy || !w.connected()))
			icon(sftpWindowRefresh[side], iconRefresh, m.SFTPRefresh, !local && (w.view.busy || !w.connected()))
			icon(sftpWindowNewFolder[side], iconFolderPlus, m.SFTPNewDirectory, local && w.pendingLocal != "" || !local && (w.view.busy || !w.connected()))
			if !local {
				icon(hitSFTPNewLink, iconLink, m.Tools.TipNewLink, w.view.busy || !w.connected())
			}
		})
		f.Row().Height(32).Gap(2).Items(flex.Center).Children(func() {
			address = f.Slot().Height(32).Grow(1)
			if !local {
				icon(hitSFTPBookmarkList, iconChevronDown, m.Tools.Bookmarks, false)
				star, hint := uint8(iconStar), m.Tools.BookmarkAdd
				if w.owner.bookmarked(&w.session, folder.path) {
					star, hint = iconStarSolid, m.Tools.BookmarkRemove
				}
				icon(hitSFTPBookmarkToggle, star, hint, w.view.busy || !w.connected())
			}
		})
		{
			f.Row().Height(20).Gap(8).PadX(8).Items(flex.Center).Children(func() {
				for column, label := range []string{m.SFTPWindow.Name, m.SFTPWindow.Size, m.SFTPWindow.Modified} {
					if w.ui.sort[side].column == column {
						if w.ui.sort[side].descending {
							label += " ↓"
						} else {
							label += " ↑"
						}
					}
					box := f.Text(label).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().HitWith(sftpWindowSortHits[side].At(column), label, ui.HitOptions{Role: ui.RoleButton})
					if column == 0 {
						box.Grow(1)
					} else if column == 1 {
						box.Width(62).TextEnd()
					} else {
						box.Width(w.dateWidth(bounds.Width))
					}
				}
			})
		}
		list = f.Slot().Grow(1)
		selection := w.selection(local)
		count := selection.count
		status := fmt.Sprintf(m.SFTPItemsFmt, len(folder.rows))
		if count > 0 {
			status += " · " + fmt.Sprintf(m.SFTPWindow.SelectedFmt, count)
		}
		if !compact {
			f.Text(status).FontSize(theme.Metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(20)
		}
		if !compact {
			f.Row().Height(28).Gap(2).Items(flex.Center).Children(func() {
				if count == 0 {
					return
				}
				for _, action := range w.selectionActions(local) {
					icon(sftpWindowActions[side].At(action.index), action.glyph, action.label, w.view.busy && !local || !local && !w.connected())
					if sftpWindowActionIDs[action.index] == hitSFTPDelete {
						buttons[buttonCount-1].style = iconButtonDanger
					}
				}
			})
			if bounds.Height >= 320 {
				if count == 1 {
					f.Text(w.owner.sftpDetailsLine(w.view, folder.rows[selection.index].entry)).FontSize(metrics.FontSizeS).Color(theme.MutedForeground).Ellipsis().LineHeight(18)
				} else {
					f.Slot().Height(18)
				}
			}
		}
	})
	f.End()
	w.owner.paintSFTPIconButtons(canvas, buttons[:buttonCount])
	w.ui.list[side] = list.Rect()
	if !local {
		w.ui.bookmarkAnchor = address.Rect()
	}
	value := folder.pathInput().String()
	components.PaintInput(canvas, theme, address.Rect(), components.InputSnapshot{ID: sftpWindowPath[side], Label: m.SFTPWindow.Path, Value: value, Hint: folder.path, Focused: canvas.Focused() == sftpWindowPath[side], PlatformEditing: canvas.Focused() == sftpWindowPath[side]})
	rowsBounds := list.Rect()
	canvas.RoundedBorder(rowsBounds, 6, 1, theme.Border, theme.Background)
	folder.visibleRows = max(1, int(rowsBounds.Height/sftpRowHeight))
	area := components.BeginScrollArea(canvas, theme, rowsBounds, ui.Size{Width: rowsBounds.Width, Height: float32(len(folder.rows)) * sftpRowHeight}).Offset(0, w.ui.offset[side]).Viewport(sftpWindowList[side], m.SFTPFiles).VerticalScrollbar(sftpWindowBar[side], m.SFTPFiles, "")
	geometry := area.BeginContent()
	w.ui.bars[side] = geometry
	w.ui.offset[side] = geometry.OffsetY
	folder.first = int(geometry.OffsetY / sftpRowHeight)
	last := min(len(folder.rows), folder.first+int(math.Ceil(float64(rowsBounds.Height/sftpRowHeight)))+1)
	r := w.ui.rows[side].Begin(canvas, theme, ui.Rect{X: rowsBounds.X + 2, Y: rowsBounds.Y - float32(math.Mod(float64(geometry.OffsetY), float64(sftpRowHeight))), Width: max(rowsBounds.Width-14, 0), Height: float32(last-folder.first) * sftpRowHeight})
	r.Column().FillWidth().Children(func() {
		for index := folder.first; index < last; index++ {
			w.paintRow(r, &folder.rows[index], sftpWindowRows[side].At(index), bounds.Width)
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

func (w *sftpWindow) paintRow(f *flex.Frame, row *sftpRow, id ui.ID, paneWidth float32) {
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
		date := row.cells[3]
		if paneWidth < 420 && len(date) >= 16 {
			date = date[5:10]
		}
		f.Text(date).FontSize(metrics.FontSizeS).Color(pal.muted).Width(w.dateWidth(paneWidth))
	})
}

func (w *sftpWindow) dateWidth(width float32) float32 {
	if width < 420 {
		return 76
	}
	return 118
}

func (w *sftpWindow) queueCount() int {
	count := 0
	for i := range w.owner.transfers.entries {
		e := &w.owner.transfers.entries[i]
		if e.owner == w.view && !e.removed {
			count++
		}
	}
	return count
}

func (w *sftpWindow) paintQueue(canvas *ui.Canvas, bounds ui.Rect) {
	theme, m := w.owner.Theme(), w.owner.messages
	canvas.Label(m.SFTPTransferQueue, ui.Rect{X: bounds.X + 4, Y: bounds.Y, Width: bounds.Width - 8, Height: 20}, ui.TextStyle{Size: theme.Metrics.FontSizeS, Weight: ui.WeightSemibold, Color: theme.Foreground})
	w.ui.queueBounds = ui.Rect{X: bounds.X, Y: bounds.Y + 24, Width: bounds.Width, Height: max(bounds.Height-24, 0)}
	area := components.BeginScrollArea(canvas, theme, w.ui.queueBounds, ui.Size{Height: float32(w.queueCount()) * transferCardHeight}).Offset(0, w.ui.queueOffset).VerticalScrollbar(hitSFTPWindowQueueBar, m.SFTPTransferQueue, "")
	w.ui.queueBar = area.BeginContent()
	w.ui.queueOffset = w.ui.queueBar.OffsetY
	row := 0
	for pass := 0; pass < 2; pass++ {
		for i := len(w.owner.transfers.entries) - 1; i >= 0; i-- {
			e := &w.owner.transfers.entries[i]
			if e.owner != w.view || e.removed || (pass == 0) != transferActive(e.snapshot) {
				continue
			}
			box := ui.Rect{X: bounds.X, Y: w.ui.queueBounds.Y + float32(row)*transferCardHeight - w.ui.queueOffset, Width: bounds.Width - 12, Height: transferCardHeight - 8}
			index := row
			row++
			if box.Y+box.Height < w.ui.queueBounds.Y || box.Y > w.ui.queueBounds.Y+w.ui.queueBounds.Height {
				continue
			}
			canvas.HitWithOptions(sftpWindowQueueRows.At(index), box, e.title, ui.HitOptions{Role: ui.RoleGroup, SkipFocus: true, Hint: e.paths})
			w.owner.paintTransferCard(canvas, box, e)
		}
	}
	area.EndContent()
}

func (w *sftpWindow) paintMenu(canvas *ui.Canvas) {
	rows := w.owner.sftpMenuRows(&w.session, w.view)
	m := w.owner.messages
	if selectedSFTPCount(w.view) == 0 {
		rows = menuRows{}
	}
	rows.separate()
	rows.add(components.ContextMenuItem{ID: hitSFTPNewDirectory, Label: m.SFTPNewDirectory, Icon: lucideIcons[iconFolderPlus]})
	if !w.activeLocal {
		rows.add(components.ContextMenuItem{ID: hitSFTPUpload, Label: m.SFTPUpload, Icon: lucideIcons[iconUpload]})
		rows.add(components.ContextMenuItem{ID: hitSFTPUploadDirectory, Label: m.SFTPUploadDirectory, Icon: lucideIcons[iconFolderUp]})
		rows.add(components.ContextMenuItem{ID: hitSFTPNewLink, Label: m.Tools.TipNewLink, Icon: lucideIcons[iconLink]})
	}
	if selectedSFTPCount(w.view) > 0 {
		rows.add(components.ContextMenuItem{ID: hitSFTPWindowMenuCopy, Label: m.SFTPWindow.CopyPath})
	}
	for i := range rows.entries[:rows.count] {
		item := &rows.entries[i].item
		action := item.ID
		if mapped, ok := sftpMenuAction(action); ok {
			action = mapped
		}
		if index := sftpWindowActionIndex(action); index >= 0 {
			item.ID = sftpWindowMenuActions.At(index)
			item.Disabled = w.pendingLocal != "" || !w.activeLocal && (!w.connected() || w.view.busy)
		}
	}
	theme := w.owner.contextMenuTheme()
	width, height := rows.size(canvas, theme)
	width = min(width, 480)
	menu := components.BeginContextMenu(canvas, theme, ui.Rect{X: w.ui.menuAt.X, Y: w.ui.menuAt.Y, Width: width, Height: height}, components.ContextMenuOptions{Layer: popupLayer, Scope: hitSFTPWindowMenuDismiss, DismissID: hitSFTPWindowMenuDismiss, Viewport: ui.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height}, Scroll: &w.ui.menuScroll, RowHeight: contextMenuRow})
	rows.paint(&menu, nil)
	menu.End()
}

func (w *sftpWindow) paintBookmarks(canvas *ui.Canvas) {
	marks := w.owner.bookmarksFor(&w.session)
	anchor := w.ui.bookmarkAnchor
	menu := components.BeginContextMenu(canvas, w.owner.Theme(), ui.Rect{X: anchor.X, Y: anchor.Y + anchor.Height + 4, Width: max(anchor.Width, 220), Height: float32(max(len(marks), 1))*30 + 8}, components.ContextMenuOptions{Layer: popupLayer, Scope: hitSFTPBookmarkDismiss, DismissID: hitSFTPBookmarkDismiss, Viewport: ui.Rect{Width: w.ui.size.Width, Height: w.ui.size.Height}, Scroll: &w.ui.bookmarkScroll, RowHeight: 30})
	if len(marks) == 0 {
		menu.AddItem(components.ContextMenuItem{Label: w.owner.messages.Tools.BookmarksEmpty, Disabled: true})
	}
	for i, mark := range marks {
		_, slot := menu.AddItemSlot(components.ContextMenuItem{ID: hitSFTPBookmarkItemList.At(i), Label: mark, Icon: lucideIcons[iconFolderOpen], TrailingWidth: 24})
		menu.BeginContent()
		w.owner.paintIconButton(canvas, hitSFTPBookmarkRemoveList.At(i), iconClose, slot, w.owner.messages.Tools.BookmarkRemove, false, iconButtonQuiet, w.owner.pal.surface)
		menu.EndContent()
	}
	menu.End()
}

func (w *sftpWindow) paintDrag(canvas *ui.Canvas) {
	local, target, row, valid := w.dropTarget(w.drag.at.X, w.drag.at.Y, w.drag.fromLocal)
	if !valid {
		return
	}
	bounds := w.ui.list[sideIndex(local)]
	if row >= 0 {
		bounds.Y += float32(row)*sftpRowHeight - w.ui.offset[sideIndex(local)]
		bounds.Height = sftpRowHeight
	}
	canvas.Clip(w.ui.list[sideIndex(local)])
	defer canvas.Unclip()
	label, glyph := w.owner.messages.SFTPWindow.DropDownloadHint, uint8(iconDownload)
	if w.drag.fromLocal {
		label, glyph = w.owner.messages.SFTPWindow.DropUploadHint, iconUpload
	}
	w.owner.paintSFTPTransferDrop(canvas, bounds, label, target, glyph)
}
