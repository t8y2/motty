package workbench

import (
	"fmt"

	"github.com/vivaui/viva/base"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

// proxySectionUI is what the proxies page's declaration leaves for its paint.
type proxySectionUI struct {
	add, batchAdd, batchDelete, batchStart, batchStop, batchClear, empty                        ui.Box
	hubToggle, hubUnbind                                                                        ui.Box
	addWidth, batchAddWidth, batchDeleteWidth, batchStartWidth, batchStopWidth, batchClearWidth float32
	hubToggleWidth, hubUnbindWidth                                                              float32
	addIcon, batchAddIcon, batchDeleteIcon, batchStartIcon, batchStopIcon, batchClearIcon       uint8
	hubToggleIcon, hubUnbindIcon                                                                uint8
	condensed                                                                                   bool
	// listScroll is the page list's scroll area, so switching hubs can return
	// the rows to the top.
	listScroll *ui.ScrollState
	// cardOver paints every visible route card after the page laid the rows
	// out. It is kept so a paint makes no closure.
	cardOver func(*ui.DrawContext)
	// draw is the page hook that paints into these slots. It is kept so a paint
	// does not build a closure for the hook.
	draw func(*ui.DrawContext)
}

// declareProxiesWorkspace declares the proxies page in the workbench page: its
// header, its open/empty body and its footer. The buttons, the table and the
// empty state's icon are painted into the slots it leaves.
func (state *State) declareProxiesWorkspace(c ui.C) {
	canvas := c.Canvas()
	pal, messages := state.pal, state.messages
	checkedCount := state.proxyCheckedCount()

	const (
		titleLeast = float32(220)
		gap        = float32(8)
	)
	bounds := state.catalogSectionBounds()
	canAdd := state.canAddJumpRoute()
	canBatch := state.canBatchAssociateProxies()
	_, hubSelected := state.selectedBastionHub()

	var (
		addWidth, batchAddWidth, batchDeleteWidth, batchStartWidth, batchStopWidth, batchClearWidth float32
		hubToggleWidth, hubUnbindWidth                                                              float32
		addIcon, batchAddIcon, batchDeleteIcon, batchStartIcon, batchStopIcon, batchClearIcon       uint8
		hubToggleIcon, hubUnbindIcon                                                                uint8
		condensed                                                                                   bool
	)

	if checkedCount > 0 {
		batchDelLabel := fmt.Sprintf("%s (%d)", messages.ProxyBatchDelete, checkedCount)
		labeled := toolbarButtonWidth(canvas, messages.ProxyBatchClearSelection, false) + gap +
			toolbarButtonWidth(canvas, messages.ProxyBatchStopAll, false) + gap +
			toolbarButtonWidth(canvas, messages.ProxyBatchStartAll, false) + gap +
			toolbarButtonWidth(canvas, batchDelLabel, true) + gap + titleLeast
		condensed = labeled > bounds.Width-36
		batchClearWidth, batchClearIcon = toolbarButtonLayout(canvas, messages.ProxyBatchClearSelection, 0, 0, condensed)
		batchStopWidth, batchStopIcon = toolbarButtonLayout(canvas, messages.ProxyBatchStopAll, 0, iconStop, condensed)
		batchStartWidth, batchStartIcon = toolbarButtonLayout(canvas, messages.ProxyBatchStartAll, 0, iconPlay, condensed)
		batchDeleteWidth, batchDeleteIcon = toolbarButtonLayout(canvas, batchDelLabel, iconTrash, iconTrash, condensed)
	} else {
		var labeled float32
		if hubSelected {
			labeled = toolbarButtonWidth(canvas, messages.ProxyHubToggleAll, false) + gap +
				toolbarButtonWidth(canvas, messages.ProxyHubUnbindAll, false) + gap
		}
		labeled += toolbarButtonWidth(canvas, messages.ProxyBatchAssociate, false) + gap +
			toolbarButtonWidth(canvas, messages.ProxyAdd, true) + gap + titleLeast
		condensed = labeled > bounds.Width-36
		if hubSelected {
			hubToggleWidth, hubToggleIcon = toolbarButtonLayout(canvas, messages.ProxyHubToggleAll, 0, iconStop, condensed)
			hubUnbindWidth, hubUnbindIcon = toolbarButtonLayout(canvas, messages.ProxyHubUnbindAll, iconTrash, iconTrash, condensed)
		}
		batchAddWidth, batchAddIcon = toolbarButtonLayout(canvas, messages.ProxyBatchAssociate, iconPlus, iconPlus, condensed)
		addWidth, addIcon = toolbarButtonLayout(canvas, messages.ProxyAdd, iconPlus, iconPlus, condensed)
	}
	view := &state.sectionBodies.proxies
	draw, cardOver := view.draw, view.cardOver
	*view = proxySectionUI{
		addWidth: addWidth, batchAddWidth: batchAddWidth,
		batchDeleteWidth: batchDeleteWidth, batchStartWidth: batchStartWidth, batchStopWidth: batchStopWidth, batchClearWidth: batchClearWidth,
		hubToggleWidth: hubToggleWidth, hubUnbindWidth: hubUnbindWidth,
		addIcon: addIcon, batchAddIcon: batchAddIcon,
		batchDeleteIcon: batchDeleteIcon, batchStartIcon: batchStartIcon, batchStopIcon: batchStopIcon, batchClearIcon: batchClearIcon,
		hubToggleIcon: hubToggleIcon, hubUnbindIcon: hubUnbindIcon,
		condensed: condensed,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintProxiesEmpty(d.Canvas) }
	}
	if cardOver == nil {
		cardOver = state.paintProxyCards
	}
	view.draw, view.cardOver = draw, cardOver
	body := ui.Column(c).Fill().PadSides(12, 18, 0, 18)
	body.Children(func() {
		ui.Row(c).MinHeight(58).Gap(gap).MarginSides(0, 0, 12, 0).Items(ui.Center).Children(func() {
			ui.Column(c).Grow(1).Gap(3).MarginSides(4, 0, 0, 0).Children(func() {
				ui.Text(c, messages.ProxyTitle).FontSize(18).Weight(gfx.WeightSemibold).Color(pal.text).Ellipsis()
				desc := messages.ProxyDescription
				if checkedCount > 0 {
					desc = fmt.Sprintf(messages.ProxyBatchSelectedCountFmt, checkedCount)
				}
				ui.Paragraph(c, desc, 2).FontSize(metrics.FontSizeS).Color(pal.muted)
			})

			if checkedCount > 0 {
				batchDelLabel := fmt.Sprintf("%s (%d)", messages.ProxyBatchDelete, checkedCount)
				view.batchClear = state.declareButtonSized(c, hitProxyBatchClearSel, messages.ProxyBatchClearSelection, batchClearIcon, false, false, true, toolbarFont, condensed).Size(batchClearWidth, toolbarHeight)
				view.batchStop = state.declareButtonSized(c, hitProxyBatchStopAll, messages.ProxyBatchStopAll, batchStopIcon, false, false, true, toolbarFont, condensed).Size(batchStopWidth, toolbarHeight)
				view.batchStart = state.declareButtonSized(c, hitProxyBatchStartAll, messages.ProxyBatchStartAll, batchStartIcon, false, false, true, toolbarFont, condensed).Size(batchStartWidth, toolbarHeight)
				view.batchDelete = state.declareButtonSized(c, hitProxyBatchDelete, batchDelLabel, batchDeleteIcon, true, true, true, toolbarFont, condensed).Size(batchDeleteWidth, toolbarHeight)
			} else {
				if hub, ok := state.selectedBastionHub(); ok && len(hub.Routes) > 0 {
					view.hubToggle = state.declareButtonSized(c, hitProxyBatchToggleAll, messages.ProxyHubToggleAll, hubToggleIcon, false, false, true, toolbarFont, condensed).Size(hubToggleWidth, toolbarHeight)
					view.hubUnbind = state.declareButtonSized(c, hitProxyBatchRemove, messages.ProxyHubUnbindAll, hubUnbindIcon, false, true, true, toolbarFont, condensed).Size(hubUnbindWidth, toolbarHeight)
				}
				view.batchAdd = state.declareButtonSized(c, hitProxyBatchAdd, messages.ProxyBatchAssociate, batchAddIcon, false, false, canBatch, toolbarFont, condensed).Size(batchAddWidth, toolbarHeight)
				view.add = state.declareButtonSized(c, hitProxyAdd, messages.ProxyAdd, addIcon, true, false, canAdd, toolbarFont, condensed).Size(addWidth, toolbarHeight)
			}
		})

		if len(state.proxies) == 0 {
			view.empty = state.reserveEmptyPage(c, bounds, messages.ProxyEmpty, messages.ProxyEmptySub, messages.ProxyAdd, hitProxyAdd, iconGlobe, canAdd)
		} else {
			// 中枢切换分段控制器 Tab 栏（Segmented Control 质感）
			hubs := state.bastionHubs
			if len(hubs) > 0 {
				ui.Row(c).Height(40).PadSides(4, 4, 4, 4).Gap(4).MarginSides(0, 0, 10, 0).Items(ui.Center).
					Background(pal.surfaceSoft).Radius(radius.control).Children(func() {
					// 全部路由
					allSelected := state.selectedBastionJumpID == 0
					allLabel := messages.HostFilterAll
					state.declareSegmentedHubTab(c, hitProxyHubTabList.At(0), allLabel, state.bastionAllCountText, allSelected, false, 0)

					for idx := range hubs {
						hub := &hubs[idx]
						isSelected := state.selectedBastionJumpID == hub.JumpHost.ID
						state.declareSegmentedHubTab(c, hitProxyHubTabList.At(idx+1), hub.JumpHost.Label, hub.CountLabel, isSelected, true, hub.Active)
					}
				})
			}
			state.syncProxyList()
			maxHops := 2
			count := state.visibleProxyCount()
			for i := range count {
				if r, ok := state.visibleProxyAt(i); ok && len(r.hops) > maxHops {
					maxHops = len(r.hops)
				}
			}
			// 基于当前表体可用宽度动态分配弹性列宽，确保在 960px 最小窗口下第 5 列操作按钮区始终至少保留 76px
			const minActionsW float32 = 76
			const col0W float32 = 42
			const col3W float32 = 108
			availW := max(bounds.Width-36-float32(catalogBarGutter), 400)
			flexW := max(availW-col0W-col3W-minActionsW, 260)
			col1W := min(float32(180), max(float32(100), flexW*0.28))
			col2W := min(float32(180), max(float32(100), flexW*0.28))
			desiredRouteW := max(float32(260), float32(maxHops)*104+float32(maxHops-1)*24+16)
			maxRouteW := max(float32(120), flexW-col1W-col2W)
			routeColWidth := min(desiredRouteW, maxRouteW)
			state.proxyColumns = [...]components.TableColumn{
				{Width: col0W, Alignment: components.TableAlignCenter},
				{Width: col1W},
				{Width: col2W},
				{Width: col3W},
				{Width: routeColWidth},
				{Width: 0},
			}
			state.proxyHeadings = [...]string{"", messages.ProxyColumnTarget, messages.ProxyColumnJump, messages.ProxyColumnStatus, messages.ProxyColumnRoute, ""}
			components.FitTableHeadings(canvas, state.Theme(), state.proxyColumns[:], state.proxyHeadings[:])
			// 若表头适配后前 5 列之和挤压了操作按钮列，优先从路径拓扑列回收多余宽度
			usedW := state.proxyColumns[0].Width + state.proxyColumns[1].Width + state.proxyColumns[2].Width + state.proxyColumns[3].Width + state.proxyColumns[4].Width
			if overflow := usedW - (availW - minActionsW); overflow > 0 {
				shrink := min(overflow, max(state.proxyColumns[4].Width-100, 0))
				state.proxyColumns[4].Width -= shrink
			}
			// The rows are the page's list. Its height, as the body lays it out,
			// decides whether the rows overflow and so keep the bar's gutter
			// clear, exactly as the canvas table did.
			listHeight := bounds.Height - 123 - 52
			if len(hubs) > 0 {
				listHeight -= 50
			}
			overflow := float32(count)*52 > listHeight
			theme := state.Theme()
			headerPad := theme.Metrics.GapItem
			totalVis, checkedVis := state.proxyVisibleStats()
			allChecked := totalVis > 0 && totalVis == checkedVis
			ui.Column(c).Grow(1).Shrink(1).MinHeight(0).Children(func() {
				header := ui.Row(c).Height(52).Shrink(0).FillWidth().Gap(0).Items(ui.Center)
				if overflow {
					header = header.PadSides(0, catalogBarGutter, 0, 0)
				}
				header.Children(func() {
					ui.Row(c).Width(state.proxyColumns[0].Width).Height(52).Shrink(0).Items(ui.Center).Justify(ui.Center).Children(func() {
						ui.Checkbox(c, state.pageFlag(allChecked), "").Hit(hitProxyCheckAll, messages.MenuSelectAll)
					})
					for column := 1; column < len(state.proxyColumns)-1; column++ {
						width := state.proxyColumns[column].Width
						if width <= 0 {
							break
						}
						ui.Text(c, state.proxyHeadings[column]).Weight(gfx.WeightMedium).PadX(headerPad).Width(width).Shrink(0).Ellipsis()
					}
					ui.Slot(c).Grow(1)
					ui.Slot(c).Absolute().Left(0).Right(0).Bottom(0).Height(1).Background(theme.Border)
				})
				list := ui.List(c, &state.proxyList, count, state.proxyRowDecl).Grow(1).Shrink(1).MinHeight(0).FillWidth()
				if overflow {
					list = list.PadSides(0, catalogBarGutter, 0, 0)
				}
				list.AlwaysScrollbars().ScrollbarSize(catalogBarGutter).Label(messages.ProxyListScroll).
					DrawOver(view.cardOver)
				view.listScroll = list.ScrollState()
				if state.proxyList.Changed() {
					state.takeProxyListSelection()
				}
			})
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(40).PadSides(8, 0, 16, 0).Items(ui.Center).Children(func() {
			ui.Text(c, state.proxyCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint)
		})
	})
	body.Clip().DrawOver(view.draw)
}

func (state *State) declareSegmentedHubTab(c ui.C, id gfx.ID, label, countLabel string, selected bool, isHub bool, active int) ui.Box {
	pal := state.pal
	foreground := pal.muted
	var fill gfx.Color
	if selected {
		fill, foreground = pal.surfaceHot, pal.text
	}
	tabBox := ui.ButtonBase(c, label).Height(32).Radius(radius.control-2).Shrink(0).PadSides(0, 12, 0, 12).Gap(6).Items(ui.Center).
		Background(fill).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Selected: selected}).
		Focus(ui.Look{}.Ring(1.5, pal.accent))
	if !selected {
		tabBox = tabBox.Hover(ui.Look{}.Background(pal.surface.WithAlpha(0.5))).
			Pressed(ui.Look{}.Background(pal.surfaceHot))
	}
	tabBox.Children(func() {
		// 跳板机健康状态微圆点
		if isHub {
			dotColor := pal.accent
			if active == 0 {
				dotColor = pal.muted
			}
			ui.Slot(c).Size(6, 6).Radius(3).Background(dotColor).Shrink(0)
		}
		ui.Text(c, label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(foreground)

		// 数量小胶囊
		ui.Row(c).Height(18).PadSides(0, 6, 0, 6).Items(ui.Center).
			Background(pal.workspace.WithAlpha(0.4)).Radius(999).Children(func() {
			ui.Text(c, countLabel).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
	})
	return tabBox
}

// declareProxyRow declares one route row of the page list: its selection box,
// the target and jump hosts, the switch and status, the route card and the
// row's edit and delete buttons.
func (state *State) declareProxyRow(row ui.ListRow) {
	route, ok := state.visibleProxyAt(row.Index)
	if !ok {
		return
	}
	c := row.C
	theme, pal, messages := state.Theme(), state.pal, state.messages
	const height = float32(52)
	// The route table never painted a row fill: selection and the pointer show in
	// the details pane beside it, not on the row itself.
	element := row.Element.Height(height).Shrink(0).FillWidth().Gap(0).Items(ui.Center)
	element.Children(func() {
		ui.Row(c).Width(state.proxyColumns[0].Width).Height(height).Shrink(0).Items(ui.Center).Justify(ui.Center).Children(func() {
			checked := state.proxyChecked != nil && state.proxyChecked[route.ID]
			ui.Checkbox(c, state.pageFlag(checked), "").Label(route.label).Hit(hitProxyRowCheckList.At(int(route.ID)), route.label)
		})
		for column := 1; column < 3; column++ {
			width := state.proxyColumns[column].Width
			if width <= 0 {
				break
			}
			ui.Text(c, route.cells[column-1]).PadX(theme.Metrics.GapItem).Width(width).Shrink(0).Ellipsis()
		}
		ui.Row(c).Width(state.proxyColumns[3].Width).Height(height).Shrink(0).Gap(8).Items(ui.Center).Children(func() {
			// The page dresses its switches differently from these rows' old
			// hand-painted switch: this one keeps the components' own look.
			ui.Switch(c, state.pageFlag(!route.Stopped), "").Label(route.label).Small().Size(24, 14).
				SwitchRecipe(components.SwitchRecipe).
				MarginSides(0, 0, 0, theme.Metrics.GapItem).Shrink(0).
				Hit(hitProxyRowSwitchList.At(int(route.ID)), route.label)
			statusText, statusColor := messages.ProxyRunning, pal.accent
			if route.Stopped {
				statusText, statusColor = messages.ProxyStopped, pal.muted
			}
			ui.Text(c, statusText).FontSize(metrics.FontSizeS).Color(statusColor).LineHeight(16).
				Grow(1).MarginSides(0, 4, 0, 0).Ellipsis()
		})
		ui.Slot(c).Width(state.proxyColumns[4].Width).Height(height).Shrink(0)
		ui.Row(c).Grow(1).Height(height).Justify(ui.End).Items(ui.Center).Gap(6).PadSides(0, 8, 0, 0).Children(func() {
			state.declareProxyRowAction(c, hitProxyRowEditList.At(int(route.ID)), iconPencil, messages.ProxyActionEdit, false)
			state.declareProxyRowAction(c, hitProxyRowDeleteList.At(int(route.ID)), iconTrash, messages.ProxyActionDelete, true)
		})
	})
	element.Hit(hitProxyRowList.At(row.Index), route.cells[0])
}

// declareProxyRowAction declares a row's pencil or bin button, keeping the
// wash the hand-painted one showed while the pointer was on it.
func (state *State) declareProxyRowAction(c ui.C, id gfx.ID, icon uint8, hint string, danger bool) {
	pal := state.pal
	hovered := false
	if canvas := c.Canvas(); canvas != nil {
		hovered = canvas.Hovered() == id
	}
	foreground := pal.muted
	button := ui.ButtonBase(c, hint).Size(24, 24).Radius(radius.control).Shrink(0).
		HitWith(id, hint, gfx.HitOptions{Role: gfx.RoleButton})
	if hovered {
		if danger {
			foreground = pal.danger
			button = button.Background(pal.danger.WithAlpha(0.15))
		} else {
			foreground = pal.text
			button = button.Background(pal.surfaceHot)
		}
	}
	button.Children(func() {
		ui.Icon(c, lucideIcons[icon]).Size(16, 16).Color(foreground)
	})
}

// paintProxyCards paints the topology cards of the rows the list shows. The
// cards sit in one column of every row, so one pass over the visible rows paints
// them without declaring a control per row.
func (state *State) paintProxyCards(d *ui.DrawContext) {
	view := &state.sectionBodies.proxies
	if view.listScroll == nil {
		return
	}
	rect := d.Rect
	if rect.Width <= 0 || rect.Height <= 0 {
		return
	}
	count := state.visibleProxyCount()
	offset := view.listScroll.Offset
	x := rect.X + state.proxyColumns[0].Width + state.proxyColumns[1].Width +
		state.proxyColumns[2].Width + state.proxyColumns[3].Width
	width := state.proxyColumns[4].Width
	const pitch float32 = 52
	first := max(0, int(offset/pitch))
	last := min(count, int((offset+rect.Height)/pitch)+1)
	d.Canvas.Clip(rect)
	for row := first; row < last; row++ {
		route, ok := state.visibleProxyAt(row)
		if !ok {
			continue
		}
		state.paintProxyHops(d.Canvas, gfx.Rect{
			X: x, Y: rect.Y + float32(row)*pitch - offset, Width: width, Height: pitch,
		}, route)
	}
	d.Canvas.Unclip()
}

// paintProxyHops paints the route's topology card in its cell: the hops side by
// side with arrows between them.
func (state *State) paintProxyHops(canvas *gfx.Canvas, c4 gfx.Rect, route *proxyRecord) {
	hops := route.hops
	if len(hops) == 0 {
		hops = []proxyHop{
			{ID: route.JumpID, Label: route.cells[1], Address: "Jump"},
			{ID: route.TargetID, Label: route.cells[0], Address: "Target"},
		}
	}
	cardW := float32(104)
	cardH := float32(38)
	cardY := c4.Y + (c4.Height-cardH)/2
	flowW := float32(24)
	canvas.Clip(c4)
	for i, hop := range hops {
		nodeX := c4.X + 6 + float32(i)*(cardW+flowW)
		canvas.Rounded(gfx.Rect{X: nodeX, Y: cardY, Width: cardW, Height: cardH}, cornerPx(6), state.pal.surfaceSoft.WithAlpha(0.55))
		canvas.Label(hop.Label, gfx.Rect{X: nodeX + 8, Y: cardY + 4, Width: cardW - 16, Height: 15},
			gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.text})
		canvas.Label(hop.Address, gfx.Rect{X: nodeX + 8, Y: cardY + 20, Width: cardW - 16, Height: 13},
			gfx.TextStyle{Size: 11, Color: state.pal.muted})
		if i < len(hops)-1 {
			arrowX := nodeX + cardW
			midY := c4.Y + c4.Height/2
			canvas.Fill(gfx.Rect{X: arrowX + 2, Y: midY, Width: 10, Height: 1}, state.pal.line)
			canvas.DrawIcon(icons.ChevronRight, gfx.Rect{X: arrowX + 10, Y: midY - 6, Width: 12, Height: 12}, state.pal.muted)
			if route.ViaShell && i == len(hops)-2 {
				canvas.Label(state.messages.ProxyViaShellTag, gfx.Rect{X: arrowX - 4, Y: cardY - 2, Width: flowW + 8, Height: 10},
					gfx.TextStyle{Size: 8, Color: state.pal.accent})
			}
		}
	}
	canvas.Unclip()
}

// declareProxyDialog declares the route editor as a dialog of the workbench
// page.
func (state *State) declareProxyDialog(c ui.C) {
	messages := state.messages
	dialogTitle := messages.ProxyDialogTitle
	submitLabel := messages.ProxyCreate
	if state.proxyEditor.editing {
		dialogTitle = messages.ProxyEditDialogTitle
		submitLabel = messages.ProxyRouteSave
	}
	bodyHeight := withError(state.proxyDialogBodyHeight(c.Theme()), state.proxyDialogError)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 500, "", bodyHeight)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: dialogTitle, Width: 500, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitProxyDialogClose,
	})
	dialog.Body(func() { state.declareProxyDialogBody(c) })
	dialog.Footer(func() {
		if dialogButton(c, messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.clickProxyDialog(hitProxyDialogCancel) })
		}
		if dialogButton(c, submitLabel, components.ButtonDefault, false).Clicked() {
			c.After(func() { state.clickProxyDialog(hitProxyDialogSubmit) })
		}
	})
}

// declareProxyDialogBody lays out the route dialog: the target and jump hosts,
// the shell-relay box and, when it is on, the relay command and its note.
func (state *State) declareProxyDialogBody(c ui.C) {
	editor, messages := &state.proxyEditor, state.messages
	viaShell := editor.viaShell
	b := &editor.dialogBody
	*b = proxyDialogBody{}
	rowHeight := dialogRowHeight(c.Theme())
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		ui.FormField(c, &editor.targetRow, messages.ProxyFieldTarget).Height(rowHeight).Children(func() {
			chooser := ui.Combobox(c, &editor.targetChoice, state.proxyTargetChoices(), state.hostPickerName, messages.ProxyFieldTarget).
				Placeholder(messages.HostPickerSearch).Empty(messages.HostPickerNoMatches).FillWidth()
			if editor.focusTarget {
				chooser.AutoFocus()
				editor.focusTarget = false
			}
		})
		if editor.targetChoice != editor.targetID {
			state.applyProxyTargetChoice(editor.targetChoice)
			editor.targetChoice = editor.targetID
		}
		ui.FormField(c, &editor.jumpRow, messages.ProxyFieldJump).Height(rowHeight).Children(func() {
			ui.Combobox(c, &editor.jumpChoice, state.proxyJumpChoices(), state.hostPickerName, messages.ProxyFieldJump).
				Placeholder(messages.HostPickerSearch).Empty(messages.HostPickerNoMatches).FillWidth()
		})
		if editor.jumpChoice != editor.jumpID {
			state.applyProxyJumpChoice(editor.jumpChoice)
			editor.jumpChoice = editor.jumpID
		}
		_, b.viaHelp = state.declareDialogCheckHelp(c, hitProxyDialogViaShell, messages.ProxyFieldViaShell, state.proxyEditor.viaShell, dialogCheckGapAbove, dialogCheckGapBelow)
		if viaShell {
			if !editor.relayBound {
				editor.relayText.Bind(&editor.relay)
				editor.relayBound = true
			}
			ui.FormField(c, &editor.relayRow, messages.ProxyFieldRelay).FillWidth().Height(rowHeight).Children(func() {
				ui.TextFieldID(c, hitProxyDialogRelay, &editor.relayText, messages.ProxyFieldRelay, messages.ProxyRelayPlaceholder)
			})
			ui.Paragraph(c, state.proxyRelayNote(), 2).FontSize(metrics.FontSizeS).Color(state.pal.muted).MarginSides(0, 6, 0, 6)
		}
		if state.proxyDialogError != "" {
			ui.Text(c, state.proxyDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	column.DrawOver(func(d *ui.DrawContext) { state.paintProxyDialogFields(d.Canvas) })
}

// paintProxyDialogFields paints the route editor's help icon into the slot the
// body laid out.
func (state *State) paintProxyDialogFields(canvas *gfx.Canvas) {
	messages := state.messages
	b := &state.proxyEditor.dialogBody
	state.paintHelpIcon(canvas, hitProxyDialogViaHelp, messages.ProxyViaShellHelp, b.viaHelp.Rect())
}

// proxyDialogBodyHeight is the height of the dialog's fields: the two hosts, the
// choice of relaying through the jump host's shell, and when it is on the relay
// command with the note under it.
func (state *State) proxyDialogBodyHeight(theme *base.Theme) float32 {
	height := 2*dialogRowHeight(theme) + dialogCheckRowHeight // two fields, then the checkbox
	if state.proxyEditor.viaShell {
		height += dialogRowHeight(theme) + 44 // the relay field, then a note of two lines
	}
	return height
}

// proxyRelayNote says what is typed on the jump host before the relay starts.
func (state *State) proxyRelayNote() string {
	if jump, ok := state.hostByID(state.proxyEditor.jumpID); ok && jump.relayPrelude() != "" {
		return fmt.Sprintf(state.messages.ProxyRelayPreludeFmt, jump.relayPrelude())
	}
	return state.messages.ProxyRelayNoPrelude
}

// declareProxyDeleteConfirmDialog 声明单条跳板路由删除确认弹窗
func (state *State) declareProxyDeleteConfirmDialog(c ui.C) {
	route, ok := state.proxyByID(state.proxyPendingDeleteID)
	if !ok {
		state.modal = modalNone
		return
	}
	messages := state.messages
	desc := fmt.Sprintf(messages.ProxyDeleteConfirmPromptFmt, route.label)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, desc, 0)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: messages.ProxyDeleteConfirmTitle, Description: desc,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitProxyDeleteConfirmClose, InitialFocusID: hitProxyDeleteConfirmCancel,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitProxyDeleteConfirmCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitProxyDeleteConfirmSubmit, Label: messages.ProxyActionDelete, Variant: components.ButtonDestructive},
		)
	})
}

// declareProxyBatchDeleteConfirmDialog 声明批量删除跳板路由确认弹窗
func (state *State) declareProxyBatchDeleteConfirmDialog(c ui.C) {
	count := state.proxyCheckedCount()
	if count == 0 {
		state.modal = modalNone
		return
	}
	messages := state.messages
	desc := fmt.Sprintf(messages.ProxyBatchDeleteConfirmPromptFmt, count)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 480, desc, 0)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: messages.ProxyBatchDeleteConfirmTitle, Description: desc,
		Width: 480, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitProxyBatchDeleteConfirmClose, InitialFocusID: hitProxyBatchDeleteConfirmCancel,
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitProxyBatchDeleteConfirmCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitProxyBatchDeleteConfirmSubmit, Label: fmt.Sprintf("%s (%d)", messages.ProxyBatchDelete, count), Variant: components.ButtonDestructive},
		)
	})
}

// paintProxiesEmpty paints the proxies page's empty state into the slot its
// declaration left. A page with routes is declared by the page itself.
func (state *State) paintProxiesEmpty(canvas *gfx.Canvas) {
	view := &state.sectionBodies.proxies
	if len(state.proxies) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconGlobe)
	}
}
