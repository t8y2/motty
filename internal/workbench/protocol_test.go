package workbench

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/serial"
	"motty/internal/sessionimport"
	"motty/internal/sshconn"
)

// streamWorld replaces the opener of Telnet and serial streams with one that
// hands out fake shells (or an error), and records what was asked for.
type streamWorld struct {
	mu               sync.Mutex
	recordedRequests []streamRequest
	shells           []*fakeShell
	err              error
	hold             chan struct{} // when set, opening waits for it to close
}

func (world *streamWorld) install(state *State) {
	state.openStream = func(ctx context.Context, request streamRequest) (Shell, error) {
		world.mu.Lock()
		world.recordedRequests = append(world.recordedRequests, request)
		world.mu.Unlock()
		if world.hold != nil {
			select {
			case <-world.hold:
			case <-ctx.Done():
				return nil, ctx.Err()
			}
		}
		if world.err != nil {
			return nil, world.err
		}
		shell := &fakeShell{done: make(chan struct{})}
		world.mu.Lock()
		world.shells = append(world.shells, shell)
		world.mu.Unlock()
		return shell, nil
	}
}

func (world *streamWorld) requests() []streamRequest {
	world.mu.Lock()
	defer world.mu.Unlock()
	return append([]streamRequest(nil), world.recordedRequests...)
}

func addStreamHost(state *State, host Host) Host {
	host.ID = state.nextHostID
	state.nextHostID++
	host.normalizeEndpoint()
	host.finish()
	state.hosts = append(state.hosts, host)
	state.selectedHostID = host.ID
	state.hostDataChanged()
	return host
}

func waitUntil(t *testing.T, state *State, what string, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if done() {
			return
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

func TestTelnetHostOpensAStreamAndNotAnSSHConnection(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)
	host := addStreamHost(state, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet})
	if host.Port != 23 || host.target != "telnet://10.0.0.2" {
		t.Fatalf("host = %+v", host)
	}

	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the shell", func() bool { return session.term.shell != nil })
	if len(world.conns) != 0 {
		t.Fatalf("a Telnet host dialed %d SSH connections", len(world.conns))
	}
	requests := streams.requests()
	if len(requests) != 1 || requests[0].Host.Address != "10.0.0.2" || requests[0].Host.Port != 23 {
		t.Fatalf("stream requests = %+v", requests)
	}
	if session.Address != "10.0.0.2:23" {
		t.Fatalf("address = %q", session.Address)
	}
	if got := screenText(state, 0); !strings.Contains(got, "Telnet is not encrypted") {
		t.Fatalf("no warning about plain text; screen %q", got)
	}
	if size := streams.shells[0].resizes; len(size) == 0 {
		t.Fatal("the stream was not told the terminal size")
	}

	streams.shells[0].say("login: ")
	waitUntil(t, state, "the prompt", func() bool { return strings.Contains(screenText(state, 0), "login:") })
	session.term.send([]byte("admin\r"))
	if got := streams.shells[0].typed(); got != "admin\r" {
		t.Fatalf("typed %q", got)
	}
}

func TestOpeningStreamHostAfterTransportEditStartsANewSession(t *testing.T) {
	tests := []struct {
		name   string
		host   Host
		change func(*State, *Host)
	}{
		{name: "serial line", host: Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial, Line: serial.Default},
			change: func(_ *State, host *Host) { host.Line.Baud = 9600 }},
		{name: "telnet proxy", host: Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet},
			change: func(state *State, _ *Host) {
				state.proxy = outboundProxy{kind: sshconn.ProxySOCKS5, address: "localhost:1080"}
			}},
	}
	for _, test := range tests {
		t.Run(test.name, func(t *testing.T) {
			_, options := newWorld(t)
			state := newTestState(t, options)
			streams := &streamWorld{}
			streams.install(state)
			host := addStreamHost(state, test.host)
			openHost(state)
			first := state.activeSessionID
			waitUntil(t, state, "first stream", func() bool { return len(streams.requests()) == 1 })
			test.change(state, &host)
			opened := state.openHostSession(host)
			if opened == nil || opened.ID == first {
				t.Fatal("edited transport reused the old session")
			}
			waitUntil(t, state, "new stream", func() bool { return len(streams.requests()) == 2 })
		})
	}
}

func TestStreamHostsOfferNoServerOnlyTools(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)
	addStreamHost(state, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial, Line: serial.Default})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the shell", func() bool { return session.term.shell != nil })
	session.term.live = true
	if _, _, offered := state.remoteStatsToggle(session); offered {
		t.Fatal("the resource monitor is offered for a serial port")
	}
	state.Click(hitSFTPOpen)
	if session.files != nil {
		t.Fatal("an SFTP panel opened for a serial port")
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitSFTPOpen); !ok || !hit.Disabled {
		t.Fatalf("Files should be shown disabled: %+v, %v", hit, ok)
	}
	if !strings.Contains(screenText(state, 0), "Opened /dev/ttyUSB0 at 115200 8N1.") {
		t.Fatalf("screen %q", screenText(state, 0))
	}
	if _, err := session.conn.RunCommand(context.Background(), "ls", 10); !errors.Is(err, errNoServer) {
		t.Fatalf("RunCommand = %v", err)
	}
}

func TestAStreamThatFailsToOpenShowsWhyAndRetries(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{err: errors.New("dial tcp 10.0.0.2:23: connection refused")}
	streams.install(state)
	addStreamHost(state, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the failure", func() bool { return session.failed() })
	if !strings.Contains(session.failure, "connection refused") {
		t.Fatalf("failure = %q", session.failure)
	}
	if !state.canReconnect(session) {
		t.Fatal("a failed tab cannot be retried")
	}
	streams.err = nil
	state.Click(hitConnRetry)
	waitUntil(t, state, "the retry", func() bool { return session.term != nil && session.term.shell != nil })
	if requests := streams.requests(); len(requests) != 2 {
		t.Fatalf("requests = %d", len(requests))
	}
}

func TestCancellingAStreamStillDialingClosesWhatItGets(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{hold: make(chan struct{})}
	streams.install(state)
	addStreamHost(state, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	session.conn.Cancel()
	close(streams.hold)
	waitUntil(t, state, "the dial to end", func() bool { return session.conn.Status().Phase == sshconn.PhaseClosed })
	if session.term != nil && session.term.shell != nil {
		t.Fatal("a cancelled dial produced a shell")
	}
}

func TestOutboundProxyCarriesTelnetToo(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)
	state.proxy = outboundProxy{kind: 1, address: "127.0.0.1:1080"}
	addStreamHost(state, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet})
	openHost(state)
	waitUntil(t, state, "the request", func() bool { return len(streams.requests()) == 1 })
	if proxy := streams.requests()[0].Proxy; proxy == nil || proxy.Address != "127.0.0.1:1080" {
		t.Fatalf("proxy = %+v", proxy)
	}
}

func TestHostDialogAddsATelnetHostAndTheSameAddressForSSHIsAnotherHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolTelnet)
	if state.editor.value(fieldPort) != "23" {
		t.Fatalf("port = %q", state.editor.value(fieldPort))
	}
	typeInto(state, hitDialogLabel, "Prod telnet")
	typeInto(state, hitDialogAddress, "prod.example.com")
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("dialog stayed open: %q", state.dialogError)
	}
	added := state.hosts[len(state.hosts)-1]
	if added.Protocol != protocolTelnet || added.Port != 23 || added.User != "" || added.CatalogID == catalogIDFor(Host{Address: "prod.example.com", Port: 23}) {
		t.Fatalf("added = %+v", added)
	}
	// The same destination under the same name again is a duplicate.
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolTelnet)
	typeInto(state, hitDialogLabel, "Prod telnet")
	typeInto(state, hitDialogAddress, "prod.example.com")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrAddressTaken {
		t.Fatalf("duplicate Telnet host accepted: %q", state.dialogError)
	}
}

func TestHostDialogAddsASerialHostWithItsLineAndFlow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolSerial)
	if state.editor.value(fieldPort) != "115200 8N1" {
		t.Fatalf("line = %q", state.editor.value(fieldPort))
	}
	state.editor.address = text.Editor{} // whatever port this machine offered
	typeInto(state, hitDialogLabel, "Router console")
	typeInto(state, hitDialogAddress, "/dev/ttyUSB0")
	state.editor.port.SetSelection(text.Range{Start: 0, End: len("115200 8N1")})
	typeInto(state, hitDialogPort, "9600 7E2")
	clickHostChoice(t, state, state.messages.Tools.FlowHardware)
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("dialog stayed open: %q", state.dialogError)
	}
	added := state.hosts[len(state.hosts)-1]
	want := serial.Settings{Baud: 9600, DataBits: 7, StopBits: 2, Parity: serial.ParityEven, Flow: serial.FlowHardware}
	if added.Protocol != protocolSerial || added.Address != "/dev/ttyUSB0" || added.Line != want || added.Port != 0 || added.User != "" {
		t.Fatalf("added = %+v", added)
	}
	if added.target != "/dev/ttyUSB0 · 9600 7E2 RTS/CTS" {
		t.Fatalf("target = %q", added.target)
	}

	// A line that is not one is refused with the way to write it.
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolSerial)
	typeInto(state, hitDialogLabel, "Bad")
	state.editor.address = text.Editor{}
	typeInto(state, hitDialogAddress, "/dev/ttyUSB1")
	state.editor.port.SetSelection(text.Range{Start: 0, End: len("115200 8N1")})
	typeInto(state, hitDialogPort, "fast")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.Tools.ErrSerialLine {
		t.Fatalf("a bad line was accepted: %q", state.dialogError)
	}
}

// The serial line offers the rates devices commonly use as a select beside the
// field. A choice writes the rate and keeps the frame; a rate the line already
// holds that is no preset is still what the select shows, so it is not lost.
func TestSerialLineOffersCommonBaudRates(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolSerial)
	state.editor.port = text.Editor{}
	typeInto(state, hitDialogPort, "9600 7E2")
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.Tools.FieldBaudRates)
	canvas, _ := paintedTexts(state)
	offered := false
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleOption && hit.Label == "115200" {
			offered = true
		}
	}
	if !offered {
		t.Fatal("the open rate list does not offer 115200")
	}
	clickPageLabel(t, state, gfx.RoleOption, "115200")
	if got := state.editor.value(fieldPort); got != "115200 7E2" {
		t.Fatalf("the chosen rate left the line at %q, want the frame kept", got)
	}

	state.editor.port = text.Editor{}
	typeInto(state, hitDialogPort, "250000 8N1")
	canvas, _ = paintedTexts(state)
	shown := ""
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleCombobox && hit.Label == state.messages.Tools.FieldBaudRates {
			shown = hit.Value
		}
	}
	if shown != "250000" {
		t.Fatalf("the select shows %q for a rate that is no preset, want 250000", shown)
	}
}

func TestSwitchingProtocolKeepsWhatTheUserTypedAndChangesWhatTheyDidNot(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolTelnet)
	clickHostChoice(t, state, state.messages.Tools.ProtocolSSH)
	if state.editor.value(fieldPort) != "22" {
		t.Fatalf("port went to %q", state.editor.value(fieldPort))
	}
	state.editor.port.SetSelection(text.Range{Start: 0, End: 2})
	typeInto(state, hitDialogPort, "2222")
	clickHostChoice(t, state, state.messages.Tools.ProtocolTelnet)
	if state.editor.value(fieldPort) != "2222" {
		t.Fatalf("a port the user typed was replaced by %q", state.editor.value(fieldPort))
	}
}

func TestHostDialogPaintsEachProtocolWithItsOwnFields(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	for _, test := range []struct {
		protocol string
		want     []gfx.ID
		wantFlow string
		absent   []gfx.ID
	}{
		{state.messages.Tools.ProtocolSSH, []gfx.ID{hitDialogUser, hitDialogPort}, "", nil},
		{state.messages.Tools.ProtocolTelnet, []gfx.ID{hitDialogPort}, "", []gfx.ID{hitDialogUser}},
		{state.messages.Tools.ProtocolSerial, []gfx.ID{hitDialogPort}, state.messages.Tools.FlowNone, []gfx.ID{hitDialogUser}},
	} {
		clickHostChoice(t, state, test.protocol)
		var canvas gfx.Canvas
		state.Paint(&canvas, hostChoicePaint)
		serial := test.protocol == state.messages.Tools.ProtocolSerial
		if painted := hasLabel(&canvas, state.messages.Tools.PortsPicker, gfx.RoleCombobox); painted != serial {
			t.Errorf("protocol %q: ports picker painted=%v, want %v", test.protocol, painted, serial)
		}
		for _, id := range test.want {
			if _, ok := hitByID(&canvas, id); !ok {
				t.Errorf("protocol %q: hit %d is not painted", test.protocol, id)
			}
		}
		for _, id := range test.absent {
			if _, ok := hitByID(&canvas, id); ok {
				t.Errorf("protocol %q: hit %d is painted", test.protocol, id)
			}
		}
		if test.wantFlow == "" {
			if _, ok := hostChoiceIn(&canvas, state.messages.Tools.FlowNone); ok {
				t.Errorf("protocol %q: the flow choice is painted", test.protocol)
			}
		} else if _, ok := hostChoiceIn(&canvas, test.wantFlow); !ok {
			t.Errorf("protocol %q: the flow choice is not painted", test.protocol)
		}
	}
}

func TestProtocolsSurviveARestartAndAnOldFileStillMeansSSH(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	first.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(first)
	telnetHost := addStreamHost(first, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet, Port: 2323})
	serialHost := addStreamHost(first, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial,
		Line: serial.Settings{Baud: 19200, DataBits: 8, StopBits: 1, Parity: serial.ParityOdd, Flow: serial.FlowSoftware}})
	first.selectedHostID = serialHost.ID
	openHost(first)
	first.save()

	second := newTestState(t, options)
	got := map[string]Host{}
	for _, host := range second.hosts {
		got[host.Label] = host
	}
	if h := got["Switch"]; h.Protocol != protocolTelnet || h.Port != 2323 || h.CatalogID != telnetHost.CatalogID {
		t.Fatalf("Telnet host = %+v", h)
	}
	if h := got["Console"]; h.Protocol != protocolSerial || h.Line != serialHost.Line || h.Address != "/dev/ttyUSB0" {
		t.Fatalf("serial host = %+v", h)
	}
	if h := got["Production"]; h.Protocol != protocolSSH || h.Port != 22 {
		t.Fatalf("an old host = %+v", h)
	}
	if len(second.sessions) != 1 || second.sessions[0].host.Protocol != protocolSerial || second.sessions[0].HostID != got["Console"].ID {
		t.Fatalf("restored sessions = %+v", second.sessions)
	}
}

func TestHostCatalogCarriesProtocolsAndRefusesDamagedSerialLines(t *testing.T) {
	state := newTestState(t, testOptions(t))
	addStreamHost(state, Host{Label: "Switch", Address: "10.0.0.2", Protocol: protocolTelnet})
	addStreamHost(state, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial, Line: serial.Default})
	var document hostCatalogDocument
	for _, host := range state.hosts {
		document.Hosts = append(document.Hosts, savedHostFromHost(host))
	}
	other := newTestState(t, testOptions(t))
	other.hosts = other.hosts[:0]
	added, duplicates := other.applyHostCatalog(document)
	if added != len(state.hosts) || duplicates != 0 {
		t.Fatalf("added %d duplicates %d", added, duplicates)
	}
	for index, host := range other.hosts {
		if host.Protocol != state.hosts[index].Protocol || host.Line != state.hosts[index].Line || host.Port != state.hosts[index].Port {
			t.Errorf("host %d = %+v, want %+v", index, host, state.hosts[index])
		}
	}
	// Importing it again changes nothing.
	if added, duplicates := other.applyHostCatalog(document); added != 0 || duplicates != len(state.hosts) {
		t.Fatalf("second import added %d duplicates %d", added, duplicates)
	}
	// A serial entry whose line is not one is dropped, not opened with a guess.
	broken := savedHost{Label: "Bad", Address: "/dev/ttyUSB9", savedTransport: savedTransport{Protocol: "serial", Line: "fast"}}
	if _, ok := other.hostFromCatalogEntry(broken); ok {
		t.Fatal("a damaged serial line was accepted")
	}
	unknown := savedHost{Label: "Odd", Address: "x", savedTransport: savedTransport{Protocol: "rlogin"}}
	if _, ok := other.hostFromCatalogEntry(unknown); ok {
		t.Fatal("an unknown protocol was accepted")
	}
}

func TestForwardAndJumpPickersSkipHostsThatAreNotSSH(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.hosts = state.hosts[:0]
	console := addStreamHost(state, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial, Line: serial.Default})
	state.openForwardDialog()
	if state.modal == modalNewForward {
		t.Fatal("a forward was offered with no SSH host to carry it")
	}
	ssh := addStreamHost(state, Host{Label: "Server", Address: "server.example.com", Port: 22})
	state.selectedHostID = console.ID
	state.openForwardDialog()
	if state.modal != modalNewForward || state.forwardEditor.hostID != ssh.ID {
		t.Fatalf("forward host = %d, want %d", state.forwardEditor.hostID, ssh.ID)
	}
	state.closeModal()
	// A jump route is its own record, not a pair of saved hosts (decision 0031),
	// so it opens with one SSH host and asks for the bastion in the dialog.
	state.openProxyDialog()
	if state.modal != modalNewProxy {
		t.Fatal("a jump route was not offered with one SSH host")
	}
	state.closeModal()
}

func TestImportedTelnetAndSerialSessionsKeepDistinctNamesAndSkipRepeats(t *testing.T) {
	state := newTestState(t, testOptions(t))
	result := sessionimport.Result{Entries: []sessionimport.Entry{
		{Name: "Switch", Group: "Lab/Net", Protocol: sessionimport.Telnet, Host: "10.0.0.2"},
		{Name: "Console", Protocol: sessionimport.Serial, Host: "COM7", Line: "115200 7E2", Flow: "rtscts"},
		{Name: "Console again", Protocol: sessionimport.Serial, Host: "COM7", Line: "9600 8N1"},
		{Name: "Console again", Protocol: sessionimport.Serial, Host: "COM7", Line: "9600 8N1"},
		{Name: "Broken", Protocol: sessionimport.Serial, Host: "COM8", Line: "fast"},
	}}
	plan := state.sessionImportPlan(result)
	if len(plan.hosts) != 3 || plan.duplicateHosts != 2 {
		t.Fatalf("hosts %+v duplicates %d", plan.hosts, plan.duplicateHosts)
	}
	telnetHost, console, other := plan.hosts[0], plan.hosts[1], plan.hosts[2]
	if telnetHost.Protocol != protocolTelnet || telnetHost.Port != 23 || telnetHost.Group != "Lab/Net" {
		t.Fatalf("Telnet host = %+v", telnetHost)
	}
	want := serial.Settings{Baud: 115200, DataBits: 7, StopBits: 2, Parity: serial.ParityEven, Flow: serial.FlowHardware}
	if console.Protocol != protocolSerial || console.Line != want || console.Address != "COM7" {
		t.Fatalf("serial host = %+v", console)
	}
	if other.Label != "Console again" || other.Protocol != protocolSerial || other.Address != "COM7" || other.Line.Baud != 9600 {
		t.Fatalf("second serial host at one device was skipped or lost its line: %+v", other)
	}
	warnings := strings.Join(plan.warnings, "\n")
	if !strings.Contains(warnings, "Console again (COM7 · 9600 8N1) was not imported") {
		t.Fatalf("the repeated session is not named: %q", warnings)
	}
}

// The serial form offers the ports the machine reports, looked up again when the
// picker opens, and choosing one writes it into the device field.
func TestPortsPickerOffersTheDetectedPorts(t *testing.T) {
	previous := serialPorts
	serialPorts = func() []serial.Port {
		return []serial.Port{{Device: "/dev/ttyUSB7", Description: "USB serial"}, {Device: "/dev/ttyS0"}}
	}
	t.Cleanup(func() { serialPorts = previous })
	state := newTestState(t, testOptions(t))
	state.editor.ports = nil
	state.Click(hitAddHost)
	clickHostChoice(t, state, state.messages.Tools.ProtocolSerial)
	clickPageLabel(t, state, gfx.RoleCombobox, state.messages.Tools.PortsPicker)
	canvas, _ := paintedTexts(state)
	found := false
	for _, hit := range canvas.Hits() {
		if hit.Role == gfx.RoleOption && hit.Label == "/dev/ttyUSB7" {
			found = true
		}
	}
	if !found {
		t.Fatal("the open picker does not offer the detected port")
	}
	clickPageLabel(t, state, gfx.RoleOption, "/dev/ttyUSB7")
	if got := state.editor.value(fieldAddress); got != "/dev/ttyUSB7" {
		t.Fatalf("the chosen port is %q, want the device field to hold it", got)
	}
	if state.modal != modalNewHost {
		t.Fatal("choosing a port closed the host dialog")
	}
}

func TestTypingATelnetOrSSHLinkConnectsToIt(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)

	state.search.Insert("telnet://10.0.0.5:2323/")
	state.connectTyped()
	waitUntil(t, state, "the Telnet request", func() bool { return len(streams.requests()) == 1 })
	if host := streams.requests()[0].Host; host.Protocol != protocolTelnet || host.Address != "10.0.0.5" || host.Port != 2323 {
		t.Fatalf("host = %+v", host)
	}
	if len(world.conns) != 0 {
		t.Fatal("a Telnet link dialed SSH")
	}

	state.search = text.Editor{}
	state.search.Insert("ssh://ops@new.example.com:2200")
	state.connectTyped()
	if len(world.configs) != 1 || world.configs[0].Host != "new.example.com" || world.configs[0].Port != 2200 || world.configs[0].User != "ops" {
		t.Fatalf("SSH configs = %+v", world.configs)
	}
}
