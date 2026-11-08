package workbench

import (
	"os"
	"path/filepath"
	"testing"

	"motty/internal/desktop/pins"
	"motty/internal/sshconn"
)

func TestRDPChangedCertificateCanBeForgottenAndPromptsAgain(t *testing.T) {
	certificate := []byte("first certificate")
	var opened []desktopRequest
	state, session := rdpState(t, &certificate, &opened)
	waitUntil(t, state, "initial certificate", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "initial picture", func() bool { return session.desk != nil })
	path := filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile)
	store := pins.New(path)
	endpoint := pins.Endpoint{Protocol: pins.RDP, Address: "win.example.com:3389"}
	previous := pins.Fingerprint(certificate)
	certificate = []byte("reinstalled server certificate")
	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "changed certificate", func() bool { return state.modal == modalHostKey })
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitHostKeyForget); !ok {
		t.Fatal("RDP changed certificate has no forget action")
	}
	// A failed removal preserves both the old pin and the unanswered prompt.
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	state.Click(hitHostKeyForget)
	if state.modal != modalHostKey || len(state.fileNotices.items) != 1 {
		t.Fatal("failed forgetting dismissed the prompt or hid the error")
	}
	if got, err := store.Lookup(endpoint); err != nil || got != previous {
		t.Fatalf("failed forgetting discarded the pin: %q, %v", got, err)
	}
	if err := os.Remove(path + ".tmp"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitHostKeyForget)
	waitUntil(t, state, "forgotten current handshake ends", func() bool { return session.phase == sshconn.PhaseFailed })
	if got, err := store.Lookup(endpoint); err != nil || got != "" {
		t.Fatalf("forgetting retained pin: %q, %v", got, err)
	}
	state.Click(hitConnRetry)
	waitUntil(t, state, "forgotten RDP identity prompts", func() bool { return state.modal == modalHostKey })
	if len(session.hostKey.Known) != 0 || session.desk != nil {
		t.Fatal("forgetting bypassed the new server identity prompt")
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "reinstalled RDP picture", func() bool { return session.desk != nil })
	if got, err := store.Lookup(endpoint); err != nil || got != pins.Fingerprint(certificate) {
		t.Fatalf("explicitly accepted replacement not saved: %q, %v", got, err)
	}
}

func TestForgetServerActionOnlyAppliesToTheDesktopTarget(t *testing.T) {
	session := &Session{
		host:    Host{Protocol: protocolVNC, Address: "pi.example.com", Port: 5900},
		hostKey: sshconn.HostKeyInfo{Address: "pi.example.com:5900", Type: vncServerKeyType, Known: []string{"old identity"}},
	}
	if !canForgetServerIdentity(session) {
		t.Fatal("desktop target cannot be forgotten")
	}
	session.role = sshconn.EndpointProxyJump
	if canForgetServerIdentity(session) {
		t.Fatal("gateway can delete desktop trust")
	}
	session.role = sshconn.EndpointTarget
	session.hostKey.Address = "another.example.com:5900"
	if canForgetServerIdentity(session) {
		t.Fatal("unrelated endpoint can be forgotten")
	}
	session.hostKey.Address = "pi.example.com:5900"
	session.host.Protocol = protocolSSH
	if canForgetServerIdentity(session) {
		t.Fatal("SSH can use the desktop forget action")
	}
	session.host.Protocol = protocolVNC
	session.hostKey.Type = rdpCertificateType
	if canForgetServerIdentity(session) {
		t.Fatal("wrong identity type can be forgotten")
	}
}
