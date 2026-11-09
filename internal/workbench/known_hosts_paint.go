package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) paintKnownHostsWorkspace(canvas *gfx.Canvas, bounds gfx.Rect) {
	state.syncKnownHostsVisible()
	view, messages, pal := &state.knownHosts, state.messages, state.pal
	value := view.search.String()
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

	var search, content, emptyIconBox, pagination, refresh, retrust, remove ui.Box
	f := state.knownHostsLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().PadSides(12, 18, 0, 18).Children(func() {
		f.Column().MarginSides(4, 0, 12, 0).Gap(3).Children(func() {
			f.Text(messages.KnownHostsTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
			f.Paragraph(messages.KnownHostsDescription, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
		f.Row().Height(40).Gap(gap).Items(ui.Center).Children(func() {
			search = f.Slot().Grow(1).MaxWidth(470).MinWidth(searchLeast).Height(40)
			f.Spacer()
			refresh = f.Slot().Size(refreshWidth, toolbarHeight)
			retrust = f.Slot().Size(retrustWidth, toolbarHeight)
			remove = f.Slot().Size(deleteWidth, toolbarHeight)
		})
		if empty {
			emptyIconBox, _ = state.declareEmptyPageWithColor(f, bounds, emptyTitle, emptyDescription, "", emptyDescriptionColor)
		} else {
			content = f.Slot().Grow(1).MarginSides(14, 0, 0, 0)
		}
		f.Divider().Background(pal.line)
		// The footer's columns narrow with the card so the status keeps its room.
		countWidth := min(168, (bounds.Width-36)*0.30)
		selectionWidth := min(138, (bounds.Width-36)*0.22)
		f.Row().Height(49).Gap(12).Items(ui.Center).Children(func() {
			f.Text(view.countText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint).Width(countWidth).Ellipsis()
			f.Text(view.selectionText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.muted).Width(selectionWidth).Ellipsis()
			f.Text(view.status).FontSize(metrics.FontSizeS).Color(statusColor).Grow(1).Ellipsis()
			if paginated {
				pagination = f.Slot().Size(242, 30)
			}
		})
	})
	f.End()

	state.paintToolbarButton(canvas, hitKnownHostsRefresh, refresh.Rect().X, refresh.Rect().Y, refreshWidth, messages.KnownHostsRefresh, refreshIcon, false, true, condensed)
	state.paintToolbarButton(canvas, hitKnownHostsRetrust, retrust.Rect().X, retrust.Rect().Y, retrustWidth, messages.KnownHostsRetrust, retrustIcon, false, view.selected > 0, condensed)
	state.paintToolbarButton(canvas, hitKnownHostsDelete, remove.Rect().X, remove.Rect().Y, deleteWidth, messages.KnownHostsDelete, deleteIcon, false, view.selected > 0, condensed)
	components.PaintInputGroup(canvas, state.Theme(), search.Rect(), components.InputGroupSnapshot{
		ID: hitKnownHostsSearch, Label: messages.NavSearch, Value: value, Placeholder: messages.KnownHostsSearchPlaceholder,
		NativeEditing: canvas.Focused() == hitKnownHostsSearch && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: lucideIcons[iconSearch]},
	})

	if empty {
		state.paintEmptyPage(canvas, emptyIconBox, ui.Box{}, emptyIconKind, gfx.ID{}, "", false)
	} else {
		state.paintKnownHostsTable(canvas, content.Rect())
	}
	if paginated {
		bar := components.BeginPagination(canvas, state.Theme(), pagination.Rect())
		bar.AddPrevious(hitKnownHostsPrevious, messages.KnownHostsPrevious, view.page == 0)
		bar.AddPage(gfx.ID{}, view.pageText, true)
		pages := (len(view.visible) + knownHostsPageSize - 1) / knownHostsPageSize
		bar.AddNext(hitKnownHostsNext, messages.KnownHostsNext, view.page+1 >= pages)
	}
}

func (state *State) paintKnownHostsTable(canvas *gfx.Canvas, bounds gfx.Rect) {
	view, messages := &state.knownHosts, state.messages
	columns := [...]components.TableColumn{
		{Width: 42}, {Width: 46, Alignment: components.TableAlignRight}, {Width: 230},
		{Width: 102}, {Width: 142}, {Width: 300}, {Width: 0},
	}
	start, end := state.knownHostsPageRange()
	theme := state.Theme()
	rowHeight, headerHeight := components.TableRowHeight(theme), components.TableHeaderHeight(theme)
	checkSize := 16 * theme.Metrics.ControlHeight / 32

	// The checkbox of each row sits in the table's first column, below its header.
	var checks [knownHostsPageSize]ui.Box
	f := state.knownHostsTableLayout.Begin(canvas, state.Theme(), bounds)
	f.Column().Fill().PadSides(headerHeight, 0, 0, 0).Children(func() {
		for offset := range view.visible[start:end] {
			f.Row().Height(rowHeight).Shrink(0).Items(ui.Center).PadSides(0, 0, 0, 13).Children(func() {
				checks[offset] = f.Slot().Size(checkSize, checkSize)
			})
		}
	})
	f.End()

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
		if box := checks[offset].Rect(); box.Y < bounds.Y+bounds.Height {
			components.BeginCheckbox(canvas, state.Theme(), hitKnownHostCheckList.At(index), gfx.Rect{X: box.X, Y: box.Y}, record.selected).Label(record.selectLabel).Paint()
		}
	}
}

func (state *State) paintKnownHostsDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	view, messages := &state.knownHosts, state.messages
	title, confirm := messages.KnownHostsDeleteTitle, messages.KnownHostsDeleteConfirm
	if view.action == knownHostsActionRetrust {
		title, confirm = messages.KnownHostsRetrustTitle, messages.KnownHostsRetrustConfirm
	}
	body := float32(24)
	if view.dialogError != "" {
		body = 72
	}
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitKnownHostsDialogDismiss, CloseID: hitKnownHostsDialogClose, InitialFocusID: hitKnownHostsDialogCancel,
		ContentSize: gfx.Size{Width: 570}, FitBody: true, BodyHeight: body,
	})
	layout := dialog.Begin(title, view.dialogDescription)
	f := state.partLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Gap(8).Children(func() {
		f.Text(messages.KnownHostsVerifyWarning).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.muted).LineHeight(24)
		if view.dialogError != "" {
			f.Text(view.dialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).LineHeight(38)
		}
	})
	f.End()
	dialog.Actions(layout,
		components.ButtonAction{ID: hitKnownHostsDialogCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitKnownHostsDialogConfirm, Label: confirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}
