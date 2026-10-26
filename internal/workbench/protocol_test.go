package workbench

import (
	"context"
	"errors"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/serial"
	"motty/internal/sessionimport"
	"motty/internal/sshconn"
)

// streamWorld replaces the opener of Telnet and serial streams with one that
// hands out fake shells (or an error), and records what was asked for.
type streamWorld struct {
	requests []streamRequest
	shells   []*fakeShell
	err      error
	hold     chan struct{} // when set, opening waits for it to close
}

func (world *streamWorld) install(state *State) {
	state.openStream = func(ctx context.Context, request streamRequest) (Shell, error) {
		world.requests = append(world.requests, request)
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
		world.shells = append(world.shells, shell)
		return shell, nil
	}
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
	if len(streams.requests) != 1 || streams.requests[0].Host.Address != "10.0.0.2" || streams.requests[0].Host.Port != 23 {
		t.Fatalf("stream requests = %+v", streams.requests)
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
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
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
	if len(streams.requests) != 2 {
		t.Fatalf("requests = %d", len(streams.requests))
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
	waitUntil(t, state, "the request", func() bool { return len(streams.requests) == 1 })
	if streams.requests[0].Proxy == nil || streams.requests[0].Proxy.Address != "127.0.0.1:1080" {
		t.Fatalf("proxy = %+v", streams.requests[0].Proxy)
	}
}

func TestHostDialogAddsATelnetHostAndTheSameAddressForSSHIsAnotherHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.Click(hitDialogProtocolTelnet)
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
	// The same destination again is a duplicate.
	state.Click(hitAddHost)
	state.Click(hitDialogProtocolTelnet)
	typeInto(state, hitDialogLabel, "Again")
	typeInto(state, hitDialogAddress, "prod.example.com")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError != state.messages.ErrAddressTaken {
		t.Fatalf("duplicate Telnet host accepted: %q", state.dialogError)
	}
}

func TestHostDialogAddsASerialHostWithItsLineAndFlow(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.Click(hitDialogProtocolSerial)
	if state.editor.value(fieldPort) != "115200 8N1" {
		t.Fatalf("line = %q", state.editor.value(fieldPort))
	}
	state.editor.address = text.Editor{} // whatever port this machine offered
	typeInto(state, hitDialogLabel, "Router console")
	typeInto(state, hitDialogAddress, "/dev/ttyUSB0")
	state.editor.port.SetSelection(text.Range{Start: 0, End: len("115200 8N1")})
	typeInto(state, hitDialogPort, "9600 7E2")
	state.Click(hitDialogFlowHardware)
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
	state.Click(hitDialogProtocolSerial)
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

func TestSwitchingProtocolKeepsWhatTheUserTypedAndChangesWhatTheyDidNot(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.Click(hitDialogProtocolTelnet)
	state.Click(hitDialogProtocolSSH)
	if state.editor.value(fieldPort) != "22" {
		t.Fatalf("port went to %q", state.editor.value(fieldPort))
	}
	state.editor.port.SetSelection(text.Range{Start: 0, End: 2})
	typeInto(state, hitDialogPort, "2222")
	state.Click(hitDialogProtocolTelnet)
	if state.editor.value(fieldPort) != "2222" {
		t.Fatalf("a port the user typed was replaced by %q", state.editor.value(fieldPort))
	}
}

func TestHostDialogPaintsEachProtocolWithItsOwnFields(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	size := ui.Size{Width: 1400, Height: 900}
	for _, test := range []struct {
		protocol ui.ID
		want     []ui.ID
		absent   []ui.ID
	}{
		{hitDialogProtocolSSH, []ui.ID{hitDialogUser, hitDialogPort}, []ui.ID{hitDialogFlowNone, hitDialogPortsTrigger}},
		{hitDialogProtocolTelnet, []ui.ID{hitDialogPort}, []ui.ID{hitDialogUser, hitDialogFlowNone}},
		{hitDialogProtocolSerial, []ui.ID{hitDialogPort, hitDialogFlowNone, hitDialogPortsTrigger}, []ui.ID{hitDialogUser}},
	} {
		state.Click(test.protocol)
		var canvas ui.Canvas
		state.Paint(&canvas, size)
		for _, id := range test.want {
			if _, ok := hitByID(&canvas, id); !ok {
				t.Errorf("protocol %d: hit %d is not painted", test.protocol, id)
			}
		}
		for _, id := range test.absent {
			if _, ok := hitByID(&canvas, id); ok {
				t.Errorf("protocol %d: hit %d is painted", test.protocol, id)
			}
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
	state.openProxyDialog()
	if state.modal == modalNewProxy {
		t.Fatal("a jump route was offered with one SSH host")
	}
}

func TestImportedTelnetAndSerialSessionsBecomeHostsOnce(t *testing.T) {
	state := newTestState(t, testOptions(t))
	result := sessionimport.Result{Entries: []sessionimport.Entry{
		{Name: "Switch", Group: "Lab/Net", Protocol: sessionimport.Telnet, Host: "10.0.0.2"},
		{Name: "Console", Protocol: sessionimport.Serial, Host: "COM7", Line: "115200 7E2", Flow: "rtscts"},
		{Name: "Console again", Protocol: sessionimport.Serial, Host: "COM7", Line: "9600 8N1"},
		{Name: "Broken", Protocol: sessionimport.Serial, Host: "COM8", Line: "fast"},
	}}
	plan := state.sessionImportPlan(result)
	if len(plan.hosts) != 2 || plan.duplicateHosts != 2 {
		t.Fatalf("hosts %+v duplicates %d", plan.hosts, plan.duplicateHosts)
	}
	telnetHost, console := plan.hosts[0], plan.hosts[1]
	if telnetHost.Protocol != protocolTelnet || telnetHost.Port != 23 || telnetHost.Group != "Lab / Net" {
		t.Fatalf("Telnet host = %+v", telnetHost)
	}
	want := serial.Settings{Baud: 115200, DataBits: 7, StopBits: 2, Parity: serial.ParityEven, Flow: serial.FlowHardware}
	if console.Protocol != protocolSerial || console.Line != want || console.Address != "COM7" {
		t.Fatalf("serial host = %+v", console)
	}
}

// With the ports picker open, a press outside it closes the picker and not the
// host dialog behind it.
func TestPortsPickerClosesOnAnOutsidePressWithoutClosingTheDialog(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.Click(hitAddHost)
	state.Click(hitDialogProtocolSerial)
	state.Click(hitDialogPortsTrigger)
	if !state.editor.portsOpen {
		t.Fatal("the ports picker did not open")
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	backdrop, ok := hitByID(&canvas, hitDialogPortsDismiss)
	if !ok || backdrop.Bounds.Width != 1400 || backdrop.Bounds.Height != 900 {
		t.Fatalf("the open picker has no window-wide backdrop: %+v, %v", backdrop, ok)
	}
	state.Click(hitDialogPortsDismiss)
	if state.editor.portsOpen || state.modal != modalNewHost {
		t.Fatalf("after the outside press: portsOpen=%v modal=%d", state.editor.portsOpen, state.modal)
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
	waitUntil(t, state, "the Telnet request", func() bool { return len(streams.requests) == 1 })
	if host := streams.requests[0].Host; host.Protocol != protocolTelnet || host.Address != "10.0.0.5" || host.Port != 2323 {
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
