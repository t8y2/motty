package workbench

import (
	"context"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

func TestVNCSingleSavedSystemAccountConnectsWithoutCredentialPrompt(t *testing.T) {
	answers := make(chan [2]string, 1)
	state, host := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.VerifyServerKey("pi.example.com:5900", []byte("server identity")); err != nil {
			return nil, err
		}
		username, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [2]string{username, password}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart("pi")), "system password"); err != nil {
		t.Fatal(err)
	}
	if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, hostPasswordPart), "legacy password"); err != nil {
		t.Fatal(err)
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "server identity confirmation", func() bool { return state.modal == modalHostKey })
	select {
	case <-answers:
		t.Fatal("saved credentials were sent before server identity confirmation")
	default:
	}
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "desktop using the saved system account", func() bool { return session.desk != nil })
	if state.modal == modalPassword {
		t.Fatal("a unique saved system account still needed credential confirmation")
	}
	if got := <-answers; got != [2]string{"pi", "system password"} {
		t.Fatalf("used the wrong saved authentication account: %q", got)
	}
	if session.host.User != "" {
		t.Fatal("recovering the VNC account rewrote the host's configured username")
	}
}

func TestVNCSavedAccountReadFailureStillAllowsManualLogin(t *testing.T) {
	answers := make(chan [2]string, 1)
	state, host := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.VerifyServerKey("pi.example.com:5900", []byte("server identity")); err != nil {
			return nil, err
		}
		username, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [2]string{username, password}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart("pi")), "saved password"); err != nil {
		t.Fatal(err)
	}
	// Reopen without access to the key, rather than using the cached key.
	path := filepath.Join(filepath.Dir(state.options.StatePath), "vault.json")
	unreadable, err := vault.Open(path, nil)
	if err != nil {
		t.Fatal(err)
	}
	state.secrets.vault = unreadable
	openHost(state)
	waitUntil(t, state, "server identity confirmation", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyOnce)
	waitUntil(t, state, "manual VNC login", func() bool { return state.modal == modalPassword })
	if state.passwordError != state.vaultErrorText(vault.ErrUnavailable) {
		t.Fatalf("saved account read failure was hidden: %q", state.passwordError)
	}
	if len(state.passwordFields) != 2 || state.passwordFields[0].String() != "" || state.passwordFields[1].String() != "" || state.secrets.canSave {
		t.Fatal("unreadable saved credentials were prefilled or offered for saving")
	}
	select {
	case <-answers:
		t.Fatal("credentials were sent before manual confirmation")
	default:
	}
	state.passwordFields[0] = *text.NewEditor("manual-user")
	state.passwordFields[1] = *text.NewEditor("manual password")
	state.Click(hitPasswordSubmit)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "desktop after manual login", func() bool { return session.desk != nil })
	if got := <-answers; got != [2]string{"manual-user", "manual password"} {
		t.Fatalf("manual credentials = %q", got)
	}
}

func TestVNCSingleSavedAccountStillRejectsUntrustedServer(t *testing.T) {
	credentials := make(chan struct{}, 1)
	state, host := realVNCState(t, "", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.VerifyServerKey("pi.example.com:5900", []byte("untrusted identity")); err != nil {
			return nil, err
		}
		credentials <- struct{}{}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	if err := state.secrets.vault.Set(hostSecretName(host.CatalogID, vncSystemPasswordPart("pi")), "saved password"); err != nil {
		t.Fatal(err)
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "server identity", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyReject)
	waitUntil(t, state, "rejected server", func() bool { return session.phase == sshconn.PhaseFailed })
	select {
	case <-credentials:
		t.Fatal("a saved account bypassed server identity rejection")
	default:
	}
}

func TestVNCSingleSavedSystemAccountOverTunnelKeepsSSHAccountSeparate(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.options.Invalidate = func() {}
	state.connectionReconnect = reconnectOff
	answers := make(chan [3]string, 1)
	state.dialDesktop = func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if request.Tunnel == nil {
			return nil, errNoServer
		}
		if err := request.VerifyServerKey("pi.example.com:5900", []byte("server identity")); err != nil {
			return nil, err
		}
		username, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [3]string{request.Host.User, username, password}
		return &encryptedVNC{newPictureOnly()}, nil
	}
	host := addStreamHost(state, Host{Label: "Pi tunnel", Address: "pi.example.com", User: "ssh-user", Protocol: protocolVNC, ViaSSH: true})
	for name, value := range map[string]string{
		hostSecretName(host.CatalogID, vncSystemPasswordPart("pi")): "system password",
		hostSecretName(host.CatalogID, hostPasswordPart):            "legacy password",
	} {
		if err := state.secrets.vault.Set(name, value); err != nil {
			t.Fatal(err)
		}
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	conn := session.conn.(*desktopConn)
	inner := conn.inner.(*fakeConn)
	inner.set(func(status *sshconn.Status) { status.Phase = sshconn.PhaseConnected })
	conn.innerChanged()
	waitUntil(t, state, "VNC server identity over SSH", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyTrust)
	waitUntil(t, state, "VNC system account over SSH", func() bool { return session.desk != nil })
	if got := <-answers; got != [3]string{"ssh-user", "pi", "system password"} {
		t.Fatalf("mixed the SSH and VNC accounts: %q", got)
	}
	if state.modal == modalPassword || session.host.User != "ssh-user" {
		t.Fatal("saved VNC login changed the tunnel's SSH account or needed confirmation")
	}
}
