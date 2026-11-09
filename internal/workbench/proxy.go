package workbench

import (
	"fmt"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sshconn"
)

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
// rather than hidden so a search can explain the absence.
func (state *State) proxyHostOptions(target bool, routeID uint32) []components.SingleSelectOption {
	options := make([]components.SingleSelectOption, 0, len(state.hosts))
	for _, host := range state.hosts {
		disabled := !host.isSSH()
		if target {
			disabled = disabled || state.proxyTargetConfiguredExcluding(host.ID, routeID)
		}
		options = append(options, hostSelectOption(host, disabled))
	}
	return options
}

func (editor *proxyEditor) relayCommand() string {
	return strings.TrimSpace(editor.relay.String())
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
	state.openProxyDialogForHost(targetID, false)
}

// openProxyDialogForHost opens the route dialog on a chosen target. The host
// form calls it for the host it edits and asks for the dialog to return there.
func (state *State) openProxyDialogForHost(targetID uint32, fromHostForm bool) {
	if state.sshHostCount() < 2 {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyHostsRequired)
		return
	}
	host, ok := state.hostByID(targetID)
	if !ok || !host.isSSH() {
		state.showActionWarning(state.messages.ProxyDialogTitle, state.messages.ErrProxyTargetRequired)
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
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, editor.targetPopup) && editor.targetSelect.Wheel(event)}
	case editor.jumpSelect.Open:
		return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, editor.jumpPopup) && editor.jumpSelect.Wheel(event)}
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
	if state.activeSection != sectionProxies || state.modal != modalNone {
		return
	}
	switch {
	case id == hitProxyAdd:
		state.openProxyDialog()
	case id == hitProxyEdit:
		state.openEditProxy()
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
}

func (state *State) refreshProxyCount() {
	state.proxyCountText = fmt.Sprintf(state.messages.ProxyCountFmt, len(state.proxies))
}
