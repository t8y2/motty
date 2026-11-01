package workbench

import (
	"fmt"
	"github.com/vivaui/viva/ui"
	"slices"
	"strings"
)

const proxyHostPageSize = 6

// proxyHostPage is the page of hosts that holds id, so opening a select shows
// its selected row instead of its first page.
func proxyHostPage(hosts []Host, id uint32) int {
	for index, host := range hosts {
		if host.ID == id {
			return index / proxyHostPageSize * proxyHostPageSize
		}
	}
	return 0
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
	label   string
	cells   [4]string
}

type proxyEditor struct {
	targetID     uint32
	jumpID       uint32
	targetOpen   bool
	jumpOpen     bool
	targetOffset int
	jumpOffset   int
}

func (editor *proxyEditor) reset(targetID, jumpID uint32) {
	*editor = proxyEditor{targetID: targetID, jumpID: jumpID}
}

func (editor *proxyEditor) validate(state *State) string {
	if _, ok := state.hostByID(editor.targetID); !ok {
		return state.messages.ErrProxyTargetRequired
	}
	if _, ok := state.hostByID(editor.jumpID); !ok {
		return state.messages.ErrProxyJumpRequired
	}
	if editor.targetID == editor.jumpID {
		return state.messages.ErrProxySameHost
	}
	for _, route := range state.proxies {
		if route.TargetID == editor.targetID {
			return state.messages.ErrProxyTargetTaken
		}
	}
	if state.proxyWouldCycle(editor.targetID, editor.jumpID) {
		return state.messages.ErrProxyCycle
	}
	return ""
}

func (state *State) openProxyDialog() {
	if state.sshHostCount() < 2 {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyHostsRequired)
		return
	}
	targetID := state.selectedHostID
	if host, ok := state.hostByID(targetID); !ok || !host.isSSH() || state.proxyTargetConfigured(targetID) {
		targetID = 0
		for _, host := range state.hosts {
			if host.isSSH() && !state.proxyTargetConfigured(host.ID) {
				targetID = host.ID
				break
			}
		}
	}
	if targetID == 0 {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyTargetTaken)
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
	state.proxyDialogError = ""
	state.modal = modalNewProxy
}

func (state *State) clickProxyDialog(id ui.ID) {
	if state.proxyEditor.targetOpen || state.proxyEditor.jumpOpen {
		switch {
		case id == hitProxyDialogDismiss || id == hitProxyDialogClose || id == hitProxyDialogCancel:
			state.closeProxyDialog()
			state.status = state.messages.StatusEditCanceled
		case id == hitProxyDialogSubmit:
			state.proxyEditor.targetOpen = false
			state.proxyEditor.jumpOpen = false
			state.submitProxyDialog()
		case id == hitProxyDialogTarget:
			state.proxyEditor.targetOpen = !state.proxyEditor.targetOpen
			state.proxyEditor.jumpOpen = false
		case id == hitProxyDialogJump:
			state.proxyEditor.jumpOpen = !state.proxyEditor.jumpOpen
			state.proxyEditor.targetOpen = false
		case id == hitProxyTargetDismiss || id == hitProxyJumpDismiss:
			state.proxyEditor.targetOpen = false
			state.proxyEditor.jumpOpen = false
		case id == hitProxyTargetScrollUp:
			if state.proxyEditor.targetOffset > 0 {
				state.proxyEditor.targetOffset--
			}
		case id == hitProxyTargetScrollDown:
			if state.proxyEditor.targetOffset+proxyHostPageSize < len(state.hosts) {
				state.proxyEditor.targetOffset++
			}
		case id == hitProxyJumpScrollUp:
			if state.proxyEditor.jumpOffset > 0 {
				state.proxyEditor.jumpOffset--
			}
		case id == hitProxyJumpScrollDown:
			if state.proxyEditor.jumpOffset+proxyHostPageSize < len(state.hosts) {
				state.proxyEditor.jumpOffset++
			}
		case id.Scope() == hitProxyJumpItemList:
			hostID := uint32(item(id))
			if _, ok := state.hostByID(hostID); ok {
				if hostID == state.proxyEditor.targetID {
					state.proxyEditor.targetID = state.proxyEditor.jumpID
				}
				state.proxyEditor.jumpID = hostID
				state.proxyEditor.jumpOpen = false
			}
		case id.Scope() == hitProxyTargetItemList:
			hostID := uint32(item(id))
			if _, ok := state.hostByID(hostID); ok && !state.proxyTargetConfigured(hostID) {
				if hostID == state.proxyEditor.jumpID {
					state.proxyEditor.jumpID = state.proxyEditor.targetID
				}
				state.proxyEditor.targetID = hostID
				state.proxyEditor.targetOpen = false
			}
		default:
			state.proxyEditor.targetOpen = false
			state.proxyEditor.jumpOpen = false
		}
		return
	}
	switch id {
	case hitProxyDialogDismiss, hitProxyDialogClose, hitProxyDialogCancel:
		state.closeProxyDialog()
		state.status = state.messages.StatusEditCanceled
	case hitProxyDialogTarget:
		state.proxyEditor.targetOpen = true
		state.proxyEditor.targetOffset = proxyHostPage(state.hosts, state.proxyEditor.targetID)
	case hitProxyDialogJump:
		state.proxyEditor.jumpOpen = true
		state.proxyEditor.jumpOffset = proxyHostPage(state.hosts, state.proxyEditor.jumpID)
	case hitProxyDialogSubmit:
		state.submitProxyDialog()
	}
}

func (state *State) submitProxyDialog() {
	if state.proxyDialogError = state.proxyEditor.validate(state); state.proxyDialogError != "" {
		return
	}
	route := proxyRecord{ID: state.nextProxyID, TargetID: state.proxyEditor.targetID, JumpID: state.proxyEditor.jumpID}
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
	state.modal = modalNone
	state.proxyDialogError = ""
	state.proxyEditor = proxyEditor{}
}

func (state *State) clickProxies(id ui.ID) {
	if state.activeSection != sectionProxies || state.modal != modalNone {
		return
	}
	switch {
	case id == hitProxyAdd:
		state.openProxyDialog()
	case id == hitProxyStartStop:
		state.toggleSelectedProxy()
	case id == hitProxyRemove:
		state.removeSelectedProxy()
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
	for index := range state.proxies {
		if state.proxies[index].ID != state.selectedProxyID {
			continue
		}
		removed := state.proxies[index]
		state.proxies = slices.Delete(state.proxies, index, index+1)
		state.selectedProxyID = 0
		if len(state.proxies) > 0 {
			state.selectedProxyID = state.proxies[min(index, len(state.proxies)-1)].ID
		}
		state.sshSyncMark()
		state.refreshProxies()
		state.status = state.messages.ProxyRemoved + state.proxyLabel(removed)
		state.save()
		return
	}
}

func (state *State) toggleSelectedProxy() {
	for index := range state.proxies {
		route := &state.proxies[index]
		if route.ID != state.selectedProxyID {
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
		return
	}
	chain, valid := state.proxyChain(target, false)
	if !valid || len(chain) == 0 {
		route.label = ""
		route.cells = [4]string{}
		return
	}
	var labels strings.Builder
	var targets strings.Builder
	labels.WriteString(target.Label)
	targets.WriteString(target.target)
	for _, hop := range chain {
		labels.WriteString(" → ")
		labels.WriteString(hop.Label)
		targets.WriteString(" → ")
		targets.WriteString(hop.target)
	}
	status := state.messages.ProxyStopped
	if !route.Stopped {
		status = state.messages.ProxyRunning
	}
	route.label = labels.String()
	route.cells = [4]string{target.Label, jump.Label, status, targets.String()}
}

func (state *State) refreshProxies() {
	for index := range state.proxies {
		state.refreshProxy(&state.proxies[index])
	}
	state.refreshProxyCount()
}

func (state *State) refreshProxyCount() {
	state.proxyCountText = fmt.Sprintf(state.messages.ProxyCountFmt, len(state.proxies))
}
