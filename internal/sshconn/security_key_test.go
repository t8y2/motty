package sshconn

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"crypto/sha256"
	"encoding/binary"
	"encoding/pem"
	"errors"
	"net"
	"os"
	"path/filepath"
	"testing"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// securityKeyPublic builds the public half of an sk-* identity around a
// software key, so the test can sign without a device.
func securityKeyPublic(t *testing.T, public ed25519.PublicKey) ssh.PublicKey {
	t.Helper()
	blob := ssh.Marshal(struct {
		Type        string
		Key         []byte
		Application string
	}{ssh.KeyAlgoSKED25519, public, "ssh:"})
	key, err := ssh.ParsePublicKey(blob)
	if err != nil {
		t.Fatal(err)
	}
	return key
}

// securityKeyPrivateFile writes the OpenSSH container ssh-keygen leaves for an
// sk key: the public half is in the clear, the private half is a device handle.
func securityKeyPrivateFile(t *testing.T, public ssh.PublicKey) string {
	t.Helper()
	stringField := func(value []byte) []byte {
		out := make([]byte, 4+len(value))
		binary.BigEndian.PutUint32(out, uint32(len(value)))
		copy(out[4:], value)
		return out
	}
	body := []byte("openssh-key-v1\x00")
	body = append(body, stringField([]byte("none"))...)
	body = append(body, stringField([]byte("none"))...)
	body = append(body, stringField(nil)...)
	body = append(body, 0, 0, 0, 1)
	body = append(body, stringField(public.Marshal())...)
	body = append(body, stringField([]byte("handle"))...)
	path := filepath.Join(t.TempDir(), "id_ed25519_sk")
	data := pem.EncodeToMemory(&pem.Block{Type: "OPENSSH PRIVATE KEY", Bytes: body})
	if err := os.WriteFile(path, data, 0o600); err != nil {
		t.Fatal(err)
	}
	return path
}

// testSecurityKeyAgent serves one sk identity and signs the way a hardware
// authenticator would: the signature covers the application and message
// digests plus the user-presence flag and counter.
type testSecurityKeyAgent struct {
	public  ssh.PublicKey
	private ed25519.PrivateKey
}

func (test *testSecurityKeyAgent) List() ([]*agent.Key, error) {
	return []*agent.Key{{Format: test.public.Type(), Blob: test.public.Marshal(), Comment: "test"}}, nil
}

func (test *testSecurityKeyAgent) Sign(key ssh.PublicKey, data []byte) (*ssh.Signature, error) {
	if !bytes.Equal(key.Marshal(), test.public.Marshal()) {
		return nil, errors.New("unknown key")
	}
	application := sha256.Sum256([]byte("ssh:"))
	message := sha256.Sum256(data)
	blob := ssh.Marshal(struct {
		Application []byte `ssh:"rest"`
		Flags       byte
		Counter     uint32
		Message     []byte `ssh:"rest"`
	}{application[:], 0x01, 0, message[:]})
	signature := ed25519.Sign(test.private, blob)
	return &ssh.Signature{
		Format: ssh.KeyAlgoSKED25519,
		Blob: ssh.Marshal(struct {
			Signature []byte `ssh:"rest"`
		}{signature}),
		Rest: ssh.Marshal(struct {
			Flags   byte
			Counter uint32
		}{0x01, 0}),
	}, nil
}

func (test *testSecurityKeyAgent) Signers() ([]ssh.Signer, error) { return nil, nil }
func (test *testSecurityKeyAgent) Add(agent.AddedKey) error       { return nil }
func (test *testSecurityKeyAgent) Remove(ssh.PublicKey) error     { return nil }
func (test *testSecurityKeyAgent) RemoveAll() error               { return nil }
func (test *testSecurityKeyAgent) Lock([]byte) error              { return nil }
func (test *testSecurityKeyAgent) Unlock([]byte) error            { return nil }

func startSecurityKeyAgent(t *testing.T, public ssh.PublicKey, private ed25519.PrivateKey) string {
	t.Helper()
	dir, err := os.MkdirTemp("", "motty-sk-")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "agent.sock")
	listener, err := net.Listen("unix", path)
	if err != nil {
		os.RemoveAll(dir)
		t.Fatal(err)
	}
	t.Cleanup(func() {
		listener.Close()
		os.RemoveAll(dir)
	})
	testAgent := &testSecurityKeyAgent{public: public, private: private}
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_ = agent.ServeAgent(testAgent, connection)
			}()
		}
	}()
	return path
}

func TestSecurityKeyIdentityUsesThePreparedAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("starts an ssh-agent fixture")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := securityKeyPublic(t, private.Public().(ed25519.PublicKey))
	keyPath := securityKeyPrivateFile(t, public)
	agentPath := startSecurityKeyAgent(t, public, private)
	server := startServer(t, func(server *testServer) { server.authorized = []ssh.PublicKey{public} })

	prepared, released := 0, 0
	harness := dial(t, server, func(config *Config) {
		config.IdentityFiles = []string{keyPath}
		config.SecurityKeyAgent = func(request SecurityKeyRequest) (string, func(), error) {
			prepared++
			if request.Path != keyPath {
				t.Errorf("request path = %q, want %q", request.Path, keyPath)
			}
			if request.PublicKey == nil || !bytes.Equal(request.PublicKey.Marshal(), public.Marshal()) {
				t.Errorf("request key = %v, want the sk key", request.PublicKey)
			}
			return agentPath, func() { released++ }, nil
		}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	connected := harness.phase(PhaseConnected)
	if connected.Method != "publickey" {
		t.Fatalf("method = %q, want publickey", connected.Method)
	}
	if prepared != 1 {
		t.Fatalf("prepared %d agents, want 1", prepared)
	}
	harness.conn.Close()
	if released != 1 {
		t.Fatalf("released %d agents, want 1", released)
	}
}

func TestSecurityKeyWithoutPreparationIsSkippedWithItsReason(t *testing.T) {
	if testing.Short() {
		t.Skip("starts an ssh-agent fixture")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := securityKeyPublic(t, private.Public().(ed25519.PublicKey))
	keyPath := securityKeyPrivateFile(t, public)
	other := newSigner(t)
	server := startServer(t, func(server *testServer) {
		server.authorized = []ssh.PublicKey{other.PublicKey()}
		server.password = "pw"
	})
	harness := dial(t, server, func(config *Config) {
		config.IdentityFiles = []string{keyPath}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	status := harness.phase(PhasePasswordPrompt)
	if len(status.Skipped) != 1 {
		t.Fatalf("skipped = %q, want one reason", status.Skipped)
	}
	if !bytes.Contains([]byte(status.Skipped[0]), []byte("security key")) {
		t.Fatalf("skipped = %q, want the security-key reason", status.Skipped)
	}
}

func TestRejectedSecurityKeyDoesNotPrepareAnAgent(t *testing.T) {
	if testing.Short() {
		t.Skip("starts an ssh-agent fixture")
	}
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public := securityKeyPublic(t, private.Public().(ed25519.PublicKey))
	keyPath := securityKeyPrivateFile(t, public)
	other := newSigner(t)
	server := startServer(t, func(server *testServer) {
		server.authorized = []ssh.PublicKey{other.PublicKey()}
		server.password = "pw"
	})
	prepared := 0
	harness := dial(t, server, func(config *Config) {
		config.IdentityFiles = []string{keyPath}
		config.SecurityKeyAgent = func(SecurityKeyRequest) (string, func(), error) {
			prepared++
			return "", nil, errors.New("not prepared for a rejected key")
		}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	if prepared != 0 {
		t.Fatalf("prepared %d agents for a key the server refused", prepared)
	}
}
