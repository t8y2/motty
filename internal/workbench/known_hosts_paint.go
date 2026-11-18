package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// knownHostsSectionUI is what the known-hosts page's declaration leaves for its
// paint.
type knownHostsSectionUI struct {
	tableBox, refresh, retrust, remove, emptySlot ui.Box
	refreshWidth, retrustWidth, deleteWidth       float32
	refreshIcon, retrustIcon, deleteIcon          uint8
	condensed                                     bool
	empty                                         bool
	emptyIcon                                     uint8
	// draw is the page hook that paints into these slots. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareKnownHostsWorkspace declares the known-hosts page in the workbench
// page: its header, its search row, its table or empty state, and its footer.
// The checks are painted over the table's first column in the draw pass.
func (state *State) declareKnownHostsWorkspace(c ui.C) {
	state.syncKnownHostsVisible()
	canvas := c.Canvas()
	view, messages, pal := &state.knownHosts, state.messages, state.pal
	bounds := state.catalogSectionBounds()
	// The search box keeps the least width it works at and the buttons keep their
	// labels while the row holds both; a narrower card shows their icons alone,
	// whose tooltips name them.
	const (
		searchLeast = float32(140)
		gap         = float32(8)
	)
	labeled := searchLeast + gap + toolbarButtonWidth(canvas, messages.KnownHostsRefresh, false) +
		gap + toolbarButtonWidth(canvas, messages.KnownHostsRetrust, false) +
		gap + toolbarButtonWidth(canvas, messages.KnownHostsDelete, false)
	condensed := labeled > bounds.Width-2*knownHostsSidePad
	refreshWidth, refreshIcon := toolbarButtonLayout(canvas, messages.KnownHostsRefresh, 0, iconRefresh, condensed)
	retrustWidth, retrustIcon := toolbarButtonLayout(canvas, messages.KnownHostsRetrust, 0, iconCircleCheck, condensed)
	deleteWidth, deleteIcon := toolbarButtonLayout(canvas, messages.KnownHostsDelete, 0, iconTrash, condensed)
	statusColor := pal.muted
	if view.errorText != "" {
		statusColor = pal.danger
	}
	pageSize := state.knownHostsPageSize()
	paginated := len(view.visible) > pageSize
	emptyTitle, emptyDescription, emptyIconKind := "", "", uint8(iconKnown)
	emptyDescriptionColor := pal.muted
	empty := view.errorText != "" || len(view.rows) == 0 || len(view.visible) == 0
	switch {
	case view.errorText != "":
		emptyTitle, emptyDescription, emptyIconKind = messages.KnownHostsError, view.errorText, iconHelp
		emptyDescriptionColor = pal.danger
	case len(view.rows) == 0:
		emptyTitle, emptyDescription = messages.KnownHostsEmpty, messages.KnownHostsEmptySub
	case len(view.visible) == 0:
		emptyTitle, emptyDescription, emptyIconKind = messages.KnownHostsNoMatches, messages.KnownHostsNoMatchesSub, iconSearch
	}
	layout := &state.sectionBodies.knownHosts
	draw := layout.draw
	*layout = knownHostsSectionUI{
		refreshWidth: refreshWidth, retrustWidth: retrustWidth, deleteWidth: deleteWidth,
		refreshIcon: refreshIcon, retrustIcon: retrustIcon, deleteIcon: deleteIcon,
		condensed: condensed, empty: empty, emptyIcon: emptyIconKind,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintKnownHostsWorkspace(d.Canvas) }
	}
	layout.draw = draw
	body := ui.Column(c).Fill().PadSides(12, knownHostsSidePad, 0, knownHostsSidePad)
	body.Children(func() {
		ui.Column(c).MarginSides(4, 0, 12, 0).Gap(3).Children(func() {
			ui.Text(c, messages.KnownHostsTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
			ui.Paragraph(c, messages.KnownHostsDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
		ui.Row(c).Height(40).Gap(gap).Items(ui.Center).Children(func() {
			if !view.searchBound {
				view.searchField.Bind(&view.search)
				view.searchBound = true
			}
			ui.TextFieldID(c, hitKnownHostsSearch, &view.searchField, messages.NavSearch, messages.KnownHostsSearchPlaceholder).
				Icon(lucideIcons[iconSearch]).Grow(1).MaxWidth(470).MinWidth(searchLeast).Height(40)
			ui.Spacer(c)
			layout.refresh = state.declareButtonSized(c, hitKnownHostsRefresh, messages.KnownHostsRefresh, refreshIcon, false, false, true, toolbarFont, condensed).Size(refreshWidth, toolbarHeight)
			layout.retrust = state.declareButtonSized(c, hitKnownHostsRetrust, messages.KnownHostsRetrust, retrustIcon, false, false, view.selected > 0, toolbarFont, condensed).Size(retrustWidth, toolbarHeight)
			layout.remove = state.declareButtonSized(c, hitKnownHostsDelete, messages.KnownHostsDelete, deleteIcon, false, false, view.selected > 0, toolbarFont, condensed).Size(deleteWidth, toolbarHeight)
		})
		if empty {
			layout.emptySlot = state.reserveEmptyPageWithColor(c, bounds, emptyTitle, emptyDescription, "", emptyDescriptionColor, gfx.ID{}, 0, false)
		} else {
			view.columns = [knownHostsColumns]components.TableColumn{
				{Width: 42}, {Width: 46, Alignment: components.TableAlignRight}, {Width: 230},
				{Width: 102}, {Width: 142}, {Width: 300}, {Width: 0},
			}
			view.headings = [knownHostsColumns]string{
				"", messages.KnownHostsColumnLine, messages.KnownHostsColumnHosts, messages.KnownHostsColumnFlags,
				messages.KnownHostsColumnType, messages.KnownHostsColumnFingerprint, messages.KnownHostsColumnComment,
			}
			components.FitTableHeadings(canvas, state.Theme(), view.columns[:], view.headings[:])
			layout.tableBox = ui.Table(c, &view.table, ui.TableSpec{
				Rows:            hitKnownHostRowList,
				Columns:         view.columns[:],
				Headers:         view.headings[:],
				Source:          &view.tableSource,
				Scrollbar:       hitKnownHostsScrollThumb,
				ScrollbarLabel:  messages.KnownHostsListScroll,
				ScrollbarSize:   catalogBarGutter,
				ScrollbarGutter: catalogBarGutter,
			}).Grow(1).Shrink(1).MarginSides(14, 0, 0, 0)
			if row, ok := layout.tableBox.RowClicked(); ok && state.modal == modalNone {
				view.table.Select(row)
			}
		}
		ui.Divider(c).Background(pal.line)
		// The footer's columns narrow with the card so the status, page-size switcher and pagination keep their room.
		countWidth := min(136, (bounds.Width-2*knownHostsSidePad)*0.22)
		selectionWidth := min(116, (bounds.Width-2*knownHostsSidePad)*0.18)
		ui.Row(c).Height(49).Gap(10).Items(ui.Center).Children(func() {
			ui.Text(c, view.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(countWidth).Ellipsis()
			ui.Text(c, view.selectionText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(selectionWidth).Ellipsis()
			ui.Text(c, view.status).FontSize(metrics.FontSizeS).Color(statusColor).Grow(1).Ellipsis()
			if len(view.rows) > 0 {
				ui.Row(c).Height(30).Gap(6).Items(ui.Center).Shrink(0).Children(func() {
					ui.Text(c, messages.KnownHostsPerPage).FontSize(metrics.FontSizeS).Color(pal.muted).Shrink(0)
					if ui.Choice(c, &view.pageSize, knownHostsPageSizes[:], view.pageSizeLabelFn).Height(30).Shrink(0).Changed() {
						newSize := view.pageSize
						view.pageSize = 0
						state.setKnownHostsPageSize(newSize)
						pageSize = state.knownHostsPageSize()
						paginated = len(view.visible) > pageSize
					}
				})
			}
			if paginated {
				pages := (len(view.visible) + pageSize - 1) / pageSize
				pageLabelW := canvas.TextWidthWeight(view.pageText, metrics.FontSizeM, gfx.WeightMedium) + 16
				ui.Row(c).Height(30).Shrink(0).Gap(6).Items(ui.Center).Children(func() {
					ui.Button(c, messages.KnownHostsPrevious).Hit(hitKnownHostsPrevious, messages.KnownHostsPrevious).
						Outline().Small().Disabled(view.page == 0).Shrink(0)
					ui.Text(c, view.pageText).FontSize(metrics.FontSizeM).Weight(gfx.WeightMedium).Color(pal.text).
						Width(pageLabelW).TextCenter().Shrink(0)
					ui.Button(c, messages.KnownHostsNext).Hit(hitKnownHostsNext, messages.KnownHostsNext).
						Outline().Small().Disabled(view.page+1 >= pages).Shrink(0)
				})
			}
		})
	})
	body.Clip().DrawOver(layout.draw)
}

// paintKnownHostsWorkspace paints the known-hosts page's row checkboxes and
// empty state into the slots its declaration left.
func (state *State) paintKnownHostsWorkspace(canvas *gfx.Canvas) {
	layout := &state.sectionBodies.knownHosts

	if layout.empty {
		state.drawReservedEmptyPage(canvas, layout.emptySlot, layout.emptyIcon)
	} else {
		state.paintKnownHostsCheckboxes(canvas, layout.tableBox.Rect())
	}
}

// paintKnownHostsCheckboxes paints the visible rows' checkboxes in the table's
// first column, clipped to the scrollable table body so rows never overlap the
// header or footer.
func (state *State) paintKnownHostsCheckboxes(canvas *gfx.Canvas, tableRect gfx.Rect) {
	if tableRect.Width <= 0 || tableRect.Height <= 0 {
		return
	}
	view := &state.knownHosts
	start, end := state.knownHostsPageRange()
	count := end - start
	if count <= 0 {
		return
	}
	theme := state.Theme()
	rowHeight := components.TableRowHeight(theme)
	headerHeight := components.TableHeaderHeight(theme)
	body := gfx.Rect{
		X:      tableRect.X,
		Y:      tableRect.Y + headerHeight,
		Width:  tableRect.Width,
		Height: max(tableRect.Height-headerHeight, 0),
	}
	if body.Height <= 0 {
		return
	}
	if body.Height+0.5 < float32(count)*rowHeight {
		body.Width = max(body.Width-float32(catalogBarGutter), 0)
	}
	side := components.CheckboxExtent(theme)
	offset := view.table.Scroll.Offset
	first := max(0, int(offset/rowHeight))
	last := min(count, int((offset+body.Height)/rowHeight)+1)

	canvas.Clip(body)
	defer canvas.Unclip()
	for row := first; row < last; row++ {
		index := view.visible[start+row]
		record := &view.rows[index]
		rowY := body.Y + float32(row)*rowHeight - offset
		rowRect := gfx.Rect{X: body.X, Y: rowY, Width: body.Width, Height: rowHeight}
		view.rowCells = components.TableCellBounds(view.columns[:], rowRect, view.rowCells[:0])
		if len(view.rowCells) == 0 {
			continue
		}
		c0 := view.rowCells[0]
		boxRect := gfx.Rect{
			X:      c0.X + (c0.Width-side)/2,
			Y:      c0.Y + (c0.Height-side)/2,
			Width:  side,
			Height: side,
		}
		components.BeginCheckbox(canvas, theme, hitKnownHostCheckList.At(index), boxRect, record.selected).Label(record.selectLabel).Paint()
	}
}

func (state *State) declareKnownHostsDialog(c ui.C) {
	view, messages := &state.knownHosts, state.messages
	if view.action == knownHostsActionNone {
		return
	}
	title, confirm := messages.KnownHostsDeleteTitle, messages.KnownHostsDeleteConfirm
	if view.action == knownHostsActionRetrust {
		title, confirm = messages.KnownHostsRetrustTitle, messages.KnownHostsRetrustConfirm
	}
	body := float32(24)
	if view.dialogError != "" {
		body = 72
	}
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 570, view.dialogDescription, body)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: view.dialogDescription,
		Width: 570, Height: height, Layer: dialogLayer,
		Scope: dialogScope, DismissID: hitKnownHostsDialogDismiss, CloseID: hitKnownHostsDialogClose,
		InitialFocusID: hitKnownHostsDialogCancel,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().Gap(8).Children(func() {
			ui.Text(c, messages.KnownHostsVerifyWarning).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).LineHeight(24)
			if view.dialogError != "" {
				ui.Text(c, view.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(38)
			}
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitKnownHostsDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitKnownHostsDialogConfirm, Label: confirm, Variant: components.ButtonDestructive},
		)
	})
}
