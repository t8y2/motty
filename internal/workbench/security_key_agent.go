package workbench

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"os"
	"os/exec"
	"path/filepath"
	"runtime"
	"strings"
	"sync"
	"time"

	"motty/internal/fidoaskpass"
	"motty/internal/sshconn"
)

// A hardware security key (OpenSSH sk-*) signs on the device, so the identity
// is only usable through an ssh-agent that can reach it. The system agent
// often starts at login without the askpass helper its ssh-sk-helper needs, and
// on Windows there may be no agent at all. This file starts a short-lived agent
// for one connection, loads the key into it, and points SSH_ASKPASS at this
// program so PIN and passphrase prompts reach the window.

const (
	securityKeyAgentStartTimeout = 10 * time.Second
	securityKeyAddTimeout        = 3 * time.Minute
	securityKeyPromptTimeout     = 3 * time.Minute
	securityKeyPromptTokenBytes  = 24
)

// securityKeyAgent is one connection's security-key agent. It starts on the
// first key and is stopped by the connection's release callback.
type securityKeyAgent struct {
	state *State

	mu      sync.Mutex
	started bool
	closed  bool
	dir     string
	socket  string
	pid     int
	agent   string
	sshAdd  string
	prompts *securityKeyPrompts
	keys    map[string]bool
	refs    int
	addMu   sync.Mutex
}

func newSecurityKeyAgent(state *State) *securityKeyAgent {
	return &securityKeyAgent{state: state, keys: make(map[string]bool)}
}

// Agent is the sshconn callback: it returns an agent holding the key, or why
// the key could not be prepared. The release is called when the connection
// closes.
func (agent *securityKeyAgent) Agent(request sshconn.SecurityKeyRequest) (string, func(), error) {
	agent.mu.Lock()
	if agent.closed {
		agent.mu.Unlock()
		return "", nil, errors.New("the security-key agent has been shut down")
	}
	if err := agent.startLocked(); err != nil {
		agent.mu.Unlock()
		return "", nil, err
	}
	// The reference goes up before the key is loaded, so a release from a
	// connection that is already closing cannot stop the agent under ssh-add.
	agent.refs++
	socket := agent.socket
	agent.mu.Unlock()

	agent.addMu.Lock()
	err := agent.addKey(request)
	agent.addMu.Unlock()
	if err != nil {
		agent.release()
		return "", nil, err
	}
	var once sync.Once
	return socket, func() { once.Do(agent.release) }, nil
}

// release drops one connection's reference and stops the agent when none is
// left.
func (agent *securityKeyAgent) release() {
	agent.mu.Lock()
	if agent.refs > 0 {
		agent.refs--
	}
	if agent.refs == 0 {
		agent.stopLocked()
	}
	agent.mu.Unlock()
}

// startLocked starts the agent and its askpass channel. A failure leaves
// nothing running.
func (agent *securityKeyAgent) startLocked() error {
	if agent.started {
		return nil
	}
	agentPath, err := findSSHAgent()
	if err != nil {
		return err
	}
	sshAddPath, err := findSSHAdd(agentPath)
	if err != nil {
		return err
	}
	dir, err := os.MkdirTemp("", "motty-sk-")
	if err != nil {
		return fmt.Errorf("create a security-key directory: %w", err)
	}
	if err := os.Chmod(dir, 0o700); err != nil {
		os.RemoveAll(dir)
		return fmt.Errorf("secure the security-key directory: %w", err)
	}
	prompts, err := newSecurityKeyPrompts(agent.state, dir)
	if err != nil {
		os.RemoveAll(dir)
		return err
	}
	socket := filepath.Join(dir, "agent.sock")

	output, err := runSSHAgent(agentPath, socket, prompts.env())
	if err != nil {
		// Not every agent accepts -a. Our own socket is still the shortest way
		// to keep this agent separate from the user's login agent.
		output, err = runSSHAgent(agentPath, "", prompts.env())
		if err != nil {
			prompts.close()
			os.RemoveAll(dir)
			return fmt.Errorf("start ssh-agent: %w", err)
		}
		socket = ""
	}
	agentSocket, pid := parseAgentOutput(output)
	if agentSocket != "" {
		socket = agentSocket
	}
	if socket == "" || pid == 0 {
		prompts.close()
		os.RemoveAll(dir)
		return errors.New("ssh-agent started without reporting its socket and process id")
	}
	agent.started = true
	agent.dir, agent.socket, agent.pid = dir, socket, pid
	agent.agent, agent.sshAdd, agent.prompts = agentPath, sshAddPath, prompts
	registerSecurityKeyAgent(agent)
	return nil
}

// addKeyLocked loads one key into the agent. The key file is written to a
// private temporary file only for a key the vault holds; file identities are
// read where they are.
func (agent *securityKeyAgent) addKey(request sshconn.SecurityKeyRequest) error {
	if agent.keys[request.Path] {
		return nil
	}
	data, name, err := agent.keyMaterial(request)
	if err != nil {
		return err
	}
	defer clear(data)
	file, err := os.CreateTemp(agent.dir, "key-*")
	if err != nil {
		return fmt.Errorf("create a temporary key file: %w", err)
	}
	defer os.Remove(file.Name())
	if err := file.Chmod(0o600); err != nil {
		file.Close()
		return fmt.Errorf("secure the temporary key file: %w", err)
	}
	if _, err := file.Write(data); err != nil {
		file.Close()
		return fmt.Errorf("write the temporary key file: %w", err)
	}
	if err := file.Close(); err != nil {
		return fmt.Errorf("write the temporary key file: %w", err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), securityKeyAddTimeout)
	defer cancel()
	command := exec.CommandContext(ctx, agent.sshAdd, file.Name())
	command.Env = append(os.Environ(), agent.prompts.env()...)
	command.Env = append(command.Env,
		"SSH_AUTH_SOCK="+agent.socket,
		fmt.Sprintf("SSH_AGENT_PID=%d", agent.pid),
	)
	var output bytes.Buffer
	command.Stdout = &output
	command.Stderr = &output
	if err := command.Run(); err != nil {
		detail := strings.TrimSpace(output.String())
		if len(detail) > 400 {
			detail = detail[:400]
		}
		if detail == "" {
			detail = err.Error()
		}
		return fmt.Errorf("%s: %s", name, detail)
	}
	agent.keys[request.Path] = true
	return nil
}

// keyMaterial reads the private key and its display name. A stored key is
// written to the temporary file that the caller removes; the vault value is
// cleared here.
func (agent *securityKeyAgent) keyMaterial(request sshconn.SecurityKeyRequest) ([]byte, string, error) {
	if !sshconn.IsStoredKey(request.Path) {
		data, err := os.ReadFile(request.Path)
		if err != nil {
			return nil, "", fmt.Errorf("read %s: %w", filepath.Base(request.Path), err)
		}
		return data, filepath.Base(request.Path), nil
	}
	if agent.state.secrets.vault == nil {
		return nil, "", errors.New("the saved key needs an unlocked vault")
	}
	value, found, err := agent.state.secrets.vault.Get(request.Path)
	if err != nil {
		return nil, "", err
	}
	if !found {
		return nil, "", errors.New("the saved key is no longer in the vault")
	}
	key, err := unmarshalStoredKey(value)
	if err != nil {
		return nil, "", err
	}
	return []byte(key.Key), key.Name, nil
}

// stopLocked ends the agent and removes its directory. It is safe to call more
// than once.
func (agent *securityKeyAgent) stopLocked() {
	if agent.closed {
		return
	}
	agent.closed = true
	if agent.prompts != nil {
		agent.prompts.close()
	}
	if agent.pid > 0 {
		killAgent(agent.agent, agent.socket, agent.pid)
	}
	if agent.dir != "" {
		os.RemoveAll(agent.dir)
	}
	unregisterSecurityKeyAgent(agent)
}

// findSSHAgent prefers a user-mode agent, which can be started with the
// askpass environment a verify-required key needs.
func findSSHAgent() (string, error) {
	if path := strings.TrimSpace(os.Getenv("MOTTY_SSH_AGENT")); path != "" {
		return path, nil
	}
	var candidates []string
	switch runtime.GOOS {
	case "darwin":
		candidates = append(candidates,
			"/opt/homebrew/bin/ssh-agent", "/usr/local/bin/ssh-agent", "/usr/bin/ssh-agent")
	case "windows":
		for _, root := range []string{
			os.Getenv("ProgramFiles"),
			os.Getenv("ProgramFiles(x86)"),
			filepath.Join(os.Getenv("LOCALAPPDATA"), "Programs"),
		} {
			if root != "" {
				candidates = append(candidates, filepath.Join(root, "Git", "usr", "bin", "ssh-agent.exe"))
			}
		}
		if scoop := os.Getenv("USERPROFILE"); scoop != "" {
			candidates = append(candidates, filepath.Join(scoop, "scoop", "apps", "git", "current", "usr", "bin", "ssh-agent.exe"))
		}
	}
	if path, err := exec.LookPath("ssh-agent"); err == nil {
		candidates = append(candidates, path)
	}
	for _, candidate := range candidates {
		if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
			return candidate, nil
		}
	}
	if runtime.GOOS == "windows" {
		return "", errors.New("ssh-agent is not installed; install OpenSSH Client or Git for Windows")
	}
	return "", errors.New("ssh-agent is not installed; install OpenSSH (macOS: brew install openssh libfido2)")
}

// findSSHAdd finds the ssh-add that belongs to the agent, so a Git agent on
// Windows is not paired with the system one.
func findSSHAdd(agentPath string) (string, error) {
	if path := strings.TrimSpace(os.Getenv("MOTTY_SSH_ADD")); path != "" {
		return path, nil
	}
	base := "ssh-add"
	if runtime.GOOS == "windows" {
		base = "ssh-add.exe"
	}
	candidate := filepath.Join(filepath.Dir(agentPath), base)
	if info, err := os.Stat(candidate); err == nil && !info.IsDir() {
		return candidate, nil
	}
	if path, err := exec.LookPath(base); err == nil {
		return path, nil
	}
	return "", errors.New("ssh-add is not installed; install OpenSSH (macOS: brew install openssh libfido2)")
}

// runSSHAgent starts ssh-agent and returns what it printed. An empty socket
// lets the agent choose its own, for agents that reject -a.
func runSSHAgent(agentPath, socket string, env []string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), securityKeyAgentStartTimeout)
	defer cancel()
	args := []string{"-s"}
	if socket != "" {
		args = append(args, "-a", socket)
	}
	command := exec.CommandContext(ctx, agentPath, args...)
	command.Env = append(os.Environ(), env...)
	output, err := command.Output()
	if err != nil {
		var exit *exec.ExitError
		if errors.As(err, &exit) && len(exit.Stderr) > 0 {
			return nil, fmt.Errorf("%s: %s", err, strings.TrimSpace(string(exit.Stderr)))
		}
		return nil, err
	}
	return output, nil
}

// parseAgentOutput reads the SSH_AUTH_SOCK and SSH_AGENT_PID lines ssh-agent
// prints for a POSIX shell.
func parseAgentOutput(output []byte) (socket string, pid int) {
	for _, line := range strings.Split(string(output), "\n") {
		line = strings.TrimSpace(line)
		if value, ok := strings.CutPrefix(line, "SSH_AUTH_SOCK="); ok {
			value = strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
			socket = value
		}
		if value, ok := strings.CutPrefix(line, "SSH_AGENT_PID="); ok {
			value = strings.TrimSpace(strings.SplitN(value, ";", 2)[0])
			fmt.Sscanf(value, "%d", &pid)
		}
	}
	return socket, pid
}

// killAgent stops only the agent this process started, never the user's login
// agent: the environment carries our socket and pid, not the ambient ones.
func killAgent(agentPath, socket string, pid int) {
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()
	command := exec.CommandContext(ctx, agentPath, "-k")
	command.Env = []string{
		"PATH=" + os.Getenv("PATH"),
		"SSH_AUTH_SOCK=" + socket,
		fmt.Sprintf("SSH_AGENT_PID=%d", pid),
	}
	if err := command.Run(); err != nil {
		if process, err := os.FindProcess(pid); err == nil {
			_ = process.Kill()
		}
	}
}

// securityKeyPrompts is the private channel between an ssh-add or
// ssh-sk-helper child and this window.
type securityKeyPrompts struct {
	state    *State
	listener fidoaskpass.ListenCloser
	address  string
	token    string
	exe      string
	once     sync.Once
}

func newSecurityKeyPrompts(state *State, dir string) (*securityKeyPrompts, error) {
	listener, address, err := fidoaskpass.Listen(dir)
	if err != nil {
		return nil, fmt.Errorf("open the askpass channel: %w", err)
	}
	token, err := randomToken()
	if err != nil {
		listener.Close()
		return nil, err
	}
	exe, err := os.Executable()
	if err != nil {
		listener.Close()
		return nil, fmt.Errorf("find the askpass helper: %w", err)
	}
	prompts := &securityKeyPrompts{state: state, listener: listener, address: address, token: token, exe: exe}
	go prompts.serve()
	return prompts, nil
}

func (prompts *securityKeyPrompts) env() []string {
	return []string{
		"SSH_ASKPASS=" + prompts.exe,
		"SSH_ASKPASS_REQUIRE=force",
		"MOTTY_FIDO_ASKPASS=1",
		"MOTTY_FIDO_ASKPASS_SOCKET=" + prompts.address,
		"MOTTY_FIDO_ASKPASS_TOKEN=" + prompts.token,
	}
}

func (prompts *securityKeyPrompts) serve() {
	for {
		connection, err := prompts.listener.Accept()
		if err != nil {
			return
		}
		go prompts.handle(connection)
	}
}

func (prompts *securityKeyPrompts) handle(connection fidoaskpass.Conn) {
	defer connection.Close()
	connection.SetDeadline(time.Now().Add(securityKeyPromptTimeout))
	var request fidoaskpass.Request
	if err := json.NewDecoder(connection).Decode(&request); err != nil {
		return
	}
	if request.Token != prompts.token {
		_ = json.NewEncoder(connection).Encode(fidoaskpass.Response{})
		return
	}
	answer, err := prompts.state.askSecurityKey(request.Prompt)
	_ = json.NewEncoder(connection).Encode(fidoaskpass.Response{OK: err == nil, Response: answer})
}

func (prompts *securityKeyPrompts) close() {
	prompts.once.Do(func() { prompts.listener.Close() })
}

func randomToken() (string, error) {
	var token [securityKeyPromptTokenBytes]byte
	if _, err := rand.Read(token[:]); err != nil {
		return "", fmt.Errorf("create an askpass token: %w", err)
	}
	return hex.EncodeToString(token[:]), nil
}

// askSecurityKey asks the window for the PIN or passphrase. The askpass child
// blocks on the answer, so the form and this wait must not hold the window
// thread.
func (state *State) askSecurityKey(prompt string) (string, error) {
	answer := make(chan string, 1)
	if !state.aiPost(func(state *State) {
		if state.modal != modalNone {
			select {
			case answer <- "":
			default:
			}
			return
		}
		messages := state.messages
		description := strings.TrimSpace(prompt)
		if description == "" {
			description = messages.SecurityKeyPromptTitle
		}
		state.openForm(formDialog{
			title:       messages.SecurityKeyPromptTitle,
			description: description,
			submit:      messages.SecurityKeyPromptSubmit,
			run: func(state *State, form *formDialog) string {
				value := form.value(0)
				form.set(0, "")
				if value == "" {
					return messages.SecurityKeyPromptRequired
				}
				select {
				case answer <- value:
				default:
				}
				return ""
			},
			cancel: func(*State) {
				select {
				case answer <- "":
				default:
				}
			},
		}, formField{label: messages.SecurityKeyPromptLabel, secure: true})
	}) {
		return "", errors.New("the window cannot ask for the security key")
	}
	select {
	case value := <-answer:
		if value == "" {
			return "", errors.New(state.messages.SecurityKeyCancelled)
		}
		return value, nil
	case <-time.After(securityKeyPromptTimeout):
		return "", errors.New("timed out waiting for the security key")
	}
}

// The registry lets the application stop every agent on quit. A connection
// normally releases its own agent; this covers a quit that outruns it.
var securityKeyAgents struct {
	sync.Mutex
	agents map[*securityKeyAgent]struct{}
}

func registerSecurityKeyAgent(agent *securityKeyAgent) {
	securityKeyAgents.Lock()
	if securityKeyAgents.agents == nil {
		securityKeyAgents.agents = make(map[*securityKeyAgent]struct{})
	}
	securityKeyAgents.agents[agent] = struct{}{}
	securityKeyAgents.Unlock()
}

func unregisterSecurityKeyAgent(agent *securityKeyAgent) {
	securityKeyAgents.Lock()
	delete(securityKeyAgents.agents, agent)
	securityKeyAgents.Unlock()
}

// ShutdownSecurityKeyAgents stops every security-key agent this process
// started. The application calls it when it quits.
func ShutdownSecurityKeyAgents() {
	securityKeyAgents.Lock()
	agents := make([]*securityKeyAgent, 0, len(securityKeyAgents.agents))
	for agent := range securityKeyAgents.agents {
		agents = append(agents, agent)
	}
	securityKeyAgents.Unlock()
	for _, agent := range agents {
		agent.mu.Lock()
		agent.stopLocked()
		agent.mu.Unlock()
	}
}
