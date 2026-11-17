package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
	"motty/internal/desktop/pins"
	"motty/internal/sshconn"
)

func TestVNCAuthOnlyConsentIsExplicitSavedAndProtocolSpecific(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	clickHostChoice(t, state, state.messages.Desktop.ProtocolVNC)
	if state.editor.host(0).VNCAuthOnly {
		t.Fatal("authentication-only encryption enabled by default")
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitDialogVNCAuthOnly); !ok {
		t.Fatal("VNC encryption permission is unavailable")
	}
	state.clickHostProtocol(hitDialogVNCAuthOnly)
	host := state.editor.host(0)
	data, err := json.Marshal(host.transport())
	if err != nil {
		t.Fatal(err)
	}
	var saved savedTransport
	if err := json.Unmarshal(data, &saved); err != nil {
		t.Fatal(err)
	}
	var restored Host
	if !restored.applyTransport(saved) || !restored.VNCAuthOnly {
		t.Fatal("VNC encryption permission lost on persistence")
	}
	state.editor.load(restored)
	if !state.editor.host(0).VNCAuthOnly {
		t.Fatal("editing/copying a VNC host lost its explicit permission")
	}
	clickHostChoice(t, state, state.messages.Desktop.ProtocolRDP)
	if state.editor.host(0).VNCAuthOnly || state.editor.host(0).transport().VNCAuthOnly {
		t.Fatal("VNC encryption permission propagated to RDP")
	}
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitDialogVNCAuthOnly); ok {
		t.Fatal("RDP exposes a VNC-only setting")
	}
	clickHostChoice(t, state, state.messages.Desktop.ProtocolVNC)
	canvas, _ = paintedTexts(state)
	if _, ok := hitByID(canvas, hitDialogVNCAuthOnly); !ok {
		t.Fatal("returning to VNC lost its permission control")
	}
	if sameSavedHost(savedHost{}, savedHost{savedTransport: savedTransport{VNCAuthOnly: true}}) {
		t.Fatal("incremental persistence ignored encryption permission change")
	}
}

func TestVNCExplicitEmptyUsernameKeepsItsPasswordSeparate(t *testing.T) {
	answers := make(chan [2]string, 1)
	state, host := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		user, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [2]string{user, password}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	legacy := hostSecretName(host.CatalogID, hostPasswordPart)
	state.secrets.vault.Set(legacy, "legacy password")
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "VNC credentials", func() bool { return state.modal == modalPassword })
	if len(state.passwordFields) != 2 || state.passwordFields[1].String() != "" {
		t.Fatal("the legacy password prefilled an account-based login")
	}
	state.passwordFields[1] = *text.NewEditor("WayVNC password")
	state.secrets.save = true
	state.Click(hitPasswordSubmit)
	waitUntil(t, state, "empty-user desktop", func() bool { return session.desk != nil })
	if got := <-answers; got != [2]string{"", "WayVNC password"} {
		t.Fatalf("submitted %q", got)
	}
	if got, ok := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart(""))); !ok || got != "WayVNC password" {
		t.Fatal("empty-account password was not saved separately")
	}
	if got, ok := vaultValue(t, state, legacy); !ok || got != "legacy password" {
		t.Fatal("the empty-account password changed the legacy secret")
	}
}

func TestVNCEmptyPasswordKeepsPromptOpen(t *testing.T) {
	for _, test := range []struct {
		name, user string
		account    bool
	}{
		{"known account", "alice", true},
		{"empty account", "", true},
		{"VNC password", "", false},
	} {
		t.Run(test.name, func(t *testing.T) {
			answers := make(chan string, 1)
			state, _ := realVNCState(t, test.user, func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				var password string
				var err error
				if test.account {
					_, password, err = request.AskCredentials()
				} else {
					password, err = request.Ask()
				}
				if err != nil {
					return nil, err
				}
				answers <- password
				return &encryptedVNC{newPictureOnly()}, nil
			})
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "VNC password prompt", func() bool { return state.modal == modalPassword })
			state.Click(hitPasswordSubmit)
			if state.modal != modalPassword || state.passwordError == "" || session.desk != nil {
				t.Fatal("empty password closed the VNC prompt")
			}
			select {
			case <-answers:
				t.Fatal("empty password reached connection")
			default:
			}
			index := passwordQuestionIndex(state.passwordQuestions)
			state.passwordFields[index] = *text.NewEditor("correct password")
			state.Click(hitPasswordSubmit)
			waitUntil(t, state, "VNC desktop after correction", func() bool { return session.desk != nil })
			if got := <-answers; got != "correct password" {
				t.Fatalf("password=%q", got)
			}
		})
	}
}

func TestVNCTLSCertificatePinsAreProtocolScopedAndCanBeForgotten(t *testing.T) {
	certificate := []byte("TLS certificate DER from protocol verifier")
	state, _ := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.Verify("pi.example.com:5900", certificate); err != nil {
			return nil, err
		}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	path := filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile)
	store := pins.New(path)
	endpoint := pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}
	if err := store.Remember(pins.Endpoint{Protocol: pins.RDP, Address: endpoint.Address}, pins.Fingerprint(certificate), ""); err != nil {
		t.Fatal(err)
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "VNC certificate", func() bool { return state.modal == modalHostKey })
	if session.hostKey.Type != vncCertificateType || len(session.hostKey.Known) != 0 || strings.Contains(state.hostKeyDescription, "Windows") {
		t.Fatalf("wrong certificate identity question: %+v", session.hostKey)
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "TLS desktop", func() bool { return session.desk != nil })
	previous := pins.Fingerprint(certificate)
	if got, err := store.Lookup(endpoint); err != nil || got != previous {
		t.Fatalf("TLS identity not saved: %q, %v", got, err)
	}
	certificate = []byte("changed TLS certificate")
	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "changed VNC certificate", func() bool { return session.phase == sshconn.PhaseFailed })
	if len(session.hostKey.Known) != 1 || state.modal == modalHostKey {
		t.Fatal("changed certificate was offered for override")
	}
	if !errors.Is(session.err, sshconn.ErrHostKeyChanged) {
		t.Fatalf("certificate rejection returned %v", session.err)
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnForget); !ok {
		t.Fatal("changed TLS identity has no forget action")
	}
	if err := os.Mkdir(path+".tmp", 0o700); err != nil {
		t.Fatal(err)
	}
	state.Click(hitConnForget)
	if got, err := store.Lookup(endpoint); err != nil || got != previous {
		t.Fatalf("failed forget changed the pin: %q, %v", got, err)
	}
	if !canForgetServerIdentity(session) || len(state.fileNotices.items) == 0 {
		t.Fatal("failed forget hid the recovery action or error")
	}
	if err := os.Remove(path + ".tmp"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitConnForget)
	if got, err := store.Lookup(endpoint); err != nil || got != "" {
		t.Fatalf("forget retained TLS pin: %q, %v", got, err)
	}
	state.Click(hitConnRetry)
	waitUntil(t, state, "forgotten TLS identity prompts", func() bool { return state.modal == modalHostKey })
	if len(session.hostKey.Known) != 0 || session.desk != nil {
		t.Fatal("forgetting bypassed identity confirmation")
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "replacement TLS desktop", func() bool { return session.desk != nil })
	if got, err := store.Lookup(endpoint); err != nil || got != pins.Fingerprint(certificate) {
		t.Fatalf("explicitly accepted replacement not saved: %q, %v", got, err)
	}
}
