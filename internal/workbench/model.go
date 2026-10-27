package workbench

import (
	"time"

	"motty/internal/serial"
	"motty/internal/sshconn"
)

// Host is one saved destination: an SSH server unless Protocol says otherwise. CatalogID is stable across restarts and
// exports; ID is the run-local hit identity and target is derived.
type Host struct {
	ID        uint32
	CatalogID string
	Label     string
	Group     string
	Tags      string
	Icon      hostIcon
	Color     string // optional #RRGGBB connection accent
	Address   string
	User      string
	Port      uint16
	System    sshconn.RemoteSystem
	// Protocol says how the host is reached. A serial host keeps its device in
	// Address and its line settings in Line; it has no user or port.
	Protocol hostProtocol
	Line     serial.Settings
	// Algo, when set, replaces the default SSH algorithms for this host. Nil is
	// the default, so a host that never asked costs one pointer.
	Algo *hostAlgo
	// ForwardX11 asks the server to forward the X11 connections of this host's
	// shells to the local display. Off unless the user turned it on.
	ForwardX11 bool
	// AgentForward asks the server to let programs in this host's shells use
	// the local ssh-agent. Off unless the user turned it on.
	AgentForward bool
	// NoHistory keeps what is typed in this host's terminals out of the command
	// history, whatever the global switch says.
	NoHistory bool
	// Transfer is the file protocol of the host's file panel.
	Transfer transferMode
	// Proxy overrides the global outbound proxy for this host. Nil inherits the
	// global proxy; an explicit ProxyNone forces a direct connection.
	Proxy *outboundProxy
	// ViaSSH and Clipboard are a VNC host's: its server is reached through an SSH
	// connection to Address (User is then the SSH account), and the clipboard is
	// shared with the remote desktop.
	ViaSSH    bool
	Clipboard bool

	// target is how the address column shows the destination: the host name,
	// plus the port when it is not the default. Built once, not per paint.
	target string
}

// defaultPort is the SSH port a host without an explicit one uses.
const defaultPort = 22

// finish fills the derived fields.
func (host *Host) finish() {
	if host.CatalogID == "" {
		host.CatalogID = catalogIDFor(*host)
	}
	host.target = hostTarget(*host)
}

// Session is one terminal tab. Transport and terminal state stay separate from
// presentation state: conn owns the network, term holds the screen and the
// shell, the other fields are what the window last read from the connection.
type Session struct {
	ID       uint32
	HostID   uint32
	Title    string
	Address  string
	paneSlot uint8

	host     Host // what was dialed, to dial again
	jumpPath string
	// proxyRoute is the effective outbound proxy frozen when this connection
	// was dialed. Editing the host updates host, but not the live transport.
	proxyRoute string
	endpoint   sshconn.Endpoint
	role       sshconn.EndpointRole
	conn       Connection
	shared     bool // conn is a tab on another tab's connection
	// An interactive remote command for this tab only; it is not persisted as a host setting.
	containerCommand string
	// avoidShare makes the next connection a fresh one: the server refused one
	// more channel on the shared connection.
	avoidShare bool
	// notified is when this terminal last had a notification shown.
	notified  time.Duration
	didNotify bool
	// filledSecret is the saved secret the last question was answered with, to be
	// forgotten if the server refuses it. pendingSecret* is what the user asked
	// to save, kept until the server accepts it.
	filledSecret       string
	pendingSecretName  string
	pendingSecretValue string
	term               *terminal
	// desk is the remote desktop a VNC tab shows; nil for every other kind.
	desk     *desktopView
	version  uint64
	phase    sshconn.Phase
	attempt  int
	answered uint64
	// otpSent is set once a one-time code was typed for this connection, so a
	// refused one is never sent again by itself.
	otpSent   bool
	busy      bool
	tone      tone
	hostKey   sshconn.HostKeyInfo
	questions []sshconn.Question
	identity  string
	files     *sftpView
	// relay is where this session's background goroutines report progress. It
	// points at the window the session is in, and moves with the session.
	relay *notifyRelay
	stats *remoteStatsView

	// The connection page. stage is the furthest step the connection reached
	// (see stageConnect...), since and finished bound the elapsed time (finished
	// stays zero while the connection is still being made), and showLog swaps
	// the page for the terminal that holds the full log. The strings are what
	// the page says in the current language, built when the connection changes
	// or the language does rather than on every paint.
	stage         int
	since         time.Duration
	finished      time.Duration
	showLog       bool
	serverVersion string
	method        string
	err           error  // why the connection or its shell failed
	shellFailed   bool   // err came from asking for the shell
	restored      bool   // tab metadata was restored without opening a connection
	autoRestore   bool   // the connection in progress was started by restoring the session
	restoreNote   string // why restore left this tab unconnected, in the current language
	details       [stageCount]string
	failure       string
	elapsed       string
	elapsedSecond int64
	railShown     float32       // where the ball is on the connection rail, easing to where the connection stands
	railFrom      float32       // where it was when it last set out
	railTo        float32       // where it is going
	railStart     time.Duration // when it set out

	established        bool
	reconnectAttempt   int
	reconnectDue       time.Duration
	reconnectPending   bool
	reconnectExhausted bool
	reconnectText      string
	reconnectSecond    int64
	connectionStory    []connectionEvent
}
