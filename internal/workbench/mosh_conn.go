package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/i18n"
	"motty/internal/mosh"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// A mosh host is an SSH host whose terminal runs over mosh. The SSH
// connection authenticates the user and starts mosh-server; once the client has
// the key, that connection is closed and the terminal is carried over UDP. The
// session therefore has no SSH server: file, forwarding, monitoring, command
// and shell-integration features are unavailable (see Session.hasServer).

// moshConnectLine is what mosh-server prints once it has a key and a UDP port.
// A login banner may surround it, so only the line itself is accepted.
var moshConnectLine = regexp.MustCompile(`(?m)^MOSH CONNECT (\d+) ([A-Za-z0-9/+]{22})\s*$`)

// moshLocaleName rejects anything that did not come out of locale -a, since the
// name is passed to mosh-server's command line.
var moshLocaleName = regexp.MustCompile(`^[A-Za-z0-9][A-Za-z0-9._@-]*$`)

// moshPendingNotice is how long a mosh session may sit without a single
// datagram before the terminal says the UDP path has not answered. The session
// keeps retrying; this is information, not a failure.
const moshPendingNotice = 5 * time.Second

// moshConn is a Connection whose terminal transport is mosh. It forwards the
// SSH bootstrap's prompts and progress until the user is authenticated, starts
// mosh-server, then reports its own connected state and never exposes the SSH
// link again.
type moshConn struct {
	host     Host
	messages i18n.ToolsMessages
	notify   func()
	timeout  time.Duration
	// cols and rows are the terminal size the session starts from: the size of
	// the screen it is about to get. Mosh governs the remote terminal's size
	// from the client, so both ends must start from the same one.
	cols, rows int

	ctx    context.Context
	cancel context.CancelFunc

	mu            sync.Mutex
	status        sshconn.Status
	inner         Connection
	client        *mosh.Client
	bootstrapping bool
	shell         *moshShell
}

func (state *State) newMoshConn(session *Session, host Host, config sshconn.Config) *moshConn {
	ctx, cancel := context.WithCancel(context.Background())
	timeout := state.connectionTimeout
	if timeout <= 0 {
		timeout = 20 * time.Second
	}
	cols, rows := 80, 24
	if session.term != nil {
		cols, rows = session.term.screen.Size()
	} else if geometry := state.layoutForSession(session.ID); geometry.cols > 0 && geometry.rows > 0 {
		cols, rows = geometry.cols, geometry.rows
	}
	conn := &moshConn{
		host:     host,
		messages: state.messages.Tools,
		notify:   state.notifyFor(session),
		timeout:  timeout,
		cols:     cols,
		rows:     rows,
		ctx:      ctx,
		cancel:   cancel,
		status:   sshconn.Status{Version: 1, Phase: sshconn.PhaseConnecting},
	}
	if state.options.Dial != nil {
		conn.inner = state.options.Dial(config)
	}
	if conn.inner == nil {
		conn.status.Phase = sshconn.PhaseFailed
		conn.status.Err = fmt.Errorf(state.messages.Tools.MoshBootstrapFailedFmt, "no SSH connection")
	}
	return conn
}

// Status returns the SSH bootstrap's status until the user is authenticated.
// The bootstrap's Connected state is deliberately not reported: mosh is not
// ready yet, and a Connected status would start the SSH-only features this
// session cannot have. Once mosh is ready, its own status is the session's.
func (conn *moshConn) Status() sshconn.Status {
	conn.mu.Lock()
	if conn.inner == nil || conn.status.Phase == sshconn.PhaseConnected ||
		conn.status.Phase == sshconn.PhaseFailed || conn.status.Phase == sshconn.PhaseClosed {
		status := conn.status
		conn.mu.Unlock()
		return status
	}
	if conn.bootstrapping {
		inner := conn.inner
		conn.mu.Unlock()
		// The SSH connection may still be refining what it knows, such as the
		// host's system after its probe. Adopt that while mosh starts; the
		// bootstrap copies the final value again when it takes over.
		latest := inner.Status()
		conn.mu.Lock()
		if conn.status.Phase == sshconn.PhaseConnecting &&
			(latest.System != conn.status.System || latest.SystemProvisional != conn.status.SystemProvisional) {
			conn.status.System, conn.status.SystemProvisional = latest.System, latest.SystemProvisional
			conn.status.Version++
		}
		status := conn.status
		conn.mu.Unlock()
		return status
	}
	inner := conn.inner
	conn.mu.Unlock()

	status := inner.Status()
	switch status.Phase {
	case sshconn.PhaseConnected:
		start := false
		conn.mu.Lock()
		if !conn.bootstrapping {
			start = true
			conn.bootstrapping = true
			conn.status = status
			conn.status.Version++
			conn.status.Phase = sshconn.PhaseConnecting
			conn.status.Err = nil
			conn.status.Method = "mosh"
			// The SSH identification string stops describing what the tab is
			// doing once mosh takes over. The system it named was already
			// adopted from the same status.
			conn.status.ServerVersion = ""
		}
		result := conn.status
		conn.mu.Unlock()
		if start {
			go conn.bootstrap()
		}
		return result
	case sshconn.PhaseFailed, sshconn.PhaseClosed:
		conn.mu.Lock()
		conn.status = status
		conn.mu.Unlock()
		return status
	default:
		return status
	}
}

// bootstrap runs once, off the window's thread: find a UTF-8 locale, start
// mosh-server over the authenticated SSH connection, then open the datagram
// socket the client speaks over.
func (conn *moshConn) bootstrap() {
	ctx, cancel := context.WithTimeout(conn.ctx, conn.timeout)
	defer cancel()

	client, err := conn.startClient(ctx)
	if err != nil {
		conn.fail(err)
		return
	}
	if conn.ctx.Err() != nil {
		// The tab was closed while the bootstrap was running; nobody is
		// waiting for this client.
		client.Close()
		return
	}

	conn.mu.Lock()
	conn.client = client
	// The SSH probe may have learned the host's system after the Connected
	// status this connection copied; take its latest answer before the SSH
	// connection goes away.
	if conn.inner != nil {
		latest := conn.inner.Status()
		conn.status.System, conn.status.SystemProvisional = latest.System, latest.SystemProvisional
	}
	conn.status.Version++
	conn.status.Phase = sshconn.PhaseConnected
	conn.status.Method = "mosh"
	conn.status.Err = nil
	// The SSH connection is gone; its identification string must not make the
	// tab's page say the SSH session is what is connected.
	conn.status.ServerVersion = ""
	conn.mu.Unlock()
	if conn.notify != nil {
		conn.notify()
	}

	// The bootstrap SSH connection has done its work. Closing it must not end
	// the session; Status no longer reads it once mosh is ready.
	if inner := conn.muLockedInner(); inner != nil {
		inner.Cancel()
	}
	go conn.watch(client)
}

func (conn *moshConn) startClient(ctx context.Context) (*mosh.Client, error) {
	locale, localeKnown := conn.utf8Locale(ctx)
	if locale == "" {
		locale = "C.UTF-8"
	}
	port, key, err := conn.startServer(ctx, locale)
	if err != nil && errors.Is(err, errMoshLocale) && localeKnown {
		locale = conn.fallbackLocale(ctx, locale)
		if locale != "" {
			port, key, err = conn.startServer(ctx, locale)
		}
	}
	if errors.Is(err, errMoshLocale) {
		return nil, errors.New(conn.messages.MoshLocaleMissing)
	}
	if err != nil {
		return nil, err
	}

	server, err := conn.serverAddress(ctx, port)
	if err != nil {
		return nil, err
	}
	network := "udp4"
	if server.IP.To4() == nil {
		network = "udp6"
	}
	socket, err := net.ListenPacket(network, ":0")
	if err != nil {
		return nil, err
	}
	client, err := mosh.Dial(mosh.Config{
		Conn:   socket,
		Server: server,
		Key:    key,
		Cols:   conn.cols,
		Rows:   conn.rows,
		Notify: conn.notify,
	})
	if err != nil {
		return nil, err
	}
	return client, nil
}

// startServer runs mosh-server over the authenticated connection. mosh-server
// prints its announcement and detaches; the SSH command returns at once.
func (conn *moshConn) startServer(ctx context.Context, locale string) (int, string, error) {
	if conn.muLockedInner() == nil {
		return 0, "", errors.New(conn.messages.MoshServerMissing)
	}
	// The name reaches a remote shell command line; only locale -a's own
	// alphabet is accepted.
	if !moshLocaleName.MatchString(locale) {
		return 0, "", errMoshLocale
	}
	command := "mosh-server new -c 256 -l LANG=" + locale + " -l LC_ALL=" + locale
	result, err := conn.inner.Exec(ctx, command, 256<<10)
	if err != nil {
		return 0, "", fmt.Errorf(conn.messages.MoshBootstrapFailedFmt, err)
	}
	if match := moshConnectLine.FindStringSubmatch(string(result.Output)); match != nil {
		port, parseErr := strconv.Atoi(match[1])
		if parseErr == nil && port > 0 && port <= 65535 {
			return port, match[2], nil
		}
		return 0, "", fmt.Errorf(conn.messages.MoshBootstrapFailedFmt, "bad MOSH CONNECT line")
	}
	output := string(result.Output)
	switch {
	case result.ExitCode == 127 || strings.Contains(output, "not found"):
		return 0, "", errors.New(conn.messages.MoshServerMissing)
	case strings.Contains(output, "UTF-8") && strings.Contains(output, "locale"):
		return 0, "", errMoshLocale
	}
	return 0, "", fmt.Errorf(conn.messages.MoshBootstrapFailedFmt, firstLine(output))
}

var errMoshLocale = errors.New("mosh-server needs a UTF-8 locale")

// utf8Locale is the server's own UTF-8 locale when it has one, so the shell
// keeps the host's language. known is false when locale could not be read at
// all, in which case the caller still tries the conventional name.
func (conn *moshConn) utf8Locale(ctx context.Context) (locale string, known bool) {
	inner := conn.muLockedInner()
	if inner == nil {
		return "", false
	}
	if result, err := inner.Exec(ctx, "locale", 64<<10); err == nil {
		for _, line := range strings.Split(string(result.Output), "\n") {
			line = strings.TrimSpace(line)
			for _, name := range []string{"LC_ALL=", "LANG="} {
				if value, ok := strings.CutPrefix(line, name); ok && isUTF8Locale(value) {
					return value, true
				}
			}
		}
	}
	result, err := inner.Exec(ctx, "locale -a", 64<<10)
	if err != nil {
		return "", false
	}
	return pickUTF8Locale(string(result.Output)), true
}

// fallbackLocale finds another UTF-8 locale when the first attempt was refused;
// it is empty when locale -a lists none.
func (conn *moshConn) fallbackLocale(ctx context.Context, tried string) string {
	inner := conn.muLockedInner()
	if inner == nil {
		return ""
	}
	result, err := inner.Exec(ctx, "locale -a", 64<<10)
	if err != nil {
		return ""
	}
	for _, candidate := range strings.Split(string(result.Output), "\n") {
		candidate = strings.TrimSpace(candidate)
		if candidate != "" && candidate != tried && isUTF8Locale(candidate) {
			return candidate
		}
	}
	return ""
}

// pickUTF8Locale prefers the C locale, then en_US, then any UTF-8 locale, so a
// host without its own UTF-8 default still gets a working mosh-server.
func pickUTF8Locale(listing string) string {
	var first string
	for _, line := range strings.Split(listing, "\n") {
		line = strings.TrimSpace(line)
		if !isUTF8Locale(line) {
			continue
		}
		lower := strings.ToLower(line)
		switch lower {
		case "c.utf-8", "c.utf8", "posix.utf-8", "posix.utf8":
			return line
		}
		if lower == "en_us.utf-8" || lower == "en_us.utf8" {
			if first == "" {
				first = line
			}
			continue
		}
		if first == "" {
			first = line
		}
	}
	return first
}

func isUTF8Locale(name string) bool {
	lower := strings.ToLower(name)
	return strings.Contains(lower, ".utf-8") || strings.Contains(lower, ".utf8")
}

// serverAddress resolves the host the SSH connection reached to the datagram
// address mosh-server announced its port on. Mosh needs a direct path; a name
// that only resolves to an unreachable address leaves the client retrying and
// the terminal explains it.
func (conn *moshConn) serverAddress(ctx context.Context, port int) (*net.UDPAddr, error) {
	ips, err := net.DefaultResolver.LookupNetIP(ctx, "ip", conn.host.Address)
	if err != nil {
		return nil, err
	}
	var chosen net.IP
	for _, ip := range ips {
		address := net.IP(ip.AsSlice())
		if address.To4() != nil {
			chosen = address
			break
		}
		if chosen == nil {
			chosen = address
		}
	}
	if chosen == nil {
		return nil, fmt.Errorf("no address for %s", conn.host.Address)
	}
	return &net.UDPAddr{IP: chosen, Port: port}, nil
}

// watch reports the end of the mosh session as the connection's end, so the
// session status and any SSH-face features agree that it is over.
func (conn *moshConn) watch(client *mosh.Client) {
	select {
	case <-client.Done():
	case <-conn.ctx.Done():
		return
	}
	conn.mu.Lock()
	if conn.status.Phase != sshconn.PhaseClosed {
		conn.status.Version++
		conn.status.Phase = sshconn.PhaseClosed
		conn.status.Err = client.Err()
	}
	conn.mu.Unlock()
	if conn.notify != nil {
		conn.notify()
	}
}

func (conn *moshConn) fail(err error) {
	if conn.ctx.Err() != nil {
		return // cancelled; the session is not waiting for a result
	}
	conn.mu.Lock()
	conn.status.Version++
	conn.status.Phase = sshconn.PhaseFailed
	conn.status.Err = err
	conn.status.ServerVersion = ""
	conn.mu.Unlock()
	if conn.inner != nil {
		conn.inner.Cancel()
	}
	if conn.notify != nil {
		conn.notify()
	}
}

func (conn *moshConn) muLockedInner() Connection {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.inner
}

// The prompt methods belong to the SSH bootstrap. After mosh is ready there is
// nothing to answer.
func (conn *moshConn) TrustHostKey(save bool) {
	if inner := conn.muLockedInner(); inner != nil {
		inner.TrustHostKey(save)
	}
}
func (conn *moshConn) RejectHostKey() {
	if inner := conn.muLockedInner(); inner != nil {
		inner.RejectHostKey()
	}
}
func (conn *moshConn) SubmitPassword(password string) {
	if inner := conn.muLockedInner(); inner != nil {
		inner.SubmitPassword(password)
	}
}
func (conn *moshConn) SubmitAnswers(answers []string) {
	if inner := conn.muLockedInner(); inner != nil {
		inner.SubmitAnswers(answers)
	}
}
func (conn *moshConn) SubmitKeyPassphrase(passphrase string) {
	if inner := conn.muLockedInner(); inner != nil {
		inner.SubmitKeyPassphrase(passphrase)
	}
}
func (conn *moshConn) SkipIdentity() {
	if inner := conn.muLockedInner(); inner != nil {
		inner.SkipIdentity()
	}
}

// Cancel ends the session: the shutdown handshake goes to mosh-server, and the
// bootstrap connection, if it is still up, is cancelled too.
func (conn *moshConn) Cancel() {
	conn.cancel()
	conn.mu.Lock()
	client, inner, shell := conn.client, conn.inner, conn.shell
	conn.client = nil
	conn.shell = nil
	if conn.status.Phase != sshconn.PhaseClosed {
		conn.status.Version++
		conn.status.Phase = sshconn.PhaseClosed
	}
	conn.mu.Unlock()
	if shell != nil {
		shell.Close()
	}
	if client != nil {
		client.Close()
	}
	if inner != nil {
		inner.Cancel()
	}
}

// OpenShell hands the mosh terminal to the workbench at its size.
func (conn *moshConn) OpenShell(config sshconn.ShellConfig) (Shell, error) {
	conn.mu.Lock()
	client := conn.client
	conn.mu.Unlock()
	if client == nil {
		return nil, fmt.Errorf(conn.messages.MoshBootstrapFailedFmt, "mosh client is not ready")
	}
	shell := newMoshShell(client, conn.notify, conn.messages)
	shell.Resize(config.Cols, config.Rows)
	conn.mu.Lock()
	conn.shell = shell
	conn.mu.Unlock()
	return shell, nil
}

func (conn *moshConn) OpenSFTP() (sftpclient.Remote, error) { return nil, errNoServer }

func (conn *moshConn) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errNoServer
}

func (conn *moshConn) DialShellContext(context.Context, string, string) (net.Conn, error) {
	return nil, errNoServer
}

func (conn *moshConn) Listen(string, string) (net.Listener, error) { return nil, errNoServer }

func (conn *moshConn) ListenContext(context.Context, string, string, func(net.Listener) error) (net.Listener, error) {
	return nil, errNoServer
}

func (conn *moshConn) RunCommand(context.Context, string, int) ([]byte, error) {
	return nil, errNoServer
}

func (conn *moshConn) Exec(context.Context, string, int) (sshconn.ExecResult, error) {
	return sshconn.ExecResult{}, errNoServer
}

func (conn *moshConn) StreamCommand(context.Context, string, int, func([]byte) bool) error {
	return errNoServer
}

// firstLine is the useful part of a failed bootstrap's output for a message.
func firstLine(output string) string {
	for _, line := range strings.Split(output, "\n") {
		if line = strings.TrimSpace(line); line != "" {
			return line
		}
	}
	return "no output"
}
