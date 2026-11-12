package workbench

import (
	"fmt"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sshconn"
)

// remoteToolsWindowPageSeq names each tools window's page root: every window
// declares its controls in the one identity tree, so two windows may never name
// their controls alike.
var remoteToolsWindowPageSeq atomic.Uint32

const (
	remoteToolsContentPad       = float32(16)
	remoteToolsActionHeight     = float32(32)
	remoteToolsActionGap        = float32(8)
	remoteToolsStatusHeight     = float32(30)
	remoteToolsFooterMargin     = float32(12)
	remoteToolsToolbarGap       = float32(12)
	remoteToolsTableMargin      = float32(4)
	remoteToolsTableHeaderSpace = float32(48)
)

func (w *remoteToolsWindow) pageRoot() string {
	if w.root == "" {
		w.root = fmt.Sprintf("remote-tools-window-%d", remoteToolsWindowPageSeq.Add(1))
	}
	return w.root
}

// windowPage is the page the tools window declares its chrome and dialogs in.
func (w *remoteToolsWindow) windowPage() *ui.Page {
	if w.page == nil {
		w.tableHandlers = ui.Handlers{Role: gfx.RoleGroup, Key: w.processTableKey, Pointer: w.tablePointer}
		w.refreshHandlers = ui.Handlers{Role: gfx.RoleButton, Key: w.ui.remoteRefreshControlKey, Pointer: w.ui.remoteRefreshControlPointer}
		w.autoHandlers = ui.Handlers{Role: gfx.RoleButton, Key: w.ui.remoteAutoControlKey, Pointer: w.ui.remoteAutoControlPointer}
		w.footerFlow.gap = remoteToolsActionGap
		w.tableDraw = w.drawTableControl
		w.tableHeaderDraw = w.drawProcessHeader
		w.refreshDraw = w.ui.drawRemoteRefreshControl
		w.autoDraw = w.ui.drawRemoteAutoControl
		w.page = ui.NewPage(ui.PageConfig{
			Tree: &tree, Root: w.pageRoot(), Theme: &w.ui.theme, View: w.declareWindow,
		})
	}
	return w.page
}

// syncWindow takes the owning workbench's presenters, since a tool window is a
// presentation of the same source and follows its language and appearance.
func (w *remoteToolsWindow) syncWindow() {
	w.ui.language, w.ui.messages = w.owner.language, w.owner.messages
	w.ui.appearance, w.ui.pal, w.ui.theme = w.owner.appearance, w.owner.pal, w.owner.theme
}

// paint draws the window through its page.
func (w *remoteToolsWindow) paint(canvas *gfx.Canvas, size gfx.Size) {
	if w.closed {
		return
	}
	w.syncWindow()
	w.ui.viewport = gfx.Size{Width: size.Width, Height: size.Height}
	w.windowPage().Paint(canvas, size)
}

// declareWindow is the window's page: its titlebar, its tool body, and the
// dialogs and menus it opens.
func (w *remoteToolsWindow) declareWindow(c ui.C) {
	m, pal := w.ui.messages.Tools, w.ui.pal
	bounds := c.Bounds()
	size := gfx.Size{Width: bounds.Width, Height: bounds.Height}
	w.ui.viewport = gfx.Size{Width: size.Width, Height: size.Height}
	ui.Column(c).Fill().Background(pal.workspace).Children(func() {
		w.declareTitlebar(c)
		ui.Column(c).Grow(1).Shrink(1).PadSides(0, remoteToolsContentPad, remoteToolsContentPad, remoteToolsContentPad).Children(func() {
			tab := w.declareToolbar(c, size)
			w.declareSearch(c)
			w.declareTable(c, tab, size)
			w.declareStatus(c, tab)
			w.declareActions(c)
		})
		// Dialogs are overlays of this page: they take no room in the column.
		w.declareProcessDetails(c, m)
		w.declareContainerDetails(c, m)
		w.declareFormDialog(c)
		w.declareSudoDialog(c)
	})
}

func (w *remoteToolsWindow) declareTitlebar(c ui.C) {
	m, pal := w.ui.messages.Tools, w.ui.pal
	leading, trailing := app.TitlebarInsets()
	const gap = float32(14)
	ui.Row(c).Height(TitlebarHeight).PadSides(0, trailing+remoteToolsContentPad, 0, leading+remoteToolsContentPad).
		Items(ui.Center).MovesWindow().Children(func() {
		ui.Text(c, m.RemoteTitle).FontSize(w.ui.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).
			Color(pal.text).Shrink(0)
		connection := w.session.Title + " · " + w.session.Address
		ui.Text(c, connection).FontSize(w.ui.Theme().Metrics.FontSizeS).Color(pal.muted).
			MarginLeft(gap).Ellipsis().Grow(1)
	})
}

// declareToolbar declares the tab choice, the refresh controls and the sudo
// switch. It returns the tab the paint started with: the choice writes a new tab
// while it is declared, and the body laid out from the old one is what the rest
// of this paint shows.
func (w *remoteToolsWindow) declareToolbar(c ui.C, size gfx.Size) remoteTab {
	view := &w.ui.remote
	tab := view.tab
	stacked := size.Width < 800
	ui.Row(c).Height(remoteToolbarHeight).Shrink(0).Gap(remoteToolsToolbarGap).Children(func() {
		if ui.Choice(c, &view.tab, remoteTabChoices[:], w.ui.remoteTabLabel).Width(600).Shrink(1).FillHeight().Changed() {
			w.tableHoverRow = -1
			w.ui.applyRemoteTab()
		}
		ui.Spacer(c)
		if !stacked {
			w.declareRefreshControls(c)
			w.declareSudo(c)
		}
	})
	if stacked {
		ui.Row(c).Height(remoteToolbarHeight).Shrink(0).MarginSides(8, 0, 0, 0).Children(func() {
			ui.Spacer(c)
			w.declareRefreshControls(c)
			w.declareSudo(c)
		})
	}
	return tab
}

func (w *remoteToolsWindow) declareRefreshControls(c ui.C) {
	ui.Control(c, w.ui.messages.Tools.RemoteRefresh, &w.refreshHandlers).
		Size(40, remoteToolbarHeight).Shrink(0).Draw(w.refreshDraw)
	auto := ui.Control(c, w.ui.remoteAutoAccessibleLabel(), &w.autoHandlers).
		Size(134, remoteToolbarHeight).Shrink(0).MarginLeft(remoteToolsActionGap).Draw(w.autoDraw)
	ui.Menu(c, auto, &w.ui.remote.auto.open, w.ui.declareRemoteAutoMenu)
}

func (w *remoteToolsWindow) declareSudo(c ui.C) {
	session := &w.session
	if session.host.System == sshconn.SystemWindows {
		return
	}
	label := w.ui.messages.SudoCommandsEnable
	if session.sudoCommands {
		label = w.ui.messages.SudoCommandsDisable
	}
	if ui.Button(c, label).Icon(lucideIcons[iconLock]).IconOnly().Ghost().Shrink(0).Clicked() {
		c.After(func() { w.act(hitRemoteSudo) })
	}
}

func (w *remoteToolsWindow) declareSearch(c ui.C) {
	m, pal, view := w.ui.messages.Tools, w.ui.pal, &w.ui.remote
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
			w.ui.rebuildRemoteRows()
		}
		if field.ActionClicked() {
			w.ui.clearRemoteSearch()
		}
		summary := view.searchSummary
		ui.Text(c, summary).FontSize(w.ui.Theme().Metrics.FontSizeS).Color(pal.muted).
			Height(20).MarginTop(4).Ellipsis().
			HitWith(hitRemoteSearchScope, summary, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: summary})
	})
}

// declareTable declares the table the current tab lists. A table is one page
// Control: its cells are painted exactly as the table component lays them out,
// and the control takes the row selection, the wheel and the process menu.
func (w *remoteToolsWindow) declareTable(c ui.C, tab remoteTab, size gfx.Size) {
	view, theme := &w.ui.remote, w.ui.Theme()
	w.paintTab = tab
	headerHeight := components.TableHeaderHeight(theme)
	tableHeight := headerHeight + float32(w.ui.remotePageSize())*components.TableRowHeight(theme)
	if tab != remoteProcesses {
		available := w.remoteTableAvailable(c.Canvas(), size)
		pageSize := max(1, int((available-remoteToolsTableHeaderSpace-headerHeight)/components.TableRowHeight(theme)))
		if pageSize != w.pageSize {
			first := view.page * w.pageSize
			w.pageSize = pageSize
			view.page = min(first/pageSize, max(0, (len(view.rows)-1)/pageSize))
			view.pageText = ""
		}
		tableHeight = headerHeight + float32(w.pageSize)*components.TableRowHeight(theme)
	}
	if tab == remoteProcesses {
		ui.Column(c).Grow(1).Shrink(1).MinHeight(headerHeight+components.TableRowHeight(theme)).
			MarginSides(remoteToolsTableMargin, 0, 0, 0).Border(1, w.ui.pal.line).Radius(radius.control).Children(func() {
			ui.Slot(c).Height(headerHeight).Shrink(0).Draw(w.tableHeaderDraw)
			body := ui.Control(c, w.ui.messages.Tools.RemoteTabProcesses, &w.tableHandlers).
				Grow(1).Shrink(1).Draw(w.tableDraw)
			w.menuCtx = c
			ui.ContextMenu(c, body, w.declareProcessMenu)
		})
		return
	}
	ui.Control(c, w.ui.remoteTabLabel(tab), &w.tableHandlers).
		Height(tableHeight).Shrink(0).MarginSides(remoteToolsTableMargin, 0, 0, 0).Draw(w.tableDraw)
}

// remoteTableAvailable is the height the table body has, less its header, once
// the toolbar, search line, footer actions and status line have taken theirs.
func (w *remoteToolsWindow) remoteTableAvailable(canvas *gfx.Canvas, size gfx.Size) float32 {
	toolbar := remoteToolbarHeight
	if size.Width < 800 {
		toolbar += remoteToolbarHeight + 8
	}
	footer := w.remoteFooterHeight(canvas, size.Width)
	available := size.Height - TitlebarHeight - 2*remoteToolsContentPad - footer - remoteToolsFooterMargin
	available -= toolbar + 8 + remoteSearchHeight + remoteToolsStatusHeight
	return max(available, 0)
}

// remoteFooterHeight measures the wrapped action row the same way the flow
// layout will, so the table knows the room the footer keeps.
func (w *remoteToolsWindow) remoteFooterHeight(canvas *gfx.Canvas, width float32) float32 {
	actions, count := w.ui.remoteToolActions()
	width = max(width-2*remoteToolsContentPad, 0)
	theme := w.ui.Theme()
	x, y := float32(0), float32(0)
	for i := 0; i < count; i++ {
		buttonWidth := min(components.BeginButton(canvas, theme, gfx.ID{}, gfx.Rect{}).Small().SizeToFit(actions[i].Label).BoundsWidth(), width)
		if x > 0 && x+buttonWidth > width {
			x, y = 0, y+remoteToolsActionHeight+remoteToolsActionGap
		}
		x += buttonWidth + remoteToolsActionGap
	}
	return y + remoteToolsActionHeight
}

func (w *remoteToolsWindow) declareStatus(c ui.C, tab remoteTab) {
	pal, view := w.ui.pal, &w.ui.remote
	pageSize := w.ui.remotePageSize()
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
	ui.Row(c).Height(remoteToolsStatusHeight).Shrink(0).Items(ui.Center).MarginSides(6, 0, 0, 0).Children(func() {
		ui.Text(c, status).FontSize(w.ui.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).Color(color).
			Height(24).Ellipsis().Grow(1)
		if view.snapshotStatus != "" {
			ui.Text(c, view.snapshotStatus).FontSize(w.ui.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).
				Color(pal.muted).Width(360).Height(24).Shrink(1).Ellipsis().
				HitWith(hitRemoteSnapshotStatus, view.snapshotStatus, gfx.HitOptions{Role: gfx.RoleStatus, SkipFocus: true, Hint: view.searchSummary})
		}
		if tab != remoteProcesses && len(view.rows) > pageSize {
			if view.pageText == "" {
				view.pageText = fmt.Sprint(view.page+1) + "/" + fmt.Sprint(pages)
			}
			if ui.Button(c, w.ui.messages.KnownHostsPrevious).Ghost().Small().Shrink(0).
				Disabled(view.page == 0).Clicked() {
				c.After(func() {
					if view.page > 0 {
						view.page--
						view.pageText = ""
					}
				})
			}
			ui.Text(c, view.pageText).FontSize(w.ui.Theme().Metrics.FontSizeS).Color(pal.muted).Width(56).TextCenter().Shrink(0)
			if ui.Button(c, w.ui.messages.KnownHostsNext).Ghost().Small().Shrink(0).
				Disabled(view.page+1 >= pages).Clicked() {
				c.After(func() {
					view.page++
					view.pageText = ""
				})
			}
		}
	})
}

// declareActions declares the footer's action buttons, wrapped into as many rows
// as their labels need.
func (w *remoteToolsWindow) declareActions(c ui.C) {
	actions, count := w.ui.remoteToolActions()
	ui.CustomLayout(c, &w.footerFlow).FillWidth().MarginTop(remoteToolsFooterMargin).Children(func() {
		for i := 0; i < count; i++ {
			action := actions[i]
			button := ui.Button(c, action.Label).Small().Shrink(0)
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
				c.After(func() { w.act(action.ID) })
			}
		}
	})
}

// remoteToolsButtonFlow wraps the footer's buttons into lines, each as tall as
// its tallest button.
type remoteToolsButtonFlow struct {
	gap        float32
	line       []int
	lineHeight []float32
}

func (f *remoteToolsButtonFlow) MeasureWidth(*ui.LayoutContext) float32 { return 0 }

func (f *remoteToolsButtonFlow) PlaceWidth(ctx *ui.LayoutContext, bounds ui.Rect) {
	f.line = f.line[:0]
	line := 0
	x := bounds.X
	right := bounds.X + bounds.Width
	for i := 0; i < ctx.Count(); i++ {
		width := min(ctx.Width(i), bounds.Width)
		if x > bounds.X && x+width > right {
			x, line = bounds.X, line+1
		}
		f.line = append(f.line, line)
		ctx.SetWidth(i, width)
		ctx.SetX(i, x)
		x += width + f.gap
	}
}

func (f *remoteToolsButtonFlow) MeasureHeight(ctx *ui.LayoutContext) float32 {
	f.lineHeight = f.lineHeight[:0]
	for i := 0; i < ctx.Count(); i++ {
		line := f.lineAt(i)
		for len(f.lineHeight) <= line {
			f.lineHeight = append(f.lineHeight, 0)
		}
		f.lineHeight[line] = max(f.lineHeight[line], ctx.Height(i))
	}
	total := float32(0)
	for i, height := range f.lineHeight {
		if i > 0 {
			total += f.gap
		}
		total += height
	}
	return total
}

func (f *remoteToolsButtonFlow) PlaceHeight(ctx *ui.LayoutContext, bounds ui.Rect) {
	y := bounds.Y
	for line, height := range f.lineHeight {
		if line > 0 {
			y += f.gap
		}
		for i := 0; i < ctx.Count(); i++ {
			if f.lineAt(i) != line {
				continue
			}
			ctx.SetHeight(i, ctx.Height(i))
			ctx.SetY(i, y)
		}
		y += height
	}
}

func (f *remoteToolsButtonFlow) lineAt(i int) int {
	if i < 0 || i >= len(f.line) {
		return 0
	}
	return f.line[i]
}

// drawTableControl paints the current tab's table inside its control: the
// process snapshot's virtual rows, or the Docker object table.
func (w *remoteToolsWindow) drawTableControl(d *ui.DrawContext) {
	if w.paintTab == remoteProcesses {
		w.paintProcessRows(d.Canvas, d.Rect)
		return
	}
	w.paintObjectTable(d.Canvas, d.Rect, w.paintTab)
}

func (w *remoteToolsWindow) drawProcessHeader(d *ui.DrawContext) {
	view, theme, m := &w.ui.remote, *d.Theme, w.ui.messages.Tools
	view.processTableBounds = gfx.Rect{X: d.Rect.X, Y: d.Rect.Y, Width: d.Rect.Width, Height: d.Rect.Height}
	columns, headings := w.ui.processTableSchema(d.Rect.Width)
	var cells [6]components.TableHeader
	order := view.tableSort()
	for index, heading := range headings {
		id := remoteTableHeaderID(remoteProcesses, index)
		cells[index] = components.TableHeader{
			ID: id, Label: heading, Disabled: view.loading, FocusableWhenDisabled: true,
			Description: m.RemoteSortAction,
		}
		if order.Direction != components.TableSortNone && order.Column == index {
			cells[index].Sort = order.Direction
			if order.Direction == components.TableSortAscending {
				cells[index].Description = m.RemoteSortAscending
			} else {
				cells[index].Description = m.RemoteSortDescending
			}
		}
		if d.Canvas.Clicked(id) {
			d.Canvas.AfterPaint(func() { w.ui.clickRemoteTableSort(id) })
		}
	}
	components.FitSortableTableHeadings(d.Canvas, theme, columns, cells[:len(headings)])
	table := components.BeginTable(d.Canvas, theme, d.Rect, columns).
		HeaderHeight(d.Rect.Height).
		RowHeight(components.TableRowHeight(theme))
	table.SortableHeader(cells[:len(headings)])
}

// paintProcessRows paints the visible slice of the process snapshot. The
// snapshot stays complete in memory; only the rows intersecting the viewport
// become text runs.
func (w *remoteToolsWindow) paintProcessRows(canvas *gfx.Canvas, bounds gfx.Rect) {
	view, theme, pal := &w.ui.remote, w.ui.Theme(), w.ui.pal
	_, rowHeight := w.ui.remoteProcessTableMetrics()
	headerHeight := components.TableHeaderHeight(theme)
	view.processTableBounds = gfx.Rect{X: bounds.X, Y: bounds.Y - headerHeight, Width: bounds.Width, Height: bounds.Height + headerHeight}
	view.tableBodyBounds = bounds
	view.tableRowHeight = rowHeight
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	columns, headings := w.ui.processTableSchema(bounds.Width)
	contentHeight := max(float32(len(view.rows))*rowHeight, bounds.Height)
	firstVisible := min(len(view.rows), 1+int(view.processScroll/rowHeight))
	if view.processScrollFirst != firstVisible || view.processScrollCount != len(view.rows) {
		view.processScrollFirst, view.processScrollCount = firstVisible, len(view.rows)
		view.processScrollValue = fmt.Sprint(firstVisible) + "/" + fmt.Sprint(len(view.rows))
	}
	area := components.BeginScrollArea(canvas, theme, bounds, gfx.Size{Width: bounds.Width, Height: contentHeight}).
		Offset(0, view.processScroll).
		VerticalScrollbar(hitRemoteProcessScroll, w.ui.messages.Tools.RemoteTabProcesses, view.processScrollValue)
	geometry := area.Geometry()
	view.processScroll = geometry.OffsetY
	view.processScrollBar = geometry
	geometry = area.BeginContent()
	view.processScroll = geometry.OffsetY

	if len(view.rows) == 0 {
		message := w.ui.messages.Tools.RemoteLoading
		color := pal.muted
		if !view.loading {
			message = view.status
			if message == "" {
				message = w.ui.messages.Tools.RemoteTabProcesses
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
		if index == w.tableHoverRow && row.key != view.selected {
			canvas.Fill(gfx.Rect{
				X: tableBounds.X, Y: tableBounds.Y + float32(index-first)*rowHeight,
				Width: tableBounds.Width, Height: rowHeight,
			}, theme.Hover)
		}
		table.Row(gfx.ID{}, row.cells[:len(headings)], row.key == view.selected)
	}
	area.EndContent()
}

// paintObjectTable paints the Docker table of the images, volumes, networks,
// containers and compose tabs.
func (w *remoteToolsWindow) paintObjectTable(canvas *gfx.Canvas, bounds gfx.Rect, tab remoteTab) {
	view, theme, m := &w.ui.remote, w.ui.Theme(), w.ui.messages.Tools
	view.tableBodyBounds = bounds
	view.tableRowHeight = components.TableRowHeight(theme)
	view.tableBodyFirst = view.page * w.ui.remotePageSize()
	missingDocker := view.noDocker && view.collectedAt.IsZero()
	if missingDocker {
		w.ui.paintRemoteDockerEmpty(canvas, bounds)
		return
	}
	if view.loading && len(view.rows) == 0 {
		w.ui.paintRemoteToolsLoading(canvas, bounds)
		return
	}
	var columns []components.TableColumn
	var headings []string
	switch tab {
	case remoteCompose:
		columns = []components.TableColumn{{Width: 220}, {Width: 200}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColStatus, m.RemoteColConfig}
		headings = view.columns[:3]
	case remoteImages:
		columns = []components.TableColumn{{Width: 470}, {Width: 150}, {Width: 100, Alignment: components.TableAlignRight}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColID, m.RemoteColSize, m.RemoteColCreated}
		headings = view.columns[:4]
	case remoteVolumes:
		columns = []components.TableColumn{{Width: 470}, {Width: 150}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColDriver, m.RemoteColScope}
		headings = view.columns[:3]
	case remoteNetworks:
		columns = []components.TableColumn{{Width: 300}, {Width: 160}, {Width: 150}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColID, m.RemoteColDriver, m.RemoteColScope}
		headings = view.columns[:4]
	default:
		columns = []components.TableColumn{{Width: 200}, {Width: 220}, {Width: 90}, {Width: 200}, {Width: 0}}
		view.columns = [6]string{m.RemoteColName, m.RemoteColImage, m.RemoteColState, m.RemoteColStatus, m.RemoteColID}
		headings = view.columns[:5]
	}
	var headerCells [6]components.TableHeader
	order := view.tableSort()
	for index, heading := range headings {
		id := remoteTableHeaderID(tab, index)
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
			canvas.AfterPaint(func() { w.ui.clickRemoteTableSort(id) })
		}
	}
	headerHeight := components.TableHeaderHeight(theme)
	components.FitSortableTableHeadings(canvas, theme, columns, headerCells[:len(headings)])
	header := components.BeginTable(canvas, theme, gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: headerHeight}, columns)
	header.SortableHeader(headerCells[:len(headings)])
	body := gfx.Rect{X: bounds.X, Y: bounds.Y + headerHeight, Width: bounds.Width, Height: max(bounds.Height-headerHeight, 0)}
	view.tableBodyBounds = body
	pageSize := w.ui.remotePageSize()
	start := view.page * pageSize
	end := min(start+pageSize, len(view.rows))
	if end <= start || body.Height <= 0 {
		return
	}
	table := components.BeginTable(canvas, theme, body, columns)
	for index := start; index < end; index++ {
		row := &view.rows[index]
		if index == w.tableHoverRow && row.key != view.selected {
			canvas.Fill(gfx.Rect{
				X: body.X, Y: body.Y + float32(index-start)*view.tableRowHeight,
				Width: body.Width, Height: view.tableRowHeight,
			}, theme.Hover)
		}
		table.Row(gfx.ID{}, row.cells[:len(headings)], row.key == view.selected)
	}
}

// declareProcessMenu declares the process context menu: the row the table's
// control last selected, and the same actions the process buttons offer.
func (w *remoteToolsWindow) declareProcessMenu(menu *ui.MenuItems) {
	after := func(fn func()) {
		if w.menuCtx != nil {
			w.menuCtx.After(fn)
			return
		}
		fn()
	}
	w.ui.declareRemoteProcessMenuItems(menu, after, w.act)
}

// declareProcessDetails declares the open process details as a dialog of the
// window's page.
func (w *remoteToolsWindow) declareProcessDetails(c ui.C, m i18n.ToolsMessages) {
	w.ui.declareRemoteProcessDetailsDialog(c, w.act)
}

// declareContainerDetails declares the open container details as a dialog of
// the window's page.
func (w *remoteToolsWindow) declareContainerDetails(c ui.C, m i18n.ToolsMessages) {
	w.ui.declareRemoteContainerDetailsDialog(c)
}

// declareFormDialog declares the open form dialog. Its fields and buttons are
// page controls; the editors they bind are the form's, which its action reads.
func (w *remoteToolsWindow) declareFormDialog(c ui.C) {
	if w.ui.modal != modalForm {
		return
	}
	form := &w.ui.form
	viewport := c.Bounds()
	bodyHeight := form.bodyHeight() + w.ui.formProblemHeight(c.Canvas(), form.formProblem())
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), gfx.Rect{Width: viewport.Width, Height: viewport.Height}, components.DialogOptions{
		ContentSize: gfx.Size{Width: formWidth}, FitBody: true, BodyHeight: bodyHeight,
	}).Layout(form.description)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: form.title, Description: form.description, Width: formWidth,
		Height: layout.Content.Height, Layer: dialogLayer, Scope: dialogScope, CloseID: hitFormClose,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().PadSides(4, 4, 0, 4).Children(func() {
			for index := 0; index < form.count; index++ {
				field := &form.fields[index]
				if !field.pageBound {
					field.pageField.Bind(&field.editor)
					field.pageBound = true
				}
				box := ui.TextFieldID(c, hitFormFieldList.At(index), &field.pageField, field.label, field.placeholder).
					Height(62).FillWidth().MarginBottom(4)
				if field.secure {
					box.Secure()
				}
				if index == 0 {
					box.AutoFocus()
				}
			}
			if form.dialogError != "" {
				ui.Paragraph(c, form.dialogError, 0).FontSize(w.ui.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).
					Color(w.ui.pal.danger).LineHeight(formProblemLine).MarginSides(formProblemGap, 0, 0, 0)
			} else if form.testNote != "" {
				ui.Paragraph(c, form.testNote, 0).FontSize(w.ui.Theme().Metrics.FontSizeS).Weight(gfx.WeightMedium).
					Color(w.ui.pal.success).LineHeight(formProblemLine).MarginSides(formProblemGap, 0, 0, 0)
			}
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, w.ui.messages.Cancel).Secondary().Small().Shrink(0).Clicked() {
			c.After(func() { w.ui.clickForm(hitFormCancel) })
		}
		if form.test != nil {
			if ui.Button(c, w.ui.messages.Tools.SyncTest).Outline().Small().Shrink(0).
				Disabled(form.testRunning).Clicked() {
				c.After(func() {
					if message := w.ui.startFormTest(); message != "" {
						w.ui.form.dialogError = message
					}
				})
			}
		}
		submit := ui.Button(c, form.submit).Small().Shrink(0)
		if form.danger {
			submit = submit.Destructive()
		}
		if form.count == 0 {
			submit = submit.AutoFocus()
		}
		if submit.Clicked() {
			c.After(w.ui.submitForm)
		}
	})
	if dialog.Dismissed() {
		c.After(func() { w.ui.clickForm(hitFormCancel) })
	}
}

// declareSudoDialog declares the open sudo password dialog. Its field and
// buttons are page controls; the editor they bind is the one submit reads.
func (w *remoteToolsWindow) declareSudoDialog(c ui.C) {
	if w.ui.modal != modalSudoSFTP {
		return
	}
	dialogState, m := &w.ui.sudoSFTP, w.ui.messages
	title, hint := m.SFTPSudoEnable, m.SFTPSudoHint
	if dialogState.commands {
		title, hint = m.SudoCommandsEnable, m.SudoCommandsHint
		if dialogState.dashboard {
			title, hint = m.DashboardSudoEnable, m.DashboardSudoHint
		}
	}
	if !dialogState.bound {
		dialogState.field.Bind(&dialogState.password)
		dialogState.bound = true
	}
	viewport := c.Bounds()
	layout := components.BeginDialog(c.Canvas(), *c.Theme(), gfx.Rect{Width: viewport.Width, Height: viewport.Height}, components.DialogOptions{
		ContentSize: gfx.Size{Width: 540}, FitBody: true, BodyHeight: w.ui.secretFieldBody(""),
	}).Layout(hint)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: hint, Width: 540, Height: layout.Content.Height,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSudoSFTPDismiss, CloseID: hitSudoSFTPCancel, InitialFocusID: hitSudoSFTPPassword,
	})
	dialog.Body(func() {
		ui.Column(c).Fill().PadSides(2, 4, 0, 4).Children(func() {
			ui.TextFieldID(c, hitSudoSFTPPassword, &dialogState.field, m.SFTPSudoPassword, "").
				Height(62).FillWidth().Secure().AutoFocus()
		})
	})
	dialog.Footer(func() {
		if ui.Button(c, m.Cancel).Secondary().Small().Shrink(0).Clicked() {
			c.After(func() { w.ui.clickSudoSFTP(hitSudoSFTPCancel) })
		}
		if ui.Button(c, title).Small().Shrink(0).Clicked() {
			c.After(func() { w.ui.submitSudoSFTP() })
		}
	})
	if dialog.Dismissed() {
		c.After(func() { w.ui.clickSudoSFTP(hitSudoSFTPCancel) })
	}
}

// processTableKey moves the process selection with the arrow, Home, End and
// page keys, and opens the selected process on Enter.
func (w *remoteToolsWindow) processTableKey(event gfx.KeyEvent) gfx.EventResult {
	if w.closed || w.ui.remote.tab != remoteProcesses {
		return gfx.EventResult{}
	}
	result := w.ui.remoteProcessTableNavigate(event)
	result.Focus, result.FocusVisible = gfx.ID{}, false
	return result
}

// tablePointer selects the row a press is on, opens the process details when a
// press on it is released, and leaves a secondary press to the context menu.
func (w *remoteToolsWindow) tablePointer(event gfx.PointerEvent) gfx.EventResult {
	if w.closed {
		return gfx.EventResult{Handled: true}
	}
	view := &w.ui.remote
	if event.Kind == gfx.PointerDown && (event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonSecondary) {
		w.tablePressRow = -1
		index := w.tableRowAt(event.X, event.Y)
		if index >= 0 && index < len(view.rows) {
			view.selected = view.rows[index].key
			w.tablePressRow, w.tableHoverRow = index, index
			if event.Button == gfx.PointerButtonSecondary {
				return gfx.EventResult{Repaint: true}
			}
			return gfx.EventResult{Handled: true, Repaint: true}
		}
		return gfx.EventResult{Handled: true}
	}
	if event.Kind == gfx.PointerMove && !event.Captured {
		index := w.tableRowAt(event.X, event.Y)
		if index != w.tableHoverRow {
			w.tableHoverRow = index
			return gfx.EventResult{Repaint: true}
		}
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerExit && !event.Captured {
		if w.tableHoverRow != -1 {
			w.tableHoverRow = -1
			return gfx.EventResult{Repaint: true}
		}
		return gfx.EventResult{}
	}
	if event.Kind == gfx.PointerUp && event.Button == gfx.PointerButtonPrimary && view.tab == remoteProcesses {
		index := w.tableRowAt(event.X, event.Y)
		defer func() { w.tablePressRow = -1 }()
		if index < 0 || index != w.tablePressRow || index >= len(view.rows) {
			return gfx.EventResult{Handled: true}
		}
		view.selected = view.rows[index].key
		if process, found := w.ui.remoteProcessByKey(view.selected); found {
			w.ui.openRemoteProcessDetails(process)
		}
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{}
}

// tableRowAt is the row index under a point in the table's body, or -1.
func (w *remoteToolsWindow) tableRowAt(x, y float32) int {
	view := &w.ui.remote
	if view.tableRowHeight <= 0 || !pointInRect(x, y, view.tableBodyBounds) {
		return -1
	}
	index := view.tableBodyFirst + int((y-view.tableBodyBounds.Y)/view.tableRowHeight)
	if index < 0 || index >= len(view.rows) {
		return -1
	}
	return index
}
