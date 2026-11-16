package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// knownHostsSectionUI is what the known-hosts page's declaration leaves for its
// paint.
type knownHostsSectionUI struct {
	search, content, pagination, refresh, retrust, remove, emptySlot ui.Box
	refreshWidth, retrustWidth, deleteWidth                          float32
	refreshIcon, retrustIcon, deleteIcon                             uint8
	condensed                                                        bool
	empty                                                            bool
	emptyIcon                                                        uint8
	// draw is the page hook that paints into these slots. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareKnownHostsWorkspace declares the known-hosts page in the workbench
// page: its header, its search row, its table or empty state, and its footer.
// The checks are slots over the table's first column.
func (state *State) declareKnownHostsWorkspace(c ui.C) {
	state.syncKnownHostsVisible()
	canvas := c.Canvas()
	view, messages, pal := &state.knownHosts, state.messages, state.pal
	bounds := state.catalogSectionBounds()
	view.checks = [knownHostsPageSize]ui.Box{}
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
	condensed := labeled > bounds.Width-36
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
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Column(c).MarginSides(4, 0, 12, 0).Gap(3).Children(func() {
			ui.Text(c, messages.KnownHostsTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
			ui.Paragraph(c, messages.KnownHostsDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
		ui.Row(c).Height(40).Gap(gap).Items(ui.Center).Children(func() {
			layout.search = ui.Slot(c).Grow(1).MaxWidth(470).MinWidth(searchLeast).Height(40)
			ui.Spacer(c)
			layout.refresh = state.declareButtonSized(c, hitKnownHostsRefresh, messages.KnownHostsRefresh, refreshIcon, false, false, true, toolbarFont, condensed).Size(refreshWidth, toolbarHeight)
			layout.retrust = state.declareButtonSized(c, hitKnownHostsRetrust, messages.KnownHostsRetrust, retrustIcon, false, false, view.selected > 0, toolbarFont, condensed).Size(retrustWidth, toolbarHeight)
			layout.remove = state.declareButtonSized(c, hitKnownHostsDelete, messages.KnownHostsDelete, deleteIcon, false, false, view.selected > 0, toolbarFont, condensed).Size(deleteWidth, toolbarHeight)
		})
		if empty {
			layout.emptySlot = state.reserveEmptyPageWithColor(c, bounds, emptyTitle, emptyDescription, "", emptyDescriptionColor, gfx.ID{}, 0, false)
		} else {
			layout.content = ui.Slot(c).Grow(1).MarginSides(14, 0, 0, 0)
			state.declareKnownHostsChecks(c, layout.content)
		}
		ui.Divider(c).Background(pal.line)
		// The footer's columns narrow with the card so the status keeps its room.
		countWidth := min(168, (bounds.Width-36)*0.30)
		selectionWidth := min(138, (bounds.Width-36)*0.22)
		ui.Row(c).Height(49).Gap(12).Items(ui.Center).Children(func() {
			ui.Text(c, view.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(countWidth).Ellipsis()
			ui.Text(c, view.selectionText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(selectionWidth).Ellipsis()
			ui.Text(c, view.status).FontSize(metrics.FontSizeS).Color(statusColor).Grow(1).Ellipsis()
			if paginated {
				layout.pagination = ui.Slot(c).Size(242, 30)
			}
		})
	})
	body.Clip().DrawOver(layout.draw)
}

// paintKnownHostsWorkspace paints the known-hosts page's buttons, search field,
// table and empty state into the slots its declaration left.
func (state *State) paintKnownHostsWorkspace(canvas *gfx.Canvas) {
	view, messages := &state.knownHosts, state.messages
	layout := &state.sectionBodies.knownHosts
	value := view.search.String()

	components.PaintInputGroup(canvas, state.Theme(), layout.search.Rect(), components.InputGroupSnapshot{
		ID: hitKnownHostsSearch, Label: messages.NavSearch, Value: value, Placeholder: messages.KnownHostsSearchPlaceholder,
		NativeEditing: canvas.Focused() == hitKnownHostsSearch && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})

	if layout.empty {
		state.drawReservedEmptyPage(canvas, layout.emptySlot, layout.emptyIcon)
	} else {
		state.paintKnownHostsTable(canvas, layout.content.Rect())
	}
	if layout.pagination.Valid() {
		bar := components.BeginPagination(canvas, state.Theme(), layout.pagination.Rect())
		bar.AddPrevious(hitKnownHostsPrevious, messages.KnownHostsPrevious, view.page == 0)
		bar.AddPage(gfx.ID{}, view.pageText, true)
		pages := (len(view.visible) + knownHostsPageSize - 1) / knownHostsPageSize
		bar.AddNext(hitKnownHostsNext, messages.KnownHostsNext, view.page+1 >= pages)
	}
}

// declareKnownHostsChecks declares the checkbox slots of the rows on the table's
// page: the first column under the header, over the content the table paints.
func (state *State) declareKnownHostsChecks(c ui.C, content ui.Box) {
	view := &state.knownHosts
	start, end := state.knownHostsPageRange()
	theme := state.Theme()
	rowHeight, headerHeight := components.TableRowHeight(theme), components.TableHeaderHeight(theme)
	checkSize := 16 * theme.Metrics.ControlHeight / 32
	content.Children(func() {
		ui.Column(c).Fill().PadSides(headerHeight, 0, 0, 0).Children(func() {
			for offset := range view.visible[start:end] {
				ui.Row(c).Height(rowHeight).Shrink(0).Items(ui.Center).PadSides(0, 0, 0, 13).Children(func() {
					view.checks[offset] = ui.Slot(c).Size(checkSize, checkSize)
				})
			}
		})
	})
}

func (state *State) paintKnownHostsTable(canvas *gfx.Canvas, bounds gfx.Rect) {
	view, messages := &state.knownHosts, state.messages
	columns := [...]components.TableColumn{
		{Width: 42}, {Width: 46, Alignment: components.TableAlignRight}, {Width: 230},
		{Width: 102}, {Width: 142}, {Width: 300}, {Width: 0},
	}
	start, end := state.knownHostsPageRange()
	theme := state.Theme()

	headings := [...]string{
		"", messages.KnownHostsColumnLine, messages.KnownHostsColumnHosts, messages.KnownHostsColumnFlags,
		messages.KnownHostsColumnType, messages.KnownHostsColumnFingerprint, messages.KnownHostsColumnComment,
	}
	components.FitTableHeadings(canvas, theme, columns[:], headings[:])
	table := components.BeginTable(canvas, theme, bounds, columns[:])
	table.Header(headings[:])
	for offset, index := range view.visible[start:end] {
		record := &view.rows[index]
		table.Row(hitKnownHostRowList.At(index), record.cells[:], record.selected)
		if box := view.checks[offset].Rect(); box.Y < bounds.Y+bounds.Height {
			components.BeginCheckbox(canvas, state.Theme(), hitKnownHostCheckList.At(index), gfx.Rect{X: box.X, Y: box.Y}, record.selected).Label(record.selectLabel).Paint()
		}
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
