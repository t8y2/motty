package sshconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"errors"
	"os"
	"path/filepath"
	"strconv"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

func TestIdentitiesOnlyFiltersAgentKeysAndUsesConfiguredPublicIdentity(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "identity")
	if err := os.WriteFile(path+".pub", ssh.MarshalAuthorizedKey(signer.PublicKey()), 0o600); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name                                   string
		only, configured, encrypted, connected bool
	}{
		{"unrestricted agent", false, false, false, true},
		{"unrelated agent excluded", true, false, false, false},
		{"public identity selects agent", true, true, false, true},
		{"agent avoids private key passphrase", true, true, true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			if test.encrypted {
				block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "test", []byte("secret"))
				if err != nil {
					t.Fatal(err)
				}
				if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
					t.Fatal(err)
				}
			}
			keyring := agent.NewKeyring()
			if err := keyring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
				t.Fatal(err)
			}
			server := startServer(t, func(server *testServer) { server.authorized = []ssh.PublicKey{signer.PublicKey()} })
			harness := dial(t, server, func(config *Config) {
				host, port := server.host()
				auth := Authentication{Methods: AuthPublicKey, IdentityFiles: []string{}, Agent: true, IdentitiesOnly: test.only}
				if test.configured {
					auth.IdentityFiles = []string{path}
				}
				config.Authentication = map[Endpoint]Authentication{{Host: host, Port: port, User: config.User}: auth}
			})
			harness.phase(PhaseHostKeyPrompt)
			harness.conn.agentmu.Lock()
			harness.conn.agentClient = keyring.(agent.ExtendedAgent)
			harness.conn.agentmu.Unlock()
			harness.conn.TrustHostKey(false)
			status := harness.until("authentication result without a key passphrase", func(status Status) bool {
				return status.Phase == PhaseConnected || status.Phase == PhaseFailed || status.Phase == PhaseKeyPassphrasePrompt
			})
			if test.connected {
				if status.Phase != PhaseConnected || status.Method != "publickey" {
					t.Fatalf("status = %+v", status)
				}
			} else if status.Phase != PhaseFailed || !errors.Is(status.Err, ErrAuthFailed) {
				t.Fatalf("unconfigured agent key was not excluded: %+v", status)
			}
		})
	}
}

func TestServerAliveCountClosesUnresponsiveTransport(t *testing.T) {
	for _, count := range []int{0, 2} {
		t.Run(strconv.Itoa(count), func(t *testing.T) {
			var requests atomic.Int32
			server := startServer(t, func(server *testServer) {
				server.password = "secret"
				server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
					if request.Type == keepAliveRequest {
						requests.Add(1)
					}
				}
			})
			harness := dial(t, server, func(config *Config) {
				config.KeepAlive = KeepAliveConfig{Interval: 30 * time.Millisecond, CountMax: &count}
			})
			harness.phase(PhaseHostKeyPrompt)
			harness.conn.TrustHostKey(false)
			harness.phase(PhasePasswordPrompt)
			harness.conn.SubmitPassword("secret")
			harness.phase(PhaseConnected)
			status := harness.phase(PhaseClosed)
			if !errors.Is(status.Err, ErrKeepAliveTimeout) || !errors.Is(status.Err, ErrDisconnected) {
				t.Fatalf("status = %+v", status)
			}
			if count == 0 && requests.Load() != 0 || count > 0 && requests.Load() == 0 {
				t.Fatalf("count=%d requests=%d", count, requests.Load())
			}
		})
	}
}

func TestStoredIdentityCanSelectAMatchingAgentKey(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "stored")
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private}); err != nil {
		t.Fatal(err)
	}
	conn := &Conn{config: Config{StoredKey: func(string) ([]byte, error) { return pem.EncodeToMemory(block), nil }}, agentClient: keyring.(agent.ExtendedAgent)}
	signers, err := conn.publicKeySigners(Authentication{IdentityFiles: []string{StoredKeyPrefix + "configured"}, Agent: true, IdentitiesOnly: true})
	if err != nil || len(signers) != 1 || len(conn.Status().Skipped) != 0 {
		t.Fatalf("stored agent identity: signers=%d err=%v skipped=%v", len(signers), err, conn.Status().Skipped)
	}
}

func TestServerAliveCountAcceptsNegativeRepliesAndReceivingTraffic(t *testing.T) {
	for _, reply := range []bool{true, false} {
		t.Run(map[bool]string{true: "replies", false: "other traffic"}[reply], func(t *testing.T) {
			connections := make(chan *ssh.ServerConn, 1)
			server := startServer(t, func(server *testServer) {
				server.password = "secret"
				server.connected = func(connection *ssh.ServerConn) { connections <- connection }
				server.request = func(_ *ssh.ServerConn, request *ssh.Request) {
					if reply {
						_ = request.Reply(false, nil)
					}
				}
			})
			count := 2
			harness := dial(t, server, func(config *Config) {
				config.KeepAlive = KeepAliveConfig{Interval: 100 * time.Millisecond, CountMax: &count}
			})
			harness.phase(PhaseHostKeyPrompt)
			harness.conn.TrustHostKey(false)
			harness.phase(PhasePasswordPrompt)
			harness.conn.SubmitPassword("secret")
			harness.phase(PhaseConnected)
			connection := <-connections
			for range 50 {
				if !reply {
					if _, _, err := connection.SendRequest("test-traffic", false, nil); err != nil {
						t.Fatal(err)
					}
				}
				time.Sleep(10 * time.Millisecond)
			}
			if status := harness.conn.Status(); status.Phase != PhaseConnected {
				t.Fatalf("responsive transport closed: %+v", status)
			}
		})
	}
}
