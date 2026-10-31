package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/desktop"
	"motty/internal/desktop/rdp"
	"motty/internal/desktop/vnc"
	"motty/internal/rfb"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// vncAccount is the name under which a VNC password is asked for and kept in the
// vault: "vnc@host:port". There is no user in VNC; the name keeps the secret
// apart from an SSH account on the same machine.
const vncAccount = "vnc"

// rdpAccountPrefix starts the name of an RDP account in an endpoint: "rdp:alice"
// is alice's RDP login, whose password is kept in the vault as
// "password:rdp:alice@host:port", apart from an SSH account of the same name on
// the same machine. It also tells the RDP server's own questions from those of
// the SSH connection a tunneled desktop goes through.
const rdpAccountPrefix = "rdp:"

// rdpAccount splits what the user typed as an RDP account into the user and the
// domain: "CORP\alice" is alice in CORP. A plain name, or a user principal name
// ("alice@corp.example"), has no separate domain.
func rdpAccount(account string) (user, domain string) {
	if index := strings.IndexByte(account, '\\'); index > 0 {
		return account[index+1:], account[:index]
	}
	return account, ""
}

// desktopRequest is what opening a remote desktop needs.
type desktopRequest struct {
	Host    Host
	Timeout time.Duration
	Proxy   *sshconn.Proxy
	// Tunnel, when set, is the SSH connection the server is reached through: the
	// address is then dialed from the SSH server, as its own loopback.
	Tunnel Connection
	// Ask is called when the server wants a password; see desktop.Options.
	Ask    func() (string, error)
	Notify func()
	// Width, Height and Scale are the desktop the pane asks for, in device
	// pixels and display scale; see desktop.Options.
	Width, Height int
	Scale         float32
	// Verify decides on the certificate an RDP server presents; see
	// desktop.Options.VerifyCertificate.
	Verify func(address string, der []byte) error
}

// openDesktopSession is the production opener.
func openDesktopSession(ctx context.Context, request desktopRequest) (desktop.Session, error) {
	host := request.Host
	address := net.JoinHostPort(host.Address, strconv.Itoa(int(host.Port)))
	dial := func(ctx context.Context, network, address string) (net.Conn, error) {
		if request.Tunnel != nil {
			return request.Tunnel.DialContext(ctx, network, address)
		}
		if request.Proxy.Uses(address) {
			return request.Proxy.Dial(ctx, request.Timeout, address)
		}
		dialer := net.Dialer{Timeout: request.Timeout}
		return dialer.DialContext(ctx, network, address)
	}
	if request.Tunnel != nil {
		address = net.JoinHostPort("127.0.0.1", strconv.Itoa(int(host.Port)))
	}
	options := desktop.Options{
		AskPassword: request.Ask, Notify: request.Notify, Clipboard: host.Clipboard, Encrypted: request.Tunnel != nil,
	}
	if host.Protocol == protocolRDP {
		options.Username, options.Domain = rdpAccount(host.User)
		options.Console = host.Console
		options.Width, options.Height, options.Scale = request.Width, request.Height, request.Scale
		options.VerifyCertificate = request.Verify
		// The certificate is pinned under the name the user typed, not the
		// loopback address a tunneled connection dials.
		session, err := rdp.Open(ctx, dial, address, options)
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	session, err := vnc.Open(ctx, dial, address, options)
	if err != nil {
		return nil, err // not the typed nil a Session would hold
	}
	return session, nil
}

// desktopSource is a Connection that carries a remote desktop.
type desktopSource interface {
	Desktop() desktop.Session
}

// desktopConn is the Connection of a remote-desktop tab. It connects in the
// background, so the tab shows the connection page meanwhile; asks for the
// password through the same dialog an SSH password uses; and, for a host reached
// through SSH, lets the SSH connection's questions (host key, password) through
// first. Once up it holds the desktop session for the tab to show.
type desktopConn struct {
	notify  func()
	ctx     context.Context
	cancel  context.CancelFunc
	request desktopRequest
	open    func(context.Context, desktopRequest) (desktop.Session, error)
	inner   Connection // the SSH tunnel, nil for a direct connection

	mu      sync.Mutex
	status  sshconn.Status
	session desktop.Session
	answers chan string
	asking  bool // the open question is the VNC password's, not the SSH tunnel's
	// certs keeps the certificates the user trusted; trusted holds those accepted
	// for this connection (the library connects more than once while it
	// negotiates), and certAsking says the open question is about a certificate.
	certs        *rdp.Pins
	trusted      map[string]bool
	certAnswers  chan certReply
	certAsking   bool
	certAsked    int
	attempt      int
	innerVersion uint64
	dialing      bool
	closed       bool
}

// newDesktopConn starts connecting host and returns at once. tunnel, if not nil,
// is the SSH connection to go through; its notifications must be routed to
// innerChanged by the caller (see State.newDesktopConn).
func newDesktopConn(host Host, notify func(), open func(context.Context, desktopRequest) (desktop.Session, error), timeout time.Duration, proxy *sshconn.Proxy, certs *rdp.Pins) *desktopConn {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &desktopConn{
		notify: notify, ctx: ctx, cancel: cancel, open: open, answers: make(chan string, 1),
		certs: certs, trusted: map[string]bool{}, certAnswers: make(chan certReply, 1),
	}
	conn.request = desktopRequest{Host: host, Timeout: timeout, Proxy: proxy, Ask: conn.ask, Notify: notify, Verify: conn.verifyCertificate}
	conn.status = sshconn.Status{Version: 1, Phase: sshconn.PhaseConnecting, Endpoint: conn.endpoint()}
	return conn
}

func (conn *desktopConn) endpoint() sshconn.Endpoint {
	host := conn.request.Host
	if host.Protocol == protocolRDP {
		return sshconn.Endpoint{Host: host.Address, Port: host.Port, User: rdpAccountPrefix + host.User}
	}
	return sshconn.Endpoint{Host: host.Address, Port: host.Port, User: vncAccount}
}

// certReply is the user's answer to a certificate question.
type certReply struct{ accept, save bool }

// verifyCertificate is the question an RDP server's certificate raises
// mid-handshake. A certificate the user trusted before, as the same server,
// passes; a new one, or a changed one, waits for the user, and a refusal ends
// the connection. It blocks until the question is answered or the connection
// cancelled.
func (conn *desktopConn) verifyCertificate(address string, der []byte) error {
	fingerprint := rdp.Fingerprint(der)
	conn.mu.Lock()
	known := conn.trusted[fingerprint]
	conn.mu.Unlock()
	if known {
		return nil
	}
	var previous string
	if conn.certs != nil {
		pinned, err := conn.certs.Lookup(address)
		if err != nil {
			return fmt.Errorf("reading the trusted certificates: %w", err)
		}
		if pinned == fingerprint {
			conn.mu.Lock()
			conn.trusted[fingerprint] = true
			conn.mu.Unlock()
			return nil
		}
		previous = pinned
	}
	info := sshconn.HostKeyInfo{Address: address, Type: rdpCertificateType, Fingerprint: fingerprint}
	if previous != "" {
		info.Known = []string{previous}
	}
	conn.mu.Lock()
	conn.certAsked++
	conn.certAsking = true
	conn.status = sshconn.Status{
		Version: conn.status.Version, Phase: sshconn.PhaseHostKeyPrompt, Endpoint: conn.endpoint(),
		Attempt: conn.certAsked, HostKey: info,
	}
	conn.publish()
	var reply certReply
	select {
	case reply = <-conn.certAnswers:
	case <-conn.ctx.Done():
		return sshconn.ErrCanceled
	}
	conn.mu.Lock()
	conn.certAsking = false
	conn.status = sshconn.Status{Version: conn.status.Version, Phase: sshconn.PhaseConnecting, Endpoint: conn.endpoint()}
	conn.publish()
	if !reply.accept {
		return sshconn.ErrHostKeyRejected
	}
	if reply.save && conn.certs != nil {
		if err := conn.certs.Remember(address, fingerprint); err != nil {
			return fmt.Errorf("remembering the certificate: %w", err)
		}
	}
	conn.mu.Lock()
	conn.trusted[fingerprint] = true
	conn.mu.Unlock()
	return nil
}

// rdpCertificateType is what the host-key dialog calls an RDP server's key.
const rdpCertificateType = "RDP certificate (X.509)"

// startDirect begins dialing the VNC server.
func (conn *desktopConn) startDirect() { go conn.run() }

// publish replaces the status and wakes the window. The caller holds the lock
// and has set the fields; publish releases it.
func (conn *desktopConn) publish() {
	conn.status.Version++
	conn.mu.Unlock()
	if conn.notify != nil {
		conn.notify()
	}
}

// ask is the password question the server raises mid-handshake. It blocks until
// the user has answered or the connection was cancelled.
func (conn *desktopConn) ask() (string, error) {
	conn.mu.Lock()
	conn.attempt++
	conn.asking = true
	conn.status = sshconn.Status{
		Version: conn.status.Version, Phase: sshconn.PhasePasswordPrompt, Endpoint: conn.endpoint(), Attempt: conn.attempt,
		// A remote desktop asks one plain secret question. Autofill and the
		// save box both decide by the question list; without it a saved VNC or
		// RDP password can be written but never used again.
		Questions: []sshconn.Question{{Secret: true}},
	}
	conn.publish()
	select {
	case password := <-conn.answers:
		conn.mu.Lock()
		conn.asking = false
		conn.status = sshconn.Status{Version: conn.status.Version, Phase: sshconn.PhaseConnecting, Endpoint: conn.endpoint()}
		conn.publish()
		return password, nil
	case <-conn.ctx.Done():
		return "", sshconn.ErrCanceled
	}
}

// run connects, asking again when the password was refused, and then waits for
// the session to end.
func (conn *desktopConn) run() {
	var session desktop.Session
	var err error
	for {
		session, err = conn.open(conn.ctx, conn.request)
		if !isDesktopAuthFailure(err) || conn.ctx.Err() != nil {
			break
		}
		// Refused: the next attempt asks again, and the dialog says so.
	}
	conn.mu.Lock()
	switch {
	case conn.ctx.Err() != nil:
		conn.status.Phase, conn.status.Err = sshconn.PhaseClosed, sshconn.ErrCanceled
		conn.publish()
		if err == nil {
			session.Close()
		}
		return
	case err != nil:
		if isDesktopAuthFailure(err) || errors.Is(err, rfb.ErrAuthRequired) || errors.Is(err, rdp.ErrAuthRequired) {
			err = fmt.Errorf("%w: %v", sshconn.ErrAuthFailed, err)
		}
		conn.status.Phase, conn.status.Err = sshconn.PhaseFailed, err
		conn.publish()
		return
	}
	conn.session = session
	conn.status.Phase, conn.status.Method = sshconn.PhaseConnected, session.Info().Protocol
	conn.status.ServerVersion, conn.status.Err = "", nil
	conn.publish()

	select {
	case <-session.Done():
	case <-conn.ctx.Done():
		session.Close()
		return
	}
	conn.mu.Lock()
	if conn.closed {
		conn.mu.Unlock()
		return
	}
	reason := session.Err()
	if reason == nil {
		reason = errors.New("the server closed the connection")
	}
	conn.status.Phase, conn.status.Err = sshconn.PhaseClosed, fmt.Errorf("%w: %v", sshconn.ErrDisconnected, reason)
	conn.publish()
}

// innerChanged follows the SSH tunnel: its questions are the user's to answer
// first; once it is connected the VNC server is dialed through it; and if it
// ends, so does the desktop.
func (conn *desktopConn) innerChanged() {
	conn.mu.Lock()
	inner := conn.inner
	conn.mu.Unlock()
	if inner == nil {
		return // newDesktopConn calls again once the tunnel is attached
	}
	status := inner.Status()
	conn.mu.Lock()
	if conn.closed || status.Version == conn.innerVersion {
		conn.mu.Unlock()
		return
	}
	conn.innerVersion = status.Version
	switch status.Phase {
	case sshconn.PhaseConnected:
		if conn.dialing {
			conn.mu.Unlock()
			return
		}
		conn.dialing = true
		conn.status = sshconn.Status{Version: conn.status.Version, Phase: sshconn.PhaseConnecting, Endpoint: conn.endpoint(), EndpointRole: sshconn.EndpointTarget}
		conn.publish()
		go conn.run()
	case sshconn.PhaseClosed, sshconn.PhaseFailed:
		session := conn.session
		if conn.session != nil || conn.dialing {
			// The tunnel went down under a running (or starting) desktop.
			if status.Err == nil || !errors.Is(status.Err, sshconn.ErrCanceled) {
				status.Err = fmt.Errorf("%w: the SSH connection ended", sshconn.ErrDisconnected)
			}
			conn.cancel()
		}
		status.Version = conn.status.Version
		conn.status = status
		conn.publish()
		if session != nil {
			session.Close()
		}
	default:
		version := conn.status.Version
		conn.status = status
		conn.status.Version = version
		conn.publish()
	}
}

func (conn *desktopConn) Status() sshconn.Status {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.status
}

// Desktop is the session once the connection is up.
func (conn *desktopConn) Desktop() desktop.Session {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.session
}

// isDesktopAuthFailure reports whether the server refused the password, after
// which the user is asked again.
func isDesktopAuthFailure(err error) bool {
	return errors.Is(err, rfb.ErrAuthFailed) || errors.Is(err, rdp.ErrAuthFailed)
}

func (conn *desktopConn) TrustHostKey(save bool) {
	conn.mu.Lock()
	certificate := conn.certAsking
	conn.mu.Unlock()
	if certificate {
		select {
		case conn.certAnswers <- certReply{accept: true, save: save}:
		default:
		}
		return
	}
	if conn.inner != nil {
		conn.inner.TrustHostKey(save)
	}
}

func (conn *desktopConn) RejectHostKey() {
	conn.mu.Lock()
	certificate := conn.certAsking
	conn.mu.Unlock()
	if certificate {
		select {
		case conn.certAnswers <- certReply{}:
		default:
		}
		return
	}
	if conn.inner != nil {
		conn.inner.RejectHostKey()
	}
}

func (conn *desktopConn) SubmitPassword(password string) {
	conn.mu.Lock()
	vncQuestion := conn.asking
	conn.mu.Unlock()
	if !vncQuestion && conn.inner != nil {
		conn.inner.SubmitPassword(password)
		return
	}
	select {
	case conn.answers <- password:
	default:
	}
}

// SubmitAnswers answers the connection behind a remote desktop. A VNC
// password is the one question desktop.Session asks; anything else belongs to
// the SSH connection the desktop may travel through.
func (conn *desktopConn) SubmitAnswers(answers []string) {
	conn.mu.Lock()
	vncQuestion := conn.asking
	conn.mu.Unlock()
	if !vncQuestion {
		if conn.inner != nil {
			conn.inner.SubmitAnswers(answers)
		}
		return
	}
	if len(answers) == 0 {
		return
	}
	select {
	case conn.answers <- answers[0]:
	default:
	}
}

func (conn *desktopConn) SubmitKeyPassphrase(passphrase string) {
	if conn.inner != nil {
		conn.inner.SubmitKeyPassphrase(passphrase)
	}
}

func (conn *desktopConn) SkipIdentity() {
	if conn.inner != nil {
		conn.inner.SkipIdentity()
	}
}

// Cancel stops connecting and lets go of the desktop.
func (conn *desktopConn) Cancel() {
	conn.mu.Lock()
	conn.closed = true
	session := conn.session
	conn.mu.Unlock()
	conn.cancel()
	if session != nil {
		session.Close()
	}
	if conn.inner != nil {
		conn.inner.Cancel()
	}
}

func (conn *desktopConn) OpenShell(sshconn.ShellConfig) (Shell, error) { return nil, errNoServer }
func (conn *desktopConn) OpenSFTP() (sftpclient.Remote, error)         { return nil, errNoServer }

func (conn *desktopConn) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errNoServer
}

func (conn *desktopConn) Listen(string, string) (net.Listener, error) { return nil, errNoServer }

func (conn *desktopConn) RunCommand(context.Context, string, int) ([]byte, error) {
	return nil, errNoServer
}

func (conn *desktopConn) StreamCommand(context.Context, string, int, func([]byte) bool) error {
	return errNoServer
}

// rdpCertificatesFile is where the certificates of trusted RDP servers are kept,
// next to the SSH known-hosts file.
const rdpCertificatesFile = "rdp_certificates"

// gatewayFor is the SSH host a tunneled desktop goes through: the saved SSH host
// with the same address and account, so its port, algorithms and jump route
// apply, or else plain SSH on the default port. An RDP host has no SSH account of
// its own, so the first saved SSH host with its address stands for it.
func (state *State) gatewayFor(host Host) Host {
	if host.Protocol == protocolRDP {
		// The account of an RDP host is the Windows one: the SSH account is the
		// one saved for the same address, or the local account.
		for _, saved := range state.hosts {
			if saved.isSSH() && saved.Address == host.Address {
				return saved
			}
		}
		gateway := Host{Address: host.Address, Port: defaultPort}
		gateway.finish()
		return gateway
	}
	login := state.login(host)
	for _, saved := range state.hosts {
		if saved.isSSH() && saved.Address == host.Address && state.login(saved) == login {
			return saved
		}
	}
	gateway := Host{Address: host.Address, User: host.User, Port: defaultPort}
	gateway.finish()
	return gateway
}

// newDesktopConn creates the connection of a VNC host.
func (state *State) newDesktopConn(session *Session, host Host) *desktopConn {
	notify := state.notifyFor(session)
	open := state.dialDesktop
	if open == nil {
		open = openDesktopSession
	}
	var certs *rdp.Pins
	if state.options.KnownHostsPath != "" {
		certs = rdp.NewPins(filepath.Join(filepath.Dir(state.options.KnownHostsPath), rdpCertificatesFile))
	}
	conn := newDesktopConn(host, notify, open, state.connectionTimeout, state.proxyForHost(host), certs)
	target := state.desktopTargetFor(session)
	conn.request.Width, conn.request.Height, conn.request.Scale = target.width, target.height, target.scale
	if !host.tunneled() {
		conn.startDirect()
		return conn
	}
	gateway := state.gatewayFor(host)
	config, _ := state.sshConfigFor(session, gateway) // a broken route has no host, and dialing says so
	config.Notify = conn.innerChanged
	inner := state.options.Dial(config)
	conn.mu.Lock()
	conn.inner = inner
	conn.request.Tunnel = inner
	conn.mu.Unlock()
	conn.innerChanged() // what it reported before it was ours to follow
	return conn
}
