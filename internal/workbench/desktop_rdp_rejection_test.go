package workbench

import (
	"errors"
	"path/filepath"
	"strings"
	"testing"

	"motty/internal/desktop/pins"
	"motty/internal/sshconn"
)

func TestRDPRejectedChangedCertificateCanBeForgottenFromFailurePage(t *testing.T) {
	for _, rejectFirst := range []bool{false, true} {
		name := "forget-from-dialog"
		if rejectFirst {
			name = "reject-then-forget"
		}
		t.Run(name, func(t *testing.T) {
			certificate := []byte("original certificate")
			var opened []desktopRequest
			state, session := rdpState(t, &certificate, &opened)
			waitUntil(t, state, "original certificate prompt", func() bool { return state.modal == modalHostKey })
			state.Click(hitHostKeyTrust)
			waitUntil(t, state, "original RDP picture", func() bool { return session.desk != nil })
			store := pins.New(filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile))
			endpoint := pins.Endpoint{Protocol: pins.RDP, Address: "win.example.com:3389"}
			previous := pins.Fingerprint(certificate)
			certificate = []byte("replacement certificate")
			session.desk.session.Close()
			state.reconnect(session)
			waitUntil(t, state, "replacement certificate prompt", func() bool { return state.modal == modalHostKey })
			if rejectFirst {
				state.Click(hitHostKeyReject)
				waitUntil(t, state, "rejected replacement failure", func() bool { return session.phase == sshconn.PhaseFailed })
				if !errors.Is(session.err, sshconn.ErrHostKeyRejected) || session.desk != nil {
					t.Fatalf("rejecting a changed certificate did not stop the connection: %v", session.err)
				}
				if got, err := store.Lookup(endpoint); err != nil || got != previous {
					t.Fatalf("rejecting replaced the saved identity: %q, %v", got, err)
				}
				if diagnostic := session.term.screen.Text(); !strings.Contains(diagnostic, previous) || !strings.Contains(diagnostic, pins.Fingerprint(certificate)) {
					t.Fatalf("changed-certificate rejection lost the known or presented fingerprint: %q", diagnostic)
				}
				canvas, _ := paintedTexts(state)
				if _, ok := hitByID(canvas, hitConnForget); !ok {
					t.Fatal("rejected changed certificate has no forget action on the failure page")
				}
				state.Click(hitConnForget)
			} else {
				state.Click(hitHostKeyForget)
				waitUntil(t, state, "forgotten replacement handshake ends", func() bool { return session.phase == sshconn.PhaseFailed })
			}
			if got, err := store.Lookup(endpoint); err != nil || got != "" {
				t.Fatalf("forgetting retained the saved identity: %q, %v", got, err)
			}
			if len(session.hostKey.Known) != 0 || session.desk != nil {
				t.Fatal("forgetting restored the old identity or accepted the current handshake")
			}
			state.Click(hitConnRetry)
			waitUntil(t, state, "forgotten replacement prompts again", func() bool { return state.modal == modalHostKey })
			if len(session.hostKey.Known) != 0 || session.desk != nil {
				t.Fatal("retry bypassed the replacement identity prompt")
			}
			state.Click(hitHostKeyTrust)
			waitUntil(t, state, "explicitly trusted replacement picture", func() bool { return session.desk != nil })
			if got, err := store.Lookup(endpoint); err != nil || got != pins.Fingerprint(certificate) {
				t.Fatalf("explicit trust did not save the replacement identity: %q, %v", got, err)
			}
		})
	}
}

func TestRDPRejectedNewCertificateDoesNotOfferForget(t *testing.T) {
	certificate := []byte("unknown certificate")
	var opened []desktopRequest
	state, session := rdpState(t, &certificate, &opened)
	waitUntil(t, state, "unknown certificate prompt", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyReject)
	waitUntil(t, state, "unknown certificate rejected", func() bool { return session.phase == sshconn.PhaseFailed })
	if !errors.Is(session.err, sshconn.ErrHostKeyRejected) || len(session.hostKey.Known) != 0 || session.desk != nil {
		t.Fatalf("unknown certificate rejection acquired trust or became a changed identity: %+v", session.hostKey)
	}
	canvas, _ := paintedTexts(state)
	if _, ok := hitByID(canvas, hitConnForget); ok {
		t.Fatal("unknown rejected certificate offers to forget an identity that was never saved")
	}
	conn := session.conn.(*desktopConn)
	conn.mu.Lock()
	trusted := len(conn.trusted)
	conn.mu.Unlock()
	if trusted != 0 {
		t.Fatal("rejecting an unknown certificate accepted it for this connection")
	}
}
