package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/text"
	"motty/internal/desktop"
	"motty/internal/desktop/pins"
	"motty/internal/sshconn"
)

func TestVNCIdentityAndAccountPasswordSurviveRestartOutsideHostRecord(t *testing.T) {
	key := []byte("VNC identity")
	answers := make(chan [2]string, 2)
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
	options := source.options
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
	encoded, err := json.Marshal(document)
	if err != nil || strings.Contains(string(encoded), "vnc_keys") || strings.Contains(string(encoded), pins.Fingerprint(key)) {
		t.Fatalf("server identity is still part of the host backup: error=%v", err)
	}
	source.Close()
	state := newTestState(t, options)
	state.connectionReconnect, state.dialDesktop = reconnectOff, open
	saved, ok := state.hostByCatalogID(host.CatalogID)
	if !ok {
		t.Fatal("host absent after restart")
	}
	state.selectedHostID = saved.ID
	state.Click(hitHosts)
	openHost(state)
	reconnected, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "restarted desktop", func() bool {
		return reconnected.desk != nil || state.modal == modalHostKey || state.modal == modalPassword
	})
	if state.modal == modalHostKey || state.modal == modalPassword {
		t.Fatal("a saved endpoint identity or account password was lost")
	}
	if got := <-answers; got != [2]string{"alice", "account password"} {
		t.Fatalf("restarted credentials = %q", got)
	}
}

func TestVNCHostsShareEndpointTrustAcrossCopyDeletionAndAddressChange(t *testing.T) {
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
	if copied.CatalogID == original.CatalogID {
		t.Fatal("copy did not get its own catalog identity")
	}
	state.selectedHostID = copied.ID
	openHost(state)
	copySession, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "copy reuses endpoint trust", func() bool { return copySession.desk != nil || state.modal == modalHostKey })
	if state.modal == modalHostKey {
		t.Fatal("another catalog host asked again for the same endpoint")
	}
	state.closeSession(copySession.ID)
	state.hostActionIDs = []uint32{original.ID}
	state.confirmDeleteHosts()
	if _, ok := state.hostByCatalogID(original.CatalogID); ok {
		t.Fatal("original host was not deleted")
	}
	state.selectedHostID = copied.ID
	openHost(state)
	afterDelete, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "trust survives host deletion", func() bool { return afterDelete.desk != nil || state.modal == modalHostKey })
	if state.modal == modalHostKey {
		t.Fatal("deleting a catalog host discarded endpoint trust")
	}
	state.closeSession(afterDelete.ID)
	state.selectedHostID = copied.ID
	clickHostAction(state, hitEditHost)
	state.editor.address = *text.NewEditor("moved.example.com")
	state.Click(hitDialogSubmit)
	openHost(state)
	moved, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "moved host identity", func() bool { return state.modal == modalHostKey })
	if len(moved.hostKey.Known) != 0 {
		t.Fatal("a pin for another endpoint was reused")
	}
	state.Click(hitHostKeyReject)
}

func TestVNCSavingEndpointIdentityFailureNeverStartsAuthentication(t *testing.T) {
	for _, scenario := range []string{"write failure", "another key saved"} {
		t.Run(scenario, func(t *testing.T) {
			key := []byte("pending identity")
			started := make(chan bool, 1)
			state, _ := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
				if err := request.VerifyServerKey("pi.example.com:5900", key); err != nil {
					return nil, err
				}
				started <- true
				return &encryptedVNC{newPictureOnly()}, nil
			})
			openHost(state)
			session, _ := state.sessionByIDPointer(state.activeSessionID)
			waitUntil(t, state, "pending identity", func() bool { return state.modal == modalHostKey })
			path := filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile)
			store := pins.New(path)
			endpoint := pins.Endpoint{Protocol: pins.VNC, Address: "pi.example.com:5900"}
			other := pins.Fingerprint([]byte("another identity"))
			if scenario == "write failure" {
				if err := os.Mkdir(path+".tmp", 0o700); err != nil {
					t.Fatal(err)
				}
			} else if err := store.Remember(endpoint, other, ""); err != nil {
				t.Fatal(err)
			}
			state.Click(hitHostKeyTrust)
			waitUntil(t, state, "failed pin save", func() bool { return session.phase == sshconn.PhaseFailed })
			select {
			case <-started:
				t.Fatal("authentication started after saving identity failed")
			default:
			}
			got, err := store.Lookup(endpoint)
			if err != nil || got == pins.Fingerprint(key) || scenario == "another key saved" && got != other {
				t.Fatalf("a failed save changed trust: %q, %v", got, err)
			}
			if scenario == "another key saved" && !errors.Is(session.err, pins.ErrChanged) {
				t.Fatalf("concurrent identity change lost its cause: %v", session.err)
			}
		})
	}
}

func TestTemporaryVNCConnectionCanSaveEndpointIdentity(t *testing.T) {
	store := pins.New(filepath.Join(t.TempDir(), "pins"))
	host := Host{Address: "temporary.example.com", Port: 5900, Protocol: protocolVNC}
	notified := make(chan struct{}, 8)
	conn := newDesktopConn(host, func() { notified <- struct{}{} }, nil, 0, nil, store)
	t.Cleanup(conn.Cancel)
	key := []byte("temporary server identity")
	result := make(chan error, 1)
	go func() { result <- conn.verifyServerKey("temporary.example.com:5900", key) }()
	select {
	case <-notified:
	case <-time.After(5 * time.Second):
		t.Fatal("temporary server did not prompt")
	}
	conn.TrustHostKey(true)
	if err := <-result; err != nil {
		t.Fatal(err)
	}
	got, err := store.Lookup(pins.Endpoint{Protocol: pins.VNC, Address: "temporary.example.com:5900"})
	if err != nil || got != pins.Fingerprint(key) {
		t.Fatalf("temporary host could not save trust: %q, %v", got, err)
	}
}

func TestVNCUnreadablePinStoreDoesNotPromptOrStartAuthentication(t *testing.T) {
	started := make(chan bool, 1)
	state, _ := realVNCState(t, "alice", func(ctx context.Context, request desktopRequest) (desktop.Session, error) {
		if err := request.VerifyServerKey("pi.example.com:5900", []byte("key")); err != nil {
			return nil, err
		}
		started <- true
		return &encryptedVNC{newPictureOnly()}, nil
	})
	path := filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile)
	if err := os.Mkdir(path, 0o700); err != nil {
		t.Fatal(err)
	}
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "unreadable pins fail closed", func() bool { return session.phase == sshconn.PhaseFailed })
	if state.modal == modalHostKey || !strings.Contains(session.err.Error(), path) {
		t.Fatalf("unreadable pins were hidden or prompted for replacement: %v", session.err)
	}
	select {
	case <-started:
		t.Fatal("unreadable trust store allowed authentication")
	default:
	}
}
