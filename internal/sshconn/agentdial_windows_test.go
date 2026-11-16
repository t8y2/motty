package sshconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"fmt"
	"os"
	"testing"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh/agent"
)

// The agent of a Windows password manager is a named pipe, which a Unix-socket
// dial cannot reach. Listing keys and then signing are consecutive requests on
// one pipe, the shape that stalls when reads and writes are not overlapped.
func TestAgentOverANamedPipe(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private, Comment: "pipe-key"}); err != nil {
		t.Fatal(err)
	}
	address := fmt.Sprintf(`\\.\pipe\motty-agent-test-%d`, os.Getpid())
	listener, err := winio.ListenPipe(address, nil)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		for {
			connection, err := listener.Accept()
			if err != nil {
				return
			}
			go func() {
				defer connection.Close()
				_ = agent.ServeAgent(keyring, connection)
			}()
		}
	}()

	link, err := dialAgent(address)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	signers, err := agent.NewClient(link).Signers()
	if err != nil || len(signers) != 1 {
		t.Fatalf("Signers = %v, %v", signers, err)
	}
	data := []byte("probe")
	signature, err := signers[0].Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := signers[0].PublicKey().Verify(data, signature); err != nil {
		t.Fatalf("signature from the pipe agent did not verify: %v", err)
	}
}
