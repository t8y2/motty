package workbench

import (
	"strconv"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

const remoteToolsModalWidth = float32(940)

// declareRemoteToolsModal declares the in-window remote tools surface as a
// dialog of the workbench page: its tabs, search, refresh controls, tables,
// status and actions are page elements, and its menus and details dialogs are
// overlays of the same page.
func (state *State) declareRemoteToolsModal(c ui.C) {
	if state.modal != modalRemoteTools {
		return
	}
	if _, ok := state.remoteSession(); !ok {
		state.closeRemoteToolsSurface()
		return
	}
	state.ensureRemoteToolsModalControls()
	state.remote.ctx = c
	m, theme := state.messages.Tools, state.Theme()
	pageSize := state.remotePageSize()
	tableHeight := components.TableHeaderHeight(theme) + float32(pageSize)*components.TableRowHeight(theme)
	bodyHeight := 52 + tableHeight + 34 + remoteSearchHeight
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), theme, viewport, remoteToolsModalWidth, "", bodyHeight)
	// The header's title keeps the dialog named for assistive technology; the
	// tools window names itself in the native titlebar instead.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.RemoteTitle, Width: remoteToolsModalWidth, Height: height,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitRemoteDismiss, CloseID: hitRemoteClose,
		InitialFocusID: hitRemoteSearch,
	})
	dialog.Body(state.remote.body)
	dialog.Footer(state.remote.footer)
	if dialog.Dismissed() {
		c.After(func() { state.closeRemoteToolsSurface() })
	}
	// The details are dialogs of the same page, above the tools surface.
	state.declareRemoteProcessDetailsDialog(c, state.remote.detailsAct)
	state.declareRemoteContainerDetailsDialog(c)
}

// ensureRemoteToolsModalControls makes the controls' handlers once, not in each
// paint: a method value made in a paint allocates.
func (state *State) ensureRemoteToolsModalControls() {
	view := &state.remote
	if view.tableHandlers.Pointer != nil {
		return
	}
	view.tableHandlers = ui.Handlers{
		Role: gfx.RoleGroup, Key: state.remoteTableControlKey, Pointer: state.remoteTableControlPointer,
	}
	view.refreshHandlers = ui.Handlers{
		Role: gfx.RoleButton, Key: state.remoteRefreshControlKey, Pointer: state.remoteRefreshControlPointer,
	}
	view.autoHandlers = ui.Handlers{
		Role: gfx.RoleButton, Key: state.remoteAutoControlKey, Pointer: state.remoteAutoControlPointer,
	}
	view.tabLabel = state.remoteTabLabel
	view.autoMenu = state.declareRemoteAutoMenu
	view.autoDraw = state.drawRemoteAutoControl
	view.processDraw = state.drawRemoteProcessTableControl
	view.objectDraw = state.drawRemoteObjectTableControl
	view.body = func() { state.declareRemoteToolsBody(view.ctx) }
	view.footer = func() { state.declareRemoteToolActions(view.ctx) }
	view.detailsAct = state.clickRemoteTools
	view.processMenu = func(menu *ui.MenuItems) {
		after := func(fn func()) { view.ctx.After(fn) }
		state.declareRemoteProcessMenuItems(menu, after, state.clickRemoteTools)
	}
}

// declareRemoteToolsBody declares the tools dialog's body: the tab switch and
// refresh controls, the search line, the table of the tab the paint started
// with, and the status and paging line under it.
func (state *State) declareRemoteToolsBody(c ui.C) {
	view, theme := &state.remote, state.Theme()
	tab := view.tab
	pageSize := state.remotePageSize()
	tableHeight := components.TableHeaderHeight(theme) + float32(pageSize)*components.TableRowHeight(theme)
	stacked := c.Bounds().Width < 800
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(remoteToolbarHeight).Shrink(0).Gap(12).Children(func() {
			if ui.Choice(c, &view.tab, remoteTabChoices[:], view.tabLabel).Width(600).Shrink(1).FillHeight().Changed() {
				state.applyRemoteTab()
			}
			ui.Spacer(c)
			if !stacked {
				state.declareRemoteRefreshControls(c)
			}
		})
		if stacked {
			ui.Row(c).Height(remoteToolbarHeight).Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
				ui.Spacer(c)
				state.declareRemoteRefreshControls(c)
			})
		}
		state.declareRemoteToolsSearch(c)
		if tab == remoteProcesses {
			state.declareRemoteProcessTable(c)
		} else {
			ui.Control(c, state.remoteTabLabel(tab), &view.tableHandlers).
				Height(tableHeight).Shrink(0).MarginSides(4, 0, 0, 0).Draw(view.objectDraw)
		}
		state.declareRemoteToolsStatus(c, tab)
	})
}

// declareRemoteRefreshControls declares the manual refresh button, its
// auto-refresh menu and the sudo command switch.
func (state *State) declareRemoteRefreshControls(c ui.C) {
	m, view := state.messages.Tools, &state.remote
	refresh := ui.Button(c, m.RemoteRefresh).Icon(lucideIcons[iconRefresh]).IconOnly().Ghost().
		Size(40, remoteToolbarHeight).Shrink(0).Disabled(view.loading)
	if refresh.Clicked() {
		c.After(state.refreshRemoteToolsExplicit)
	}
	auto := ui.Control(c, state.remoteAutoAccessibleLabel(), &view.autoHandlers).
		Size(134, remoteToolbarHeight).Shrink(0).MarginLeft(8).Draw(view.autoDraw)
	ui.Menu(c, auto, &view.auto.open, view.autoMenu)
	if session, ok := state.remoteSession(); ok && session.host.System != sshconn.SystemWindows {
		label := state.messages.SudoCommandsEnable
		if session.sudoCommands {
			label = state.messages.SudoCommandsDisable
		}
		if ui.Button(c, label).Icon(lucideIcons[iconLock]).IconOnly().Ghost().
			Size(28, remoteToolbarHeight).Shrink(0).Clicked() {
			c.After(func() { state.toggleSudoCommands(session, true) })
		}
	}
}

// declareRemoteToolsSearch declares the search field of the active tab. The
// field keeps its own draft per tab, and the summary under it names the number
// of rows the query keeps.
func (state *State) declareRemoteToolsSearch(c ui.C) {
	m, pal, view := state.messages.Tools, state.pal, &state.remote
	search := &view.searches[view.tab]
	if !search.fieldBound {
		search.field.Bind(search.queryEditor())
		search.fieldBound = true
	}
	query := search.queryEditor().String()
	ui.Column(c).Height(remoteSearchHeight).Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
		field := ui.TextFieldID(c, hitRemoteSearch, &search.field, m.RemoteSearch, m.RemoteSearchPlaceholder).
			Height(32).FillWidth().Shrink(0).Icon(lucideIcons[iconSearch]).NavigationKeys()
		if query != "" {
			field = field.Action(lucideIcons[iconClose], m.RemoteSearchClear)
		}
		if field.Changed() {
			view.page = 0
			state.rebuildRemoteRows()
		}
		if field.ActionClicked() {
			state.clearRemoteSearch()
		}
		summary := view.searchSummary
		ui.Text(c, summary).FontSize(state.Theme().Metrics.FontSizeS).Color(pal.muted).
			Height(20).MarginTop(4).Ellipsis().
			HitWith(hitRemoteSearchScope, summary, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: summary})
	})
}

// declareRemoteProcessTable declares the process snapshot as one page Control:
// its control draws the table exactly as the component lays it out, and takes
// the row selection, the wheel and the process context menu.
func (state *State) declareRemoteProcessTable(c ui.C) {
	m, theme, view := state.messages.Tools, state.Theme(), &state.remote
	headerHeight := components.TableHeaderHeight(theme)
	ui.Column(c).Grow(1).Shrink(1).MinHeight(headerHeight+components.TableRowHeight(theme)).
		MarginSides(4, 0, 0, 0).Children(func() {
		table := ui.Control(c, m.RemoteTabProcesses, &view.tableHandlers).
			Grow(1).Shrink(1).Draw(view.processDraw)
		ui.ContextMenu(c, table, view.processMenu)
	})
}

// declareRemoteToolsStatus declares the line under the table: what the last
// read or action did, when the snapshot was collected, and the paging controls
// of the object tabs.
func (state *State) declareRemoteToolsStatus(c ui.C, tab remoteTab) {
	pal, view := state.pal, &state.remote
	pageSize := state.remotePageSize()
	pages := (len(view.rows) + pageSize - 1) / pageSize
	status := view.status
	missingDocker := view.noDocker && tab != remoteProcesses && view.collectedAt.IsZero()
	if view.loading && len(view.rows) == 0 && !missingDocker {
		status = ""
	}
	if missingDocker && !view.loading {
		status = ""
	}
	color := pal.muted
	if view.failed {
		color = pal.danger
	}
	ui.Row(c).Height(30).Shrink(0).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
		ui.Text(c, status).FontSize(state.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color).
			Height(24).Ellipsis().Grow(1)
		if view.snapshotStatus != "" {
			ui.Text(c, view.snapshotStatus).FontSize(state.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).
				Color(pal.muted).Width(360).Height(24).Shrink(1).Ellipsis().
				HitWith(hitRemoteSnapshotStatus, view.snapshotStatus, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: view.searchSummary})
		}
		if tab == remoteProcesses || len(view.rows) <= pageSize {
			return
		}
		if view.pageText == "" {
			view.pageText = strconv.Itoa(view.page+1) + "/" + strconv.Itoa(pages)
		}
		if ui.Button(c, state.messages.KnownHostsPrevious).Ghost().Small().Shrink(0).
			Disabled(view.page == 0).Clicked() {
			c.After(func() {
				if view.page > 0 {
					view.page--
					view.pageText = ""
				}
			})
		}
		ui.Text(c, view.pageText).FontSize(state.Theme().Metrics.FontSizeS).Color(pal.muted).
			Width(56).TextCenter().Shrink(0)
		if ui.Button(c, state.messages.KnownHostsNext).Ghost().Small().Shrink(0).
			Disabled(view.page+1 >= pages).Clicked() {
			c.After(func() {
				view.page++
				view.pageText = ""
			})
		}
	})
}

// declareRemoteToolActions declares the footer's action buttons.
func (state *State) declareRemoteToolActions(c ui.C) {
	actions, count := state.remoteToolActions()
	for index := 0; index < count; index++ {
		action := actions[index]
		button := ui.Button(c, action.Label).Shrink(0)
		switch action.Variant {
		case components.ButtonDestructive:
			button = button.Destructive()
		case components.ButtonSecondary:
			button = button.Secondary()
		default:
			button = button.Outline()
		}
		if action.Disabled {
			button = button.Disabled(true)
		}
		if button.Clicked() {
			id := action.ID
			c.After(func() { state.clickRemoteTools(id) })
		}
	}
}

// drawRemoteProcessTableControl paints the process snapshot inside its page
// control: the sortable header, the scrollbar, and the rows intersecting the
// viewport. The snapshot stays complete in memory.
func (state *State) drawRemoteProcessTableControl(d *ui.DrawContext) {
	view, theme, pal, m := &state.remote, *d.Theme, state.pal, state.messages.Tools
	bounds := d.Rect
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas := d.Canvas
	view.processTableBounds = bounds
	canvas.RoundedBorder(bounds, radius.control, 1, pal.line, pal.workspace)

	headerHeight, rowHeight := state.remoteProcessTableMetrics()
	headerBounds := gfx.Rect{X: bounds.X + 1, Y: bounds.Y + 1, Width: max(bounds.Width-2, 0), Height: headerHeight}
	bodyBounds := gfx.Rect{
		X: bounds.X + 1, Y: headerBounds.Bottom(),
		Width: max(bounds.Width-2, 0), Height: max(bounds.Height-headerHeight-1, 0),
	}
	columns, headings := state.processTableSchema(bounds.Width - 2)
	var headerCells [6]components.TableHeader
	order := view.tableSort()
	for index, heading := range headings {
		id := remoteTableHeaderID(remoteProcesses, index)
		headerCells[index] = components.TableHeader{
			ID: id, Label: heading, Disabled: view.loading, FocusableWhenDisabled: true,
			Description: m.RemoteSortAction,
		}
		if order.Direction != components.TableSortNone && order.Column == index {
			headerCells[index].Sort = order.Direction
			if order.Direction == components.TableSortAscending {
				headerCells[index].Description = m.RemoteSortAscending
			} else {
				headerCells[index].Description = m.RemoteSortDescending
			}
		}
		if canvas.Clicked(id) {
			canvas.AfterPaint(func() { state.clickRemoteTableSort(id) })
		}
	}
	components.FitSortableTableHeadings(canvas, theme, columns, headerCells[:len(headings)])
	header := components.BeginTable(canvas, theme, headerBounds, columns).
		HeaderHeight(headerHeight).
		RowHeight(rowHeight)
	header.SortableHeader(headerCells[:len(headings)])

	contentHeight := max(float32(len(view.rows))*rowHeight, bodyBounds.Height)
	firstVisible := min(len(view.rows), 1+int(view.processScroll/rowHeight))
	if view.processScrollFirst != firstVisible || view.processScrollCount != len(view.rows) {
		view.processScrollFirst, view.processScrollCount = firstVisible, len(view.rows)
		view.processScrollValue = strconv.Itoa(firstVisible) + "/" + strconv.Itoa(len(view.rows))
	}
	area := components.BeginScrollArea(canvas, theme, bodyBounds, gfx.Size{Width: bodyBounds.Width, Height: contentHeight}).
		Offset(0, view.processScroll).
		VerticalScrollbar(hitRemoteProcessScroll, m.RemoteTabProcesses, view.processScrollValue)
	geometry := area.Geometry()
	view.processScroll = geometry.OffsetY
	view.processScrollBar = geometry
	geometry = area.BeginContent()
	view.processScroll = geometry.OffsetY
	view.tableBodyBounds = bodyBounds
	view.tableRowHeight = rowHeight
	view.tableBodyFirst = 0

	if len(view.rows) == 0 {
		message := m.RemoteLoading
		color := pal.muted
		if !view.loading {
			message = view.status
			if message == "" {
				message = m.RemoteTabProcesses
			}
		}
		canvas.LabelCenter(message, geometry.Viewport, gfx.TextStyle{Size: theme.Metrics.FontSizeM, Color: color})
		area.EndContent()
		return
	}

	first := min(max(int(geometry.OffsetY/rowHeight), 0), len(view.rows))
	visible := int(geometry.Viewport.Height/rowHeight) + 2
	last := min(first+visible, len(view.rows))
	rowRemainder := geometry.OffsetY - float32(first)*rowHeight
	tableBounds := gfx.Rect{
		X: geometry.Viewport.X, Y: geometry.Viewport.Y - rowRemainder,
		Width: geometry.Viewport.Width, Height: min(geometry.Viewport.Height+rowHeight, float32(last-first)*rowHeight),
	}
	view.tableBodyFirst = first
	table := components.BeginTable(canvas, theme, tableBounds, columns).RowHeight(rowHeight)
	for index := first; index < last; index++ {
		row := &view.rows[index]
		table.Row(gfx.ID{}, row.cells[:len(headings)], row.key == view.selected)
	}
	area.EndContent()
}

// drawRemoteObjectTableControl paints the Docker table of the images, volumes,
// networks, containers and compose tabs inside its page control.
func (state *State) drawRemoteObjectTableControl(d *ui.DrawContext) {
	view, theme, m := &state.remote, *d.Theme, state.messages.Tools
	bounds := d.Rect
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas := d.Canvas
	missingDocker := view.noDocker && view.collectedAt.IsZero()
	if missingDocker {
		state.paintRemoteDockerEmpty(canvas, bounds)
		return
	}
	if view.loading && len(view.rows) == 0 {
		state.paintRemoteToolsLoading(canvas, bounds)
		return
	}
	columns, headings := state.objectTableSchema(view.tab)
	var headerCells [6]components.TableHeader
	order := view.tableSort()
	for index, heading := range headings {
		id := remoteTableHeaderID(view.tab, index)
		headerCells[index] = components.TableHeader{
			ID: id, Label: heading, Disabled: view.loading, FocusableWhenDisabled: true,
			Description: m.RemoteSortAction,
		}
		if order.Direction != components.TableSortNone && order.Column == index {
			headerCells[index].Sort = order.Direction
			if order.Direction == components.TableSortAscending {
				headerCells[index].Description = m.RemoteSortAscending
			} else {
				headerCells[index].Description = m.RemoteSortDescending
			}
		}
		if canvas.Clicked(id) {
			canvas.AfterPaint(func() { state.clickRemoteTableSort(id) })
		}
	}
	headerHeight := components.TableHeaderHeight(theme)
	components.FitSortableTableHeadings(canvas, theme, columns, headerCells[:len(headings)])
	header := components.BeginTable(canvas, theme, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: headerHeight}, columns)
	header.SortableHeader(headerCells[:len(headings)])
	body := gfx.Rect{X: bounds.X, Y: bounds.Y + headerHeight, Width: bounds.Width, Height: max(bounds.Height-headerHeight, 0)}
	view.tableBodyBounds = body
	view.tableRowHeight = components.TableRowHeight(theme)
	pageSize := state.remotePageSize()
	start := view.page * pageSize
	end := min(start+pageSize, len(view.rows))
	view.tableBodyFirst = start
	if end <= start || body.Height <= 0 {
		return
	}
	table := components.BeginTable(canvas, theme, body, columns)
	for index := start; index < end; index++ {
		row := &view.rows[index]
		table.Row(gfx.ID{}, row.cells[:len(headings)], row.key == view.selected)
	}
}

// objectTableSchema is the Docker table's columns and headings by tab.
func (state *State) objectTableSchema(tab remoteTab) ([]components.TableColumn, []string) {
	m, view := state.messages.Tools, &state.remote
	switch tab {
	case remoteCompose:
		view.columns = [6]string{m.RemoteColName, m.RemoteColStatus, m.RemoteColConfig}
		return []components.TableColumn{{Width: 220}, {Width: 200}, {Width: 0}}, view.columns[:3]
	case remoteImages:
		view.columns = [6]string{m.RemoteColName, m.RemoteColID, m.RemoteColSize, m.RemoteColCreated}
		return []components.TableColumn{{Width: 470}, {Width: 150}, {Width: 100, Alignment: components.TableAlignRight}, {Width: 0}}, view.columns[:4]
	case remoteVolumes:
		view.columns = [6]string{m.RemoteColName, m.RemoteColDriver, m.RemoteColScope}
		return []components.TableColumn{{Width: 470}, {Width: 150}, {Width: 0}}, view.columns[:3]
	case remoteNetworks:
		view.columns = [6]string{m.RemoteColName, m.RemoteColID, m.RemoteColDriver, m.RemoteColScope}
		return []components.TableColumn{{Width: 300}, {Width: 160}, {Width: 150}, {Width: 0}}, view.columns[:4]
	default:
		view.columns = [6]string{m.RemoteColName, m.RemoteColImage, m.RemoteColState, m.RemoteColStatus, m.RemoteColID}
		return []components.TableColumn{{Width: 200}, {Width: 220}, {Width: 90}, {Width: 200}, {Width: 0}}, view.columns[:5]
	}
}

// remoteTableControlPointer selects the row a press is on, opens the process
// details when a press on it is released, and leaves a secondary press to the
// page's context menu.
func (state *State) remoteTableControlPointer(event gfx.PointerEvent) gfx.EventResult {
	view := &state.remote
	if event.Kind == gfx.PointerDown && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonSecondary) {
		view.tablePressRow = -1
		index := state.remoteTableRowAt(event.X, event.Y)
		if index >= 0 && index < len(view.rows) {
			view.selected = view.rows[index].key
			view.tablePressRow = index
			if event.Button == gfx.PointerButtonSecondary {
				return gfx.EventResult{Repaint: true}
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{Handled: true}
	}
	if event.Kind == gfx.PointerUp && event.Button == gfx.PointerButtonPrimary {
		index := state.remoteTableRowAt(event.X, event.Y)
		pressed := view.tablePressRow
		view.tablePressRow = -1
		if index >= 0 && index == pressed && index < len(view.rows) {
			view.selected = view.rows[index].key
			if view.tab == remoteProcesses {
				if process, found := state.remoteProcessByKey(view.selected); found {
					state.openRemoteProcessDetails(process)
				}
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{}
}

// remoteTableControlKey moves the process selection with the arrow, Home, End
// and page keys, and opens the selected process on Enter. The control keeps the
// focus; the row identities are painted, not focused.
func (state *State) remoteTableControlKey(event gfx.KeyEvent) gfx.EventResult {
	view := &state.remote
	if view.tab != remoteProcesses {
		return gfx.EventResult{}
	}
	result := state.remoteProcessTableNavigate(event)
	result.Focus, result.FocusVisible = gfx.ID{}, false
	return result
}

// remoteTableRowAt is the row index under a point in the table's body, or -1.
func (state *State) remoteTableRowAt(x, y float32) int {
	view := &state.remote
	if view.tableRowHeight <= 0 || !pointInRect(x, y, view.tableBodyBounds) {
		return -1
	}
	index := view.tableBodyFirst + int((y-view.tableBodyBounds.Y)/view.tableRowHeight)
	if index < 0 || index >= len(view.rows) {
		return -1
	}
	return index
}
