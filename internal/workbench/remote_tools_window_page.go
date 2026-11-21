package workbench

import (
	"fmt"
	"sync/atomic"

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
	remoteToolsContentPad   = float32(16)
	remoteToolsActionGap    = float32(8)
	remoteToolsStatusHeight = float32(30)
	remoteToolsFooterMargin = float32(12)
	remoteToolsToolbarGap   = float32(12)
	remoteToolsTableMargin  = float32(4)
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
		w.refreshHandlers = ui.Handlers{Role: gfx.RoleButton, Key: w.ui.remoteRefreshControlKey, Pointer: w.ui.remoteRefreshControlPointer}
		w.autoHandlers = ui.Handlers{Role: gfx.RoleButton, Key: w.ui.remoteAutoControlKey, Pointer: w.ui.remoteAutoControlPointer}
		w.ui.remote.processMenu = w.declareProcessMenu
		w.ui.remote.containerMenu = w.declareContainerMenu
		w.footerFlow.gap = remoteToolsActionGap
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
			w.declareTable(c, tab)
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
	declareWindowTitlebar(c, w.ui.pal, w.ui.messages.Tools.RemoteTitle, w.session.Title+" · "+w.session.Address)
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
			view.resetScroll = true
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

// declareTable declares the table the current tab lists. The header is a page
// row of sortable cells and the rows are a page List: the list takes the row
// selection, the wheel and the row's context menu, and paints only the rows on
// screen.
func (w *remoteToolsWindow) declareTable(c ui.C, tab remoteTab) {
	w.menuCtx = c
	fallback := w.ui.viewport.Width - 2*remoteToolsContentPad - remoteToolsTableMargin - 2
	w.ui.declareRemoteToolsTable(c, tab, fallback)
}

func (w *remoteToolsWindow) declareStatus(c ui.C, tab remoteTab) {
	pal, view := w.ui.pal, &w.ui.remote
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
			button = button.Variant(remoteToolsVariant(action.Variant))
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

func (w *remoteToolsWindow) declareContainerMenu(menu *ui.MenuItems) {
	after := func(fn func()) {
		if w.menuCtx != nil {
			w.menuCtx.After(fn)
			return
		}
		fn()
	}
	w.ui.declareRemoteContainerMenuItems(menu, after, w.act)
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
	bodyHeight := form.bodyHeight(c.Theme()) + w.ui.formProblemHeight(c.Canvas(), form.formProblem())
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: form.title, Description: form.description, Width: formWidth,
		Layer: dialogLayer, Scope: dialogScope, CloseID: hitFormClose,
	})
	dialog.Body(func() {
		ui.Column(c).Height(bodyHeight).PadSides(4, 4, 0, 4).Children(func() {
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
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Description: hint, Width: 540,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitSudoSFTPDismiss, CloseID: hitSudoSFTPCancel, InitialFocusID: hitSudoSFTPPassword,
	})
	dialog.Body(func() {
		ui.Column(c).Height(sudoDialogBody(c.Theme())).PadSides(2, 4, 0, 4).Children(func() {
			ui.FormField(c, &dialogState.row, m.SFTPSudoPassword).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
				ui.TextFieldID(c, hitSudoSFTPPassword, &dialogState.field, m.SFTPSudoPassword, "").
					FillWidth().Secure().AutoFocus()
			})
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
