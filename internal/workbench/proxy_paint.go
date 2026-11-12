package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/workbench/icons"
)

// proxySectionUI is what the proxies page's declaration leaves for its paint.
type proxySectionUI struct {
	scroll                                                                                      ui.Box
	content, add, batchAdd, batchDelete, batchStart, batchStop, batchClear, empty               ui.Box
	addWidth, batchAddWidth, batchDeleteWidth, batchStartWidth, batchStopWidth, batchClearWidth float32
	addIcon, batchAddIcon, batchDeleteIcon, batchStartIcon, batchStopIcon, batchClearIcon       uint8
	condensed                                                                                   bool
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
	canAdd := state.sshHostCount() >= 2

	var (
		addWidth, batchAddWidth, batchDeleteWidth, batchStartWidth, batchStopWidth, batchClearWidth float32
		addIcon, batchAddIcon, batchDeleteIcon, batchStartIcon, batchStopIcon, batchClearIcon       uint8
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
		labeled := toolbarButtonWidth(canvas, messages.ProxyBatchAssociate, true) + gap +
			toolbarButtonWidth(canvas, messages.ProxyAdd, true) + gap + titleLeast
		condensed = labeled > bounds.Width-36
		batchAddWidth, batchAddIcon = toolbarButtonLayout(canvas, messages.ProxyBatchAssociate, iconPlus, iconPlus, condensed)
		addWidth, addIcon = toolbarButtonLayout(canvas, messages.ProxyAdd, iconPlus, iconPlus, condensed)
	}

	view := &state.sectionBodies.proxies
	draw := view.draw
	*view = proxySectionUI{
		addWidth: addWidth, batchAddWidth: batchAddWidth,
		batchDeleteWidth: batchDeleteWidth, batchStartWidth: batchStartWidth, batchStopWidth: batchStopWidth, batchClearWidth: batchClearWidth,
		addIcon: addIcon, batchAddIcon: batchAddIcon,
		batchDeleteIcon: batchDeleteIcon, batchStartIcon: batchStartIcon, batchStopIcon: batchStopIcon, batchClearIcon: batchClearIcon,
		condensed: condensed,
	}
	if draw == nil {
		draw = func(d *ui.DrawContext) { state.paintProxiesWorkspace(d.Canvas) }
	}
	view.draw = draw
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
				view.batchAdd = state.declareButtonSized(c, hitProxyBatchAdd, messages.ProxyBatchAssociate, batchAddIcon, false, false, canAdd, toolbarFont, condensed).Size(batchAddWidth, toolbarHeight)
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
					state.declareSegmentedHubTab(c, hitProxyHubTabList.At(0), allLabel, len(state.proxies), allSelected, false, 0)

					for idx := range hubs {
						hub := &hubs[idx]
						isSelected := state.selectedBastionJumpID == hub.JumpHost.ID
						state.declareSegmentedHubTab(c, hitProxyHubTabList.At(idx+1), hub.JumpHost.Label, hub.Total, isSelected, true, hub.Active)
					}
				})
			}
			maxHops := 2
			for _, r := range state.proxies {
				if len(r.hops) > maxHops {
					maxHops = len(r.hops)
				}
			}
			routeColWidth := max(float32(260), float32(maxHops)*104+float32(maxHops-1)*24+16)
			minContentWidth := max(float32(880), 42+180+180+130+routeColWidth+72)
			view.scroll = ui.Scroll(c).Grow(1).Sideways().Scrollbars().MarginSides(0, 0, 9, 0)
			view.scroll.Children(func() {
				view.content = ui.Slot(c).MinWidth(minContentWidth).Fill()
			})
		}
		ui.Divider(c).Background(pal.line)
		ui.Row(c).Height(40).PadSides(8, 0, 16, 0).Items(ui.Center).Children(func() {
			ui.Text(c, state.proxyCountText).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.faint)
		})
	})
	body.Clip().DrawOver(view.draw)
}

func (state *State) declareSegmentedHubTab(c ui.C, id gfx.ID, label string, count int, selected bool, isHub bool, active int) ui.Box {
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
		countStr := fmt.Sprintf("%d", count)
		ui.Row(c).Height(18).PadSides(0, 6, 0, 6).Items(ui.Center).
			Background(pal.workspace.WithAlpha(0.4)).Radius(999).Children(func() {
			ui.Text(c, countStr).FontSize(metrics.FontSizeS).Color(pal.muted)
		})
	})
	return tabBox
}

// paintProxiesWorkspace paints the proxies page's buttons, table and empty state
// into the slots its declaration left.
func (state *State) paintProxiesWorkspace(canvas *gfx.Canvas) {
	view, messages, pal := &state.sectionBodies.proxies, state.messages, state.pal

	if len(state.proxies) == 0 {
		state.drawReservedEmptyPage(canvas, view.empty, iconGlobe)
		return
	}

	clipped := view.scroll.Rect().Width > 0
	if clipped {
		canvas.Clip(view.scroll.Rect())
	}

	maxHops := 2
	for _, r := range state.proxies {
		if len(r.hops) > maxHops {
			maxHops = len(r.hops)
		}
	}
	routeColWidth := max(float32(260), float32(maxHops)*104+float32(maxHops-1)*24+16)

	// 路径列固定 routeColWidth，尾部保留弹性列，操作按钮固定吸附在视口最右侧
	columns := [...]components.TableColumn{
		{Width: 42, Alignment: components.TableAlignCenter},
		{Width: 180},
		{Width: 180},
		{Width: 130},
		{Width: routeColWidth},
		{Width: 0},
	}
	headings := [...]string{"", messages.ProxyColumnTarget, messages.ProxyColumnJump, messages.ProxyColumnStatus, messages.ProxyColumnRoute, ""}
	components.FitTableHeadings(canvas, state.Theme(), columns[:], headings[:])
	table := components.BeginTable(canvas, state.Theme(), view.content.Rect(), columns[:]).RowHeight(52)
	table.Header(headings[:])

	// 1. 绘制表头 Checkbox
	totalVis, checkedVis := state.proxyVisibleStats()
	allChecked := totalVis > 0 && totalVis == checkedVis
	theme := state.Theme()
	side := components.CheckboxExtent(theme)
	headerRect := gfx.Rect{X: view.content.Rect().X, Y: view.content.Rect().Y, Width: 42, Height: components.TableHeaderHeight(theme)}
	boxRect := gfx.Rect{
		X:      headerRect.X + (headerRect.Width-side)/2,
		Y:      headerRect.Y + (headerRect.Height-side)/2,
		Width:  side,
		Height: side,
	}
	components.BeginCheckbox(canvas, theme, hitProxyCheckAll, boxRect, allChecked).Label(messages.MenuSelectAll).Paint()

	// 2. 依据当前选中的跳板机过滤展示每一行
	swWidth, swHeight := components.SwitchExtent(theme, true)
	btnSize := float32(24)

	for _, route := range state.proxies {
		if state.selectedBastionJumpID != 0 && route.JumpID != state.selectedBastionJumpID {
			continue
		}
		var cellRects [6]gfx.Rect
		var rowCells [6]string
		rowCells[0] = ""
		rowCells[1] = route.cells[0]
		rowCells[2] = route.cells[1]
		rowCells[3] = ""
		rowCells[4] = ""
		rowCells[5] = ""

		isChecked := state.proxyChecked != nil && state.proxyChecked[route.ID]
		row := table.RowLayout(hitProxyRowList.At(int(route.ID)), rowCells[:], components.TableRowState{Selected: false}, cellRects[:0])
		if len(row.Cells) != 6 {
			continue
		}
		if clicked, ok := canvas.ClickedIn(hitProxyRowList); ok && clicked == int(route.ID) && state.modal == modalNone {
			state.selectProxy(route.ID)
		}

		// 列 0: 勾选框
		cell0 := row.Cells[0]
		cboxRect := gfx.Rect{
			X:      cell0.X + (cell0.Width-side)/2,
			Y:      cell0.Y + (cell0.Height-side)/2,
			Width:  side,
			Height: side,
		}
		components.BeginCheckbox(canvas, theme, hitProxyRowCheckList.At(int(route.ID)), cboxRect, isChecked).Paint()

		// 列 3: Switch 开关与状态文字
		cell3 := row.Cells[3]
		swRect := gfx.Rect{
			X:      cell3.X + theme.Metrics.GapItem,
			Y:      cell3.Y + (cell3.Height-swHeight)/2,
			Width:  swWidth,
			Height: swHeight,
		}
		switchID := hitProxyRowSwitchList.At(int(route.ID))
		components.BeginSwitch(canvas, theme, switchID, swRect, !route.Stopped).Small().Label(route.cells[2]).Paint()

		statusText := state.messages.ProxyRunning
		statusColor := pal.accent
		if route.Stopped {
			statusText = state.messages.ProxyStopped
			statusColor = pal.muted
		}
		stRect := gfx.Rect{
			X:      swRect.X + swWidth + 8,
			Y:      cell3.Y + (cell3.Height-16)/2,
			Width:  max(0, cell3.Width-swWidth-16),
			Height: 16,
		}
		canvas.Label(statusText, stRect, gfx.TextStyle{Size: metrics.FontSizeS, Color: statusColor})

		// 列 4: 路径精美拓扑卡片呈现（支持单跳与多跳完整链路，卡片与箭头尺寸恒定）
		cell4 := row.Cells[4]
		hops := route.hops
		cardW := float32(104)
		cardH := float32(38)
		cardY := cell4.Y + (52-cardH)/2
		flowW := float32(24)

		for i, hop := range hops {
			nodeX := cell4.X + 6 + float32(i)*(cardW+flowW)
			// 1) 节点微卡片
			canvas.Rounded(gfx.Rect{X: nodeX, Y: cardY, Width: cardW, Height: cardH}, 6, pal.surfaceSoft.WithAlpha(0.55))
			canvas.Label(hop.Label, gfx.Rect{X: nodeX + 8, Y: cardY + 4, Width: cardW - 16, Height: 15},
				gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: pal.text})
			canvas.Label(hop.Address, gfx.Rect{X: nodeX + 8, Y: cardY + 20, Width: cardW - 16, Height: 13},
				gfx.TextStyle{Size: 11, Color: pal.muted})

			// 2) 如果不是最后一个节点，绘制流向箭头
			if i < len(hops)-1 {
				arrowX := nodeX + cardW
				midY := cell4.Y + 26
				canvas.Fill(gfx.Rect{X: arrowX + 2, Y: midY, Width: 10, Height: 1}, pal.line)
				canvas.DrawIcon(icons.ChevronRight, gfx.Rect{X: arrowX + 10, Y: midY - 6, Width: 12, Height: 12}, pal.muted)
				// 若本路由包含终端中转，且是进入最终目标的最后一跳
				if route.ViaShell && i == len(hops)-2 {
					canvas.Label(state.messages.ProxyViaShellTag, gfx.Rect{X: arrowX - 4, Y: cardY - 2, Width: flowW + 8, Height: 10},
						gfx.TextStyle{Size: 8, Color: pal.accent})
				}
			}
		}

		// 4. 行尾操作按钮（编辑与删除，随表格一起水平滚动，不强制吸附遮挡）
		cell5 := row.Cells[5]
		btnY := cell5.Y + (52-btnSize)/2
		btnX := max(cell5.X+8, cell5.X+cell5.Width-btnSize*2-8)
		editRect := gfx.Rect{
			X:      btnX,
			Y:      btnY,
			Width:  btnSize,
			Height: btnSize,
		}
		delRect := gfx.Rect{
			X:      btnX + btnSize + 6,
			Y:      btnY,
			Width:  btnSize,
			Height: btnSize,
		}

		editID := hitProxyRowEditList.At(int(route.ID))
		editColor := pal.muted
		if canvas.Hovered() == editID {
			canvas.Rounded(editRect, radius.control, pal.surfaceHot)
			editColor = pal.text
		}
		canvas.DrawIcon(lucideIcons[iconPencil], gfx.Rect{X: editRect.X + 4, Y: editRect.Y + 4, Width: 16, Height: 16}, editColor)
		canvas.HitWithOptions(editID, editRect, state.messages.ProxyActionEdit, gfx.HitOptions{Role: gfx.RoleButton})

		delID := hitProxyRowDeleteList.At(int(route.ID))
		delColor := pal.muted
		if canvas.Hovered() == delID {
			canvas.Rounded(delRect, radius.control, pal.danger.WithAlpha(0.15))
			delColor = pal.danger
		}
		canvas.DrawIcon(lucideIcons[iconTrash], gfx.Rect{X: delRect.X + 4, Y: delRect.Y + 4, Width: 16, Height: 16}, delColor)
		canvas.HitWithOptions(delID, delRect, state.messages.ProxyActionDelete, gfx.HitOptions{Role: gfx.RoleButton})
	}

	if clipped {
		canvas.Unclip()
	}
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
	bodyHeight := withError(state.proxyDialogBodyHeight(), state.proxyDialogError)
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 500, "", bodyHeight)
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: dialogTitle, Width: 500, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitProxyDialogClose, InitialFocusID: hitProxyDialogTarget,
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
	const rowHeight = 64
	column := ui.Column(c).Fill().PadSides(2, 4, 0, 4)
	column.Children(func() {
		b.target = ui.Slot(c).Height(rowHeight)
		b.jump = ui.Slot(c).Height(rowHeight)
		b.via, b.viaHelp = state.declareDialogCheckboxHelpC(c, messages.ProxyFieldViaShell, 4, 6)
		if viaShell {
			b.relay = ui.Slot(c).Height(rowHeight)
			ui.Paragraph(c, state.proxyRelayNote(), 2).FontSize(metrics.FontSizeS).Color(state.pal.muted).MarginSides(0, 6, 0, 6)
		}
		if state.proxyDialogError != "" {
			ui.Text(c, state.proxyDialogError).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(state.pal.danger).MarginSides(6, 0, 0, 0)
		}
	})
	column.DrawOver(func(d *ui.DrawContext) { state.paintProxyDialogFields(d.Canvas) })
}

// paintProxyDialogFields paints the route editor's controls into the slots the
// body laid out.
func (state *State) paintProxyDialogFields(canvas *gfx.Canvas) {
	messages, theme := state.messages, state.Theme()
	b := &state.proxyEditor.dialogBody
	state.paintProxyHostField(canvas, b.target.Rect(), true)
	state.paintProxyHostField(canvas, b.jump.Rect(), false)
	state.paintDialogCheckbox(canvas, hitProxyDialogViaShell, messages.ProxyFieldViaShell, state.proxyEditor.viaShell, b.via)
	state.paintHelpIcon(canvas, hitProxyDialogViaHelp, messages.ProxyViaShellHelp, b.viaHelp.Rect())
	if state.proxyEditor.viaShell {
		content := components.BeginField(canvas, theme, b.relay.Rect()).Paint(messages.ProxyFieldRelay, "", "")
		value := state.proxyEditor.relay.String()
		focused := canvas.Focused() == hitProxyDialogRelay
		components.PaintInput(canvas, theme, content, components.InputSnapshot{
			ID: hitProxyDialogRelay, Label: messages.ProxyFieldRelay, Value: value, Placeholder: messages.ProxyRelayPlaceholder,
			Focused: focused, PlatformEditing: focused && value != "",
		})
	}
}

// proxyDialogBodyHeight is the height of the dialog's fields: the two hosts, the
// choice of relaying through the jump host's shell, and when it is on the relay
// command with the note under it.
func (state *State) proxyDialogBodyHeight() float32 {
	height := float32(130 + 30) // two fields, then the checkbox
	if state.proxyEditor.viaShell {
		height += 64 + 44 // the relay field, then a note of two lines
	}
	return height
}

// proxyRelayNote says what is typed on the jump host before the relay starts.
func (state *State) proxyRelayNote() string {
	if jump, ok := state.hostByID(state.proxyEditor.jumpID); ok && jump.LoginCommand != "" {
		return fmt.Sprintf(state.messages.ProxyRelayPreludeFmt, jump.LoginCommand)
	}
	return state.messages.ProxyRelayNoPrelude
}

// paintProxyHostField paints one searchable host picker. Typing in the field
// filters the list by name, address, account and group, so a long catalog is
// still reachable without scrolling.
func (state *State) paintProxyHostField(canvas *gfx.Canvas, bounds gfx.Rect, target bool) {
	theme := state.Theme()
	label := state.messages.ProxyFieldJump
	inputID, triggerID, dismissID := hitProxyDialogJump, hitProxyJumpTrigger, hitProxyJumpDismiss
	scrollID, itemBase := hitProxyJumpScroll, hitProxyJumpItemList
	scope := hitProxyJumpPickerScope
	control := &state.proxyEditor.jumpSelect
	if target {
		label = state.messages.ProxyFieldTarget
		inputID, triggerID, dismissID = hitProxyDialogTarget, hitProxyTargetTrigger, hitProxyTargetDismiss
		scrollID, itemBase = hitProxyTargetScroll, hitProxyTargetItemList
		scope = hitProxyTargetPickerScope
		control = &state.proxyEditor.targetSelect
	}
	content := components.BeginField(canvas, theme, bounds).Paint(label, "", "")
	layout := components.PaintSingleSelect(canvas, theme, content, control, components.SingleSelectOptions{
		InputID: inputID, TriggerID: triggerID, DismissID: dismissID, Scope: scope,
		ScrollID: scrollID, OptionIDs: itemBase,
		Label: label, Placeholder: state.messages.HostPickerSearch,
		EmptyLabel: state.messages.HostPickerNoMatches,
		Viewport:   state.windowRect(), Layer: dialogLayer + 1,
	})
	if target {
		state.proxyEditor.targetPopup = layout.Popup
		return
	}
	state.proxyEditor.jumpPopup = layout.Popup
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
