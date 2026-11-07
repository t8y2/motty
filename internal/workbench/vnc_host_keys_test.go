package workbench

import (
	"context"
	"errors"
	"net"
	"slices"
	"strconv"
	"testing"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
	"motty/internal/desktop/rdp"
	"motty/internal/sshconn"
)

func TestVNCIdentityAndAccountPasswordSurviveBackupAndRestart(t *testing.T) {
	key := []byte("VNC identity")
	answers := make(chan [2]string, 3)
	open := func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.VerifyServerKey("pi.example.com:5900", key); err != nil {
			return nil, err
		}
		user, password, err := request.AskCredentials()
		if err != nil {
			return nil, err
		}
		answers <- [2]string{user, password}
		return &encryptedVNC{newPictureOnly()}, nil
	}
	source, host := realVNCState(t, "alice", open)
	openHost(source)
	waitUntil(t, source, "initial identity", func() bool { return source.modal == modalHostKey })
	source.Click(hitHostKeyTrust)
	waitUntil(t, source, "initial password", func() bool { return source.modal == modalPassword })
	source.Click(hitPasswordSave)
	source.passwordFields[0] = *text.NewEditor("account password")
	source.Click(hitPasswordSubmit)
	session, _ := source.sessionByIDPointer(source.activeSessionID)
	waitUntil(t, source, "source desktop", func() bool { return session.desk != nil })
	if got := <-answers; got != [2]string{"alice", "account password"} {
		t.Fatalf("source credentials = %q", got)
	}
	document, err := source.backupDocument()
	if err != nil {
		t.Fatal(err)
	}
	source.Close()

	options := testOptions(t)
	options.Invalidate = func() {}
	target := newTestState(t, options)
	if _, err := target.applyBackup(document); err != nil {
		t.Fatal(err)
	}
	for _, state := range []*State{target, nil} {
		if state == nil {
			state = newTestState(t, options)
		}
		state.connectionReconnect, state.dialDesktop = reconnectOff, open
		saved, ok := state.hostByCatalogID(host.CatalogID)
		if !ok || saved.vncFingerprint("pi.example.com:5900", vncServerKeyType) != rdp.Fingerprint(key) {
			t.Fatal("restoring the host lost its identity")
		}
		state.selectedHostID = saved.ID
		state.Click(hitHosts)
		openHost(state)
		reconnected, _ := state.sessionByIDPointer(state.activeSessionID)
		waitUntil(t, state, "restored desktop", func() bool { return reconnected.desk != nil })
		if state.modal == modalHostKey || state.modal == modalPassword {
			t.Fatal("a saved identity or account password was lost")
		}
		if got := <-answers; got != [2]string{"alice", "account password"} {
			t.Fatalf("restored credentials = %q", got)
		}
		state.closeSession(reconnected.ID)
		state.Close()
	}
}

func TestVNCHostIdentityCopyAddressChangeAndDeletion(t *testing.T) {
	key := []byte("same server identity")
	state, original := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		address := net.JoinHostPort(request.Host.Address, strconv.Itoa(int(request.Host.Port)))
		if err := request.VerifyServerKey(address, key); err != nil {
			return nil, err
		}
		return &encryptedVNC{newPictureOnly()}, nil
	})
	openHost(state)
	waitUntil(t, state, "original identity", func() bool { return state.modal == modalHostKey })
	state.Click(hitHostKeyTrust)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "original desktop", func() bool { return session.desk != nil })
	state.closeSession(session.ID)
	state.selectedHostID = original.ID
	clickHostAction(state, hitCopyHost)
	state.editor.label = *text.NewEditor("Pi copy")
	state.editor.user = *text.NewEditor("bob")
	state.Click(hitDialogSubmit)
	if state.modal != modalNone {
		t.Fatalf("copy failed: %q", state.dialogError)
	}
	copied := savedHostNamed(t, state, "Pi copy")
	source, _ := state.hostByCatalogID(original.CatalogID)
	if copied.CatalogID == source.CatalogID || !slices.Equal(copied.VNCKeys, source.VNCKeys) {
		t.Fatal("copy did not preserve an independently owned identity")
	}
	copied.VNCKeys[0].Fingerprint = "changed copy"
	if source.VNCKeys[0].Fingerprint != rdp.Fingerprint(key) {
		t.Fatal("copy shares mutable identity storage with its source")
	}
	// Edit through the actual form: preserving a pin must not reuse it at a
	// different target, even when that target presents the identical key.
	state.selectedHostID = copied.ID
	clickHostAction(state, hitEditHost)
	state.editor.address = *text.NewEditor("moved.example.com")
	state.Click(hitDialogSubmit)
	openHost(state)
	moved, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "moved host identity", func() bool { return state.modal == modalHostKey })
	if len(moved.hostKey.Known) != 0 {
		t.Fatal("a pin for another target was reused")
	}
	state.Click(hitHostKeyReject)
	waitUntil(t, state, "rejected moved identity", func() bool { return moved.phase == sshconn.PhaseFailed })
	state.closeSession(moved.ID)
	state.hostActionIDs = []uint32{original.ID}
	state.confirmDeleteHosts()
	if _, ok := state.hostByCatalogID(original.CatalogID); ok {
		t.Fatal("deleted host retained its identity record")
	}
	remaining, ok := state.hostByCatalogID(copied.CatalogID)
	if !ok || len(remaining.VNCKeys) != 1 {
		t.Fatal("deleting the source removed the copy's identity")
	}
	replacement := addStreamHost(state, Host{Label: "Replacement", Address: "pi.example.com", User: "alice", Protocol: protocolVNC})
	state.selectedHostID = replacement.ID
	openHost(state)
	waitUntil(t, state, "replacement identity", func() bool { return state.modal == modalHostKey })
	if len(state.promptSession().hostKey.Known) != 0 {
		t.Fatal("a recreated host inherited the deleted host's trust")
	}
}

func TestVNCSavingIdentityFailureNeverStartsAuthentication(t *testing.T) {
	for _, scenario := range []string{"save blocked", "host deleted", "another key saved"} {
		t.Run(scenario, func(t *testing.T) {
			key := []byte("pending identity")
			started := make(chan bool, 1)
			state, host := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				if err := request.VerifyServerKey("pi.example.com:5900", key); err != nil {
					return nil, err
				}
				started <- true
				return &encryptedVNC{newPictureOnly()}, nil
			})
			state.save()
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "pending identity", func() bool { return state.modal == modalHostKey })
			if scenario == "save blocked" {
				state.pauseSave++
				defer func() { state.pauseSave-- }()
			} else {
				other := newTestState(t, state.options)
				current, ok := other.hostByCatalogID(host.CatalogID)
				if !ok {
					t.Fatal("host absent in other window")
				}
				if scenario == "host deleted" {
					for index := len(other.sessions) - 1; index >= 0; index-- {
						if other.sessions[index].host.CatalogID == current.CatalogID {
							other.closeSession(other.sessions[index].ID)
						}
					}
					other.hostActionIDs = []uint32{current.ID}
					other.confirmDeleteHosts()
					if _, ok := other.hostByCatalogID(host.CatalogID); ok {
						t.Fatalf("host deletion failed: %q", other.dialogError)
					}
				} else {
					current.VNCKeys = []vncHostKey{{Address: "pi.example.com:5900", Kind: vncServerKeyType, Fingerprint: rdp.Fingerprint([]byte("another identity"))}}
					other.hosts[other.hostIndex(current.ID)] = current
					if err := other.saveWithResult(); err != nil {
						t.Fatal(err)
					}
				}
				other.Close()
			}
			state.Click(hitHostKeyTrust)
			waitUntil(t, state, "failed identity save", func() bool {
				current, ok := state.sessionByIDPointer(session.ID)
				return !ok || current.phase == sshconn.PhaseFailed || current.phase == sshconn.PhaseClosed
			})
			select {
			case <-started:
				t.Fatal("authentication started after saving identity failed")
			default:
			}
			if saved, ok := state.hostByCatalogID(host.CatalogID); ok && saved.vncFingerprint("pi.example.com:5900", vncServerKeyType) == rdp.Fingerprint(key) {
				t.Fatal("a failed save left the pending identity trusted")
			}
			if scenario == "save blocked" && !errors.Is(session.err, sshconn.ErrHostKeyRejected) {
				t.Fatalf("failed save = %v", session.err)
			}
		})
	}
}
