package workbench

import (
	"context"
	"errors"
	"os"
	"path/filepath"
	"testing"

	"motty/internal/desktop"
	"motty/internal/desktop/pins"
	"motty/internal/sshconn"
)

func TestVNCTLSCertificateTrustSurvivesRestart(t *testing.T) {
	certificate := []byte("TLS certificate")
	open := func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.Verify("pi.example.com:5900", certificate); err != nil {
			return nil, err
		}
		return &encryptedVNC{newPictureOnly()}, nil
	}
	state, host := realVNCState(t, "alice", open)
	if err := state.saveWithResult(); err != nil {
		t.Fatal(err)
	}
	options := state.options
	openHost(state)
	waitUntil(t, state, "TLS identity question", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyTrust)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "TLS desktop", func() bool { return session.desk != nil })
	state.Close()

	restarted := newTestState(t, options)
	restarted.connectionReconnect, restarted.dialDesktop = reconnectOff, open
	saved, ok := restarted.hostByCatalogID(host.CatalogID)
	if !ok {
		t.Fatal("saved host missing after restart")
	}
	restarted.selectedHostID = saved.ID
	restarted.Click(hitHosts)
	openHost(restarted)
	reconnected, _ := restarted.sessionByIDPointer(restarted.activeSessionID)
	waitUntil(t, restarted, "TLS desktop after restart", func() bool { return reconnected.desk != nil })
	if restarted.modal == modalHostKey {
		t.Fatal("saved TLS certificate prompted again")
	}
	previous := pins.Fingerprint(certificate)
	certificate = []byte("changed TLS certificate")
	reconnected.desk.session.Close()
	restarted.reconnect(reconnected)
	waitUntil(t, restarted, "changed TLS certificate", func() bool { return reconnected.phase == sshconn.PhaseFailed })
	if !errors.Is(reconnected.err, sshconn.ErrHostKeyChanged) || restarted.modal == modalHostKey {
		t.Fatalf("changed TLS certificate was not refused: %v", reconnected.err)
	}
	store := pins.New(filepath.Join(filepath.Dir(options.KnownHostsPath), rdpCertificatesFile))
	if got, err := store.Lookup(pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}); err != nil || got != previous {
		t.Fatalf("refusing a change replaced the saved pin: %q, %v", got, err)
	}
}

func TestVNCTLSCertificateTrustOnceDoesNotSurviveRestart(t *testing.T) {
	open := func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.Verify("pi.example.com:5900", []byte("TLS certificate")); err != nil {
			return nil, err
		}
		return &encryptedVNC{newPictureOnly()}, nil
	}
	state, host := realVNCState(t, "alice", open)
	if err := state.saveWithResult(); err != nil {
		t.Fatal(err)
	}
	options := state.options
	openHost(state)
	waitUntil(t, state, "TLS identity question", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyOnce)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "one-time TLS desktop", func() bool { return session.desk != nil })
	state.Close()
	restarted := newTestState(t, options)
	restarted.connectionReconnect, restarted.dialDesktop = reconnectOff, open
	saved, ok := restarted.hostByCatalogID(host.CatalogID)
	if !ok {
		t.Fatal("saved host missing after restart")
	}
	restarted.selectedHostID = saved.ID
	restarted.Click(hitHosts)
	openHost(restarted)
	waitUntil(t, restarted, "TLS question after one-time trust", func() bool { return restarted.modal == modalHostKey })
	restarted.Click(hitHostKeyReject)
}

func TestVNCTLSCertificateSaveFailurePreventsAuthentication(t *testing.T) {
	for _, scenario := range []string{"write failure", "concurrent pin change"} {
		t.Run(scenario, func(t *testing.T) {
			started := make(chan struct{}, 1)
			state, _ := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				if err := request.Verify("pi.example.com:5900", []byte("TLS certificate")); err != nil {
					return nil, err
				}
				started <- struct{}{}
				return &encryptedVNC{newPictureOnly()}, nil
			})
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "TLS identity question", func() bool { return state.modal == modalHostKey })
			path := filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile)
			store := pins.New(path)
			endpoint := pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}
			previous := ""
			if scenario == "write failure" {
				if err := os.Mkdir(path+".tmp", 0o700); err != nil {
					t.Fatal(err)
				}
			} else {
				previous = pins.Fingerprint([]byte("another certificate"))
				if err := store.Remember(endpoint, previous, ""); err != nil {
					t.Fatal(err)
				}
			}
			state.Click(hitHostKeyTrust)
			waitUntil(t, state, "failed TLS identity save", func() bool { return session.phase == sshconn.PhaseFailed })
			if scenario == "write failure" {
				var pathError *os.PathError
				if !errors.As(session.err, &pathError) {
					t.Fatalf("identity save lost the storage error: %v", session.err)
				}
			}
			select {
			case <-started:
				t.Fatal("authentication started after failed identity save")
			default:
			}
			if got, err := store.Lookup(endpoint); err != nil || got != previous {
				t.Fatalf("failed save changed the pin: %q, %v", got, err)
			}
		})
	}
}
