package workbench

import (
	"errors"
	"runtime"
	"strings"
	"unicode"

	"motty/internal/i18n"
	"motty/internal/localshell"
	"motty/internal/sshconn"
)

func validWSLEndpoint(host Host) bool {
	return !host.isWSL() || validEndpoint(protocolWSL, host.Address) && validWSLUser(host.User)
}

func validWSLUser(user string) bool {
	return len(user) <= 256 && !strings.HasPrefix(user, "-") &&
		strings.IndexFunc(user, func(r rune) bool {
			return unicode.IsControl(r) || unicode.IsSpace(r) || strings.ContainsRune(`"\/`, r)
		}) < 0
}

func validWSLDirectory(directory string) bool {
	return directory == "" || strings.HasPrefix(directory, "/") && len(directory) <= 2048 &&
		strings.IndexFunc(directory, unicode.IsControl) < 0
}

func (host Host) wslTarget(defaultDistro string) string {
	distro := host.Address
	if distro == "" {
		distro = defaultDistro
	}
	if host.User != "" {
		return host.User + "@" + distro
	}
	return distro
}

func wslHostSystem(distro string) sshconn.RemoteSystem {
	if system := wslDistroSystem(distro); system != sshconn.SystemUnknown {
		return system
	}
	return sshconn.SystemLinux
}

func newWSLHostConn(host Host, directory string, resolveDefault func() string, messages i18n.WSLMessages) *localConn {
	conn := newLocalConn("", networkProxyDirect)
	if !validWSLEndpoint(host) || !validWSLDirectory(host.WSLDirectory) {
		conn.startErr = errors.New(messages.ErrInvalidHost)
		return conn
	}
	distro := host.Address
	if distro == "" {
		distro = resolveDefault()
	}
	if !validEndpoint(protocolWSL, distro) || distro == "" {
		conn.startErr = errors.New(messages.ErrNoDefaultDistribution)
		return conn
	}
	conn.shell = localshell.WSLShell(distro)
	conn.dir = localStartupDirectory(directory, conn.shell)
	conn.wslUser, conn.wslDirectory, conn.wslNoGUI = host.User, host.WSLDirectory, host.WSLNoGUI
	return conn
}

func (state *State) localConnection(session *Session, host Host) *localConn {
	if !host.isWSL() {
		conn := newLocalConn(state.resolveLocalShell(host), state.networkProxy)
		if _, wsl := localshell.WSLDistro(conn.shell); wsl {
			session.host.System = sshconn.SystemLinux
		}
		conn.dir = localStartupDirectory(session.localDirectory, conn.shell)
		return conn
	}
	if runtime.GOOS != "windows" {
		conn := newLocalConn("", networkProxyDirect)
		conn.startErr = errors.New(state.messages.WSL.ErrNeedsWindows)
		return conn
	}
	conn := newWSLHostConn(host, session.localDirectory, localshell.DefaultWSLDistro, state.messages.WSL)
	if distro, ok := localshell.WSLDistro(conn.shell); ok {
		if host.Address == "" {
			state.localShellMenu.defaultWSLDistro = distro
		}
		if old, local := session.conn.(*localConn); local && (old.shell != conn.shell || old.wslUser != conn.wslUser || old.wslDirectory != conn.wslDirectory) {
			conn.dir, session.localDirectory = "", ""
		}
		if _, valid := localshell.WSLPath(session.localDirectory, distro); !valid {
			session.localDirectory = ""
		}
		session.host.System = wslHostSystem(distro)
		session.Address = host.wslTarget(distro)
	}
	return conn
}
