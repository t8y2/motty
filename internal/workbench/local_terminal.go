package workbench

import (
	"context"
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"sync"

	"motty/internal/localshell"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
	"motty/internal/systemproxy"
)

// errLocalOnly is what the remote-only capabilities say to a local terminal.
var errLocalOnly = errors.New("workbench: not available in a local terminal")

// localHostID marks a session that runs a shell on this machine. It is not a
// saved host: no catalog entry has it.
const localHostID = ^uint32(0)

// localConn is the Connection of a local terminal: connected from the start, no
// credentials to ask for, and a shell on this machine instead of a remote one.
// Everything that needs a server (files, forwarding, commands) refuses.
type localConn struct {
	mu                sync.Mutex
	status            sshconn.Status
	dir               string
	shell             string
	networkProxy      networkProxyMode
	lookupSystemProxy func() systemproxy.Settings
}

func newLocalConn(shell string, networkProxy networkProxyMode) *localConn {
	return &localConn{
		status: sshconn.Status{Version: 1, Phase: sshconn.PhaseConnected, Method: "local"}, shell: shell,
		networkProxy: networkProxy, lookupSystemProxy: systemproxy.Lookup,
	}
}

func (conn *localConn) Status() sshconn.Status {
	conn.mu.Lock()
	defer conn.mu.Unlock()
	return conn.status
}

func (conn *localConn) TrustHostKey(bool)          {}
func (conn *localConn) RejectHostKey()             {}
func (conn *localConn) SubmitPassword(string)      {}
func (conn *localConn) SubmitAnswers([]string)     {}
func (conn *localConn) SubmitKeyPassphrase(string) {}
func (conn *localConn) SkipIdentity()              {}
func (conn *localConn) Cancel()                    {}

func (conn *localConn) OpenShell(config sshconn.ShellConfig) (Shell, error) {
	return localshell.Start(conn.shellConfig(config, runtime.GOOS))
}

// shellConfig is how the shell starts on goos, with the folder and prompt
// reporting the shell supports there.
func (conn *localConn) shellConfig(config sshconn.ShellConfig, goos string) localshell.Config {
	shell := localshell.Config{Shell: conn.shell, Args: localshell.ArgsFor(conn.shell), Cols: config.Cols, Rows: config.Rows, Dir: conn.dir, Notify: config.Notify}
	proxy := systemproxy.Settings{}
	if conn.networkProxy == networkProxySystem && conn.lookupSystemProxy != nil {
		proxy = conn.lookupSystemProxy()
	}
	shell.Env = systemproxy.Environment(proxy)
	if config.IntegrateShell && goos == "windows" {
		shell.DirectoryToken = config.IntegrationToken
		if _, wsl := localshell.WSLDistro(conn.shell); wsl {
			// A distribution's shell reports through a script it runs itself.
			shell.WSLScript = sshconn.WSLIntegrationScript(config.IntegrationToken, localshell.WSLDirectoryVariable)
		}
	}
	if config.IntegrateShell && goos != "windows" {
		program := conn.shell
		if program == "" {
			program = os.Getenv("SHELL")
		}
		// Bash and Zsh report their folder when started through this shim, so
		// the files panel can open and follow where the terminal is.
		if args := sshconn.IntegrationArgs(program, config.IntegrationToken); args != nil {
			shell.Shell, shell.Args = "/bin/sh", args
		}
	}
	return shell
}

// A remembered path is process startup data, never text sent to the shell.
// Deleted/unmounted folders fall back to the normal home-directory startup.
func localStartupDirectory(directory, shell string) string {
	if !validLocalDirectory(directory) {
		return ""
	}
	if _, wsl := localshell.WSLDistro(shell); wsl {
		// Looking at a distribution's files from Windows starts the distribution
		// if it is stopped, which can take seconds; its own start script already
		// stays where it is when the folder is gone.
		return filepath.Clean(directory)
	}
	if localshell.IsWSLUNC(directory) {
		return "" // a distribution's folder is no place for a Windows shell to start
	}
	if info, err := os.Stat(directory); err == nil && info.IsDir() {
		return filepath.Clean(directory)
	}
	return ""
}

// validLocalShell reports whether a saved shell names something Start can run:
// a WSL distribution, or an absolute program path. What it names may be gone by
// now; that is reported when the tab starts, not here.
func validLocalShell(shell string) bool {
	if _, wsl := localshell.WSLDistro(shell); wsl {
		return true
	}
	return filepath.IsAbs(shell) && len(shell) <= 2048 && strings.IndexFunc(shell, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

func validLocalDirectory(directory string) bool {
	return filepath.IsAbs(directory) && len(directory) <= 2048 && strings.IndexFunc(directory, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

func localDirectoryHost(host string) bool {
	if host == "" || strings.EqualFold(host, "localhost") {
		return true
	}
	name, err := os.Hostname()
	if err != nil {
		return false
	}
	return strings.EqualFold(host, name) || strings.EqualFold(shortHostName(host), shortHostName(name))
}

// shortHostName drops a domain suffix, so a shell that reports its FQDN still
// names the same machine as a short hostname.
func shortHostName(name string) string {
	if index := strings.IndexByte(name, '.'); index > 0 {
		return name[:index]
	}
	return name
}

// Coalesce reports from one output drain. Terminal callbacks themselves only
// update memory, without reentering persistence while the parser is running.
func (state *State) flushLocalDirectories() {
	if state.localDirectoryDirty {
		state.localDirectoryDirty = false
		state.save()
	}
}

func (conn *localConn) OpenSFTP() (sftpclient.Remote, error) { return nil, errLocalOnly }

func (conn *localConn) DialContext(context.Context, string, string) (net.Conn, error) {
	return nil, errLocalOnly
}

func (conn *localConn) Listen(string, string) (net.Listener, error) { return nil, errLocalOnly }

func (conn *localConn) RunCommand(context.Context, string, int) ([]byte, error) {
	return nil, errLocalOnly
}

func (conn *localConn) StreamCommand(context.Context, string, int, func([]byte) bool) error {
	return errLocalOnly
}

// openLocalTerminal opens a shell on this computer, in the empty pane the user
// is looking at or else in a new tab, like any other host.
func (state *State) openLocalTerminal() *Session {
	return state.openLocalTerminalWith(localshell.Choice{})
}

// openLocalTerminalWith opens a local terminal that runs shell, whatever the
// default shell is. A zero Choice starts the default one, which stays the
// default for this tab on a reconnect, so changing the setting later moves it.
func (state *State) openLocalTerminalWith(shell localshell.Choice) *Session {
	label := state.messages.LocalTerminalTitle
	if shell.Path != "" {
		label = shell.Name
	}
	host := localHost(label)
	host.localShell = shell.Path
	return state.createHostSession(host, host.Address)
}

// localSystem is the operating system this program runs on, so the tab wears
// the same badge a remote machine of that kind would.
func localSystem() sshconn.RemoteSystem {
	switch runtime.GOOS {
	case "darwin":
		return sshconn.SystemDarwin
	case "windows":
		return sshconn.SystemWindows
	case "linux":
		return sshconn.SystemLinux
	}
	return sshconn.SystemUnknown
}

// sessionAddress is what a session shows as its address: user, host and port for
// an SSH host, the computer's name for a shell on this one.
func (state *State) sessionAddress(host Host) string {
	switch {
	case host.ID == localHostID:
		return host.Address
	case host.Protocol == protocolSerial:
		return host.Address
	case host.Protocol == protocolTelnet || host.isDesktop():
		return fmt.Sprintf("%s:%d", host.Address, host.Port)
	}
	return fmt.Sprintf("%s@%s:%d", state.login(host), host.Address, host.Port)
}

// isLocal reports whether a session runs on this machine.
func (session *Session) isLocal() bool { return session.HostID == localHostID }

// localHost is the Host a local session carries, so the code that shows or saves
// a session's host has something to read.
func localHost(label string) Host {
	name, _ := os.Hostname()
	host := Host{ID: localHostID, Label: label, Address: name, System: localSystem()}
	host.finish()
	return host
}
