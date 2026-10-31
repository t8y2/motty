package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"strconv"
	"strings"
	"sync"
	"time"

	"motty/internal/serial"
	"motty/internal/sftpclient"
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

func (state *State) sshHostCount() int {
	count := 0
	for _, host := range state.hosts {
		if host.isSSH() {
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
	}
	return defaultPort
}

// isDesktop reports whether the host is a remote desktop: its tab shows a
// picture, not a terminal.
func (host Host) isDesktop() bool {
	return host.Protocol == protocolVNC || host.Protocol == protocolRDP
}

// tunneled reports whether the host is reached through an SSH connection of its
// own: a remote desktop whose server is only listening on the SSH server's own
// loopback.
func (host Host) tunneled() bool { return host.isDesktop() && host.ViaSSH }

// hasServer reports whether the session talks to an SSH server: the only kind
// that can list files, forward ports and run commands.
func (session *Session) hasServer() bool {
	return !session.isLocal() && session.host.isSSH()
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
	Protocol string `json:"protocol,omitempty"` // "telnet", "serial", "vnc" or "rdp"
	Line     string `json:"line,omitempty"`     // serial: "115200 8N1"
	Flow     string `json:"flow,omitempty"`     // serial: "rtscts" or "xonxoff"
	// Proxy is nil when this host inherits the global outbound proxy. An entry
	// whose kind is "direct" explicitly bypasses it.
	Proxy *savedOutboundProxy `json:"proxy,omitempty"`
	// Algorithms is "compatible" or "custom" for an SSH host that does not use
	// the default algorithms; AlgorithmList is the custom list.
	Algorithms    string `json:"algorithms,omitempty"`
	AlgorithmList string `json:"algorithm_list,omitempty"`
	// Authentication is "password", "key" or "agent" when an SSH host does
	// not use the automatic authentication order. Identity optionally selects
	// one imported key fingerprint for the key policy.
	Authentication string `json:"authentication,omitempty"`
	Identity       string `json:"identity,omitempty"`
	// ForwardX11 is set for an SSH host that forwards X11.
	ForwardX11 bool `json:"forward_x11,omitempty"`
	// AgentForward is set for an SSH host whose shells may use the local
	// ssh-agent.
	AgentForward bool `json:"agent_forward,omitempty"`
	// NoHistory is set for an SSH host whose commands are not remembered.
	NoHistory bool `json:"no_history,omitempty"`
	// LoginCommand is the line an SSH host types into its shell after login.
	LoginCommand string `json:"login_command,omitempty"`
	// Transfer is "sftp" or "scp" for an SSH host whose file panel does not
	// choose by itself.
	Transfer string `json:"transfer,omitempty"`
	// ViaSSH and Clipboard are a remote desktop host's (VNC or RDP): reached
	// through an SSH connection to the same machine, and sharing the clipboard
	// with the remote desktop. Console is an RDP host's: connect to the server's
	// console session instead of a new one, as mstsc /admin does. The password is
	// never here: it is asked for, and kept in the vault only if the user says so.
	ViaSSH    bool `json:"via_ssh,omitempty"`
	Clipboard bool `json:"clipboard,omitempty"`
	Console   bool `json:"console,omitempty"`
}

func (host Host) transport() savedTransport {
	transport := savedTransport{Proxy: savedHostProxy(host.Proxy)}
	switch host.Protocol {
	case protocolTelnet:
		transport.Protocol = "telnet"
		return transport
	case protocolVNC:
		transport.Protocol, transport.ViaSSH, transport.Clipboard = "vnc", host.ViaSSH, host.Clipboard
		return transport
	case protocolRDP:
		transport.Protocol, transport.ViaSSH, transport.Clipboard = "rdp", host.ViaSSH, host.Clipboard
		transport.Console = host.Console
		return transport
	case protocolSerial:
		transport.Proxy, transport.Protocol, transport.Line = nil, "serial", host.Line.String()
		switch host.Line.Flow {
		case serial.FlowHardware:
			transport.Flow = "rtscts"
		case serial.FlowSoftware:
			transport.Flow = "xonxoff"
		}
		return transport
	}
	mode, list := host.savedAlgo()
	transport.Algorithms, transport.AlgorithmList = mode, list
	transport.Authentication, transport.Identity = host.Auth.saved(), host.Identity
	transport.ForwardX11, transport.AgentForward, transport.NoHistory, transport.Transfer = host.ForwardX11, host.AgentForward, host.NoHistory, host.savedTransfer()
	transport.LoginCommand = host.LoginCommand
	return transport
}

// applyTransport reads a saved transport into the host. It reports false for a
// serial line that cannot be used, so a damaged entry is dropped rather than
// opened with a guess.
func (host *Host) applyTransport(transport savedTransport) bool {
	host.Proxy = restoredHostProxy(transport.Proxy)
	switch transport.Protocol {
	case "", "ssh":
		host.Protocol = protocolSSH
		var ok bool
		if host.Auth, ok = parseHostAuth(transport.Authentication); !ok {
			return false
		}
		host.Identity = strings.TrimSpace(transport.Identity)
		if host.Auth != authPrivateKey {
			host.Identity = ""
		}
		host.ForwardX11 = transport.ForwardX11
		host.AgentForward = transport.AgentForward
		host.NoHistory = transport.NoHistory
		host.LoginCommand = restoredLoginCommand(transport.LoginCommand)
		return host.applyTransfer(transport.Transfer) && host.applyAlgo(transport.Algorithms, transport.AlgorithmList)
	case "telnet":
		host.Protocol = protocolTelnet
	case "vnc":
		host.Protocol, host.ViaSSH, host.Clipboard = protocolVNC, transport.ViaSSH, transport.Clipboard
	case "rdp":
		host.Protocol, host.ViaSSH, host.Clipboard = protocolRDP, transport.ViaSSH, transport.Clipboard
		host.Console = transport.Console
	case "serial":
		host.Proxy = nil
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
	default:
		return false
	}
	return true
}

// validEndpoint reports whether address can name a destination of the protocol:
// a host name for the network protocols, a device name for a serial port.
func validEndpoint(protocol hostProtocol, address string) bool {
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
// Telnet the address, account and port, for a serial port the device.
func sameEndpoint(a Host, b Host) bool {
	if a.Protocol != b.Protocol || a.Address != b.Address {
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
				return request.Proxy.Dial(ctx, request.Timeout, address)
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
	shell.Resize(config.Cols, config.Rows)
	return shell, nil
}

func (conn *streamConn) OpenSFTP() (sftpclient.Remote, error) { return nil, errNoServer }

func (conn *streamConn) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errNoServer
}

func (conn *streamConn) Listen(string, string) (net.Listener, error) { return nil, errNoServer }

func (conn *streamConn) RunCommand(context.Context, string, int) ([]byte, error) {
	return nil, errNoServer
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
	if host.Protocol == protocolSerial {
		host.User, host.Port = "", 0
		return
	}
	if host.Protocol == protocolVNC && !host.ViaSSH {
		host.User = "" // the account is the SSH tunnel's
	}
	if host.Port == 0 {
		host.Port = host.defaultPort()
	}
}

// hostIndexByEndpoint is where the list has a host that is the same destination
// as host, or -1.
func (state *State) hostIndexByEndpoint(host Host) int {
	for index, saved := range state.hosts {
		if sameEndpoint(saved, host) {
			return index
		}
	}
	return -1
}

// sayStreamOpened writes the one line a Telnet or serial tab starts with: that
// Telnet is not encrypted, or which port was opened at what line.
func (state *State) sayStreamOpened(session *Session) {
	m := state.messages.Tools
	switch session.host.Protocol {
	case protocolTelnet:
		session.term.say(sgrWarn, m.TelnetPlaintext)
	case protocolVNC:
		if !session.host.tunneled() {
			session.term.say(sgrWarn, state.messages.Desktop.VNCPlaintext)
		}
	case protocolSerial:
		session.term.say(sgrMuted, fmt.Sprintf(m.SerialOpenedFmt, session.host.Address, lineText(session.host.Line)))
	}
}
