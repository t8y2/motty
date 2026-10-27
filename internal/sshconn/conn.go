// Package sshconn dials an SSH server, optionally through SSH jump hosts, and
// walks every endpoint through host-key verification and authentication.
// The connection runs on its own goroutine; the UI reads Status snapshots and
// answers prompts through methods, so the window thread never blocks on the
// network.
package sshconn

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"motty/internal/x11fwd"
)

// Phase is where a connection is in its life.
type Phase uint8

const (
	// PhaseConnecting covers the TCP dial and the key exchange.
	PhaseConnecting Phase = iota
	// PhaseHostKeyPrompt waits for the user to trust or reject an unknown key.
	PhaseHostKeyPrompt
	// PhaseAuthenticating covers key and agent attempts, no input needed.
	PhaseAuthenticating
	// PhaseKeyPassphrasePrompt waits for an encrypted private-key passphrase.
	PhaseKeyPassphrasePrompt
	// PhasePasswordPrompt waits for the account password.
	PhasePasswordPrompt
	// PhaseConnected means the server accepted the credentials.
	PhaseConnected
	// PhaseFailed is terminal; Status.Err says why.
	PhaseFailed
	// PhaseClosed is terminal after a connected session ended or was closed.
	PhaseClosed
)

// Errors a connection can end with. Test with errors.Is.
var (
	ErrCanceled         = errors.New("sshconn: canceled")
	ErrHostKeyRejected  = errors.New("sshconn: host key rejected")
	ErrHostKeyChanged   = errors.New("sshconn: host key changed")
	ErrAuthFailed       = errors.New("sshconn: authentication failed")
	ErrDisconnected     = errors.New("sshconn: disconnected")
	ErrKeepAliveTimeout = errors.New("sshconn: keepalive timed out")
)

// maxPasswordAttempts is how many passwords the user may try per method.
const maxPasswordAttempts = 3

// HostKeyInfo describes the key a server presented.
type HostKeyInfo struct {
	// Address is the host:port the key was presented for.
	Address string
	// Type is the key algorithm, such as ssh-ed25519.
	Type string
	// Fingerprint is the SHA256 fingerprint in the form OpenSSH prints.
	Fingerprint string
	// Known lists the fingerprints on file for this address when the key
	// changed; it is empty for a first connection.
	Known []string
	// File is the known-hosts file that holds (or will hold) the entry.
	File string
}

// Endpoint identifies one SSH account.
type Endpoint struct {
	Host string
	Port uint16
	User string
}

// Authentication describes which credentials one endpoint may use. A zero
// Methods value enables every supported SSH method; identity files and agent
// use remain explicit fields.
type Authentication struct {
	// Methods limits the SSH authentication methods offered. Zero enables every
	// supported method for compatibility with callers that do not set a policy.
	Methods AuthMethods
	// IdentityFiles lists private keys to try. Nil means the default OpenSSH
	// identities; an empty slice disables identity files.
	IdentityFiles []string
	// AdditionalIdentityFiles are tried after IdentityFiles, or after the
	// default identities when IdentityFiles is nil.
	AdditionalIdentityFiles []string
	// Agent controls whether the running ssh-agent is offered.
	Agent bool
}

// AuthMethods is a bit set of SSH authentication methods.
type AuthMethods uint8

const (
	AuthPublicKey AuthMethods = 1 << iota
	AuthKeyboardInteractive
	AuthPassword
	AuthAll = AuthPublicKey | AuthKeyboardInteractive | AuthPassword
)

// EndpointRole says which endpoint currently owns the connection status.
type EndpointRole uint8

const (
	EndpointTarget EndpointRole = iota
	EndpointProxyJump
)

// Question is one prompt a server asks during keyboard-interactive
// authentication. The plain password method is one secret question without
// wording.
type Question struct {
	// Text is the server's own wording.
	Text string
	// Secret marks an answer that must not be shown as it is typed.
	Secret bool
}

// Status is a consistent snapshot of a connection.
type Status struct {
	// Version increases with every change, so a reader can tell cheaply
	// whether anything happened since it last looked.
	Version uint64
	Phase   Phase
	// Endpoint is the account currently being connected or authenticated.
	Endpoint Endpoint
	// EndpointRole distinguishes the final target from its jump hosts.
	EndpointRole EndpointRole
	// Attempt counts password prompts, starting at 1. Above 1 the previous
	// password was refused.
	Attempt int
	// Questions are the prompts the server is waiting for, in order. The
	// plain password method is one secret question with no wording.
	Questions []Question
	// IdentityPath is the encrypted private key awaiting a passphrase.
	IdentityPath string
	HostKey      HostKeyInfo
	Banner       string
	// ServerVersion is the server's identification string once known.
	ServerVersion string
	// Method names how the user authenticated, once connected.
	Method string
	// System is detected after authentication without writing to the user's
	// interactive shell. It remains unknown when command sessions are refused.
	System RemoteSystem
	// Negotiated is what the last endpoint agreed on, once its handshake is done.
	Negotiated Negotiated
	// Skipped names identity files the user chose not to unlock.
	Skipped []string
	// Err describes a failed connection attempt or an unexpected end to an
	// established connection. It is nil when Close or Cancel ended the session.
	Err error
}

// KeepAliveConfig probes an established SSH connection for liveness. A zero
// value is disabled and starts no timer or goroutine.
type KeepAliveConfig struct {
	// Interval is the idle time between probes. A positive value enables
	// keepalive; zero disables it.
	Interval time.Duration
	// Timeout bounds each probe response. It must be positive when Interval is.
	Timeout time.Duration
}

// Config describes one connection attempt.
type Config struct {
	Host string
	Port uint16
	User string
	// ProxyJumps routes the target connection through independently verified and
	// authenticated SSH endpoints, ordered outermost to innermost. An empty slice
	// connects directly.
	ProxyJumps []Endpoint
	// KnownHostsPath is the app-owned file that remembers trusted keys. It is
	// created on first use.
	KnownHostsPath string
	// IdentityFiles lists private keys to try. Nil means ~/.ssh/id_ed25519,
	// id_ecdsa, and id_rsa when they exist; an empty slice disables defaults.
	IdentityFiles []string
	// AdditionalIdentityFiles are tried after IdentityFiles (or the default
	// identities when IdentityFiles is nil).
	AdditionalIdentityFiles []string
	// Agent controls whether the running ssh-agent is offered. It is ignored
	// when SSH_AUTH_SOCK is unset.
	Agent bool
	// Authentication overrides the legacy identity, agent and password policy
	// for individual endpoints. An endpoint not in the map uses the fields
	// above and every supported authentication method.
	Authentication map[Endpoint]Authentication
	// Proxy, when set and not bypassed for the destination, carries the first
	// TCP connection: to the target, or to the first jump host.
	Proxy *Proxy
	// DialTimeout bounds each TCP connect, including connects made through jump
	// hosts. Zero disables the timeout.
	DialTimeout time.Duration
	// KeepAlive optionally detects an unresponsive established connection.
	KeepAlive KeepAliveConfig
	// Algorithms sets what each endpoint negotiates, keyed by "host:port" as
	// Endpoint formats it. An endpoint without an entry keeps the defaults.
	Algorithms map[string]Algorithms
	// Notify is called from the connection goroutine after every change of
	// Status. It must be safe to call from any goroutine and must not block.
	Notify func()
}

// Conn is one SSH connection. Methods are safe for concurrent use.
type Conn struct {
	config Config
	ctx    context.Context
	cancel context.CancelFunc
	done   chan struct{}

	mu      sync.Mutex
	status  Status
	pending *prompt
	client  *ssh.Client
	// attempts counts password prompts across authentication methods.
	attempts   int
	lastMethod string
	closers    []io.Closer
	// disconnectErr records why an established connection was force-closed.
	disconnectErr error

	// x11 serves the X11 channels of the current client for every tab on it.
	x11mu     sync.Mutex
	x11client *ssh.Client
	x11       *x11fwd.Hub

	// agentClient is the local ssh-agent, dialed once for authentication and
	// agent forwarding alike; agentForwardClient is the client whose
	// "auth-agent@openssh.com" channels are already being served from it.
	agentmu            sync.Mutex
	agentClient        agent.ExtendedAgent
	agentForwardClient *ssh.Client

	// A connection can carry several tabs (see Share). The Conn that dialed is
	// the owner: it holds the client and the goroutine, and lives on, retired,
	// while shares remain after its own tab has closed. A shared Conn has the
	// owner it borrows from and no goroutine.
	owner      *Conn
	shares     []*Conn
	retired    bool
	terminated bool
	doneOnce   sync.Once
}

// finish closes the done channel once, whichever of the goroutine ending and
// the tab letting go comes first.
func (conn *Conn) finish() { conn.doneOnce.Do(func() { close(conn.done) }) }

// prompt is a question the connection goroutine is blocked on.
type prompt struct {
	phase Phase
	reply chan reply
}

type reply struct {
	accept bool
	save   bool
	// answers are the replies to Questions, in order; a key passphrase is one.
	answers []string
}

// Dial starts connecting in the background and returns at once.
func Dial(config Config) *Conn {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &Conn{config: config, ctx: ctx, cancel: cancel, done: make(chan struct{})}
	go conn.run()
	return conn
}

// Status returns the current snapshot.
func (conn *Conn) Status() Status {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.status
}

// Done is closed once the connection goroutine has finished, which is when the
// connection failed, was closed, or its session ended.
func (conn *Conn) Done() <-chan struct{} { return conn.done }

// Client returns the authenticated SSH client, or nil before PhaseConnected.
func (conn *Conn) Client() *ssh.Client {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.client
}

// DialContext opens one stream through the authenticated SSH connection. It
// fails before PhaseConnected and observes ctx while the channel is opening.
func (conn *Conn) DialContext(ctx context.Context, network, address string) (net.Conn, error) {
	client := conn.Client()
	if client == nil {
		return nil, errors.New("sshconn: not connected")
	}
	return client.DialContext(ctx, network, address)
}

// Listen asks the authenticated SSH server to open one remote listener.
func (conn *Conn) Listen(network, address string) (net.Listener, error) {
	client := conn.Client()
	if client == nil {
		return nil, errors.New("sshconn: not connected")
	}
	return client.Listen(network, address)
}

// TrustHostKey answers a host-key prompt by accepting the key. With save the
// key is remembered; without it the key is trusted for this connection only.
func (conn *Conn) TrustHostKey(save bool) {
	conn.answer(PhaseHostKeyPrompt, reply{accept: true, save: save})
}

// RejectHostKey answers a host-key prompt by refusing the key.
func (conn *Conn) RejectHostKey() {
	conn.answer(PhaseHostKeyPrompt, reply{})
}

// SubmitAnswers answers the open keyboard-interactive or password prompt with
// one answer per question, in the order the server asked them.
func (conn *Conn) SubmitAnswers(answers []string) {
	conn.answer(PhasePasswordPrompt, reply{accept: true, answers: append([]string(nil), answers...)})
}

// SubmitPassword answers a password prompt, which is one question.
func (conn *Conn) SubmitPassword(password string) {
	conn.SubmitAnswers([]string{password})
}

// SubmitKeyPassphrase answers an encrypted private-key prompt.
func (conn *Conn) SubmitKeyPassphrase(passphrase string) {
	conn.answer(PhaseKeyPassphrasePrompt, reply{accept: true, answers: []string{passphrase}})
}

// SkipIdentity declines the encrypted private key currently awaiting a
// passphrase and continues with the remaining authentication methods.
func (conn *Conn) SkipIdentity() {
	conn.answer(PhaseKeyPassphrasePrompt, reply{})
}

// Cancel starts ending the connection and answers any open prompt as
// canceled. It returns at once, so the UI thread can call it; Done reports when
// the goroutine has finished. A connection that other tabs share is not ended
// for them: this tab lets go of it, and the last to do so ends it.
func (conn *Conn) Cancel() {
	if conn.letGo() {
		return
	}
	conn.cancel()
}

// Close is Cancel that also waits for the connection goroutine, so it must not
// be called from Notify.
func (conn *Conn) Close() {
	conn.Cancel()
	<-conn.done
}

// answer delivers a reply to the prompt of the given phase, if one is open.
func (conn *Conn) answer(phase Phase, response reply) {
	conn.mu.Lock()
	open := conn.pending
	if open == nil || open.phase != phase {
		conn.mu.Unlock()
		return
	}
	conn.pending = nil
	conn.mu.Unlock()
	open.reply <- response // buffered, never blocks
}

// ask publishes a prompt and blocks until it is answered or the connection is
// closed. fill sets the status fields the UI needs to show the prompt.
func (conn *Conn) ask(phase Phase, fill func(*Status)) (reply, error) {
	open := &prompt{phase: phase, reply: make(chan reply, 1)}
	conn.mu.Lock()
	conn.pending = open
	conn.status.Version++
	conn.status.Phase = phase
	conn.status.Questions = nil
	if fill != nil {
		fill(&conn.status)
	}
	conn.mu.Unlock()
	conn.notify()

	select {
	case response := <-open.reply:
		return response, nil
	case <-conn.ctx.Done():
		conn.mu.Lock()
		if conn.pending == open {
			conn.pending = nil
		}
		conn.mu.Unlock()
		return reply{}, ErrCanceled
	}
}

// update mutates the status under the lock and tells the UI.
func (conn *Conn) update(change func(*Status)) {
	conn.mu.Lock()
	change(&conn.status)
	conn.status.Version++
	conn.mu.Unlock()
	conn.notify()
}

func (conn *Conn) notify() {
	if conn.config.Notify != nil {
		conn.config.Notify()
	}
}

func (endpoint Endpoint) normalized() Endpoint {
	if endpoint.Port == 0 {
		endpoint.Port = 22
	}
	return endpoint
}

// EndpointKey is how Config.Algorithms names an endpoint.
func EndpointKey(host string, port uint16) string {
	return net.JoinHostPort(host, strconv.Itoa(int(port)))
}

func (endpoint Endpoint) address() string {
	return net.JoinHostPort(endpoint.Host, strconv.Itoa(int(endpoint.Port)))
}

func (conn *Conn) run() {
	defer conn.finish()
	defer conn.closeAuxiliary()
	err := conn.connect()
	conn.mu.Lock()
	client := conn.client
	conn.mu.Unlock()
	if client != nil {
		// Connected: hold the connection until it ends or Close is called.
		stop := context.AfterFunc(conn.ctx, func() { client.Close() })
		keepAlive := conn.startKeepAlive(client)
		waitErr := client.Wait()
		stop()
		if keepAlive != nil {
			close(keepAlive.stop)
			<-keepAlive.done
		}
		closedErr := conn.closedError(waitErr)
		conn.update(func(status *Status) {
			status.Phase = PhaseClosed
			status.Err = closedErr
		})
		conn.endShares(closedErr)
		return
	}
	if conn.ctx.Err() != nil && !errors.Is(err, ErrHostKeyChanged) {
		err = ErrCanceled
	}
	conn.update(func(status *Status) {
		status.Phase = PhaseFailed
		status.Err = err
	})
}

func (conn *Conn) connect() error {
	if err := conn.config.validate(); err != nil {
		return err
	}
	target := Endpoint{Host: conn.config.Host, Port: conn.config.Port, User: conn.config.User}.normalized()
	if target.Host == "" {
		return errors.New("sshconn: no host")
	}
	known, err := newHostKeys(conn.config.KnownHostsPath)
	if err != nil {
		return err
	}

	var previous *ssh.Client
	var previousEndpoint Endpoint
	for index, configured := range conn.config.ProxyJumps {
		jump := configured.normalized()
		if jump.Host == "" {
			return fmt.Errorf("proxy jump %d: no host", index+1)
		}
		var jumpClient *ssh.Client
		if previous == nil {
			jumpClient, err = conn.dialEndpoint(jump, EndpointProxyJump, known)
		} else {
			jumpClient, err = conn.dialEndpointThrough(previous, previousEndpoint, jump, known)
		}
		if err != nil {
			return fmt.Errorf("proxy jump %d %s: %w", index+1, jump.address(), err)
		}
		conn.addCloser(jumpClient)
		previous = jumpClient
		previousEndpoint = jump
	}

	conn.beginEndpoint(target, EndpointTarget)
	var socket net.Conn
	if previous != nil {
		socket, err = conn.dialThrough(previous, target.address())
		if err != nil {
			return fmt.Errorf("connect to %s through %s: %w", target.address(), previousEndpoint.address(), err)
		}
	} else {
		socket, err = conn.dialTCP(target.address())
		if err != nil {
			return fmt.Errorf("connect to %s: %w", target.address(), err)
		}
	}
	client, serverVersion, err := conn.handshake(target, socket, known)
	if err != nil {
		return err
	}
	conn.mu.Lock()
	conn.client = client
	method := conn.lastMethod
	conn.mu.Unlock()
	conn.update(func(status *Status) {
		status.Phase = PhaseConnected
		status.ServerVersion = serverVersion
		status.Method = method
		status.System = classifyBanner(serverVersion)
	})
	if system := conn.Status().System; system == SystemUnknown || system == SystemUbuntu || system == SystemDebian {
		go conn.detectSystem(client, serverVersion)
	}
	return nil
}

func (conn *Conn) detectSystem(client *ssh.Client, serverVersion string) {
	system := detectRemoteSystem(client, serverVersion)
	if system == SystemUnknown || conn.ctx.Err() != nil {
		return
	}
	conn.mu.Lock()
	if conn.status.Phase != PhaseConnected || conn.status.System == system {
		conn.mu.Unlock()
		return
	}
	conn.status.System = system
	conn.status.Version++
	conn.mu.Unlock()
	conn.notify()
}

// dialTCP opens the first connection of a route: straight to address, or through
// the outbound proxy when one is set for it.
func (conn *Conn) dialTCP(address string) (net.Conn, error) {
	if conn.config.Proxy.Uses(address) {
		return conn.config.Proxy.Dial(conn.ctx, conn.config.DialTimeout, address)
	}
	dialer := net.Dialer{Timeout: conn.config.DialTimeout}
	return dialer.DialContext(conn.ctx, "tcp", address)
}

func (conn *Conn) dialEndpoint(endpoint Endpoint, role EndpointRole, known *hostKeys) (*ssh.Client, error) {
	conn.beginEndpoint(endpoint, role)
	socket, err := conn.dialTCP(endpoint.address())
	if err != nil {
		return nil, fmt.Errorf("connect to %s: %w", endpoint.address(), err)
	}
	client, _, err := conn.handshake(endpoint, socket, known)
	return client, err
}

func (conn *Conn) dialEndpointThrough(previous *ssh.Client, previousEndpoint, endpoint Endpoint, known *hostKeys) (*ssh.Client, error) {
	conn.beginEndpoint(endpoint, EndpointProxyJump)
	socket, err := conn.dialThrough(previous, endpoint.address())
	if err != nil {
		return nil, fmt.Errorf("connect to %s through %s: %w", endpoint.address(), previousEndpoint.address(), err)
	}
	client, _, err := conn.handshake(endpoint, socket, known)
	return client, err
}

func (conn *Conn) beginEndpoint(endpoint Endpoint, role EndpointRole) {
	conn.mu.Lock()
	conn.attempts = 0
	conn.lastMethod = ""
	conn.status.Version++
	conn.status.Phase = PhaseConnecting
	conn.status.Endpoint = endpoint
	conn.status.EndpointRole = role
	conn.status.Attempt = 0
	conn.status.Questions = nil
	conn.status.IdentityPath = ""
	conn.status.HostKey = HostKeyInfo{}
	conn.status.Banner = ""
	conn.status.ServerVersion = ""
	conn.status.Method = ""
	conn.status.System = SystemUnknown
	conn.status.Skipped = nil
	conn.status.Err = nil
	conn.mu.Unlock()
	conn.notify()
}

func (conn *Conn) handshake(endpoint Endpoint, socket net.Conn, known *hostKeys) (*ssh.Client, string, error) {
	address := endpoint.address()
	wanted := conn.config.Algorithms[address]
	authentication := conn.authentication(endpoint)
	clientConfig := &ssh.ClientConfig{
		Config: ssh.Config{KeyExchanges: wanted.KeyExchanges, Ciphers: wanted.Ciphers, MACs: wanted.MACs},
		User:   endpoint.User,
		Auth:   conn.authMethods(authentication),
		HostKeyCallback: func(hostname string, remote net.Addr, key ssh.PublicKey) error {
			return conn.verifyHostKey(known, hostname, remote, key)
		},
		BannerCallback: func(message string) error {
			conn.update(func(status *Status) { status.Banner = message })
			return nil
		},
		HostKeyAlgorithms: hostKeyAlgorithms(known.algorithms(address), wanted.HostKeys),
		ClientVersion:     "SSH-2.0-Motty",
	}

	// Closing the socket is the only way to interrupt a handshake that is
	// blocked on the network or on a prompt.
	stop := context.AfterFunc(conn.ctx, func() { socket.Close() })
	defer stop()

	sshConn, channels, requests, err := ssh.NewClientConn(socket, address, clientConfig)
	if err != nil {
		socket.Close()
		return nil, "", conn.explain(err)
	}
	if negotiated, ok := negotiatedFrom(sshConn); ok {
		conn.update(func(status *Status) { status.Negotiated = negotiated })
	}
	return ssh.NewClient(sshConn, channels, requests), string(sshConn.ServerVersion()), nil
}

func (conn *Conn) authentication(endpoint Endpoint) Authentication {
	if authentication, ok := conn.config.Authentication[endpoint.normalized()]; ok {
		return authentication
	}
	return Authentication{
		Methods:                 AuthAll,
		IdentityFiles:           conn.config.IdentityFiles,
		AdditionalIdentityFiles: conn.config.AdditionalIdentityFiles,
		Agent:                   conn.config.Agent,
	}
}

// explain turns a handshake error into one of the package errors when it is
// one, so callers can branch on errors.Is.
func (conn *Conn) explain(err error) error {
	conn.mu.Lock()
	status := conn.status
	conn.mu.Unlock()
	switch {
	case errors.Is(err, ErrCanceled), errors.Is(err, ErrHostKeyRejected), errors.Is(err, ErrHostKeyChanged):
		return err
	case status.Attempt >= maxPasswordAttempts || containsAuthFailure(err):
		return fmt.Errorf("%w: %v", ErrAuthFailed, err)
	}
	return err
}

func containsAuthFailure(err error) bool {
	message := err.Error()
	return strings.Contains(message, "unable to authenticate") || strings.Contains(message, "no supported methods remain")
}

func (conn *Conn) addCloser(closer io.Closer) {
	conn.mu.Lock()
	conn.closers = append(conn.closers, closer)
	conn.mu.Unlock()
}

// closeAuxiliary releases jump connections from innermost to outermost.
func (conn *Conn) closeAuxiliary() {
	conn.mu.Lock()
	closers := conn.closers
	conn.closers = nil
	conn.mu.Unlock()
	for index := len(closers) - 1; index >= 0; index-- {
		closers[index].Close()
	}
}
