package workbench

import (
	"context"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/portforward"
	"motty/internal/sshconn"
)

// defaultForwardBindAddress keeps a newly created forward private to this
// computer. A record without an address means the same.
const defaultForwardBindAddress = "127.0.0.1"

type forwardRecord struct {
	ID          uint32
	Label       string
	Mode        portforward.Mode
	HostLabel   string
	HostAddress string
	HostUser    string
	HostPort    uint16
	// BindAddress is the address this device's listener binds to. Empty means
	// defaultForwardBindAddress; a non-loopback address comes only from this
	// device's ForwardBindAddresses or the user editing the dialog here.
	BindAddress     string
	LocalPort       uint16
	DestinationHost string
	DestinationPort uint16
	// Resume makes the forward wait through an automatic reconnect of its tab
	// and start again once it is connected, instead of stopping.
	Resume bool
	// AutoStart starts the forward whenever a tab connects to its host, so it
	// does not have to be opened by hand from the forwards page.
	AutoStart bool
	// ViaShell carries each connection through a relay program run in a shell on
	// the host, for a server that refuses forwarded channels: the shell becomes
	// the TCP stream. Relay is that program, with the tokens of a proxy command;
	// empty means defaultRelayCommand.
	ViaShell bool
	Relay    string

	runner        *portforward.Forward
	runnerVersion uint64
	sessionID     uint32
	pending       bool
	starting      bool
	startID       uint64
	lastError     string
	detail        string
	cells         [5]string
}

type forwardStartResult struct {
	forwardID uint32
	startID   uint64
	runner    *portforward.Forward
	err       error
}

// initForwardList names the forward list's rows by their forward numbers, so a
// row keeps its keyboard place as forwards are added above it or removed.
func (state *State) initForwardList() {
	state.forwardRowDecl = state.declareForwardRow
	state.forwardList.Key = func(row int) int {
		if row >= 0 && row < len(state.forwards) {
			return int(state.forwards[row].ID)
		}
		return row
	}
	state.forwardList.Label = func(row int) string {
		if row >= 0 && row < len(state.forwards) {
			return state.forwards[row].Label
		}
		return ""
	}
}

// syncForwardList makes the list show the forward the application selected.
func (state *State) syncForwardList() {
	row := -1
	for index := range state.forwards {
		if state.forwards[index].ID == state.selectedForwardID {
			row = index
			break
		}
	}
	state.forwardList.Select(row)
}

// takeForwardListSelection adopts the row a click or a key moved the list to.
func (state *State) takeForwardListSelection() {
	row := state.forwardList.Selected()
	if row >= 0 && row < len(state.forwards) {
		state.selectForward(state.forwards[row].ID)
	}
}

// forwardListKey moves the forward list's keyboard row: the arrows, Home and End
// and Page Up and Down, from a row that has the keyboard, as the canvas table
// did.
func (state *State) forwardListKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.activeSection != sectionForwards || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	if event.Modifiers&(gfx.ModifierControl|gfx.ModifierMeta|gfx.ModifierAlt) != 0 {
		return gfx.EventResult{}, false
	}
	row, ok := event.Target.In(hitForwardRowList)
	if !ok {
		return gfx.EventResult{}, false
	}
	last := len(state.forwards) - 1
	if last < 0 {
		return gfx.EventResult{}, false
	}
	first, lastVisible := state.forwardList.Visible()
	page := max(lastVisible-first, 1)
	switch event.Key {
	case gfx.KeyUp:
		row--
	case gfx.KeyDown:
		row++
	case gfx.KeyPageUp:
		row -= page
	case gfx.KeyPageDown:
		row += page
	case gfx.KeyHome:
		row = 0
	case gfx.KeyEnd:
		row = last
	default:
		return gfx.EventResult{}, false
	}
	row = min(max(row, 0), last)
	state.forwardList.Select(row)
	state.forwardList.ScrollIntoView(row)
	state.takeForwardListSelection()
	return gfx.EventResult{
		Handled: true, Repaint: true,
		Focus: hitForwardRowList.At(row), FocusVisible: true,
	}, true
}

// listenHost is the address the listener binds to, with older records that
// saved no address kept on loopback.
func (forward *forwardRecord) listenHost() string {
	if forward.BindAddress == "" {
		return defaultForwardBindAddress
	}
	return forward.BindAddress
}

func (forward *forwardRecord) listenAddress() string {
	return net.JoinHostPort(forward.listenHost(), strconv.Itoa(int(forward.LocalPort)))
}

// forwardBindKey names a forward in the device-local listener map. The label
// keeps two forwards that share host, ports and destination but differ by bind
// from overwriting each other's address.
func forwardBindKey(forward forwardRecord) string {
	return joinKey(forward.Label, forward.savedMode(), forward.HostAddress, forward.HostUser,
		strconv.Itoa(int(forward.HostPort)), strconv.Itoa(int(forward.LocalPort)),
		forward.DestinationHost, strconv.Itoa(int(forward.DestinationPort)))
}

// bindAddressForSave keeps the default out of the device document, so a forward
// that never changed the setting needs no local entry.
func bindAddressForSave(address string) string {
	if address == defaultForwardBindAddress {
		return ""
	}
	return address
}

// validForwardBindAddress reports whether address is a literal IP address, the
// only form the listener accepts. An empty address means the default.
func validForwardBindAddress(address string) bool {
	return address == "" || net.ParseIP(address) != nil
}

func (forward *forwardRecord) destination() string {
	if forward.Mode != portforward.ModeLocal && forward.Mode != portforward.ModeRemote {
		return ""
	}
	return net.JoinHostPort(forward.DestinationHost, strconv.Itoa(int(forward.DestinationPort)))
}

func (forward *forwardRecord) displayListener(messages i18n.Messages) string {
	address := forward.listenAddress()
	if forward.Mode == portforward.ModeRemote {
		return fmt.Sprintf(messages.ForwardRemoteListenerFmt, address)
	}
	return address
}

func (forward *forwardRecord) displayDestination(messages i18n.Messages) string {
	switch forward.Mode {
	case portforward.ModeSOCKS5:
		return messages.ForwardModeSOCKS5
	case portforward.ModeHTTPConnect:
		return messages.ForwardModeHTTPConnect
	default:
		return forward.destination()
	}
}

func (forward *forwardRecord) savedMode() string {
	switch forward.Mode {
	case portforward.ModeRemote:
		return "remote"
	case portforward.ModeSOCKS5:
		return "socks5"
	case portforward.ModeHTTPConnect:
		return "http-connect"
	default:
		return ""
	}
}

func (forward forwardRecord) matchesHost(host Host) bool {
	return forward.HostAddress == host.Address && forward.HostUser == host.User && forward.HostPort == host.Port
}

// relayCommand is the program a ViaShell forward runs on the host.
func (forward *forwardRecord) relayCommand() string {
	if command := strings.TrimSpace(forward.Relay); command != "" {
		return command
	}
	return defaultRelayCommand
}

func (forward *forwardRecord) refresh(messages i18n.Messages, hostAvailable bool) {
	status := messages.ForwardStopped
	forward.detail = forward.lastError
	switch {
	case !hostAvailable:
		status = messages.ForwardHostUnavailable
	case forward.pending:
		status = messages.ForwardWaiting
		forward.detail = ""
	case forward.starting:
		status = messages.ForwardStarting
		forward.detail = ""
	case forward.runner != nil:
		runtime := forward.runner.Status()
		forward.runnerVersion = runtime.Version
		if runtime.LastError != nil {
			forward.lastError = runtime.LastError.Error()
			forward.detail = forwardFailureText(messages, *forward, runtime.LastError)
			status = messages.ForwardRunningError
		} else {
			forward.lastError = ""
			forward.detail = fmt.Sprintf(messages.ForwardTrafficFmt, runtime.Accepted, runtime.Uploaded, runtime.Downloaded)
			status = fmt.Sprintf(messages.ForwardRunningFmt, runtime.Active)
		}
	case forward.lastError != "":
		status = messages.ForwardStoppedError
	}
	forward.cells = [5]string{forward.Label, forward.HostLabel, forward.displayListener(messages), forward.displayDestination(messages), status}
}

// forwardFailureText is what a failed connection says in the page: a server
// that refuses the channel gets an explanation naming the way out instead of
// the protocol's own words.
func forwardFailureText(messages i18n.Messages, forward forwardRecord, err error) string {
	if !sshconn.IsChannelRefused(err) {
		return err.Error()
	}
	if forward.ViaShell {
		return messages.ForwardRelayRefused
	}
	return messages.ForwardRefusedHint
}

func (forward *forwardRecord) stop() {
	if forward.runner != nil {
		runner := forward.runner
		if forward.Mode == portforward.ModeRemote {
			go runner.Close()
		} else {
			runner.CloseAsync()
		}
	}
	forward.runner = nil
	forward.runnerVersion = 0
	forward.sessionID = 0
	forward.pending = false
	forward.starting = false
	forward.startID = 0
}

// hold closes what the forward runs and leaves it waiting for its session to
// connect again, as it waits for a session that is still connecting.
func (forward *forwardRecord) hold() {
	sessionID := forward.sessionID
	forward.stop()
	forward.sessionID = sessionID
	forward.pending = true
	forward.lastError = ""
}

type forwardEditor struct {
	label           text.Editor
	bindAddress     text.Editor
	localPort       text.Editor
	destinationHost text.Editor
	destinationPort text.Editor
	hostID          uint32
	// hostChoice is the page host combobox's value: when the user picks a host
	// it changes, and the dialog adopts it.
	hostChoice uint32
	// hostChoices are the hosts the combobox offers, rebuilt when the dialog is
	// declared.
	hostChoices []uint32
	// hostRow is the labelled row the host combobox sits in.
	hostRow   ui.FormFieldState
	mode      portforward.Mode
	resume    bool
	autoStart bool
	viaShell  bool
	relay     text.Editor
	editing   bool
	forwardID uint32
	// dialogBody is where the dialog body laid out the controls painted by hand.
	dialogBody forwardDialogBody
	// The dialog's text fields are page fields, indexed by forwardField: a labelled row and a
	// text bound to the editor above, which the dialog binds when it is first declared.
	rows  [forwardFieldCount]ui.FormFieldState
	texts [forwardFieldCount]ui.Field
	bound bool
}

// forwardDialogBody is where the forward editor's body put each control it
// paints by hand.
type forwardDialogBody struct {
	viaShell, viaShellHelp   ui.Box
	resume, resumeHelp       ui.Box
	autoStart, autoStartHelp ui.Box
}

type forwardField uint8

const (
	forwardFieldLabel forwardField = iota
	forwardFieldBindAddress
	forwardFieldLocalPort
	forwardFieldDestinationHost
	forwardFieldDestinationPort
	forwardFieldRelay
	forwardFieldCount
)

func (editor *forwardEditor) field(field forwardField) *text.Editor {
	switch field {
	case forwardFieldLabel:
		return &editor.label
	case forwardFieldBindAddress:
		return &editor.bindAddress
	case forwardFieldLocalPort:
		return &editor.localPort
	case forwardFieldDestinationHost:
		return &editor.destinationHost
	case forwardFieldRelay:
		return &editor.relay
	default:
		return &editor.destinationPort
	}
}

func (editor *forwardEditor) value(field forwardField) string {
	return strings.TrimSpace(editor.field(field).String())
}

func (editor *forwardEditor) reset(hostID uint32) {
	*editor = forwardEditor{hostID: hostID, hostChoice: hostID}
	digits := func(value rune) bool { return value >= '0' && value <= '9' }
	editor.localPort.Restrict(digits, 5)
	editor.destinationPort.Restrict(digits, 5)
	editor.bindAddress.Insert(defaultForwardBindAddress)
	editor.localPort.Insert("8080")
	editor.destinationPort.Insert("80")
	editor.relay.Insert(defaultRelayCommand)
}

func (editor *forwardEditor) load(forward forwardRecord, hostID uint32) {
	*editor = forwardEditor{
		hostID:     hostID,
		hostChoice: hostID,
		mode:       forward.Mode,
		resume:     forward.Resume,
		autoStart:  forward.AutoStart,
		viaShell:   forward.ViaShell,
		editing:    true,
		forwardID:  forward.ID,
	}
	digits := func(value rune) bool { return value >= '0' && value <= '9' }
	editor.localPort.Restrict(digits, 5)
	editor.destinationPort.Restrict(digits, 5)
	editor.label.Insert(forward.Label)
	editor.bindAddress.Insert(forward.listenHost())
	editor.localPort.Insert(strconv.Itoa(int(forward.LocalPort)))
	if forward.DestinationHost != "" {
		editor.destinationHost.Insert(forward.DestinationHost)
	}
	if forward.DestinationPort != 0 {
		editor.destinationPort.Insert(strconv.Itoa(int(forward.DestinationPort)))
	}
	editor.relay.Insert(forward.relayCommand())
}

func (editor *forwardEditor) usesClientDestination() bool {
	return modeUsesClientDestination(editor.mode)
}

// relayCommand is the program a ViaShell forward runs on the host, as typed.
func (editor *forwardEditor) relayCommand() string {
	if command := strings.TrimSpace(editor.relay.String()); command != "" {
		return command
	}
	return defaultRelayCommand
}

// modeUsesClientDestination reports whether a forward of this kind ends at the
// client, so the destination fields are not shown.
func modeUsesClientDestination(mode portforward.Mode) bool {
	return mode == portforward.ModeSOCKS5 || mode == portforward.ModeHTTPConnect
}

// modeUsesServerDestination reports whether a forward of this kind dials its
// destination through the SSH server, which is when relaying through the host's
// shell can stand in for a forwarded channel. A remote forward listens on the
// server and dials locally, so it cannot.
func modeUsesServerDestination(mode portforward.Mode) bool {
	return mode != portforward.ModeRemote
}

func (editor *forwardEditor) port(field forwardField) (uint16, bool) {
	value, err := strconv.Atoi(editor.value(field))
	if err != nil || value < 1 || value > 65535 {
		return 0, false
	}
	return uint16(value), true
}

// bindHost is the address the editor will bind to, with an empty field falling
// back to the private default.
func (editor *forwardEditor) bindHost() string {
	if value := editor.value(forwardFieldBindAddress); value != "" {
		return value
	}
	return defaultForwardBindAddress
}

// bindExposed reports whether the chosen address reaches beyond this computer
// (or beyond the SSH server, for a remote forward), which is what the dialog
// warns about.
func (editor *forwardEditor) bindExposed() bool {
	ip := net.ParseIP(editor.bindHost())
	return ip != nil && !ip.IsLoopback()
}

func (editor *forwardEditor) validate(state *State) string {
	if editor.value(forwardFieldLabel) == "" {
		return state.messages.ErrForwardNameRequired
	}
	if _, ok := state.hostByID(editor.hostID); !ok {
		if editor.editing {
			return state.messages.ErrForwardHostMissing
		}
		return state.messages.ErrForwardHostRequired
	}
	if !validForwardBindAddress(editor.value(forwardFieldBindAddress)) {
		return state.messages.ErrForwardBindInvalid
	}
	if _, ok := editor.port(forwardFieldLocalPort); !ok {
		return state.messages.ErrForwardPortInvalid
	}
	if !editor.usesClientDestination() {
		if _, ok := editor.port(forwardFieldDestinationPort); !ok {
			return state.messages.ErrForwardPortInvalid
		}
		destination := editor.value(forwardFieldDestinationHost)
		if destination == "" || strings.ContainsAny(destination, " \t\r\n") {
			return state.messages.ErrForwardDestinationRequired
		}
	}
	if editor.viaShell && modeUsesServerDestination(editor.mode) {
		if err := sshconn.CheckRelay(sshconn.Relay{Command: editor.relayCommand()}); err != nil {
			return fmt.Sprintf(state.messages.ErrForwardRelayFmt, err.Error())
		}
	}
	// A port may sit on several forwards: the catalog keeps only names unique,
	// and which of them gets to listen is decided when a forward starts.
	for _, forward := range state.forwards {
		if editor.editing && forward.ID == editor.forwardID {
			continue
		}
		if strings.EqualFold(forward.Label, editor.value(forwardFieldLabel)) {
			return state.messages.ErrForwardNameTaken
		}
	}
	return ""
}

func (editor *forwardEditor) record(id uint32, host Host) forwardRecord {
	localPort, _ := editor.port(forwardFieldLocalPort)
	forward := forwardRecord{
		ID: id, Label: editor.value(forwardFieldLabel), Mode: editor.mode,
		HostLabel: host.Label, HostAddress: host.Address, HostUser: host.User, HostPort: host.Port,
		BindAddress: bindAddressForSave(editor.bindHost()),
		LocalPort:   localPort, Resume: editor.resume, AutoStart: editor.autoStart,
	}
	if editor.viaShell && modeUsesServerDestination(editor.mode) {
		forward.ViaShell, forward.Relay = true, editor.relayCommand()
	}
	if !editor.usesClientDestination() {
		forward.DestinationHost = editor.value(forwardFieldDestinationHost)
		forward.DestinationPort, _ = editor.port(forwardFieldDestinationPort)
	}
	return forward
}

// forwardHostChoices are the hosts a forward can travel through: the SSH hosts
// that start their own connection rather than a mosh one.
func (state *State) forwardHostChoices() []uint32 {
	editor := &state.forwardEditor
	choices := editor.hostChoices[:0]
	for _, host := range state.hosts {
		if host.isSSH() && !host.mosh() {
			choices = append(choices, host.ID)
		}
	}
	editor.hostChoices = choices
	return choices
}

// applyForwardHostChoice commits a host chosen in the page combobox.
func (state *State) applyForwardHostChoice(id uint32) {
	if _, ok := state.hostByID(id); !ok {
		return
	}
	state.forwardEditor.hostID = id
}

func (state *State) openForwardDialog() {
	if len(state.hosts) == 0 {
		state.showActionWarning(state.messages.ForwardDialogTitle, state.messages.ErrForwardHostRequired)
		return
	}
	hostID := state.selectedHostID
	if host, ok := state.hostByID(hostID); !ok || !host.isSSH() || host.mosh() {
		hostID = 0
		for _, host := range state.hosts {
			if host.isSSH() && !host.mosh() {
				hostID = host.ID
				break
			}
		}
	}
	if hostID == 0 {
		state.showActionWarning(state.messages.ForwardDialogTitle, state.messages.ErrForwardHostRequired)
		return
	}
	state.forwardEditor.reset(hostID)
	state.forwardDialogError = ""
	state.modal = modalNewForward
}

func (state *State) openEditForwardID(id uint32) {
	forward, ok := state.forwardByID(id)
	if !ok {
		return
	}
	state.selectedForwardID = id
	// The host the forward travels through is chosen explicitly: falling back to
	// the first SSH host would move the tunnel to another machine as soon as the
	// original one was removed, while the user only meant to change a port.
	hostID := uint32(0)
	if host, ok := state.forwardHost(forward); ok {
		hostID = host.ID
	}
	state.forwardEditor.load(forward, hostID)
	state.forwardDialogError = ""
	state.modal = modalNewForward
}

func (state *State) clickForwardDialog(id gfx.ID) {
	switch id {
	case hitForwardDialogClose, hitForwardDialogCancel:
		state.closeForwardDialog()
		state.status = state.messages.StatusEditCanceled
	case hitForwardDialogResume:
		state.forwardEditor.resume = !state.forwardEditor.resume
	case hitForwardDialogAutoStart:
		state.forwardEditor.autoStart = !state.forwardEditor.autoStart
	case hitForwardDialogViaShell:
		state.forwardEditor.viaShell = !state.forwardEditor.viaShell
	case hitForwardDialogSubmit:
		state.submitForwardDialog()
	}
}

func (state *State) submitForwardDialog() {
	if state.forwardDialogError = state.forwardEditor.validate(state); state.forwardDialogError != "" {
		return
	}
	host, _ := state.hostByID(state.forwardEditor.hostID)
	if state.forwardEditor.editing {
		for index := range state.forwards {
			if state.forwards[index].ID != state.forwardEditor.forwardID {
				continue
			}
			f := &state.forwards[index]
			tunnelID := f.sessionID
			f.stop()
			localPort, _ := state.forwardEditor.port(forwardFieldLocalPort)
			f.Label = state.forwardEditor.value(forwardFieldLabel)
			f.Mode = state.forwardEditor.mode
			f.HostLabel = host.Label
			f.HostAddress = host.Address
			f.HostUser = host.User
			f.HostPort = host.Port
			f.BindAddress = bindAddressForSave(state.forwardEditor.bindHost())
			f.LocalPort = localPort
			f.Resume = state.forwardEditor.resume
			f.AutoStart = state.forwardEditor.autoStart
			f.ViaShell, f.Relay = false, ""
			if state.forwardEditor.viaShell && modeUsesServerDestination(state.forwardEditor.mode) {
				f.ViaShell, f.Relay = true, state.forwardEditor.relayCommand()
			}
			f.lastError = ""
			if !state.forwardEditor.usesClientDestination() {
				f.DestinationHost = state.forwardEditor.value(forwardFieldDestinationHost)
				f.DestinationPort, _ = state.forwardEditor.port(forwardFieldDestinationPort)
			} else {
				f.DestinationHost = ""
				f.DestinationPort = 0
			}
			f.refresh(state.messages, state.forwardHostAvailable(*f))
			state.releaseForwardTunnel(tunnelID)
			state.status = state.messages.ForwardUpdated + f.Label
			state.closeForwardDialog()
			state.save()
			return
		}
		// The forward this dialog was opened on is gone: saving must not quietly
		// become a second copy of it under a new number.
		state.forwardDialogError = state.messages.ErrForwardEditMissing
		return
	}
	forward := state.forwardEditor.record(state.nextForwardID, host)
	forward.refresh(state.messages, true)
	state.forwards = append(state.forwards, forward)
	state.selectedForwardID = forward.ID
	state.nextForwardID++
	state.refreshForwardCount()
	state.status = state.messages.ForwardAdded + forward.Label
	state.closeForwardDialog()
	state.save()
}

func (state *State) closeForwardDialog() {
	state.modal = modalNone
	state.forwardDialogError = ""
	state.forwardEditor = forwardEditor{}
}

func (state *State) clickForwards(id gfx.ID) {
	if state.activeSection != sectionForwards || state.modal != modalNone {
		return
	}
	switch {
	case id == hitForwardAdd:
		state.openForwardDialog()
	case id.Scope() == hitForwardRowList:
		if row, ok := id.In(hitForwardRowList); ok && row >= 0 && row < len(state.forwards) {
			state.selectForward(state.forwards[row].ID)
		}
	case id.Scope() == hitForwardRowEditList:
		if forwardID, ok := id.In(hitForwardRowEditList); ok {
			state.openEditForwardID(uint32(forwardID))
		}
	case id.Scope() == hitForwardRowToggleList:
		if forwardID, ok := id.In(hitForwardRowToggleList); ok {
			state.toggleForwardByID(uint32(forwardID))
		}
	case id.Scope() == hitForwardRowRemoveList:
		if forwardID, ok := id.In(hitForwardRowRemoveList); ok {
			state.removeForwardByID(uint32(forwardID))
		}
	}
}

// selectForward makes a forward the selected one. The list calls it from the
// paint that delivers the click on its row.
func (state *State) selectForward(id uint32) {
	if _, ok := state.forwardByID(id); ok {
		state.selectedForwardID = id
		state.status = state.messages.ForwardSelected + state.forwardLabel(id)
	}
}

// removeForwardByID removes a forward from the list. A forward that is not the
// selected one leaves the selection where it is.
func (state *State) removeForwardByID(id uint32) {
	for index := range state.forwards {
		if state.forwards[index].ID != id {
			continue
		}
		removed := state.forwards[index]
		state.forwards[index].stop()
		state.forwards = slices.Delete(state.forwards, index, index+1)
		state.releaseForwardTunnel(removed.sessionID)
		if state.selectedForwardID == removed.ID {
			state.selectedForwardID = 0
			if len(state.forwards) > 0 {
				state.selectedForwardID = state.forwards[min(index, len(state.forwards)-1)].ID
			}
		}
		state.refreshForwardCount()
		state.status = state.messages.ForwardRemoved + removed.Label
		state.save()
		return
	}
}

// toggleForwardByID starts the forward if it is stopped, and stops it if it
// runs, waits or starts.
func (state *State) toggleForwardByID(id uint32) {
	forward, ok := state.forwardByIDPointer(id)
	if !ok {
		return
	}
	if forward.runner != nil || forward.pending || forward.starting {
		tunnelID := forward.sessionID
		forward.stop()
		forward.lastError = ""
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
		state.releaseForwardTunnel(tunnelID)
		state.status = state.messages.ForwardStoppedStatus + forward.Label
		return
	}
	host, ok := state.forwardHost(*forward)
	if !ok {
		forward.lastError = state.messages.ForwardHostUnavailable
		forward.refresh(state.messages, false)
		state.status = state.messages.ForwardHostUnavailable
		return
	}
	// A running forward already holds the port: say so before dialing anything.
	if message := state.forwardPortConflict(*forward); message != "" {
		state.failForward(forward, message)
		return
	}
	if session := state.connectedSessionForHost(host); session != nil {
		state.startForward(forward, session)
		return
	}
	if tunnel := state.connectedForwardTunnel(host); tunnel != nil {
		state.startForward(forward, &tunnel.session)
		return
	}
	// No tab carries a connection to this host: dial one for the forwards alone,
	// so a tunnel never costs a terminal window.
	tunnel := state.forwardTunnelForHost(host)
	fresh := tunnel == nil
	if fresh {
		tunnel = state.newForwardTunnel(host)
	}
	forward.pending = true
	forward.lastError = ""
	forward.sessionID = tunnel.id
	forward.refresh(state.messages, true)
	state.status = fmt.Sprintf(state.messages.ForwardConnectingFmt, host.Label)
	if fresh {
		state.dialForwardTunnel(tunnel)
	}
	state.save()
}

// forwardPortConflict reports the message of a running forward that already
// listens on this forward's port, or "" when the port is free. A port can be
// saved on several forwards; the listener decides which of them runs, so the
// second one to start is told which port it lost.
func (state *State) forwardPortConflict(forward forwardRecord) string {
	for index := range state.forwards {
		other := &state.forwards[index]
		if other.ID == forward.ID || other.runner == nil || !forwardListenersOverlap(*other, forward) {
			continue
		}
		if forward.Mode == portforward.ModeRemote {
			if other.Mode == portforward.ModeRemote &&
				other.HostAddress == forward.HostAddress && other.HostUser == forward.HostUser && other.HostPort == forward.HostPort {
				return state.messages.ErrForwardRemoteTaken
			}
			continue
		}
		if other.Mode != portforward.ModeRemote {
			return state.messages.ErrForwardLocalTaken
		}
	}
	return ""
}

// forwardListenersOverlap reports whether two listeners compete for one socket:
// they share a port, and either bind the same address or one binds every
// interface.
func forwardListenersOverlap(one, other forwardRecord) bool {
	if one.LocalPort != other.LocalPort {
		return false
	}
	oneHost, otherHost := one.listenHost(), other.listenHost()
	if oneHost == otherHost {
		return true
	}
	oneIP, otherIP := net.ParseIP(oneHost), net.ParseIP(otherHost)
	return oneIP == nil || otherIP == nil || oneIP.IsUnspecified() || otherIP.IsUnspecified()
}

// failForward leaves a forward that could not start stopped, with the reason on
// its row and the status line.
func (state *State) failForward(forward *forwardRecord, message string) {
	tunnelID := forward.sessionID
	forward.sessionID = 0
	forward.pending = false
	forward.starting = false
	forward.startID = 0
	forward.lastError = message
	forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	state.releaseForwardTunnel(tunnelID)
	state.status = fmt.Sprintf(state.messages.ForwardStartFailedFmt, message)
}

func (state *State) startForward(forward *forwardRecord, session *Session) {
	forward.pending = false
	forward.lastError = ""
	if message := state.forwardPortConflict(*forward); message != "" {
		state.failForward(forward, message)
		return
	}
	if forward.Mode == portforward.ModeRemote {
		state.startRemoteForward(forward, session)
		return
	}
	dial := session.conn.DialContext
	if forward.ViaShell {
		// The server refuses forwarded channels but gives a shell: each accepted
		// connection starts the relay program there, and that shell's input and
		// output are the stream.
		command := forward.relayCommand()
		dial = func(ctx context.Context, network, address string) (net.Conn, error) {
			return session.conn.DialShellContext(ctx, command, address)
		}
	}
	runner, err := portforward.Start(portforward.Config{
		ListenAddress:    forward.listenAddress(),
		Destination:      forward.destination(),
		Mode:             forward.Mode,
		AllowNonLoopback: true,
		Dial:             dial,
		Notify:           state.options.Invalidate,
	})
	if err != nil {
		state.failForward(forward, err.Error())
		return
	}
	forward.runner = runner
	forward.sessionID = session.ID
	forward.refresh(state.messages, true)
	state.status = fmt.Sprintf(state.messages.ForwardStartedFmt, forward.displayListener(state.messages), forward.displayDestination(state.messages))
}

func (state *State) startRemoteForward(forward *forwardRecord, session *Session) {
	state.nextForwardStartID++
	startID := state.nextForwardStartID
	forward.startID = startID
	forward.starting = true
	forward.sessionID = session.ID
	forward.refresh(state.messages, true)
	state.status = fmt.Sprintf(state.messages.ForwardStartingFmt, forward.listenAddress())

	forwardID := forward.ID
	listenAddress := forward.listenAddress()
	destination := forward.destination()
	listen := session.conn.Listen
	go func() {
		runner, err := portforward.Start(portforward.Config{
			ListenAddress:    listenAddress,
			Destination:      destination,
			Mode:             portforward.ModeRemote,
			AllowNonLoopback: true,
			Listen:           listen,
			Dial:             (&net.Dialer{}).DialContext,
			Notify:           state.options.Invalidate,
		})
		state.forwardStarts <- forwardStartResult{forwardID: forwardID, startID: startID, runner: runner, err: err}
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}()
}

// startPendingForwards starts the forwards waiting for this session, and those
// set to start with their host.
func (state *State) startPendingForwards(session *Session) {
	if !session.hasServer() {
		return // a mosh, Telnet, serial or local session has no SSH server to forward through
	}
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.pending && forward.sessionID == session.ID {
			state.startForward(forward, session)
			continue
		}
		// An idle forward that asked to follow its host starts with the first
		// tab that connects to it; one already running or waiting is left alone.
		if forward.AutoStart && forward.matchesHost(session.host) &&
			forward.runner == nil && !forward.pending && !forward.starting {
			state.startForward(forward, session)
		}
	}
}

// stopSessionForwards stops the forwards that run over a session. A connection
// that was lost (failed) while the automatic reconnect setting will dial the tab
// again leaves the forwards marked Resume waiting instead: they start again once
// the tab is connected, and stop if the retry is canceled or gives up.
func (state *State) stopSessionForwards(sessionID uint32, failed bool) {
	resume := false
	if failed {
		session, ok := state.sessionByIDPointer(sessionID)
		resume = ok && session.reconnectPending
	}
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.sessionID != sessionID {
			continue
		}
		if resume && forward.Resume {
			forward.hold()
		} else {
			forward.stop()
			if failed {
				forward.lastError = state.messages.ForwardConnectionClosed
			}
		}
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	}
}

// redialSessionForwards readies a session's forwards for a new connection of
// the same tab. Those waiting for it keep waiting, so pressing Enter during an
// automatic retry does not lose them; those running over the connection that is
// being replaced stop.
func (state *State) redialSessionForwards(sessionID uint32) {
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.sessionID != sessionID || forward.pending {
			continue
		}
		forward.stop()
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	}
}

// releaseWaitingForwards stops the forwards that wait for a session which will
// not connect again by itself: the tab is gone, or it is closed and no
// automatic retry is planned any more.
func (state *State) releaseWaitingForwards() {
	for index := range state.forwards {
		forward := &state.forwards[index]
		if !forward.pending || forward.sessionID == 0 {
			continue
		}
		// A tunnel owns the wait for its own connection, retries included; only
		// a tunnel that is already gone leaves the forward to stop here.
		if tunnel := state.forwardTunnelByID(forward.sessionID); tunnel != nil {
			if !tunnel.closed {
				continue
			}
		}
		session, ok := state.sessionByIDPointer(forward.sessionID)
		if ok && (session.reconnectPending || session.phase != sshconn.PhaseClosed && session.phase != sshconn.PhaseFailed) {
			continue
		}
		forward.stop()
		forward.lastError = state.messages.ForwardConnectionClosed
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	}
}

func (state *State) pumpForwards() {
	state.pumpForwardStarts()
	state.releaseWaitingForwards()
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.runner == nil {
			continue
		}
		status := forward.runner.Status()
		if status.Version == forward.runnerVersion {
			continue
		}
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
	}
}

func (state *State) pumpForwardStarts() {
	for {
		select {
		case result := <-state.forwardStarts:
			forward, ok := state.forwardByIDPointer(result.forwardID)
			if !ok || !forward.starting || forward.startID != result.startID {
				if result.runner != nil {
					go result.runner.Close()
				}
				continue
			}
			forward.starting = false
			forward.startID = 0
			if result.err != nil {
				state.failForward(forward, result.err.Error())
				continue
			}
			forward.runner = result.runner
			forward.refresh(state.messages, true)
			state.status = fmt.Sprintf(state.messages.ForwardStartedFmt, forward.displayListener(state.messages), forward.displayDestination(state.messages))
		default:
			return
		}
	}
}

// forwardBindAddressFrom is the device-local address this forward should use,
// or "" when this device has no custom address for it. A value that is not a
// literal IP is ignored, so a damaged local document leaves the listener on
// loopback instead of failing to start.
func forwardBindAddressFrom(forward forwardRecord, addresses map[string]string) string {
	if len(addresses) == 0 {
		return ""
	}
	address := strings.TrimSpace(addresses[forwardBindKey(forward)])
	if !validForwardBindAddress(address) || address == "" || address == defaultForwardBindAddress {
		return ""
	}
	return address
}

// restoreForwards replaces the list of forwards with the shared records alone,
// leaving every listener on loopback.
func (state *State) restoreForwards(saved []savedForward) {
	state.restoreForwardsWithBindAddresses(saved, nil)
}

// restoreForwardsWithBindAddresses replaces the list of forwards with a saved
// one, applying the addresses this device keeps. A forward that is in both
// keeps the number it was saved with and whatever it is running, so a list that
// changes under a forward that is still wanted does not stop it; one that is
// gone is stopped. A forward whose saved configuration differs from the one that
// was running under the same number is stopped and restored fresh: its tunnel
// listens on the old settings.
func (state *State) restoreForwardsWithBindAddresses(saved []savedForward, addresses map[string]string) {
	previous := state.forwards
	state.forwards = nil
	wanted := make([]uint32, len(saved))
	for index, entry := range saved {
		wanted[index] = entry.ID
	}
	numbers := recordNumbers(wanted)
	next := uint32(1)
	for index, entry := range saved {
		mode, modeOK := restoreForwardMode(entry.Mode)
		if !modeOK || entry.Label == "" || entry.HostAddress == "" || entry.HostPort == 0 || entry.LocalPort == 0 {
			continue
		}
		if (mode == portforward.ModeLocal || mode == portforward.ModeRemote) && (entry.DestinationHost == "" || entry.DestinationPort == 0) {
			continue
		}
		if numbers[index] == 0 {
			numbers[index] = freeRecordNumber(numbers, &next)
		}
		forward := forwardRecord{
			ID: numbers[index], Label: entry.Label, Mode: mode,
			HostLabel: entry.HostLabel, HostAddress: entry.HostAddress, HostUser: entry.HostUser, HostPort: entry.HostPort,
			LocalPort: entry.LocalPort, Resume: entry.Resume, AutoStart: entry.AutoStart, ViaShell: entry.ViaShell, Relay: entry.Relay,
		}
		if !modeUsesServerDestination(mode) {
			forward.ViaShell, forward.Relay = false, ""
		}
		if mode == portforward.ModeLocal || mode == portforward.ModeRemote {
			forward.DestinationHost = entry.DestinationHost
			forward.DestinationPort = entry.DestinationPort
		}
		forward.BindAddress = forwardBindAddressFrom(forward, addresses)
		// A number tells which record this is; an older save without one falls
		// back to recognising the forward by what it listens on and where it goes.
		match := -1
		if entry.ID != 0 {
			match = slices.IndexFunc(previous, func(kept forwardRecord) bool { return kept.ID == forward.ID })
		}
		if match < 0 {
			match = slices.IndexFunc(previous, forward.sameAs)
		}
		if match >= 0 {
			kept := previous[match]
			previous = slices.Delete(previous, match, match+1)
			if kept.sameAs(forward) {
				kept.ID = forward.ID
				kept.Label, kept.HostLabel, kept.Resume, kept.AutoStart = forward.Label, forward.HostLabel, forward.Resume, forward.AutoStart
				forward = kept
			} else {
				kept.stop()
			}
		}
		forward.refresh(state.messages, state.forwardHostAvailable(forward))
		state.forwards = append(state.forwards, forward)
	}
	state.nextForwardID = max(state.nextForwardID, highestRecordNumber(state.forwards))
	for index := range previous {
		previous[index].stop()
	}
	if _, ok := state.forwardByID(state.selectedForwardID); !ok {
		state.selectedForwardID = 0
		if len(state.forwards) > 0 {
			state.selectedForwardID = state.forwards[0].ID
		}
	}
	state.refreshForwardCount()
}

// highestRecordNumber is one past the largest number these forwards use: the
// number the next one takes.
func highestRecordNumber(forwards []forwardRecord) uint32 {
	var highest uint32
	for _, forward := range forwards {
		highest = max(highest, forward.ID)
	}
	return highest + 1
}

// sameAs says whether two records are the same forward: what is listened on and
// where it goes, whatever it is called.
func (forward forwardRecord) sameAs(other forwardRecord) bool {
	return forward.Mode == other.Mode && forward.HostAddress == other.HostAddress && forward.HostUser == other.HostUser &&
		forward.HostPort == other.HostPort && forward.listenHost() == other.listenHost() && forward.LocalPort == other.LocalPort &&
		forward.DestinationHost == other.DestinationHost && forward.DestinationPort == other.DestinationPort &&
		forward.ViaShell == other.ViaShell && forward.relayCommand() == other.relayCommand()
}

func restoreForwardMode(saved string) (portforward.Mode, bool) {
	switch saved {
	case "", "local":
		return portforward.ModeLocal, true
	case "remote":
		return portforward.ModeRemote, true
	case "socks5":
		return portforward.ModeSOCKS5, true
	case "http-connect":
		return portforward.ModeHTTPConnect, true
	default:
		return 0, false
	}
}

func (state *State) refreshForwards() {
	for index := range state.forwards {
		state.forwards[index].refresh(state.messages, state.forwardHostAvailable(state.forwards[index]))
	}
	state.refreshForwardCount()
}

func (state *State) refreshForwardCount() {
	state.forwardCountText = fmt.Sprintf(state.messages.ForwardCountFmt, len(state.forwards))
}

func (state *State) forwardHost(forward forwardRecord) (Host, bool) {
	for _, host := range state.hosts {
		if forward.matchesHost(host) {
			return host, true
		}
	}
	return Host{}, false
}

func (state *State) forwardHostAvailable(forward forwardRecord) bool {
	_, ok := state.forwardHost(forward)
	return ok
}

func (state *State) connectedSessionForHost(host Host) *Session {
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.conn != nil && session.phase == sshconn.PhaseConnected && session.host.ID == host.ID {
			return session
		}
	}
	return nil
}

func (state *State) forwardByID(id uint32) (forwardRecord, bool) {
	for _, forward := range state.forwards {
		if forward.ID == id {
			return forward, true
		}
	}
	return forwardRecord{}, false
}

func (state *State) forwardByIDPointer(id uint32) (*forwardRecord, bool) {
	for index := range state.forwards {
		if state.forwards[index].ID == id {
			return &state.forwards[index], true
		}
	}
	return nil, false
}

func (state *State) forwardLabel(id uint32) string {
	forward, _ := state.forwardByID(id)
	return forward.Label
}

func forwardDialogField(id gfx.ID) forwardField {
	switch id {
	case hitForwardDialogBindAddress:
		return forwardFieldBindAddress
	case hitForwardDialogLocalPort:
		return forwardFieldLocalPort
	case hitForwardDialogDestination:
		return forwardFieldDestinationHost
	case hitForwardDialogDestinationPort:
		return forwardFieldDestinationPort
	case hitForwardDialogRelay:
		return forwardFieldRelay
	default:
		return forwardFieldLabel
	}
}
