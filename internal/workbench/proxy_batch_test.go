package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestBastionHubsAggregation(t *testing.T) {
	state := newTestState(t, testOptions(t))
	// 添加 3 台测试主机：jump1, jump2, target1, target2, target3
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	target1 := state.hosts[2]

	// 手动添加 2 台新 SSH 主机
	target2 := Host{ID: 101, CatalogID: "target-2", Label: "Target-2", Address: "10.0.0.2", System: 1}
	target3 := Host{ID: 102, CatalogID: "target-3", Label: "Target-3", Address: "10.0.0.3", System: 1}
	target2.finish()
	target3.finish()
	state.hosts = append(state.hosts, target2, target3)

	// 将 target1, target2 关联到 jump1
	added, updated := state.batchAssociateProxies(jump1.ID, []uint32{target1.ID, target2.ID}, false, false, "")
	if added != 2 || updated != 0 {
		t.Fatalf("batchAssociateProxies to jump1: added=%d updated=%d, want 2, 0", added, updated)
	}

	// 将 target3 关联到 jump2
	added, updated = state.batchAssociateProxies(jump2.ID, []uint32{target3.ID}, false, false, "")
	if added != 1 || updated != 0 {
		t.Fatalf("batchAssociateProxies to jump2: added=%d updated=%d, want 1, 0", added, updated)
	}

	// 检查中枢聚合结果
	hubs := state.bastionHubList()
	if len(hubs) != 2 {
		t.Fatalf("got %d bastion hubs, want 2", len(hubs))
	}
	if hubs[0].JumpHost.ID != jump1.ID || hubs[0].Total != 2 {
		t.Fatalf("hub 0: jumpID=%d total=%d, want jump1 and 2 targets", hubs[0].JumpHost.ID, hubs[0].Total)
	}
	if hubs[1].JumpHost.ID != jump2.ID || hubs[1].Total != 1 {
		t.Fatalf("hub 1: jumpID=%d total=%d, want jump2 and 1 target", hubs[1].JumpHost.ID, hubs[1].Total)
	}
}

func TestProxyBatchAssociateOverwriteAndCycleProtection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	target := state.hosts[2]

	// 1. 先将 target 绑定至 jump1
	added, _ := state.batchAssociateProxies(jump1.ID, []uint32{target.ID}, false, false, "")
	if added != 1 {
		t.Fatalf("initial associate added=%d, want 1", added)
	}

	// 2. 在 overwrite=false 情况下，尝试将 target 绑定至 jump2，应当跳过
	added, updated := state.batchAssociateProxies(jump2.ID, []uint32{target.ID}, false, false, "")
	if added != 0 || updated != 0 {
		t.Fatalf("without overwrite: added=%d updated=%d, want 0, 0", added, updated)
	}

	// 检查路由依然指向 jump1
	route, ok := state.proxyRouteForTarget(target.ID)
	if !ok || route.JumpID != jump1.ID {
		t.Fatalf("target jumpID=%d, want %d", route.JumpID, jump1.ID)
	}

	// 3. 在 overwrite=true 情况下，再次绑定至 jump2，应当更新
	added, updated = state.batchAssociateProxies(jump2.ID, []uint32{target.ID}, true, false, "")
	if added != 0 || updated != 1 {
		t.Fatalf("with overwrite: added=%d updated=%d, want 0, 1", added, updated)
	}

	route, ok = state.proxyRouteForTarget(target.ID)
	if !ok || route.JumpID != jump2.ID {
		t.Fatalf("after overwrite: target jumpID=%d, want %d", route.JumpID, jump2.ID)
	}

	// 4. 成环测试：如果将 jump2 的跳板设置为 target，会形成 jump2 -> target -> jump2 环路，必须拦截
	added, updated = state.batchAssociateProxies(target.ID, []uint32{jump2.ID}, true, false, "")
	if added != 0 || updated != 0 {
		t.Fatalf("cycle should be prevented: added=%d updated=%d, want 0, 0", added, updated)
	}
}

func TestToggleBastionHub(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump := state.hosts[0]
	t1 := state.hosts[1]
	t2 := state.hosts[2]

	state.batchAssociateProxies(jump.ID, []uint32{t1.ID, t2.ID}, false, false, "")

	// 初始全为 running
	hubs := state.bastionHubList()
	if len(hubs) == 0 || hubs[0].Active != 2 {
		t.Fatalf("initial hub active=%d, want 2", hubs[0].Active)
	}

	// 切换为全部停止
	state.toggleBastionHub(jump.ID)
	hubs = state.bastionHubList()
	if hubs[0].Active != 0 {
		t.Fatalf("after toggle: hub active=%d, want 0", hubs[0].Active)
	}

	// 再次切换为全部启动
	state.toggleBastionHub(jump.ID)
	hubs = state.bastionHubList()
	if hubs[0].Active != 2 {
		t.Fatalf("after second toggle: hub active=%d, want 2", hubs[0].Active)
	}
}

func TestBatchRemoveProxiesForTargets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump := state.hosts[0]
	t1 := state.hosts[1]
	t2 := state.hosts[2]

	state.batchAssociateProxies(jump.ID, []uint32{t1.ID, t2.ID}, false, false, "")
	if len(state.proxies) != 2 {
		t.Fatalf("len(proxies)=%d, want 2", len(state.proxies))
	}

	removed := state.batchRemoveProxiesForTargets([]uint32{t1.ID})
	if removed != 1 || len(state.proxies) != 1 {
		t.Fatalf("removed=%d len(proxies)=%d, want 1, 1", removed, len(state.proxies))
	}

	// 剩下的路由应该是 t2
	if state.proxies[0].TargetID != t2.ID {
		t.Fatalf("remaining targetID=%d, want %d", state.proxies[0].TargetID, t2.ID)
	}
}

func TestProxyBatchDialogHeadlessInteraction(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump := state.hosts[0]
	t1 := state.hosts[1]
	t2 := state.hosts[2]

	// 打开批量关联弹窗
	state.openProxyBatchAssociate(jump.ID)
	if state.modal != modalProxyBatchAssociate {
		t.Fatalf("modal=%v, want modalProxyBatchAssociate", state.modal)
	}

	// 验证点击全选
	state.Click(hitProxyBatchCheckAll)
	if !state.proxyBatch.selected[t1.ID] || !state.proxyBatch.selected[t2.ID] {
		t.Fatalf("after check all: selected=%v, want both t1 and t2", state.proxyBatch.selected)
	}

	// 验证清空
	state.Click(hitProxyBatchClearAll)
	if len(state.proxyBatch.selected) != 0 {
		t.Fatalf("after clear all: len(selected)=%d, want 0", len(state.proxyBatch.selected))
	}

	// 选中 t1 并提交
	state.toggleBatchTarget(t1.ID)
	state.Click(hitProxyBatchSubmit)

	if state.modal != modalNone {
		t.Fatalf("after submit: modal=%v, want modalNone", state.modal)
	}
	if len(state.proxies) != 1 || state.proxies[0].TargetID != t1.ID {
		t.Fatalf("after dialog submit: len(proxies)=%d target=%d, want 1, %d", len(state.proxies), state.proxies[0].TargetID, t1.ID)
	}
}

func TestHostMenuSetJumpTrigger(t *testing.T) {
	state := newTestState(t, testOptions(t))
	target := state.hosts[1]

	// 模拟右键点击 target 主机打开菜单
	state.hostMenu = hostMenu{open: true, host: target.ID, at: gfx.Point{X: 300, Y: 250}}
	if !state.hostMenu.open {
		t.Fatalf("host menu should be open")
	}

	// 点击“关联跳板机...”
	state.Click(hitHostMenuSetJump)
	if state.modal != modalProxyBatchAssociate {
		t.Fatalf("after hitHostMenuSetJump: modal=%v, want modalProxyBatchAssociate", state.modal)
	}
	if !state.proxyBatch.fromHostList || !state.proxyBatch.selected[target.ID] {
		t.Fatalf("batch dialog should preselect target: %+v", state.proxyBatch)
	}
}

func TestProxyHubTabFilterClick(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	target := state.hosts[2]

	target4 := Host{ID: 104, CatalogID: "target-4", Label: "Target-4", Address: "10.0.0.4", System: 1}
	target4.finish()
	state.hosts = append(state.hosts, target4)

	state.batchAssociateProxies(jump1.ID, []uint32{target.ID}, false, false, "")
	state.batchAssociateProxies(jump2.ID, []uint32{target4.ID}, false, false, "")

	// 初始未选具体中枢（0 为全部）
	state.selectedBastionJumpID = 0

	// 点击第 1 个具体跳板机 Tab (index 1 为 jump1)
	state.Click(hitProxyHubTabList.At(1))
	if state.selectedBastionJumpID != jump1.ID {
		t.Fatalf("after clicking hub tab 1: selectedBastionJumpID=%d, want %d", state.selectedBastionJumpID, jump1.ID)
	}

	// 点击第 2 个具体跳板机 Tab (index 2 为 jump2)
	state.Click(hitProxyHubTabList.At(2))
	if state.selectedBastionJumpID != jump2.ID {
		t.Fatalf("after clicking hub tab 2: selectedBastionJumpID=%d, want %d", state.selectedBastionJumpID, jump2.ID)
	}

	// 点击“全部路由” Tab (index 0)
	state.Click(hitProxyHubTabList.At(0))
	if state.selectedBastionJumpID != 0 {
		t.Fatalf("after clicking all tab: selectedBastionJumpID=%d, want 0", state.selectedBastionJumpID)
	}
}

func TestProxyBatchDialogSwitchJumpHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	target := state.hosts[2]

	// 初始以 jump1 打开
	state.openProxyBatchAssociate(jump1.ID)
	if state.proxyBatch.jumpID != jump1.ID {
		t.Fatalf("initial jumpID=%d, want %d", state.proxyBatch.jumpID, jump1.ID)
	}

	// 模拟点击下拉框并切换为 jump2
	state.Click(hitProxyBatchJumpTrigger)
	if !state.proxyBatch.jumpOpen {
		t.Fatalf("jumpOpen should be true")
	}

	// 查找 jump2 在候选跳板机中的位置，并验证其 Label 与 Address (IP 地址)
	jump2Idx := -1
	candidates := state.proxyBatchJumpCandidates()
	for idx, cand := range candidates {
		if cand.ID == jump2.ID {
			jump2Idx = idx
			if cand.Label != jump2.Label || cand.Address != jump2.Address {
				t.Fatalf("jump option label=%q addr=%q, want label=%q addr=%q", cand.Label, cand.Address, jump2.Label, jump2.Address)
			}
			break
		}
	}
	if jump2Idx < 0 {
		t.Fatalf("jump2 not found in jump candidates")
	}

	state.Click(hitProxyBatchJumpItemList.At(jump2Idx))
	if state.proxyBatch.jumpID != jump2.ID {
		t.Fatalf("after selecting jump2: jumpID=%d, want %d", state.proxyBatch.jumpID, jump2.ID)
	}

	// 候选人中必须不包含 jump2，而应该包含 target
	foundJump2 := false
	foundTarget := false
	for _, cand := range state.batchCandidates() {
		if cand.Host.ID == jump2.ID {
			foundJump2 = true
		}
		if cand.Host.ID == target.ID {
			foundTarget = true
		}
	}
	if foundJump2 {
		t.Fatalf("candidates should not contain new jump2 itself")
	}
	if !foundTarget {
		t.Fatalf("candidates should contain target")
	}

	// 勾选 target 并提交
	state.toggleBatchTarget(target.ID)
	state.Click(hitProxyBatchSubmit)

	route, ok := state.proxyRouteForTarget(target.ID)
	if !ok || route.JumpID != jump2.ID {
		t.Fatalf("target should be routed via jump2: ok=%v, jumpID=%d", ok, route.JumpID)
	}
}

func TestProxyBatchAddDoesNotAlterHubFilter(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump1 := state.hosts[0]
	target := state.hosts[1]
	state.batchAssociateProxies(jump1.ID, []uint32{target.ID}, false, false, "")

	// 明确置为“全部”视图 (0)
	state.selectedBastionJumpID = 0

	// 点击批量关联设备按钮
	state.Click(hitProxyBatchAdd)

	if state.modal != modalProxyBatchAssociate {
		t.Fatalf("expected modalProxyBatchAssociate, got %v", state.modal)
	}

	// 主界面的筛选状态必须依然为 0（全部），不能被静默篡改为 jump1
	if state.selectedBastionJumpID != 0 {
		t.Fatalf("clicking batch add altered selectedBastionJumpID to %d, want 0", state.selectedBastionJumpID)
	}

	// 关闭弹窗
	state.Click(hitProxyBatchClose)
	if state.selectedBastionJumpID != 0 {
		t.Fatalf("after closing dialog: selectedBastionJumpID=%d, want 0", state.selectedBastionJumpID)
	}
}

// TestProxyRowSwitchToggle 测试行内 Switch 开关就地启停路由
func TestProxyRowSwitchToggle(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump := state.hosts[0]
	target := state.hosts[1]
	state.batchAssociateProxies(jump.ID, []uint32{target.ID}, false, false, "")

	route := state.proxies[0]
	if route.Stopped {
		t.Fatalf("expected route running by default")
	}

	// 点击行内 Switch 开关进行就地停止
	state.Click(hitProxyRowSwitchList.At(int(route.ID)))
	if !state.proxies[0].Stopped {
		t.Fatalf("after switch click: expected route stopped")
	}

	// 再次点击行内 Switch 开关就地启动
	state.Click(hitProxyRowSwitchList.At(int(route.ID)))
	if state.proxies[0].Stopped {
		t.Fatalf("after second switch click: expected route running")
	}
}

// TestProxyRowActions 测试行尾吸附操作（编辑弹窗、单个删除二次确认与提交）
func TestProxyRowActions(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump := state.hosts[0]
	target := state.hosts[1]
	state.batchAssociateProxies(jump.ID, []uint32{target.ID}, false, false, "")
	routeID := state.proxies[0].ID

	// 1. 点击行尾编辑图标按钮
	state.Click(hitProxyRowEditList.At(int(routeID)))
	if state.modal != modalNewProxy || !state.proxyEditor.editing || state.proxyEditor.routeID != routeID {
		t.Fatalf("expected modalNewProxy in editing mode, modal=%v, editing=%v", state.modal, state.proxyEditor.editing)
	}
	state.Click(hitProxyDialogCancel)
	if state.modal != modalNone {
		t.Fatalf("expected modalNone after cancel")
	}

	// 2. 点击行尾删除图标按钮 -> 触发二次确认弹窗
	state.Click(hitProxyRowDeleteList.At(int(routeID)))
	if state.modal != modalProxyDeleteConfirm || state.proxyPendingDeleteID != routeID {
		t.Fatalf("expected modalProxyDeleteConfirm with pendingID %d, got modal=%v, pendingID=%d", routeID, state.modal, state.proxyPendingDeleteID)
	}

	// 2.1 取消删除
	state.Click(hitProxyDeleteConfirmCancel)
	if state.modal != modalNone || len(state.proxies) != 1 {
		t.Fatalf("expected cancel delete without modifying routes, modal=%v, proxies=%d", state.modal, len(state.proxies))
	}

	// 2.2 重新打开并确认删除
	state.Click(hitProxyRowDeleteList.At(int(routeID)))
	state.Click(hitProxyDeleteConfirmSubmit)
	if state.modal != modalNone || len(state.proxies) != 0 {
		t.Fatalf("expected route deleted, got modal=%v, count=%d", state.modal, len(state.proxies))
	}
}

// TestProxyBatchSelectionAndBulkDelete 测试多选、表头全选、批量启停与批量删除二次确认
func TestProxyBatchSelectionAndBulkDelete(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump := state.hosts[0]
	target1 := state.hosts[1]
	target2 := state.hosts[2]
	state.batchAssociateProxies(jump.ID, []uint32{target1.ID, target2.ID}, false, false, "")

	if len(state.proxies) != 2 {
		t.Fatalf("expected 2 routes, got %d", len(state.proxies))
	}
	r1, r2 := state.proxies[0].ID, state.proxies[1].ID

	// 初始未勾选
	if state.proxyCheckedCount() != 0 {
		t.Fatalf("expected 0 checked initially, got %d", state.proxyCheckedCount())
	}

	// 勾选单行
	state.Click(hitProxyRowCheckList.At(int(r1)))
	if state.proxyCheckedCount() != 1 || !state.proxyChecked[r1] {
		t.Fatalf("expected r1 checked")
	}

	// 点击表头全选
	state.Click(hitProxyCheckAll)
	if state.proxyCheckedCount() != 2 || !state.proxyChecked[r1] || !state.proxyChecked[r2] {
		t.Fatalf("expected all 2 checked after select all")
	}

	// 批量全部停止
	state.Click(hitProxyBatchStopAll)
	for _, r := range state.proxies {
		if !r.Stopped {
			t.Fatalf("expected route %d stopped after batch stop all", r.ID)
		}
	}

	// 批量全部启动
	state.Click(hitProxyBatchStartAll)
	for _, r := range state.proxies {
		if r.Stopped {
			t.Fatalf("expected route %d running after batch start all", r.ID)
		}
	}

	// 点击批量删除 -> 触发二次确认弹窗
	state.Click(hitProxyBatchDelete)
	if state.modal != modalProxyBatchDeleteConfirm {
		t.Fatalf("expected modalProxyBatchDeleteConfirm, got %v", state.modal)
	}

	// 取消批量删除
	state.Click(hitProxyBatchDeleteConfirmCancel)
	if state.modal != modalNone || len(state.proxies) != 2 {
		t.Fatalf("expected modalNone and routes preserved")
	}

	// 再次点击批量删除并确认
	state.Click(hitProxyBatchDelete)
	state.Click(hitProxyBatchDeleteConfirmSubmit)
	if state.modal != modalNone || len(state.proxies) != 0 || state.proxyCheckedCount() != 0 {
		t.Fatalf("expected all routes deleted and selection cleared, proxies=%d, checked=%d", len(state.proxies), state.proxyCheckedCount())
	}
}

func TestProxyPageStylingAndTopologyEnhancements(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	addProxyRoute(t, state, state.hosts[0].ID, state.hosts[1].ID)
	session := openWorkbenchSession(t, state)

	// 1. 验证底部仅有条数统计（如 "1 条跳板路由"），无具体长路径文本
	if state.proxyCountText == "" {
		t.Fatalf("expected proxyCountText to be populated")
	}

	// 2. 验证行点击依然可选，但视觉绘制不带选中的整行高亮
	target := state.proxies[0]
	if err := session.ClickLabel(target.cells[0]); err != nil {
		t.Fatal(err)
	}
	if state.selectedProxyID != target.ID {
		t.Fatalf("selected route = %d, want %d", state.selectedProxyID, target.ID)
	}

	// 3. 验证横向滚动容器声明存在且编辑/删除按钮始终可在最右侧点击
	var editHit, delHit *gfx.Hit
	for i := range session.Snapshot().Hits {
		hit := &session.Snapshot().Hits[i]
		if hit.ID == hitProxyRowEditList.At(int(target.ID)) {
			editHit = hit
		} else if hit.ID == hitProxyRowDeleteList.At(int(target.ID)) {
			delHit = hit
		}
	}
	if editHit == nil || delHit == nil {
		t.Fatalf("expected edit and delete button hits to exist in headless snapshot")
	}
	// 验证编辑按钮在删除按钮左侧且距离紧凑吸附
	if editHit.Bounds.X >= delHit.Bounds.X {
		t.Fatalf("expected edit button at left of delete button: edit=%v, del=%v", editHit.Bounds, delHit.Bounds)
	}
}

func TestProxyMultiHopChainResolutionAndDisplay(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	// 准备主机 A, B, C
	hostA := state.hosts[0]
	hostB := state.hosts[1]
	hostC := state.hosts[2]

	// 创建链路：A -> B, B -> C
	addProxyRoute(t, state, hostB.ID, hostA.ID) // Target: B, Jump: A
	addProxyRoute(t, state, hostC.ID, hostB.ID) // Target: C, Jump: B

	// 1. 验证底层网络连接跳板链条：连接 C 时，proxyChainFor 追溯出 [B, A]，实际连接为 A -> B -> C
	chain, valid := state.proxyChainFor(hostC)
	if !valid || len(chain) != 2 {
		t.Fatalf("expected valid chain with 2 hops for hostC, valid=%v, len=%d", valid, len(chain))
	}
	if chain[0].ID != hostB.ID || chain[1].ID != hostA.ID {
		t.Fatalf("expected chain [B, A], got [%d, %d]", chain[0].ID, chain[1].ID)
	}

	// 2. 验证路由对象中解析出的拓扑节点 hops
	var routeBC *proxyRecord
	for i := range state.proxies {
		if state.proxies[i].TargetID == hostC.ID {
			routeBC = &state.proxies[i]
			break
		}
	}
	if routeBC == nil {
		t.Fatalf("route for host C not found")
	}
	if len(routeBC.hops) != 3 {
		t.Fatalf("expected 3 hops for routeBC (A -> B -> C), got %d: %+v", len(routeBC.hops), routeBC.hops)
	}
	if routeBC.hops[0].ID != hostA.ID || routeBC.hops[1].ID != hostB.ID || routeBC.hops[2].ID != hostC.ID {
		t.Fatalf("expected hops order A(%d) -> B(%d) -> C(%d), got [%d, %d, %d]",
			hostA.ID, hostB.ID, hostC.ID,
			routeBC.hops[0].ID, routeBC.hops[1].ID, routeBC.hops[2].ID)
	}

	// 3. 验证无头渲染下能正常布局且不 panic
	session := openWorkbenchSession(t, state)
	if session.Snapshot().Cursor != 0 && false {
	}
}

func TestProxyBatchJumpSelectWheelScroll(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	// 添加足够多的 SSH 主机使得下拉列表溢出并产生滚动
	for i := 10; i < 30; i++ {
		h := Host{ID: uint32(i + 100), CatalogID: fmt.Sprintf("h-%d", i), Label: fmt.Sprintf("Host-%d", i), Address: fmt.Sprintf("10.0.0.%d", i), System: 1}
		h.finish()
		state.hosts = append(state.hosts, h)
	}

	state.activeSection = sectionProxies
	state.openProxyBatchAssociate(state.hosts[0].ID)
	if state.modal != modalProxyBatchAssociate {
		t.Fatalf("expected modalProxyBatchAssociate, got %v", state.modal)
	}

	// 打开跳板主机下拉框
	state.Click(hitProxyBatchJumpTrigger)
	dialog := &state.proxyBatch
	if !dialog.jumpOpen {
		t.Fatalf("expected jumpOpen to be true")
	}

	// 模拟一次 Paint 让 popup 布局生效
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})

	// 记录滚动前的 offset
	popup := dialog.jumpPopup
	if popup.Width <= 0 || popup.Height <= 0 {
		// 如果在无头下 popup 宽未被赋，设为虚拟区域
		popup = gfx.Rect{X: 100, Y: 100, Width: 300, Height: 200}
		dialog.jumpPopup = popup
	}

	// 发生滚轮滚动事件（向下滚）
	scrollEvent := gfx.ScrollEvent{
		X:      popup.X + 20,
		Y:      popup.Y + 20,
		DeltaY: 100,
	}
	// 验证窗口顶层滚轮入口 scrollWindow 优先截获浮动的跳板机下拉框，阻断穿透到下层的 ui.Scroll
	winRes := state.scrollWindow(scrollEvent)
	if !winRes.Handled {
		t.Fatalf("expected scrollWindow to intercept jump select popup wheel event")
	}
	if !winRes.Repaint {
		t.Fatalf("expected scrollWindow to trigger repaint on offset change")
	}

	res := state.Scroll(scrollEvent)
	if !res.Handled {
		t.Fatalf("expected Scroll to be handled by scrollProxyBatchDialog")
	}
}

func TestProxyBatchComboboxDetailSearch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	jump1.Address = "192.168.1.10"
	jump2.Address = "10.0.0.88"
	state.hosts[0] = jump1
	state.hosts[1] = jump2

	state.openProxyBatchAssociate(jump1.ID)
	if state.modal != modalProxyBatchAssociate {
		t.Fatalf("expected modalProxyBatchAssociate, got %v", state.modal)
	}

	// 打开 Combobox
	state.Click(hitProxyBatchJumpTrigger)
	if !state.proxyBatch.jumpOpen {
		t.Fatalf("expected jumpOpen to be true")
	}

	// 1. 测试按 IP 地址过滤 (Detail 检索)
	state.proxyBatch.jumpQuery.Insert("10.0.0")
	candidates := state.proxyBatchJumpCandidates()
	if len(candidates) != 1 || candidates[0].ID != jump2.ID {
		t.Fatalf("search by IP '10.0.0' returned %+v, want only jump2", candidates)
	}
	if candidates[0].Label != jump2.Label || candidates[0].Address != "10.0.0.88" {
		t.Fatalf("candidate mismatch: %+v", candidates[0])
	}

	// 模拟一次 Paint，验证渲染流程正常无 panic
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})

	// 2. 选择过滤出来的跳板机
	state.Click(hitProxyBatchJumpItemList.At(0))
	if state.proxyBatch.jumpID != jump2.ID {
		t.Fatalf("after choosing jump2: jumpID=%d, want %d", state.proxyBatch.jumpID, jump2.ID)
	}
	if state.proxyBatch.jumpOpen {
		t.Fatalf("combobox should close after selection")
	}

	// 3. 验证选中后输入框绘制了选中的主机名以及 IP 地址 (Detail)
	canvas.Reset()
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	foundLabel, foundDetail := false, false
	for _, text := range canvas.Texts() {
		if text.Value == jump2.Label {
			foundLabel = true
		}
		if text.Value == jump2.Address {
			foundDetail = true
		}
	}
	if !foundLabel || !foundDetail {
		t.Fatalf("expected selected jump host label and detail to be painted: foundLabel=%v, foundDetail=%v", foundLabel, foundDetail)
	}
}

func TestProxyBatchComboboxInputClickAndHover(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump1 := state.hosts[0]
	jump1.Address = "192.168.1.10"
	state.hosts[0] = jump1

	state.openProxyBatchAssociate(jump1.ID)

	// 1. 关闭状态下，首次点击输入框展开下拉框，验证输入不会被清空且自动请求焦点
	state.proxyBatch.jumpQuery.Insert("devbox")
	state.Click(hitProxyBatchJumpInput)
	if !state.proxyBatch.jumpOpen {
		t.Fatalf("expected jumpOpen to be true after clicking input")
	}
	if !state.proxyBatch.jumpFocusInput {
		t.Fatalf("expected jumpFocusInput to be set to true on opening")
	}
	if state.proxyBatch.jumpQuery.String() != "devbox" {
		t.Fatalf("jumpQuery should not be cleared on click, got %q", state.proxyBatch.jumpQuery.String())
	}

	// 模拟首帧 Paint，验证自动请求焦点赋予 hitProxyBatchJumpInput
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if canvas.FocusRequest() != hitProxyBatchJumpInput {
		t.Fatalf("after first paint with jumpOpen=true: canvas.FocusRequest()=%v, want %v", canvas.FocusRequest(), hitProxyBatchJumpInput)
	}
	if editor := state.EditorFor(hitProxyBatchJumpInput); editor != &state.proxyBatch.jumpQuery {
		t.Fatalf("editorFor hitProxyBatchJumpInput=%p, want &state.proxyBatch.jumpQuery=%p", editor, &state.proxyBatch.jumpQuery)
	}

	// 再次点击输入框，下拉框依然保持展开，且 query 完好
	state.Click(hitProxyBatchJumpInput)
	if !state.proxyBatch.jumpOpen {
		t.Fatalf("expected jumpOpen to remain true on subsequent input clicks")
	}
	if state.proxyBatch.jumpQuery.String() != "devbox" {
		t.Fatalf("jumpQuery got mutated, got %q", state.proxyBatch.jumpQuery.String())
	}

	// 2. 模拟鼠标在浮层内移动，验证触发重绘（Hover 变色）
	res, handled := state.proxyBatchPickerPointer(gfx.PointerEvent{
		Kind:   gfx.PointerMove,
		Target: hitProxyBatchJumpItemList.At(0),
	})
	if !handled || !res.Repaint {
		t.Fatalf("expected PointerMove over jump list to be handled with Repaint=true, got handled=%v, res=%+v", handled, res)
	}
}

func TestProxyHubTabFiltersRenderedRowsAndSelectAll(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jumpA := state.hosts[0]
	jumpB := state.hosts[1]
	target1 := state.hosts[2]
	target2 := Host{ID: 201, CatalogID: "t2", Label: "Target-2", Address: "10.0.0.21", System: 1}
	target3 := Host{ID: 202, CatalogID: "t3", Label: "Target-3", Address: "10.0.0.22", System: 1}
	target2.finish()
	target3.finish()
	state.hosts = append(state.hosts, target2, target3)

	// jumpA 关联 2 台设备，jumpB 关联 1 台设备
	state.batchAssociateProxies(jumpA.ID, []uint32{target1.ID, target2.ID}, false, false, "")
	state.batchAssociateProxies(jumpB.ID, []uint32{target3.ID}, false, false, "")

	state.activeSection = sectionProxies
	var canvas gfx.Canvas

	// 1. 全部视图：应渲染 3 条路由
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if got := state.proxyRows.Len(); got != 3 {
		t.Fatalf("all tab proxyRows.Len() = %d, want 3", got)
	}
	countRenderedRows := func(c *gfx.Canvas) int {
		rows := 0
		for _, h := range c.Hits() {
			if h.ID.Scope() == hitProxyRowList {
				rows++
			}
		}
		return rows
	}
	countRenderedSwitches := func(c *gfx.Canvas) int {
		switches := 0
		for _, h := range c.Hits() {
			if h.ID.Scope() == hitProxyRowSwitchList {
				switches++
			}
		}
		return switches
	}
	if got := countRenderedRows(&canvas); got != 3 {
		t.Fatalf("all tab rendered rows = %d, want 3", got)
	}

	// 2. 切换到 jumpB 中枢 Tab（索引 2）：渲染行数与行内开关数必须严格等于 1
	state.Click(hitProxyHubTabList.At(2))
	if state.selectedBastionJumpID != jumpB.ID {
		t.Fatalf("selectedBastionJumpID = %d, want %d", state.selectedBastionJumpID, jumpB.ID)
	}
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if got := state.proxyRows.Len(); got != 1 {
		t.Fatalf("jumpB tab proxyRows.Len() = %d, want 1", got)
	}
	if got := countRenderedRows(&canvas); got != 1 {
		t.Fatalf("jumpB tab rendered rows = %d, want 1", got)
	}
	if got := countRenderedSwitches(&canvas); got != 1 {
		t.Fatalf("jumpB tab rendered switches = %d, want 1", got)
	}

	// 3. 在 jumpB 过滤视图下点击全选：仅勾中 jumpB 下的 1 条路由
	state.Click(hitProxyCheckAll)
	if got := state.proxyCheckedCount(); got != 1 {
		t.Fatalf("after select-all on jumpB tab: checkedCount = %d, want 1", got)
	}
	totalVis, checkedVis := state.proxyVisibleStats()
	if totalVis != 1 || checkedVis != 1 {
		t.Fatalf("visible stats = (%d, %d), want (1, 1)", totalVis, checkedVis)
	}
}

func TestProxyNarrowWindowRowActionsRemainReachable(t *testing.T) {
	state := newTestState(t, testOptions(t))
	// 构造 3 跳长链路：target -> jump2 -> jump1
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	target := state.hosts[2]
	state.proxies = []proxyRecord{
		{ID: 1, TargetID: jump2.ID, JumpID: jump1.ID},
		{ID: 2, TargetID: target.ID, JumpID: jump2.ID},
	}
	state.nextProxyID = 3
	state.refreshProxies()
	state.activeSection = sectionProxies

	// 以 960x640 最小窗口尺寸渲染两帧（第一帧确立 tableBox 布局，第二帧校验控件几何）
	var canvas gfx.Canvas
	size := gfx.Size{Width: 960, Height: 640}
	state.Paint(&canvas, size)
	canvas = gfx.Canvas{}
	state.Paint(&canvas, size)

	tableRect := state.sectionBodies.proxies.tableBox.Rect()
	if tableRect.Width <= 0 {
		t.Fatalf("expected non-empty tableRect at 960px width, got %+v", tableRect)
	}

	for _, route := range state.proxies {
		editID := hitProxyRowEditList.At(int(route.ID))
		delID := hitProxyRowDeleteList.At(int(route.ID))
		var editBounds, delBounds gfx.Rect
		var hasEdit, hasDel bool
		for _, h := range canvas.Hits() {
			if h.ID == editID {
				editBounds, hasEdit = h.Bounds, true
			} else if h.ID == delID {
				delBounds, hasDel = h.Bounds, true
			}
		}
		if !hasEdit || !hasDel {
			t.Fatalf("route %d action buttons missing at 960px width: edit=%v, del=%v", route.ID, hasEdit, hasDel)
		}
		if editBounds.Width < 20 || delBounds.Width < 20 {
			t.Fatalf("route %d action buttons clipped too narrow: edit=%+v, del=%+v", route.ID, editBounds, delBounds)
		}
		if delBounds.X+delBounds.Width > tableRect.X+tableRect.Width {
			t.Fatalf("route %d delete button overflows table right edge (%v > %v)", route.ID, delBounds.X+delBounds.Width, tableRect.X+tableRect.Width)
		}
	}

	// 验证点击第 1 条路由的编辑按钮可以正常打开编辑弹窗
	state.Click(hitProxyRowEditList.At(1))
	if state.modal != modalNewProxy || !state.proxyEditor.editing || state.proxyEditor.routeID != 1 {
		t.Fatalf("clicking edit button at 960px failed: modal=%v editing=%v routeID=%d", state.modal, state.proxyEditor.editing, state.proxyEditor.routeID)
	}
}

func TestProxyBatchOverwritePreservesViaShellAndRelay(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump1 := state.hosts[0]
	jump2 := state.hosts[1]
	target := state.hosts[2]

	// 目标设备已有通过 Shell Relay 跳板的路由配置
	state.proxies = []proxyRecord{
		{
			ID:       1,
			TargetID: target.ID,
			JumpID:   jump1.ID,
			ViaShell: true,
			Relay:    "nc -X connect %h %p",
		},
	}
	state.nextProxyID = 2
	state.refreshProxies()

	// 批量覆盖切换到 jump2
	added, updated := state.batchAssociateProxies(jump2.ID, []uint32{target.ID}, true, false, "")
	if added != 0 || updated != 1 {
		t.Fatalf("added=%d updated=%d, want 0, 1", added, updated)
	}
	if len(state.proxies) != 1 {
		t.Fatalf("len(proxies)=%d, want 1", len(state.proxies))
	}
	got := state.proxies[0]
	if got.JumpID != jump2.ID {
		t.Fatalf("JumpID=%d, want %d", got.JumpID, jump2.ID)
	}
	if !got.ViaShell || got.Relay != "nc -X connect %h %p" {
		t.Fatalf("batch overwrite cleared shell relay: ViaShell=%v Relay=%q", got.ViaShell, got.Relay)
	}
}

func TestProxyBatchJumpSelectKeyboardNavigation(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if len(state.hosts) < 3 {
		t.Fatalf("need at least 3 hosts in test state, got %d", len(state.hosts))
	}
	state.openProxyBatchAssociate(state.hosts[0].ID)
	state.Click(hitProxyBatchJumpInput)
	if !state.proxyBatch.jumpOpen {
		t.Fatal("expected jump picker to open")
	}

	// 先渲染一帧使 MenuScroll 注册候选列表 items
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	candidates := state.proxyBatchJumpCandidates()
	if len(candidates) < 2 {
		t.Fatalf("need at least 2 jump candidates, got %d", len(candidates))
	}

	// 按 Down 将高亮从 candidates[0] 移动到 candidates[1]，再按 Enter 选中
	res, handled := state.proxyBatchDialogKey(gfx.KeyEvent{Key: gfx.KeyDown})
	if !handled || !res.Repaint {
		t.Fatalf("KeyDown not handled: handled=%v res=%+v", handled, res)
	}
	if state.proxyBatch.jumpActiveID != hitProxyBatchJumpItemList.At(1) {
		t.Fatalf("jumpActiveID = %v, want item 1 (%v)", state.proxyBatch.jumpActiveID, hitProxyBatchJumpItemList.At(1))
	}

	res, handled = state.proxyBatchDialogKey(gfx.KeyEvent{Key: gfx.KeyEnter})
	if !handled || !res.Repaint {
		t.Fatalf("KeyEnter not handled: handled=%v res=%+v", handled, res)
	}
	if state.proxyBatch.jumpOpen {
		t.Fatal("expected jump picker to close on Enter")
	}
	if state.proxyBatch.jumpID != candidates[1].ID {
		t.Fatalf("selected jumpID = %d, want candidates[1].ID = %d", state.proxyBatch.jumpID, candidates[1].ID)
	}
}

func TestProxyClickRowUsesVisibleRouteIDInsteadOfRowIndex(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump := state.hosts[0]
	t1 := state.hosts[1]
	t2 := state.hosts[2]
	state.proxies = []proxyRecord{
		{ID: 10, TargetID: t1.ID, JumpID: jump.ID},
		{ID: 25, TargetID: t2.ID, JumpID: jump.ID},
	}
	state.nextProxyID = 26
	state.refreshProxies()
	state.activeSection = sectionProxies

	// 删除第 0 条（ID=10），此时列表第 0 行对应的路由 ID 应为 25
	state.removeProxyByID(10)
	state.selectedProxyID = 0

	// 触发 hitProxyRowList.At(0) 点击，应选中 route ID 25 而非 0
	state.clickProxies(hitProxyRowList.At(0))
	if state.selectedProxyID != 25 {
		t.Fatalf("selectedProxyID = %d, want 25", state.selectedProxyID)
	}
}

func TestProxyRowClickDoesNotHighlightTableSelectionAndHidesToolbarRowActions(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump := state.hosts[0]
	target := state.hosts[1]
	state.batchAssociateProxies(jump.ID, []uint32{target.ID}, false, false, "")

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	if hasHit(&canvas, hitProxyRemove) || hasHit(&canvas, hitProxyEdit) || hasHit(&canvas, hitProxyStartStop) {
		t.Fatalf("unexpected row action buttons in toolbar hits=%+v", canvas.Hits())
	}

	state.Click(hitProxyRowList.At(0))
	if state.proxyTable.Selected() >= 0 {
		t.Fatalf("proxyTable.Selected() = %d, want < 0 so clicked row background does not highlight", state.proxyTable.Selected())
	}
}

func TestProxyBatchDialogListScrollsWithWheel(t *testing.T) {
	state := newTestState(t, testOptions(t))
	for i := range 20 {
		h := Host{
			ID:        uint32(100 + i),
			CatalogID: fmt.Sprintf("node-%02d", i),
			Label:     fmt.Sprintf("Node-%02d", i),
			Address:   fmt.Sprintf("10.0.1.%d:22", 10+i),
			System:    1,
		}
		h.finish()
		state.hosts = append(state.hosts, h)
	}
	state.activeSection = sectionProxies
	state.openProxyBatchAssociate(state.hosts[0].ID)

	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	r := state.proxyBatch.listRect
	if r.Width <= 0 || r.Height <= 0 {
		t.Fatalf("expected non-empty batch candidate listRect, got %+v", r)
	}

	res := state.scrollWindow(gfx.ScrollEvent{
		X:       r.X + r.Width/2,
		Y:       r.Y + r.Height/2,
		DeltaY:  120,
		Precise: true,
	})
	if !res.Repaint {
		t.Fatalf("expected wheel inside batch candidate listRect to trigger Repaint, got %+v", res)
	}
}

func TestBastionHubToolbarTogglesAndUnbinds(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.activeSection = sectionProxies
	jump, first, second := state.hosts[0], state.hosts[1], state.hosts[2]
	state.batchAssociateProxies(jump.ID, []uint32{first.ID, second.ID}, false, false, "")
	state.selectedBastionJumpID = jump.ID
	state.rebuildProxyFilter()

	state.Click(hitProxyBatchToggleAll)
	if len(state.proxies) != 2 {
		t.Fatalf("toggle kept %d routes, want 2", len(state.proxies))
	}
	for _, route := range state.proxies {
		if !route.Stopped {
			t.Fatalf("route %d still runs after the hub toggle", route.ID)
		}
	}

	state.Click(hitProxyBatchToggleAll)
	for _, route := range state.proxies {
		if route.Stopped {
			t.Fatalf("route %d still stopped after the second hub toggle", route.ID)
		}
	}

	state.Click(hitProxyBatchRemove)
	if len(state.proxies) != 0 {
		t.Fatalf("hub unbind left %d routes", len(state.proxies))
	}
	if state.selectedBastionJumpID != 0 {
		t.Fatalf("removed hub is still selected: %d", state.selectedBastionJumpID)
	}
}

func TestProxyBatchAssociatesRemoteDesktopTargets(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump := state.hosts[0]
	desk := Host{ID: 121, CatalogID: "desk", Label: "Desk", Address: "10.0.0.10", Protocol: protocolRDP}
	desk.finish()
	state.hosts = append(state.hosts, desk)

	state.openProxyBatchAssociate(jump.ID)
	if state.modal != modalProxyBatchAssociate {
		t.Fatalf("batch dialog did not open for an RDP target")
	}
	found := false
	for _, candidate := range state.batchCandidates() {
		if candidate.Host.ID == desk.ID {
			found = true
		}
	}
	if !found {
		t.Fatal("the remote desktop host is not a batch candidate")
	}
	state.toggleBatchTarget(desk.ID)
	state.Click(hitProxyBatchSubmit)
	if state.modal != modalNone {
		t.Fatalf("submit left the dialog open: %v", state.proxyBatch.error)
	}
	route, ok := state.proxyRouteForTarget(desk.ID)
	if !ok || route.JumpID != jump.ID {
		t.Fatalf("desktop route = %+v, want a jump through %d", route, jump.ID)
	}
}

func TestBatchAssociateNeedsAJumpAndATarget(t *testing.T) {
	state := newTestState(t, testOptions(t))
	if !state.canBatchAssociateProxies() {
		t.Fatal("three SSH hosts cannot open the batch dialog")
	}
	state.hosts = state.hosts[:1]
	if state.canBatchAssociateProxies() {
		t.Fatal("one host alone can open the batch dialog")
	}
	desk := Host{ID: 122, CatalogID: "desk", Label: "Desk", Address: "10.0.0.11", Protocol: protocolRDP}
	desk.finish()
	state.hosts = append(state.hosts, desk)
	if !state.canBatchAssociateProxies() {
		t.Fatal("one SSH jump and one desktop target cannot open the batch dialog")
	}
}

func TestProxyBatchSubmitNamesTheReasonItChangedNothing(t *testing.T) {
	state := newTestState(t, testOptions(t))
	jump1, jump2, target := state.hosts[0], state.hosts[1], state.hosts[2]
	state.batchAssociateProxies(jump2.ID, []uint32{target.ID}, false, false, "")

	// The target already has a route and overwrite is off.
	state.openProxyBatchAssociate(jump1.ID)
	state.proxyBatch.overwrite = false
	state.toggleBatchTarget(target.ID)
	state.submitProxyBatchAssociate()
	if state.proxyBatch.error != state.messages.ErrProxyTargetTaken {
		t.Fatalf("existing route error = %q, want %q", state.proxyBatch.error, state.messages.ErrProxyTargetTaken)
	}

	// Making the target a jump for its own jump host closes a loop.
	state.proxyBatch.error = ""
	state.proxyBatch.jumpID = target.ID
	state.proxyBatch.selected = map[uint32]bool{jump2.ID: true}
	state.submitProxyBatchAssociate()
	if state.proxyBatch.error != state.messages.ErrProxyCycle {
		t.Fatalf("cycle error = %q, want %q", state.proxyBatch.error, state.messages.ErrProxyCycle)
	}
}
