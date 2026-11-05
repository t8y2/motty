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

func TestIntegrationSSHStartupAndFallback(t *testing.T) {
	for _, mode := range []string{"integrated", "probe-refused", "unsupported", "exec-refused", "custom-command"} {
		t.Run(mode, func(t *testing.T) {
			server := startServer(t, func(server *testServer) {
				server.password, server.shell = "secret", echoShell
				server.version = "SSH-2.0-OpenSSH_9.6" // probes run only on known implementations
				server.command = func(command string, channel ssh.Channel) {
					defer channel.Close()
					text := "integrated-startup"
					if command == shellIntegrationProbe {
						text = "\x1emotty-shell:/bin/bash\x1f"
						if mode == "unsupported" {
							text = "\x1emotty-shell:/bin/fish\x1f"
						}
					} else if mode == "custom-command" {
						if command != "custom shell" {
							t.Errorf("custom command replaced by %q", command)
						}
						text = "custom-startup"
					}
					io.WriteString(channel, text)
					channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				}
				server.refuseCommand = func(command string) bool {
					return mode == "probe-refused" && command == shellIntegrationProbe ||
						mode == "exec-refused" && command != shellIntegrationProbe
				}
			})
			h := connect(t, server)
			config := ShellConfig{Cols: 80, Rows: 24, IntegrateShell: true}
			want := "integrated-startup"
			if mode == "custom-command" {
				config.Command, want = "custom shell", "custom-startup"
			} else if mode != "integrated" {
				want = "welcome"
			}
			w := openShellWith(t, h, config)
			w.until(want, func(output string) bool { return strings.Contains(output, want) })
		})
	}
}

// Tailscale SSH names itself "Tailscale" rather than after an OpenSSH build,
// yet it is a full server that answers a second command channel. The shell it
// starts must still be given directory reporting, or the file panel never
// follows the working directory (issue #198).
func TestTailscaleSSHStartsTheShellIntegrated(t *testing.T) {
	server := startServer(t, func(server *testServer) {
		server.password, server.shell = "secret", echoShell
		server.version = "SSH-2.0-Tailscale"
		server.command = func(command string, channel ssh.Channel) {
			defer channel.Close()
			if command == shellIntegrationProbe {
				io.WriteString(channel, "\x1emotty-shell:/bin/zsh\x1f")
			} else {
				io.WriteString(channel, "integrated-startup")
			}
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
		}
	})
	h := connect(t, server)
	w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24, IntegrateShell: true})
	w.until("the integrated startup", func(output string) bool { return strings.Contains(output, "integrated-startup") })
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

// The shell family decides how the assistant may type a command, so it is
// learned from the server's own default shell, once per connection, and only
// from a server that is known to take a command channel beside a shell.
func TestShellKindComesFromTheServersDefaultShell(t *testing.T) {
	answers := map[string]struct {
		output string
		want   shellkind.Kind
	}{
		"posix":      {"motty-shell:\n", shellkind.POSIX},
		"powershell": {"motty-shell:System.Management.Automation.PSVersionHashtable\r\n", shellkind.PowerShell},
		"cmd":        {"\"motty-shell:$PSVersionTable$FISH_VERSION\"\r\n", shellkind.Cmd},
		"garbage":    {"unexpected", shellkind.Unknown},
	}
	for name, answer := range answers {
		t.Run(name, func(t *testing.T) {
			var probes int
			var mu sync.Mutex
			server := startServer(t, func(server *testServer) {
				server.password, server.shell = "secret", echoShell
				server.version = "SSH-2.0-OpenSSH_for_Windows_9.5"
				server.command = func(command string, channel ssh.Channel) {
					defer channel.Close()
					if command == shellkind.Probe {
						mu.Lock()
						probes++
						mu.Unlock()
						io.WriteString(channel, answer.output)
					}
					_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				}
			})
			h := connect(t, server)
			first := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24})
			second := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24})
			if first.shell.Kind() != answer.want || second.shell.Kind() != answer.want {
				t.Fatalf("kinds = %v, %v; want %v", first.shell.Kind(), second.shell.Kind(), answer.want)
			}
			mu.Lock()
			defer mu.Unlock()
			if probes != 1 {
				t.Fatalf("the server was asked %d times, want once per connection", probes)
			}
		})
	}
}

func TestShellKindIsNotProbedOnAnUnknownImplementationOrForACustomCommand(t *testing.T) {
	for _, mode := range []string{"gateway", "custom-command"} {
		t.Run(mode, func(t *testing.T) {
			server := startServer(t, func(server *testServer) {
				server.password, server.shell = "secret", echoShell
				server.version = "SSH-2.0-OpenSSH_9.6"
				if mode == "gateway" {
					server.version = "SSH-2.0-SomeGateway"
				}
				server.command = func(command string, channel ssh.Channel) {
					defer channel.Close()
					if command == shellkind.Probe {
						t.Errorf("%s: the shell family was probed", mode)
					}
					_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				}
			})
			h := connect(t, server)
			config := ShellConfig{Cols: 80, Rows: 24}
			if mode == "custom-command" {
				config.Command = "custom shell"
			}
			if kind := openShellWith(t, h, config).shell.Kind(); kind != shellkind.Unknown {
				t.Fatalf("kind = %v, want Unknown", kind)
			}
		})
	}
}
