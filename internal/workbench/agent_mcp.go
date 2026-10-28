package workbench

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/ai/gate"
	"motty/internal/ai/mcp"
	"motty/internal/ai/risk"
	"motty/internal/ai/store"
	"motty/internal/ai/tools"
	"motty/internal/configstore"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	"motty/internal/vault"
)

// This file is the MCP server that runs without a window: `motty mcp-serve`.
// An MCP client starts it as a child process and talks to it over stdio. It
// reads what the application saved (the hosts, the vault, the trusted host keys)
// and works on the hosts the user opened to agents, over connections of its own.
// It never writes to the application's files except the audit log, never asks a
// person anything, and ends with its client.

const (
	// agentModeSetting is the stdio access level in ai.db: observe, ask, or
	// allow-all. Unknown values remain read-only.
	agentModeSetting = "ai.mcp.stdio.mode"
	// AgentModeCommands is the value of that setting that allows commands.
	AgentModeCommands = "ask"
	// AgentModeReadOnly is the value that allows only reading.
	AgentModeReadOnly = "observe"
	// AgentModeAllowAll allows all headless tools after explicit UI consent.
	AgentModeAllowAll = "allow-all"

	agentIdleDefault   = 2 * time.Minute
	agentDialLimit     = 60 * time.Second
	agentCallTimeout   = 12 * time.Minute // a command may take tools.MaxTimeoutSeconds, and dialing comes first
	agentAuditRetained = 30 * 24 * time.Hour
)

// AgentOptions locates what the stdio MCP server reads. An empty path means the
// application's own place for it: under the user's configuration directory, next
// to the known-hosts file.
type AgentOptions struct {
	KnownHostsPath string
	StatePath      string
	AIDatabasePath string
	VaultPath      string
	// Keystore holds the vault's data key; nil is the system's.
	Keystore vault.Keystore
	// Idle is how long a connection nobody is using stays open; zero is two
	// minutes.
	Idle time.Duration
	// Log receives what the server has to say about itself; nil discards it.
	Log io.Writer
}

func (options AgentOptions) withDefaults() AgentOptions {
	if options.KnownHostsPath == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		options.KnownHostsPath = filepath.Join(dir, "motty", "known_hosts")
	}
	dir := filepath.Dir(options.KnownHostsPath)
	if options.StatePath == "" {
		options.StatePath = filepath.Join(dir, "config.db")
	}
	if options.AIDatabasePath == "" {
		options.AIDatabasePath = filepath.Join(dir, "ai.db")
	}
	if options.VaultPath == "" {
		options.VaultPath = filepath.Join(filepath.Dir(options.StatePath), "vault.json")
	}
	if options.Keystore == nil {
		options.Keystore = systemKeystore{}
	}
	if options.Idle <= 0 {
		options.Idle = agentIdleDefault
	}
	if options.Log == nil {
		options.Log = io.Discard
	}
	return options
}

// ServeAgentMCP serves the hosts the user opened to agents to an MCP client on
// in and out, until in ends or ctx does. It fails before serving anything when
// the saved hosts cannot be read, or when the audit log cannot be opened: an
// agent is not served unrecorded.
func ServeAgentMCP(ctx context.Context, in io.Reader, out io.Writer, options AgentOptions) error {
	options = options.withDefaults()
	catalog, err := readAgentCatalog(options.StatePath)
	if err != nil {
		return err
	}
	log, err := store.Open(options.AIDatabasePath)
	if err != nil {
		return fmt.Errorf("cannot open the audit log, so no agent is served: %w", err)
	}
	defer log.Close()
	_, _ = log.PruneAudit(time.Now().Add(-agentAuditRetained))

	value, _, _ := log.Setting(agentModeSetting)
	mode := mcpModeFromSetting(value)
	backend := newAgentBackend(catalog, options)
	backend.allowWrites = mode == gate.ModeAllowAll
	defer backend.Close()
	if len(backend.hosts) == 0 {
		fmt.Fprintln(options.Log, "motty mcp-serve: no host is open to agents; turn it on in the host's settings in Motty")
	}
	// Only the separately confirmed full-access mode skips approvals. In other
	// modes nothing can approve a call, so policy requests remain refusals.
	agentGate := gate.Gate{Backend: backend, Policy: risk.Policy{Enabled: true, AutoUpTo: risk.Low}, Mode: mode}
	instructions := "Tools to work on the saved hosts the user opened to you in Motty. Call list_sessions first to get a host id. " +
		"A host connects when you first use it. Commands run without a terminal and keep no state between calls. "
	if mode == gate.ModeAllowAll {
		instructions += "The user enabled full access for these hosts. Commands and file changes run without approval."
	} else {
		instructions += "Nobody can approve anything here, so a call that would need approval is refused."
	}
	return mcp.ServeStdio(ctx, in, out, mcp.Config{
		Tools:        tools.Headless(mode != gate.ModeObserve, mode == gate.ModeAllowAll),
		Dispatch:     backend.audited(agentGate, log),
		ServerName:   "motty",
		Version:      "1",
		CallTimeout:  agentCallTimeout,
		Instructions: instructions,
	})
}

// readAgentCatalog reads the saved state without changing it and builds the
// catalog of the hosts in it.
func readAgentCatalog(statePath string) (hostCatalog, error) {
	entries, local, err := configstore.ReadOnly(statePath, configFormat)
	var tooNew *configstore.TooNewError
	switch {
	case errors.Is(err, os.ErrNotExist):
		return hostCatalog{}, fmt.Errorf("no saved hosts at %s: start Motty once and add a host", statePath)
	case errors.As(err, &tooNew):
		return hostCatalog{}, fmt.Errorf("the saved hosts were written by a newer Motty (%s); update Motty", tooNew.Found)
	case err != nil:
		return hostCatalog{}, fmt.Errorf("cannot read the saved hosts: %w", err)
	}
	saved, _, err := joinState(entries, local[localKey])
	if err != nil {
		return hostCatalog{}, fmt.Errorf("the saved hosts are damaged: %w", err)
	}
	return catalogFromSaved(saved, systemLogin(), keyMessagesIn(i18n.For(i18n.English))), nil
}

// agentBackend is gate.Backend over the hosts opened to agents. A host connects
// the first time a call needs it and stays connected while calls keep coming.
type agentBackend struct {
	catalog hostCatalog
	hosts   []Host
	options AgentOptions

	mu          sync.Mutex
	entries     map[string]*agentEntry
	closed      bool
	allowWrites bool // immutable after startup, before requests are accepted
}

var _ gate.Backend = (*agentBackend)(nil)

func newAgentBackend(catalog hostCatalog, options AgentOptions) *agentBackend {
	return &agentBackend{
		catalog: catalog,
		hosts:   catalog.agentHostList(),
		options: options,
		entries: make(map[string]*agentEntry),
	}
}

// agentEntry is one host's connection and what it has open.
type agentEntry struct {
	backend *agentBackend
	host    Host

	mu     sync.Mutex // guards everything below, and is held while dialing
	conn   *sshconn.Conn
	remote sftpclient.Remote
	active int // calls using the connection
	timer  *time.Timer
}

// Close ends every connection.
func (b *agentBackend) Close() {
	b.mu.Lock()
	b.closed = true
	entries := make([]*agentEntry, 0, len(b.entries))
	for _, entry := range b.entries {
		entries = append(entries, entry)
	}
	b.mu.Unlock()
	for _, entry := range entries {
		entry.mu.Lock()
		entry.stopIdle()
		entry.drop()
		entry.mu.Unlock()
	}
}

var (
	errAgentNoHost     = errors.New("no such host; call list_sessions")
	errAgentWhichHost  = errors.New("name a host: more than one is open to you; call list_sessions")
	errAgentNoTerminal = errors.New("these hosts have no terminal screen to read")
	errAgentNotOffered = errors.New("this server does not offer that")
	errAgentClosed     = errors.New("the server is shutting down")
)

// resolve finds the host a handle names: its id, or its label when that is
// unambiguous. An empty handle is the only host there is.
func (b *agentBackend) resolve(handle string) (Host, error) {
	if handle == "" {
		if len(b.hosts) == 1 {
			return b.hosts[0], nil
		}
		if len(b.hosts) == 0 {
			return Host{}, errAgentNoHost
		}
		return Host{}, errAgentWhichHost
	}
	for _, host := range b.hosts {
		if host.CatalogID == handle {
			return host, nil
		}
	}
	var found []Host
	for _, host := range b.hosts {
		if host.Label == handle {
			found = append(found, host)
		}
	}
	if len(found) == 1 {
		return found[0], nil
	}
	return Host{}, errAgentNoHost
}

func (b *agentBackend) entryFor(host Host) (*agentEntry, error) {
	b.mu.Lock()
	defer b.mu.Unlock()
	if b.closed {
		return nil, errAgentClosed
	}
	entry := b.entries[host.CatalogID]
	if entry == nil {
		entry = &agentEntry{backend: b, host: host}
		b.entries[host.CatalogID] = entry
	}
	return entry, nil
}

// acquire returns the host's connection, dialing it if there is none, and holds
// it open until release.
func (b *agentBackend) acquire(ctx context.Context, handle string) (*agentEntry, error) {
	host, err := b.resolve(handle)
	if err != nil {
		return nil, err
	}
	entry, err := b.entryFor(host)
	if err != nil {
		return nil, err
	}
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.conn == nil || entry.conn.Status().Phase != sshconn.PhaseConnected {
		entry.drop()
		conn, err := b.dial(ctx, host)
		if err != nil {
			return nil, err
		}
		entry.conn = conn
	}
	entry.active++
	entry.stopIdle()
	return entry, nil
}

func (entry *agentEntry) release() {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	entry.active--
	if entry.active == 0 && entry.conn != nil {
		entry.timer = time.AfterFunc(entry.backend.options.Idle, entry.expire)
	}
}

// expire closes a connection nobody has used for the idle time.
func (entry *agentEntry) expire() {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.active == 0 {
		entry.drop()
	}
}

func (entry *agentEntry) stopIdle() {
	if entry.timer != nil {
		entry.timer.Stop()
		entry.timer = nil
	}
}

// drop closes the connection and the file client on it. The caller holds mu.
func (entry *agentEntry) drop() {
	if entry.remote != nil {
		_ = entry.remote.Close()
		entry.remote = nil
	}
	if entry.conn != nil {
		entry.conn.Close()
		entry.conn = nil
	}
}

// files returns the host's file client, opening it on first use.
func (entry *agentEntry) files() (sftpclient.Remote, error) {
	entry.mu.Lock()
	defer entry.mu.Unlock()
	if entry.remote != nil {
		return entry.remote, nil
	}
	if entry.conn == nil {
		return nil, errors.New("the connection to the host closed")
	}
	remote, err := openRemoteFiles(realConn{entry.conn}, entry.host.Transfer)
	if err != nil {
		return nil, err
	}
	entry.remote = remote
	return remote, nil
}

// dropFiles forgets a file client that failed, so the next call opens another.
func (entry *agentEntry) dropFiles() {
	entry.mu.Lock()
	remote := entry.remote
	entry.remote = nil
	entry.mu.Unlock()
	if remote != nil {
		_ = remote.Close()
	}
}

func (b *agentBackend) Sessions(context.Context) ([]gate.SessionInfo, error) {
	sessions := make([]gate.SessionInfo, len(b.hosts))
	for index, host := range b.hosts {
		sessions[index] = gate.SessionInfo{
			ID: host.CatalogID, Title: host.Label, Ready: true,
			Address: b.catalog.login(host) + "@" + host.Address + ":" + strconv.Itoa(int(host.Port)),
		}
	}
	return sessions, nil
}

func (b *agentBackend) ReadTerminal(context.Context, tools.TerminalReadArgs) (string, error) {
	return "", errAgentNoTerminal
}

func (b *agentBackend) Exec(ctx context.Context, args tools.TerminalExecArgs) (gate.ExecResult, error) {
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		return gate.ExecResult{}, err
	}
	defer entry.release()
	limited, cancel := context.WithTimeout(ctx, execTimeout(args.TimeoutSeconds))
	defer cancel()
	result, err := entry.conn.Exec(limited, args.Command, maxExecOutput)
	output := strings.ToValidUTF8(string(result.Output), "�")
	switch {
	case err == nil:
		return gate.ExecResult{Output: output, ExitCode: result.ExitCode, Truncated: result.Truncated}, nil
	case ctx.Err() == nil && errors.Is(err, context.DeadlineExceeded):
		return gate.ExecResult{Output: output, Truncated: result.Truncated, TimedOut: true}, nil
	}
	return gate.ExecResult{}, err
}

func (b *agentBackend) ListDir(ctx context.Context, args tools.SFTPListArgs) ([]gate.FileEntry, error) {
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		return nil, err
	}
	defer entry.release()
	remote, err := entry.files()
	if err != nil {
		return nil, err
	}
	entries, err := listRemoteDir(remote, args.Path)
	if err != nil {
		entry.dropFiles()
		return nil, err
	}
	return entries, nil
}

func (b *agentBackend) ReadFile(ctx context.Context, args tools.SFTPReadArgs) (string, bool, error) {
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		return "", false, err
	}
	defer entry.release()
	remote, err := entry.files()
	if err != nil {
		return "", false, err
	}
	text, truncated, err := readRemoteFile(remote, args.Path, args.MaxBytes)
	if err != nil {
		entry.dropFiles()
		return "", false, err
	}
	return text, truncated, nil
}

func (b *agentBackend) WriteFile(ctx context.Context, args tools.SFTPWriteArgs) error {
	if !b.allowWrites {
		return errAgentNotOffered
	}
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		return err
	}
	defer entry.release()
	remote, err := entry.files()
	if err != nil {
		return err
	}
	if err := sftpclient.WriteFile(remote, args.Path, []byte(args.Content), args.Overwrite); err != nil {
		if !errors.Is(err, sftpclient.ErrExists) {
			entry.dropFiles()
		}
		return err
	}
	return nil
}

func (b *agentBackend) Remove(ctx context.Context, args tools.SFTPRemoveArgs) error {
	if !b.allowWrites {
		return errAgentNotOffered
	}
	entry, err := b.acquire(ctx, args.Session)
	if err != nil {
		return err
	}
	defer entry.release()
	remote, err := entry.files()
	if err != nil {
		return err
	}
	if err := sftpclient.Delete(remote, args.Path); err != nil {
		entry.dropFiles()
		return err
	}
	return nil
}

// dial connects to a host and answers every question the connection asks from
// the vault. It never asks a person: an unknown host key, a password that is not
// saved, a one-time code or a locked vault ends the attempt with a message that
// says what to do in Motty.
func (b *agentBackend) dial(ctx context.Context, host Host) (*sshconn.Conn, error) {
	secrets, err := vault.Open(b.options.VaultPath, b.options.Keystore)
	if err != nil {
		secrets = nil // no saved secrets this time; key and agent logins still work
	}
	changed := make(chan struct{}, 1)
	env := connectEnv{
		KnownHostsPath: b.options.KnownHostsPath,
		Notify: func() {
			select {
			case changed <- struct{}{}:
			default:
			}
		},
		ProxyLocked: errors.New("the saved proxy password is locked; unlock the saved passwords in Motty"),
	}
	if secrets != nil {
		env.Passwords = secrets
	}
	config, _, valid := b.catalog.sshConfigFor(host, host, env)
	if !valid {
		return nil, fmt.Errorf("%s: its jump route is broken; fix it in Motty", host.Label)
	}
	conn := sshconn.Dial(config)
	limit := time.NewTimer(agentDialLimit)
	defer limit.Stop()
	var answered uint64
	for {
		status := conn.Status()
		switch status.Phase {
		case sshconn.PhaseConnected:
			return conn, nil
		case sshconn.PhaseFailed, sshconn.PhaseClosed:
			conn.Close()
			return nil, explainAgentDial(host, status)
		}
		if status.Version != answered {
			answered = status.Version
			if err := answerAgentPrompt(conn, status, secrets); err != nil {
				conn.Cancel()
				return nil, fmt.Errorf("%s: %w", host.Label, err)
			}
		}
		select {
		case <-changed:
		case <-conn.Done():
		case <-limit.C:
			conn.Cancel()
			return nil, fmt.Errorf("%s: no answer within %s", host.Label, agentDialLimit)
		case <-ctx.Done():
			conn.Cancel()
			return nil, ctx.Err()
		}
	}
}

// answerAgentPrompt answers what the connection waits on, from the vault, or
// returns why it cannot.
func answerAgentPrompt(conn *sshconn.Conn, status sshconn.Status, secrets *vault.Vault) error {
	lookup := func(name string) (string, error) {
		if secrets == nil || secrets.Len() == 0 {
			return "", nil // nothing saved: do not ask the system's store for a key
		}
		value, found, err := secrets.Get(name)
		switch {
		case errors.Is(err, vault.ErrLocked):
			return "", errors.New("the saved passwords are locked by a master password, which an agent cannot enter; unlock them in Motty and use a key, or turn the master password off")
		case err != nil || !found:
			return "", nil
		}
		return value, nil
	}
	switch status.Phase {
	case sshconn.PhaseHostKeyPrompt:
		conn.RejectHostKey()
		return fmt.Errorf("the host key %s (%s) of %s is not trusted yet; connect to it once from the Motty window to trust it",
			status.HostKey.Fingerprint, status.HostKey.Type, status.HostKey.Address)
	case sshconn.PhasePasswordPrompt:
		who := formatEndpoint(status.Endpoint)
		if status.Attempt > 1 {
			return fmt.Errorf("the saved password for %s was refused; update it in Motty", who)
		}
		if !isPlainPassword(status.Questions) {
			return fmt.Errorf("%s asks for more than a password (a one-time code?), which an agent cannot answer", who)
		}
		password, err := lookup(passwordSecretName(status.Endpoint))
		if err != nil {
			return err
		}
		if password == "" {
			return fmt.Errorf("no password is saved for %s; save one in Motty, or sign in with a key", who)
		}
		conn.SubmitPassword(password)
	case sshconn.PhaseKeyPassphrasePrompt:
		passphrase := ""
		if status.Attempt == 1 && status.IdentityPath != "" {
			var err error
			if passphrase, err = lookup(passphraseSecretName(status.IdentityPath)); err != nil {
				return err
			}
		}
		if passphrase == "" {
			conn.SkipIdentity() // the other ways to sign in are still tried
		} else {
			conn.SubmitKeyPassphrase(passphrase)
		}
	}
	return nil
}

// explainAgentDial says why a connection ended, with what to do about it.
func explainAgentDial(host Host, status sshconn.Status) error {
	switch {
	case errors.Is(status.Err, sshconn.ErrHostKeyChanged):
		return fmt.Errorf("%s: the host key changed (trusted %s, offered %s); resolve it in Motty before agents can use this host",
			host.Label, strings.Join(status.HostKey.Known, ", "), status.HostKey.Fingerprint)
	case status.Err != nil:
		return fmt.Errorf("%s: %w", host.Label, status.Err)
	}
	return fmt.Errorf("%s: the connection closed", host.Label)
}

// audited wraps the gate so that every call, refused ones too, leaves a line in
// the audit log: when, which host, which tool, the command or path, and how it
// ended. Never the output.
func (b *agentBackend) audited(agentGate gate.Gate, log *store.Store) func(context.Context, string, []byte) gate.Result {
	return func(ctx context.Context, name string, raw []byte) gate.Result {
		result := agentGate.Run(ctx, name, raw)
		var call struct {
			Session  string   `json:"session"`
			Sessions []string `json:"sessions"`
			Command  string   `json:"command"`
			Path     string   `json:"path"`
		}
		_ = json.Unmarshal(raw, &call)
		ids := call.Sessions
		if len(ids) == 0 && call.Session != "" {
			ids = []string{call.Session}
		}
		var hostIDs, labels []string
		for _, id := range ids {
			if host, err := b.resolve(id); err == nil {
				hostIDs, labels = append(hostIDs, host.CatalogID), append(labels, host.Label)
			} else {
				hostIDs, labels = append(hostIDs, id), append(labels, id)
			}
		}
		if len(ids) == 0 && name != tools.NameListSessions {
			if host, err := b.resolve(""); err == nil { // the only host there is
				hostIDs, labels = []string{host.CatalogID}, []string{host.Label}
			}
		}
		outcome := store.AuditOK
		switch {
		case result.Denied:
			outcome = store.AuditRefused
		case result.IsError:
			outcome = store.AuditFailed
		}
		summary := call.Command
		if summary == "" {
			summary = call.Path
		}
		_ = log.RecordAudit(store.AuditEntry{
			HostID: strings.Join(hostIDs, ","), HostLabel: strings.Join(labels, ", "),
			Tool: name, Summary: summary, Outcome: outcome,
		})
		return result
	}
}
