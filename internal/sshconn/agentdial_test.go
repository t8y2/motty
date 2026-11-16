package sshconn

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"
)

func TestAgentPresetAddresses(t *testing.T) {
	home := filepath.FromSlash("/home/u")
	none := func(string) bool { return false }
	for _, test := range []struct {
		name   string
		preset AgentPreset
		goos   string
		exists func(string) bool
		want   string
	}{
		{"system", AgentSystem, "linux", none, ""},
		{"1password linux", AgentOnePassword, "linux", none, filepath.Join(home, ".1password", "agent.sock")},
		{"1password mac", AgentOnePassword, "darwin", none, filepath.Join(home, "Library", "Group Containers", "2BUA8C4S2C.com.1password", "t", "agent.sock")},
		{"1password windows", AgentOnePassword, "windows", none, `\\.\pipe\openssh-ssh-agent`},
		{"bitwarden windows", AgentBitwarden, "windows", none, `\\.\pipe\openssh-ssh-agent`},
		{"bitwarden download", AgentBitwarden, "darwin", none, filepath.Join(home, ".bitwarden-ssh-agent.sock")},
		{"bitwarden store", AgentBitwarden, "darwin",
			func(path string) bool { return strings.Contains(path, "Containers") },
			filepath.Join(home, "Library", "Containers", "com.bitwarden.desktop", "Data", ".bitwarden-ssh-agent.sock")},
		{"bitwarden flatpak", AgentBitwarden, "linux",
			func(path string) bool { return strings.Contains(path, ".var") },
			filepath.Join(home, ".var", "app", "com.bitwarden.desktop", "data", ".bitwarden-ssh-agent.sock")},
		{"bitwarden snap", AgentBitwarden, "linux",
			func(path string) bool { return strings.Contains(path, "snap") },
			filepath.Join(home, "snap", "bitwarden", "current", ".bitwarden-ssh-agent.sock")},
	} {
		t.Run(test.name, func(t *testing.T) {
			if got := test.preset.Address(test.goos, home, test.exists); got != test.want {
				t.Fatalf("Address = %q, want %q", got, test.want)
			}
		})
	}
}

func TestConfiguredAgentBeatsTheEnvironment(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "/run/system-agent")
	if got := agentAddress("/run/1password"); got != "/run/1password" {
		t.Fatalf("agentAddress = %q, want the configured one", got)
	}
	if got := agentAddress(""); got != "/run/system-agent" {
		t.Fatalf("agentAddress = %q, want SSH_AUTH_SOCK", got)
	}
}

func TestAgentAuthenticatesThroughTheConfiguredAddress(t *testing.T) {
	path, public := startAgent(t, "manager-key")
	t.Setenv("SSH_AUTH_SOCK", "") // the system has no agent; only the setting leads to this one
	server := startServer(t, func(s *testServer) { s.authorized = []ssh.PublicKey{public}; s.password = "unused" })
	h := dial(t, server, func(c *Config) { c.Agent = true; c.AgentAddress = path })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	if connected := h.phase(PhaseConnected); connected.Method != "publickey" || len(connected.Skipped) != 0 {
		t.Fatalf("connected = %+v", connected)
	}
}

func TestUnreachableConfiguredAgentIsReported(t *testing.T) {
	missing := filepath.Join(t.TempDir(), "agent.sock")
	other := newSigner(t)
	server := startServer(t, func(s *testServer) { s.authorized = []ssh.PublicKey{other.PublicKey()}; s.password = "pw" })
	h := dial(t, server, func(c *Config) { c.Agent = true; c.AgentAddress = missing })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	status := h.phase(PhasePasswordPrompt)
	if len(status.Skipped) != 1 || !strings.HasPrefix(status.Skipped[0], "ssh-agent (") || !strings.Contains(status.Skipped[0], missing) {
		t.Fatalf("skipped = %q, want the agent and the address it could not reach", status.Skipped)
	}
}

func TestMissingDefaultAgentIsNotReported(t *testing.T) {
	t.Setenv("SSH_AUTH_SOCK", "")
	if _, err := os.Stat(openSSHAgentPipe); err == nil {
		t.Skip("a Windows agent pipe exists on this machine")
	}
	other := newSigner(t)
	server := startServer(t, func(s *testServer) { s.authorized = []ssh.PublicKey{other.PublicKey()}; s.password = "pw" })
	h := dial(t, server, func(c *Config) { c.Agent = true })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	if status := h.phase(PhasePasswordPrompt); len(status.Skipped) != 0 {
		t.Fatalf("a machine without an agent reported %q", status.Skipped)
	}
}
