package sshconn

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"
)

// startTestAgent runs a real ssh-agent (the reference x/crypto keyring) on a
// unix socket, adds one ed25519 key with the given comment, and points
// SSH_AUTH_SOCK at it.
func startTestAgent(t *testing.T, comment string) {
	t.Helper()
	path, _ := startAgent(t, comment)
	t.Setenv("SSH_AUTH_SOCK", path)
}

// startAgent is startTestAgent without touching the environment: it returns the
// socket and the public half of the key it holds.
func startAgent(t *testing.T, comment string) (string, ssh.PublicKey) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private, Comment: comment}); err != nil {
		t.Fatal(err)
	}
	dir, err := os.MkdirTemp("", "motty-agent-")
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
	return path, signer.PublicKey()
}

// queryForwardedAgent opens the agent channel of a session that asked for
// forwarding and lists the keys the client's ssh-agent holds, as a remote
// ssh-add -l would. The comments land on the fake shell for the test to read.
func (server *testServer) queryForwardedAgent(conn ssh.Conn, shell *fakeShell) {
	server.group.Add(1)
	go func() {
		defer server.group.Done()
		channel, requests, err := conn.OpenChannel("auth-agent@openssh.com", nil)
		if err != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		defer channel.Close()
		comments, err := listAgentIdentities(channel)
		if err != nil {
			return
		}
		shell.mu.Lock()
		shell.agentComments = comments
		shell.mu.Unlock()
	}()
}

// listAgentIdentities speaks one agent REQUEST_IDENTITIES round over rw.
func listAgentIdentities(rw io.ReadWriter) (comments []string, err error) {
	request := []byte{11} // REQUEST_IDENTITIES
	framed := make([]byte, 4+len(request))
	binary.BigEndian.PutUint32(framed, uint32(len(request)))
	copy(framed[4:], request)
	if _, err := rw.Write(framed); err != nil {
		return nil, err
	}
	header := make([]byte, 4)
	if _, err := io.ReadFull(rw, header); err != nil {
		return nil, err
	}
	length := binary.BigEndian.Uint32(header)
	if length == 0 || length > 1<<20 {
		return nil, errors.New("agent reply has a bad length")
	}
	body := make([]byte, length)
	if _, err := io.ReadFull(rw, body); err != nil {
		return nil, err
	}
	if body[0] != 12 { // AGENT_IDENTITIES_ANSWER
		return nil, fmt.Errorf("agent reply type %d", body[0])
	}
	rest := body[1:]
	if len(rest) < 4 {
		return nil, errors.New("agent reply is short")
	}
	count := binary.BigEndian.Uint32(rest[:4])
	rest = rest[4:]
	if count > 1024 {
		return nil, errors.New("agent lists too many keys")
	}
	for i := uint32(0); i < count; i++ {
		_, rest, err = takeAgentString(rest)
		if err != nil {
			return nil, err
		}
		var comment []byte
		comment, rest, err = takeAgentString(rest)
		if err != nil {
			return nil, err
		}
		comments = append(comments, string(comment))
	}
	return comments, nil
}

func takeAgentString(data []byte) (value, rest []byte, err error) {
	if len(data) < 4 {
		return nil, nil, errors.New("agent reply is short")
	}
	length := binary.BigEndian.Uint32(data[:4])
	if uint64(length) > uint64(len(data[4:])) {
		return nil, nil, errors.New("agent string overruns the reply")
	}
	return data[4 : 4+length], data[4+length:], nil
}

func TestAgentForwardingServesLocalAgent(t *testing.T) {
	startTestAgent(t, "forwarded-key@e2e")
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.agentForward = true
		server.shell = func(shell *fakeShell) { shell.exit(0) }
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)

	shell, err := harness.conn.OpenShell(ShellConfig{Cols: 80, Rows: 24, AgentForward: true})
	if err != nil {
		t.Fatalf("open shell: %v", err)
	}
	defer shell.Close()
	if note := shell.AgentForwardNote(); note != "" {
		t.Fatalf("agent forwarding note: %q", note)
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		server.mu.Lock()
		last := server.lastShell
		server.mu.Unlock()
		if last != nil {
			last.mu.Lock()
			comments := append([]string(nil), last.agentComments...)
			last.mu.Unlock()
			for _, comment := range comments {
				if comment == "forwarded-key@e2e" {
					return // the remote side listed the local agent's key
				}
			}
		}
		time.Sleep(10 * time.Millisecond)
	}
	t.Fatal("the server never saw the forwarded agent's key")
}

func TestAgentForwardingNotesMissingAgent(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.shell = func(shell *fakeShell) { shell.exit(0) }
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)

	shell, err := harness.conn.OpenShell(ShellConfig{Cols: 80, Rows: 24, AgentForward: true})
	if err != nil {
		t.Fatalf("open shell: %v", err)
	}
	defer shell.Close()
	if note := shell.AgentForwardNote(); note == "" {
		t.Fatal("without a running agent the shell must carry a note")
	}
}
