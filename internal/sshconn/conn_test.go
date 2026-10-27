package sshconn

import (
	"context"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

func TestDialContextNeedsAConnectedConn(t *testing.T) {
	conn := &Conn{}
	if stream, err := conn.DialContext(context.Background(), "tcp", "127.0.0.1:80"); err == nil {
		stream.Close()
		t.Fatal("DialContext succeeded without an authenticated client")
	}
}

func TestListenNeedsAConnectedConn(t *testing.T) {
	conn := &Conn{}
	if listener, err := conn.Listen("tcp", "127.0.0.1:8080"); err == nil {
		listener.Close()
		t.Fatal("Listen succeeded without an authenticated client")
	}
}

func TestListenAcceptsRemoteForwardedConnections(t *testing.T) {
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.remote = true
	})
	harness := dial(t, server, nil)
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	harness.phase(PhaseConnected)

	listener, err := harness.conn.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	remote, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	accepted, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	if _, err := io.WriteString(remote, "from remote"); err != nil {
		t.Fatal(err)
	}
	buffer := make([]byte, len("from remote"))
	if _, err := io.ReadFull(accepted, buffer); err != nil || string(buffer) != "from remote" {
		t.Fatalf("accepted read = %q, %v", buffer, err)
	}
	if _, err := io.WriteString(accepted, "from client"); err != nil {
		t.Fatal(err)
	}
	buffer = make([]byte, len("from client"))
	if _, err := io.ReadFull(remote, buffer); err != nil || string(buffer) != "from client" {
		t.Fatalf("remote read = %q, %v", buffer, err)
	}
	_ = accepted.Close()
	_ = remote.Close()
	if err := listener.Close(); err != nil {
		t.Fatal(err)
	}
	if connection, err := net.DialTimeout("tcp", listener.Addr().String(), 100*time.Millisecond); err == nil {
		connection.Close()
		t.Fatal("closed remote listener accepted a connection")
	}
}

func TestUnknownHostKeyIsTrustedThenPasswordConnectsAndRemembers(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "s3cret"; s.banner = "Welcome to the test box\n" })
	h := dial(t, server, nil)

	prompt := h.phase(PhaseHostKeyPrompt)
	if prompt.HostKey.Type != "ssh-ed25519" || prompt.HostKey.Fingerprint != ssh.FingerprintSHA256(server.hostKey.PublicKey()) {
		t.Fatalf("host key prompt = %+v", prompt.HostKey)
	}
	if len(prompt.HostKey.Known) != 0 {
		t.Fatalf("a first connection must not list known keys: %+v", prompt.HostKey)
	}
	h.conn.TrustHostKey(true)

	asked := h.phase(PhasePasswordPrompt)
	if asked.Attempt != 1 || len(asked.Questions) != 1 || asked.Questions[0] != (Question{Secret: true}) {
		t.Fatalf("password prompt = attempt %d questions %+v", asked.Attempt, asked.Questions)
	}
	h.conn.SubmitPassword("s3cret")

	connected := h.phase(PhaseConnected)
	if connected.Method != "password" || connected.ServerVersion != "SSH-2.0-GopTest" {
		t.Fatalf("connected status = %+v", connected)
	}
	if !strings.Contains(connected.Banner, "Welcome to the test box") {
		t.Fatalf("banner = %q", connected.Banner)
	}
	if h.conn.Client() == nil {
		t.Fatal("no client once connected")
	}
	if !strings.Contains(h.knownHosts(), "ssh-ed25519") {
		t.Fatalf("trusted key was not remembered: %q", h.knownHosts())
	}
	info, err := os.Stat(h.conn.config.KnownHostsPath)
	if err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o600 {
		t.Fatalf("known-hosts file mode = %v, %v; want 0600", info, err)
	}

	// A second connection with the same file trusts the key without asking.
	again := dial(t, server, func(c *Config) { c.KnownHostsPath = h.conn.config.KnownHostsPath })
	status := again.until("a password prompt without a host-key prompt", func(s Status) bool {
		if s.Phase == PhaseHostKeyPrompt {
			t.Fatal("a remembered host key prompted again")
		}
		return s.Phase == PhasePasswordPrompt
	})
	if status.Attempt != 1 {
		t.Fatalf("attempt = %d", status.Attempt)
	}
}

func TestTrustWithoutSavingLeavesTheFileEmpty(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "pw" })
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	if strings.TrimSpace(h.knownHosts()) != "" {
		t.Fatalf("an unsaved key was written: %q", h.knownHosts())
	}
}

func TestRejectingTheHostKeyFailsWithoutAuthenticating(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "pw" })
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.RejectHostKey()
	failed := h.phase(PhaseFailed)
	if !errors.Is(failed.Err, ErrHostKeyRejected) {
		t.Fatalf("error = %v, want ErrHostKeyRejected", failed.Err)
	}
	if len(server.offered()) != 0 {
		t.Fatalf("credentials were offered to an untrusted server: %v", server.offered())
	}
	if strings.TrimSpace(h.knownHosts()) != "" {
		t.Fatal("a rejected key was remembered")
	}
}

func TestChangedHostKeyFailsWithoutPromptingAndKeepsTheFile(t *testing.T) {
	genuine := startServer(t, func(s *testServer) { s.password = "pw" })
	first := dial(t, genuine, nil)
	first.phase(PhaseHostKeyPrompt)
	first.conn.TrustHostKey(true)
	first.phase(PhasePasswordPrompt)
	first.conn.Close()
	path := first.conn.config.KnownHostsPath
	trusted, _ := os.ReadFile(path)

	// Same address and port, different key: a rebuilt server or an attacker.
	_, port := genuine.host()
	genuine.stop()
	impostor := startServer(t, func(s *testServer) { s.port = port; s.password = "pw" })
	h := dial(t, impostor, func(c *Config) { c.KnownHostsPath = path })
	failed := h.until("a verdict", func(s Status) bool { return s.Phase == PhaseFailed || s.Phase == PhaseHostKeyPrompt })
	if failed.Phase == PhaseHostKeyPrompt {
		t.Fatal("a changed host key was offered for approval")
	}
	if !errors.Is(failed.Err, ErrHostKeyChanged) {
		t.Fatalf("error = %v, want ErrHostKeyChanged", failed.Err)
	}
	if len(failed.HostKey.Known) != 1 || failed.HostKey.Known[0] != ssh.FingerprintSHA256(genuine.hostKey.PublicKey()) ||
		failed.HostKey.Fingerprint != ssh.FingerprintSHA256(impostor.hostKey.PublicKey()) {
		t.Fatalf("fingerprints = %+v", failed.HostKey)
	}
	if after, _ := os.ReadFile(path); string(after) != string(trusted) {
		t.Fatal("a changed key modified the known-hosts file")
	}
	if len(impostor.offered()) != 0 {
		t.Fatal("credentials were offered to a server with a changed key")
	}
}

func TestWrongPasswordPromptsAgainThenFailsAfterThreeAttempts(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "right" })
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	for attempt := 1; attempt <= 3; attempt++ {
		status := h.until("password prompt", func(s Status) bool { return s.Phase == PhasePasswordPrompt && s.Attempt == attempt })
		if status.Attempt != attempt {
			t.Fatalf("attempt = %d, want %d", status.Attempt, attempt)
		}
		h.conn.SubmitPassword("wrong" + itoa(uint16(attempt)))
	}
	failed := h.phase(PhaseFailed)
	if !errors.Is(failed.Err, ErrAuthFailed) {
		t.Fatalf("error = %v, want ErrAuthFailed", failed.Err)
	}
	if got := server.offered(); len(got) != 3 {
		t.Fatalf("server saw %d passwords %v, want exactly 3", len(got), got)
	}
}

func TestPasswordCorrectedOnSecondAttemptConnects(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "right" })
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.until("first prompt", func(s Status) bool { return s.Phase == PhasePasswordPrompt && s.Attempt == 1 })
	h.conn.SubmitPassword("nope")
	h.until("second prompt", func(s Status) bool { return s.Phase == PhasePasswordPrompt && s.Attempt == 2 })
	h.conn.SubmitPassword("right")
	if connected := h.phase(PhaseConnected); connected.Method != "password" {
		t.Fatalf("method = %q", connected.Method)
	}
}

func TestIdentityFileAuthenticatesWithoutAPasswordPrompt(t *testing.T) {
	path, signer := writeKey(t, t.TempDir())
	server := startServer(t, func(s *testServer) { s.authorized = []ssh.PublicKey{signer.PublicKey()}; s.password = "unused" })
	h := dial(t, server, func(c *Config) { c.IdentityFiles = []string{path} })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	status := h.until("connected without a password prompt", func(s Status) bool {
		if s.Phase == PhasePasswordPrompt {
			t.Fatal("asked for a password although the key was accepted")
		}
		return s.Phase == PhaseConnected
	})
	if status.Method != "publickey" {
		t.Fatalf("method = %q", status.Method)
	}
	if len(server.offered()) != 0 {
		t.Fatal("a password was sent despite key authentication")
	}
}

func TestEndpointPasswordPolicyDoesNotOfferAValidIdentity(t *testing.T) {
	path, signer := writeKey(t, t.TempDir())
	server := startServer(t, func(server *testServer) {
		server.authorized = []ssh.PublicKey{signer.PublicKey()}
		server.password = "pw"
	})
	harness := dial(t, server, func(config *Config) {
		endpoint := Endpoint{Host: config.Host, Port: config.Port, User: config.User}
		config.Authentication = map[Endpoint]Authentication{
			endpoint: {Methods: AuthKeyboardInteractive | AuthPassword, IdentityFiles: []string{path}},
		}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	harness.phase(PhasePasswordPrompt)
	harness.conn.SubmitPassword("pw")
	if connected := harness.phase(PhaseConnected); connected.Method != "password" {
		t.Fatalf("connected = %+v", connected)
	}
}

func TestEndpointKeyPolicyDoesNotFallBackToPassword(t *testing.T) {
	server := startServer(t, func(server *testServer) { server.password = "pw" })
	harness := dial(t, server, func(config *Config) {
		endpoint := Endpoint{Host: config.Host, Port: config.Port, User: config.User}
		config.Authentication = map[Endpoint]Authentication{
			endpoint: {Methods: AuthPublicKey, IdentityFiles: []string{}},
		}
	})
	harness.phase(PhaseHostKeyPrompt)
	harness.conn.TrustHostKey(false)
	failed := harness.phase(PhaseFailed)
	if !errors.Is(failed.Err, ErrAuthFailed) {
		t.Fatalf("failure = %v", failed.Err)
	}
	if offered := server.offered(); len(offered) != 0 {
		t.Fatalf("passwords offered = %v", offered)
	}
}

func TestUnauthorizedKeyFallsBackToPassword(t *testing.T) {
	path, _ := writeKey(t, t.TempDir())
	other := newSigner(t)
	server := startServer(t, func(s *testServer) { s.authorized = []ssh.PublicKey{other.PublicKey()}; s.password = "pw" })
	h := dial(t, server, func(c *Config) { c.IdentityFiles = []string{path} })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("pw")
	h.phase(PhaseConnected)
}

func TestEncryptedIdentityPromptsAfterHostTrustAndAuthenticates(t *testing.T) {
	_, private := newPrivate(t)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("locked"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pemEncode(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	server := startServer(t, func(s *testServer) { s.authorized = []ssh.PublicKey{signer.PublicKey()}; s.password = "unused" })
	h := dial(t, server, func(c *Config) { c.IdentityFiles = []string{path} })
	status := h.phase(PhaseHostKeyPrompt)
	if status.IdentityPath != "" {
		t.Fatalf("identity prompted before host trust: %+v", status)
	}
	h.conn.TrustHostKey(false)
	first := h.phase(PhaseKeyPassphrasePrompt)
	if first.IdentityPath != path || first.Attempt != 1 {
		t.Fatalf("first prompt = %+v", first)
	}
	h.conn.SubmitKeyPassphrase("wrong")
	second := h.until("second key-passphrase attempt", func(status Status) bool {
		return status.Phase == PhaseKeyPassphrasePrompt && status.Attempt == 2
	})
	if second.IdentityPath != path {
		t.Fatalf("second prompt = %+v", second)
	}
	h.conn.SubmitKeyPassphrase("locked")
	connected := h.phase(PhaseConnected)
	if connected.Method != "publickey" || len(connected.Skipped) != 0 {
		t.Fatalf("connected = %+v", connected)
	}
	if len(server.offered()) != 0 {
		t.Fatal("password authentication ran despite an accepted encrypted key")
	}
}

func TestSkippingEncryptedIdentityFallsBackToPassword(t *testing.T) {
	_, private := newPrivate(t)
	block, err := ssh.MarshalPrivateKeyWithPassphrase(private, "", []byte("locked"))
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(t.TempDir(), "id_ed25519")
	if err := os.WriteFile(path, pemEncode(block), 0o600); err != nil {
		t.Fatal(err)
	}
	other := newSigner(t)
	server := startServer(t, func(s *testServer) {
		s.authorized = []ssh.PublicKey{other.PublicKey()}
		s.password = "pw"
	})
	h := dial(t, server, func(c *Config) { c.IdentityFiles = []string{path} })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhaseKeyPassphrasePrompt)
	h.conn.SkipIdentity()
	status := h.phase(PhasePasswordPrompt)
	if len(status.Skipped) != 1 || status.Skipped[0] != path {
		t.Fatalf("skipped = %v", status.Skipped)
	}
	h.conn.SubmitPassword("pw")
	if connected := h.phase(PhaseConnected); connected.Method != "password" {
		t.Fatalf("connected = %+v", connected)
	}
}

func TestConnectedStatusDetectsRemoteSystemOnASeparateSession(t *testing.T) {
	server := startServer(t, func(server *testServer) {
		server.password = "pw"
		server.exec = func(command string) (string, uint32) {
			if !strings.Contains(command, "/etc/os-release") {
				t.Fatalf("system command = %q", command)
			}
			return "kernel=Linux\nid=ubuntu\n", 0
		}
	})
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("pw")
	status := h.until("Ubuntu system", func(status Status) bool {
		return status.Phase == PhaseConnected && status.System == SystemUbuntu
	})
	if status.Method != "password" {
		t.Fatalf("connected status = %+v", status)
	}
}

func TestKeyboardInteractiveSingleHiddenQuestionUsesThePasswordPrompt(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.question = "Verification code: "; s.password = "123456" })
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	status := h.phase(PhasePasswordPrompt)
	if len(status.Questions) != 1 || status.Questions[0] != (Question{Text: "Verification code: ", Secret: true}) {
		t.Fatalf("questions = %+v, want the server's wording", status.Questions)
	}
	h.conn.SubmitPassword("123456")
	if connected := h.phase(PhaseConnected); connected.Method != "keyboard-interactive" {
		t.Fatalf("method = %q", connected.Method)
	}
}

func TestKeyboardInteractiveAsksEveryQuestionOfOneRoundAtOnce(t *testing.T) {
	server := startServer(t, func(s *testServer) {
		s.questions = []string{"Password: ", "Verification code: "}
		s.answers = []string{"secret", "123456"}
	})
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	status := h.phase(PhasePasswordPrompt)
	want := []Question{{Text: "Password: ", Secret: true}, {Text: "Verification code: ", Secret: true}}
	if len(status.Questions) != len(want) || status.Questions[0] != want[0] || status.Questions[1] != want[1] {
		t.Fatalf("questions = %+v, want %+v", status.Questions, want)
	}
	h.conn.SubmitAnswers([]string{"secret", "123456"})
	if connected := h.phase(PhaseConnected); connected.Method != "keyboard-interactive" {
		t.Fatalf("method = %q", connected.Method)
	}
}

func TestClosingDuringAPromptCancelsAndReleasesTheGoroutine(t *testing.T) {
	for name, phase := range map[string]Phase{"host key": PhaseHostKeyPrompt, "password": PhasePasswordPrompt} {
		t.Run(name, func(t *testing.T) {
			server := startServer(t, func(s *testServer) { s.password = "pw" })
			h := dial(t, server, nil)
			h.phase(PhaseHostKeyPrompt)
			if phase == PhasePasswordPrompt {
				h.conn.TrustHostKey(false)
				h.phase(PhasePasswordPrompt)
			}
			h.conn.Close()
			select {
			case <-h.conn.Done():
			default:
				t.Fatal("Close returned before the connection goroutine finished")
			}
			if status := h.conn.Status(); status.Phase != PhaseFailed || !errors.Is(status.Err, ErrCanceled) {
				t.Fatalf("status after Close = %+v", status)
			}
		})
	}
}

func TestAnswersOutOfPhaseAreIgnored(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "pw" })
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.SubmitPassword("early") // not asked yet: must neither unblock nor leak
	time.Sleep(50 * time.Millisecond)
	if h.conn.Status().Phase != PhaseHostKeyPrompt {
		t.Fatal("a password answered the host-key prompt")
	}
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.TrustHostKey(true) // stale answer
	if h.conn.Status().Phase != PhasePasswordPrompt {
		t.Fatal("a stale host-key answer advanced the password prompt")
	}
	if len(server.offered()) != 0 {
		t.Fatal("the early password reached the server")
	}
}

func TestUnreachableHostFailsWithTheAddress(t *testing.T) {
	server := startServer(t, nil)
	host, port := server.host()
	server.listener.Close() // nothing listens on this port now
	h := dial(t, server, func(c *Config) { c.Host, c.Port = host, port })
	failed := h.phase(PhaseFailed)
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), host+":"+itoa(port)) {
		t.Fatalf("error = %v, want one naming the address", failed.Err)
	}
	if errors.Is(failed.Err, ErrCanceled) {
		t.Fatal("a refused connection reported as canceled")
	}
}

// The user once trusted this address under a different key type. OpenSSH
// offers only the algorithms on file; a server without that key type then
// cannot connect, which is not the same as its key having changed.
func TestKnownKeyOfAnotherTypeIsNotReportedAsAChangedKey(t *testing.T) {
	server := startServer(t, func(s *testServer) { s.password = "pw" })
	host, port := server.host()
	path := filepath.Join(t.TempDir(), "known_hosts")
	line := "[" + host + "]:" + itoa(port) + " " + string(ssh.MarshalAuthorizedKey(newECDSAPublic(t)))
	if err := os.WriteFile(path, []byte(line), 0o600); err != nil {
		t.Fatal(err)
	}
	h := dial(t, server, func(c *Config) { c.KnownHostsPath = path })
	status := h.until("a verdict", func(s Status) bool { return s.Phase == PhaseFailed || s.Phase == PhaseHostKeyPrompt })
	if status.Phase == PhaseHostKeyPrompt {
		t.Fatal("an ed25519 key was offered for a host trusted under ECDSA")
	}
	if errors.Is(status.Err, ErrHostKeyChanged) {
		t.Fatalf("a different key type was reported as a changed key: %v", status.Err)
	}
	if len(server.offered()) != 0 {
		t.Fatal("credentials reached a server whose key type is not on file")
	}
}

func TestProxyJumpAuthenticatesEachEndpointAndConnectsOnlyThroughTheJump(t *testing.T) {
	target := startServer(t, func(server *testServer) { server.password = "target-secret" })
	targetHost, targetPort := target.host()
	targetAddress := net.JoinHostPort(targetHost, itoa(targetPort))
	jump := startServer(t, func(server *testServer) {
		server.password = "jump-secret"
		server.direct = targetAddress
	})
	jumpHost, jumpPort := jump.host()

	h := dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{{Host: jumpHost, Port: jumpPort, User: "bastion"}}
	})
	jumpKey := h.phase(PhaseHostKeyPrompt)
	if jumpKey.EndpointRole != EndpointProxyJump || jumpKey.Endpoint != (Endpoint{Host: jumpHost, Port: jumpPort, User: "bastion"}) {
		t.Fatalf("jump host-key endpoint = %+v role=%d", jumpKey.Endpoint, jumpKey.EndpointRole)
	}
	if jumpKey.HostKey.Fingerprint != ssh.FingerprintSHA256(jump.hostKey.PublicKey()) {
		t.Fatalf("jump host key = %+v", jumpKey.HostKey)
	}
	h.conn.TrustHostKey(true)
	jumpPassword := h.phase(PhasePasswordPrompt)
	if jumpPassword.EndpointRole != EndpointProxyJump || jumpPassword.Attempt != 1 {
		t.Fatalf("jump password status = %+v", jumpPassword)
	}
	h.conn.SubmitPassword("jump-secret")

	targetKey := h.phase(PhaseHostKeyPrompt)
	if targetKey.EndpointRole != EndpointTarget || targetKey.Endpoint != (Endpoint{Host: targetHost, Port: targetPort, User: "tester"}) {
		t.Fatalf("target host-key endpoint = %+v role=%d", targetKey.Endpoint, targetKey.EndpointRole)
	}
	if targetKey.HostKey.Fingerprint != ssh.FingerprintSHA256(target.hostKey.PublicKey()) {
		t.Fatalf("target host key = %+v", targetKey.HostKey)
	}
	h.conn.TrustHostKey(true)
	targetPassword := h.phase(PhasePasswordPrompt)
	if targetPassword.EndpointRole != EndpointTarget || targetPassword.Attempt != 1 {
		t.Fatalf("target password status = %+v", targetPassword)
	}
	h.conn.SubmitPassword("target-secret")

	connected := h.phase(PhaseConnected)
	if connected.EndpointRole != EndpointTarget || connected.Method != "password" || h.conn.Client() == nil {
		t.Fatalf("connected status = %+v", connected)
	}
	if offered := jump.offered(); len(offered) != 1 || offered[0] != "jump-secret" {
		t.Fatalf("jump passwords = %v", offered)
	}
	if offered := target.offered(); len(offered) != 1 || offered[0] != "target-secret" {
		t.Fatalf("target passwords = %v", offered)
	}
	known := h.knownHosts()
	if strings.Count(known, "ssh-ed25519") != 2 {
		t.Fatalf("known hosts = %q", known)
	}
}

func TestProxyJumpsAuthenticateEveryEndpointInRouteOrder(t *testing.T) {
	target := startServer(t, func(server *testServer) { server.password = "target-secret" })
	targetHost, targetPort := target.host()
	targetAddress := net.JoinHostPort(targetHost, itoa(targetPort))
	inner := startServer(t, func(server *testServer) {
		server.password = "inner-secret"
		server.direct = targetAddress
	})
	innerHost, innerPort := inner.host()
	innerAddress := net.JoinHostPort(innerHost, itoa(innerPort))
	outer := startServer(t, func(server *testServer) {
		server.password = "outer-secret"
		server.direct = innerAddress
	})
	outerHost, outerPort := outer.host()

	h := dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{
			{Host: outerHost, Port: outerPort, User: "outer-user"},
			{Host: innerHost, Port: innerPort, User: "inner-user"},
		}
	})
	steps := []struct {
		endpoint Endpoint
		server   *testServer
		password string
		role     EndpointRole
	}{
		{Endpoint{Host: outerHost, Port: outerPort, User: "outer-user"}, outer, "outer-secret", EndpointProxyJump},
		{Endpoint{Host: innerHost, Port: innerPort, User: "inner-user"}, inner, "inner-secret", EndpointProxyJump},
		{Endpoint{Host: targetHost, Port: targetPort, User: "tester"}, target, "target-secret", EndpointTarget},
	}
	for _, step := range steps {
		key := h.phase(PhaseHostKeyPrompt)
		if key.Endpoint != step.endpoint || key.EndpointRole != step.role {
			t.Fatalf("host-key endpoint = %+v role=%d, want %+v role=%d", key.Endpoint, key.EndpointRole, step.endpoint, step.role)
		}
		if key.HostKey.Fingerprint != ssh.FingerprintSHA256(step.server.hostKey.PublicKey()) {
			t.Fatalf("host key for %+v = %+v", step.endpoint, key.HostKey)
		}
		h.conn.TrustHostKey(true)
		password := h.phase(PhasePasswordPrompt)
		if password.Endpoint != step.endpoint || password.EndpointRole != step.role || password.Attempt != 1 {
			t.Fatalf("password endpoint = %+v role=%d attempt=%d", password.Endpoint, password.EndpointRole, password.Attempt)
		}
		h.conn.SubmitPassword(step.password)
	}

	connected := h.phase(PhaseConnected)
	if connected.EndpointRole != EndpointTarget || connected.Method != "password" || h.conn.Client() == nil {
		t.Fatalf("connected status = %+v", connected)
	}
	for _, step := range steps {
		if offered := step.server.offered(); len(offered) != 1 || offered[0] != step.password {
			t.Fatalf("passwords for %+v = %v", step.endpoint, offered)
		}
	}
	if known := h.knownHosts(); strings.Count(known, "ssh-ed25519") != 3 {
		t.Fatalf("known hosts = %q", known)
	}
}

func TestProxyJumpRefusalDoesNotFallBackToADirectConnection(t *testing.T) {
	target := startServer(t, func(server *testServer) { server.password = "target-secret" })
	jump := startServer(t, func(server *testServer) { server.password = "jump-secret" })
	jumpHost, jumpPort := jump.host()

	h := dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{{Host: jumpHost, Port: jumpPort, User: "bastion"}}
	})
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("jump-secret")
	failed := h.phase(PhaseFailed)
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "through") {
		t.Fatalf("proxy refusal error = %v", failed.Err)
	}
	if len(target.offered()) != 0 {
		t.Fatalf("target received credentials despite refused forwarding: %v", target.offered())
	}
}

func TestMiddleProxyJumpRefusalDoesNotFallBackToADirectConnection(t *testing.T) {
	target := startServer(t, func(server *testServer) { server.password = "target-secret" })
	inner := startServer(t, func(server *testServer) { server.password = "inner-secret" })
	innerHost, innerPort := inner.host()
	innerAddress := net.JoinHostPort(innerHost, itoa(innerPort))
	outer := startServer(t, func(server *testServer) {
		server.password = "outer-secret"
		server.direct = innerAddress
	})
	outerHost, outerPort := outer.host()

	h := dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{
			{Host: outerHost, Port: outerPort, User: "outer-user"},
			{Host: innerHost, Port: innerPort, User: "inner-user"},
		}
	})
	for _, password := range []string{"outer-secret", "inner-secret"} {
		h.phase(PhaseHostKeyPrompt)
		h.conn.TrustHostKey(false)
		h.phase(PhasePasswordPrompt)
		h.conn.SubmitPassword(password)
	}
	failed := h.phase(PhaseFailed)
	if failed.Err == nil || !strings.Contains(failed.Err.Error(), "through") {
		t.Fatalf("middle proxy refusal error = %v", failed.Err)
	}
	if len(target.offered()) != 0 {
		t.Fatalf("target received credentials despite refused middle forwarding: %v", target.offered())
	}
}

func TestCancelClosesEveryEstablishedProxyJumpConnection(t *testing.T) {
	target := startServer(t, func(server *testServer) { server.password = "target-secret" })
	targetHost, targetPort := target.host()
	inner := startServer(t, func(server *testServer) {
		server.password = "inner-secret"
		server.direct = net.JoinHostPort(targetHost, itoa(targetPort))
	})
	innerHost, innerPort := inner.host()
	outer := startServer(t, func(server *testServer) {
		server.password = "outer-secret"
		server.direct = net.JoinHostPort(innerHost, itoa(innerPort))
	})
	outerHost, outerPort := outer.host()

	h := dial(t, target, func(config *Config) {
		config.ProxyJumps = []Endpoint{
			{Host: outerHost, Port: outerPort, User: "outer-user"},
			{Host: innerHost, Port: innerPort, User: "inner-user"},
		}
	})
	for _, password := range []string{"outer-secret", "inner-secret", "target-secret"} {
		h.phase(PhaseHostKeyPrompt)
		h.conn.TrustHostKey(false)
		h.phase(PhasePasswordPrompt)
		h.conn.SubmitPassword(password)
	}
	h.phase(PhaseConnected)
	for name, server := range map[string]*testServer{"outer": outer, "inner": inner, "target": target} {
		if active := server.activeConnections(); active != 1 {
			t.Fatalf("%s active connections = %d, want 1", name, active)
		}
	}

	h.conn.Cancel()
	select {
	case <-h.conn.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("connection did not stop after cancel")
	}
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if outer.activeConnections() == 0 && inner.activeConnections() == 0 && target.activeConnections() == 0 {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("active connections after cancel: outer=%d inner=%d target=%d", outer.activeConnections(), inner.activeConnections(), target.activeConnections())
}
