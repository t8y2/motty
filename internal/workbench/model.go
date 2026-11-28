package workbench

import (
	"time"

	"motty/internal/desktop"
	"motty/internal/serial"
	"motty/internal/sessionlog"
	"motty/internal/sshbrowser"
	"motty/internal/sshconfig"
	"motty/internal/sshconn"
	"motty/internal/term"
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
	// Terminal is how an SSH host's interactive session is carried. The zero
	// value is SSH itself; terminalMosh bootstraps mosh-server over SSH and
	// carries the terminal over UDP. It is meaningful only for protocolSSH.
	Terminal hostTerminal
	Line     serial.Settings
	// Backspace is the byte the Backspace key sends: DEL, or Ctrl+H for a
	// console whose line editor erases on BS alone, or an SSH server that
	// expects BS. It is also the pty's erase character for an SSH host.
	// Meaningful only where hostAsksBackspace says the host has the choice.
	Backspace backspaceKey
	// A WSL host uses Address for its distribution and User for the Linux
	// account. Both may be empty to use WSL's defaults.
	WSLDirectory string
	WSLNoGUI     bool
	// Algo, when set, replaces the default SSH algorithms for this host. Nil is
	// the default, so a host that never asked costs one pointer.
	Algo *hostAlgo
	// Auth limits which credentials an SSH connection offers. Identity is the
	// fingerprint of a specific imported private key when Auth is authPrivateKey;
	// an empty value lets that mode try the default and imported keys.
	Auth       hostAuth
	Identity   string
	SSHOptions *sshconfig.ConnectionOptions
	// IdentitiesOnly keeps automatic authentication to the identities
	// configured here: the ssh-agent is offered only the keys that match a
	// default, imported or host-specific identity, so an agent holding many
	// keys cannot spend the server's authentication budget first.
	IdentitiesOnly bool
	// Credential is the id of the saved password (see credentials.go) the host
	// takes its password from; empty keeps the host's own. An id that no saved
	// password has is treated as empty.
	Credential string
	// ForwardX11 asks the server to forward the X11 connections of this host's
	// shells to the local display. Off unless the user turned it on.
	ForwardX11 bool
	// AgentForward asks the server to let programs in this host's shells use
	// the local ssh-agent. Off unless the user turned it on.
	AgentForward bool
	// InteractiveOnly keeps this host to its interactive session: no command
	// probes, shell integration or locale requests are sent beside the shell.
	// It is how a gateway that permits only an interactive login is reached,
	// and is learned from the server that refuses the first probe.
	InteractiveOnly bool
	// NoHistory keeps what is typed in this host's terminals out of the command
	// history, whatever the global switch says.
	NoHistory bool
	// LoginCommand is typed into each new shell of an SSH host once it is ready
	// for input, as the user would type it. One or more lines, empty for none.
	LoginCommand         string
	LoginCommandMode     loginSendMode
	LoginCommandInterval time.Duration
	LoginCommandPolicy   loginFailurePolicy
	RemoteLogPaths       []remoteLogPath
	// Encoding is the character set of the remote shell's byte stream. Empty
	// means UTF-8. A legacy server whose LANG names GB18030, Big5 or Shift_JIS
	// needs its output decoded and its input encoded with that charset.
	Encoding string
	// IdleKeepAlive types input into an idle shell at this interval, which some
	// servers require to keep a session from being closed for inactivity. Zero
	// is off. IdleInput chooses what is typed, and IdleInputCustom holds the
	// escaped text when that choice is custom.
	IdleKeepAlive   time.Duration
	IdleInput       idleInputMode
	IdleInputCustom string
	// tunnel marks the ephemeral host of a .xsh bastion tunnel opened on this
	// computer. It is never saved, so only that launch gets the relaxed policy.
	tunnel bool
	// linkPassword is the password a link carried in its userinfo. It answers
	// that one connection and is cleared when it is used; it has no saved
	// field, so it never reaches the state file.
	linkPassword string
	// Transfer is the file protocol of the host's file panel.
	Transfer transferMode
	// Proxy overrides the global outbound proxy for this host. Nil inherits the
	// global proxy; an explicit ProxyNone forces a direct connection.
	Proxy *outboundProxy
	// ProxyProfile, when set, is the id of a saved proxy profile (see
	// outbound_proxy_profiles.go) that carries this host instead of Proxy. The
	// profile wins over Proxy; an id no profile has treats the host as direct.
	// ProxyBypass is this host's own direct list for that route, since the
	// profile is shared by several hosts.
	ProxyProfile string
	ProxyBypass  string
	// ViaSSH and Clipboard are a remote desktop host's: its server is reached
	// through an SSH connection to Address (User is then the SSH account), and
	// the clipboard is shared with the remote desktop. Console is an RDP host's:
	// it asks Windows for its console (administrative) session instead of a new
	// one. NoAudio is an RDP host's too: the server is asked for no audio, and
	// this computer opens no output device for the session. Microphone is an
	// RDP host's too: this computer's default microphone is redirected to the
	// server when a remote application records. StandardSecurity is
	// an RDP host's as well: the connection uses Standard RDP Security alone,
	// with no TLS or NLA, for a server that requires it. LegacyTLS is an RDP
	// host's too: the TLS offer includes the RSA key exchange suites (AES-CBC
	// and AES-GCM) that an old server offers and Go leaves out by default.
	// Chromeless is a desktop host's: its page shows the picture alone,
	// without the bar above it and the status line under it.
	ViaSSH           bool
	Clipboard        bool
	Console          bool
	NoAudio          bool
	Microphone       bool
	StandardSecurity bool
	LegacyTLS        bool
	Chromeless       bool
	// ColorDepth, PreferredEncoding and Effects are a remote desktop host's
	// quality and bandwidth choices; the zero values are what a host has always
	// had. ColorDepth is the bits per pixel to ask for (VNC 32, 16 or 8; RDP 32,
	// 24 or 16), PreferredEncoding the VNC encoding listed first, and Effects
	// the visual effects an RDP server may render; nil is desktop.DefaultEffects.
	// Effects is never modified in place, so hosts may share it.
	ColorDepth        int
	PreferredEncoding desktop.Encoding
	Effects           *desktop.Effects
	// DesktopWidth and DesktopHeight are the remote resolution an RDP host asks
	// for; zero lets the desktop follow the pane.
	DesktopWidth, DesktopHeight int

	// localShell is the shell a local terminal was opened with: a program path,
	// or a WSL distribution as localshell.WSLShell names it. Empty starts the
	// default shell of the settings. Only the local host has one, and it is
	// never saved with the catalog.
	localShell string

	// target is how the address column shows the destination: the host name,
	// plus the port when it is not the default. Built once, not per paint.
	target string
}

type reuseHostPolicy struct {
	line             serial.Settings
	backspace        backspaceKey
	encoding         string
	user             string
	localShell       string
	wslDirectory     string
	wslNoGUI         bool
	wslCommand       string
	viaSSH           bool
	clipboard        bool
	console          bool
	noAudio          bool
	microphone       bool
	standardSecurity bool
	legacyTLS        bool
	interactiveOnly  bool
	colorDepth       int
	desktopEncoding  desktop.Encoding
	effects          desktop.Effects
	desktopWidth     int
	desktopHeight    int
}

func reusePolicyFor(host Host) reuseHostPolicy {
	policy := reuseHostPolicy{line: host.Line, backspace: host.Backspace, encoding: host.Encoding, user: host.User,
		localShell: host.localShell, viaSSH: host.ViaSSH, clipboard: host.Clipboard,
		console: host.Console, noAudio: host.NoAudio, microphone: host.Microphone, standardSecurity: host.StandardSecurity,
		legacyTLS: host.LegacyTLS, interactiveOnly: host.InteractiveOnly,
		colorDepth: host.ColorDepth, desktopEncoding: host.PreferredEncoding, effects: host.desktopEffects(),
		desktopWidth: host.DesktopWidth, desktopHeight: host.DesktopHeight}
	if host.isWSL() {
		policy.wslDirectory, policy.wslNoGUI, policy.wslCommand = host.WSLDirectory, host.WSLNoGUI, host.LoginCommand
	}
	return policy
}

// hostAsksBackspace reports whether the form lets a host of the protocol and
// terminal choose its Backspace key. A serial line and Telnet have no handshake
// for the erase character, so a console whose line editor erases on BS alone
// needs the choice made per host. An SSH host has the choice too, for a server
// whose terminals erase with Ctrl+H; a mosh terminal runs on mosh-server, not on
// the SSH pty, so it has none.
func hostAsksBackspace(protocol hostProtocol, terminal hostTerminal) bool {
	if protocol == protocolSSH {
		return terminal != terminalMosh
	}
	return protocol == protocolSerial || protocol == protocolTelnet
}

// backspaceKey is the byte such a host's Backspace key sends: DEL, the
// terminal's own, or Ctrl+H.
type backspaceKey uint8

const (
	backspaceDEL backspaceKey = iota
	backspaceCtrlH
)

func (key backspaceKey) byte() byte {
	if key == backspaceCtrlH {
		return term.CtrlHByte
	}
	return term.DeleteByte
}

// desktopEffects is the effects an RDP host asks for: its own, or the defaults.
func (host Host) desktopEffects() desktop.Effects {
	if host.Effects != nil {
		return *host.Effects
	}
	return desktop.DefaultEffects()
}

// defaultPort is the SSH port a host without an explicit one uses.
const defaultPort = 22

// finish fills the derived fields.
func (host *Host) finish() {
	host.Group = normalizeHostGroupPath(host.Group)
	if host.CatalogID == "" {
		host.CatalogID = catalogIDFor(*host)
	}
	host.target = hostTarget(*host)
}

// Session is one terminal tab. Transport and terminal state stay separate from
// presentation state: conn owns the network, term holds the screen and the
// shell, the other fields are what the window last read from the connection.
type Session struct {
	remoteLogs       *remoteLogView
	workspaceCommand WorkspaceCommand     // immutable workspace configuration for this session
	loginResults     []LoginCommandResult // results of post-login commands executed in this session
	loginAborted     string               // reason remaining post-login commands were aborted
	outputAlert      terminalOutputAlert  // runtime only; connection status stays independent

	ID          uint32
	HostID      uint32
	Title       string
	customTitle bool // 仅用户命名或复制自定义名称时设置。
	// Mention is this session's unique @ name. Titles may repeat; the mention
	// stays distinct so a message names exactly one terminal.
	Mention  string
	Address  string // dialed address; changes to a saved host take effect on reconnect
	paneSlot uint8
	// localDirectory is this shell's last reported directory, independent of
	// the files panel and of other sessions on the same machine.
	localDirectory string
	// startDirectory is where this remote shell starts: the last directory a
	// trusted prompt reported, or the place a split asked for. It is a hint to
	// the server, which may no longer have that directory.
	startDirectory string
	// pathCompletion is this session's directory cache and its own reader for
	// path completion. A pointer: it holds channels and a once, and the session
	// value is copied when a tab moves between windows.
	pathCompletion *pathCompletion

	host            Host // current host settings, used on the next dial
	dialedProtocol  hostProtocol
	dialedTerminal  hostTerminal
	jumpPath        string
	jumpPolicies    []jumpPolicy
	reuseRoute      connectionRoute
	reuseRouteValid bool
	reusePolicy     reuseHostPolicy
	reuseOptions    *sshconfig.ConnectionOptions
	// desktopTunnel says this remote desktop is reached over SSH: its ViaSSH
	// tunnel, or the jump route that carries its port. It is what the desktop's
	// own encryption and login rows follow, where host.tunneled() names only
	// the same-machine switch.
	desktopTunnel bool
	// proxyRoute is the effective outbound proxy frozen when this connection
	// was dialed. Editing the host updates host, but not the live transport.
	proxyRoute string
	// authentication is the policy this connection was dialed with. Like
	// sshOptions it is frozen at the dial: a transport authenticated one way is
	// not lent to a host that would authenticate another (see shareFrom).
	authentication sshconn.Authentication
	// These are the host-owned authentication settings at the time of the dial.
	// Keep them separate from authentication: the resolved policy also contains
	// the global key library, which may change without editing this host.
	dialedHostAuth           hostAuth
	dialedHostIdentity       string
	dialedHostCredential     string
	dialedHostIdentitiesOnly bool
	dialedAlgoMode           string
	dialedAlgoList           string
	// interactiveOnly is the probe policy this connection was dialed with,
	// frozen like authentication. A transport opened for a host confined to its
	// interactive session sent no command probes and carries TCP keepalive, so
	// another tab may not borrow it after the host is edited (see shareFrom).
	interactiveOnly bool
	sshOptions      *sshconfig.ConnectionOptions
	endpoint        sshconn.Endpoint
	role            sshconn.EndpointRole
	conn            Connection
	browser         *sshbrowser.Browser
	sudoCommands    bool
	sudoPassword    string
	recordRecent    bool // this user dial may update its recent success once
	shared          bool // conn is a tab on another tab's connection
	// An interactive remote command for this tab only; it is not persisted as a host setting.
	containerCommand string
	// avoidShare makes the next connection a fresh one: the server refused one
	// more channel on the shared connection.
	avoidShare bool
	// fileConnectionSeparate says the server refused a session channel on this
	// terminal's transport, so the file panel opens its own connection instead
	// of asking it again. A new terminal connection forgets it.
	fileConnectionSeparate bool
	// notified is when this terminal last had a notification shown.
	notified  time.Duration
	didNotify bool
	// belled is when this terminal last rang the bell.
	belled  time.Duration
	didBell bool
	// filledSecret is the saved secret the last question was answered with, to be
	// forgotten if the server refuses it. pendingSecret* is what the user asked
	// to save, kept until the server accepts it.
	filledSecret       string
	pendingSecretName  string
	pendingSecretValue string
	term               *terminal
	// transfer is this terminal's ZMODEM or X/YMODEM transfer. Each session has
	// its own: the protocol uses that shell's bytes, so another tab can transfer
	// at the same time. rzsz is an assistant transfer armed here, waiting for
	// the remote rz or sz.
	transfer fileTransfer
	rzsz     *rzszJob
	// desk is the remote desktop a VNC tab shows; nil for every other kind.
	desk    *desktopView
	version uint64
	phase   sshconn.Phase
	attempt int
	// securityKey is set while the agent waits for a hardware key.
	securityKey bool
	answered    uint64
	// otpSent is set once a one-time code was typed for this connection, so a
	// refused one is never sent again by itself.
	otpSent   bool
	busy      bool
	tone      tone
	hostKey   sshconn.HostKeyInfo
	questions []sshconn.Question
	identity  string
	files     *sftpView

	// Idle pane history lives with this session, not its worker.
	fileHistory [2]sftpSavedHistory
	// pastingImage is set while a pasted image is on its way to the server.
	pastingImage bool
	// agent is the coding agent in this local terminal's foreground, and
	// agentWatch is when that was last read; both are cleared when the shell ends.
	agent      codingAgent
	agentWatch agentWatch
	// relay is where this session's background goroutines report progress. It
	// points at the window the session is in, and moves with the session.
	relay *notifyRelay
	stats *remoteStatsView

	// rttMs holds the latest measured round-trip time in milliseconds,
	// rttAvailable is 1 when rttMs is valid, rttMeasuring is 1 while a ping is
	// in flight, rttBroken stops probing after a failed request, rttGeneration
	// invalidates results from a previous connection, and rttLastPing records
	// the last probe, and rttSampledAt (Unix nanoseconds) when rttMs was
	// measured, so a reading that stopped being refreshed can be dropped.
	rttMs         int64
	rttSampledAt  int64
	rttAvailable  int32
	rttMeasuring  int32
	rttBroken     int32
	rttGeneration uint32
	rttLastPing   time.Time

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
	disconnected       bool // the user ended the connection on purpose; the closed status that follows is not a failure
	reconnectAttempt   int
	reconnectDue       time.Duration
	reconnectPending   bool
	reconnectStop      func()
	reconnectExhausted bool
	reconnectText      string
	reconnectSecond    int64
	connectionStory    []connectionEvent

	// logRecordID is the stable identifier of this tab's row in the session
	// journal. It is a random value, not the integer session ID: a tab keeps it
	// when it moves between panes or windows, and session IDs may be reused.
	// logStatus is what this window believes the row says, so a final status is
	// written once even though several close paths can reach it.
	logRecordID  string
	logStatus    sessionlog.Status
	logFinished  bool
	logBarrier   <-chan struct{} // earlier window's writes must land before this tab's writes
	logLastEvent sessionlog.EventKind

	// keptAfterShell: the shell ended but the file panel or window still uses the
	// connection, which closes with the last of them.
	keptAfterShell bool
	// localProxy is this session's subscription to the process-owned tunnel for
	// its exact authenticated SSH transport. Zero means it has not joined one.
	localProxy localProxySubscription
}
