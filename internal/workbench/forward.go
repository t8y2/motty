package workbench

import (
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/i18n"
	"motty/internal/portforward"
	"motty/internal/sshconn"
)

type forwardRecord struct {
	ID              uint32
	Label           string
	Mode            portforward.Mode
	HostLabel       string
	HostAddress     string
	HostUser        string
	HostPort        uint16
	LocalPort       uint16
	DestinationHost string
	DestinationPort uint16
	// Resume makes the forward wait through an automatic reconnect of its tab
	// and start again once it is connected, instead of stopping.
	Resume bool

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

func (forward *forwardRecord) listenAddress() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(forward.LocalPort)))
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
			forward.detail = forward.lastError
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
	localPort       text.Editor
	destinationHost text.Editor
	destinationPort text.Editor
	hostID          uint32
	// hostSelect is the searchable host picker. Its query stays here so typing
	// filters the list without opening a second surface.
	hostSelect components.SingleSelectState
	hostPopup  gfx.Rect
	mode       portforward.Mode
	resume     bool
	editing    bool
	forwardID  uint32
}

type forwardField uint8

const (
	forwardFieldLabel forwardField = iota
	forwardFieldLocalPort
	forwardFieldDestinationHost
	forwardFieldDestinationPort
)

func (editor *forwardEditor) field(field forwardField) *text.Editor {
	switch field {
	case forwardFieldLabel:
		return &editor.label
	case forwardFieldLocalPort:
		return &editor.localPort
	case forwardFieldDestinationHost:
		return &editor.destinationHost
	default:
		return &editor.destinationPort
	}
}

func (editor *forwardEditor) value(field forwardField) string {
	return strings.TrimSpace(editor.field(field).String())
}

func (editor *forwardEditor) reset(hostID uint32) {
	*editor = forwardEditor{hostID: hostID}
	digits := func(value rune) bool { return value >= '0' && value <= '9' }
	editor.localPort.Restrict(digits, 5)
	editor.destinationPort.Restrict(digits, 5)
	editor.localPort.Insert("8080")
	editor.destinationPort.Insert("80")
}

func (editor *forwardEditor) load(forward forwardRecord, hostID uint32) {
	*editor = forwardEditor{
		hostID:    hostID,
		mode:      forward.Mode,
		resume:    forward.Resume,
		editing:   true,
		forwardID: forward.ID,
	}
	digits := func(value rune) bool { return value >= '0' && value <= '9' }
	editor.localPort.Restrict(digits, 5)
	editor.destinationPort.Restrict(digits, 5)
	editor.label.Insert(forward.Label)
	editor.localPort.Insert(strconv.Itoa(int(forward.LocalPort)))
	if forward.DestinationHost != "" {
		editor.destinationHost.Insert(forward.DestinationHost)
	}
	if forward.DestinationPort != 0 {
		editor.destinationPort.Insert(strconv.Itoa(int(forward.DestinationPort)))
	}
}

func (editor *forwardEditor) usesClientDestination() bool {
	return editor.mode == portforward.ModeSOCKS5 || editor.mode == portforward.ModeHTTPConnect
}

func (editor *forwardEditor) port(field forwardField) (uint16, bool) {
	value, err := strconv.Atoi(editor.value(field))
	if err != nil || value < 1 || value > 65535 {
		return 0, false
	}
	return uint16(value), true
}

func (editor *forwardEditor) validate(state *State) string {
	if editor.value(forwardFieldLabel) == "" {
		return state.messages.ErrForwardNameRequired
	}
	host, ok := state.hostByID(editor.hostID)
	if !ok {
		if editor.editing {
			return state.messages.ErrForwardHostMissing
		}
		return state.messages.ErrForwardHostRequired
	}
	localPort, localOK := editor.port(forwardFieldLocalPort)
	if !localOK {
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
	for _, forward := range state.forwards {
		if editor.editing && forward.ID == editor.forwardID {
			continue
		}
		if strings.EqualFold(forward.Label, editor.value(forwardFieldLabel)) {
			return state.messages.ErrForwardNameTaken
		}
		if editor.mode == portforward.ModeRemote {
			if forward.Mode == portforward.ModeRemote && forward.LocalPort == localPort && forward.matchesHost(host) {
				return state.messages.ErrForwardRemoteTaken
			}
			continue
		}
		if forward.Mode != portforward.ModeRemote && forward.LocalPort == localPort {
			return state.messages.ErrForwardLocalTaken
		}
	}
	return ""
}

func (editor *forwardEditor) record(id uint32, host Host) forwardRecord {
	localPort, _ := editor.port(forwardFieldLocalPort)
	forward := forwardRecord{
		ID: id, Label: editor.value(forwardFieldLabel), Mode: editor.mode,
		HostLabel: host.Label, HostAddress: host.Address, HostUser: host.User, HostPort: host.Port,
		LocalPort: localPort, Resume: editor.resume,
	}
	if !editor.usesClientDestination() {
		forward.DestinationHost = editor.value(forwardFieldDestinationHost)
		forward.DestinationPort, _ = editor.port(forwardFieldDestinationPort)
	}
	return forward
}

// syncForwardHostSelect rebuilds the host picker from the catalog and shows the
// host the editor currently holds. It runs when the dialog opens, not while
// painting, so an unchanged dialog allocates nothing for its list.
func (state *State) syncForwardHostSelect() {
	editor := &state.forwardEditor
	options := make([]components.SingleSelectOption, 0, len(state.hosts))
	for _, host := range state.hosts {
		options = append(options, hostSelectOption(host, !host.isSSH()))
	}
	editor.hostSelect.SetOptions(options)
	editor.hostSelect.SetValue(hostSelectValue(editor.hostID))
}

func (state *State) openForwardDialog() {
	if len(state.hosts) == 0 {
		state.showActionWarning(state.messages.ForwardDialogTitle, state.messages.ErrForwardHostRequired)
		return
	}
	hostID := state.selectedHostID
	if host, ok := state.hostByID(hostID); !ok || !host.isSSH() {
		hostID = 0
		for _, host := range state.hosts {
			if host.isSSH() {
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
	state.syncForwardHostSelect()
	state.forwardDialogError = ""
	state.modal = modalNewForward
}

func (state *State) openEditForward() {
	if state.selectedForwardID == 0 {
		return
	}
	state.openEditForwardID(state.selectedForwardID)
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
	state.syncForwardHostSelect()
	state.forwardDialogError = ""
	state.modal = modalNewForward
}

func (state *State) clickForwardDialog(id gfx.ID) {
	editor := &state.forwardEditor
	if editor.hostSelect.Open {
		switch {
		case id == hitForwardDialogClose || id == hitForwardDialogCancel:
			state.closeForwardDialog()
			state.status = state.messages.StatusEditCanceled
		case id == hitForwardDialogSubmit:
			editor.hostSelect.Open = false
			state.submitForwardDialog()
		case id == hitForwardDialogHost || id == hitForwardHostTrigger:
			editor.hostSelect.Open = true
		case id == hitForwardHostDismiss:
			editor.hostSelect.Open = false
		case id == hitForwardHostScroll:
			// The scrollbar drag belongs to the pointer handler.
		case id.Scope() == hitForwardHostItemList:
			if index, ok := id.In(hitForwardHostItemList); ok && editor.hostSelect.Choose(index) {
				if hostID, ok := hostSelectID(editor.hostSelect.Value()); ok {
					editor.hostID = hostID
				}
			}
		default:
			editor.hostSelect.Open = false
		}
		return
	}
	switch id {
	case hitForwardDialogClose, hitForwardDialogCancel:
		state.closeForwardDialog()
		state.status = state.messages.StatusEditCanceled
	case hitForwardDialogHost, hitForwardHostTrigger:
		editor.hostSelect.Open = true
	case hitForwardModeLocal:
		state.forwardEditor.mode = portforward.ModeLocal
		state.forwardDialogError = ""
	case hitForwardModeRemote:
		state.forwardEditor.mode = portforward.ModeRemote
		state.forwardDialogError = ""
	case hitForwardModeSOCKS5:
		state.forwardEditor.mode = portforward.ModeSOCKS5
		state.forwardDialogError = ""
	case hitForwardModeHTTPConnect:
		state.forwardEditor.mode = portforward.ModeHTTPConnect
		state.forwardDialogError = ""
	case hitForwardDialogResume:
		state.forwardEditor.resume = !state.forwardEditor.resume
	case hitForwardDialogSubmit:
		state.submitForwardDialog()
	}
}

// forwardDialogKey routes arrows, Enter, Escape and Tab to the open host
// picker, so Enter chooses a host instead of submitting the form.
func (state *State) forwardDialogKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalNewForward {
		return gfx.EventResult{}, false
	}
	editor := &state.forwardEditor
	var result gfx.EventResult
	switch {
	case event.Target == hitForwardDialogHost || event.Target == hitForwardHostTrigger:
		result = editor.hostSelect.Key(event)
	case event.Key == gfx.KeyEscape && editor.hostSelect.Open:
		result = editor.hostSelect.Key(event)
	case event.Key == gfx.KeyTab && editor.hostSelect.Open:
		result = editor.hostSelect.Key(event)
	default:
		return gfx.EventResult{}, false
	}
	if result.Handled || result.Repaint {
		// A host chosen with Enter changes the picker's value; a picker that is
		// still open is a search in progress, not a choice.
		if !editor.hostSelect.Open {
			if hostID, ok := hostSelectID(editor.hostSelect.Value()); ok {
				editor.hostID = hostID
			}
		}
	}
	return result, result.Handled || result.Repaint
}

func (state *State) forwardPickerPointer(event gfx.PointerEvent) (gfx.EventResult, bool) {
	editor := &state.forwardEditor
	if !editor.hostSelect.Open {
		return gfx.EventResult{}, false
	}
	inside := event.Target == hitForwardDialogHost || event.Target == hitForwardHostTrigger ||
		event.Target == hitForwardHostScroll || event.Target.Scope() == hitForwardHostItemList
	if event.Kind == gfx.PointerDown && event.Button == gfx.PointerButtonPrimary && !inside {
		editor.hostSelect.Open = false
		return gfx.EventResult{Handled: true, Repaint: true}, true
	}
	if result := editor.hostSelect.Pointer(event); result.Handled {
		return result, true
	}
	return gfx.EventResult{}, false
}

func (state *State) scrollForwardDialog(event gfx.ScrollEvent) gfx.EventResult {
	editor := &state.forwardEditor
	if !editor.hostSelect.Open {
		return gfx.EventResult{}
	}
	return gfx.EventResult{Handled: true, Repaint: pointInRect(event.X, event.Y, editor.hostPopup) && editor.hostSelect.Wheel(event)}
}

func (state *State) submitForwardDialog() {
	state.forwardEditor.hostSelect.Open = false
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
			f.stop()
			localPort, _ := state.forwardEditor.port(forwardFieldLocalPort)
			f.Label = state.forwardEditor.value(forwardFieldLabel)
			f.Mode = state.forwardEditor.mode
			f.HostLabel = host.Label
			f.HostAddress = host.Address
			f.HostUser = host.User
			f.HostPort = host.Port
			f.LocalPort = localPort
			f.Resume = state.forwardEditor.resume
			f.lastError = ""
			if !state.forwardEditor.usesClientDestination() {
				f.DestinationHost = state.forwardEditor.value(forwardFieldDestinationHost)
				f.DestinationPort, _ = state.forwardEditor.port(forwardFieldDestinationPort)
			} else {
				f.DestinationHost = ""
				f.DestinationPort = 0
			}
			f.refresh(state.messages, state.forwardHostAvailable(*f))
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
	case id == hitForwardEdit:
		state.openEditForward()
	case id == hitForwardRemove:
		state.removeSelectedForward()
	case id == hitForwardStartStop:
		state.toggleSelectedForward()
	}
}

// selectForward makes a forward the selected one. The table calls it from the
// paint that delivers the click on its row.
func (state *State) selectForward(id uint32) {
	if _, ok := state.forwardByID(id); ok {
		state.selectedForwardID = id
		state.status = state.messages.ForwardSelected + state.forwardLabel(id)
	}
}

func (state *State) removeSelectedForward() {
	for index := range state.forwards {
		if state.forwards[index].ID != state.selectedForwardID {
			continue
		}
		removed := state.forwards[index]
		state.forwards[index].stop()
		state.forwards = slices.Delete(state.forwards, index, index+1)
		state.selectedForwardID = 0
		if len(state.forwards) > 0 {
			state.selectedForwardID = state.forwards[min(index, len(state.forwards)-1)].ID
		}
		state.refreshForwardCount()
		state.status = state.messages.ForwardRemoved + removed.Label
		state.save()
		return
	}
}

func (state *State) toggleSelectedForward() {
	forward, ok := state.forwardByIDPointer(state.selectedForwardID)
	if !ok {
		return
	}
	if forward.runner != nil || forward.pending || forward.starting {
		forward.stop()
		forward.lastError = ""
		forward.refresh(state.messages, state.forwardHostAvailable(*forward))
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
	if session := state.connectedSessionForHost(host); session != nil {
		state.startForward(forward, session)
		return
	}
	forward.pending = true
	forward.lastError = ""
	forward.refresh(state.messages, true)
	session := state.openHostSession(host)
	forward.sessionID = session.ID
	state.activeSection = sectionForwards
	state.status = fmt.Sprintf(state.messages.ForwardConnectingFmt, host.Label)
	state.save()
}

func (state *State) startForward(forward *forwardRecord, session *Session) {
	forward.pending = false
	forward.lastError = ""
	if forward.Mode == portforward.ModeRemote {
		state.startRemoteForward(forward, session)
		return
	}
	runner, err := portforward.Start(portforward.Config{
		ListenAddress: forward.listenAddress(),
		Destination:   forward.destination(),
		Mode:          forward.Mode,
		Dial:          session.conn.DialContext,
		Notify:        state.options.Invalidate,
	})
	if err != nil {
		forward.sessionID = 0
		forward.lastError = err.Error()
		forward.refresh(state.messages, true)
		state.status = fmt.Sprintf(state.messages.ForwardStartFailedFmt, err)
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
			ListenAddress: listenAddress,
			Destination:   destination,
			Mode:          portforward.ModeRemote,
			Listen:        listen,
			Dial:          (&net.Dialer{}).DialContext,
			Notify:        state.options.Invalidate,
		})
		state.forwardStarts <- forwardStartResult{forwardID: forwardID, startID: startID, runner: runner, err: err}
		if state.options.Invalidate != nil {
			state.options.Invalidate()
		}
	}()
}

func (state *State) startPendingForwards(session *Session) {
	for index := range state.forwards {
		forward := &state.forwards[index]
		if forward.pending && forward.sessionID == session.ID {
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
				forward.sessionID = 0
				forward.lastError = result.err.Error()
				forward.refresh(state.messages, true)
				state.status = fmt.Sprintf(state.messages.ForwardStartFailedFmt, result.err)
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

// restoreForwards replaces the list of forwards with a saved one. A forward that
// is in both keeps the number it was saved with and whatever it is running, so a
// list that changes under a forward that is still wanted does not stop it; one
// that is gone is stopped. A forward whose saved configuration differs from the
// one that was running under the same number is stopped and restored fresh: its
// tunnel listens on the old settings.
func (state *State) restoreForwards(saved []savedForward) {
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
			LocalPort: entry.LocalPort, Resume: entry.Resume,
		}
		if mode == portforward.ModeLocal || mode == portforward.ModeRemote {
			forward.DestinationHost = entry.DestinationHost
			forward.DestinationPort = entry.DestinationPort
		}
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
				kept.Label, kept.HostLabel, kept.Resume = forward.Label, forward.HostLabel, forward.Resume
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
		forward.HostPort == other.HostPort && forward.LocalPort == other.LocalPort &&
		forward.DestinationHost == other.DestinationHost && forward.DestinationPort == other.DestinationPort
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
	case hitForwardDialogLocalPort:
		return forwardFieldLocalPort
	case hitForwardDialogDestination:
		return forwardFieldDestinationHost
	case hitForwardDialogDestinationPort:
		return forwardFieldDestinationPort
	default:
		return forwardFieldLabel
	}
}
