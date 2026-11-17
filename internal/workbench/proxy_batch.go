package workbench

import (
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

type proxyBatchAssociateDialog struct {
	jumpID         uint32
	jumpOpen       bool
	jumpFocusInput bool
	jumpQuery      text.Editor
	jumpScroll     components.MenuScroll
	jumpPopup      gfx.Rect
	jumpActiveID   gfx.ID
	query          text.Editor
	selected       map[uint32]bool
	overwrite      bool
	viaShell       bool
	relay          text.Editor
	error          string
	fromHostList   bool
	scroll         float32
	content        float32
	bounds         gfx.Rect
	listRect       gfx.Rect
	dialogBody     proxyBatchDialogBody
}

type proxyBatchDialogBody struct {
	jumpField    ui.Box
	searchField  ui.Box
	actionsRow   ui.Box
	listSlot     ui.Box
	overwriteBox ui.Box
	errorSlot    ui.Box
}

type proxyBatchCandidate struct {
	Host         Host
	ExistingJump string
	HasRoute     bool
	Disabled     bool
	Cycle        bool
}

// openProxyBatchAssociate 打开以指定跳板机为中枢的批量关联设备弹窗
func (state *State) openProxyBatchAssociate(jumpID uint32) {
	if !state.canBatchAssociateProxies() {
		state.showActionWarning(state.messages.ProxyBatchAssociateTitle, state.messages.ErrProxyHostsRequired)
		return
	}

	// 如果未指定 jumpID，或者指定的不合法，默认选第一台可用跳板机（不修改主界面筛选状态）
	if jumpID == 0 {
		if hub, ok := state.selectedBastionHub(); ok {
			jumpID = hub.JumpHost.ID
		} else if len(state.bastionHubs) > 0 {
			jumpID = state.bastionHubs[0].JumpHost.ID
		} else {
			for _, host := range state.hosts {
				if host.isSSH() {
					jumpID = host.ID
					break
				}
			}
		}
	}

	dialog := proxyBatchAssociateDialog{
		jumpID:    jumpID,
		selected:  make(map[uint32]bool),
		overwrite: true,
	}
	dialog.relay.Insert(defaultRelayCommand)
	state.proxyBatch = dialog
	state.modal = modalProxyBatchAssociate
}

// openProxyBatchAssignForSelectedHosts 从主机列表多选打开批量分配跳板机弹窗
func (state *State) openProxyBatchAssignForSelectedHosts(targetIDs []uint32) {
	if len(targetIDs) == 0 || !state.canBatchAssociateProxies() {
		return
	}

	// 候选跳板机：排除 targetIDs 中的主机，选一个非目标的 SSH 主机
	targetSet := make(map[uint32]bool, len(targetIDs))
	for _, id := range targetIDs {
		targetSet[id] = true
	}

	jumpID := uint32(0)
	for _, host := range state.hosts {
		if host.isSSH() && !targetSet[host.ID] {
			jumpID = host.ID
			break
		}
	}
	if jumpID == 0 {
		for _, host := range state.hosts {
			if host.isSSH() {
				jumpID = host.ID
				break
			}
		}
	}

	dialog := proxyBatchAssociateDialog{
		jumpID:       jumpID,
		selected:     targetSet,
		overwrite:    true,
		fromHostList: true,
	}
	dialog.relay.Insert(defaultRelayCommand)
	state.proxyBatch = dialog
	state.modal = modalProxyBatchAssociate
}

func (state *State) closeProxyBatchAssociate() {
	state.proxyBatch = proxyBatchAssociateDialog{}
	state.modal = modalNone
}

// proxyBatchJumpCandidates 获取当前筛选后的候选跳板主机列表（支持按主机名或 IP 地址模糊搜索）
func (state *State) proxyBatchJumpCandidates() []Host {
	dialog := &state.proxyBatch
	query := strings.ToLower(strings.TrimSpace(dialog.jumpQuery.String()))
	var candidates []Host
	for _, host := range state.hosts {
		if !host.isSSH() {
			continue
		}
		if query != "" {
			match := strings.Contains(strings.ToLower(host.Label), query) ||
				strings.Contains(strings.ToLower(host.Address), query)
			if !match {
				continue
			}
		}
		candidates = append(candidates, host)
	}
	return candidates
}

func (state *State) batchCandidates() []proxyBatchCandidate {
	dialog := &state.proxyBatch
	jumpID := dialog.jumpID
	query := strings.ToLower(strings.TrimSpace(dialog.query.String()))

	var candidates []proxyBatchCandidate
	for _, host := range state.hosts {
		if !host.isRouteTarget() {
			continue
		}
		if host.ID == jumpID {
			continue
		}

		if query != "" {
			match := strings.Contains(strings.ToLower(host.Label), query) ||
				strings.Contains(strings.ToLower(host.Address), query) ||
				strings.Contains(strings.ToLower(host.Group), query) ||
				strings.Contains(strings.ToLower(host.Tags), query)
			if !match {
				continue
			}
		}

		cand := proxyBatchCandidate{Host: host}
		if state.proxyWouldCycle(host.ID, jumpID) {
			cand.Disabled = true
			cand.Cycle = true
		}

		if route, has := state.proxyRouteForTarget(host.ID); has {
			cand.HasRoute = true
			if jhost, ok := state.hostByID(route.JumpID); ok {
				cand.ExistingJump = jhost.Label
			}
		}
		candidates = append(candidates, cand)
	}
	return candidates
}

func (state *State) batchCheckAllEligible() {
	dialog := &state.proxyBatch
	for _, cand := range state.batchCandidates() {
		if !cand.Disabled {
			dialog.selected[cand.Host.ID] = true
		}
	}
}

func (state *State) batchClearAll() {
	clear(state.proxyBatch.selected)
}

func (state *State) toggleBatchTarget(targetID uint32) {
	dialog := &state.proxyBatch
	if dialog.selected[targetID] {
		delete(dialog.selected, targetID)
	} else {
		dialog.selected[targetID] = true
	}
}

func (state *State) submitProxyBatchAssociate() {
	dialog := &state.proxyBatch
	if len(dialog.selected) == 0 {
		dialog.error = state.messages.ErrProxyTargetRequired
		return
	}
	if dialog.jumpID == 0 {
		dialog.error = state.messages.ErrProxyJumpRequired
		return
	}

	targetIDs := make([]uint32, 0, len(dialog.selected))
	for id, sel := range dialog.selected {
		if sel {
			targetIDs = append(targetIDs, id)
		}
	}

	relayCmd := ""
	if dialog.viaShell {
		relayCmd = strings.TrimSpace(dialog.relay.String())
		if relayCmd == "" {
			relayCmd = defaultRelayCommand
		}
	}

	added, updated := state.batchAssociateProxies(dialog.jumpID, targetIDs, dialog.overwrite, dialog.viaShell, relayCmd)
	if added+updated == 0 {
		// Name the reason the batch changed nothing instead of reporting a
		// loop for a conflict the user can resolve another way.
		cycle, taken := 0, 0
		for _, id := range targetIDs {
			if state.proxyWouldCycle(id, dialog.jumpID) {
				cycle++
				continue
			}
			if _, has := state.proxyRouteForTarget(id); has && !dialog.overwrite {
				taken++
			}
		}
		switch {
		case cycle > 0:
			dialog.error = state.messages.ErrProxyCycle
		case taken > 0:
			dialog.error = state.messages.ErrProxyTargetTaken
		default:
			dialog.error = state.messages.ErrProxyTargetRequired
		}
		return
	}

	state.closeProxyBatchAssociate()
}

// declareProxyBatchDialog 声明批量关联模态弹窗
func (state *State) declareProxyBatchDialog(c ui.C) {
	if state.modal != modalProxyBatchAssociate {
		return
	}
	messages, pal := state.messages, state.pal
	dialog := &state.proxyBatch

	const width float32 = 640
	const height float32 = 580

	d := ui.Dialog(c, ui.DialogSpec{
		Title: messages.ProxyBatchAssociateTitle,
		Width: width, Height: height,
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitProxyBatchClose, InitialFocusID: hitProxyBatchSearch,
	})

	d.Body(func() {
		ui.Column(c).Fill().PadSides(4, 10, 4, 10).Children(func() {
			// 1. 选择跳板主机选择器（整宽布局，与下方搜索和列表完美对齐）
			ui.Column(c).FillWidth().Gap(6).MarginSides(0, 0, 10, 0).Children(func() {
				ui.Text(c, messages.ProxyFieldJump).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text)
				ui.Slot(c).FillWidth().Height(34).Draw(func(d *ui.DrawContext) {
					state.paintProxyBatchJumpSelect(d.Canvas, d.Rect)
				})
			})

			// 2. 搜索框与全选/清空行
			ui.Row(c).Height(36).Items(ui.Center).Gap(8).MarginSides(0, 0, 8, 0).Children(func() {
				ui.Slot(c).Grow(1).Height(32).Draw(func(ctx *ui.DrawContext) {
					state.paintBatchSearchInput(ctx.Canvas, ctx.Rect)
				})
				if dialogButton(c, messages.ProxyBatchCheckAll, components.ButtonSecondary, false).Clicked() {
					state.batchCheckAllEligible()
				}
				if dialogButton(c, messages.ProxyBatchUncheckAll, components.ButtonSecondary, false).Clicked() {
					state.batchClearAll()
				}
			})

			// 3. 候选设备多选列表
			candidates := state.batchCandidates()
			theme := *c.Theme()
			side := components.CheckboxExtent(theme)
			hitWidth, hitHeight := components.CheckboxHitExtent(theme)

			list := ui.Scroll(c).Grow(1).FillWidth().Gap(2).Border(1, pal.line).Radius(radius.control).Background(pal.surface).Clip().
				Draw(func(d *ui.DrawContext) {
					dialog.listRect = d.Rect
				})
			list.Children(func() {
				for index, cand := range candidates {
					hostID := cand.Host.ID
					isSelected := dialog.selected[hostID]
					rowHit := hitProxyBatchCheckList.At(index)
					disabled := cand.Disabled

					bg := gfx.Color{}
					if isSelected {
						bg = pal.surfaceSoft
					}

					row := ui.Row(c).Height(38).FillWidth().PadSides(0, 10, 0, 10).Gap(8).Items(ui.Center).
						Background(bg).Radius(radius.control)
					if !disabled {
						row = row.HitWith(rowHit, cand.Host.Label, gfx.HitOptions{Role: gfx.RoleCheckbox, Selected: isSelected})
					}

					row.Children(func() {
						// 勾选框
						ui.Slot(c).Size(hitWidth, hitHeight).Draw(func(d *ui.DrawContext) {
							bounds := gfx.Rect{
								X:     d.Rect.X + (d.Rect.Width-side)/2,
								Y:     d.Rect.Y + (d.Rect.Height-side)/2,
								Width: side, Height: side,
							}
							cb := components.BeginCheckbox(d.Canvas, state.Theme(), rowHit, bounds, isSelected).Label(cand.Host.Label)
							if disabled {
								cb = cb.Disabled()
							}
							cb.Paint()
						})

						// 主机名与地址
						ui.Text(c, cand.Host.Label).FontSize(metrics.FontSizeS).Weight(gfx.WeightMedium).Color(pal.text).MarginSides(0, 4, 0, 4)
						ui.Text(c, cand.Host.Address).FontSize(metrics.FontSizeS).Color(pal.muted).Grow(1)

						// 胶囊徽章（Badge / Pill 风格）
						if cand.Cycle {
							// 成环不可选胶囊
							ui.Row(c).Height(22).PadSides(0, 8, 0, 8).Items(ui.Center).Gap(4).
								Background(pal.surfaceSoft).Border(1, pal.danger).Radius(999).Children(func() {
								ui.Icon(c, lucideIcons[iconAlert]).Size(11, 11).Color(pal.danger).Shrink(0)
								ui.Text(c, state.messages.ErrProxyCycle).FontSize(metrics.FontSizeS).Color(pal.danger)
							})
						} else if cand.HasRoute {
							// 已走跳板胶囊
							ui.Row(c).Height(22).PadSides(0, 8, 0, 8).Items(ui.Center).Gap(4).
								Background(pal.surfaceSoft).Border(1, pal.line).Radius(999).Children(func() {
								ui.Icon(c, lucideIcons[iconGlobe]).Size(11, 11).Color(pal.muted).Shrink(0)
								ui.Text(c, cand.ExistingJump).FontSize(metrics.FontSizeS).Color(pal.muted)
							})
						}
					})
				}
			})

			// 4. 覆盖已有路由开关
			ui.Row(c).Height(32).Items(ui.Center).Gap(6).MarginSides(6, 0, 4, 0).
				HitWith(hitProxyBatchOverwrite, messages.ProxyBatchOverwrite, gfx.HitOptions{Role: gfx.RoleCheckbox, Selected: dialog.overwrite}).
				Children(func() {
					ui.Slot(c).Size(hitWidth, hitHeight).Draw(func(d *ui.DrawContext) {
						bounds := gfx.Rect{
							X:     d.Rect.X + (d.Rect.Width-side)/2,
							Y:     d.Rect.Y + (d.Rect.Height-side)/2,
							Width: side, Height: side,
						}
						components.BeginCheckbox(d.Canvas, state.Theme(), hitProxyBatchOverwrite, bounds, dialog.overwrite).
							Label(messages.ProxyBatchOverwrite).Paint()
					})
					ui.Text(c, messages.ProxyBatchOverwrite).FontSize(metrics.FontSizeS).Color(pal.text)
				})

			// 错误信息提示
			if dialog.error != "" {
				ui.Text(c, dialog.error).FontSize(metrics.FontSizeS).Color(pal.danger).MarginSides(2, 0, 0, 0)
			}
		})
	})

	d.Footer(func() {
		selCount := 0
		for _, s := range dialog.selected {
			if s {
				selCount++
			}
		}
		submitLabel := fmt.Sprintf("%s (%d)", messages.ProxyCreate, selCount)

		if dialogButton(c, messages.Cancel, components.ButtonSecondary, false).Clicked() {
			c.After(func() { state.closeProxyBatchAssociate() })
		}
		if dialogButton(c, submitLabel, components.ButtonDefault, selCount == 0).Clicked() {
			c.After(func() { state.submitProxyBatchAssociate() })
		}
	})
}

func (state *State) paintProxyBatchJumpSelect(canvas *gfx.Canvas, bounds gfx.Rect) {
	theme, messages := state.Theme(), state.messages
	dialog := &state.proxyBatch

	selectedLabel := ""
	selectedDetail := ""
	if jhost, ok := state.hostByID(dialog.jumpID); ok {
		selectedLabel = jhost.Label
		selectedDetail = jhost.Address
	}

	candidates := state.proxyBatchJumpCandidates()
	rowHeight := float32(32)
	displayCount := len(candidates)
	if displayCount > 6 {
		displayCount = 6
	}
	contentHeight := float32(displayCount)*rowHeight + 8
	if len(candidates) == 0 {
		contentHeight = 36 + 8
	}

	content := gfx.Rect{
		X:      bounds.X,
		Y:      bounds.Y + bounds.Height + 4,
		Width:  bounds.Width,
		Height: contentHeight,
	}

	queryStr := dialog.jumpQuery.String()

	if dialog.jumpOpen && dialog.jumpFocusInput {
		canvas.RequestFocus(hitProxyBatchJumpInput)
		dialog.jumpFocusInput = false
	}

	combo := components.BeginCombobox(canvas, theme, bounds, components.ComboboxOptions{
		InputID:         hitProxyBatchJumpInput,
		TriggerID:       hitProxyBatchJumpTrigger,
		Label:           messages.ProxyFieldJump,
		Value:           selectedLabel,
		Detail:          selectedDetail,
		Query:           queryStr,
		Placeholder:     messages.HostPickerSearch,
		Open:            dialog.jumpOpen,
		ShowTrigger:     true,
		MatchInputWidth: true,
		Content:         content,
		Layer:           dialogLayer + 1,
		Scope:           hitProxyBatchJumpScope,
		DismissID:       hitProxyBatchJumpDismiss,
		Viewport:        state.windowRect(),
		Scroll:          &dialog.jumpScroll,
	})
	dialog.jumpPopup = combo.ContentBounds()

	defer combo.End()
	if !dialog.jumpOpen {
		return
	}

	if len(candidates) == 0 {
		dialog.jumpActiveID = gfx.ID{}
		combo.AddEmpty(messages.HostPickerNoMatches)
		return
	}

	if idx, ok := dialog.jumpActiveID.In(hitProxyBatchJumpItemList); !ok || idx < 0 || idx >= len(candidates) {
		dialog.jumpActiveID = hitProxyBatchJumpItemList.At(0)
	}

	hovered := -1
	for i := range candidates {
		if canvas.Hovered() == hitProxyBatchJumpItemList.At(i) {
			hovered = i
			break
		}
	}

	for i, host := range candidates {
		itemID := hitProxyBatchJumpItemList.At(i)
		isHighlighted := hovered == i || (hovered < 0 && dialog.jumpActiveID == itemID)
		combo.AddItem(components.ComboboxItem{
			ID:          itemID,
			Label:       host.Label,
			Detail:      host.Address,
			Value:       hostSelectValue(host.ID),
			Selected:    host.ID == dialog.jumpID,
			Highlighted: isHighlighted,
		})
	}
}

func (state *State) paintBatchSearchInput(canvas *gfx.Canvas, bounds gfx.Rect) {
	theme, messages := state.Theme(), state.messages
	val := state.proxyBatch.query.String()
	focused := canvas.Focused() == hitProxyBatchSearch
	components.PaintInput(canvas, theme, bounds, components.InputSnapshot{
		ID:          hitProxyBatchSearch,
		Label:       "",
		Value:       val,
		Placeholder: messages.ProxyBatchSelectPlaceholder,
		Focused:     focused,
	})
}

// clickProxyBatchDialog 处理批量弹窗点击事件
func (state *State) clickProxyBatchDialog(id gfx.ID) bool {
	if state.modal != modalProxyBatchAssociate {
		return false
	}
	dialog := &state.proxyBatch

	// 当跳板机下拉框处于打开状态时的事件处理
	if dialog.jumpOpen {
		switch {
		case id == hitProxyBatchClose || id == hitProxyBatchCancel:
			state.closeProxyBatchAssociate()
			return true
		case id == hitProxyBatchJumpTrigger:
			dialog.jumpOpen = false
			dialog.jumpFocusInput = false
			return true
		case id == hitProxyBatchJumpInput:
			dialog.jumpFocusInput = true
			return true
		case id == hitProxyBatchJumpDismiss:
			dialog.jumpOpen = false
			dialog.jumpFocusInput = false
			return true
		case id.Scope() == hitProxyBatchJumpItemList:
			if index, ok := id.In(hitProxyBatchJumpItemList); ok {
				candidates := state.proxyBatchJumpCandidates()
				if index >= 0 && index < len(candidates) {
					dialog.jumpID = candidates[index].ID
					delete(dialog.selected, dialog.jumpID)
					dialog.jumpOpen = false
					dialog.jumpFocusInput = false
					dialog.jumpQuery = text.Editor{}
				}
			}
			return true
		default:
			// 点击弹窗内其他控件时关闭下拉框，并继续执行该控件的点击逻辑
			dialog.jumpOpen = false
			dialog.jumpFocusInput = false
		}
	}

	switch {
	case id == hitProxyBatchClose || id == hitProxyBatchCancel:
		state.closeProxyBatchAssociate()
		return true
	case id == hitProxyBatchJumpInput || id == hitProxyBatchJumpTrigger:
		dialog.jumpOpen = true
		dialog.jumpFocusInput = true
		return true
	case id == hitProxyBatchSubmit:
		state.submitProxyBatchAssociate()
		return true
	case id == hitProxyBatchCheckAll:
		state.batchCheckAllEligible()
		return true
	case id == hitProxyBatchClearAll:
		state.batchClearAll()
		return true
	case id == hitProxyBatchOverwrite:
		dialog.overwrite = !dialog.overwrite
		return true
	case id.Scope() == hitProxyBatchCheckList:
		if index, ok := id.In(hitProxyBatchCheckList); ok {
			candidates := state.batchCandidates()
			if index >= 0 && index < len(candidates) && !candidates[index].Disabled {
				state.toggleBatchTarget(candidates[index].Host.ID)
			}
		}
		return true
	}
	return false
}

// proxyBatchPickerPointer 捕获跳板机选择器外部点击与内部滚动
func (state *State) proxyBatchPickerPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	if state.modal != modalProxyBatchAssociate {
		return gfx.EventResult{}, false
	}
	dialog := &state.proxyBatch
	if !dialog.jumpOpen {
		return gfx.EventResult{}, false
	}
	inside := event.Target == hitProxyBatchJumpInput || event.Target == hitProxyBatchJumpTrigger ||
		event.Target == hitProxyBatchJumpScroll || event.Target.Scope() == hitProxyBatchJumpItemList ||
		pointInRect(event.X, event.Y, dialog.jumpPopup)
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && !inside {
		dialog.jumpOpen = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Kind == gfx.PointerMove && inside {
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	return gfx.EventResult{}, false
}

// scrollProxyBatchDialog 处理批量关联设备弹窗中的跳板机下拉框滚轮滑动
func (state *State) scrollProxyBatchDialog(event gfx.ScrollEvent) gfx.EventResult {
	if state.modal != modalProxyBatchAssociate {
		return gfx.EventResult{}
	}
	dialog := &state.proxyBatch
	if dialog.jumpOpen {
		inside := pointInRect(event.X, event.Y, dialog.jumpPopup) ||
			event.Target == hitProxyBatchJumpInput ||
			event.Target == hitProxyBatchJumpTrigger ||
			event.Target == hitProxyBatchJumpScroll ||
			event.Target.Scope() == hitProxyBatchJumpItemList
		if inside {
			return gfx.EventResult{Handled: true, Repaint: dialog.jumpScroll.Wheel(event)}
		}
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{}
}

// proxyBatchDialogKey 处理跳板机选择器键盘事件
func (state *State) proxyBatchDialogKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalProxyBatchAssociate {
		return gfx.EventResult{}, false
	}
	dialog := &state.proxyBatch
	if !dialog.jumpOpen {
		return gfx.EventResult{}, false
	}
	switch event.Key {
	case gfx.KeyEscape, gfx.KeyTab:
		dialog.jumpOpen = false
		dialog.jumpActiveID = gfx.ID{}
		return gfx.EventResult{Handled: true, Repaint: true}, true
	case gfx.KeyEnter:
		if !event.Release {
			candidates := state.proxyBatchJumpCandidates()
			if len(candidates) > 0 {
				pickIdx := 0
				if idx, ok := dialog.jumpActiveID.In(hitProxyBatchJumpItemList); ok && idx >= 0 && idx < len(candidates) {
					pickIdx = idx
				}
				dialog.jumpID = candidates[pickIdx].ID
				delete(dialog.selected, dialog.jumpID)
				dialog.jumpOpen = false
				dialog.jumpActiveID = gfx.ID{}
				dialog.jumpQuery = text.Editor{}
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
	case gfx.KeyUp, gfx.KeyDown, gfx.KeyHome, gfx.KeyEnd, gfx.KeyPageUp, gfx.KeyPageDown:
		if !event.Release {
			if id, ok := dialog.jumpScroll.Navigate(event.Key, dialog.jumpActiveID, true); ok {
				dialog.jumpActiveID = id
				return gfx.EventResult{Handled: true, Repaint: true}, true
			}
		}
	}
	return gfx.EventResult{}, false
}
