//go:build windows

package workbench

import (
	"crypto/ed25519"
	"crypto/rand"
	"encoding/pem"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	"golang.org/x/crypto/ssh"

	"motty/internal/sshconn"
)

func TestFindSSHAgentPrefersGitFromPathBeforeWindowsService(t *testing.T) {
	gitPath, err := exec.LookPath("git")
	if err != nil {
		t.Skip("git is not installed on PATH")
	}
	var expected string
	for _, candidate := range gitAgentCandidates(gitPath) {
		if info, statErr := os.Stat(candidate); statErr == nil && !info.IsDir() {
			expected = candidate
			break
		}
	}
	if expected == "" {
		t.Skip("Git for Windows ssh-agent.exe not alongside git")
	}
	// Point standard ProgramFiles env vars at an empty folder so discovery
	// must rely on git on PATH rather than falling back to System32\OpenSSH.
	empty := t.TempDir()
	t.Setenv("MOTTY_SSH_AGENT", "")
	t.Setenv("ProgramFiles", empty)
	t.Setenv("ProgramFiles(x86)", empty)
	t.Setenv("LOCALAPPDATA", empty)
	t.Setenv("USERPROFILE", empty)
	t.Setenv("SCOOP", empty)
	t.Setenv("ProgramData", empty)

	got, err := findSSHAgent()
	if err != nil {
		t.Fatalf("findSSHAgent: %v", err)
	}
	if !strings.EqualFold(filepath.Clean(got), filepath.Clean(expected)) {
		t.Fatalf("findSSHAgent() = %q, want %q", got, expected)
	}
}

func TestWindowsSecurityKeyAgentStartsLoadsAndReleases(t *testing.T) {
	t.Setenv("MOTTY_SSH_AGENT", "")
	t.Setenv("MOTTY_SSH_ADD", "")
	agentExe, err := findSSHAgent()
	if err != nil {
		t.Skipf("no ssh-agent: %v", err)
	}
	if !strings.Contains(strings.ToLower(agentExe), "git") {
		t.Skipf("agent %q is not Git for Windows MSYS2 ssh-agent", agentExe)
	}

	// Ensure Git\usr\bin is absent from PATH to mirror a normal Windows install.
	agentDir := filepath.Dir(agentExe)
	var filtered []string
	for _, entry := range filepath.SplitList(os.Getenv("PATH")) {
		if !strings.EqualFold(filepath.Clean(entry), filepath.Clean(agentDir)) {
			filtered = append(filtered, entry)
		}
	}
	t.Setenv("PATH", strings.Join(filtered, string(os.PathListSeparator)))

	_, private, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	block, err := ssh.MarshalPrivateKey(private, "win-sk-agent-test")
	if err != nil {
		t.Fatal(err)
	}
	keyPath := filepath.Join(t.TempDir(), "id_test")
	if err := os.WriteFile(keyPath, pem.EncodeToMemory(block), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := newSecurityKeyAgent(nil)
	socket, release, err := manager.Agent(sshconn.SecurityKeyRequest{Path: keyPath})
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	data, err := os.ReadFile(socket)
	if err != nil {
		release()
		t.Fatalf("read agent socket %q: %v", socket, err)
	}
	if !strings.HasPrefix(string(data), "!<socket >") {
		release()
		t.Fatalf("socket %q contents = %q, want Cygwin socket descriptor", socket, data)
	}
	agentDirTemp := filepath.Dir(socket)
	release()

	manager.mu.Lock()
	closed := manager.closed
	manager.mu.Unlock()
	if !closed {
		t.Fatal("the agent stayed open after its last reference was released")
	}
	if _, err := os.Stat(agentDirTemp); !os.IsNotExist(err) {
		t.Fatalf("temp agent dir %q still exists after release (err=%v)", agentDirTemp, err)
	}
}
