package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// knownHostsSectionUI is what the known-hosts page's declaration leaves for its
// paint.
type knownHostsSectionUI struct {
	refresh, retrust, remove, emptySlot     ui.Box
	refreshWidth, retrustWidth, deleteWidth float32
	refreshIcon, retrustIcon, deleteIcon    uint8
	condensed                               bool
	empty                                   bool
	emptyIcon                               uint8
	// draw is the page hook that paints the empty state. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareKnownHostsWorkspace declares the known-hosts page in the workbench
// page: its header, its search row, its table or empty state, and its footer.
// The table's rows are a page list; its empty state is painted by the hook.
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
	paginated := len(view.visible) > knownHostsPageSize
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
			state.fitKnownHostsColumns(bounds.Width - 2*knownHostsSidePad)
			theme := state.Theme()
			headerHeight := components.TableHeaderHeight(theme)
			headerPad := theme.Metrics.GapItem
			ui.Column(c).Grow(1).Shrink(1).MarginSides(14, 0, 0, 0).Children(func() {
				ui.Row(c).Height(headerHeight).Shrink(0).FillWidth().Gap(0).Items(ui.Center).Children(func() {
					ui.Slot(c).Width(view.columns[0].Width).Height(headerHeight).Shrink(0)
					for column := 1; column < knownHostsColumns; column++ {
						width := view.columns[column].Width
						if width <= 0 {
							break
						}
						cell := ui.Text(c, view.headings[column]).Weight(gfx.WeightMedium).PadX(headerPad).Width(width).Shrink(0)
						if view.columns[column].Alignment == components.TableAlignRight {
							cell = cell.TextEnd()
						}
						cell.Ellipsis()
					}
					ui.Slot(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(theme.Border)
				})
				start, end := state.knownHostsPageRange()
				ui.List(c, &view.list, end-start, view.rowDecl).Grow(1).Shrink(1).MinHeight(0).FillWidth().
					Label(messages.KnownHostsTitle)
			})
		}
		ui.Divider(c).Background(pal.line)
		// The footer's columns narrow with the card so the status keeps its room.
		countWidth := min(168, (bounds.Width-2*knownHostsSidePad)*0.30)
		selectionWidth := min(138, (bounds.Width-2*knownHostsSidePad)*0.22)
		ui.Row(c).Height(49).Gap(12).Items(ui.Center).Children(func() {
			ui.Text(c, view.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(countWidth).Ellipsis()
			ui.Text(c, view.selectionText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(selectionWidth).Ellipsis()
			ui.Text(c, view.status).FontSize(metrics.FontSizeS).Color(statusColor).Grow(1).Ellipsis()
			if paginated {
				pages := (len(view.visible) + knownHostsPageSize - 1) / knownHostsPageSize
				// The items carry the application's identities, so the central
				// click handler keeps turning the pages as it did for the old
				// paint-time chooser.
				ui.Pagination(c, []ui.PaginationItem{
					{ID: hitKnownHostsPrevious, Label: messages.KnownHostsPrevious, Kind: ui.PaginationPrevious, Disabled: view.page == 0},
					{Label: view.pageText, Kind: ui.PaginationPage, Current: true},
					{ID: hitKnownHostsNext, Label: messages.KnownHostsNext, Kind: ui.PaginationNext, Disabled: view.page+1 >= pages},
				}).Size(242, 30).Shrink(0)
			}
		})
	})
	body.Clip().DrawOver(layout.draw)
}

// paintKnownHostsWorkspace paints the known-hosts page's empty state into the
// slot its declaration left. The table and the page chooser are declared by the
// page itself.
func (state *State) paintKnownHostsWorkspace(canvas *gfx.Canvas) {
	layout := &state.sectionBodies.knownHosts
	if layout.empty {
		state.drawReservedEmptyPage(canvas, layout.emptySlot, layout.emptyIcon)
	}
}

// declareKnownHostRow declares one row of the known-hosts table: its checkbox,
// then the record's cells at the table's column widths. A press on the row or on
// its checkbox is the central click handler's under their identities.
func (state *State) declareKnownHostRow(row ui.ListRow) {
	view := &state.knownHosts
	theme := state.Theme()
	start, end := state.knownHostsPageRange()
	at := start + row.Index
	if at >= end || at >= len(view.visible) {
		return
	}
	index := view.visible[at]
	record := &view.rows[index]
	height := components.TableRowHeight(theme)
	pad := theme.Metrics.GapItem
	c := row.C
	element := row.Element.Height(height).Shrink(0).FillWidth().Gap(0).Items(ui.Center)
	if record.selected {
		element = element.Background(theme.Muted)
	} else {
		// The table's row lightened under the pointer; the page List keeps it.
		element = element.
			Hover(ui.Look{}.Background(theme.Hover)).
			Pressed(ui.Look{}.Background(theme.Muted))
	}
	element.Children(func() {
		ui.Row(c).Width(view.columns[0].Width).Height(height).Shrink(0).Items(ui.Center).Children(func() {
			ui.Checkbox(c, state.pageFlag(record.selected), "").Hit(hitKnownHostCheckList.At(index), record.selectLabel)
		})
		for column := 1; column < knownHostsColumns; column++ {
			width := view.columns[column].Width
			if width <= 0 {
				break
			}
			cell := ui.Text(c, record.cells[column]).PadX(pad).Width(width).Shrink(0)
			if view.columns[column].Alignment == components.TableAlignRight {
				cell = cell.TextEnd()
			}
			cell.Ellipsis()
		}
		ui.Slot(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(theme.Border)
	})
	element.Hit(hitKnownHostRowList.At(index), record.rowLabel)
}

// fitKnownHostsColumns narrows the columns to the room the table has, as the
// canvas table does: each column keeps its width while it fits, and the ones
// after it take what is left, so a narrow page ellipsizes rather than overflows.
func (state *State) fitKnownHostsColumns(width float32) {
	remaining := max(width, 0)
	for index := range state.knownHosts.columns {
		column := &state.knownHosts.columns[index]
		columnWidth := column.Width
		if columnWidth <= 0 || index == knownHostsColumns-1 && columnWidth > remaining {
			columnWidth = remaining
		}
		columnWidth = min(columnWidth, remaining)
		column.Width = columnWidth
		remaining -= columnWidth
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
