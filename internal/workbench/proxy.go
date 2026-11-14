package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sshconn"
)

type proxyHop struct {
	ID      uint32
	Label   string
	Address string
}

// proxyTableSource reads the jump route page's table cells when the table asks
// for them, one visible cell at a time.
type proxyTableSource struct {
	records *[]proxyRecord
	indices *[]int
	jumpID  *uint32
}

func (source *proxyTableSource) Len() int {
	if source == nil || source.records == nil {
		return 0
	}
	if source.jumpID != nil && *source.jumpID != 0 && source.indices != nil {
		return len(*source.indices)
	}
	return len(*source.records)
}

func (source *proxyTableSource) recordAt(row int) (*proxyRecord, bool) {
	if source == nil || source.records == nil || row < 0 {
		return nil, false
	}
	records := *source.records
	if source.jumpID != nil && *source.jumpID != 0 && source.indices != nil {
		indices := *source.indices
		if row >= len(indices) {
			return nil, false
		}
		idx := indices[row]
		if idx < 0 || idx >= len(records) {
			return nil, false
		}
		return &records[idx], true
	}
	if row >= len(records) {
		return nil, false
	}
	return &records[row], true
}

func (source *proxyTableSource) Cell(row, column int) string {
	route, ok := source.recordAt(row)
	if !ok {
		return ""
	}
	switch column {
	case 1:
		return route.cells[0]
	case 2:
		return route.cells[1]
	default:
		return ""
	}
}

// rebuildProxyFilter 依据当前选中的跳板中枢重新构建可见路由索引切片（复用底层数组，零额外分配）
func (state *State) rebuildProxyFilter() {
	state.proxyFilteredIndices = state.proxyFilteredIndices[:0]
	for i := range state.proxies {
		if state.selectedBastionJumpID == 0 || state.proxies[i].JumpID == state.selectedBastionJumpID {
			state.proxyFilteredIndices = append(state.proxyFilteredIndices, i)
		}
	}
}

// visibleProxyCount 返回当前中枢过滤视图下的路由总数
func (state *State) visibleProxyCount() int {
	if state.selectedBastionJumpID == 0 {
		return len(state.proxies)
	}
	return len(state.proxyFilteredIndices)
}

// visibleProxyAt 返回当前中枢过滤视图中第 row 行的实际路由指针
func (state *State) visibleProxyAt(row int) (*proxyRecord, bool) {
	if row < 0 {
		return nil, false
	}
	if state.selectedBastionJumpID == 0 {
		if row >= len(state.proxies) {
			return nil, false
		}
		return &state.proxies[row], true
	}
	if row >= len(state.proxyFilteredIndices) {
		return nil, false
	}
	idx := state.proxyFilteredIndices[row]
	if idx < 0 || idx >= len(state.proxies) {
		return nil, false
	}
	return &state.proxies[idx], true
}

// syncProxyTable keeps table selection within visible bounds without highlighting clicked rows.
func (state *State) syncProxyTable() {
	if state.proxyTable.Selected() >= state.visibleProxyCount() {
		state.proxyTable.Select(-1)
	}
}

// takeProxyTableSelection adopts the row a key moved the table to.
func (state *State) takeProxyTableSelection() {
	row := state.proxyTable.Selected()
	if route, ok := state.visibleProxyAt(row); ok {
		state.selectProxy(route.ID)
	}
}

type proxyRecord struct {
	ID       uint32
	TargetID uint32
	JumpID   uint32
	// Stopped keeps the route configured but out of Motty's own connections, which
	// then go straight to the target. It is not part of what is synced with the
	// SSH config, which keeps describing the route, and does not touch sessions
	// that are already open.
	Stopped bool
	// ViaShell carries the connection to the target through a relay program run in
	// a shell on the jump host, instead of a forwarded channel: for a jump host
	// that only offers a shell, such as a bastion that has the user switch
	// accounts first. The jump host's own command after login is typed first.
	ViaShell bool
	// Relay is the relay program of a ViaShell route, with the tokens of a proxy
	// command; empty means defaultRelayCommand.
	Relay string
	label string
	cells [4]string
	hops  []proxyHop
}

// defaultRelayCommand connects its standard input and output to the target.
const defaultRelayCommand = "nc %h %p"

// relayCommand is the program a ViaShell route runs on the jump host.
func (route proxyRecord) relayCommand() string {
	if command := strings.TrimSpace(route.Relay); command != "" {
		return command
	}
	return defaultRelayCommand
}

type proxyEditor struct {
	targetID uint32
	jumpID   uint32
	// targetSelect and jumpSelect are the two searchable host pickers. Their
	// queries stay here so typing filters without opening a second surface.
	targetSelect components.SingleSelectState
	jumpSelect   components.SingleSelectState
	targetPopup  gfx.Rect
	jumpPopup    gfx.Rect
	viaShell     bool
	relay        text.Editor
	editing      bool
	routeID      uint32
	// fromHostForm remembers that the host form opened this route dialog, so
	// closing it returns to the form instead of to the page.
	fromHostForm bool
	// dialogBody is where the dialog body laid out the controls painted by hand.
	dialogBody proxyDialogBody
}

// proxyDialogBody is where the route dialog's body put each control it paints
// by hand.
type proxyDialogBody struct {
	target, jump, via, viaHelp, relay ui.Box
}

func (editor *proxyEditor) reset(targetID, jumpID uint32) {
	*editor = proxyEditor{targetID: targetID, jumpID: jumpID}
	editor.relay.Insert(defaultRelayCommand)
}

func (editor *proxyEditor) load(route proxyRecord) {
	*editor = proxyEditor{
		targetID: route.TargetID,
		jumpID:   route.JumpID,
		viaShell: route.ViaShell,
		editing:  true,
		routeID:  route.ID,
	}
	relay := route.Relay
	if relay == "" {
		relay = defaultRelayCommand
	}
	editor.relay.Insert(relay)
}

// syncProxyHostSelects rebuilds both host pickers from the catalog and shows
// the hosts the editor currently holds. It is called when the dialog opens and
// after a choice changes which hosts are selectable, not while painting.
func (state *State) syncProxyHostSelects() {
	editor := &state.proxyEditor
	editor.targetSelect.SetOptions(state.proxyHostOptions(true, editor.routeID))
	editor.jumpSelect.SetOptions(state.proxyHostOptions(false, editor.routeID))
	editor.targetSelect.SetValue(hostSelectValue(editor.targetID))
	editor.jumpSelect.SetValue(hostSelectValue(editor.jumpID))
}

// proxyHostOptions is the closed list of a jump-route host picker: every host
// is offered, and the ones that cannot be part of this route are disabled
// rather than hidden so a search can explain the absence. A target is any host
// a route can reach; a jump is always an SSH host, the only kind it can sign in
// to.
func (state *State) proxyHostOptions(target bool, routeID uint32) []components.SingleSelectOption {
	options := make([]components.SingleSelectOption, 0, len(state.hosts))
	for _, host := range state.hosts {
		disabled := !host.isSSH()
		if target {
			disabled = !host.isRouteTarget() || state.proxyTargetConfiguredExcluding(host.ID, routeID)
		}
		options = append(options, hostSelectOption(host, disabled))
	}
	return options
}

func (editor *proxyEditor) relayCommand() string {
	return strings.TrimSpace(editor.relay.String())
}

func (editor *proxyEditor) validate(state *State) string {
	target, ok := state.hostByID(editor.targetID)
	if !ok || !target.isRouteTarget() {
		return state.messages.ErrProxyTargetRequired
	}
	jump, ok := state.hostByID(editor.jumpID)
	if !ok || !jump.isSSH() {
		// Only an SSH host can be a jump: the route signs in to it.
		return state.messages.ErrProxyJumpRequired
	}
	if editor.targetID == editor.jumpID {
		return state.messages.ErrProxySameHost
	}
	for _, route := range state.proxies {
		if (!editor.editing || route.ID != editor.routeID) && route.TargetID == editor.targetID {
			return state.messages.ErrProxyTargetTaken
		}
	}
	if state.proxyWouldCycleExcluding(editor.targetID, editor.jumpID, editor.routeID) {
		return state.messages.ErrProxyCycle
	}
	if editor.viaShell && sshconn.CheckRelay(sshconn.Relay{Command: editor.relayCommand()}) != nil {
		return state.messages.ErrProxyRelay
	}
	return ""
}

// hostJumpRouteUsable reports whether a route can be made for host now: it is
// a host a route can reach, it has none yet, and enough SSH hosts exist to jump
// through. An SSH target needs a second SSH host for the jump; a remote desktop
// needs the jump alone.
func (state *State) hostJumpRouteUsable(host Host) bool {
	if !host.isRouteTarget() || state.proxyTargetConfigured(host.ID) {
		return false
	}
	if host.isSSH() {
		return state.sshHostCount() >= 2
	}
	return state.sshHostCount() >= 1
}

// canAddJumpRoute reports whether any host could take a route now.
func (state *State) canAddJumpRoute() bool {
	for _, host := range state.hosts {
		if state.hostJumpRouteUsable(host) {
			return true
		}
	}
	return false
}

// canBatchAssociateProxies reports whether the batch dialog has a jump host and
// at least one other host to associate with it.
func (state *State) canBatchAssociateProxies() bool {
	return state.sshHostCount() >= 1 && state.routeTargetCount() >= 2
}

func (state *State) openProxyDialog() {
	if !state.canAddJumpRoute() {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyHostsRequired)
		return
	}
	targetID := state.selectedHostID
	if host, ok := state.hostByID(targetID); !ok || !state.hostJumpRouteUsable(host) {
		targetID = 0
		for _, host := range state.hosts {
			if state.hostJumpRouteUsable(host) {
				targetID = host.ID
				break
			}
		}
	}
	if targetID == 0 {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyTargetTaken)
		return
	}
	state.openProxyDialogForHost(targetID, false)
}

// openProxyDialogForHost opens the route dialog on a chosen target. The host
// form calls it for the host it edits and asks for the dialog to return there.
func (state *State) openProxyDialogForHost(targetID uint32, fromHostForm bool) {
	host, ok := state.hostByID(targetID)
	if !ok || !host.isRouteTarget() {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyTargetRequired)
		return
	}
	if state.sshHostCount() == 0 || (host.isSSH() && state.sshHostCount() < 2) {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyHostsRequired)
		return
	}
	jumpID := uint32(0)
	for _, host := range state.hosts {
		if host.ID != targetID && host.isSSH() {
			jumpID = host.ID
			break
		}
	}
	state.proxyEditor.reset(targetID, jumpID)
	state.proxyEditor.fromHostForm = fromHostForm
	state.syncProxyHostSelects()
	state.proxyDialogError = ""
	state.modal = modalNewProxy
}

// proxyRouteForTarget returns the route that makes a host reachable through
// another, if one is configured. A host is the target of at most one route.
func (state *State) proxyRouteForTarget(targetID uint32) (proxyRecord, bool) {
	for _, route := range state.proxies {
		if route.TargetID == targetID {
			return route, true
		}
	}
	return proxyRecord{}, false
}

func (state *State) openEditProxy() {
	if state.selectedProxyID == 0 {
		return
	}
	state.openEditProxyID(state.selectedProxyID)
}

func (state *State) openEditProxyID(id uint32) {
	route, ok := state.proxyByID(id)
	if !ok {
		return
	}
	state.selectedProxyID = id
	state.proxyEditor.load(route)
	state.syncProxyHostSelects()
	state.proxyDialogError = ""
	state.modal = modalNewProxy
}

// applyProxyTargetChoice commits a target row and keeps the two pickers
// distinct: choosing the host that is currently the jump swaps the two.
func (state *State) applyProxyTargetChoice(id uint32) {
	if !state.chooseProxyTarget(id) {
		return
	}
	state.syncProxyHostSelects()
}

func (state *State) applyProxyJumpChoice(id uint32) {
	if !state.chooseProxyJump(id) {
		return
	}
	state.syncProxyHostSelects()
}

// closeProxyPickers dismisses both pickers and drops their search text.
func (state *State) closeProxyPickers() {
	state.proxyEditor.targetSelect.Dismiss()
	state.proxyEditor.jumpSelect.Dismiss()
}

// proxyDialogKey routes arrows, Enter, Escape and Tab to the host picker that
// has the keyboard. It runs before the dialog's own keys, so Enter chooses the
// highlighted host instead of submitting the form.
func (state *State) proxyDialogKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNewProxy {
		return gfx.EventResult{}, false
	}
	editor := &state.proxyEditor
	var result gfx.EventResult
	switch {
	case event.Target == hitProxyDialogTarget || event.Target == hitProxyTargetTrigger:
		editor.jumpSelect.Open = false
		result = editor.targetSelect.Key(event)
	case event.Target == hitProxyDialogJump || event.Target == hitProxyJumpTrigger:
		editor.targetSelect.Open = false
		result = editor.jumpSelect.Key(event)
	case event.Key == gfx.KeyEscape && editor.targetSelect.Open:
		result = editor.targetSelect.Key(event)
	case event.Key == gfx.KeyEscape && editor.jumpSelect.Open:
		result = editor.jumpSelect.Key(event)
	case event.Key == gfx.KeyTab && editor.targetSelect.Open:
		result = editor.targetSelect.Key(event)
	case event.Key == gfx.KeyTab && editor.jumpSelect.Open:
		result = editor.jumpSelect.Key(event)
	default:
		return gfx.EventResult{}, false
	}
	if result.Handled || result.Repaint {
		state.syncProxyPickerChoices()
	}
	return result, result.Handled || result.Repaint
}

// syncProxyPickerChoices adopts a host chosen from the keyboard. A picker that
// is still open is a search in progress, not a choice.
func (state *State) syncProxyPickerChoices() {
	editor := &state.proxyEditor
	if !editor.targetSelect.Open {
		if hostID, ok := hostSelectID(editor.targetSelect.Value()); ok && hostID != editor.targetID {
			state.applyProxyTargetChoice(hostID)
		}
	}
	if !editor.jumpSelect.Open {
		if hostID, ok := hostSelectID(editor.jumpSelect.Value()); ok && hostID != editor.jumpID {
			state.applyProxyJumpChoice(hostID)
		}
	}
}

// isProxyPickerTarget reports whether a pointer event belongs to either host
// picker, so a press beside an open list closes only the list.
func isProxyPickerTarget(id gfx.ID) bool {
	return id == hitProxyDialogTarget || id == hitProxyTargetTrigger || id == hitProxyTargetScroll ||
		id.Scope() == hitProxyTargetItemList ||
		id == hitProxyDialogJump || id == hitProxyJumpTrigger || id == hitProxyJumpScroll ||
		id.Scope() == hitProxyJumpItemList
}

// proxyPickerPointer lets an open list own its scrollbar and closes it on a
// press anywhere else without discarding the dialog.
func (state *State) proxyPickerPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	editor := &state.proxyEditor
	if !editor.targetSelect.Open && !editor.jumpSelect.Open {
		return gfx.EventResult{}, false
	}
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && !isProxyPickerTarget(event.Target) {
		state.closeProxyPickers()
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if result := editor.targetSelect.Pointer(event); result.Handled {
		return result, true
	}
	if result := editor.jumpSelect.Pointer(event); result.Handled {
		return result, true
	}
	return gfx.EventResult{}, false
}

// scrollProxyDialog gives the wheel to whichever host list is open, and keeps
// it from moving the page behind the dialog.
func (state *State) scrollProxyDialog(event gfx.ScrollEvent) gfx.EventResult {
	editor := &state.proxyEditor
	switch {
	case editor.targetSelect.Open:
		inside := pointInRect(event.X, event.Y, editor.targetPopup) ||
			event.Target == hitProxyDialogTarget || event.Target == hitProxyTargetTrigger ||
			event.Target == hitProxyTargetScroll || event.Target.Scope() == hitProxyTargetItemList
		if inside {
			return gfx.EventResult{Handled: true, Repaint: editor.targetSelect.Wheel(event)}
		}
		return gfx.EventResult{Handled: true}
	case editor.jumpSelect.Open:
		inside := pointInRect(event.X, event.Y, editor.jumpPopup) ||
			event.Target == hitProxyDialogJump || event.Target == hitProxyJumpTrigger ||
			event.Target == hitProxyJumpScroll || event.Target.Scope() == hitProxyJumpItemList
		if inside {
			return gfx.EventResult{Handled: true, Repaint: editor.jumpSelect.Wheel(event)}
		}
		return gfx.EventResult{Handled: true}
	}
	return gfx.EventResult{}
}

func (state *State) clickProxyDialog(id gfx.ID) {
	editor := &state.proxyEditor
	if editor.targetSelect.Open || editor.jumpSelect.Open {
		switch {
		case id == hitProxyDialogClose || id == hitProxyDialogCancel:
			state.closeProxyDialog()
			state.status = state.messages.StatusEditCanceled
		case id == hitProxyDialogSubmit:
			state.closeProxyPickers()
			state.submitProxyDialog()
		case id == hitProxyDialogTarget || id == hitProxyTargetTrigger:
			editor.jumpSelect.Open = false
			editor.targetSelect.Open = true
		case id == hitProxyDialogJump || id == hitProxyJumpTrigger:
			editor.targetSelect.Open = false
			editor.jumpSelect.Open = true
		case id == hitProxyTargetDismiss:
			editor.targetSelect.Open = false
		case id == hitProxyJumpDismiss:
			editor.jumpSelect.Open = false
		case id.Scope() == hitProxyJumpItemList:
			if index, ok := id.In(hitProxyJumpItemList); ok && editor.jumpSelect.Choose(index) {
				if hostID, ok := hostSelectID(editor.jumpSelect.Value()); ok {
					state.applyProxyJumpChoice(hostID)
				}
			}
		case id.Scope() == hitProxyTargetItemList:
			if index, ok := id.In(hitProxyTargetItemList); ok && editor.targetSelect.Choose(index) {
				if hostID, ok := hostSelectID(editor.targetSelect.Value()); ok {
					state.applyProxyTargetChoice(hostID)
				}
			}
		default:
			state.closeProxyPickers()
		}
		return
	}
	switch id {
	case hitProxyDialogClose, hitProxyDialogCancel:
		state.closeProxyDialog()
		state.status = state.messages.StatusEditCanceled
	case hitProxyDialogTarget, hitProxyTargetTrigger:
		editor.targetSelect.Open = true
	case hitProxyDialogJump, hitProxyJumpTrigger:
		editor.jumpSelect.Open = true
	case hitProxyDialogViaShell:
		state.proxyEditor.viaShell = !state.proxyEditor.viaShell
	case hitProxyDialogSubmit:
		state.submitProxyDialog()
	}
}

func (state *State) submitProxyDialog() {
	state.closeProxyPickers()
	if state.proxyDialogError = state.proxyEditor.validate(state); state.proxyDialogError != "" {
		return
	}
	if state.proxyEditor.editing {
		for index := range state.proxies {
			if state.proxies[index].ID != state.proxyEditor.routeID {
				continue
			}
			route := &state.proxies[index]
			route.TargetID = state.proxyEditor.targetID
			route.JumpID = state.proxyEditor.jumpID
			if state.proxyEditor.viaShell {
				route.ViaShell, route.Relay = true, state.proxyEditor.relayCommand()
			} else {
				route.ViaShell, route.Relay = false, ""
			}
			state.sshSyncMark()
			state.refreshProxies()
			state.status = state.messages.ProxyUpdated + route.label
			state.closeProxyDialog()
			state.save()
			return
		}
		// The route this dialog was opened on is gone: saving must not quietly
		// become a second copy of it under a new number.
		state.proxyDialogError = state.messages.ErrProxyEditMissing
		return
	}
	route := proxyRecord{ID: state.nextProxyID, TargetID: state.proxyEditor.targetID, JumpID: state.proxyEditor.jumpID}
	if state.proxyEditor.viaShell {
		route.ViaShell, route.Relay = true, state.proxyEditor.relayCommand()
	}
	state.nextProxyID++
	state.proxies = append(state.proxies, route)
	state.selectedProxyID = route.ID
	state.sshSyncMark()
	state.refreshProxies()
	state.status = state.messages.ProxyAdded + state.proxies[len(state.proxies)-1].label
	state.closeProxyDialog()
	state.save()
}

func (state *State) closeProxyDialog() {
	returnToHostForm := state.proxyEditor.fromHostForm
	state.proxyDialogError = ""
	state.proxyEditor = proxyEditor{}
	if returnToHostForm {
		state.modal = modalNewHost
		return
	}
	state.modal = modalNone
}

func (state *State) clickProxies(id gfx.ID) {
	if state.activeSection != sectionProxies {
		return
	}
	// 二次确认弹窗的处理
	if state.modal == modalProxyDeleteConfirm {
		switch id {
		case hitProxyDeleteConfirmSubmit:
			state.removeProxyByID(state.proxyPendingDeleteID)
			state.modal = modalNone
			state.proxyPendingDeleteID = 0
		case hitProxyDeleteConfirmClose, hitProxyDeleteConfirmCancel:
			state.modal = modalNone
			state.proxyPendingDeleteID = 0
		}
		return
	}
	if state.modal == modalProxyBatchDeleteConfirm {
		switch id {
		case hitProxyBatchDeleteConfirmSubmit:
			state.batchDeleteCheckedProxies()
		case hitProxyBatchDeleteConfirmClose, hitProxyBatchDeleteConfirmCancel:
			state.modal = modalNone
		}
		return
	}

	if state.modal != modalNone {
		return
	}

	switch {
	case id == hitProxyAdd:
		state.openProxyDialog()
	case id == hitProxyBatchAdd:
		state.openProxyBatchAssociate(state.selectedBastionJumpID)
	case id == hitProxyEdit:
		state.openEditProxy()
	case id == hitProxyStartStop:
		state.toggleSelectedProxy()
	case id == hitProxyRemove:
		state.removeSelectedProxy()
	case id == hitProxyCheckAll:
		state.toggleProxyCheckAll()
	case id == hitProxyBatchDelete:
		state.openProxyBatchDeleteConfirm()
	case id == hitProxyBatchStartAll:
		state.batchToggleCheckedProxies(false)
	case id == hitProxyBatchStopAll:
		state.batchToggleCheckedProxies(true)
	case id == hitProxyBatchToggleAll:
		if hub, ok := state.selectedBastionHub(); ok {
			state.toggleBastionHub(hub.JumpHost.ID)
		}
	case id == hitProxyBatchRemove:
		if hub, ok := state.selectedBastionHub(); ok {
			targets := make([]uint32, 0, len(hub.Targets))
			for _, host := range hub.Targets {
				targets = append(targets, host.ID)
			}
			state.batchRemoveProxiesForTargets(targets)
		}
	case id == hitProxyBatchClearSel:
		state.clearProxyChecked()
	case id.Scope() == hitProxyRowList:
		if index, ok := id.In(hitProxyRowList); ok {
			if route, found := state.visibleProxyAt(index); found {
				state.selectProxy(route.ID)
			}
		}
	case id.Scope() == hitProxyRowCheckList:
		if index, ok := id.In(hitProxyRowCheckList); ok {
			state.toggleProxyCheck(uint32(index))
		}
	case id.Scope() == hitProxyRowSwitchList:
		if index, ok := id.In(hitProxyRowSwitchList); ok {
			state.toggleProxyByID(uint32(index))
		}
	case id.Scope() == hitProxyRowEditList:
		if index, ok := id.In(hitProxyRowEditList); ok {
			state.openEditProxyID(uint32(index))
		}
	case id.Scope() == hitProxyRowDeleteList:
		if index, ok := id.In(hitProxyRowDeleteList); ok {
			state.openProxyDeleteConfirm(uint32(index))
		}
	case id.Scope() == hitProxyHubTabList:
		if index, ok := id.In(hitProxyHubTabList); ok {
			hubs := state.bastionHubList()
			if index == 0 {
				state.selectedBastionJumpID = 0
			} else if index-1 < len(hubs) {
				state.selectedBastionJumpID = hubs[index-1].JumpHost.ID
			}
			state.rebuildProxyFilter()
			state.proxyTable.Scroll.Offset = 0
			state.proxyTable.Select(-1)
			if state.visibleProxyCount() > 0 {
				if first, found := state.visibleProxyAt(0); found {
					state.selectedProxyID = first.ID
				}
			}
		}
	}
}

// selectProxy makes a route the selected one. The table calls it from the paint
// that delivers the click on its row.
func (state *State) selectProxy(id uint32) {
	if route, ok := state.proxyByID(id); ok {
		state.selectedProxyID = id
		state.status = state.messages.ProxySelected + state.proxyLabel(route)
	}
}

func (state *State) removeSelectedProxy() {
	if state.selectedProxyID != 0 {
		state.removeProxyByID(state.selectedProxyID)
	}
}

// removeProxyByID 删除指定跳板路由
func (state *State) removeProxyByID(routeID uint32) {
	for index := range state.proxies {
		if state.proxies[index].ID != routeID {
			continue
		}
		removed := state.proxies[index]
		state.proxies = slices.Delete(state.proxies, index, index+1)
		if state.proxyChecked != nil {
			delete(state.proxyChecked, routeID)
		}
		if state.selectedProxyID == routeID {
			state.selectedProxyID = 0
			if len(state.proxies) > 0 {
				state.selectedProxyID = state.proxies[min(index, len(state.proxies)-1)].ID
			}
		}
		state.sshSyncMark()
		state.refreshProxies()
		state.status = state.messages.ProxyRemoved + state.proxyLabel(removed)
		state.save()
		return
	}
}

// toggleProxyByID 就地启停指定跳板路由
func (state *State) toggleProxyByID(routeID uint32) {
	for index := range state.proxies {
		route := &state.proxies[index]
		if route.ID != routeID {
			continue
		}
		route.Stopped = !route.Stopped
		state.refreshProxies()
		state.status = state.messages.ProxyStartedStatus + route.label
		if route.Stopped {
			state.status = state.messages.ProxyStoppedStatus + route.label
		}
		state.save()
		return
	}
}

func (state *State) toggleSelectedProxy() {
	if state.selectedProxyID != 0 {
		state.toggleProxyByID(state.selectedProxyID)
	}
}

// openProxyDeleteConfirm 打开单条路由删除确认弹窗
func (state *State) openProxyDeleteConfirm(routeID uint32) {
	if _, ok := state.proxyByID(routeID); !ok {
		return
	}
	state.proxyPendingDeleteID = routeID
	state.modal = modalProxyDeleteConfirm
}

// openProxyBatchDeleteConfirm 打开批量删除确认弹窗
func (state *State) openProxyBatchDeleteConfirm() {
	if state.proxyCheckedCount() == 0 {
		return
	}
	state.modal = modalProxyBatchDeleteConfirm
}

// batchDeleteCheckedProxies 批量删除所有已勾选的跳板路由
func (state *State) batchDeleteCheckedProxies() int {
	if len(state.proxyChecked) == 0 {
		state.modal = modalNone
		return 0
	}
	var remaining []proxyRecord
	deleted := 0
	for _, route := range state.proxies {
		if state.proxyChecked[route.ID] {
			deleted++
			if state.selectedProxyID == route.ID {
				state.selectedProxyID = 0
			}
			continue
		}
		remaining = append(remaining, route)
	}
	if deleted > 0 {
		state.proxies = remaining
		clear(state.proxyChecked)
		if len(state.proxies) > 0 && state.selectedProxyID == 0 {
			state.selectedProxyID = state.proxies[0].ID
		}
		state.sshSyncMark()
		state.refreshProxies()
		state.status = fmt.Sprintf(state.messages.ProxyBatchUnboundCountFmt, deleted)
		state.save()
	}
	state.modal = modalNone
	return deleted
}

// batchToggleCheckedProxies 批量启动/停止已勾选的跳板路由
func (state *State) batchToggleCheckedProxies(stopped bool) {
	if len(state.proxyChecked) == 0 {
		return
	}
	changed := 0
	for index := range state.proxies {
		route := &state.proxies[index]
		if state.proxyChecked[route.ID] && route.Stopped != stopped {
			route.Stopped = stopped
			changed++
		}
	}
	if changed > 0 {
		state.refreshProxies()
		state.save()
	}
}

// toggleProxyCheck 切换单行勾选状态
func (state *State) toggleProxyCheck(routeID uint32) {
	if state.proxyChecked == nil {
		state.proxyChecked = make(map[uint32]bool)
	}
	if state.proxyChecked[routeID] {
		delete(state.proxyChecked, routeID)
	} else {
		state.proxyChecked[routeID] = true
	}
}

// clearProxyChecked 清空所有勾选
func (state *State) clearProxyChecked() {
	if state.proxyChecked != nil {
		clear(state.proxyChecked)
	}
}

// proxyCheckedCount 返回当前已勾选的数量
func (state *State) proxyCheckedCount() int {
	return len(state.proxyChecked)
}

// proxyVisibleStats 统计当前中枢过滤视图下的路由总数与已勾选数（零堆内存分配）
func (state *State) proxyVisibleStats() (total int, checked int) {
	for _, r := range state.proxies {
		if state.selectedBastionJumpID != 0 && r.JumpID != state.selectedBastionJumpID {
			continue
		}
		total++
		if state.proxyChecked != nil && state.proxyChecked[r.ID] {
			checked++
		}
	}
	return total, checked
}

// toggleProxyCheckAll 切换当前可见视图全选/反选（零堆内存分配）
func (state *State) toggleProxyCheckAll() {
	if state.proxyChecked == nil {
		state.proxyChecked = make(map[uint32]bool)
	}
	total, checked := state.proxyVisibleStats()
	if total == 0 {
		return
	}
	allChecked := total == checked
	for _, r := range state.proxies {
		if state.selectedBastionJumpID != 0 && r.JumpID != state.selectedBastionJumpID {
			continue
		}
		if allChecked {
			delete(state.proxyChecked, r.ID)
		} else {
			state.proxyChecked[r.ID] = true
		}
	}
}

func (state *State) proxyByID(id uint32) (proxyRecord, bool) {
	for _, route := range state.proxies {
		if route.ID == id {
			return route, true
		}
	}
	return proxyRecord{}, false
}

func (state *State) proxyLabel(route proxyRecord) string {
	return route.label
}

func (state *State) refreshProxy(route *proxyRecord) {
	target, targetOK := state.hostByID(route.TargetID)
	jump, jumpOK := state.hostByID(route.JumpID)
	if !targetOK || !jumpOK {
		route.label = ""
		route.cells = [4]string{}
		route.hops = nil
		return
	}
	chain, valid := state.proxyChain(target, false)
	if !valid || len(chain) == 0 {
		route.label = ""
		route.cells = [4]string{}
		route.hops = nil
		return
	}
	var labels strings.Builder
	var targets strings.Builder
	var hops []proxyHop
	for index := len(chain) - 1; index >= 0; index-- {
		hop := chain[index]
		labels.WriteString(hop.Label)
		labels.WriteString(" → ")
		targets.WriteString(hop.target)
		targets.WriteString(" → ")
		addr := hop.target
		if addr == "" {
			addr = hop.Address
		}
		hops = append(hops, proxyHop{ID: hop.ID, Label: hop.Label, Address: addr})
	}
	labels.WriteString(target.Label)
	targets.WriteString(target.target)
	targetAddr := target.target
	if targetAddr == "" {
		targetAddr = target.Address
	}
	hops = append(hops, proxyHop{ID: target.ID, Label: target.Label, Address: targetAddr})
	route.hops = hops
	status := state.messages.ProxyStopped
	if !route.Stopped {
		status = state.messages.ProxyRunning
	}
	route.label = labels.String()
	jumpCell := jump.Label
	if route.ViaShell {
		jumpCell += " · " + state.messages.ProxyViaShellTag
	}
	route.cells = [4]string{target.Label, jumpCell, status, targets.String()}
}

func (state *State) refreshProxies() {
	for index := range state.proxies {
		state.refreshProxy(&state.proxies[index])
	}
	state.refreshProxyCount()
	state.refreshBastionHubs()
	resetHub := false
	if state.selectedBastionJumpID != 0 {
		found := false
		for _, hub := range state.bastionHubList() {
			if hub.JumpHost.ID == state.selectedBastionJumpID {
				found = true
				break
			}
		}
		if !found {
			// The selected hub lost its last route: show all routes again
			// rather than leave the table filtered to nothing.
			state.selectedBastionJumpID = 0
			state.proxyTable.Select(-1)
			resetHub = true
		}
	}
	state.rebuildProxyFilter()
	if resetHub {
		state.selectedProxyID = 0
		if state.visibleProxyCount() > 0 {
			if first, ok := state.visibleProxyAt(0); ok {
				state.selectedProxyID = first.ID
			}
		}
	}
}

func (state *State) refreshProxyCount() {
	state.proxyCountText = fmt.Sprintf(state.messages.ProxyCountFmt, len(state.proxies))
}

// bastionHub 代表一个跳板机中枢及其管辖的路由与目标设备集合
type bastionHub struct {
	JumpHost  Host
	Routes    []proxyRecord
	Targets   []Host
	Active    int
	Total     int
	ChipLabel string
}

func (state *State) refreshBastionHubs() {
	state.bastionAllChipText = fmt.Sprintf("%s (%d)", state.messages.HostFilterAll, len(state.proxies))
	if len(state.proxies) == 0 {
		state.bastionHubs = nil
		return
	}
	jumpMap := make(map[uint32]*bastionHub)
	var orderedJumps []uint32

	for _, route := range state.proxies {
		hub, exists := jumpMap[route.JumpID]
		if !exists {
			jumpHost, ok := state.hostByID(route.JumpID)
			if !ok {
				continue
			}
			hub = &bastionHub{JumpHost: jumpHost}
			jumpMap[route.JumpID] = hub
			orderedJumps = append(orderedJumps, route.JumpID)
		}
		targetHost, ok := state.hostByID(route.TargetID)
		if !ok {
			continue
		}
		hub.Routes = append(hub.Routes, route)
		hub.Targets = append(hub.Targets, targetHost)
		hub.Total++
		if !route.Stopped {
			hub.Active++
		}
	}

	result := make([]bastionHub, 0, len(orderedJumps))
	for _, jid := range orderedJumps {
		hub := *jumpMap[jid]
		hub.ChipLabel = fmt.Sprintf("%s (%d)", hub.JumpHost.Label, hub.Total)
		result = append(result, hub)
	}
	state.bastionHubs = result
}

// bastionHubList 返回已缓存的跳板机中枢集合，保证渲染过程零分配
func (state *State) bastionHubList() []bastionHub {
	return state.bastionHubs
}

// selectedBastionHub 获取当前选中的跳板机中枢，若未指定或失效则返回 false（无副作用）
func (state *State) selectedBastionHub() (bastionHub, bool) {
	hubs := state.bastionHubs
	if len(hubs) == 0 {
		return bastionHub{}, false
	}
	if state.selectedBastionJumpID != 0 {
		for _, hub := range hubs {
			if hub.JumpHost.ID == state.selectedBastionJumpID {
				return hub, true
			}
		}
	}
	return bastionHub{}, false
}

// batchAssociateProxies 批量将多台目标设备关联至指定跳板机
func (state *State) batchAssociateProxies(jumpID uint32, targetIDs []uint32, overwrite bool, viaShell bool, relay string) (added int, updated int) {
	jumpHost, ok := state.hostByID(jumpID)
	if !ok || !jumpHost.isSSH() {
		return 0, 0
	}

	for _, targetID := range targetIDs {
		if targetID == jumpID {
			continue
		}
		targetHost, ok := state.hostByID(targetID)
		if !ok || !targetHost.isRouteTarget() {
			continue
		}

		// 环路检查
		if state.proxyWouldCycle(targetID, jumpID) {
			continue
		}

		// 检查目标是否已有路由
		existingRoute, hasExisting := state.proxyRouteForTarget(targetID)
		if hasExisting {
			if !overwrite {
				continue
			}
			// 覆盖现有路由时仅更新跳板机，保留原有的 ViaShell 与 Relay 配置，避免静默清空造成断连
			for i := range state.proxies {
				if state.proxies[i].ID == existingRoute.ID {
					state.proxies[i].JumpID = jumpID
					updated++
					break
				}
			}
		} else {
			// 新增跳板路由
			route := proxyRecord{
				ID:       state.nextProxyID,
				TargetID: targetID,
				JumpID:   jumpID,
				ViaShell: viaShell,
				Relay:    relay,
			}
			state.nextProxyID++
			state.proxies = append(state.proxies, route)
			added++
		}
	}

	if added > 0 || updated > 0 {
		if state.selectedBastionJumpID != 0 {
			state.selectedBastionJumpID = jumpID
		}
		state.sshSyncMark()
		state.refreshProxies()
		state.status = fmt.Sprintf(state.messages.ProxyBatchAssociatedCountFmt, added+updated, jumpHost.Label)
		state.save()
	}
	return added, updated
}

// batchRemoveProxiesForTargets 批量移除指定目标设备的跳板路由
func (state *State) batchRemoveProxiesForTargets(targetIDs []uint32) int {
	if len(targetIDs) == 0 {
		return 0
	}
	targets := make(map[uint32]bool, len(targetIDs))
	for _, id := range targetIDs {
		targets[id] = true
	}

	kept := state.proxies[:0]
	removed := 0
	for _, route := range state.proxies {
		if targets[route.TargetID] {
			removed++
			continue
		}
		kept = append(kept, route)
	}
	if removed > 0 {
		state.proxies = kept
		state.selectedProxyID = 0
		if len(state.proxies) > 0 {
			state.selectedProxyID = state.proxies[0].ID
		}
		state.sshSyncMark()
		state.refreshProxies()
		state.status = fmt.Sprintf(state.messages.ProxyBatchUnboundCountFmt, removed)
		state.save()
	}
	return removed
}

// toggleBastionHub 批量切换某个跳板机下所有路由的生效/暂停状态
func (state *State) toggleBastionHub(jumpID uint32) {
	hub, found := func() (bastionHub, bool) {
		for _, h := range state.bastionHubList() {
			if h.JumpHost.ID == jumpID {
				return h, true
			}
		}
		return bastionHub{}, false
	}()
	if !found || len(hub.Routes) == 0 {
		return
	}

	// 若当前有任何运行中的，则全部暂停；否则全部恢复运行
	newStopped := hub.Active > 0

	for i := range state.proxies {
		if state.proxies[i].JumpID == jumpID {
			state.proxies[i].Stopped = newStopped
		}
	}
	state.refreshProxies()
	state.save()
}
