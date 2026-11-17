package workbench

import (
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"motty/internal/sshconn"
)

func TestParseAgentOutput(t *testing.T) {
	output := []byte("SSH_AUTH_SOCK=/tmp/ssh-x/agent.42; export SSH_AUTH_SOCK;\n" +
		"SSH_AGENT_PID=42; export SSH_AGENT_PID;\necho Agent pid 42;\n")
	socket, pid := parseAgentOutput(output)
	if socket != "/tmp/ssh-x/agent.42" || pid != 42 {
		t.Fatalf("socket=%q pid=%d", socket, pid)
	}
}

func TestSecurityKeyAgentLoadsTheKeyAndReleases(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("the fake agent is a shell script")
	}
	dir := t.TempDir()
	agentPath := filepath.Join(dir, "ssh-agent")
	addPath := filepath.Join(dir, "ssh-add")
	addLog := filepath.Join(dir, "add.log")
	addCopy := filepath.Join(dir, "add-copy")
	socketPath := filepath.Join(dir, "agent.sock")
	agentScript := "#!/bin/sh\n" +
		"echo \"SSH_AUTH_SOCK=$MOTTY_TEST_SOCKET; export SSH_AUTH_SOCK;\"\n" +
		"echo \"SSH_AGENT_PID=$$; export SSH_AGENT_PID;\"\n"
	addScript := "#!/bin/sh\n" +
		"test -n \"$SSH_ASKPASS\" || exit 1\n" +
		"test -n \"$MOTTY_FIDO_ASKPASS_SOCKET\" || exit 1\n" +
		"test -n \"$SSH_AUTH_SOCK\" || exit 1\n" +
		"printf '%s\\n' \"$@\" > \"$MOTTY_TEST_ADD_LOG\"\n" +
		"cp \"$1\" \"$MOTTY_TEST_ADD_COPY\"\n"
	for path, script := range map[string]string{agentPath: agentScript, addPath: addScript} {
		if err := os.WriteFile(path, []byte(script), 0o700); err != nil {
			t.Fatal(err)
		}
	}
	t.Setenv("MOTTY_SSH_AGENT", agentPath)
	t.Setenv("MOTTY_SSH_ADD", addPath)
	t.Setenv("MOTTY_TEST_SOCKET", socketPath)
	t.Setenv("MOTTY_TEST_ADD_LOG", addLog)
	t.Setenv("MOTTY_TEST_ADD_COPY", addCopy)
	keyPath := filepath.Join(dir, "id_sk")
	if err := os.WriteFile(keyPath, []byte("key material"), 0o600); err != nil {
		t.Fatal(err)
	}

	manager := newSecurityKeyAgent(nil)
	socket, release, err := manager.Agent(sshconn.SecurityKeyRequest{Path: keyPath})
	if err != nil {
		t.Fatalf("Agent: %v", err)
	}
	if socket != socketPath {
		t.Fatalf("socket = %q, want %q", socket, socketPath)
	}
	logged, err := os.ReadFile(addLog)
	if err != nil {
		t.Fatalf("ssh-add was not run: %v", err)
	}
	if !strings.Contains(filepath.Base(strings.TrimSpace(string(logged))), "key-") {
		t.Fatalf("ssh-add arguments = %q, want the temporary key file", logged)
	}
	copied, err := os.ReadFile(addCopy)
	if err != nil {
		t.Fatalf("ssh-add did not receive a readable key file: %v", err)
	}
	if string(copied) != "key material" {
		t.Fatalf("ssh-add key contents = %q", copied)
	}
	release()
	manager.mu.Lock()
	closed := manager.closed
	manager.mu.Unlock()
	if !closed {
		t.Fatal("the agent stayed open after its last reference was released")
	}
}

func TestAskSecurityKeyReturnsTheTypedAnswer(t *testing.T) {
	state := newTestState(t, Options{})
	posted := make(chan func(), 1)
	state.SetPost(func(fn func()) { posted <- fn })

	type result struct {
		answer string
		err    error
	}
	done := make(chan result, 1)
	go func() {
		answer, err := state.askSecurityKey("Enter PIN for id_sk")
		done <- result{answer: answer, err: err}
	}()

	paint := <-posted
	paint()
	if state.modal != modalForm {
		t.Fatalf("modal = %v, want the askpass form", state.modal)
	}
	state.form.set(0, "123456")
	state.submitForm()
	got := <-done
	if got.err != nil || got.answer != "123456" {
		t.Fatalf("answer=%q err=%v", got.answer, got.err)
	}
}

func TestAskSecurityKeyCancelFailsThePrompt(t *testing.T) {
	state := newTestState(t, Options{})
	posted := make(chan func(), 1)
	state.SetPost(func(fn func()) { posted <- fn })

	done := make(chan error, 1)
	go func() {
		_, err := state.askSecurityKey("Enter PIN")
		done <- err
	}()
	paint := <-posted
	paint()
	state.clickForm(hitFormCancel)
	if err := <-done; err == nil {
		t.Fatal("cancelling the prompt did not fail the askpass request")
	}
}

func TestSecurityKeyIdentityIsMarkedInTheKeyList(t *testing.T) {
	record := identityRecord{Name: "id_sk", Algorithm: "sk-ssh-ed25519@openssh.com", SecurityKey: true}
	record.refreshCells(keyMessages{
		Unavailable: "Unavailable",
		Unencrypted: "None",
		Passphrase:  "Passphrase",
		SecurityKey: "Security key",
	})
	if got, want := record.cells[2], "Security key"; got != want {
		t.Fatalf("protection = %q, want %q", got, want)
	}
}
