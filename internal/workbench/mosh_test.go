package workbench

import "testing"

// A mosh host is still an SSH host: its saved record keeps every SSH field and
// gains only the terminal transport, and it has no SSH server capabilities.
func TestMoshTransportRoundTrip(t *testing.T) {
	host := Host{Protocol: protocolSSH, Terminal: terminalMosh, Address: "example.test", User: "skyler", Port: 22}
	host.finish()
	saved := host.transport()
	if saved.Terminal != "mosh" {
		t.Fatalf("saved terminal = %q, want mosh", saved.Terminal)
	}
	var restored Host
	if !restored.applyTransport(saved) {
		t.Fatal("applyTransport rejected a mosh record")
	}
	if restored.Terminal != terminalMosh || !restored.mosh() {
		t.Fatalf("restored terminal = %v, mosh = %v", restored.Terminal, restored.mosh())
	}
	session := Session{host: restored}
	if session.hasServer() {
		t.Fatal("a mosh session reports an SSH server")
	}

	// mosh-server requires a UTF-8 locale.
	bad := saved
	bad.Encoding = "GB18030"
	var rejected Host
	if rejected.applyTransport(bad) {
		t.Fatal("applyTransport accepted mosh with a legacy encoding")
	}
}

// The terminal transport is part of an SSH destination: typing the address of a
// mosh host must not open it as an SSH session, and the two must not merge.
func TestMoshTransportIsPartOfTheEndpoint(t *testing.T) {
	ssh := Host{Protocol: protocolSSH, Address: "example.test", User: "skyler", Port: 22}
	moshHost := ssh
	moshHost.Terminal = terminalMosh
	if sameEndpoint(ssh, moshHost) {
		t.Fatal("the same address with different terminal transports is one endpoint")
	}
	if mergeKeyOf(ssh) == mergeKeyOf(moshHost) {
		t.Fatal("the same address with different terminal transports has one merge key")
	}
}

// The form will not submit settings a mosh session cannot carry.
func TestMoshFormRejectsIncompatibleSettings(t *testing.T) {
	state := newTestState(t, testOptions(t))
	editor := &state.editor
	editor.label.Insert("Mosh lab")
	editor.address.Insert("example.test")
	editor.protocol, editor.terminal = protocolSSH, terminalMosh

	editor.encoding = "GB18030"
	if got := editor.validate(nil, 0, state.messages); got != state.messages.Tools.ErrMoshEncoding {
		t.Fatalf("legacy encoding: %q", got)
	}
	editor.encoding = ""
	editor.forwardX11 = true
	if got := editor.validate(nil, 0, state.messages); got != state.messages.Tools.ErrMoshForwarding {
		t.Fatalf("X11 forwarding: %q", got)
	}
	editor.forwardX11 = false
	if got := editor.validate(nil, 0, state.messages); got != "" {
		t.Fatalf("valid mosh host rejected: %q", got)
	}
}

// Mosh is a connection-type entry on the same SSH host. Switching between SSH
// and Mosh keeps the credentials and port the user typed; switching to another
// protocol clears them as any protocol change does.
func TestMoshConnectionChoiceKeepsSSHCredentials(t *testing.T) {
	editor := &hostEditor{protocol: protocolSSH, terminal: terminalSSH}
	editor.password.Insert("secret")
	editor.port.Insert("2222")

	editor.connection = connectionMosh
	editor.applyConnection(connectionSSH)
	if editor.protocol != protocolSSH || editor.terminal != terminalMosh ||
		editor.password.String() != "secret" || editor.value(fieldPort) != "2222" {
		t.Fatalf("SSH→Mosh changed the host: protocol=%v terminal=%v password=%q port=%q",
			editor.protocol, editor.terminal, editor.password.String(), editor.value(fieldPort))
	}
	editor.encoding = "GB18030"
	editor.connection = connectionSSH
	editor.applyConnection(connectionMosh)
	if editor.protocol != protocolSSH || editor.terminal != terminalSSH || editor.password.String() != "secret" {
		t.Fatalf("Mosh→SSH changed the host")
	}

	editor.connection = connectionMosh
	editor.applyConnection(connectionSSH)
	if editor.encoding != "" {
		t.Fatalf("choosing Mosh kept encoding %q", editor.encoding)
	}
	editor.connection = connectionTelnet
	editor.applyConnection(connectionMosh)
	if editor.protocol != protocolTelnet || editor.password.String() != "" {
		t.Fatalf("Mosh→Telnet: protocol=%v password=%q", editor.protocol, editor.password.String())
	}
}
