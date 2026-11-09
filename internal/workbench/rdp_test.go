package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"

	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
	"motty/internal/sshconn"
)

// pictureOnly is a desktop session with a picture and nothing else: what a
// workbench test needs from a server that already accepted the connection.
type pictureOnly struct {
	done chan struct{}
	once sync.Once
}

func newPictureOnly() *pictureOnly { return &pictureOnly{done: make(chan struct{})} }

func (s *pictureOnly) Info() desktop.Info                                       { return desktop.Info{Protocol: "RDP", Encrypted: true} }
func (s *pictureOnly) Size() (int, int)                                         { return 64, 32 }
func (s *pictureOnly) Present(dst desktop.Presenter) bool                       { dst.Resize(64, 32); return true }
func (s *pictureOnly) Done() <-chan struct{}                                    { return s.done }
func (s *pictureOnly) Err() error                                               { return nil }
func (s *pictureOnly) SendPointer(int, int, desktop.Buttons) error              { return nil }
func (s *pictureOnly) SendWheel(int, int, desktop.Wheel, desktop.Buttons) error { return nil }
func (s *pictureOnly) SendKey(desktop.KeyEvent) error                           { return nil }
func (s *pictureOnly) SendText(string) error                                    { return nil }
func (s *pictureOnly) SetClipboard(string) error                                { return nil }
func (s *pictureOnly) RemoteClipboard() (string, bool)                          { return "", false }
func (s *pictureOnly) Bells() int                                               { return 0 }
func (s *pictureOnly) SetActive(bool)                                           {}
func (s *pictureOnly) Close() error                                             { s.once.Do(func() { close(s.done) }); return nil }

// rdpState opens an RDP host whose "server" presents the certificate that
// *certificate holds when it is dialed, through the real certificate question,
// and records what the opener was asked.
func rdpState(t *testing.T, certificate *[]byte, opened *[]desktopRequest) (*State, *Session) {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	state.dialDesktop = func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		*opened = append(*opened, request)
		if err := request.Verify("win.example.com:3389", *certificate); err != nil {
			return nil, err
		}
		return newPictureOnly(), nil
	}
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: `CORP\alice`, Protocol: protocolRDP})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	return state, session
}

func TestRDPHostIsSavedWithItsAccountAndDefaultPort(t *testing.T) {
	host := Host{Label: "Win", Address: "win.example.com", User: `CORP\alice`, Protocol: protocolRDP, Clipboard: true, Console: true}
	host.normalizeEndpoint()
	host.finish()
	if host.Port != 3389 || host.User != `CORP\alice` || host.target != "rdp://win.example.com" {
		t.Fatalf("host = %+v", host)
	}
	var restored Host
	if !restored.applyTransport(host.transport()) || restored.Protocol != protocolRDP || !restored.Clipboard || !restored.Console {
		t.Fatalf("restored = %+v", restored)
	}
	if user, domain := rdpAccount(`CORP\alice`); user != "alice" || domain != "CORP" {
		t.Errorf("account split into %q and %q", user, domain)
	}
	if user, domain := rdpAccount("alice@corp.example"); user != "alice@corp.example" || domain != "" {
		t.Errorf("a principal name was split into %q and %q", user, domain)
	}
}

func TestRDPCertificateIsAskedOnceThenPinnedAndAChangeIsAskedLoudly(t *testing.T) {
	certificate := []byte("first certificate")
	var opened []desktopRequest
	state, session := rdpState(t, &certificate, &opened)

	// A server never seen before: the question says so, and trusting it connects.
	waitUntil(t, state, "the certificate question", func() bool { return state.modal == modalHostKey })
	if description := state.hostKeyDescription; strings.Contains(description, "CHANGED") || !strings.Contains(description, "win.example.com:3389") {
		t.Fatalf("a new certificate was described as %q", description)
	}
	if session.hostKey.Fingerprint == "" || !strings.HasPrefix(session.hostKey.Fingerprint, "SHA256:") {
		t.Fatalf("the dialog has no fingerprint: %+v", session.hostKey)
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "the picture", func() bool { return session.desk != nil })
	pinned, _ := os.ReadFile(filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile))
	if !strings.HasPrefix(string(pinned), "rdp:win.example.com:3389 SHA256:") {
		t.Fatalf("pinned file = %q", pinned)
	}
	if request := opened[0]; request.Host.User != `CORP\alice` {
		t.Fatalf("the opener was asked for %+v", request.Host)
	}

	// The same certificate again: no question.
	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "the picture again", func() bool {
		return session.desk != nil && session.desk.session != nil && session.phase == sshconn.PhaseConnected
	})
	if state.modal == modalHostKey {
		t.Fatal("a pinned certificate was asked about again")
	}

	// A different certificate for the same server: a louder question, and
	// refusing it ends the connection without a picture.
	certificate = []byte("a different certificate")
	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "the changed-certificate question", func() bool { return state.modal == modalHostKey })
	if description := state.hostKeyDescription; !strings.Contains(description, "CHANGED") {
		t.Fatalf("a changed certificate was described as %q", description)
	}
	state.Click(hitHostKeyReject)
	waitUntil(t, state, "the failure", func() bool { return session.phase == sshconn.PhaseFailed })
	if !errors.Is(session.err, sshconn.ErrHostKeyRejected) {
		t.Fatalf("session error = %v", session.err)
	}
}

func TestRDPConsoleSessionIsAnRDPOnlySwitch(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.setProtocol(protocolVNC)
	canvas, _ := paintAt(state, gfx.Size{Width: 1200, Height: 1000})
	if _, ok := hitByID(canvas, hitDialogConsole); ok {
		t.Fatal("a VNC host was offered a console session")
	}
	state.editor.setProtocol(protocolRDP)
	canvas, _ = paintAt(state, gfx.Size{Width: 1200, Height: 1000})
	if _, ok := hitByID(canvas, hitDialogConsole); !ok {
		t.Fatal("an RDP host was not offered a console session")
	}
	state.Click(hitDialogConsole)
	typeInto(state, hitDialogLabel, "Console")
	typeInto(state, hitDialogAddress, "console.example.com")
	typeInto(state, hitDialogUser, `CORP\alice`)
	state.Click(hitDialogSubmit)
	if state.modal != modalNone || state.dialogError != "" {
		t.Fatalf("submit: modal=%d error=%q", state.modal, state.dialogError)
	}
	index := state.hostIndexByEndpoint(Host{Protocol: protocolRDP, Address: "console.example.com", User: `CORP\alice`, Port: 3389})
	if index < 0 {
		t.Fatal("the RDP host was not added")
	}
	host := state.hosts[index]
	if !host.Console {
		t.Fatal("the console switch was not saved with the host")
	}
	var restored Host
	if !restored.applyTransport(host.transport()) || !restored.Console {
		t.Fatalf("restored = %+v", restored)
	}
}
