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
	jumpID       uint32
	jumpChoices  []uint32
	query        text.Editor
	search       ui.Field
	searchBound  bool
	selected     map[uint32]bool
	overwrite    bool
	viaShell     bool
	relay        text.Editor
	error        string
	fromHostList bool
	scroll       float32
	content      float32
	bounds       gfx.Rect
	listRect     gfx.Rect
	dialogBody   proxyBatchDialogBody
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

// proxyBatchJumpChoices are the hosts the jump picker offers: the SSH hosts.
func (state *State) proxyBatchJumpChoices() []uint32 {
	choices := state.proxyBatch.jumpChoices[:0]
	for _, host := range state.hosts {
		if host.isSSH() {
			choices = append(choices, host.ID)
		}
	}
	state.proxyBatch.jumpChoices = choices
	return choices
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
				before := dialog.jumpID
				ui.Combobox(c, &dialog.jumpID, state.proxyBatchJumpChoices(), state.proxyBatchJumpName, messages.ProxyFieldJump).
					FillWidth().Placeholder(messages.HostPickerSearch).Empty(messages.HostPickerNoMatches)
				if dialog.jumpID != before {
					delete(dialog.selected, dialog.jumpID)
				}
			})

			// 2. 搜索框与全选/清空行
			ui.Row(c).Height(36).Items(ui.Center).Gap(8).MarginSides(0, 0, 8, 0).Children(func() {
				if !dialog.searchBound {
					dialog.search.Bind(&dialog.query)
					dialog.searchBound = true
				}
				ui.TextField(c, &dialog.search, "", messages.ProxyBatchSelectPlaceholder, ui.WithID(hitProxyBatchSearch)).Grow(1)
				if dialogButton(c, messages.ProxyBatchCheckAll, components.ButtonSecondary, false).Clicked() {
					state.batchCheckAllEligible()
				}
				if dialogButton(c, messages.ProxyBatchUncheckAll, components.ButtonSecondary, false).Clicked() {
					state.batchClearAll()
				}
			})

			// 3. 候选设备多选列表
			candidates := state.batchCandidates()

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
						box := ui.Checkbox(c, state.pageFlag(isSelected), "").Hit(rowHit, cand.Host.Label).Shrink(0)
						if disabled {
							box.Disabled(true)
						}

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
					ui.Checkbox(c, state.pageFlag(dialog.overwrite), "").Hit(hitProxyBatchOverwrite, messages.ProxyBatchOverwrite).Shrink(0)
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

// proxyBatchJumpName is how the jump host picker names a host: its label and, so a
// search by address finds it, its address.
func (state *State) proxyBatchJumpName(id uint32) string {
	host, ok := state.hostByID(id)
	if !ok {
		return ""
	}
	return host.Label + " · " + host.Address
}

// clickProxyBatchDialog 处理批量弹窗点击事件
func (state *State) clickProxyBatchDialog(id gfx.ID) bool {
	if state.modal != modalProxyBatchAssociate {
		return false
	}
	dialog := &state.proxyBatch

	switch {
	case id == hitProxyBatchClose || id == hitProxyBatchCancel:
		state.closeProxyBatchAssociate()
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
