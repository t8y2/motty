package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

const remoteToolsModalWidth = float32(940)

// smallDialogFooterAdjust is how much shorter a dialog's footer is when its
// buttons are Small: components.Dialog's FitBody then fits a body four pixels
// lower than its default footer does. The detail dialogs sized their old
// bodies with ButtonSizeSmall, so the page's fixed height keeps it.
func smallDialogFooterAdjust(theme gfx.Theme) float32 {
	height := theme.Metrics.ControlHeight
	return max(height, 20) - max(height-4, 20)
}

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
	tableHeight := components.TableHeaderHeight(theme) + float32(remoteDefaultVisibleRows)*components.TableRowHeight(theme)
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
	if view.refreshHandlers.Pointer != nil {
		return
	}
	state.ensureRemoteTableHooks()
	view.refreshHandlers = ui.Handlers{
		Role: gfx.RoleButton, Key: state.remoteRefreshControlKey, Pointer: state.remoteRefreshControlPointer,
	}
	view.autoHandlers = ui.Handlers{
		Role: gfx.RoleButton, Key: state.remoteAutoControlKey, Pointer: state.remoteAutoControlPointer,
	}
	view.tabLabel = state.remoteTabLabel
	view.autoMenu = state.declareRemoteAutoMenu
	view.autoDraw = state.drawRemoteAutoControl
	view.body = func() { state.declareRemoteToolsBody(view.ctx) }
	view.footer = func() { state.declareRemoteToolActions(view.ctx) }
	view.detailsAct = state.clickRemoteTools
	view.processMenu = func(menu *ui.MenuItems) {
		after := func(fn func()) { view.ctx.After(fn) }
		state.declareRemoteProcessMenuItems(menu, after, state.clickRemoteTools)
	}
	view.containerMenu = func(menu *ui.MenuItems) {
		after := func(fn func()) { view.ctx.After(fn) }
		state.declareRemoteContainerMenuItems(menu, after, state.clickRemoteTools)
	}
}

// declareRemoteToolsBody declares the tools dialog's body: the tab switch and
// refresh controls, the search line, the table of the tab the paint started
// with, and the status line under it.
func (state *State) declareRemoteToolsBody(c ui.C) {
	view := &state.remote
	tab := view.tab
	ui.Container(c).Fill().Children(func() {
		// The tabs and the refresh controls share a line once the dialog's body
		// is wide enough for both; a narrower body puts the controls on a line
		// of their own under the tabs.
		ui.Column(c).Shrink(0).Gap(8).From(ui.Width(remoteToolsInlineToolbarWidth), func(b ui.Box) { b.Row().Gap(12) }).Children(func() {
			ui.Row(c).Height(remoteToolbarHeight).Grow(1).Shrink(1).Children(func() {
				if ui.Choice(c, &view.tab, remoteTabChoices[:], view.tabLabel).Width(600).Shrink(1).FillHeight().Changed() {
					state.applyRemoteTab()
				}
			})
			ui.Row(c).Height(remoteToolbarHeight).Shrink(0).Children(func() {
				ui.Spacer(c)
				state.declareRemoteRefreshControls(c)
			})
		})
		state.declareRemoteToolsSearch(c)
		state.declareRemoteTable(c)
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
		field := ui.TextField(c, &search.field, m.RemoteSearch, m.RemoteSearchPlaceholder, ui.WithID(hitRemoteSearch)).
			Height(32).FillWidth().Shrink(0).Icon(lucideIcons[iconSearch]).NavigationKeys()
		if query != "" {
			field = field.Action(lucideIcons[iconClose], m.RemoteSearchClear)
		}
		if field.Changed() {
			view.resetScroll = true
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

// declareRemoteTable declares the selected snapshot as the page's sortable
// header and List. The list takes the row selection, the wheel and the row's
// context menu, and paints only the rows on screen.
func (state *State) declareRemoteTable(c ui.C) {
	view, theme := &state.remote, state.Theme()
	padding := theme.Metrics.PadSurface + theme.Metrics.GapItem
	width := max(min(remoteToolsModalWidth, dialogViewport(c).Width-32), 0)
	fallback := width - 2*padding - remoteToolsTableMargin - 2
	view.ctx = c
	state.declareRemoteToolsTable(c, view.tab, fallback)
}

// declareRemoteToolsStatus declares the line under the table: what the last
// read or action did and when the snapshot was collected.
func (state *State) declareRemoteToolsStatus(c ui.C, tab remoteTab) {
	pal, view := state.pal, &state.remote
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
	})
}

// declareRemoteToolActions declares the footer's action buttons.
func (state *State) declareRemoteToolActions(c ui.C) {
	actions, count := state.remoteToolActions()
	for index := 0; index < count; index++ {
		action := actions[index]
		button := ui.Button(c, action.Label).Shrink(0)
		button = button.Variant(remoteToolsVariant(action.Variant))
		if action.Disabled {
			button = button.Disabled(true)
		}
		if button.Clicked() {
			id := action.ID
			c.After(func() { state.clickRemoteTools(id) })
		}
	}
}

// objectTableSchema is the Docker table's columns and headings by tab. It
// fills the view's own column storage, so a paint neither allocates the schema
// nor loses it before the rows are declared.
func (state *State) objectTableSchema(tab remoteTab, width float32) ([]components.TableColumn, []string) {
	m, view := state.messages.Tools, &state.remote
	switch tab {
	case remoteCompose:
		view.columns = [remoteMaxColumns]string{m.RemoteColName, m.RemoteColStatus, m.RemoteColConfig}
		view.processColumns = [remoteMaxColumns]components.TableColumn{{Width: 220}, {Width: 200}, {Width: 0}}
		return view.processColumns[:3], view.columns[:3]
	case remoteImages:
		view.columns = [remoteMaxColumns]string{m.RemoteColName, m.RemoteColID, m.RemoteColSize, m.RemoteColCreated}
		view.processColumns = [remoteMaxColumns]components.TableColumn{{Width: 470}, {Width: 150}, {Width: 100, Alignment: components.TableAlignRight}, {Width: 0}}
		return view.processColumns[:4], view.columns[:4]
	case remoteVolumes:
		view.columns = [remoteMaxColumns]string{m.RemoteColName, m.RemoteColDriver, m.RemoteColScope}
		view.processColumns = [remoteMaxColumns]components.TableColumn{{Width: 470}, {Width: 150}, {Width: 0}}
		return view.processColumns[:3], view.columns[:3]
	case remoteNetworks:
		view.columns = [remoteMaxColumns]string{m.RemoteColName, m.RemoteColID, m.RemoteColDriver, m.RemoteColScope}
		view.processColumns = [remoteMaxColumns]components.TableColumn{{Width: 300}, {Width: 160}, {Width: 150}, {Width: 0}}
		return view.processColumns[:4], view.columns[:4]
	default:
		// Docker's CPU and memory figures sit beside the image, as they do in
		// docker stats. The status line takes the room a wide window leaves:
		// it is the only value whose length varies a lot, while the ID is
		// always twelve characters.
		view.columns = [remoteMaxColumns]string{
			m.RemoteColName, m.RemoteColImage, m.RemoteColCPU, m.RemoteColContainerMemory,
			m.RemoteColState, m.RemoteColID, m.RemoteColStatus,
		}
		view.processColumns = [remoteMaxColumns]components.TableColumn{
			{Width: 160}, {Width: 150}, {Width: 80, Alignment: components.TableAlignRight},
			{Width: 160, Alignment: components.TableAlignRight}, {Width: 80}, {Width: 120}, {Width: 0},
		}
		const fixed = 160 + 150 + 80 + 160 + 80 + 120
		if width < fixed+120 {
			scale := min(max(width-120, 1)/fixed, 1)
			for index := range view.processColumns[:6] {
				view.processColumns[index].Width *= scale
			}
		}
		return view.processColumns[:7], view.columns[:7]
	}
}

// remoteToolsVariant is the look of a remote tool's action: a destructive or
// secondary action keeps its own, and every other is an outline button, so the
// tool's actions read as one quiet row.
func remoteToolsVariant(variant components.ButtonVariant) components.ButtonVariant {
	switch variant {
	case components.ButtonDestructive, components.ButtonSecondary:
		return variant
	}
	return components.ButtonOutline
}
