package sshconn

import (
	"crypto/ecdsa"
	"crypto/ed25519"
	"crypto/elliptic"
	"crypto/rand"
	"encoding/pem"
	"fmt"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// testServer is a real SSH server on the loopback interface: it performs the
// full key exchange and authentication, then refuses every channel.
type testServer struct {
	listener net.Listener
	hostKey  ssh.Signer
	password string // accepted password; empty disables password auth
	question string // accepted keyboard-interactive question; empty disables it
	// questions asks a whole keyboard-interactive round instead of question;
	// answers, when set, are the replies it accepts, in order.
	questions   []string
	answers     []string
	authorized  []ssh.PublicKey
	banner      string
	port        uint16 // fixed port to listen on; zero picks one
	direct      string // exact direct-tcpip destination; empty refuses forwarding
	exec        func(string) (string, uint32)
	remote      bool // permits loopback-only tcpip-forward requests
	blockDirect bool // leaves direct-tcpip requests unanswered
	request     func(*ssh.ServerConn, *ssh.Request)
	connected   func(*ssh.ServerConn)
	// algorithms, when set, restricts what the server will negotiate, to stand
	// in for an old device.
	algorithms *ssh.Config

	// shell, when set, runs for every session that asks for a shell, with the
	// pty it requested. command handles exec requests. Without either every
	// session channel is refused.
	shell     func(*fakeShell)
	command   func(string, ssh.Channel)
	lastShell *fakeShell // the most recent shell session, guarded by mu
	// x11 makes sessions accept "x11-req" and record it; refuseX11 answers it
	// with a refusal instead.
	x11, refuseX11 bool
	// agentForward accepts "auth-agent-req@openssh.com" on sessions and
	// queries the forwarded agent through an "auth-agent@openssh.com"
	// channel, the way a remote ssh-add would.
	agentForward bool
	lastConn     *ssh.ServerConn // the most recent connection, guarded by mu

	mu        sync.Mutex
	passwords []string // every password the server was offered
	active    int      // authenticated SSH connections currently open
	group     sync.WaitGroup
}

func newSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}

func startServer(t *testing.T, configure func(*testServer)) *testServer {
	t.Helper()
	server := &testServer{hostKey: newSigner(t)}
	if configure != nil {
		configure(server)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:"+strconv.Itoa(int(server.port)))
	if err != nil {
		t.Fatal(err)
	}
	server.listener = listener
	config := &ssh.ServerConfig{ServerVersion: "SSH-2.0-GopTest"}
	if server.algorithms != nil {
		config.Config = *server.algorithms
	}
	config.AddHostKey(server.hostKey)
	if server.banner != "" {
		banner := server.banner
		config.BannerCallback = func(ssh.ConnMetadata) string { return banner }
	}
	if server.password != "" {
		config.PasswordCallback = func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			server.mu.Lock()
			server.passwords = append(server.passwords, string(password))
			server.mu.Unlock()
			if string(password) == server.password {
				return nil, nil
			}
			return nil, os.ErrPermission
		}
	}
	if server.question != "" || len(server.questions) > 0 {
		questions := server.questions
		if len(questions) == 0 {
			questions = []string{server.question}
		}
		echo := make([]bool, len(questions))
		config.KeyboardInteractiveCallback = func(_ ssh.ConnMetadata, challenge ssh.KeyboardInteractiveChallenge) (*ssh.Permissions, error) {
			answers, err := challenge("", "", questions, echo)
			if err != nil {
				return nil, err
			}
			server.mu.Lock()
			server.passwords = append(server.passwords, answers...)
			server.mu.Unlock()
			accepted := server.answers
			if len(accepted) == 0 {
				accepted = []string{server.password}
			}
			if len(answers) != len(accepted) {
				return nil, os.ErrPermission
			}
			for index := range accepted {
				if answers[index] != accepted[index] {
					return nil, os.ErrPermission
				}
			}
			return nil, nil
		}
	}
	if len(server.authorized) > 0 {
		config.PublicKeyCallback = func(_ ssh.ConnMetadata, key ssh.PublicKey) (*ssh.Permissions, error) {
			for _, allowed := range server.authorized {
				if string(allowed.Marshal()) == string(key.Marshal()) {
					return nil, nil
				}
			}
			return nil, os.ErrPermission
		}
	}
	server.group.Add(1)
	go func() {
		defer server.group.Done()
		for {
			socket, err := listener.Accept()
			if err != nil {
				return
			}
			server.group.Add(1)
			go func() {
				defer server.group.Done()
				defer socket.Close()
				serverConn, channels, requests, err := ssh.NewServerConn(socket, config)
				if err != nil {
					return
				}
				server.mu.Lock()
				server.active++
				server.lastConn = serverConn
				server.mu.Unlock()
				if server.connected != nil {
					server.connected(serverConn)
				}
				defer func() {
					server.mu.Lock()
					server.active--
					server.mu.Unlock()
				}()
				if server.request != nil {
					go func() {
						for request := range requests {
							server.request(serverConn, request)
						}
					}()
				} else if server.remote {
					go serveTestRemoteRequests(server, serverConn, requests)
				} else {
					go ssh.DiscardRequests(requests)
				}
				go func() {
					for newChannel := range channels {
						switch newChannel.ChannelType() {
						case "session":
							if server.shell == nil && server.exec == nil && server.command == nil {
								newChannel.Reject(ssh.Prohibited, "no session channels in tests")
								continue
							}
							channel, channelRequests, err := newChannel.Accept()
							if err != nil {
								continue
							}
							server.group.Add(1)
							go func() {
								defer server.group.Done()
								serveSession(server, serverConn, channel, channelRequests)
							}()
						case "direct-tcpip":
							if server.blockDirect {
								continue
							}
							target, err := testDirectTarget(newChannel.ExtraData(), server.direct)
							if err != nil {
								newChannel.Reject(ssh.Prohibited, err.Error())
								continue
							}
							remote, err := net.Dial("tcp", target)
							if err != nil {
								newChannel.Reject(ssh.ConnectionFailed, err.Error())
								continue
							}
							channel, channelRequests, err := newChannel.Accept()
							if err != nil {
								remote.Close()
								continue
							}
							server.group.Add(1)
							go func() {
								defer server.group.Done()
								serveTestDirect(channel, remote, channelRequests)
							}()
						default:
							newChannel.Reject(ssh.UnknownChannelType, "unsupported channel")
						}
					}
				}()
				serverConn.Wait()
			}()
		}
	}()
	t.Cleanup(server.stop)
	return server
}

type testDirectTCPIPRequest struct {
	DestinationHost string
	DestinationPort uint32
	OriginHost      string
	OriginPort      uint32
}

type testRemoteForwardRequest struct {
	Address string
	Port    uint32
}

type testForwardedTCPIPRequest struct {
	ConnectedAddress string
	ConnectedPort    uint32
	OriginAddress    string
	OriginPort       uint32
}

func serveTestRemoteRequests(server *testServer, conn *ssh.ServerConn, requests <-chan *ssh.Request) {
	listeners := make(map[string]net.Listener)
	defer func() {
		for _, listener := range listeners {
			_ = listener.Close()
		}
	}()
	for request := range requests {
		var forward testRemoteForwardRequest
		if err := ssh.Unmarshal(request.Payload, &forward); err != nil {
			request.Reply(false, nil)
			continue
		}
		address := net.JoinHostPort(forward.Address, strconv.Itoa(int(forward.Port)))
		switch request.Type {
		case "tcpip-forward":
			ip := net.ParseIP(forward.Address)
			if ip == nil || !ip.IsLoopback() || forward.Port > 65535 {
				request.Reply(false, nil)
				continue
			}
			listener, err := net.Listen("tcp", address)
			if err != nil {
				request.Reply(false, nil)
				continue
			}
			actualPort := uint32(listener.Addr().(*net.TCPAddr).Port)
			address = net.JoinHostPort(forward.Address, strconv.Itoa(int(actualPort)))
			listeners[address] = listener
			response := []byte(nil)
			if forward.Port == 0 {
				response = ssh.Marshal(struct{ Port uint32 }{actualPort})
			}
			request.Reply(true, response)
			server.group.Add(1)
			go func(bindAddress string, bindPort uint32) {
				defer server.group.Done()
				serveTestRemoteListener(server, conn, listener, bindAddress, bindPort)
			}(forward.Address, actualPort)
		case "cancel-tcpip-forward":
			listener := listeners[address]
			if listener == nil {
				request.Reply(false, nil)
				continue
			}
			delete(listeners, address)
			_ = listener.Close()
			request.Reply(true, nil)
		default:
			request.Reply(false, nil)
		}
	}
}

func serveTestRemoteListener(server *testServer, conn *ssh.ServerConn, listener net.Listener, bindAddress string, bindPort uint32) {
	for {
		incoming, err := listener.Accept()
		if err != nil {
			return
		}
		origin := incoming.RemoteAddr().(*net.TCPAddr)
		payload := ssh.Marshal(testForwardedTCPIPRequest{
			ConnectedAddress: bindAddress,
			ConnectedPort:    bindPort,
			OriginAddress:    origin.IP.String(),
			OriginPort:       uint32(origin.Port),
		})
		channel, requests, err := conn.OpenChannel("forwarded-tcpip", payload)
		if err != nil {
			_ = incoming.Close()
			continue
		}
		server.group.Add(1)
		go func() {
			defer server.group.Done()
			serveTestDirect(channel, incoming, requests)
		}()
	}
}

func testDirectTarget(payload []byte, allowed string) (string, error) {
	if allowed == "" {
		return "", fmt.Errorf("direct-tcpip disabled")
	}
	var request testDirectTCPIPRequest
	if err := ssh.Unmarshal(payload, &request); err != nil {
		return "", fmt.Errorf("invalid direct-tcpip request")
	}
	target := net.JoinHostPort(request.DestinationHost, strconv.Itoa(int(request.DestinationPort)))
	if target != allowed {
		return "", fmt.Errorf("direct-tcpip target refused")
	}
	return target, nil
}

func serveTestDirect(channel ssh.Channel, remote net.Conn, requests <-chan *ssh.Request) {
	defer channel.Close()
	defer remote.Close()
	go ssh.DiscardRequests(requests)
	uploaded := make(chan struct{})
	go func() {
		_, _ = io.Copy(remote, channel)
		if tcp, ok := remote.(*net.TCPConn); ok {
			_ = tcp.CloseWrite()
		}
		close(uploaded)
	}()
	_, _ = io.Copy(channel, remote)
	_ = channel.CloseWrite()
	<-uploaded
}

// stop closes the listener and waits for in-flight connections.
func (server *testServer) stop() {
	server.listener.Close()
	server.group.Wait()
}

func (server *testServer) host() (string, uint16) {
	address := server.listener.Addr().(*net.TCPAddr)
	return "127.0.0.1", uint16(address.Port)
}

func (server *testServer) offered() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	return append([]string(nil), server.passwords...)
}

func (server *testServer) activeConnections() int {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.active
}

// harness wires a connection to a notification channel so tests wait on
// status changes instead of sleeping.
type harness struct {
	t       *testing.T
	conn    *Conn
	changes chan struct{}
}

func dial(t *testing.T, server *testServer, mutate func(*Config)) *harness {
	t.Helper()
	host, port := server.host()
	h := &harness{t: t, changes: make(chan struct{}, 1)}
	config := Config{
		Host: host, Port: port, User: "tester",
		KnownHostsPath: filepath.Join(t.TempDir(), "motty", "known_hosts"),
		IdentityFiles:  []string{}, // never read the developer's real keys
		Notify: func() {
			select {
			case h.changes <- struct{}{}:
			default:
			}
		},
	}
	if mutate != nil {
		mutate(&config)
	}
	h.conn = Dial(config)
	t.Cleanup(h.conn.Close)
	return h
}

// until waits for a status satisfying want.
func (h *harness) until(description string, want func(Status) bool) Status {
	h.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		if status := h.conn.Status(); want(status) {
			return status
		}
		select {
		case <-h.changes:
		case <-deadline:
			h.t.Fatalf("timed out waiting for %s; last status %+v", description, h.conn.Status())
		}
	}
}

func (h *harness) phase(phase Phase) Status {
	h.t.Helper()
	return h.until("phase "+strconv.Itoa(int(phase)), func(status Status) bool { return status.Phase == phase })
}

func (h *harness) knownHosts() string {
	data, _ := os.ReadFile(h.conn.config.KnownHostsPath)
	return string(data)
}

func writeKey(t *testing.T, dir string) (string, ssh.Signer) {
	t.Helper()
	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "")
	if err != nil {
		t.Fatal(err)
	}
	path := filepath.Join(dir, "id_ed25519")
	if err := os.WriteFile(path, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return path, signer
}

func itoa[T ~int | ~uint16](value T) string { return strconv.Itoa(int(value)) }

func newPrivate(t *testing.T) (ed25519.PublicKey, ed25519.PrivateKey) {
	t.Helper()
	public, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	return public, private
}

func pemEncode(block *pem.Block) []byte { return pem.EncodeToMemory(block) }

// newECDSAPublic returns a public key of a different type than the test
// servers' ed25519 host keys.
func newECDSAPublic(t *testing.T) ssh.PublicKey {
	t.Helper()
	private, err := ecdsa.GenerateKey(elliptic.P256(), rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	public, err := ssh.NewPublicKey(&private.PublicKey)
	if err != nil {
		t.Fatal(err)
	}
	return public
}

// fakeShell is the server's side of one shell session: what the client asked
// for, and the channel to talk to it on.
type fakeShell struct {
	Channel ssh.Channel

	mu      sync.Mutex
	term    string
	cols    int
	rows    int
	env     map[string]string
	resizes [][2]int
	x11     *x11Request
	// agentComments are the key comments the forwarded ssh-agent listed, once
	// this session asked for agent forwarding and the server queried it.
	agentComments []string
}

func (shell *fakeShell) state() (term string, cols, rows int, env map[string]string, resizes [][2]int) {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	copied := map[string]string{}
	for key, value := range shell.env {
		copied[key] = value
	}
	return shell.term, shell.cols, shell.rows, copied, append([][2]int(nil), shell.resizes...)
}

// exit ends the session with a status the way sshd does.
func (shell *fakeShell) exit(status uint32) {
	shell.Channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
	shell.Channel.Close()
}

func serveSession(server *testServer, conn ssh.Conn, channel ssh.Channel, requests <-chan *ssh.Request) {
	shell := &fakeShell{Channel: channel, env: map[string]string{}}
	started := false
	for request := range requests {
		switch request.Type {
		case "env":
			var payload struct{ Name, Value string }
			ssh.Unmarshal(request.Payload, &payload)
			shell.mu.Lock()
			shell.env[payload.Name] = payload.Value
			shell.mu.Unlock()
			request.Reply(true, nil)
		case "pty-req":
			var payload struct {
				Term          string
				Columns, Rows uint32
				Width, Height uint32
				Modes         string
			}
			ssh.Unmarshal(request.Payload, &payload)
			shell.mu.Lock()
			shell.term, shell.cols, shell.rows = payload.Term, int(payload.Columns), int(payload.Rows)
			shell.mu.Unlock()
			request.Reply(true, nil)
		case "x11-req":
			var payload x11Request
			if !server.x11 || server.refuseX11 || ssh.Unmarshal(request.Payload, &payload) != nil {
				request.Reply(false, nil)
				continue
			}
			shell.mu.Lock()
			shell.x11 = &payload
			shell.mu.Unlock()
			request.Reply(true, nil)
		case "window-change":
			var payload struct{ Columns, Rows, Width, Height uint32 }
			ssh.Unmarshal(request.Payload, &payload)
			shell.mu.Lock()
			shell.resizes = append(shell.resizes, [2]int{int(payload.Columns), int(payload.Rows)})
			shell.mu.Unlock()
		case "auth-agent-req@openssh.com":
			if !server.agentForward {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			server.queryForwardedAgent(conn, shell)
		case "shell":
			accepted := server.shell != nil && !started
			request.Reply(accepted, nil)
			if accepted {
				started = true
				server.mu.Lock()
				server.lastShell = shell
				server.mu.Unlock()
				go server.shell(shell)
			}
		case "exec":
			var payload struct{ Command string }
			if err := ssh.Unmarshal(request.Payload, &payload); err != nil {
				request.Reply(false, nil)
				continue
			}
			system := payload.Command == remoteSystemCommand
			accepted := !started && ((system && server.exec != nil) || (!system && server.command != nil))
			request.Reply(accepted, nil)
			if accepted {
				started = true
				if system {
					output, status := server.exec(payload.Command)
					_, _ = channel.Write([]byte(output))
					_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
					_ = channel.Close()
					return
				}
				server.mu.Lock()
				server.lastShell = shell
				server.mu.Unlock()
				go server.command(payload.Command, channel)
			}
		default:
			request.Reply(false, nil)
		}
	}
}
