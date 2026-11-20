package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

// remoteTableSortIconSize and remoteTableSortIconGap are components.Table's
// sort glyph geometry, so the page's header keeps the arrow where the canvas
// table painted it.
const (
	remoteTableSortIconSize = float32(14)
	remoteTableSortIconGap  = float32(4)
)

// The sort arrows are components.Table's own glyphs, kept here because the page
// paints the header itself and Viva does not export them.
var (
	remoteTableUnsortedIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(8, 9), gfx.LineTo(12, 5), gfx.LineTo(16, 9),
		gfx.MoveTo(8, 15), gfx.LineTo(12, 19), gfx.LineTo(16, 15),
	}}
	remoteTableAscendingIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 19), gfx.LineTo(12, 5),
		gfx.MoveTo(7, 10), gfx.LineTo(12, 5), gfx.LineTo(17, 10),
	}}
	remoteTableDescendingIcon = gfx.Icon{ViewBox: 24, Stroke: 2, Cap: gfx.CapRound, Join: gfx.JoinRound, Commands: []gfx.PathCommand{
		gfx.MoveTo(12, 5), gfx.LineTo(12, 19),
		gfx.MoveTo(7, 14), gfx.LineTo(12, 19), gfx.LineTo(17, 14),
	}}
)

// ensureRemoteTableHooks makes the table's page declaration functions once:
// method values made in each paint allocate.
func (state *State) ensureRemoteTableHooks() {
	view := &state.remote
	if view.rowDecl != nil {
		return
	}
	view.rowDecl = state.declareRemoteTableRow
	view.rowMenu = state.declareRemoteTableMenu
	view.headerDraw = state.paintRemoteTableHeader
	view.borderDraw = state.paintRemoteTableBorder
	view.menuRow = -1
}

// prepareRemoteTable fits the current tab's columns and headings for a table
// width, so the header paint and the row cells use the same sizes.
func (state *State) prepareRemoteTable(c ui.C, tab remoteTab, width float32) {
	view, theme := &state.remote, state.Theme()
	view.paintTab = tab
	view.tableRowHeight = components.TableRowHeight(theme)
	var columns []components.TableColumn
	var headings []string
	if tab == remoteProcesses {
		columns, headings = state.processTableSchema(width)
	} else {
		columns, headings = state.objectTableSchema(tab)
	}
	view.tableCount = len(headings)
	order := view.tableSort()
	for index := 0; index < len(headings); index++ {
		cell := components.TableHeader{
			ID: remoteTableHeaderID(tab, index), Label: headings[index],
			Disabled: view.loading, FocusableWhenDisabled: true,
			Description: state.messages.Tools.RemoteSortAction,
		}
		if order.Direction != components.TableSortNone && order.Column == index {
			cell.Sort = order.Direction
			if order.Direction == components.TableSortAscending {
				cell.Description = state.messages.Tools.RemoteSortAscending
			} else {
				cell.Description = state.messages.Tools.RemoteSortDescending
			}
		}
		view.tableHeaderCells[index] = cell
		view.tableHeadings[index] = headings[index]
		if index < len(columns) {
			view.tableColumns[index] = columns[index]
		} else {
			view.tableColumns[index] = components.TableColumn{}
		}
	}
	components.FitSortableTableHeadings(c.Canvas(), theme, view.tableColumns[:len(headings)], view.tableHeaderCells[:len(headings)])
}

// declareRemoteToolsTable declares the current tab's sortable header and page
// list. fallbackWidth is the room the table has before the page has laid it
// out: the first paint has no bounds to measure yet.
func (state *State) declareRemoteToolsTable(c ui.C, tab remoteTab, fallbackWidth float32) {
	state.ensureRemoteTableHooks()
	view, theme := &state.remote, state.Theme()
	width := fallbackWidth
	if bounds := c.Bounds(); bounds.Width > 0 {
		width = bounds.Width
	}
	state.prepareRemoteTable(c, tab, width)
	headerHeight := components.TableHeaderHeight(theme)
	label := state.remoteTabLabel(tab)
	placeholder := state.remoteTablePlaceholder(tab)
	showHeader := !state.remoteTableHeaderHidden(tab)
	ui.Column(c).Grow(1).Shrink(1).MinHeight(headerHeight+view.tableRowHeight).
		MarginSides(remoteToolsTableMargin, 0, 0, 0).Radius(radius.control).
		Background(state.pal.workspace).Children(func() {
		// The old control drew its header and rows inside its one-pixel frame,
		// so the page keeps the same inset.
		ui.Column(c).Fill().PadSides(1, 1, 0, 1).Children(func() {
			if showHeader {
				ui.Stack(c).Height(headerHeight).Shrink(0).FillWidth().Children(func() {
					ui.Slot(c).Fill().Draw(view.headerDraw)
					ui.Row(c).Fill().Children(func() {
						state.declareRemoteTableHeaderHits(c)
					})
				})
			}
			if placeholder {
				state.declareRemoteTablePlaceholder(c, tab)
				return
			}
			state.syncRemoteTableChoice(tab)
			list := ui.List(c, &view.lists[tab], len(view.rows), view.rowDecl).
				Grow(1).Shrink(1).FillWidth().AlwaysScrollbars().ScrollbarSize(10).Label(label)
			if scroll := list.ScrollState(); scroll != nil {
				if view.resetScroll {
					scroll.ScrollTo(0)
					view.resetScroll = false
				}
				view.tableScroll = scroll
			}
			state.takeRemoteTableChoice(c, tab)
		})
		ui.Slot(c).Absolute().Left(0).Right(0).Top(0).Bottom(0).Draw(view.borderDraw)
	})
}

// paintRemoteTableBorder paints the rounded frame over the table's edge. It is
// an overlay so the frame stays above the rows; the surface beneath them is the
// container's background, which the page paints before its children.
func (state *State) paintRemoteTableBorder(d *ui.DrawContext) {
	d.Canvas.RoundedOutline(d.Rect, radius.control, 1, state.pal.line)
}

// remoteTablePlaceholder reports whether the tab shows a placeholder instead of
// rows: nothing to list, Docker missing, or a first read still loading.
func (state *State) remoteTablePlaceholder(tab remoteTab) bool {
	view := &state.remote
	if len(view.rows) == 0 {
		return true
	}
	return tab != remoteProcesses && view.noDocker && view.collectedAt.IsZero()
}

// remoteTableHeaderHidden reports whether the header is withheld: the old table
// drew no header while Docker was missing or the first read was loading.
func (state *State) remoteTableHeaderHidden(tab remoteTab) bool {
	view := &state.remote
	return tab != remoteProcesses && (view.noDocker && view.collectedAt.IsZero() ||
		view.loading && len(view.rows) == 0)
}

// declareRemoteTableHeaderHits declares one hit per sortable column over the
// painted header row, keeping the columns' explicit widths.
func (state *State) declareRemoteTableHeaderHits(c ui.C) {
	view, headerHeight := &state.remote, components.TableHeaderHeight(state.Theme())
	for index := 0; index < view.tableCount; index++ {
		cell := view.tableHeaderCells[index]
		column := view.tableColumns[index]
		options := gfx.HitOptions{
			Role: gfx.RoleButton, Disabled: cell.Disabled,
			FocusableWhenDisabled: cell.FocusableWhenDisabled, Description: cell.Description,
		}
		hit := ui.Slot(c).Height(headerHeight).Shrink(0).HitWith(cell.ID, cell.Label, options)
		if column.Width > 0 {
			hit.Width(column.Width)
		} else {
			hit.Grow(1)
		}
		if c.Canvas().Clicked(cell.ID) {
			id := cell.ID
			c.After(func() { state.clickRemoteTableSort(id) })
		}
	}
}

// syncRemoteTableChoice keeps the list's chosen row on the page's selected key,
// which survives a sort, a search and a refresh.
func (state *State) syncRemoteTableChoice(tab remoteTab) {
	view := &state.remote
	if view.lists[tab].Changed() {
		// A key moved the list's row since the last paint: keep that move and
		// let takeRemoteTableChoice write the page's selection from it.
		return
	}
	if index := view.selectedRowIndex(); index >= 0 {
		view.lists[tab].Select(index)
	}
}

// takeRemoteTableChoice turns the list's click, Enter or double click into the
// page's selection, and opens details on Enter (processes) or a double click.
func (state *State) takeRemoteTableChoice(c ui.C, tab remoteTab) {
	view := &state.remote
	list := &view.lists[tab]
	opened := -1
	if index, ok := list.Submitted(); ok {
		opened = index
	}
	if list.Changed() {
		if index := list.Selected(); index >= 0 && index < len(view.rows) {
			view.selected = view.rows[index].key
		}
	}
	if opened < 0 || opened >= len(view.rows) {
		return
	}
	view.selected = view.rows[opened].key
	open := tab == remoteProcesses
	if info, clicked := c.Canvas().ClickInfo(); clicked {
		open = info.Clicks >= 2
	}
	if !open {
		return
	}
	key := view.selected
	c.After(func() {
		switch tab {
		case remoteProcesses:
			if process, found := state.remoteProcessByKey(key); found {
				state.openRemoteProcessDetails(process)
			}
		case remoteContainers:
			if container, found := state.selectedContainer(); found {
				state.openRemoteContainerDetails(container)
			}
		}
	})
}

// declareRemoteTableRow declares one visible row: its cells at the fitted
// column widths, its selection or hover surface, and its context menu.
func (state *State) declareRemoteTableRow(row ui.ListRow) {
	view, theme := &state.remote, state.Theme()
	if row.Index < 0 || row.Index >= len(view.rows) {
		return
	}
	record := &view.rows[row.Index]
	height := view.tableRowHeight
	element := row.Element.Height(height).Shrink(0).FillWidth().Items(ui.Center)
	selected := row.Selected()
	if selected {
		element = element.Background(theme.Muted)
	} else {
		element = element.Hover(ui.Look{}.Background(theme.Hover))
	}
	element.Children(func() {
		for column := 0; column < view.tableCount; column++ {
			spec := view.tableColumns[column]
			cell := ui.Text(row.C, record.cells[column]).Height(height).Shrink(0).
				FontSize(theme.Metrics.FontSizeM).Color(theme.Foreground).Ellipsis().LineHeight(height).
				PadX(theme.Metrics.GapItem)
			if spec.Alignment == components.TableAlignRight {
				cell = cell.TextEnd()
			}
			if spec.Width > 0 {
				cell.Width(spec.Width)
			} else {
				cell.Grow(1).Basis(0)
			}
		}
		if row.Index+1 < len(view.rows) {
			ui.Slot(row.C).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(theme.Border)
		}
	})
	if event, ok := row.C.Canvas().PointerOn(row.ID); ok &&
		event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonSecondary {
		view.selected = record.key
		view.menuRow, view.rowMenuOpen = row.Index, true
		view.rowMenuAt = gfx.Point{X: event.X, Y: event.Y}
	}
	if view.rowMenuOpen && view.menuRow == row.Index {
		ui.ContextMenuAt(row.C, row.Element, &view.rowMenuOpen, view.rowMenuAt, view.rowMenu)
	}
}

// declareRemoteTableMenu declares the open row's context menu: the row the
// secondary press selected, and the actions the footer offers for it.
func (state *State) declareRemoteTableMenu(menu *ui.MenuItems) {
	view := &state.remote
	if view.paintTab == remoteProcesses {
		if view.processMenu != nil {
			view.processMenu(menu)
		}
		return
	}
	if view.containerMenu != nil {
		view.containerMenu(menu)
	}
}

// paintRemoteTableHeader paints the sortable header row: its hover surface,
// labels and sort arrows, at components.Table's geometry.
func (state *State) paintRemoteTableHeader(d *ui.DrawContext) {
	view, theme := &state.remote, *d.Theme
	canvas, bounds := d.Canvas, d.Rect
	x, remaining := bounds.X, bounds.Width
	size, pad := theme.Metrics.FontSizeM, theme.Metrics.GapItem
	for column := 0; column < view.tableCount; column++ {
		spec := view.tableColumns[column]
		width := spec.Width
		if width <= 0 {
			width = max(remaining, 0)
		}
		cell := gfx.Rect{X: x, Y: bounds.Y, Width: width, Height: bounds.Height}
		header := view.tableHeaderCells[column]
		if canvas.Hovered() == header.ID || canvas.Pressed() == header.ID {
			canvas.Fill(cell, theme.Hover)
		}
		if !header.Disabled && canvas.FocusVisible() == header.ID {
			canvas.RoundedBorder(cell.Inset(gfx.All(1)), theme.Metrics.RadiusControl, 1, theme.Ring, gfx.Color{})
		}
		color := theme.Foreground
		if header.Disabled {
			color.Alpha *= 0.5
		}
		textBounds := cell.Inset(gfx.Insets{Left: pad, Right: pad})
		label := canvas.FitLine(header.Label, size, gfx.WeightMedium, max(textBounds.Width-remoteTableSortIconSize-remoteTableSortIconGap, 0))
		labelWidth := min(canvas.TextWidthWeight(label, size, gfx.WeightMedium), max(textBounds.Width-remoteTableSortIconSize-remoteTableSortIconGap, 0))
		groupWidth := min(labelWidth+remoteTableSortIconGap+remoteTableSortIconSize, max(textBounds.Width, 0))
		switch spec.Alignment {
		case components.TableAlignRight:
			textBounds.X += max(textBounds.Width-groupWidth, 0)
		case components.TableAlignCenter:
			textBounds.X += max(textBounds.Width-groupWidth, 0) / 2
		}
		textBounds.Width = labelWidth
		canvas.Clip(cell)
		canvas.Label(label, textBounds, gfx.TextStyle{Size: size, Weight: gfx.WeightMedium, Color: color})
		icon := remoteTableUnsortedIcon
		switch header.Sort {
		case components.TableSortAscending:
			icon = remoteTableAscendingIcon
		case components.TableSortDescending:
			icon = remoteTableDescendingIcon
		}
		canvas.DrawIcon(icon, gfx.Rect{
			X:     textBounds.X + labelWidth + remoteTableSortIconGap,
			Y:     bounds.Y + (bounds.Height-remoteTableSortIconSize)/2,
			Width: remoteTableSortIconSize, Height: remoteTableSortIconSize,
		}, color)
		canvas.Unclip()
		x += width
		remaining -= width
	}
}

// declareRemoteTablePlaceholder declares the empty, loading or Docker-missing
// state in the page instead of a list.
func (state *State) declareRemoteTablePlaceholder(c ui.C, tab remoteTab) {
	view, m, pal := &state.remote, state.messages.Tools, state.pal
	theme := state.Theme()
	if tab != remoteProcesses {
		if view.noDocker && view.collectedAt.IsZero() {
			// components.Empty's bare-icon state: a 32px glyph in the
			// foreground, the large medium title, and the body-size muted
			// description in a 384px column.
			titleSize := theme.Metrics.FontSizeM + 5
			ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).Items(ui.Center).Children(func() {
				ui.Icon(c, icons.LinuxDocker).Size(32, 32).Color(theme.Foreground).
					MarginBottom(theme.Metrics.GapItem * 2)
				ui.Text(c, m.RemoteDockerMissing).Height(titleSize + 10).LineHeight(titleSize + 10).FontSize(titleSize).
					Weight(gfx.WeightMedium).Color(theme.Foreground)
				ui.Paragraph(c, m.RemoteDockerMissingHint, 6).FontSize(theme.Metrics.FontSizeM).
					LineHeight(theme.Metrics.FontSizeM * 1.625).MaxWidth(384).
					Color(theme.MutedForeground).TextCenter().MarginTop(theme.Metrics.GapItem)
			})
			return
		}
		if view.loading && len(view.rows) == 0 {
			// The spinner sat 24 above the center and the label started at it.
			ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).Items(ui.Center).Children(func() {
				ui.Spinner(c).Size(16, 16)
				ui.Slot(c).Height(theme.Metrics.GapItem * 2).Shrink(0)
				ui.Text(c, m.RemoteLoading).Height(24).LineHeight(24).FontSize(theme.Metrics.FontSizeM).Color(pal.muted)
			})
			return
		}
	}
	message, color := m.RemoteLoading, pal.muted
	if !view.loading {
		message = view.status
		if message == "" {
			message = state.remoteTabLabel(tab)
		}
	}
	ui.Column(c).Grow(1).FillWidth().Justify(ui.Center).Items(ui.Center).Children(func() {
		ui.Text(c, message).FontSize(theme.Metrics.FontSizeM).Color(color)
	})
}

// selectedRowIndex is the index of the page's selected key, or -1.
func (view *remoteToolsView) selectedRowIndex() int {
	if view.selected == "" {
		return -1
	}
	for index := range view.rows {
		if view.rows[index].key == view.selected {
			return index
		}
	}
	return -1
}
