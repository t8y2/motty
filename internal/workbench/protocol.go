package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"slices"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/charset"
	"motty/internal/desktop"
	"motty/internal/localshell"
	"motty/internal/serial"
	"motty/internal/sftpclient"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
	"motty/internal/telnet"
)

// A host is reached over SSH unless it says otherwise. Telnet and a serial port
// give the same terminal tab, without the things only an SSH server offers:
// files, forwarding, remote commands and the resource monitor.

type hostProtocol uint8

const (
	protocolSSH hostProtocol = iota
	protocolTelnet
	protocolSerial
	protocolVNC
	protocolRDP
	protocolWSL
)

// hostTerminal is how an SSH host's interactive session is carried. Mosh is
// not a protocol of its own: it is bootstrapped and authenticated over SSH and
// then carries the terminal over UDP, so the host keeps every SSH setting.
type hostTerminal uint8

const (
	terminalSSH hostTerminal = iota
	terminalMosh
)

// defaultTelnetPort is the port a Telnet host without an explicit one uses,
// defaultVNCPort the one of a VNC server (display :0) and defaultRDPPort the one
// of Remote Desktop.
const (
	defaultTelnetPort = 23
	defaultVNCPort    = 5900
	defaultRDPPort    = 3389
)

// isSSH reports whether the host is reached over SSH.
func (host Host) isSSH() bool { return host.Protocol == protocolSSH }

func (host Host) isWSL() bool { return host.Protocol == protocolWSL }

func (host Host) isLocal() bool { return host.ID == localHostID || host.isWSL() }

// mosh reports whether this SSH host's interactive terminal runs over mosh.
// The SSH connection is used only for the bootstrap; the session has no SSH
// server once the terminal is up.
func (host Host) mosh() bool { return host.Protocol == protocolSSH && host.Terminal == terminalMosh }

func (state *State) sshHostCount() int {
	count := 0
	for _, host := range state.hosts {
		if host.isSSH() {
			count++
		}
	}
	return count
}

// routeTargetCount is how many hosts a jump route can reach: SSH hosts and
// remote desktops.
func (state *State) routeTargetCount() int {
	count := 0
	for _, host := range state.hosts {
		if host.isRouteTarget() {
			count++
		}
	}
	return count
}

// defaultPort is the port a host of its protocol uses when it names none.
func (host Host) defaultPort() uint16 {
	switch host.Protocol {
	case protocolTelnet:
		return defaultTelnetPort
	case protocolVNC:
		return defaultVNCPort
	case protocolRDP:
		return defaultRDPPort
	case protocolWSL:
		return 0
	}
	return defaultPort
}

// isDesktop reports whether the protocol carries a remote desktop.
func (protocol hostProtocol) isDesktop() bool {
	return protocol == protocolVNC || protocol == protocolRDP
}

// isDesktop reports whether the host is a remote desktop: its tab shows a
// picture, not a terminal.
func (host Host) isDesktop() bool {
	return host.Protocol.isDesktop()
}

// isRouteTarget reports whether a jump route can reach the host as its target:
// an SSH host, whose connection the route extends, or a remote desktop, whose
// port the route's jump host forwards. A jump itself is always an SSH host.
func (host Host) isRouteTarget() bool { return host.isSSH() || host.isDesktop() }

// tunneled reports whether the host is reached through an SSH connection of its
// own: a remote desktop whose server is only listening on the SSH server's own
// loopback.
func (host Host) tunneled() bool { return host.isDesktop() && host.ViaSSH }

// hasServer reports whether the session talks to an SSH server: the only kind
// that can list files, forward ports and run commands. A mosh host keeps no SSH
// connection after its bootstrap, so it has none of those.
func (session *Session) hasServer() bool {
	return !session.isLocal() && session.host.isSSH() && !session.host.mosh()
}

// canBrowseFiles reports whether the session has files to show: the server's
// over SFTP, or this computer's for a local terminal.
func (session *Session) canBrowseFiles() bool {
	return session.isLocal() || session.hasServer()
}

// savedTransport is how a host that is not SSH is reached, in the state file and
// in a host catalog. Fields a protocol does not use stay empty, and a file
// without any of them describes an SSH host as before.
type savedTransport struct {
	Protocol     string `json:"protocol,omitempty"` // "telnet", "serial", "vnc", "rdp" or "wsl"
	WSLDirectory string `json:"wsl_directory,omitempty"`
	WSLNoGUI     bool   `json:"wsl_no_gui,omitempty"`
	// Terminal is "mosh" for an SSH host whose interactive session runs over
	// mosh. Empty is SSH, and a file without it describes the host as before.
	Terminal string `json:"terminal,omitempty"`
	// Encoding is the charset of the remote shell's byte stream. Empty (or
	// "UTF-8") is the default; any other value names a charset.Encodings entry.
	Encoding string `json:"encoding,omitempty"`
	Line     string `json:"line,omitempty"` // serial: "115200 8N1"
	Flow     string `json:"flow,omitempty"` // serial: "rtscts" or "xonxoff"
	// Backspace is "ctrl-h" for a serial, Telnet or SSH host whose Backspace
	// key sends BS instead of DEL; empty (or missing) sends DEL, as a host that
	// never chose one always did.
	Backspace string `json:"backspace,omitempty"`
	// Proxy is nil when this host inherits the global outbound proxy. An entry
	// whose kind is "direct" explicitly bypasses it.
	Proxy *savedOutboundProxy `json:"proxy,omitempty"`
	// ProxyProfile, when set, names a saved proxy profile that carries the host
	// instead of Proxy, and ProxyBypass is the host's own direct list for it.
	ProxyProfile string `json:"proxy_profile,omitempty"`
	ProxyBypass  string `json:"proxy_bypass,omitempty"`
	// Algorithms is "compatible" or "custom" for an SSH host that does not use
	// the default algorithms; AlgorithmList is the custom list.
	Algorithms    string `json:"algorithms,omitempty"`
	AlgorithmList string `json:"algorithm_list,omitempty"`
	// Authentication is "password", "key", "agent" or "kerberos" when an SSH host does
	// not use the automatic authentication order. Identity optionally selects
	// one imported key fingerprint for the key policy.
	Authentication string                       `json:"authentication,omitempty"`
	Identity       string                       `json:"identity,omitempty"`
	SSHOptions     *sshconfig.ConnectionOptions `json:"ssh_options,omitempty"`
	// IdentitiesOnly is set for an automatic SSH host that offers the
	// ssh-agent only the keys matching its configured identities.
	IdentitiesOnly bool `json:"identities_only,omitempty"`
	// Credential is the id of the saved password an SSH, VNC or RDP host takes
	// its password from (see credentials.go).
	Credential string `json:"credential,omitempty"`
	// ForwardX11 is set for an SSH host that forwards X11.
	ForwardX11 bool `json:"forward_x11,omitempty"`
	// AgentForward is set for an SSH host whose shells may use the local
	// ssh-agent.
	AgentForward bool `json:"agent_forward,omitempty"`
	// InteractiveOnly is set for an SSH host that must be sent only the pty and
	// shell requests, the way a gateway that permits only an interactive login
	// requires. A host that refused the client's probes is saved this way.
	InteractiveOnly bool `json:"interactive_only,omitempty"`
	// NoHistory is set for an SSH host whose commands are not remembered.
	NoHistory bool `json:"no_history,omitempty"`
	// LoginCommand is the command lines an SSH host types into its shell after login.
	LoginCommand       string `json:"login_command,omitempty"`
	LoginCommandPolicy string `json:"login_command_policy,omitempty"`
	// IdleKeepAliveSeconds is how often an idle terminal is sent input; zero is
	// off. IdleInput is "cr", "nul" or "custom" (empty follows the protocol),
	// and IdleInputCustom is the escaped text for "custom".
	IdleKeepAliveSeconds int             `json:"idle_keepalive_seconds,omitempty"`
	IdleInput            string          `json:"idle_keepalive_input,omitempty"`
	IdleInputCustom      string          `json:"idle_keepalive_custom,omitempty"`
	RemoteLogPaths       []remoteLogPath `json:"remote_log_paths,omitempty"`
	// Transfer is "sftp" or "scp" for an SSH host whose file panel does not
	// choose by itself.
	Transfer string `json:"transfer,omitempty"`
	// ViaSSH and Clipboard are a remote desktop host's (VNC or RDP): reached
	// through an SSH connection to the same machine, and sharing the clipboard
	// with the remote desktop. Console is an RDP host's: connect to the server's
	// console session instead of a new one, as mstsc /admin does. NoAudio is an
	// RDP host's too: ask the server for no audio. StandardSecurity is an RDP
	// host's as well: use Standard RDP Security alone, with no TLS or NLA.
	// LegacyTLS is an RDP host's too: offer the RSA key exchange TLS suites
	// that an old server requires.
	// Chromeless is a desktop host's: its page shows the picture without the
	// bars around it.
	// The password is never here: it is asked for, and kept in the vault only if
	// the user says so.
	ViaSSH           bool `json:"via_ssh,omitempty"`
	Clipboard        bool `json:"clipboard,omitempty"`
	Console          bool `json:"console,omitempty"`
	NoAudio          bool `json:"no_audio,omitempty"`
	Microphone       bool `json:"microphone,omitempty"`
	StandardSecurity bool `json:"standard_security,omitempty"`
	LegacyTLS        bool `json:"legacy_tls,omitempty"`
	Chromeless       bool `json:"chromeless,omitempty"`
	// ColorDepth, DesktopEncoding and Effects are a desktop host's quality
	// choices. They are written only when they differ from what a host that never
	// chose has, so an older record stays byte-for-byte what it was.
	ColorDepth      int              `json:"color_depth,omitempty"`
	DesktopEncoding desktop.Encoding `json:"desktop_encoding,omitempty"`
	Effects         *desktop.Effects `json:"effects,omitempty"`
	// DesktopWidth and DesktopHeight are an RDP host's fixed remote resolution.
	DesktopWidth  int `json:"desktop_width,omitempty"`
	DesktopHeight int `json:"desktop_height,omitempty"`
}

func (host Host) transport() savedTransport {
	transport := savedTransport{Proxy: savedHostProxy(host.Proxy), Credential: host.Credential,
		ProxyProfile: host.ProxyProfile, ProxyBypass: host.ProxyBypass}
	// UTF-8 is the empty value, so a host that never chose a charset keeps its
	// old record byte-for-byte.
	if host.Encoding != "" && host.Encoding != charset.UTF8 {
		transport.Encoding = host.Encoding
	}
	// Every terminal protocol keeps this setting, so it is written before the
	// switch that gives the rest of the transport its protocol-specific shape.
	if validIdleKeepAliveInterval(host.IdleKeepAlive) {
		transport.IdleKeepAliveSeconds = int(host.IdleKeepAlive / time.Second)
	}
	transport.IdleInput = host.savedIdleInput()
	if host.IdleInput == idleInputCustom {
		transport.IdleInputCustom = host.IdleInputCustom
	}
	switch host.Protocol {
	case protocolTelnet:
		transport.Protocol = "telnet"
		if host.Backspace == backspaceCtrlH {
			transport.Backspace = "ctrl-h"
		}
		return transport
	case protocolVNC:
		transport.Protocol, transport.ViaSSH, transport.Clipboard, transport.Chromeless = "vnc", host.ViaSSH, host.Clipboard, host.Chromeless
		transport.ColorDepth, transport.DesktopEncoding = host.ColorDepth, host.PreferredEncoding
		return transport
	case protocolRDP:
		transport.Protocol, transport.ViaSSH, transport.Clipboard, transport.Chromeless = "rdp", host.ViaSSH, host.Clipboard, host.Chromeless
		transport.Console = host.Console
		transport.NoAudio = host.NoAudio
		transport.Microphone = host.Microphone
		transport.StandardSecurity = host.StandardSecurity
		transport.LegacyTLS = host.LegacyTLS
		transport.ColorDepth = host.ColorDepth
		transport.Effects = host.Effects
		transport.DesktopWidth, transport.DesktopHeight = savedDesktopSize(host.DesktopWidth, host.DesktopHeight)
		return transport
	case protocolWSL:
		return savedTransport{Protocol: "wsl", WSLDirectory: host.WSLDirectory, WSLNoGUI: host.WSLNoGUI,
			LoginCommand: host.LoginCommand, NoHistory: host.NoHistory}
	case protocolSerial:
		transport.Proxy, transport.Protocol, transport.Line = nil, "serial", host.Line.String()
		if host.Backspace == backspaceCtrlH {
			transport.Backspace = "ctrl-h"
		}
		switch host.Line.Flow {
		case serial.FlowHardware:
			transport.Flow = "rtscts"
		case serial.FlowSoftware:
			transport.Flow = "xonxoff"
		}
		return transport
	}
	mode, list := host.savedAlgo()
	if host.mosh() {
		transport.Terminal = "mosh"
	} else if host.Backspace == backspaceCtrlH {
		transport.Backspace = "ctrl-h"
	}
	transport.Algorithms, transport.AlgorithmList = mode, list
	transport.Authentication, transport.Identity = host.Auth.saved(), host.Identity
	transport.SSHOptions = host.SSHOptions.Clone()
	transport.IdentitiesOnly = host.IdentitiesOnly
	transport.ForwardX11, transport.AgentForward, transport.NoHistory, transport.Transfer = host.ForwardX11, host.AgentForward, host.NoHistory, host.savedTransfer()
	transport.InteractiveOnly = host.InteractiveOnly
	transport.LoginCommand = host.LoginCommand
	transport.LoginCommandPolicy = host.savedLoginCommandPolicy()
	transport.RemoteLogPaths = host.RemoteLogPaths
	return transport
}

// applyTransport reads a saved transport into the host. It reports false for a
// serial line that cannot be used, so a damaged entry is dropped rather than
// opened with a guess.
func (host *Host) applyTransport(transport savedTransport) bool {
	host.Proxy = restoredHostProxy(transport.Proxy)
	host.Credential = strings.TrimSpace(transport.Credential)
	host.ProxyProfile = strings.TrimSpace(transport.ProxyProfile)
	host.ProxyBypass = strings.TrimSpace(transport.ProxyBypass)
	if host.ProxyProfile != "" {
		// A profile carries the route; a stale per-host proxy in the same
		// record must not shadow it.
		host.Proxy = nil
	}
	// An unknown charset is a damaged record: drop it rather than opening a
	// session that would show replacement characters.
	encoding, ok := charset.Normalize(strings.TrimSpace(transport.Encoding))
	if !ok {
		return false
	}
	if encoding == charset.UTF8 {
		encoding = "" // keep the zero value as the UTF-8 default
	}
	host.Encoding = encoding
	host.Terminal = terminalSSH
	host.applyIdleKeepAlive(transport)
	switch transport.Protocol {
	case "", "ssh":
		host.Protocol = protocolSSH
		if !transport.SSHOptions.Valid() {
			return false
		}
		host.SSHOptions = transport.SSHOptions.Clone()
		var ok bool
		if host.Auth, ok = parseHostAuth(transport.Authentication); !ok {
			return false
		}
		host.Identity = strings.TrimSpace(transport.Identity)
		if host.Auth != authPrivateKey {
			host.Identity = ""
		}
		host.IdentitiesOnly = transport.IdentitiesOnly
		host.ForwardX11 = transport.ForwardX11
		host.AgentForward = transport.AgentForward
		host.InteractiveOnly = transport.InteractiveOnly
		host.NoHistory = transport.NoHistory
		host.LoginCommand = restoredLoginCommand(transport.LoginCommand)
		host.applyLoginCommandPolicy(transport.LoginCommandPolicy)
		host.RemoteLogPaths = restoreRemoteLogPaths(transport.RemoteLogPaths)
		switch transport.Terminal {
		case "", "ssh":
			host.Terminal = terminalSSH
		case "mosh":
			// mosh-server needs a UTF-8 locale and a command session to start;
			// X11, agent forwarding and an interactive-only gateway belong to
			// an SSH session, not to a mosh one.
			if host.Encoding != "" {
				return false
			}
			host.Terminal = terminalMosh
			host.ForwardX11, host.AgentForward = false, false
			host.InteractiveOnly = false
		default:
			return false
		}
		host.Backspace = backspaceDEL
		if !host.mosh() {
			host.Backspace = restoredBackspace(transport.Backspace)
		}
		return host.applyTransfer(transport.Transfer) && host.applyAlgo(transport.Algorithms, transport.AlgorithmList)
	case "telnet":
		host.Protocol, host.Credential = protocolTelnet, ""
		host.Backspace = restoredBackspace(transport.Backspace)
	case "vnc":
		host.Protocol, host.ViaSSH, host.Clipboard, host.Chromeless = protocolVNC, transport.ViaSSH, transport.Clipboard, transport.Chromeless
		host.ColorDepth, host.PreferredEncoding = vncColorDepth(transport.ColorDepth), transport.DesktopEncoding
		if !host.PreferredEncoding.Valid() {
			host.PreferredEncoding = desktop.EncodingAuto
		}
	case "rdp":
		host.Protocol, host.ViaSSH, host.Clipboard, host.Chromeless = protocolRDP, transport.ViaSSH, transport.Clipboard, transport.Chromeless
		host.Console = transport.Console
		host.NoAudio = transport.NoAudio
		host.Microphone = transport.Microphone
		host.StandardSecurity = transport.StandardSecurity
		host.LegacyTLS = transport.LegacyTLS
		host.ColorDepth = rdpColorDepth(transport.ColorDepth)
		host.Effects = transport.Effects
		host.DesktopWidth, host.DesktopHeight = savedDesktopSize(transport.DesktopWidth, transport.DesktopHeight)
	case "wsl":
		host.Protocol = protocolWSL
		host.WSLDirectory, host.WSLNoGUI = transport.WSLDirectory, transport.WSLNoGUI
		host.LoginCommand, host.NoHistory = transport.LoginCommand, transport.NoHistory
		return validWSLDirectory(host.WSLDirectory) && validSingleLineCommand(host.LoginCommand)
	case "serial":
		host.Proxy, host.Credential = nil, ""
		flow := serial.FlowNone
		switch transport.Flow {
		case "rtscts":
			flow = serial.FlowHardware
		case "xonxoff":
			flow = serial.FlowSoftware
		}
		line, err := serial.ParseLine(transport.Line, flow)
		if err != nil {
			return false
		}
		host.Protocol, host.Line = protocolSerial, line
		host.Backspace = restoredBackspace(transport.Backspace)
	default:
		return false
	}
	return true
}

// restoredBackspace reads the saved Backspace key. A missing value, or one
// that names no choice, opens on the default, as the flow control does.
func restoredBackspace(saved string) backspaceKey {
	if saved == "ctrl-h" {
		return backspaceCtrlH
	}
	return backspaceDEL
}

// validEndpoint reports whether address can name a destination of the protocol:
// a host name for the network protocols, a device name for a serial port.
func validEndpoint(protocol hostProtocol, address string) bool {
	if protocol == protocolWSL {
		_, valid := localshell.WSLDistro(localshell.WSLShell(address))
		return address == "" || valid
	}
	if protocol != protocolSerial {
		return validHost(address)
	}
	if address == "" || len(address) > 256 || strings.HasPrefix(address, "-") {
		return false
	}
	for _, r := range address {
		if r < ' ' || r == 0x7f {
			return false
		}
	}
	return true
}

// sameEndpoint reports whether two hosts are the same destination: for SSH and
// Telnet the address, account and port, for a serial port the device. The
// terminal transport is part of an SSH destination: typing its SSH address must
// not silently open the mosh host saved for it.
func sameEndpoint(a Host, b Host) bool {
	if a.isWSL() && b.isWSL() {
		return strings.EqualFold(a.Address, b.Address) && a.User == b.User
	}
	if a.Protocol != b.Protocol || a.Terminal != b.Terminal || a.Address != b.Address {
		return false
	}
	return a.Protocol == protocolSerial || a.User == b.User && a.Port == b.Port && a.ViaSSH == b.ViaSSH
}

// streamRequest is what opening a Telnet or serial stream needs.
type streamRequest struct {
	Host    Host
	Timeout time.Duration
	Proxy   *sshconn.Proxy
	Notify  func()
}

// openStream opens the stream a Telnet or serial host describes. Tests replace
// it on the state with one that talks to no network or device.
func openStream(ctx context.Context, request streamRequest) (Shell, error) {
	host := request.Host
	switch host.Protocol {
	case protocolTelnet:
		address := net.JoinHostPort(host.Address, strconv.Itoa(int(host.Port)))
		var dialer telnet.Dialer
		if request.Proxy.Uses(address) {
			dialer = func(ctx context.Context, address string) (net.Conn, error) {
				return request.Proxy.DialAs(ctx, request.Timeout, address, host.User)
			}
		}
		session, err := telnet.Dial(ctx, address, request.Timeout, dialer, telnet.Config{Cols: 80, Rows: 24, Notify: request.Notify})
		if err != nil {
			return nil, err // not the typed nil a Shell would hold
		}
		return session, nil
	case protocolSerial:
		session, err := serial.Start(serial.Config{Device: host.Address, Settings: host.Line, Notify: request.Notify})
		if err != nil {
			return nil, err
		}
		return session, nil
	}
	return nil, errors.New("workbench: not a stream protocol")
}

// errNoServer is what the capabilities only an SSH server has say to a Telnet or
// serial session.
var errNoServer = errors.New("workbench: not available without an SSH server")

// streamConn is the Connection of a Telnet or serial tab. It dials in the
// background, so the tab shows the connection page while it waits, and hands the
// stream to the terminal once it is up. There are no credentials to ask for:
// whoever answers, a login prompt appears in the terminal itself.
type streamConn struct {
	method string
	notify func()
	cancel context.CancelFunc

	mu     sync.Mutex
	status sshconn.Status
	shell  Shell
}

// newStreamConn starts dialing host and returns at once.
func (state *State) newStreamConn(host Host, notify func()) *streamConn {
	ctx, cancel := context.WithCancel(context.Background())
	conn := &streamConn{notify: notify, cancel: cancel, method: "telnet"}
	if host.Protocol == protocolSerial {
		conn.method = "serial"
	}
	conn.status = sshconn.Status{Version: 1, Phase: sshconn.PhaseConnecting}
	request := streamRequest{Host: host, Timeout: state.connectionTimeout, Proxy: state.proxyForHost(host), Notify: notify}
	open := state.openStream
	if open == nil {
		open = openStream
	}
	go conn.run(ctx, open, request)
	return conn
}

func (conn *streamConn) run(ctx context.Context, open func(context.Context, streamRequest) (Shell, error), request streamRequest) {
	shell, err := open(ctx, request)
	conn.mu.Lock()
	switch {
	case ctx.Err() != nil:
		// Cancelled while dialing: nobody is waiting for the result.
		conn.status.Version++
		conn.status.Phase = sshconn.PhaseClosed
		if shell != nil {
			defer shell.Close()
		}
	case err != nil:
		conn.status.Version++
		conn.status.Phase = sshconn.PhaseFailed
		conn.status.Err = err
	default:
		conn.shell = shell
		conn.status.Version++
		conn.status.Phase = sshconn.PhaseConnected
		conn.status.Method = conn.method
	}
	conn.mu.Unlock()
	if conn.notify != nil {
		conn.notify()
	}
}

func (conn *streamConn) Status() sshconn.Status {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.status
}

func (conn *streamConn) TrustHostKey(bool)          {}
func (conn *streamConn) RejectHostKey()             {}
func (conn *streamConn) SubmitPassword(string)      {}
func (conn *streamConn) SubmitAnswers([]string)     {}
func (conn *streamConn) SubmitKeyPassphrase(string) {}
func (conn *streamConn) SkipIdentity()              {}

// Cancel stops dialing, or lets go of a stream the terminal has not taken.
func (conn *streamConn) Cancel() {
	conn.cancel()
	conn.mu.Lock()
	shell := conn.shell
	conn.shell = nil
	conn.mu.Unlock()
	if shell != nil {
		shell.Close()
	}
}

// OpenShell hands over the stream the dial produced, at the terminal's size.
func (conn *streamConn) OpenShell(config sshconn.ShellConfig) (Shell, error) {
	conn.mu.Lock()
	shell := conn.shell
	conn.shell = nil
	conn.mu.Unlock()
	if shell == nil {
		return nil, errors.New("workbench: the connection has no stream")
	}
	shell.Resize(config.Size())
	return shell, nil
}

func (conn *streamConn) OpenSFTP() (sftpclient.Remote, error) { return nil, errNoServer }

func (conn *streamConn) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errNoServer
}

func (conn *streamConn) DialShellContext(context.Context, string, string) (net.Conn, error) {
	return nil, errNoServer
}

func (conn *streamConn) Listen(string, string) (net.Listener, error) { return nil, errNoServer }

func (conn *streamConn) ListenContext(context.Context, string, string, func(net.Listener) error) (net.Listener, error) {
	return nil, errNoServer
}

func (conn *streamConn) RunCommand(context.Context, string, int) ([]byte, error) {
	return nil, errNoServer
}

func (conn *streamConn) Exec(context.Context, string, int) (sshconn.ExecResult, error) {
	return sshconn.ExecResult{}, errNoServer
}

func (conn *streamConn) StreamCommand(context.Context, string, int, func([]byte) bool) error {
	return errNoServer
}

// lineText is how a serial host's line is shown: "115200 8N1", with its flow
// control when it has one.
func lineText(line serial.Settings) string {
	switch line.Flow {
	case serial.FlowHardware:
		return line.String() + " RTS/CTS"
	case serial.FlowSoftware:
		return line.String() + " XON/XOFF"
	}
	return line.String()
}

// hostTarget is how the address column shows a destination: a Telnet or VNC host
// with its scheme, since a bare name reads as SSH, and a serial port with its line.
func hostTarget(host Host) string {
	switch host.Protocol {
	case protocolWSL:
		if host.Address == "" {
			return "WSL"
		}
		return host.Address
	case protocolVNC:
		target := "vnc://" + host.Address
		if host.ViaSSH {
			target = "vnc+ssh://"
			if host.User != "" {
				target += host.User + "@"
			}
			target += host.Address
		}
		if host.Port != defaultVNCPort {
			target += ":" + strconv.Itoa(int(host.Port))
		}
		return target
	case protocolRDP:
		target := "rdp://"
		if host.ViaSSH {
			target = "rdp+ssh://"
		}
		target += host.Address
		if host.Port != defaultRDPPort {
			target += ":" + strconv.Itoa(int(host.Port))
		}
		return target
	case protocolSerial:
		return fmt.Sprintf("%s · %s", host.Address, lineText(host.Line))
	case protocolTelnet:
		target := "telnet://" + host.Address
		if host.Port != defaultTelnetPort {
			target += ":" + strconv.Itoa(int(host.Port))
		}
		return target
	}
	if host.Port != defaultPort {
		return host.Address + ":" + strconv.Itoa(int(host.Port))
	}
	return host.Address
}

// normalizeEndpoint gives a host the port its protocol defaults to when it has
// none, and takes the account and port away from one that has no use for them.
func (host *Host) normalizeEndpoint() {
	if host.isWSL() {
		host.Port, host.System = 0, wslHostSystem(host.Address)
		host.Credential, host.Proxy, host.ProxyProfile, host.ProxyBypass = "", nil, "", ""
		host.Encoding, host.IdleKeepAlive, host.IdleInput, host.IdleInputCustom = "", 0, idleInputProtocolDefault, ""
		return
	}
	if host.Protocol == protocolSerial {
		host.User, host.Port = "", 0
		return
	}
	if host.Port == 0 {
		host.Port = host.defaultPort()
	}
}

// sameHost reports whether two hosts are one entry as the user sees it: the same
// destination under the same name. A host is identified by its catalog ID, not
// by where it connects, so several hosts may share a destination as long as they
// are named apart: a bastion host is reached at one address, and each target
// behind it is its own host. This is only the test for an accidental repeat, such
// as adding or importing the same entry twice.
func sameHost(a Host, b Host) bool {
	return sameEndpoint(a, b) && strings.EqualFold(strings.TrimSpace(a.Label), strings.TrimSpace(b.Label))
}

// sameHostGroup reports whether two hosts are filed under one group. A host may
// repeat a name and a destination in another group: the user keeps the same
// server in two places on purpose, and the rows are told apart by the group they
// sit in. A repeat inside one group is still the accidental duplicate the form
// refuses.
func sameHostGroup(a Host, b Host) bool {
	return sameHostGroupPath(a, b.Group)
}

// sameHostGroupPath reports whether host is filed under group, as sameHostGroup
// compares two hosts.
func sameHostGroupPath(host Host, group string) bool {
	return normalizeHostGroupPath(host.Group) == normalizeHostGroupPath(group)
}

// hostGroupTaken reports whether hosts already hold this destination under this
// name in group, other than by the host itself. Moving a host there would make
// the accidental duplicate the form refuses, and the fold that reconciles hosts
// saved on two devices would drop one of the two on the next start.
//
// It reads the fold's own identity (mergeKeyOf, with the group swapped for the
// one the host would land in) so the guard and the fold can never disagree about
// which rows are the same host.
func hostGroupTaken(hosts []Host, host Host, group string) bool {
	want := mergeKeyInGroup(host, group)
	for _, other := range hosts {
		if other.ID != host.ID && mergeKeyOf(other) == want {
			return true
		}
	}
	return false
}

// mergeKeyInGroup is mergeKeyOf(host) with the group replaced: the identity the
// fold would use for host once it is filed under group.
func mergeKeyInGroup(host Host, group string) mergeKey {
	host.Group = group
	return mergeKeyOf(host)
}

// groupMoveRepeat reports the first moving host the fold would drop once the
// moving hosts (given by their indexes in hosts) are all filed under group: one
// that group already holds under the same identity, or one that repeats another
// host moving with it. Hosts already in group change nothing and are left out of
// moving by the caller.
//
// It keys hosts by mergeKeyOf, the fold's own identity, so the check is one
// linear pass instead of a scan per host and a pairwise pass over the selection,
// and it cannot drift from the fold as that key gains fields.
func groupMoveRepeat(hosts []Host, moving []int, group string) (Host, bool) {
	movingSet := make(map[uint32]bool, len(moving))
	for _, index := range moving {
		movingSet[hosts[index].ID] = true
	}
	seen := make(map[mergeKey]Host, len(hosts))
	for _, host := range hosts {
		key := mergeKeyOf(host)
		if movingSet[host.ID] {
			key = mergeKeyInGroup(host, group)
		}
		if other, ok := seen[key]; ok && other.ID != host.ID {
			if movingSet[host.ID] {
				return host, true
			}
			if movingSet[other.ID] {
				return other, true
			}
		}
		seen[key] = host
	}
	return Host{}, false
}

// hostIndexByEndpoint is where the list has a host that is the same destination
// as host, or -1. The SSH config sync uses it to tell a block that only names a
// server already saved from a host of its own. A session imported from another
// client is identified by its name as well (see sameHost), because one bastion
// address can hold several saved sessions.
func (state *State) hostIndexByEndpoint(host Host) int {
	for index, saved := range state.hosts {
		if sameEndpoint(saved, host) {
			return index
		}
	}
	return -1
}

// hostIndexSame is where the list has the same host as host (see sameHost), filed
// under the group host is, or -1.
func (state *State) hostIndexSame(host Host) int {
	for index, saved := range state.hosts {
		if sameHost(saved, host) && sameHostGroup(saved, host) {
			return index
		}
	}
	return -1
}

// hostIndexSameName is where the list already holds host's name at its
// destination, whatever group that host is filed in, or -1. An import asks this
// rather than hostIndexSame: a session the user already saved is not imported
// again just because the file files it under another group.
func (state *State) hostIndexSameName(host Host) int {
	for index, saved := range state.hosts {
		if sameHost(saved, host) {
			return index
		}
	}
	return -1
}

// endpointShared reports whether a host of the list other than those with the
// given IDs connects to the destination of host.
func (state *State) endpointShared(host Host, except ...uint32) bool {
	for _, other := range state.hosts {
		if !slices.Contains(except, other.ID) && sameEndpoint(other, host) {
			return true
		}
	}
	return false
}

// sayStreamOpened writes the one line a Telnet or serial tab starts with: that
// Telnet is not encrypted, or which port was opened at what line.
func (state *State) sayStreamOpened(session *Session) {
	m := state.messages.Tools
	switch session.host.Protocol {
	case protocolTelnet:
		session.term.say(sgrWarn, m.TelnetPlaintext)
	case protocolVNC:
		protected := session.host.tunneled() || session.desktopTunnel
		if source, ok := session.conn.(desktopSource); ok && source.Desktop() != nil {
			protected = protected || source.Desktop().Info().Encrypted
		}
		if !protected {
			session.term.say(sgrWarn, state.messages.Desktop.VNCPlaintext)
		}
	case protocolSerial:
		session.term.say(sgrMuted, fmt.Sprintf(m.SerialOpenedFmt, session.host.Address, lineText(session.host.Line)))
	}
}
