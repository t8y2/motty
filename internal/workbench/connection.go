package workbench

import (
	"context"
	"errors"
	"fmt"
	"github.com/vivaui/viva/ui"
	"motty/internal/diag"
	"net"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"motty/internal/ai/llm"
	"motty/internal/ai/store"
	"motty/internal/aiproviderimport"
	"motty/internal/localshell"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	"motty/internal/update"
	"motty/internal/vault"
)

// Connection is what the workbench needs from one SSH connection. It is
// defined here, where it is used, so tests can stand in for the network;
// *sshconn.Conn satisfies it.
type Connection interface {
	Status() sshconn.Status
	TrustHostKey(save bool)
	RejectHostKey()
	SubmitPassword(password string)
	SubmitAnswers(answers []string)
	SubmitKeyPassphrase(passphrase string)
	SkipIdentity()
	Cancel()
	// OpenShell starts the interactive shell once the status says connected.
	OpenShell(config sshconn.ShellConfig) (Shell, error)
	// OpenSFTP starts an application-owned remote-file subsystem.
	OpenSFTP() (sftpclient.Remote, error)
	// DialContext opens a remote stream through the authenticated connection.
	DialContext(context.Context, string, string) (net.Conn, error)
	// Listen asks the authenticated server to open a remote listener.
	Listen(string, string) (net.Listener, error)
	// RunCommand executes one bounded command on the authenticated server.
	RunCommand(context.Context, string, int) ([]byte, error)
	// StreamCommand runs a long-lived command and hands over its stdout lines
	// as they arrive, until the callback stops or the context ends.
	StreamCommand(context.Context, string, int, func([]byte) bool) error
}

// sharer is a Connection that can lend its authenticated client to another tab.
// It is optional: a Connection that cannot (a shell on this computer) is simply
// dialed again.
type sharer interface {
	Share(notify func()) (Connection, error)
}

// realConn adapts *sshconn.Conn, whose OpenShell returns the concrete shell
// type, to Connection.
type realConn struct{ *sshconn.Conn }

func (conn realConn) OpenShell(config sshconn.ShellConfig) (Shell, error) {
	shell, err := conn.Conn.OpenShell(config)
	if err != nil {
		return nil, err
	}
	return shell, nil
}

// slowShellOpener marks a Connection whose OpenShell waits on the network, so
// the workbench opens its shell off the window's thread.
type slowShellOpener interface{ opensShellSlowly() }

func (realConn) opensShellSlowly() {}

// pinger is a Connection that can measure its network round-trip time.
type pinger interface {
	Ping(context.Context) (time.Duration, error)
}

func (conn realConn) Ping(ctx context.Context) (time.Duration, error) {
	return conn.Conn.Ping(ctx)
}

// Share opens another tab on this connection's authenticated client.
func (conn realConn) Share(notify func()) (Connection, error) {
	child, err := conn.Conn.Share(notify)
	if err != nil {
		return nil, err
	}
	return realConn{child}, nil
}

func (conn realConn) OpenSFTP() (sftpclient.Remote, error) {
	return sftpclient.Open(conn.Conn.Client())
}

// OpenSCP is the file backend for a server that has no SFTP subsystem.
func (conn realConn) OpenSCP() (sftpclient.Remote, error) {
	return sftpclient.OpenSCP(conn.Conn.Client())
}

// scpOpener is a Connection that can move files over SCP. It is optional: only
// a real SSH connection can.
type scpOpener interface {
	OpenSCP() (sftpclient.Remote, error)
}

var errNoSCP = errors.New("scp: this connection cannot use SCP")

// openRemoteFiles opens the file backend the host asks for. Automatic tries
// SFTP and falls back to SCP only when the server refuses the SFTP subsystem,
// never for a connection that failed.
func openRemoteFiles(conn Connection, mode transferMode) (sftpclient.Remote, error) {
	scp, canSCP := conn.(scpOpener)
	switch mode {
	case transferSCP:
		if !canSCP {
			return nil, errNoSCP
		}
		return scp.OpenSCP()
	case transferAuto:
		remote, err := conn.OpenSFTP()
		if err != nil && canSCP && sftpclient.IsSubsystemRefused(err) {
			return scp.OpenSCP()
		}
		return remote, err
	}
	return conn.OpenSFTP()
}

// Options configures the parts of the workbench that touch the outside world.
type Options struct {
	// KnownHostsPath is where trusted host keys are remembered. Empty means
	// motty/known_hosts under the user's config directory.
	KnownHostsPath string
	// SSHConfigPath is the OpenSSH config the sync with it reads and writes. Empty
	// means .ssh/config under the user's home directory.
	SSHConfigPath string
	// StatePath is the configuration store: the saved hosts and settings. Empty
	// means config.db next to the known-hosts file.
	StatePath string
	// Invalidate asks the window to repaint. Connection goroutines call it
	// when their status changes; the window then calls Wake.
	Invalidate func()
	// NewWindow creates another independent workbench window through Viva's
	// application lifecycle. Nil means the host did not enable multi-window.
	NewWindow func() error
	// OpenContentWindow is Viva's reusable native content-window factory.
	OpenContentWindow func(app.Config, *ui.Canvas) (*app.ContentWindow, error)
	// CloseWindow closes this window the way its close button does. Command+W
	// calls it on a page that has no tab to close. Nil leaves that key unbound.
	CloseWindow func()
	// Keystore keeps the data key of the saved-passwords vault. Nil uses the
	// system's own secret store, and a key file beside the vault where there is none.
	Keystore vault.Keystore
	// Dial starts a connection. Nil dials for real.
	Dial func(sshconn.Config) Connection
	// Log holds the program's recent log lines, for the diagnostics report. Nil
	// leaves the report without any.
	Log *diag.Log
	// LogDir is where the program keeps its log and crash reports. The
	// diagnostics report names it. Empty means the program keeps none.
	LogDir string
	// PreviousCrash is the crash report the previous run left, if it ended
	// abnormally. The window says so once and the diagnostics report carries it.
	PreviousCrash string
	// SaveFile chooses a local download destination. Nil uses app.SaveFile.
	SaveFile func(app.SaveFileOptions) (string, error)
	// OpenFiles chooses local files for identity/config imports and uploads. Nil
	// uses app.OpenFiles.
	OpenFiles func(app.OpenFileOptions) ([]string, error)
	// OpenDirectory chooses one local directory for recursive transfers. Nil
	// uses app.OpenDirectory.
	OpenDirectory func(app.OpenDirectoryOptions) (string, error)
	// Raise brings this window to the front, for a request from outside (a link,
	// or a second launch). Nil leaves the window where it is.
	Raise func()
	// StartAtLogin reads and changes starting the program at login. Nil hides
	// the setting.
	StartAtLogin *LoginStart
	// StatusArea reports whether the application has an icon in the status
	// area, which is what makes "stay there on close" a choice. Nil, or false,
	// hides the setting.
	StatusArea func() bool
	// CheckUpdate asks the release feed for the newest version. Nil asks GitHub.
	CheckUpdate func() (update.Release, error)
	// Updater installs a newer release over the running program. Nil, or a nil
	// Quit, offers only the download page.
	Updater Updater
	// Quit ends the application, every window of it. An update needs it to hand
	// over to the new version.
	Quit func()
	// OpenURL opens an external HTTP or HTTPS target. Nil uses app.OpenURL.
	OpenURL func(string) error
	// CLIAuthStart is a dedicated local auth terminal, never an AI-visible session.
	CLIAuthStart func(localshell.Config) (Shell, error)
	// Group lets this window's tabs move to and from the group's other windows.
	// Nil keeps tabs in their window.
	Group *Group
	// TearOff opens a new window with its top-left at origin (screen
	// coordinates) and calls adopt with its state before it first paints; it is
	// what dropping a tab outside every window does. Nil means a tab cannot be
	// torn off.
	TearOff func(origin ui.Point, adopt func(*State)) error
	// Blank starts the window empty: no tabs are restored into it.
	Blank bool
	// RememberWindowFrame saves where this window's content area is, and opens
	// the next first window there. Only the first window of a process sets it;
	// a torn-off or secondary window belongs to one session.
	RememberWindowFrame bool
	// OpenPath opens a local document with the system's default application. Nil
	// uses app.OpenPath.
	OpenPath func(string) error
	// FileIcon returns the system's icon for a local file or folder at the given
	// pixel size, as the file panel shows an application bundle with its own icon.
	// Nil uses app.FileIcon.
	FileIcon func(path string, pixels int) (ui.Bitmap, error)
	// EditPollInterval is how often a file open in the system's editor is looked
	// at for a save. Zero means a second.
	EditPollInterval time.Duration
	// PreferredLanguages is the system's language list, most preferred first. It
	// decides the language of a user who has not chosen one. Nil uses
	// app.PreferredLanguages.
	PreferredLanguages func() []string
	// SystemAppearance is the system's light or dark look. It decides the look of
	// a user who has not chosen one. Nil uses app.SystemAppearance.
	SystemAppearance func() app.Appearance
	// WatchSystemAppearance calls its argument whenever the system's look changes
	// and returns what stops that. Nil uses app.WatchSystemAppearance.
	WatchSystemAppearance func(changed func()) (stop func())
	// SetNativeAppearance pins the look of what the system draws for the window
	// (buttons, menus, dialogs, native text editing). Nil uses
	// app.SetNativeAppearance.
	SetNativeAppearance func(app.Appearance)
	// Executable is the path of the running program, which the settings page
	// puts into the client configuration of the MCP server that needs no window.
	// Nil uses os.Executable.
	Executable func() (string, error)
	// SetClipboardText replaces the system clipboard after the configured OSC
	// 52 policy permits it. Nil uses app.SetClipboardText.
	SetClipboardText func(string) error
	// ClipboardText reads the system clipboard for an explicit paste shortcut.
	// Nil uses app.ClipboardText.
	ClipboardText func() (string, error)
	// ClipboardImagePNG reads an image from the system clipboard as PNG bytes.
	// Nil uses app.ClipboardImagePNG.
	ClipboardImagePNG func() ([]byte, error)
	// HistoryPath is the command history's database. Empty means history.db
	// next to the state file; it is only opened once history is used.
	HistoryPath string
	// AIStorePath is the assistant's database. Empty means ai.db next to the
	// state file; it is only opened once the assistant is used.
	AIStorePath string
	// AISettingsChanged is called after the user changed an assistant setting.
	AISettingsChanged func()
	// AIProvider builds the client for a configured provider. Nil talks to the
	// real service; tests supply a fake.
	AIProvider func(store.Provider) llm.Provider
	// AIProviderScan detects magpie and cc-switch provider data for the AI
	// import dialog. Nil reads the standard locations. It is called only after
	// the user activates Import, never from a paint.
	AIProviderScan func() []aiproviderimport.SourceInfo
	// Notify shows a desktop notification a terminal's program asked for. Nil
	// uses app.SendNotification.
	Notify func(app.Notification) error
	// Bell sounds the terminal bell. Nil plays the system alert sound.
	Bell func() error
	// MonoFamilies lists the installed fixed-pitch font families. Nil uses
	// app.MonospaceFamilies.
	MonoFamilies func() []string
	// WallNow is the time of day, for one-time codes. Nil uses time.Now.
	WallNow func() time.Time
	// Now is a monotonic clock, for how long a connection has taken and how long
	// a silent shell is waited for. Nil is the time since the workbench opened.
	Now func() time.Duration
	// SessionLogRunID names this process run for the connection journal. Empty
	// uses one random value for the whole process, which tests may override to
	// exercise startup recovery.
	SessionLogRunID string
}

func (options Options) withDefaults() Options {
	if options.KnownHostsPath == "" {
		dir, err := os.UserConfigDir()
		if err != nil {
			dir = os.TempDir()
		}
		options.KnownHostsPath = filepath.Join(dir, "motty", "known_hosts")
	}
	if options.StatePath == "" {
		options.StatePath = filepath.Join(filepath.Dir(options.KnownHostsPath), "config.db")
	}
	if options.Dial == nil {
		options.Dial = func(config sshconn.Config) Connection { return realConn{sshconn.Dial(config)} }
	}
	if options.SaveFile == nil {
		options.SaveFile = app.SaveFile
	}
	if options.OpenFiles == nil {
		options.OpenFiles = app.OpenFiles
	}
	if options.OpenDirectory == nil {
		options.OpenDirectory = app.OpenDirectory
	}
	if options.OpenPath == nil {
		options.OpenPath = app.OpenPath
	}
	if options.FileIcon == nil {
		options.FileIcon = app.FileIcon
	}
	if options.EditPollInterval <= 0 {
		options.EditPollInterval = defaultEditPollInterval
	}
	if options.OpenURL == nil {
		options.OpenURL = app.OpenURL
	}
	if options.PreferredLanguages == nil {
		options.PreferredLanguages = app.PreferredLanguages
	}
	if options.SystemAppearance == nil {
		options.SystemAppearance = app.SystemAppearance
	}
	if options.WatchSystemAppearance == nil {
		options.WatchSystemAppearance = app.WatchSystemAppearance
	}
	if options.SetNativeAppearance == nil {
		options.SetNativeAppearance = app.SetNativeAppearance
	}
	if options.SetClipboardText == nil {
		options.SetClipboardText = app.SetClipboardText
	}
	if options.Executable == nil {
		options.Executable = executableOf
	}
	if options.ClipboardImagePNG == nil {
		options.ClipboardImagePNG = app.ClipboardImagePNG
	}
	if options.ClipboardText == nil {
		options.ClipboardText = app.ClipboardText
	}
	if options.Notify == nil {
		options.Notify = app.SendNotification
	}
	if options.Bell == nil {
		options.Bell = playBell
	}
	if options.MonoFamilies == nil {
		options.MonoFamilies = app.MonospaceFamilies
	}
	if options.WallNow == nil {
		options.WallNow = time.Now
	}
	if options.Now == nil {
		opened := time.Now() // carries a monotonic reading
		options.Now = func() time.Duration { return time.Since(opened) }
	}
	return options
}

// modalKind says which dialog, if any, owns the window.
type modalKind uint8

const (
	modalNone modalKind = iota
	modalNewHost
	modalDeleteHost
	modalMoveHostGroup
	modalHostImportResult
	modalNewForward
	modalNewProxy
	modalHostKey
	modalKeyPassphrase
	modalPassword
	modalRemoteClipboard
	modalCommandPalette
	modalShortcutCapture
	modalStateNotice
	modalVaultUnlock
	modalVaultForget
	modalHighlightRule
	modalSnippet
	modalImportSessions
	modalFontPicker
	modalOutboundProxy
	modalForm
	modalRemoteTools
	modalFileTransfer
	modalXYTransfer
	modalOpenLink
	modalRenameTab
	modalAIRename
	modalCloseTabs
	modalSessionLink
	modalAutofillRule
	modalAutofillConfirm
	modalHistory
	modalUpdate
	modalUpdateProgress
	modalDashboard
	modalDashboardAdd
	modalToolbarSettings
	modalSFTPColumns
	modalTerminalInput
	modalDeleteRule
)

// tone colors a session's headline.
type tone uint8

const (
	toneBusy tone = iota
	toneOK
	toneFail
)

// maxBannerLines bounds how much of a server banner the terminal shows.
const maxBannerLines = 8

// startConnection dials host for session and shows progress on its connection
// page. A tab that is dialed again keeps its terminal history. Dialing returns
// at once; everything after happens on the connection's goroutine.
func (state *State) startConnection(session *Session, host Host) {
	state.cancelReconnect(session)
	// A user dial is a new logical session; automatic retries and the
	// share-refusal re-dial go through startConnectionMode and keep the row.
	state.startSessionLog(session, host)
	state.startConnectionMode(session, host, false)
}

func (state *State) startConnectionMode(session *Session, host Host, automatic bool) {
	session.resetLatency()
	avoidShare := session.avoidShare || loopbackTunnel(host)
	session.avoidShare = false
	state.stopSessionForwards(session.ID, false)
	state.closeSFTP(session)
	state.stopRemoteStats(session)
	if session.term != nil && session.term.shell != nil {
		releaseShell(session.term.shell)
	}
	session.term.abandonShell()
	if session.conn != nil {
		session.conn.Cancel()
	}
	if session.term != nil {
		session.term.recording.stop()
	}
	if !automatic {
		state.cancelReconnect(session)
	}
	session.established = false
	session.disconnected = false
	session.restored = false
	session.autoRestore, session.restoreNote = false, ""
	session.host = host
	session.Address = state.sessionAddress(host)
	session.endpoint = sshconn.Endpoint{Host: host.Address, Port: host.Port, User: state.login(host)}
	session.role = sshconn.EndpointTarget
	session.jumpPath = ""
	if host.ID == localHostID {
		// A shell on this machine: nothing to dial, so it is connected at once.
		shell := host.localShell
		if shell == "" {
			shell = state.localShell
		}
		conn := newLocalConn(shell, state.networkProxy)
		conn.dir = localStartupDirectory(session.localDirectory, conn.shell)
		session.conn = conn
		state.finishStartingConnection(session, automatic)
		// There is no goroutine to report that it is connected, so say so now:
		// the shell starts here instead of on a wake nobody would send.
		if status := session.conn.Status(); status.Version != session.version {
			state.apply(session, status)
		}
		state.pump(session)
		return
	}
	if host.isDesktop() {
		// A remote desktop: dialed in the background, through SSH if the host says so.
		if session.desk != nil {
			session.desk.close()
			session.desk = nil
		}
		session.conn = state.newDesktopConn(session, host)
		state.finishStartingConnection(session, automatic)
		return
	}
	if !host.isSSH() {
		// Telnet and serial: a stream, dialed in the background.
		session.conn = state.newStreamConn(host, state.notifyFor(session))
		state.finishStartingConnection(session, automatic)
		return
	}
	config, valid := state.sshConfigFor(session, host)
	if valid && state.shareConnections == shareConnectionsOn && !avoidShare {
		if shared, _ := state.shareFrom(session, host); shared != nil {
			session.conn, session.shared = shared, true
			state.finishStartingConnection(session, automatic)
			// Nothing will report that it is connected, so say so now: the shell
			// starts here instead of on a wake nobody would send.
			if status := session.conn.Status(); status.Version != session.version {
				state.apply(session, status)
			}
			state.pump(session)
			return
		}
	}
	session.conn, session.shared = state.options.Dial(config), false
	state.finishStartingConnection(session, automatic)
}

// sshConfigFor is how to connect to an SSH host: its endpoint, identities and
// keep-alive, and the jump chain its route names, which it also records as the
// session's path. valid is false when the route is broken, and the config then
// has no host to dial.
func (state *State) sshConfigFor(session *Session, host Host) (config sshconn.Config, valid bool) {
	config, route, valid := state.hostCatalog.sshConfigFor(host, session.host, state.connectionEnv(state.notifyFor(session)))
	session.proxyRoute, session.jumpPath = route.proxyKey, route.jumpPath
	return config, valid
}

// connectionEnv is what resolving a connection needs that the catalog does not
// hold: the trusted-keys file, the vault, and how to wake this window.
func (state *State) connectionEnv(notify func()) connectEnv {
	env := connectEnv{
		KnownHostsPath: state.options.KnownHostsPath,
		Notify:         notify,
		ProxyLocked:    errors.New(state.messages.Tools.ProxyPasswordLocked),
	}
	if state.secrets.vault != nil {
		env.Passwords = state.secrets.vault
	}
	return env
}

// shareFrom opens a tab on the connection another tab of this window already
// has to the same account through the same jumps, so a split or a second tab
// needs no new handshake and no new password. It returns nil when there is no
// such connection, or when the server will not lend it.
func (state *State) shareFrom(session *Session, host Host) (Connection, *Session) {
	address := state.sessionAddress(host)
	for index := range state.sessions {
		other := &state.sessions[index]
		// The saved host may have been edited since this transport was opened.
		// Compare its dialed account/address, not the next connection's settings.
		if other.ID == session.ID || other.conn == nil || other.Address != address ||
			other.jumpPath != session.jumpPath || other.proxyRoute != session.proxyRoute {
			continue
		}
		lender, ok := other.conn.(sharer)
		if !ok {
			continue
		}
		if shared, err := lender.Share(state.notifyFor(session)); err == nil {
			return shared, other
		}
	}
	return nil, nil
}

// finishStartingConnection resets what the connection page shows for a
// connection that has just been created, and gives the session its screen.
func (state *State) finishStartingConnection(session *Session, automatic bool) {
	session.version = 0
	session.phase = sshconn.PhaseConnecting
	session.answered, session.otpSent = 0, false
	session.busy = true
	session.tone = toneBusy
	session.stage, session.since, session.finished = stageConnect, state.now(), 0
	session.showLog, session.serverVersion, session.method = false, "", ""
	session.err, session.shellFailed, session.keptAfterShell = nil, false, false
	session.hostKey = sshconn.HostKeyInfo{}
	if automatic {
		state.recordConnectionEvent(session, connectionEventReconnectStarted, "")
	} else {
		state.recordConnectionEvent(session, connectionEventConnectStarted, "")
	}
	if session.term == nil {
		geometry := state.layoutForSession(session.ID)
		session.term = newTerminalWithScrollback(geometry.cols, geometry.rows, state.terminalScrollback)
		state.configureTerminal(session)
	} else {
		// A reconnect invalidates any list built from the old prompt.
		state.closeCompletionList()
		t := session.term
		t.shell, t.ended, t.live, t.inputSent, t.told = nil, false, false, false, toldNothing
		t.resetFolderSync()
		t.rx = t.rx[:0]
		t.skippedEndpoint, t.skippedRole, t.skipsTold = sshconn.Endpoint{}, sshconn.EndpointTarget, 0
		t.preedit = app.TextState{}
		t.write([]byte("\r\n"))
	}
	state.describe(session)
	session.term.find.refreshIfChanged(session.term.screen, state.messages, 0)
}

// Wake applies connection changes to the UI state and moves shell output onto
// the screens. The window calls it on its own thread before a repaint that a
// connection goroutine asked for, so Paint only ever reads state. It does
// nothing for sessions whose status is unchanged and whose shell is quiet.
func (state *State) Wake() {
	defer state.flushLocalDirectories()
	state.adoptHistoryClear()
	state.adoptCompletion()
	state.pumpFileWindows()
	if state.syncPending {
		state.syncPending = false
		if state.syncLeader() == state {
			state.autoSync()
		}
	}
	if state.restorePending {
		state.restorePending = false
		state.connectRestoredSessions()
	}
	state.advanceReconnects()
	state.pumpHostTest()
	state.probeLatency()
	findNow := state.now()
	for index := range state.sessions {
		session := &state.sessions[index]
		if session.conn != nil {
			if status := session.conn.Status(); status.Version != session.version {
				state.apply(session, status)
			}
		}
		state.pumpSFTP(session)
		state.pumpRemoteStats(session)
		state.pumpDesktop(session)
		state.pump(session)
		state.runLoginCommand(session)
		// A session-local charset notice changes the available character grid.
		// Resize after parsing output, never reentrantly from an OSC callback.
		state.fitTerminal(session)
		state.followTerminalDirectory(session)
		if session.ID == state.activeSessionID && state.activeSection == sectionTerminal && state.monitorShown(state.deck.ActiveWorkspace()) {
			state.ensureMonitor(session)
		} else if session.stats != nil && session.stats.panel.worker != nil {
			session.stats.panel.worker.Close()
			session.stats.panel.worker = nil
		}
		if session.term != nil {
			session.term.find.refreshIfChanged(session.term.screen, state.messages, findNow)
			state.stepSelectionScroll(session.term, findNow)
		}
	}
	state.pumpForwards()
	state.pumpTransfer()
	state.runLaunches()
	if state.updateAtStart && !state.updateStarted {
		state.updateStarted = true
		state.scheduleUpdateCheck(state.update.firstDelay)
	}
	state.pumpSFTPConnections()
	state.pumpDashboard()
	state.pumpSessionLogs()
	state.showNextPrompt()
	state.drainAI()
	state.syncSFTPWindows()
	state.aiPumpCLILogin()
	state.syncRemoteToolsWindows()
}

// apply reads one status into a session: its dot, its busy flag, and the next
// lines of its story on the screen.
func (state *State) apply(session *Session, status sshconn.Status) {
	if session.autoRestore {
		if isPromptPhase(status.Phase) && !state.canAutofill(session, status) {
			// Restore only connects what needs no one; a question for the user
			// returns the tab to its page instead of opening a dialog.
			state.abandonRestoredConnect(session)
			return
		}
		if status.Phase == sshconn.PhaseConnected || status.Phase == sshconn.PhaseFailed || status.Phase == sshconn.PhaseClosed {
			session.autoRestore = false
		}
	}
	previousPhase := session.phase
	session.version = status.Version
	session.phase = status.Phase
	session.attempt = status.Attempt
	session.busy = false
	session.hostKey = status.HostKey
	session.questions = status.Questions
	session.identity = status.IdentityPath
	if status.Endpoint.Host != "" {
		session.endpoint = status.Endpoint
		session.role = status.EndpointRole
	}
	if status.ServerVersion != "" {
		session.serverVersion = status.ServerVersion
	}
	if status.Method != "" {
		session.method = status.Method
	}
	if status.System != sshconn.SystemUnknown && session.host.System != status.System {
		state.setSessionSystem(session, status.System)
	}
	switch {
	case status.EndpointRole == sshconn.EndpointProxyJump && status.Phase != sshconn.PhaseConnected:
		session.stage = stageConnect
	case status.Phase == sshconn.PhaseConnecting:
		session.stage = stageConnect
	case status.Phase == sshconn.PhaseHostKeyPrompt:
		session.stage = stageHostKey
	case status.Phase == sshconn.PhaseAuthenticating || status.Phase == sshconn.PhaseKeyPassphrasePrompt || status.Phase == sshconn.PhasePasswordPrompt:
		session.stage = stageLogin
	case status.Phase == sshconn.PhaseConnected:
		session.stage = stageShell
	case status.Phase == sshconn.PhaseFailed || status.Phase == sshconn.PhaseClosed:
		if session.finished == 0 {
			session.finished = state.now()
		}
	}

	if status.Err != nil && !session.disconnected {
		session.err = status.Err
		if errors.Is(status.Err, sshconn.ErrHostKeyChanged) && status.EndpointRole != sshconn.EndpointProxyJump {
			session.stage = stageHostKey // refused while the key was being checked
		}
	}
	state.recordConnectionStatus(session, previousPhase, status)
	state.updateReconnect(session, status)
	// The retry policy has run: the journal can now tell a transient close that
	// will be retried from a final one.
	state.journalConnectionOutcome(session, status)

	switch status.Phase {
	case sshconn.PhaseConnecting, sshconn.PhaseHostKeyPrompt:
		session.busy, session.tone = true, toneBusy
	case sshconn.PhaseAuthenticating, sshconn.PhaseKeyPassphrasePrompt, sshconn.PhasePasswordPrompt:
		session.busy, session.tone = true, toneBusy
	case sshconn.PhaseConnected:
		session.tone = toneOK
		session.filledSecret = ""
		if previousPhase != sshconn.PhaseConnected {
			state.tell(session, toldBanner, status)
			state.sayStreamOpened(session)
			state.startPendingForwards(session)
		}
		state.status = state.messages.StatusConnectedFmt + session.Title
		if weak := status.Negotiated.Weak(); len(weak) > 0 && previousPhase != sshconn.PhaseConnected {
			// Allowed on purpose for this host, but not something to leave unsaid.
			state.status = fmt.Sprintf(state.messages.Tools.AlgoWeakFmt, strings.Join(weak, ", "))
		}
		// After the connected line: a saved or failed save is what the user needs to read.
		state.rememberSecret(session)
		if session.ID == state.activeSessionID {
			state.ensureContextSFTP()
		}
	case sshconn.PhaseClosed:
		state.endDesktop(session)
		session.pendingSecretName, session.pendingSecretValue = "", ""
		state.stopSessionForwards(session.ID, true)
		state.closeSFTP(session)
		state.closeDisconnectedShell(session)
		if status.Err != nil && !session.disconnected {
			session.tone = toneFail
			state.status = state.messages.StatusConnectFailed + session.Title
		} else {
			session.tone = toneBusy
		}
		state.tell(session, toldEnded, status)
	case sshconn.PhaseFailed:
		state.endDesktop(session)
		session.pendingSecretName, session.pendingSecretValue = "", ""
		state.stopSessionForwards(session.ID, true)
		state.closeSFTP(session)
		session.tone = toneFail
		state.tell(session, toldEnded, status)
		state.status = state.messages.StatusConnectFailed + session.Title
	}
	if session.term.skippedEndpoint != status.Endpoint || session.term.skippedRole != status.EndpointRole {
		session.term.skippedEndpoint = status.Endpoint
		session.term.skippedRole = status.EndpointRole
		session.term.skipsTold = 0
	}
	if session.term.skipsTold > len(status.Skipped) {
		session.term.skipsTold = 0
	}
	if session.term.skipsTold < len(status.Skipped) {
		for _, skipped := range status.Skipped[session.term.skipsTold:] {
			session.term.say(sgrWarn, fmt.Sprintf(state.messages.ConnSkippedKeyFmt, skipped))
		}
		session.term.skipsTold = len(status.Skipped)
	}
	if status.Phase == sshconn.PhaseConnected {
		if session.host.isDesktop() {
			state.openDesktop(session)
		} else {
			state.openShell(session)
		}
	}
	state.describe(session)

	// A prompt that can no longer be answered must not stay on screen.
	if state.modalSession == session.ID && (state.modal == modalHostKey || state.modal == modalKeyPassphrase || state.modal == modalPassword) &&
		status.Phase != sshconn.PhaseHostKeyPrompt && status.Phase != sshconn.PhaseKeyPassphrasePrompt && status.Phase != sshconn.PhasePasswordPrompt {
		state.closeModal()
	}
}

func (state *State) closeDisconnectedShell(session *Session) {
	terminal := session.term
	if terminal == nil || terminal.shell == nil {
		return
	}
	terminal.rx = terminal.shell.Drain(terminal.rx[:0])
	if len(terminal.rx) > 0 {
		terminal.recording.append(state.now(), terminalRecordingOutput, terminal.rx)
		terminal.write(terminal.rx)
		state.goLive(session)
	}
	releaseShell(terminal.shell)
	terminal.shell = nil
	terminal.recording.stop()
	terminal.ended = true
	terminal.preedit = app.TextState{}
}

func (state *State) setSessionSystem(session *Session, system sshconn.RemoteSystem) {
	session.host.System = system
	if session.HostID != 0 {
		for index := range state.hosts {
			if state.hosts[index].ID == session.HostID {
				state.hosts[index].System = system
				break
			}
		}
		for index := range state.sessions {
			if state.sessions[index].HostID == session.HostID {
				state.sessions[index].host.System = system
			}
		}
	}
	state.save()
}

// tell writes local terminal output that belongs beside remote output, unless
// the screen has already shown it. Connection progress stays on its page.
func (state *State) tell(session *Session, stage announcement, status sshconn.Status) {
	t := session.term
	if t.told >= stage {
		return
	}
	t.told = stage
	m := state.messages
	switch stage {
	case toldBanner:
		lines := bannerLines(status.Banner)
		for _, line := range lines {
			t.say("", line)
		}
		if len(lines) > 0 {
			t.write([]byte("\r\n"))
		}
	case toldEnded:
		if status.Phase == sshconn.PhaseClosed && status.Err == nil {
			t.say(sgrMuted, m.ConnClosed)
			return
		}
		state.tellFailure(session, status)
	}
}

func formatEndpoint(endpoint sshconn.Endpoint) string {
	return fmt.Sprintf("%s@%s:%d", endpoint.User, endpoint.Host, endpoint.Port)
}

// tellFailure writes why a connection failed, in red for the headline.
func (state *State) tellFailure(session *Session, status sshconn.Status) {
	t, m := session.term, state.messages
	switch {
	case errors.Is(status.Err, sshconn.ErrHostKeyChanged):
		t.say(sgrFail, m.ConnChangedTitle)
		t.say("", "")
		t.say("", fmt.Sprintf(m.ConnChangedBodyFmt, status.HostKey.Address))
		t.say("", "")
		for _, known := range status.HostKey.Known {
			t.say("", fmt.Sprintf(m.ConnChangedTrustedFmt, known))
		}
		t.say("", fmt.Sprintf(m.ConnChangedPresented, status.HostKey.Fingerprint))
		t.say("", "")
		t.say(sgrMuted, fmt.Sprintf(m.ConnChangedHintFmt, status.HostKey.File))
	case errors.Is(status.Err, sshconn.ErrHostKeyRejected):
		t.say(sgrFail, m.ConnRejected)
	case errors.Is(status.Err, sshconn.ErrCanceled):
		t.say(sgrFail, m.ConnCanceled)
	case errors.Is(status.Err, sshconn.ErrAuthFailed):
		t.say(sgrFail, m.ConnAuthFailed)
	default:
		t.say(sgrFail, fmt.Sprintf(m.ConnFailedFmt, status.Err))
	}
	if session.reconnectPending {
		t.say(sgrMuted, fmt.Sprintf(m.ConnReconnectWaitingFmt, session.reconnectAttempt, clockText(int64(reconnectDelay(session.reconnectAttempt)/time.Second))))
	} else {
		t.say(sgrMuted, m.SessionReconnectHint)
	}
}

// bannerLines is the server's banner, trimmed and bounded.
func bannerLines(banner string) []string {
	banner = strings.TrimSpace(banner)
	if banner == "" {
		return nil
	}
	lines := []string{""}
	for count, line := range strings.Split(banner, "\n") {
		if count == maxBannerLines {
			break
		}
		lines = append(lines, strings.TrimRight(line, "\r\t "))
	}
	return lines
}

// promptKey identifies one question a connection asked, so an answered one is
// not shown again and a repeated password prompt is.
func promptKey(phase sshconn.Phase, attempt int, identity string, endpoint sshconn.Endpoint, role sshconn.EndpointRole) uint64 {
	var kind uint64
	switch phase {
	case sshconn.PhaseHostKeyPrompt:
		kind = 1
	case sshconn.PhasePasswordPrompt:
		kind = 2
	case sshconn.PhaseKeyPassphrasePrompt:
		kind = 3
	default:
		return 0
	}
	hash := uint64(1469598103934665603)
	mix := func(value string) {
		for index := 0; index < len(value); index++ {
			hash ^= uint64(value[index])
			hash *= 1099511628211
		}
	}
	mix(identity)
	mix(endpoint.Host)
	mix(endpoint.User)
	hash ^= uint64(endpoint.Port) | uint64(role)<<16 | uint64(attempt&0xff)<<24 | kind<<32
	hash *= 1099511628211
	return hash
}

// showNextPrompt opens the dialog for the first unanswered question, unless
// another dialog is already up.
func (state *State) showNextPrompt() {
	if state.modal != modalNone {
		return
	}
	// A vault still kept by the master password of an earlier version cannot
	// answer anything until that password has been typed once.
	if vaultFile := state.secrets.vault; vaultFile != nil && vaultFile.NeedsConversion() && !state.secrets.declined {
		state.openVaultUnlock(nil, nil)
		return
	}
	for session := range state.connectionSessions() {
		key := promptKey(session.phase, session.attempt, session.identity, session.endpoint, session.role)
		if session.conn == nil || key == 0 || key == session.answered {
			continue
		}
		if session.phase == sshconn.PhasePasswordPrompt {
			// The questions are part of the prompt, not of its key: lay out a
			// field per question before autofill can answer a single one.
			state.preparePassword(session)
		}
		if state.autofill(session) == autofillSent {
			continue
		}
		state.modalSession = session.ID
		switch session.phase {
		case sshconn.PhaseHostKeyPrompt:
			state.modal = modalHostKey
			state.hostKeyDescription = fmt.Sprintf(state.messages.HostKeyDescriptionFmt, session.hostKey.Address)
			if session.hostKey.Type == rdpCertificateType {
				state.hostKeyDescription = state.rdpCertificateDescription(session.hostKey)
			}
		case sshconn.PhasePasswordPrompt:
			state.modal = modalPassword
		case sshconn.PhaseKeyPassphrasePrompt:
			state.modal = modalKeyPassphrase
			state.keyPassphrase = text.Editor{}
			state.keyPassphraseError = ""
			state.offerToSave()
			if session.attempt > 1 {
				state.keyPassphraseError = state.messages.KeyPassphraseRetry
				if state.forgetRefusedSecret(session, passphraseSecretName(session.identity)) {
					state.keyPassphraseError = state.messages.PasswordSavedRefused
				}
			}
			state.keyPassphraseDescription = fmt.Sprintf(state.messages.KeyPassphraseDescriptionFmt, filepath.Base(session.identity))
		}
		state.raiseSFTPAuthentication(session)
		return
	}
	if state.clipboardRequest.pending {
		state.modal = modalRemoteClipboard
		state.modalSession = state.clipboardRequest.sessionID
		return
	}
	if state.transfer.phase == transferOffered {
		state.modal = modalFileTransfer
		state.modalSession = state.transfer.sessionID
		return
	}
	if state.update.offer {
		state.update.offer = false
		state.update.notes.scroll = 0
		state.modal = modalUpdate
	}
}

func (state *State) closeModal() {
	if state.modal == modalTerminalInput {
		state.inputConfirmation = terminalInputConfirmation{}
	}
	if state.modal == modalDashboard {
		state.dashboard.editor.order.Cancel()
	}
	if state.modal == modalDashboardAdd {
		state.dashboard.addHosts = nil
	}
	if state.modal == modalToolbarSettings {
		state.toolbarEditor.list.Cancel()
	}
	if state.modal == modalSFTPColumns {
		state.sftpColumnEditor.list.Cancel()
	}
	if state.modal == modalRemoteClipboard {
		state.clipboardRequest = remoteClipboardRequest{}
	}
	if state.modal == modalAutofillConfirm {
		state.autofillRules.ask = autofillAsk{}
	}
	if state.modal == modalDeleteRule {
		state.ruleDelete = ruleDelete{}
	}
	state.modal = modalNone
	state.modalSession = 0
	state.reveal.reset()
	state.passwordFields = nil
	state.passwordRects = nil
	state.passwordBoxes = nil
	state.passwordQuestions = nil
	state.passwordError = ""
	state.keyPassphrase = text.Editor{}
	state.keyPassphraseError = ""
	state.secrets.unlock = text.Editor{}
	state.secrets.dialogError = ""
	state.form = formDialog{}
	state.closeAsked = false
}

// promptSession returns the session the open prompt belongs to.
func (state *State) promptSession() *Session {
	for _, source := range state.dashboard.sources {
		if source.session.ID == state.modalSession {
			return &source.session
		}
	}
	for index := range state.sessions {
		if state.sessions[index].ID == state.modalSession {
			return &state.sessions[index]
		}
	}
	if w := state.sftpWindows[state.modalSession]; w != nil && w.ownsConnection && !w.closed {
		return &w.session
	}
	return nil
}

// answered records that the open prompt was handled and moves on.
func (state *State) answered(session *Session) {
	session.answered = promptKey(session.phase, session.attempt, session.identity, session.endpoint, session.role)
	state.closeModal()
	state.showNextPrompt()
}

// clickHostKey handles the host-key dialog. Dismissing it rejects the key:
// silence must never mean trust.
func (state *State) clickHostKey(id ui.ID) {
	session := state.promptSession()
	if session == nil || session.conn == nil {
		state.closeModal()
		return
	}
	switch id {
	case hitHostKeyTrust:
		session.conn.TrustHostKey(true)
	case hitHostKeyOnce:
		session.conn.TrustHostKey(false)
	case hitHostKeyReject, hitHostKeyClose, hitHostKeyDismiss:
		session.conn.RejectHostKey()
	default:
		return
	}
	state.answered(session)
}

// clickPassword handles the password dialog. Dismissing it cancels the
// connection.
func (state *State) clickPassword(id ui.ID) {
	switch id {
	case hitPasswordSave:
		state.secrets.save = !state.secrets.save
	case hitPasswordSubmit:
		state.submitPassword()
	case hitPasswordCancel, hitPasswordClose, hitPasswordDismiss:
		session := state.promptSession()
		if session != nil && session.conn != nil {
			session.conn.Cancel()
			state.answered(session)
		} else {
			state.closeModal()
		}
	}
}

func (state *State) clickKeyPassphrase(id ui.ID) {
	switch id {
	case hitKeyPassphraseSave:
		state.secrets.save = !state.secrets.save
	case hitKeyPassphraseSubmit:
		state.submitKeyPassphrase()
	case hitKeyPassphraseDismiss:
		// A click beside the dialog or Escape must not drop the key: the login
		// then fails with nothing to show for it. Skip and the close button do.
	case hitKeyPassphraseSkip, hitKeyPassphraseClose:
		session := state.promptSession()
		if session != nil && session.conn != nil {
			session.conn.SkipIdentity()
			state.answered(session)
		} else {
			state.closeModal()
		}
	}
}

func (state *State) submitKeyPassphrase() {
	session := state.promptSession()
	if session == nil || session.conn == nil {
		state.closeModal()
		return
	}
	passphrase := state.keyPassphrase.String()
	session.conn.SubmitKeyPassphrase(passphrase)
	state.stageSecret(session, passphraseSecretName(session.identity), passphrase)
	state.answered(session)
}
