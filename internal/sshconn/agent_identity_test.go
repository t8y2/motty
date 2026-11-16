package sshconn

import (
	"errors"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// A server ends authentication once failed key offers reach MaxAuthTries. An
// agent with more keys than that budget must not hide the identity the user
// configured: IdentitiesOnly offers the agent only the keys that match the
// configured identities, so the configured key is still reached.
func TestAgentKeysDoNotExhaustTheServerBeforeAConfiguredIdentity(t *testing.T) {
	_, private := newPrivate(t)
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, pemEncode(block), 0o600); err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	for range 10 {
		_, unrelated := newPrivate(t)
		if err := keyring.Add(agent.AddedKey{PrivateKey: unrelated}); err != nil {
			t.Fatal(err)
		}
	}
	for _, test := range []struct {
		name           string
		identitiesOnly bool
		connected      bool
	}{
		{"agent keys spend the budget first", false, false},
		{"configured identities only", true, true},
	} {
		t.Run(test.name, func(t *testing.T) {
			server := startServer(t, func(server *testServer) {
				server.authorized = []ssh.PublicKey{signer.PublicKey()}
				server.maxAuthTries = 2
			})
			harness := dial(t, server, func(config *Config) {
				host, port := server.host()
				config.Authentication = map[Endpoint]Authentication{{Host: host, Port: port, User: config.User}: {
					Methods:        AuthPublicKey,
					IdentityFiles:  []string{path},
					Agent:          true,
					IdentitiesOnly: test.identitiesOnly,
				}}
			})
			harness.phase(PhaseHostKeyPrompt)
			harness.conn.agentmu.Lock()
			harness.conn.agentClient = keyring.(agent.ExtendedAgent)
			harness.conn.agentmu.Unlock()
			harness.conn.TrustHostKey(false)
			status := harness.until("authentication result", func(status Status) bool {
				return status.Phase == PhaseConnected || status.Phase == PhaseFailed
			})
			if test.connected {
				if status.Phase != PhaseConnected || status.Method != "publickey" {
					t.Fatalf("status = %+v", status)
				}
				return
			}
			if status.Phase != PhaseFailed ||
				!errors.Is(status.Err, ErrTooManyAuthAttempts) ||
				!errors.Is(status.Err, ErrAuthFailed) {
				t.Fatalf("status = %+v", status)
			}
		})
	}
}
