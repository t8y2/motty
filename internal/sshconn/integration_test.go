package sshconn

import (
	"context"
	"errors"
	"io"
	"strings"
	"sync"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"

	"motty/internal/shellkind"
)

func TestIntegrationProbeCancellationWhileExecAcknowledgmentIsWithheld(t *testing.T) {
	started, release := make(chan struct{}), make(chan struct{})
	server := startServer(t, func(server *testServer) {
		server.password, server.shell = "secret", echoShell
		server.beforeExecReply = func(command string) {
			if command == shellIntegrationProbe {
				close(started)
				<-release
			}
		}
		server.command = func(_ string, channel ssh.Channel) {
			defer channel.Close()
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		}
	})
	defer close(release)
	h := connect(t, server)
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	result := make(chan error, 1)
	go func() {
		_, err := h.conn.RunCommand(ctx, shellIntegrationProbe, 4096)
		result <- err
	}()
	select {
	case <-started:
	case <-time.After(5 * time.Second):
		t.Fatal("probe never reached exec request")
	}
	cancel()
	select {
	case err := <-result:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("canceled establishment returned %v", err)
		}
	case <-time.After(time.Second):
		t.Fatal("cancellation is waiting for the withheld exec acknowledgment")
	}
	w := openShell(t, h, 80, 24)
	w.until("normal shell fallback", func(output string) bool { return strings.Contains(output, "welcome") })
}

// A shell is started with a shell request, not as a command wrapped in a
// startup script: OpenSSH prints the connection's login message only to a
// session that is not a command, and only on the first session (ADR 0025).
func TestOpenShellSendsTheShellRequestFirst(t *testing.T) {
	for _, mode := range []string{"shell", "custom-command"} {
		t.Run(mode, func(t *testing.T) {
			var mu sync.Mutex
			var requests []string
			server := startServer(t, func(server *testServer) {
				server.password, server.shell = "secret", echoShell
				server.version = "SSH-2.0-OpenSSH_9.6"
				server.command = func(command string, channel ssh.Channel) {
					defer channel.Close()
					mu.Lock()
					requests = append(requests, command)
					mu.Unlock()
					io.WriteString(channel, "custom-startup")
					_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				}
			})
			h := connect(t, server)
			config := ShellConfig{Cols: 80, Rows: 24}
			want := "welcome"
			if mode == "custom-command" {
				config.Command, want = "custom shell", "custom-startup"
			}
			w := openShellWith(t, h, config)
			w.until(want, func(output string) bool { return strings.Contains(output, want) })
			mu.Lock()
			defer mu.Unlock()
			if mode == "shell" && len(requests) != 0 {
				t.Fatalf("a shell request was preceded by commands %q", requests)
			}
			if mode == "custom-command" && (len(requests) != 1 || requests[0] != "custom shell") {
				t.Fatalf("commands = %q, want the custom command once", requests)
			}
		})
	}
}

// Tailscale SSH names itself "Tailscale" rather than after an OpenSSH build,
// yet it is a full server that answers the shell request and a later command
// channel. Folder reporting is installed into that shell by the caller
// (ADR 0025); this checks the transport gives it a shell and answers ProbeShell.
func TestTailscaleSSHStartsAPlainShell(t *testing.T) {
	server := startServer(t, func(server *testServer) {
		server.password, server.shell = "secret", echoShell
		server.version = "SSH-2.0-Tailscale"
		server.command = func(command string, channel ssh.Channel) {
			defer channel.Close()
			io.WriteString(channel, "\x1emotty-shell:/bin/zsh\x1f\nmotty-shell:\n")
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		}
	})
	h := connect(t, server)
	w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24})
	w.until("the login shell", func(output string) bool { return strings.Contains(output, "welcome") })
	path, _ := h.conn.ProbeShell(context.Background())
	if path != "/bin/zsh" {
		t.Fatalf("ProbeShell path = %q, want /bin/zsh", path)
	}
}

func TestWSLIntegrationScriptRefusesWhatCouldBeInjected(t *testing.T) {
	for _, test := range []struct{ token, variable string }{
		{"", "MOTTY_CD"}, {"bad token", "MOTTY_CD"}, {"tok';touch /tmp/no", "MOTTY_CD"}, {strings.Repeat("a", 129), "MOTTY_CD"},
		{"token", ""}, {"token", "lower"}, {"token", "A;touch /tmp/no"}, {"token", "A B"}, {"token", "1A"},
	} {
		if script := WSLIntegrationScript(test.token, test.variable); script != "" {
			t.Errorf("token %q and variable %q were accepted", test.token, test.variable)
		}
	}
	script := WSLIntegrationScript("token-1", "MOTTY_CD")
	if script == "" || strings.Contains(script, "__MOTTY_SUFFIX__") || !strings.Contains(script, ";motty=token-1") {
		t.Fatalf("script = %q", script)
	}
}

// The command-start marker the terminal matches must be the one the integration
// scripts print. The scripts spell ESC and BEL as printf escapes.
func TestIntegrationCommandStartMarkerMatchesScripts(t *testing.T) {
	const token = "marker-token"
	marker := string(IntegrationCommandStartMarker(token))
	printed := strings.NewReplacer("\x1b", `\033`, "\x07", `\007`).Replace(marker)
	if printed == marker {
		t.Fatal("the marker has no byte the scripts would spell as an escape")
	}
	for _, shell := range []string{"/bin/bash", "/bin/zsh"} {
		if script := integrationScriptWith(shell, false, token); !strings.Contains(script, printed) {
			t.Errorf("%s integration does not print %q", shell, printed)
		}
	}
	if script := WSLIntegrationScript(token, "MOTTY_CD"); !strings.Contains(script, printed) {
		t.Errorf("WSL integration does not print %q", printed)
	}
}

// The shell family decides how the assistant may type a command. It is asked
// after the interactive shell is open, over a command channel that therefore
// cannot consume the connection's login message (ADR 0025).
func TestProbeShellAnswersFromTheLoginShell(t *testing.T) {
	answers := map[string]struct {
		output string
		path   string
		want   shellkind.Kind
	}{
		"posix":      {"\x1emotty-shell:/bin/bash\x1f\nmotty-shell:\n", "/bin/bash", shellkind.POSIX},
		"powershell": {"motty-shell:System.Management.Automation.PSVersionHashtable\r\n", "", shellkind.PowerShell},
		"cmd":        {"\"motty-shell:$PSVersionTable$FISH_VERSION\"\r\n", "", shellkind.Cmd},
		"nushell":    {"\x1emotty-shell:$SHELL\x1f\nmotty-shell:$PSVersionTable$FISH_VERSION\n", "$SHELL", shellkind.Nushell},
		"fish":       {"\x1emotty-shell:/bin/bash\x1f\nmotty-shell:3.7.1\n", "/bin/bash", shellkind.Fish},
		"garbage":    {"unexpected", "", shellkind.Unknown},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			server := startServer(t, func(server *testServer) {
				server.password, server.shell = "secret", echoShell
				server.version = "SSH-2.0-OpenSSH_9.6"
				server.command = func(command string, channel ssh.Channel) {
					defer channel.Close()
					if command != shellProbe {
						t.Errorf("unexpected command %q", command)
					}
					io.WriteString(channel, answer.output)
					_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				}
			})
			h := connect(t, server)
			path, kind := h.conn.ProbeShell(context.Background())
			if path != answer.path || kind != answer.want {
				t.Fatalf("ProbeShell = %q, %v; want %q, %v", path, kind, answer.path, answer.want)
			}
		})
	}
}

// The activation line begins with the tag the terminal matches when the line's
// echo only arrives after the hidden window closed. The tag in the line and
// IntegrationEchoTag are the same bytes: a shell echoes the line as text.
func TestIntegrationActivationStartsWithTheEchoTag(t *testing.T) {
	const token = "echo-tag-token"
	for _, name := range []string{"bash", "zsh"} {
		line := integrationActivation(name, token, "/tmp/motty-shell.x/init", "/tmp/motty-shell.x")
		if want := " " + string(IntegrationEchoTag(token)) + "; "; !strings.HasPrefix(line, want) {
			t.Fatalf("%s activation = %q, want it to start with %q", name, line, want)
		}
	}
	zshLine := integrationActivation("zsh", token, "/tmp/motty-shell.x/init", "/tmp/motty-shell.x")
	if !strings.Contains(zshLine, "[[ -n ${ZSH_VERSION-} ]] && printf") {
		t.Fatalf("zsh activation must guard the ready marker on ZSH_VERSION, got %q", zshLine)
	}
}

// sshd gives a connection's login message to the first session that starts a
// program and drops it for a command, so a connection made for a terminal opens
// nothing else before the shell: not a probe, a file transfer or a monitor
// (ADR 0025). A session asked for early waits, and stops waiting when its
// caller gives up.
func TestACommandSessionWaitsForTheTerminalsShell(t *testing.T) {
	var mu sync.Mutex
	var commands []string
	server := startServer(t, func(server *testServer) {
		server.password, server.shell = "secret", echoShell
		// A Windows banner keeps the system probe away from this test.
		server.version = "SSH-2.0-OpenSSH_for_Windows_9.5"
		server.command = func(command string, channel ssh.Channel) {
			defer channel.Close()
			mu.Lock()
			commands = append(commands, command)
			mu.Unlock()
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		}
	})
	h := dial(t, server, func(config *Config) { config.PrimaryShell = true })
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	h.phase(PhaseConnected)

	early, cancel := context.WithTimeout(context.Background(), 100*time.Millisecond)
	defer cancel()
	if _, err := h.conn.RunCommand(early, "early", 64); !errors.Is(err, context.DeadlineExceeded) {
		t.Fatalf("a command before the shell: err = %v, want it to wait until its context ended", err)
	}

	done := make(chan error, 1)
	go func() {
		_, err := h.conn.RunCommand(context.Background(), "after", 64)
		done <- err
	}()
	select {
	case err := <-done:
		t.Fatalf("a command ran before the shell opened (err = %v)", err)
	case <-time.After(200 * time.Millisecond):
	}
	mu.Lock()
	if len(commands) != 0 {
		t.Fatalf("the server received %q before the shell request", commands)
	}
	mu.Unlock()

	openShell(t, h, 80, 24)
	select {
	case err := <-done:
		if err != nil {
			t.Fatalf("the command waiting for the shell failed: %v", err)
		}
	case <-time.After(10 * time.Second):
		t.Fatal("the command was not released when the shell opened")
	}
	mu.Lock()
	defer mu.Unlock()
	if len(commands) != 1 || commands[0] != "after" {
		t.Fatalf("commands = %q, want only the one asked for after the shell", commands)
	}
}
