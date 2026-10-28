package workbench

import (
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"encoding/json"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/knownhosts"

	"motty/internal/ai/gate"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
	"motty/internal/configstore"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// agentServer is a real SSH server on the loopback interface for the headless
// MCP server to work against. It takes one password, answers a few fixed
// commands and serves SFTP from a directory.
type agentServer struct {
	listener net.Listener
	hostKey  ssh.Signer
	password string
	root     string
	port     uint16

	mu          sync.Mutex
	connections int // authenticated connections accepted so far
	active      int // and how many are open now
	commands    []string
	conns       []*ssh.ServerConn
	group       sync.WaitGroup
}

func startAgentServer(t *testing.T) *agentServer {
	t.Helper()
	_, private, err := newTestEd25519()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &agentServer{
		listener: listener, hostKey: signer, password: "pw", root: t.TempDir(),
		port: uint16(listener.Addr().(*net.TCPAddr).Port),
	}
	server.group.Add(1)
	go func() {
		defer server.group.Done()
		for {
			raw, err := listener.Accept()
			if err != nil {
				return
			}
			server.group.Add(1)
			go server.handle(raw)
		}
	}()
	t.Cleanup(func() {
		listener.Close()
		server.mu.Lock()
		conns := append([]*ssh.ServerConn(nil), server.conns...)
		server.mu.Unlock()
		for _, conn := range conns {
			conn.Close()
		}
		server.group.Wait()
	})
	return server
}

func (server *agentServer) handle(raw net.Conn) {
	defer server.group.Done()
	config := &ssh.ServerConfig{
		PasswordCallback: func(_ ssh.ConnMetadata, password []byte) (*ssh.Permissions, error) {
			if string(password) == server.password {
				return nil, nil
			}
			return nil, os.ErrPermission
		},
	}
	config.AddHostKey(server.hostKey)
	conn, channels, requests, err := ssh.NewServerConn(raw, config)
	if err != nil {
		raw.Close()
		return
	}
	server.mu.Lock()
	server.connections++
	server.active++
	server.conns = append(server.conns, conn)
	server.mu.Unlock()
	defer func() {
		conn.Close()
		server.mu.Lock()
		server.active--
		server.mu.Unlock()
	}()
	server.group.Add(1)
	go func() {
		defer server.group.Done()
		ssh.DiscardRequests(requests)
	}()
	for newChannel := range channels {
		if newChannel.ChannelType() != "session" {
			newChannel.Reject(ssh.UnknownChannelType, "no")
			continue
		}
		channel, sessionRequests, err := newChannel.Accept()
		if err != nil {
			continue
		}
		server.group.Add(1)
		go server.session(channel, sessionRequests)
	}
}

func (server *agentServer) session(channel ssh.Channel, requests <-chan *ssh.Request) {
	defer server.group.Done()
	defer channel.Close()
	closed := make(chan struct{}) // closed when the client is done with the session
	var running sync.WaitGroup
	for request := range requests {
		switch request.Type {
		case "exec":
			var payload struct{ Command string }
			if ssh.Unmarshal(request.Payload, &payload) != nil {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			server.mu.Lock()
			server.commands = append(server.commands, payload.Command)
			server.mu.Unlock()
			running.Add(1)
			go func() {
				defer running.Done()
				server.run(channel, payload.Command, closed)
			}()
		case "subsystem":
			var payload struct{ Name string }
			if ssh.Unmarshal(request.Payload, &payload) != nil || payload.Name != "sftp" {
				request.Reply(false, nil)
				continue
			}
			request.Reply(true, nil)
			running.Add(1)
			go func() {
				defer running.Done()
				files, err := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(server.root))
				if err == nil {
					_ = files.Serve()
					files.Close()
				}
			}()
		default:
			request.Reply(false, nil)
		}
	}
	close(closed)
	running.Wait()
}

func (server *agentServer) run(channel ssh.Channel, command string, closed <-chan struct{}) {
	status := uint32(0)
	switch command {
	case "echo hello":
		io.WriteString(channel, "hello\n")
	case "fail":
		io.WriteString(channel, "partial\n")
		io.WriteString(channel.Stderr(), "boom\n")
		status = 3
	case "hang":
		io.WriteString(channel, "started\n")
		<-closed
		return
	default:
		io.WriteString(channel, "unknown command\n")
		status = 127
	}
	channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
	channel.Close()
}

func (server *agentServer) counts() (connections, active int) {
	server.mu.Lock()
	defer server.mu.Unlock()
	return server.connections, server.active
}

// ran is the commands the tests asked for. The connection itself runs one more
// to learn what system the host is; that is not a call and is left out.
func (server *agentServer) ran() []string {
	server.mu.Lock()
	defer server.mu.Unlock()
	var asked []string
	for _, command := range server.commands {
		if !strings.HasPrefix(command, "printf 'kernel=") {
			asked = append(asked, command)
		}
	}
	return asked
}

func (server *agentServer) address() string {
	return net.JoinHostPort("127.0.0.1", strconv.Itoa(int(server.port)))
}

// agentWorld is a server, a vault holding the password for it, a file of
// trusted host keys, and the catalog of one host opened to agents.
type agentWorld struct {
	t       *testing.T
	server  *agentServer
	dir     string
	keys    *memKeys
	options AgentOptions
	catalog hostCatalog
}

func newAgentWorld(t *testing.T) *agentWorld {
	t.Helper()
	server := startAgentServer(t)
	dir := t.TempDir()
	world := &agentWorld{t: t, server: server, dir: dir, keys: &memKeys{}}
	world.options = AgentOptions{
		KnownHostsPath: filepath.Join(dir, "known_hosts"),
		StatePath:      filepath.Join(dir, "config.db"),
		AIDatabasePath: filepath.Join(dir, "ai.db"),
		VaultPath:      filepath.Join(dir, "vault.json"),
		Keystore:       world.keys,
		Idle:           time.Minute,
	}
	world.trust(server.hostKey)
	world.savePassword("pw")
	host := Host{
		ID: 1, CatalogID: "host_web", Label: "web", Address: "127.0.0.1", Port: server.port, User: "ops",
		Protocol: protocolSSH, Auth: authPassword,
	}
	world.catalog = hostCatalog{
		hosts: []Host{host}, systemUser: "tester", connectionTimeout: 5 * time.Second,
		agentHosts: map[string]bool{"host_web": true},
	}
	return world
}

// trust makes key the trusted key of the server, replacing any other.
func (world *agentWorld) trust(key ssh.Signer) {
	world.t.Helper()
	line := knownhosts.Line([]string{knownhosts.Normalize(world.server.address())}, key.PublicKey())
	if err := os.WriteFile(world.options.KnownHostsPath, []byte(line+"\n"), 0o600); err != nil {
		world.t.Fatal(err)
	}
}

func (world *agentWorld) savePassword(password string) {
	world.t.Helper()
	saved, err := vault.Open(world.options.VaultPath, world.keys)
	if err != nil {
		world.t.Fatal(err)
	}
	endpoint := sshconn.Endpoint{Host: "127.0.0.1", Port: world.server.port, User: "ops"}
	if err := saved.Set(passwordSecretName(endpoint), password); err != nil {
		world.t.Fatal(err)
	}
}

func (world *agentWorld) backend() *agentBackend {
	world.t.Helper()
	backend := newAgentBackend(world.catalog, world.options.withDefaults())
	world.t.Cleanup(backend.Close)
	return backend
}

func TestAgentHostsAreOnlyTheOnesOpenedToAgents(t *testing.T) {
	world := newAgentWorld(t)
	other := world.catalog.hosts[0]
	other.ID, other.CatalogID, other.Label = 2, "host_db", "db"
	world.catalog.hosts = append(world.catalog.hosts, other)
	backend := world.backend()

	sessions, _ := backend.Sessions(context.Background())
	if len(sessions) != 1 || sessions[0].ID != "host_web" || sessions[0].Title != "web" || !sessions[0].Ready {
		t.Fatalf("sessions = %+v", sessions)
	}
	if _, err := backend.Exec(context.Background(), tools.TerminalExecArgs{Session: "host_db", Command: "echo hello"}); !errors.Is(err, errAgentNoHost) {
		t.Fatalf("a host that is not open to agents gave %v", err)
	}
	if got := world.server.ran(); len(got) != 0 {
		t.Fatalf("a refused call still ran %q", got)
	}
	// A label names the host when it is unambiguous, and the only host is the default.
	if result, err := backend.Exec(context.Background(), tools.TerminalExecArgs{Session: "web", Command: "echo hello"}); err != nil || result.Output != "hello\n" {
		t.Fatalf("by label: %+v, %v", result, err)
	}
	if result, err := backend.Exec(context.Background(), tools.TerminalExecArgs{Command: "echo hello"}); err != nil || result.Output != "hello\n" {
		t.Fatalf("by default: %+v, %v", result, err)
	}
}

func TestAgentSignsInWithTheSavedPasswordAndKeepsOneConnection(t *testing.T) {
	world := newAgentWorld(t)
	backend := world.backend()
	ctx := context.Background()

	first, err := backend.Exec(ctx, tools.TerminalExecArgs{Session: "host_web", Command: "echo hello"})
	if err != nil || first.Output != "hello\n" || first.ExitCode != 0 {
		t.Fatalf("first = %+v, %v", first, err)
	}
	failed, err := backend.Exec(ctx, tools.TerminalExecArgs{Session: "host_web", Command: "fail"})
	if err != nil || failed.ExitCode != 3 || !strings.Contains(failed.Output, "partial") || !strings.Contains(failed.Output, "boom") {
		t.Fatalf("a failing command is a result with both streams: %+v, %v", failed, err)
	}
	if connections, _ := world.server.counts(); connections != 1 {
		t.Fatalf("two calls used %d connections", connections)
	}
}

func TestAgentTimeoutIsAResultAndTheConnectionSurvivesIt(t *testing.T) {
	world := newAgentWorld(t)
	backend := world.backend()
	ctx := context.Background()
	// The shortest timeout a call can ask for is a second.
	result, err := backend.Exec(ctx, tools.TerminalExecArgs{Session: "host_web", Command: "hang", TimeoutSeconds: 1})
	if err != nil || !result.TimedOut || !strings.Contains(result.Output, "started") {
		t.Fatalf("result = %+v, %v", result, err)
	}
	if again, err := backend.Exec(ctx, tools.TerminalExecArgs{Session: "host_web", Command: "echo hello"}); err != nil || again.Output != "hello\n" {
		t.Fatalf("after the timeout: %+v, %v", again, err)
	}
	if connections, _ := world.server.counts(); connections != 1 {
		t.Fatalf("a timeout cost %d connections", connections)
	}
}

func TestAgentNeverTrustsAHostKeyItWasNotGiven(t *testing.T) {
	world := newAgentWorld(t)
	backend := world.backend()

	if err := os.Remove(world.options.KnownHostsPath); err != nil {
		t.Fatal(err)
	}
	_, err := backend.Exec(context.Background(), tools.TerminalExecArgs{Session: "host_web", Command: "echo hello"})
	if err == nil || !strings.Contains(err.Error(), "connect to it once from the Motty window") {
		t.Fatalf("an unknown host key gave %v", err)
	}
	if data, _ := os.ReadFile(world.options.KnownHostsPath); strings.TrimSpace(string(data)) != "" {
		t.Fatalf("the agent trusted a key by itself: %q", data)
	}

	other := newTestSigner(t)
	world.trust(other)
	_, err = backend.Exec(context.Background(), tools.TerminalExecArgs{Session: "host_web", Command: "echo hello"})
	if err == nil || !strings.Contains(err.Error(), "host key changed") {
		t.Fatalf("a changed host key gave %v", err)
	}
	if got := world.server.ran(); len(got) != 0 {
		t.Fatalf("a command ran on a host whose key was not trusted: %q", got)
	}
}

func TestAgentSaysWhatToDoWhenThePasswordCannotBeUsed(t *testing.T) {
	t.Run("not saved", func(t *testing.T) {
		world := newAgentWorld(t)
		if err := os.Remove(world.options.VaultPath); err != nil {
			t.Fatal(err)
		}
		_, err := world.backend().Exec(context.Background(), tools.TerminalExecArgs{Command: "echo hello"})
		if err == nil || !strings.Contains(err.Error(), "no password is saved for ops@127.0.0.1") {
			t.Fatalf("got %v", err)
		}
	})
	t.Run("wrong", func(t *testing.T) {
		world := newAgentWorld(t)
		world.savePassword("not the password")
		_, err := world.backend().Exec(context.Background(), tools.TerminalExecArgs{Command: "echo hello"})
		if err == nil || !strings.Contains(err.Error(), "was refused") {
			t.Fatalf("got %v", err)
		}
		if connections, _ := world.server.counts(); connections != 0 {
			t.Fatalf("a refused password still connected %d times", connections)
		}
	})
	t.Run("locked by a master password", func(t *testing.T) {
		world := newAgentWorld(t)
		saved, err := vault.Open(world.options.VaultPath, world.keys)
		if err != nil {
			t.Fatal(err)
		}
		if err := saved.SetMasterPassword("a long master password"); err != nil {
			t.Fatal(err)
		}
		_, err = world.backend().Exec(context.Background(), tools.TerminalExecArgs{Command: "echo hello"})
		if err == nil || !strings.Contains(err.Error(), "locked by a master password") {
			t.Fatalf("got %v", err)
		}
	})
}

func TestAgentClosesAConnectionNobodyUsesAnymore(t *testing.T) {
	world := newAgentWorld(t)
	world.options.Idle = 40 * time.Millisecond
	backend := world.backend()
	if _, err := backend.Exec(context.Background(), tools.TerminalExecArgs{Command: "echo hello"}); err != nil {
		t.Fatal(err)
	}
	deadline := time.Now().Add(3 * time.Second)
	for {
		if _, active := world.server.counts(); active == 0 {
			break
		}
		if time.Now().After(deadline) {
			t.Fatal("the idle connection was never closed")
		}
		time.Sleep(10 * time.Millisecond)
	}
	// The next call connects again.
	if result, err := backend.Exec(context.Background(), tools.TerminalExecArgs{Command: "echo hello"}); err != nil || result.Output != "hello\n" {
		t.Fatalf("after the idle close: %+v, %v", result, err)
	}
	if connections, _ := world.server.counts(); connections != 2 {
		t.Fatalf("connections = %d, want 2", connections)
	}
}

func TestAgentListsAndReadsFilesOverSFTP(t *testing.T) {
	world := newAgentWorld(t)
	if err := os.WriteFile(filepath.Join(world.server.root, "notes.txt"), []byte("first line\nsecond line\n"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.Mkdir(filepath.Join(world.server.root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	backend := world.backend()
	ctx := context.Background()

	entries, err := backend.ListDir(ctx, tools.SFTPListArgs{Session: "host_web", Path: "."})
	if err != nil || len(entries) != 2 || entries[0].Name != "docs" || !entries[0].Dir || entries[1].Name != "notes.txt" {
		t.Fatalf("entries = %+v, %v", entries, err)
	}
	text, truncated, err := backend.ReadFile(ctx, tools.SFTPReadArgs{Session: "host_web", Path: "notes.txt", MaxBytes: 5})
	if err != nil || text != "first" || !truncated {
		t.Fatalf("read = %q, %v, %v", text, truncated, err)
	}
	if connections, _ := world.server.counts(); connections != 1 {
		t.Fatalf("files and listing used %d connections", connections)
	}
	if err := backend.WriteFile(ctx, tools.SFTPWriteArgs{Session: "host_web", Path: "x", Content: "x"}); !errors.Is(err, errAgentNotOffered) {
		t.Fatalf("writing gave %v", err)
	}
}

func TestAgentAuditRecordsRefusalsAndOutcomesWithoutOutput(t *testing.T) {
	world := newAgentWorld(t)
	backend := world.backend()
	log, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	defer log.Close()
	policy := risk.Policy{Enabled: true, AutoUpTo: risk.Low}
	readOnly := backend.audited(gate.Gate{Backend: backend, Policy: policy, Mode: gate.ModeObserve}, log)
	commands := backend.audited(gate.Gate{Backend: backend, Policy: policy, Mode: gate.ModeAsk}, log)
	ctx := context.Background()

	if result := readOnly(ctx, tools.NameTerminalExec, []byte(`{"command":"echo hello"}`)); !result.Denied {
		t.Fatalf("a read-only server ran a command: %+v", result)
	}
	if result := commands(ctx, tools.NameTerminalExec, []byte(`{"command":"echo hello"}`)); result.IsError || !strings.Contains(result.Text, "hello") {
		t.Fatalf("a low-risk command did not run: %+v", result)
	}
	if result := commands(ctx, tools.NameTerminalExec, []byte(`{"session":"host_web","command":"rm -rf /"}`)); !result.Denied {
		t.Fatalf("a command that needs approval ran with nobody to ask: %+v", result)
	}
	if got := world.server.ran(); len(got) != 1 || got[0] != "echo hello" {
		t.Fatalf("the server ran %q", got)
	}

	entries, err := log.Audit(10)
	if err != nil || len(entries) != 3 {
		t.Fatalf("audit = %+v, %v", entries, err)
	}
	// Newest first.
	want := []struct{ summary, outcome string }{
		{"rm -rf /", store.AuditRefused}, {"echo hello", store.AuditOK}, {"echo hello", store.AuditRefused},
	}
	for index, entry := range entries {
		if entry.Summary != want[index].summary || entry.Outcome != want[index].outcome || entry.HostLabel != "web" || entry.HostID != "host_web" {
			t.Errorf("entry %d = %+v, want %+v", index, entry, want[index])
		}
		if strings.Contains(entry.Summary, "hello\n") {
			t.Errorf("entry %d holds output: %q", index, entry.Summary)
		}
	}
}

// serveAgent runs ServeAgentMCP on the given requests and returns the replies
// by request id.
func serveAgent(t *testing.T, options AgentOptions, requests ...string) map[int]string {
	t.Helper()
	var out strings.Builder
	input := strings.NewReader(strings.Join(requests, "\n") + "\n")
	if err := ServeAgentMCP(context.Background(), input, &syncWriter{writer: &out}, options); err != nil {
		t.Fatal(err)
	}
	replies := map[int]string{}
	for _, line := range strings.Split(strings.TrimSpace(out.String()), "\n") {
		if line == "" {
			continue
		}
		var reply struct {
			ID int `json:"id"`
		}
		if json.Unmarshal([]byte(line), &reply) != nil {
			t.Fatalf("not JSON: %q", line)
		}
		replies[reply.ID] = line
	}
	return replies
}

type syncWriter struct {
	mu     sync.Mutex
	writer io.Writer
}

func (w *syncWriter) Write(data []byte) (int, error) {
	w.mu.Lock()
	defer w.mu.Unlock()
	return w.writer.Write(data)
}

// saveAgentState writes the world's host into a configuration store the way the
// application does, with the host opened to agents.
func saveAgentState(t *testing.T, world *agentWorld, open bool) {
	t.Helper()
	saved := savedState{
		Hosts: []savedHost{{
			ID: "host_web", Label: "web", Address: "127.0.0.1", User: "ops", Port: world.server.port,
			savedTransport: savedTransport{Authentication: "password"},
		}},
	}
	if open {
		saved.AgentHosts = []string{"host_web"}
	}
	entries, local, err := splitState(saved, newConfigBase(), nil)
	if err != nil {
		t.Fatal(err)
	}
	config, err := configstore.Open(world.options.StatePath, configFormat)
	if err != nil {
		t.Fatal(err)
	}
	defer config.Close()
	if _, err := config.Commit(configstore.Change{Put: entries, Local: map[string][]byte{localKey: local}}); err != nil {
		t.Fatal(err)
	}
}

func TestServeAgentMCPOffersReadingUntilTheUserAllowsCommands(t *testing.T) {
	world := newAgentWorld(t)
	saveAgentState(t, world, true)
	initialize := `{"jsonrpc":"2.0","id":1,"method":"initialize","params":{"protocolVersion":"2025-06-18"}}`
	list := `{"jsonrpc":"2.0","id":2,"method":"tools/list"}`
	hosts := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}`
	run := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"command":"echo hello"}}}`

	replies := serveAgent(t, world.options, initialize, list, hosts, run)
	if strings.Contains(replies[2], "terminal_exec") || strings.Contains(replies[2], "terminal_read") || strings.Contains(replies[2], "sftp_write") || !strings.Contains(replies[2], "sftp_list") {
		t.Errorf("the read-only catalog = %s", replies[2])
	}
	if !strings.Contains(replies[3], "host_web") || !strings.Contains(replies[3], "ops@127.0.0.1") {
		t.Errorf("list_sessions = %s", replies[3])
	}
	if !strings.Contains(replies[4], "unknown tool") {
		t.Errorf("a command on a read-only server = %s", replies[4])
	}
	if got := world.server.ran(); len(got) != 0 {
		t.Fatalf("the read-only server ran %q", got)
	}

	log, err := store.Open(world.options.AIDatabasePath)
	if err != nil {
		t.Fatal(err)
	}
	if err := log.SetSetting(agentModeSetting, AgentModeCommands); err != nil {
		t.Fatal(err)
	}
	log.Close()
	replies = serveAgent(t, world.options, initialize, list, run)
	if !strings.Contains(replies[2], "terminal_exec") || strings.Contains(replies[2], "sftp_write") {
		t.Errorf("the catalog with commands = %s", replies[2])
	}
	if !strings.Contains(replies[4], "hello") || !strings.Contains(replies[4], "exit status 0") {
		t.Errorf("terminal_exec = %s", replies[4])
	}
	waitUntil := time.Now().Add(3 * time.Second)
	for {
		connections, active := world.server.counts()
		if connections == 1 && active == 0 {
			break
		}
		if time.Now().After(waitUntil) {
			t.Fatalf("connections %d, still open %d: the server must close what it opened", connections, active)
		}
		time.Sleep(10 * time.Millisecond)
	}
}

func TestServeAgentMCPServesNoHostThatWasNotOpened(t *testing.T) {
	world := newAgentWorld(t)
	saveAgentState(t, world, false)
	replies := serveAgent(t, world.options,
		`{"jsonrpc":"2.0","id":1,"method":"tools/call","params":{"name":"list_sessions","arguments":{}}}`)
	if strings.Contains(replies[1], "host_web") {
		t.Fatalf("a host that was not opened to agents is listed: %s", replies[1])
	}
}

func TestServeAgentMCPFullAccessRunsCommandsAndChangesFilesOnlyOnAuthorizedHosts(t *testing.T) {
	world := newAgentWorld(t)
	saveAgentState(t, world, true)
	setMode := func(value string) {
		t.Helper()
		db, err := store.Open(world.options.AIDatabasePath)
		if err != nil {
			t.Fatal(err)
		}
		defer db.Close()
		if err := db.SetSetting(agentModeSetting, value); err != nil {
			t.Fatal(err)
		}
	}
	setMode(AgentModeAllowAll)
	list := `{"jsonrpc":"2.0","id":1,"method":"tools/list"}`
	write := `{"jsonrpc":"2.0","id":2,"method":"tools/call","params":{"name":"sftp_write","arguments":{"session":"host_web","path":"allow-all-test.txt","content":"test content"}}}`
	remove := `{"jsonrpc":"2.0","id":3,"method":"tools/call","params":{"name":"sftp_remove","arguments":{"session":"host_web","path":"allow-all-test.txt"}}}`
	// The fixture SSH server records this command; it never executes a shell.
	run := `{"jsonrpc":"2.0","id":4,"method":"tools/call","params":{"name":"terminal_exec","arguments":{"session":"host_web","command":"rm -rf /tmp/motty-test-only"}}}`
	replies := serveAgent(t, world.options, list, write, run)
	if !strings.Contains(replies[1], "sftp_write") || !strings.Contains(replies[1], "sftp_remove") || strings.Contains(replies[1], "terminal_read") {
		t.Fatalf("wrong full-access tool catalog: %s", replies[1])
	}
	if strings.Contains(replies[2], `"isError":true`) || strings.Contains(replies[4], `"isError":true`) {
		t.Fatalf("full access refused a tool: write=%s command=%s", replies[2], replies[4])
	}
	path := filepath.Join(world.server.root, "allow-all-test.txt")
	if content, err := os.ReadFile(path); err != nil || string(content) != "test content" {
		t.Fatalf("SFTP did not write the file: %q %v", content, err)
	}
	if commands := world.server.ran(); len(commands) != 1 || commands[0] != "rm -rf /tmp/motty-test-only" {
		t.Fatalf("the command did not reach SSH: %v", commands)
	}
	replies = serveAgent(t, world.options, remove)
	if strings.Contains(replies[3], `"isError":true`) {
		t.Fatalf("remove was refused: %s", replies[3])
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("SFTP did not remove the file: %v", err)
	}
	setMode(AgentModeCommands)
	replies = serveAgent(t, world.options, list, write, run)
	if strings.Contains(replies[1], "sftp_write") || !strings.Contains(replies[2], "unknown tool") || !strings.Contains(replies[4], `"isError":true`) {
		t.Fatalf("revoking full access did not restore restrictions: %+v", replies)
	}
	if len(world.server.ran()) != 1 {
		t.Fatal("a command requiring approval ran after full access was revoked")
	}
	setMode(AgentModeAllowAll)
	saveAgentState(t, world, false)
	replies = serveAgent(t, world.options, write)
	if !strings.Contains(replies[2], `"isError":true`) {
		t.Fatal("full access bypassed host authorization")
	}
	if _, err := os.Stat(path); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("a disallowed host was modified")
	}
}

func TestServeAgentMCPRefusesWhatItCannotReadOrRecord(t *testing.T) {
	world := newAgentWorld(t)
	var out strings.Builder
	err := ServeAgentMCP(context.Background(), strings.NewReader(""), &out, world.options)
	if err == nil || !strings.Contains(err.Error(), "no saved hosts") {
		t.Fatalf("without a configuration store: %v", err)
	}

	saveAgentState(t, world, true)
	if err := os.WriteFile(world.options.AIDatabasePath, []byte(strings.Repeat("not a database ", 20)), 0o600); err != nil {
		t.Fatal(err)
	}
	err = ServeAgentMCP(context.Background(), strings.NewReader(""), &out, world.options)
	if err == nil || !strings.Contains(err.Error(), "audit log") {
		t.Fatalf("with an audit log that cannot be opened: %v", err)
	}
}

func newTestEd25519() (ed25519.PublicKey, ed25519.PrivateKey, error) {
	return ed25519.GenerateKey(rand.Reader)
}

func newTestSigner(t *testing.T) ssh.Signer {
	t.Helper()
	_, private, err := newTestEd25519()
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(private)
	if err != nil {
		t.Fatal(err)
	}
	return signer
}
