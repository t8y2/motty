package sshconn

import (
	"errors"
	"fmt"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"time"
)

// openSSHAgentPipe is where Windows password managers (1Password, Bitwarden) and
// the OpenSSH Authentication Agent service listen. Windows OpenSSH does not read
// SSH_AUTH_SOCK, so this is the agent a Windows user has without configuring.
const openSSHAgentPipe = `\\.\pipe\openssh-ssh-agent`

const agentDialTimeout = 3 * time.Second

// AgentPreset names a password manager's ssh-agent, whose address depends on the
// operating system and on how the manager was installed.
type AgentPreset uint8

const (
	// AgentSystem is the agent the system provides: SSH_AUTH_SOCK, or on Windows
	// the OpenSSH pipe.
	AgentSystem AgentPreset = iota
	// AgentOnePassword is the 1Password SSH agent.
	AgentOnePassword
	// AgentBitwarden is the Bitwarden SSH agent.
	AgentBitwarden
)

// Address is where the preset's agent listens on goos for the user at home. A
// manager with several install flavours (Bitwarden's store, Snap and Flatpak
// builds) returns the first whose socket exists, and the usual one when none
// does, so that a manager that is not running fails with a path the user can
// recognise. AgentSystem has no fixed address and returns "".
func (preset AgentPreset) Address(goos, home string, exists func(string) bool) string {
	if goos == "windows" {
		if preset == AgentSystem {
			return ""
		}
		return openSSHAgentPipe
	}
	var candidates []string
	switch preset {
	case AgentOnePassword:
		if goos == "darwin" {
			candidates = []string{filepath.Join(home, "Library", "Group Containers", "2BUA8C4S2C.com.1password", "t", "agent.sock")}
		} else {
			candidates = []string{filepath.Join(home, ".1password", "agent.sock")}
		}
	case AgentBitwarden:
		candidates = []string{filepath.Join(home, ".bitwarden-ssh-agent.sock")}
		if goos == "darwin" {
			candidates = append(candidates, filepath.Join(home, "Library", "Containers", "com.bitwarden.desktop", "Data", ".bitwarden-ssh-agent.sock"))
		} else {
			candidates = append(candidates,
				filepath.Join(home, "snap", "bitwarden", "current", ".bitwarden-ssh-agent.sock"),
				filepath.Join(home, ".var", "app", "com.bitwarden.desktop", "data", ".bitwarden-ssh-agent.sock"))
		}
	default:
		return ""
	}
	for _, candidate := range candidates {
		if exists(candidate) {
			return candidate
		}
	}
	return candidates[0]
}

// agentAddress is the ssh-agent a connection offers: the address the user
// chose, else SSH_AUTH_SOCK, else on Windows the OpenSSH pipe. The empty string
// means there is none.
func agentAddress(configured string) string {
	if configured != "" {
		return configured
	}
	if socket := os.Getenv("SSH_AUTH_SOCK"); socket != "" {
		return socket
	}
	if runtime.GOOS == "windows" {
		return openSSHAgentPipe
	}
	return ""
}

func isAgentPipe(address string) bool {
	return strings.HasPrefix(address, `\\.\pipe\`) || strings.HasPrefix(address, `//./pipe/`)
}

// dialAgent connects to the agent at address: a named pipe on Windows, a Unix
// socket everywhere else (and on Windows for a path that is not a pipe).
func dialAgent(address string) (net.Conn, error) {
	if isAgentPipe(address) {
		return dialAgentPipe(strings.ReplaceAll(address, "/", `\`))
	}
	conn, err := net.DialTimeout("unix", address, agentDialTimeout)
	if err != nil {
		return nil, err
	}
	return conn, nil
}

var errNoAgent = errors.New("no ssh-agent is running (SSH_AUTH_SOCK is not set)")

func agentUnreachable(address string, err error) error {
	return fmt.Errorf("connect to the ssh-agent at %s: %w", address, err)
}
