package workbench

import (
	"context"
	"crypto/rand"
	"crypto/rsa"
	"encoding/binary"
	"errors"
	"io"
	"math/big"
	"net"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
	"motty/internal/desktop/pins"
	"motty/internal/rfb"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

type encryptedVNC struct{ *pictureOnly }

func (s *encryptedVNC) Info() desktop.Info { return desktop.Info{Protocol: "VNC", Encrypted: true} }

func realVNCState(t *testing.T, user string, open func(context.Context, desktopRequest) (desktop.Session, error)) (*State, Host) {
	t.Helper()
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	state.dialDesktop = open
	host := addStreamHost(state, Host{Label: "Pi", Address: "pi.example.com", User: user, Protocol: protocolVNC})
	return state, host
}

func TestVNCServerKeyPinIsSeparateAndAChangedKeyIsRefused(t *testing.T) {
	key := []byte("canonical RSA key")
	state, _ := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.VerifyServerKey("pi.example.com:5900", key); err != nil {
			return nil, err
		}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "VNC server key", func() bool { return state.modal == modalHostKey })
	if session.hostKey.Type != vncServerKeyType || len(session.hostKey.Known) != 0 || strings.Contains(state.hostKeyDescription, "Windows") {
		t.Fatalf("unexpected server key question: %+v, %q", session.hostKey, state.hostKeyDescription)
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "encrypted VNC picture", func() bool { return session.desk != nil })
	store := pins.New(filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile))
	endpoint := pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}
	pinned, err := store.Lookup(endpoint)
	if err != nil || pinned != pins.Fingerprint(key) {
		t.Fatalf("server identity was not saved by endpoint: %q, %v", pinned, err)
	}
	if got, err := store.Lookup(pins.Endpoint{Protocol: pins.RDP, Address: endpoint.Address}); err != nil || got != "" {
		t.Fatalf("VNC saved an RDP pin: %q, %v", got, err)
	}
	if strip := state.desktopStripLeft(session.desk, session); !strings.Contains(strip, state.messages.Desktop.StripVNCEncrypted) || strings.Contains(strip, "TLS") {
		t.Fatalf("RSA-AES protection described as %q", strip)
	}
	if strings.Contains(session.term.screen.Text(), state.messages.Desktop.VNCPlaintext) {
		t.Fatal("an encrypted VNC session logged an unencrypted warning")
	}

	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "VNC with saved key", func() bool { return session.phase == sshconn.PhaseConnected && session.desk != nil })
	if state.modal == modalHostKey {
		t.Fatal("the accepted key was asked again")
	}

	key = []byte("changed canonical RSA key")
	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "changed VNC key", func() bool { return session.phase == sshconn.PhaseFailed })
	if len(session.hostKey.Known) != 1 || state.modal == modalHostKey {
		t.Fatalf("changed key offered for override: %+v", session.hostKey)
	}
	if !errors.Is(session.err, sshconn.ErrHostKeyChanged) {
		t.Fatalf("refusal = %v", session.err)
	}
	pinnedAfter, err := store.Lookup(endpoint)
	if err != nil || pinnedAfter != pinned {
		t.Fatal("rejecting a changed key replaced the saved pin")
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnForget); !ok {
		t.Fatal("a changed VNC identity has no forget action")
	}
	state.Click(hitConnForget)
	if got, err := store.Lookup(endpoint); err != nil || got != "" {
		t.Fatalf("forgetting the server kept its pin: %q, %v", got, err)
	}
	state.Click(hitConnRetry)
	waitUntil(t, state, "forgotten VNC server prompts again", func() bool { return state.modal == modalHostKey })
	if len(session.hostKey.Known) != 0 || session.hostKey.Fingerprint != pins.Fingerprint(key) || session.desk != nil {
		t.Fatal("forgetting a changed server granted trust instead of prompting again")
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "reinstalled server desktop", func() bool { return session.desk != nil })
	if got, err := store.Lookup(endpoint); err != nil || got != pins.Fingerprint(key) {
		t.Fatalf("new identity was not saved after explicit trust: %q, %v", got, err)
	}
}

func TestVNCTrustOnceRejectDismissAndCancelProtectCredentials(t *testing.T) {
	for _, action := range []string{"once", "reject", "dismiss", "cancel"} {
		t.Run(action, func(t *testing.T) {
			credentials := make(chan bool, 1)
			state, _ := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				if err := request.VerifyServerKey("pi.example.com:5900", []byte("key")); err != nil {
					return nil, err
				}
				credentials <- true
				return &encryptedVNC{newPictureOnly()}, nil
			})
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "server identity question", func() bool { return state.modal == modalHostKey })
			select {
			case <-credentials:
				t.Fatal("authentication started before server identity confirmation")
			default:
			}
			switch action {
			case "once":
				state.Click(hitHostKeyOnce)
				waitUntil(t, state, "trusted-once picture", func() bool { return session.desk != nil })
			case "reject":
				state.Click(hitHostKeyReject)
			case "dismiss":
				state.Click(hitHostKeyDismiss)
			case "cancel":
				session.conn.Cancel()
			}
			if action != "once" {
				waitUntil(t, state, "terminated VNC handshake", func() bool {
					return session.phase == sshconn.PhaseFailed || session.phase == sshconn.PhaseClosed
				})
				select {
				case <-credentials:
					t.Fatal("credentials sent after identity rejection or cancellation")
				default:
				}
			}
			store := pins.New(filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile))
			if got, err := store.Lookup(pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}); err != nil || got != "" {
				t.Fatal("one-time trust/refusal saved a key")
			}
		})
	}
}

func TestVNCSystemPasswordNeverAutofillsTheLegacySecret(t *testing.T) {
	answers := make(chan [2]string, 1)
	state, host := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		user, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [2]string{user, password}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	legacy := hostSecretName(host.CatalogID, hostPasswordPart)
	system := hostSecretName(host.CatalogID, vncSystemPasswordPart("alice"))
	if err := state.secrets.vault.Set(legacy, "legacy password"); err != nil {
		t.Fatal(err)
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "system password question", func() bool { return state.modal == modalPassword })
	if len(state.passwordFields) != 1 || state.passwordFields[0].String() != "" || state.passwordName(session) != system {
		t.Fatalf("system question used the legacy secret: fields=%d name=%q", len(state.passwordFields), state.passwordName(session))
	}
	state.secrets.save = true
	state.passwordFields[0].Insert("system password")
	state.Click(hitPasswordSubmit)
	waitUntil(t, state, "system picture", func() bool { return session.desk != nil })
	if got := <-answers; got != [2]string{"alice", "system password"} {
		t.Fatalf("credentials = %q", got)
	}
	if value, found := vaultValue(t, state, legacy); !found || value != "legacy password" {
		t.Fatalf("legacy secret replaced = %q, %v", value, found)
	}
	if value, found := vaultValue(t, state, system); !found || value != "system password" {
		t.Fatalf("system secret not saved = %q, %v", value, found)
	}

	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "autofilled system picture", func() bool { return session.phase == sshconn.PhaseConnected && session.desk != nil })
	if state.modal == modalPassword {
		t.Fatal("the system secret was not reused")
	}
	if got := <-answers; got != [2]string{"alice", "system password"} {
		t.Fatalf("reused credentials = %q", got)
	}
}

func TestVNCUnknownSystemUserAsksBothFieldsAndSavesForTheAnsweredAccount(t *testing.T) {
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
	waitUntil(t, state, "system account question", func() bool { return state.modal == modalPassword })
	if len(state.passwordQuestions) != 2 || state.passwordQuestions[0].Secret || !state.passwordQuestions[1].Secret || state.passwordFields[1].String() != "" {
		t.Fatalf("wrong credential fields: %+v", state.passwordQuestions)
	}
	state.Click(hitPasswordSubmit)
	if state.modal != modalPassword || state.passwordError == "" {
		t.Fatal("missing VNC credentials were submitted")
	}
	state.Click(hitPasswordSave)
	state.passwordFields[0].Insert("bob")
	state.passwordFields[1].Insert("bob password")
	state.Click(hitPasswordSubmit)
	waitUntil(t, state, "account picture", func() bool { return session.desk != nil })
	if got := <-answers; got != [2]string{"bob", "bob password"} {
		t.Fatalf("answered credentials = %q", got)
	}
	if value, found := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart("bob"))); !found || value != "bob password" {
		t.Fatalf("secret saved under wrong account: %q, %v", value, found)
	}
	if value, found := vaultValue(t, state, legacy); !found || value != "legacy password" {
		t.Fatalf("system account changed legacy secret: %q, %v", value, found)
	}
	session.desk.session.Close()
	state.reconnect(session)
	waitUntil(t, state, "saved account reconnect", func() bool { return session.desk != nil })
	if state.modal == modalPassword {
		t.Fatal("the unique saved account required another credential confirmation")
	}
	if got := <-answers; got != [2]string{"bob", "bob password"} {
		t.Fatalf("reconnected credentials = %q", got)
	}
}

func TestVNCRefusedSystemPasswordCanCorrectTheAccountWithoutDeletingLegacyPassword(t *testing.T) {
	attempts := 0
	state, host := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		user, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		attempts++
		if attempts == 1 {
			return nil, rfb.ErrAuthFailed
		}
		if user != "bob" || password != "correct" {
			return nil, errors.New("unexpected retry account")
		}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	legacy := hostSecretName(host.CatalogID, hostPasswordPart)
	alice := hostSecretName(host.CatalogID, vncSystemPasswordPart("alice"))
	state.secrets.vault.Set(legacy, "legacy")
	state.secrets.vault.Set(alice, "refused")
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "account retry", func() bool { return state.modal == modalPassword && session.attempt == 2 })
	if len(state.passwordFields) != 2 || state.passwordFields[0].String() != "alice" || state.passwordFields[1].String() != "" {
		t.Fatalf("retry cannot correct the account: fields=%d", len(state.passwordFields))
	}
	if _, found := vaultValue(t, state, alice); found {
		t.Fatal("the refused saved system password was retained")
	}
	state.passwordFields[0] = *text.NewEditor("bob")
	state.passwordFields[1].Insert("correct")
	state.secrets.save = true
	state.Click(hitPasswordSubmit)
	waitUntil(t, state, "corrected account picture", func() bool { return session.desk != nil })
	if value, found := vaultValue(t, state, legacy); !found || value != "legacy" {
		t.Fatalf("the legacy password was removed after a system failure: %q, %v", value, found)
	}
	if value, found := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart("bob"))); !found || value != "correct" {
		t.Fatalf("corrected password = %q, %v", value, found)
	}
}

func TestVNCAndRDPCannotReuseEachOthersIdentityPin(t *testing.T) {
	store := pins.New(filepath.Join(t.TempDir(), "pins"))
	address, key := "same.example.com:5900", []byte("same key")
	if err := store.Remember(pins.Endpoint{Protocol: pins.RDP, Address: address}, pins.Fingerprint(key), ""); err != nil {
		t.Fatal(err)
	}
	notified := make(chan struct{}, 1)
	conn := newDesktopConn(Host{Address: "same.example.com", Port: 5900, Protocol: protocolVNC}, func() { notified <- struct{}{} }, nil, 0, nil, store)
	t.Cleanup(conn.Cancel)
	result := make(chan error, 1)
	go func() { result <- conn.verifyServerKey(address, key) }()
	select {
	case <-notified:
		if conn.Status().Phase != sshconn.PhaseHostKeyPrompt {
			t.Fatalf("unexpected VNC identity status: %+v", conn.Status())
		}
	case err := <-result:
		t.Fatalf("VNC reused an RDP identity pin: %v", err)
	case <-time.After(5 * time.Second):
		t.Fatal("VNC identity check did not raise a trust question")
	}
	conn.Cancel()
	if err := <-result; !errors.Is(err, sshconn.ErrCanceled) {
		t.Fatalf("canceled identity check = %v", err)
	}
}

func TestVNCSystemHostEditorKeepsTheOptionalAccountAndSeparatePassword(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.openHostDialog()
	state.editor.setProtocol(protocolVNC)
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitDialogUser); !ok {
		t.Fatal("a direct VNC host has no optional system-account input")
	}
	state.editor.label.Insert("Pi")
	state.editor.address.Insert("pi.example.com")
	state.editor.user.Insert("alice")
	state.editor.password.Insert("alice password")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("saving VNC host: %q", state.dialogError)
	}
	host := savedHostNamed(t, state, "Pi")
	if host.User != "alice" || host.Port != 5900 {
		t.Fatalf("account not retained: %+v", host)
	}
	if value, found := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart("alice"))); !found || value != "alice password" {
		t.Fatalf("system password not kept under its account: %q, %v", value, found)
	}
	if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, hostPasswordPart)); found {
		t.Fatal("a system password was also saved as a legacy VNC password")
	}

	state.selectedHostID = host.ID
	clickHostAction(state, hitEditHost)
	if state.editor.password.String() != "alice password" {
		t.Fatal("the system account's saved password was not shown in the editor")
	}
	state.editor.user = *text.NewEditor("bob")
	state.Click(hitDialogSubmit)
	if state.modal != modalNewHost || state.dialogError == "" {
		t.Fatal("changing a VNC account copied its old prefilled password without asking")
	}
	state.editor.password = text.Editor{}
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("changing the VNC account without moving its old secret: %q", state.dialogError)
	}
	if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart("alice"))); !found {
		t.Fatal("changing the account deleted its former account's password")
	}
	if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart("bob"))); found {
		t.Fatal("changing the account copied the password to the new account")
	}
}

func TestDeletingVNCRemovesAllItsSystemAccountsAndLeavesOtherHosts(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := addStreamHost(state, Host{Label: "Pi", Address: "pi.example.com", Protocol: protocolVNC})
	other := state.hosts[0]
	for _, username := range []string{"alice", "bob"} {
		if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart(username)), "secret"); err != nil {
			t.Fatal(err)
		}
	}
	otherName := hostSecretName(other.CatalogID, vncSystemPasswordPart("alice"))
	state.secrets.vault.Set(otherName, "other")
	state.hostActionIDs = []uint32{host.ID}
	state.confirmDeleteHosts()
	if state.hostIndex(host.ID) >= 0 {
		t.Fatalf("host deletion failed: %q", state.dialogError)
	}
	for _, username := range []string{"alice", "bob"} {
		if _, found := vaultValue(t, state, hostSecretName(host.CatalogID, vncSystemPasswordPart(username))); found {
			t.Fatalf("deleting the host left %s's saved system password", username)
		}
	}
	if value, found := vaultValue(t, state, otherName); !found || value != "other" {
		t.Fatalf("deleting another host removed its secret: %q, %v", value, found)
	}
}

func TestLegacyVNCWithAnOptionalSystemUserStillUsesOnlyItsLegacyPassword(t *testing.T) {
	passwords := make(chan string, 1)
	state, host := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		password, err := request.Ask()
		if err != nil {
			return nil, err
		}
		passwords <- password
		return &encryptedVNC{newPictureOnly()}, nil
	})
	state.secrets.vault.Set(hostSecretName(host.CatalogID, hostPasswordPart), "legacy")
	state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart("alice")), "system")
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "legacy VNC picture", func() bool { return session.desk != nil })
	if got := <-passwords; got != "legacy" {
		t.Fatalf("legacy VNC received a system password: %q", got)
	}
	if session.endpoint.User != vncAccount {
		t.Fatalf("legacy VNC retained a system-auth endpoint: %+v", session.endpoint)
	}
}

func TestVNCSystemCredentialCancellationSendsNoAnswer(t *testing.T) {
	answered := make(chan bool, 1)
	state, _ := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		_, _, err := request.AskCredentials()
		if err == nil {
			answered <- true
			return &encryptedVNC{newPictureOnly()}, nil
		}
		return nil, err
	})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "system account dialog", func() bool { return state.modal == modalPassword })
	state.passwordFields[0].Insert("alice")
	state.passwordFields[1].Insert("unsubmitted")
	state.secrets.save = true
	state.Click(hitPasswordCancel)
	waitUntil(t, state, "canceled system login", func() bool { return session.phase == sshconn.PhaseClosed })
	select {
	case <-answered:
		t.Fatal("canceling sent system credentials")
	default:
	}
	if state.secrets.vault.Len() != 0 {
		t.Fatal("canceling saved unsubmitted credentials")
	}
}

func TestTunneledVNCVerifiesTheTypedServerAddress(t *testing.T) {
	key, err := rsa.GenerateKey(rand.Reader, 2048)
	if err != nil {
		t.Fatal(err)
	}
	size := key.Size()
	wire := make([]byte, 4+2*size)
	binary.BigEndian.PutUint32(wire, uint32(key.N.BitLen()))
	key.N.FillBytes(wire[4 : 4+size])
	big.NewInt(int64(key.E)).FillBytes(wire[4+size:])
	client, peer := net.Pipe()
	t.Cleanup(func() { client.Close(); peer.Close() })
	finished := make(chan error, 1)
	go func() {
		defer peer.Close()
		peer.SetDeadline(time.Now().Add(5 * time.Second))
		peer.Write([]byte("RFB 005.000\n"))
		banner := make([]byte, 12)
		if _, err := io.ReadFull(peer, banner); err != nil {
			finished <- err
			return
		}
		peer.Write([]byte{1, 5})
		choice := make([]byte, 1)
		if _, err := io.ReadFull(peer, choice); err != nil || choice[0] != 5 {
			finished <- errors.New("RSA-AES was not selected")
			return
		}
		if _, err := peer.Write(wire); err != nil {
			finished <- err
			return
		}
		n, err := peer.Read(choice)
		if n != 0 || err == nil {
			finished <- errors.New("client continued after server identity rejection")
			return
		}
		finished <- nil
	}()
	tunnel := &fakeConn{dial: func(context.Context, string, string) (net.Conn, error) { return client, nil }}
	refused := errors.New("typed server identity refused")
	var identityAddress string
	_, err = openDesktopSession(context.Background(), desktopRequest{
		Host:   Host{Address: "pi.example.com", Port: 5900, User: "ssh-user", Protocol: protocolVNC, ViaSSH: true},
		Tunnel: tunnel,
		VerifyServerKey: func(address string, der []byte) error {
			identityAddress = address
			return refused
		},
	})
	if !errors.Is(err, refused) || identityAddress != "pi.example.com:5900" {
		t.Fatalf("verified %q, error=%v", identityAddress, err)
	}
	if len(tunnel.dialCalls) != 1 || tunnel.dialCalls[0].address != "127.0.0.1:5900" {
		t.Fatalf("tunnel destination = %+v", tunnel.dialCalls)
	}
	if err := <-finished; err != nil {
		t.Fatal(err)
	}
}

func TestChangingToVNCSystemAccountDoesNotLeaveAnSSHPasswordForLegacyVNC(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.secrets.vault.Set(prodPasswordName, "SSH password")
	clickHostAction(state, hitEditHost)
	state.editor.setProtocol(protocolVNC)
	state.editor.user = *text.NewEditor("alice")
	state.editor.password.Insert("system password")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("protocol change: %q", state.dialogError)
	}
	if _, found := vaultValue(t, state, prodPasswordName); found {
		t.Fatal("the former SSH password can still autofill legacy VNC")
	}
	if value, found := vaultValue(t, state, hostSecretName(prodCatalogID, vncSystemPasswordPart("alice"))); !found || value != "system password" {
		t.Fatalf("system password after protocol change: %q, %v", value, found)
	}
}

func TestVNCCopyKeepsSameAccountSavedPassword(t *testing.T) {
	for _, variant := range []struct {
		name, user, copiedUser string
		viaSSH                 bool
	}{
		{"legacy", "", "", false},
		{"system", "alice", "alice", false},
		{"through SSH", "ssh-user", "ssh-user", true},
		{"different SSH account", "ssh-user", "other-ssh-user", true},
	} {
		t.Run(variant.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			source := addStreamHost(state, Host{
				Label: "Pi", Address: "pi.example.com", User: variant.user,
				Protocol: protocolVNC, ViaSSH: variant.viaSSH,
			})
			sourceName, _ := hostPasswordSecretName(source)
			if err := state.secrets.vault.Set(sourceName, "source password"); err != nil {
				t.Fatal(err)
			}
			state.openCopyHostID(source.ID)
			if got := state.editor.password.String(); got != "source password" {
				t.Fatalf("copy did not prefill source password: %q", got)
			}
			state.editor.address = *text.NewEditor("pi-copy.example.com")
			state.editor.user = *text.NewEditor(variant.copiedUser)
			state.Click(hitDialogSubmit)
			if state.modal != modalNone {
				t.Fatalf("copy of unchanged VNC account was rejected: %q", state.dialogError)
			}
			copied := state.hosts[len(state.hosts)-1]
			if copied.ID == source.ID || copied.CatalogID == source.CatalogID || copied.User != variant.copiedUser || copied.ViaSSH != variant.viaSSH {
				t.Fatal("copied host does not have its own identity with the same account")
			}
			copiedName, _ := hostPasswordSecretName(copied)
			if value, found := vaultValue(t, state, copiedName); !found || value != "source password" {
				t.Fatalf("copied password=%q found=%t", value, found)
			}
			if err := state.secrets.vault.Set(copiedName, "copy password"); err != nil {
				t.Fatal(err)
			}
			if value, found := vaultValue(t, state, sourceName); !found || value != "source password" {
				t.Fatalf("source password changed: %q found=%t", value, found)
			}
		})
	}
}

func TestVNCAccountChangeDoesNotPlanWritesWhenSavedPasswordCannotBeRead(t *testing.T) {
	state, host := realVNCState(t, "alice", nil)
	if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart("alice")), "saved password"); err != nil {
		t.Fatal(err)
	}
	clickHostAction(state, hitEditHost)
	state.editor.user = *text.NewEditor("bob")
	state.editor.password = *text.NewEditor("new account password")
	unreadable, err := vault.Open(filepath.Join(filepath.Dir(state.options.StatePath), "vault.json"), nil)
	if err != nil {
		t.Fatal(err)
	}
	state.secrets.vault = unreadable
	changes, failure := state.hostDialogPasswordChanges(state.editor.host(host.ID), &host)
	if len(changes) != 0 || !strings.Contains(failure, state.vaultErrorText(vault.ErrUnavailable)) {
		t.Fatalf("unreadable previous password planned changes=%v, failure=%q", changes, failure)
	}
	state.Click(hitDialogSubmit)
	saved, ok := state.hostByCatalogID(host.CatalogID)
	if !ok || saved.User != "alice" || state.modal != modalNewHost || !strings.Contains(state.dialogError, state.vaultErrorText(vault.ErrUnavailable)) {
		t.Fatalf("account changed despite unreadable saved password: user=%q, modal=%d, error=%q", saved.User, state.modal, state.dialogError)
	}
}

func TestVNCCopyChangingAccountDoesNotReuseSourcePassword(t *testing.T) {
	for _, variant := range []struct {
		name, sourceUser, copiedUser string
		sourceViaSSH, copiedViaSSH   bool
		copiedPassword               string
	}{
		{name: "system account", sourceUser: "alice", copiedUser: "bob"},
		{name: "legacy to system", copiedUser: "alice"},
		{name: "system to legacy", sourceUser: "alice"},
		{name: "system to SSH tunnel", sourceUser: "alice", copiedUser: "alice", copiedViaSSH: true},
		{name: "SSH tunnel to system", sourceUser: "alice", copiedUser: "alice", sourceViaSSH: true},
		{name: "system account with a new password", sourceUser: "alice", copiedUser: "bob", copiedPassword: "new account password"},
	} {
		t.Run(variant.name, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			source := addStreamHost(state, Host{
				Label: "Pi", Address: "pi.example.com", User: variant.sourceUser,
				Protocol: protocolVNC, ViaSSH: variant.sourceViaSSH,
			})
			sourceName, _ := hostPasswordSecretName(source)
			if err := state.secrets.vault.Set(sourceName, "source password"); err != nil {
				t.Fatal(err)
			}
			state.openCopyHostID(source.ID)
			state.editor.address = *text.NewEditor("pi-copy.example.com")
			state.editor.user = *text.NewEditor(variant.copiedUser)
			state.editor.viaSSH = variant.copiedViaSSH
			state.Click(hitDialogSubmit)
			if state.modal != modalNewHost || state.dialogError == "" {
				t.Fatal("copying a changed account reused the source's prefilled password")
			}
			state.editor.password = *text.NewEditor(variant.copiedPassword)
			state.Click(hitDialogSubmit)
			if state.modal != modalNone {
				t.Fatalf("copy without transferring the source password was rejected: %q", state.dialogError)
			}
			copied := state.hosts[len(state.hosts)-1]
			copiedName, _ := hostPasswordSecretName(copied)
			if value, found := vaultValue(t, state, copiedName); found != (variant.copiedPassword != "") || value != variant.copiedPassword {
				t.Fatalf("copied account password=%q found=%t", value, found)
			}
			if value, found := vaultValue(t, state, sourceName); !found || value != "source password" {
				t.Fatalf("copy changed the source password: %q found=%t", value, found)
			}
		})
	}
}
