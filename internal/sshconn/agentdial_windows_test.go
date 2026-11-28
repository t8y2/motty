package sshconn

import (
	"bytes"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/binary"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"github.com/Microsoft/go-winio"
	"golang.org/x/crypto/ssh"
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

// Git for Windows ships an MSYS2 ssh-agent whose -a socket is a Cygwin text
// descriptor pointing at a loopback TCP port guarded by a secret and peer
// credential handshake.
func TestAgentOverACygwinSocket(t *testing.T) {
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	keyring := agent.NewKeyring()
	if err := keyring.Add(agent.AddedKey{PrivateKey: private, Comment: "cygwin-key"}); err != nil {
		t.Fatal(err)
	}
	secret := [16]byte{0x11, 0x22, 0x33, 0x44, 0x55, 0x66, 0x77, 0x88, 0x99, 0xaa, 0xbb, 0xcc, 0xdd, 0xee, 0xff, 0x01}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
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
				var gotSecret [16]byte
				if _, err := io.ReadFull(connection, gotSecret[:]); err != nil {
					return
				}
				if !bytes.Equal(gotSecret[:], secret[:]) {
					return
				}
				if _, err := connection.Write(secret[:]); err != nil {
					return
				}
				var peerCred [12]byte
				if _, err := io.ReadFull(connection, peerCred[:]); err != nil {
					return
				}
				var selfCred [12]byte
				binary.LittleEndian.PutUint32(selfCred[0:4], uint32(os.Getpid()))
				if _, err := connection.Write(selfCred[:]); err != nil {
					return
				}
				_ = agent.ServeAgent(keyring, connection)
			}()
		}
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	descriptor := fmt.Sprintf("!<socket >%d s %08x-%08x-%08x-%08x\x00",
		port,
		binary.LittleEndian.Uint32(secret[0:4]),
		binary.LittleEndian.Uint32(secret[4:8]),
		binary.LittleEndian.Uint32(secret[8:12]),
		binary.LittleEndian.Uint32(secret[12:16]),
	)
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	if err := os.WriteFile(socketPath, []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}

	link, err := dialAgent(socketPath)
	if err != nil {
		t.Fatal(err)
	}
	defer link.Close()
	signers, err := agent.NewClient(link).Signers()
	if err != nil || len(signers) != 1 {
		t.Fatalf("Signers = %v, %v", signers, err)
	}
	data := []byte("cygwin-probe")
	signature, err := signers[0].Sign(rand.Reader, data)
	if err != nil {
		t.Fatal(err)
	}
	if err := signers[0].PublicKey().Verify(data, signature); err != nil {
		t.Fatalf("signature from the Cygwin socket did not verify: %v", err)
	}
}

func TestCygwinSocketSecretMismatchFails(t *testing.T) {
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { listener.Close() })
	go func() {
		connection, err := listener.Accept()
		if err != nil {
			return
		}
		defer connection.Close()
		var got [16]byte
		_, _ = io.ReadFull(connection, got[:])
		got[0] ^= 0xff
		_, _ = connection.Write(got[:])
	}()

	port := listener.Addr().(*net.TCPAddr).Port
	socketPath := filepath.Join(t.TempDir(), "agent.sock")
	descriptor := fmt.Sprintf("!<socket >%d s 01020304-05060708-090a0b0c-0d0e0f10\x00", port)
	if err := os.WriteFile(socketPath, []byte(descriptor), 0o600); err != nil {
		t.Fatal(err)
	}
	if _, err := dialAgent(socketPath); err == nil || !strings.Contains(err.Error(), "secret mismatch") {
		t.Fatalf("err = %v, want secret mismatch", err)
	}
}

func TestParseCygwinSocketRejectsInvalidInputs(t *testing.T) {
	for _, input := range []string{
		"not a socket",
		"!<socket >0 s 01020304-05060708-090a0b0c-0d0e0f10\x00",
		"!<socket >70000 s 01020304-05060708-090a0b0c-0d0e0f10\x00",
		"!<socket >1234 d 01020304-05060708-090a0b0c-0d0e0f10\x00",
		"!<socket >1234 s 01020304-05060708-090a0b0c\x00",
		"!<socket >1234 s zzzzzzzz-05060708-090a0b0c-0d0e0f10\x00",
	} {
		if _, err := parseCygwinSocket([]byte(input)); err == nil {
			t.Errorf("parseCygwinSocket(%q) unexpectedly succeeded", input)
		}
	}
}

func TestDialRealMSYS2SSHAgent(t *testing.T) {
	agentExe := findGitMSYS2SSHAgent(t)
	if agentExe == "" {
		t.Skip("Git for Windows MSYS2 ssh-agent.exe is not installed")
	}
	addExe := filepath.Join(filepath.Dir(agentExe), "ssh-add.exe")
	if _, err := os.Stat(addExe); err != nil {
		t.Skip("Git for Windows MSYS2 ssh-add.exe is not installed")
	}

	dir := t.TempDir()
	socketPath := filepath.Join(dir, "agent.sock")
	start := exec.Command(agentExe, "-s", "-a", socketPath)
	out, err := start.CombinedOutput()
	if err != nil {
		t.Fatalf("start %s: %v (%s)", agentExe, err, out)
	}
	var pid string
	for _, line := range strings.Split(string(out), "\n") {
		if rest, ok := strings.CutPrefix(strings.TrimSpace(line), "SSH_AGENT_PID="); ok {
			pid, _, _ = strings.Cut(rest, ";")
			break
		}
	}
	if pid == "" {
		t.Fatalf("ssh-agent output did not include SSH_AGENT_PID: %q", out)
	}
	t.Cleanup(func() {
		kill := exec.Command(agentExe, "-k")
		kill.Env = []string{
			"PATH=" + os.Getenv("PATH"),
			"SSH_AUTH_SOCK=" + socketPath,
			"SSH_AGENT_PID=" + pid,
		}
		_ = kill.Run()
	})

	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "msys2-test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	add := exec.Command(addExe, keyPath)
	add.Env = append(os.Environ(), "SSH_AUTH_SOCK="+socketPath, "SSH_AGENT_PID="+pid)
	if addOut, err := add.CombinedOutput(); err != nil {
		t.Fatalf("ssh-add: %v (%s)", err, addOut)
	}

	sshPub, err := ssh.NewPublicKey(public)
	if err != nil {
		t.Fatal(err)
	}
	server := startServer(t, func(s *testServer) {
		s.authorized = []ssh.PublicKey{sshPub}
		s.password = "unused"
	})
	h := dial(t, server, func(c *Config) {
		c.Agent = true
		c.AgentAddress = socketPath
	})
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	connected := h.phase(PhaseConnected)
	if connected.Method != "publickey" || len(connected.Skipped) != 0 {
		t.Fatalf("connected = %+v, want publickey without skipped items", connected)
	}
}

func findGitMSYS2SSHAgent(t *testing.T) string {
	t.Helper()
	var candidates []string
	for _, root := range []string{
		os.Getenv("ProgramFiles"),
		os.Getenv("ProgramFiles(x86)"),
		filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
	} {
		if root != "" {
			candidates = append(candidates, filepath.Join(root, "Git", "usr", "bin", "ssh-agent.exe"))
		}
	}
	if gitPath, err := exec.LookPath("git"); err == nil {
		dir := filepath.Dir(gitPath)
		candidates = append(candidates,
			filepath.Join(filepath.Dir(dir), "usr", "bin", "ssh-agent.exe"),
			filepath.Join(filepath.Dir(filepath.Dir(dir)), "usr", "bin", "ssh-agent.exe"),
		)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate
		}
	}
	return ""
}
