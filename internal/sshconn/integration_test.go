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
