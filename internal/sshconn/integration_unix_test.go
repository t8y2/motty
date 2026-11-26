//go:build !windows

package sshconn

import (
	"context"
	"io"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/creack/pty"
	"golang.org/x/crypto/ssh"
	"motty/internal/localshell"
	"motty/internal/shellkind"
	"motty/internal/term"
)

func TestIntegrationRealSSHPTYTracksCD(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable", name)
			}
			home := t.TempDir()
			target := filepath.Join(home, "ssh dir%#")
			start := filepath.Join(home, "start dir")
			for _, directory := range []string{target, start} {
				if err := os.Mkdir(directory, 0700); err != nil {
					t.Fatal(err)
				}
			}
			server := startServer(t, func(server *testServer) {
				server.password = "secret"
				server.version = "SSH-2.0-OpenSSH_9.6"
				// The shell request starts a real login shell, the way sshd does;
				// the integration is installed into it afterwards (ADR 0025).
				server.shell = func(shell *fakeShell) {
					cmd := exec.Command(program, "-l")
					cmd.Dir = home
					cmd.Env = append(os.Environ(), "HOME="+home, "SHELL="+program, "TERM=xterm-256color", "LANG=en_US.UTF-8")
					master, err := pty.Start(cmd)
					if err != nil {
						t.Errorf("real shell: %v", err)
						shell.exit(1)
						return
					}
					defer master.Close()
					inputDone := make(chan struct{})
					go func() {
						io.Copy(master, shell.Channel)
						master.Close()
						close(inputDone)
					}()
					io.Copy(shell.Channel, master)
					cmd.Wait()
					shell.exit(0)
					<-inputDone
				}
				server.command = func(command string, channel ssh.Channel) {
					defer channel.Close()
					if command == shellProbe {
						io.WriteString(channel, "\x1emotty-shell:"+program+"\x1f\nmotty-shell:\n")
						channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
						return
					}
					// The integration file is written over a command session,
					// not the pty (ADR 0025).
					cmd := exec.Command("/bin/sh", "-c", command)
					cmd.Stdin = channel
					cmd.Stdout = channel
					status := uint32(0)
					if err := cmd.Run(); err != nil {
						status = 1
					}
					channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
				}
			})
			h := connect(t, server)
			w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24})
			path, _ := h.conn.ProbeShell(context.Background())
			if path != program {
				t.Fatalf("ProbeShell path = %q, want %q", path, program)
			}
			line, err := h.conn.UploadIntegration(context.Background(), path, "pty-test-token", start)
			if err != nil {
				t.Fatal(err)
			}
			w.shell.Write([]byte(line))
			screen := term.New(80, 24, 0)
			screen.SetPromptToken("pty-test-token")
			ready, consumed, output := 0, 0, ""
			screen.SetPromptReady(func() { ready++ })
			parse := func(out string) {
				screen.Write([]byte(out[consumed:]))
				consumed = len(out)
			}
			wait := func(what string, done func() bool) {
				t.Helper()
				w.until(what, func(out string) bool { output = out; parse(out); return done() })
			}
			wait("integrated SSH prompt", func() bool { return ready > 0 })
			// The first prompt clears the line the hidden activation line
			// occupied, in the prompt's own write, so the terminal never has to
			// paint that line empty (issue #960).
			if !strings.Contains(output, "\r\x1b[2K") {
				t.Fatalf("the first prompt did not clear the hidden activation line: %q", output)
			}
			if cwd, ok := screen.WorkingDirectory(); !ok || cwd.Path != start {
				t.Fatalf("the first prompt reports %q (ok %v), want the start directory %q", cwd.Path, ok, start)
			}
			// The activation line must not be in the shell's history. The
			// pattern is built at run time, so the check command itself cannot
			// be what it finds.
			before := ready
			w.shell.Write([]byte("printf 'HISTORY_MOTTY=%s\\n' \"$(history | grep -c -e \"$(printf motty)-ready\" -e \"$(printf _motty)_hist_on\")\"\r"))
			wait("history check", func() bool { return ready > before && strings.Contains(output, "HISTORY_MOTTY=") })
			if !strings.Contains(output, "HISTORY_MOTTY=0") {
				t.Fatalf("the integration line was recorded in history: %q", output)
			}
			before = ready
			w.shell.Write([]byte("cd -- '" + target + "'\r"))
			wait("SSH cd report", func() bool {
				cwd, ok := screen.WorkingDirectory()
				return ready > before && ok && cwd.Path == target
			})
			if got := strings.Count(output, "\r\x1b[2K"); got != 1 {
				t.Fatalf("the first-prompt clear ran %d times, want once: %q", got, output)
			}
			w.shell.Write([]byte("exit\r"))
			w.waitDone()
		})
	}
}

// integrationPTYServer serves real shells on a pty the way sshd does, with
// the integration file delivered over a command session (ADR 0025), for the
// first-prompt-erase tests. Several shells can run over it one after another.
func integrationPTYServer(t *testing.T, name, program string) *harness {
	t.Helper()
	home := t.TempDir()
	// A .zshrc keeps zsh-newuser-install quiet: with an empty home it comes up
	// before the first prompt and consumes the first character of the lines
	// the test types ahead, which would mangle them.
	if err := os.WriteFile(filepath.Join(home, ".zshrc"), []byte("# test\n"), 0600); err != nil {
		t.Fatal(err)
	}
	server := startServer(t, func(server *testServer) {
		server.password = "secret"
		server.version = "SSH-2.0-OpenSSH_9.6"
		server.shell = func(shell *fakeShell) {
			// name comes from the fixed {bash, zsh} allowlist; the switch
			// keeps the started executables explicit literals.
			var cmd *exec.Cmd
			switch name {
			case "bash":
				cmd = exec.Command("bash", "-l")
			case "zsh":
				cmd = exec.Command("zsh", "-l")
			default:
				t.Errorf("real shell: %s is not a supported shell", name)
				shell.exit(1)
				return
			}
			cmd.Dir = home
			cmd.Env = append(os.Environ(), "HOME="+home, "SHELL="+program, "TERM=xterm-256color", "LANG=en_US.UTF-8")
			master, err := pty.Start(cmd)
			if err != nil {
				t.Errorf("real shell: %v", err)
				shell.exit(1)
				return
			}
			defer master.Close()
			inputDone := make(chan struct{})
			go func() {
				io.Copy(master, shell.Channel)
				master.Close()
				close(inputDone)
			}()
			io.Copy(shell.Channel, master)
			cmd.Wait()
			shell.exit(0)
			<-inputDone
		}
		server.command = func(command string, channel ssh.Channel) {
			defer channel.Close()
			if command == shellProbe {
				io.WriteString(channel, "\x1emotty-shell:"+program+"\x1f\nmotty-shell:\n")
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
			// The command is the integration upload the code under test sends
			// over this test's own channel. It passes through the environment,
			// so it is never part of the command line's arguments.
			cmd := exec.Command("/bin/sh", "-c", `eval "$MOTTY_SESSION_COMMAND"`)
			cmd.Env = append(os.Environ(), "MOTTY_SESSION_COMMAND="+command)
			cmd.Stdin = channel
			cmd.Stdout = channel
			status := uint32(0)
			if err := cmd.Run(); err != nil {
				status = 1
			}
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		}
	})
	return connect(t, server)
}

// eraseSession is a fresh pty shell with the integration injected, tracking
// the raw output and the prompt reports for assertions.
type eraseSession struct {
	w      *shellWatcher
	token  string
	prompt string
	output string
}

// startIntegratedShell injects the integration into a fresh shell and waits
// for the first integrated prompt. before, when set, is typed at the shell's
// own prompt before the activation line: an environment that was already
// active when the integration landed.
func startIntegratedShell(t *testing.T, h *harness, token, before string) *eraseSession {
	t.Helper()
	s := &eraseSession{
		w:      openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24}),
		token:  token,
		prompt: "\x1b]133;A;motty=" + token + "\x07",
	}
	if before != "" {
		s.w.shell.Write([]byte(before + "\r"))
	}
	path, _ := h.conn.ProbeShell(context.Background())
	line, err := h.conn.UploadIntegration(context.Background(), path, token, "")
	if err != nil {
		t.Fatal(err)
	}
	s.w.shell.Write([]byte(line))
	// The OSC 133;B report sits at the end of PS1, so its arrival means the
	// whole prompt, first-prompt clear included, has been received.
	end := "\x1b]133;B;motty=" + token + "\x07"
	s.w.until("integrated prompt", func(out string) bool {
		s.output = out
		return strings.Count(out, s.prompt) > 0 && strings.Contains(out, end)
	})
	return s
}

// nextPrompt types line at the current prompt and returns the output that
// follows it, up to and including the next prompt's full render.
func (s *eraseSession) nextPrompt(t *testing.T, what, line string) string {
	t.Helper()
	start := len(s.output)
	before := strings.Count(s.output, s.prompt)
	s.w.shell.Write([]byte(line + "\r"))
	end := "\x1b]133;B;motty=" + s.token + "\x07"
	s.w.until(what, func(out string) bool {
		s.output = out
		return strings.Count(out, s.prompt) > before && strings.Contains(out[start:], end)
	})
	return s.output[start:]
}

// TestIntegrationFirstPromptEraseSurvivesEnvActivation covers the window
// between the first and the second prompt after the injection, where an
// activation (conda, virtualenv) prepends the environment name in front of
// the first-prompt clear. The clear must still be dropped from PS1, and from
// no other position: a residue wipes the environment name off every later
// prompt (issue #1121), while dropping it has to keep clearing the hidden
// activation line once (issue #960).
func TestIntegrationFirstPromptEraseSurvivesEnvActivation(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable", name)
			}
			h := integrationPTYServer(t, name, program)
			for _, tc := range []struct {
				what     string
				before   string // typed before the injection: the env was already active
				activate string // typed after the first prompt: activates the env late
				rendered string // what the activation adds in front of the prompt
			}{
				// The typed line splits the environment name with "" so its
				// echo can never match the rendered prefix.
				{what: "conda activates between the prompts", activate: `PS1="(motty""-conda) "$PS1`, rendered: "(motty-conda) "},
				{what: "venv activates between the prompts", activate: `PS1="(motty""-venv) "$PS1`, rendered: "(motty-venv) "},
				{what: "nested envs activate between the prompts", activate: `PS1="(motty""-venv) (motty""-conda) "$PS1`, rendered: "(motty-venv) (motty-conda) "},
				{what: "env active before the injection", before: `PS1="(motty""-before) "$PS1`, rendered: "(motty-before) "},
			} {
				t.Run(tc.what, func(t *testing.T) {
					s := startIntegratedShell(t, h, "erase-env", tc.before)
					if !strings.Contains(s.output, "\r\x1b[2K") {
						t.Fatalf("the first prompt did not clear the hidden activation line: %q", s.output)
					}
					line := tc.activate
					if line == "" {
						line = "true"
					}
					rest := s.nextPrompt(t, "the prompt after the activation", line)
					if !strings.Contains(rest, tc.rendered) {
						t.Fatalf("the prompt does not show %q: %q", tc.rendered, rest)
					}
					if strings.Contains(rest, "\r\x1b[2K") {
						t.Fatalf("the first-prompt clear survived the activation and wipes %q off the prompt: %q", tc.rendered, rest)
					}
					// The variable itself is the exact state, seen from the
					// shell: after the one-shot drop it must be out of PS1.
					rest = s.nextPrompt(t, "the PS1 erase probe", `printf 'MOTTY_ERASE=%s\n' "$([[ $PS1 == *"$_motty_first_prompt_erase"* ]] && echo yes || echo no)"`)
					if !strings.Contains(rest, "MOTTY_ERASE=no") {
						t.Fatalf("the first-prompt clear is still in PS1: %q", rest)
					}
					if got := strings.Count(s.output, "\r\x1b[2K"); got != 1 {
						t.Fatalf("the first-prompt clear ran %d times, want once: %q", got, s.output)
					}
					s.w.shell.Write([]byte("exit\r"))
					s.w.waitDone()
				})
			}
		})
	}
}

// TestIntegrationFirstPromptEraseSurvivesVenvSnapshotRestore covers the other
// way a virtualenv keeps the clear alive: activate snapshots PS1 and
// deactivate restores that snapshot, which still holds the clear. Every
// prompt has to drop it again, or the environment label of a later prompt is
// erased (issue #1121).
func TestIntegrationFirstPromptEraseSurvivesVenvSnapshotRestore(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable: %v", name, err)
			}
			h := integrationPTYServer(t, name, program)
			s := startIntegratedShell(t, h, "erase-venv", "")
			if !strings.Contains(s.output, "\r\x1b[2K") {
				t.Fatalf("the first prompt did not clear the hidden activation line: %q", s.output)
			}
			// A real venv activate snapshots PS1 and then prepends its label.
			rest := s.nextPrompt(t, "the venv activation", `_motty_old_ps1=$PS1; PS1="(motty""-venv) "$PS1`)
			if !strings.Contains(rest, "(motty-venv) ") || strings.Contains(rest, "\r\x1b[2K") {
				t.Fatalf("the activated prompt is wrong: %q", rest)
			}
			// deactivate restores the snapshot, clear included.
			rest = s.nextPrompt(t, "the venv deactivation", `PS1=$_motty_old_ps1`)
			if strings.Contains(rest, "\r\x1b[2K") {
				t.Fatalf("the restored snapshot kept the clear: %q", rest)
			}
			rest = s.nextPrompt(t, "a second activation", `PS1="(motty""-venv2) "$PS1`)
			if !strings.Contains(rest, "(motty-venv2) ") || strings.Contains(rest, "\r\x1b[2K") {
				t.Fatalf("the second activated prompt is wrong: %q", rest)
			}
			rest = s.nextPrompt(t, "the PS1 erase probe", `printf 'MOTTY_ERASE=%s\n' "$([[ $PS1 == *"$_motty_first_prompt_erase"* ]] && echo yes || echo no)"`)
			if !strings.Contains(rest, "MOTTY_ERASE=no") {
				t.Fatalf("the first-prompt clear is still in PS1: %q", rest)
			}
			if got := strings.Count(s.output, "\r\x1b[2K"); got != 1 {
				t.Fatalf("the first-prompt clear ran %d times, want once: %q", got, s.output)
			}
			s.w.shell.Write([]byte("exit\r"))
			s.w.waitDone()
		})
	}
}

// TestIntegrationFirstPromptSurvivesResizeBeforeRedraw verifies issue #1642 end
// to end on real Bash and Zsh PTYs: when the login shell prints its initial
// prompt, the activation line's echo is hidden up to IntegrationReadyMarker,
// and the terminal resizes before typing at the first integrated prompt, the
// echoed input must not wipe the prompt off the row.
func TestIntegrationFirstPromptSurvivesResizeBeforeRedraw(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable: %v", name, err)
			}
			h := integrationPTYServer(t, name, program)
			w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24})
			screen := term.New(80, 24, 100)
			const token = "first-prompt-resize"
			screen.SetPromptToken(token)

			// Wait for the pre-injection login prompt so the screen cursor stands
			// past column zero, matching a newly opened SSH terminal.
			consumed := 0
			w.until("initial login prompt", func(out string) bool {
				screen.Write([]byte(out[consumed:]))
				consumed = len(out)
				return screen.Cursor().X > 0
			})

			path, _ := h.conn.ProbeShell(context.Background())
			line, err := h.conn.UploadIntegration(context.Background(), path, token, "")
			if err != nil {
				t.Fatal(err)
			}
			suppressFrom := consumed
			marker := string(IntegrationReadyMarker(token))
			ready := 0
			screen.SetPromptReady(func() { ready++ })
			w.shell.Write([]byte(line))
			fedAfterMarker := 0
			w.until("first integrated prompt", func(out string) bool {
				idx := strings.Index(out[suppressFrom:], marker)
				if idx < 0 {
					return false
				}
				after := out[suppressFrom+idx+len(marker):]
				if len(after) > fedAfterMarker {
					screen.Write([]byte(after[fedAfterMarker:]))
					fedAfterMarker = len(after)
				}
				consumed = len(out)
				return ready > 0
			})

			promptRow := screen.Cursor().Y
			promptText := strings.TrimRight(screen.RowText(promptRow), " ")
			if promptText == "" {
				t.Fatalf("expected non-empty prompt before resize, got row %q", screen.RowText(promptRow))
			}

			// Resize the terminal screen before the shell redraws, then type "docker".
			screen.Resize(60, 24)
			w.shell.Write([]byte("docker"))
			w.until("echoed docker", func(out string) bool {
				if len(out) > consumed {
					screen.Write([]byte(out[consumed:]))
					consumed = len(out)
				}
				return strings.Contains(screen.RowText(screen.Cursor().Y), "docker")
			})
			gotRow := screen.RowText(screen.Cursor().Y)
			if !strings.HasPrefix(gotRow, promptText) {
				t.Fatalf("typing after resize wiped the prompt: got row %q, want prefix %q", gotRow, promptText)
			}

			w.shell.Write([]byte("\x15exit\r"))
			w.waitDone()
		})
	}
}

func TestIntegrationReportsRealShellDirectoriesWithoutChangingProfiles(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable: %v", name, err)
			}
			home, runtimeDir := filepath.Join(t.TempDir(), "home %#;中文"), t.TempDir()
			if err := os.MkdirAll(home, 0700); err != nil {
				t.Fatal(err)
			}
			target := filepath.Join(home, "a b%'#;$()", "caf\u00e9")
			if err := os.MkdirAll(target, 0700); err != nil {
				t.Fatal(err)
			}
			profile := "printf 'user-profile-loaded\\n'\n"
			file := ".bash_profile"
			if name == "bash" {
				profile += "PS1='user-prompt> '; PROMPT_COMMAND='printf \"user-hook\\n\"'\n"
			} else {
				file = ".zshrc"
				profile += "PS1='user-prompt> '; precmd() { printf 'user-hook\\n'; }\n"
			}
			profilePath := filepath.Join(home, file)
			if err := os.WriteFile(profilePath, []byte(profile), 0600); err != nil {
				t.Fatal(err)
			}
			shell, err := localshell.Start(localshell.Config{
				Shell: "/bin/sh", Args: []string{"-c", "umask 022; " + integrationCommand(program, "pty-test-token")}, Dir: home,
				Env:  []string{"HOME=" + home, "ZDOTDIR=" + home, "SHELL=" + program, "TMPDIR=" + runtimeDir, "LANG=en_US.UTF-8"},
				Cols: 120, Rows: 24,
			})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(shell.Close)
			screen := term.New(120, 24, 0)
			screen.SetPromptToken("pty-test-token")
			reportedHome := ""
			screen.SetHomeDirectory(func(value string) { reportedHome = value })
			ready, commandStarts, output := 0, 0, ""
			screen.SetPromptReady(func() { ready++ })
			screen.SetCommandStart(func() { commandStarts++ })
			lastExit := -1
			screen.SetCommandFinished(func(code int) { lastExit = code })
			await := func(want string, predicate func() bool) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					data := shell.Drain(nil)
					output += string(data)
					screen.Write(data)
					if predicate() {
						return
					}
					time.Sleep(5 * time.Millisecond)
				}
				t.Fatalf("waiting for %s: %q", want, output)
			}
			await("initial ready prompt", func() bool { return ready > 0 })
			if reportedHome != home {
				t.Fatalf("reported home = %q, want %q", reportedHome, home)
			}
			// The first prompt of a shell Motty starts is on a fresh line. Zsh's
			// PROMPT_SP mark would be sized by the width the shell started with and
			// leave a stray "%" when the pane is narrower by the time it arrives.
			if strings.Contains(output, "\x1b[7m%") {
				t.Fatalf("first prompt printed the PROMPT_SP mark: %q", output)
			}
			beforeReady := ready
			shell.Write([]byte("false\r"))
			await("failed command status", func() bool { return ready > beforeReady })
			if lastExit != 1 {
				t.Fatalf("false exit status = %d; output %q", lastExit, output)
			}
			beforeReady = ready
			shell.Write([]byte("true\r"))
			await("successful command status", func() bool { return ready > beforeReady })
			if lastExit != 0 {
				t.Fatalf("true exit status = %d", lastExit)
			}

			// Every Bash reports command starts: PS0 from 4.4, a DEBUG trap before.
			commandReporting := true
			if commandStarts == 0 {
				t.Fatal("shell did not announce command-start reporting")
			}
			if !strings.Contains(output, "user-profile-loaded") || !strings.Contains(output, "user-hook") {
				t.Fatalf("profile/hook lost: %q", output)
			}
			shell.Write([]byte(": > \"$HOME/mask-file\"; mkdir \"$HOME/mask-dir\"; printf 'mask-check-done\\n'\r"))
			await("file creation", func() bool { return strings.Contains(output, "mask-check-done\r\n") })
			for name, mode := range map[string]os.FileMode{"mask-file": 0644, "mask-dir": 0755} {
				info, err := os.Stat(filepath.Join(home, name))
				if err != nil || info.Mode().Perm() != mode {
					t.Fatalf("inherited umask changed for %s: info=%v, err=%v; want %04o", name, info, err, mode)
				}
			}
			before, startsBefore := ready, commandStarts
			shell.Write([]byte("cd -- '" + strings.ReplaceAll(target, "'", "'\\''") + "'\r"))
			await("cd report", func() bool {
				cwd, ok := screen.WorkingDirectory()
				return ok && cwd.Path == target && ready > before
			})
			if commandReporting && commandStarts <= startsBefore {
				t.Fatal("an actual cd was not preceded by a command-start marker")
			}
			if strings.Count(output, "user-hook\r\n") < 2 {
				t.Fatalf("hook was overwritten: %q", output)
			}
			if name == "zsh" {
				shell.Write([]byte("printf 'zdot=%s\\n' \"$ZDOTDIR\"\r"))
				await("restored ZDOTDIR", func() bool { return strings.Contains(output, "zdot="+home+"\r\n") })
			}
			shell.Write([]byte("exit 7\r"))
			select {
			case <-shell.Done():
			case <-time.After(8 * time.Second):
				t.Fatal("integrated shell did not exit")
			}
			if shell.ExitStatus() != 7 {
				t.Fatalf("exit status = %d, want 7", shell.ExitStatus())
			}
			if data, err := os.ReadFile(profilePath); err != nil || string(data) != profile {
				t.Fatalf("profile changed: %q, %v", data, err)
			}
			if files, err := os.ReadDir(runtimeDir); err != nil || len(files) != 0 {
				t.Fatalf("session files leaked: %v, %v", files, err)
			}
		})
	}
}

// A login shell reads ~/.bashrc only through the login profile that sources
// it. The startup shim must run the user's startup files the way that login
// shell does: sourcing ~/.bashrc after the profile already did ran the user's
// interactive startup twice on the distributions whose profiles source it.
func TestIntegrationBashStartupMatchesALoginShell(t *testing.T) {
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	for _, test := range []struct {
		name, profile string
		want          int
	}{
		{
			// The Debian, Ubuntu and RHEL default: the login profile sources
			// ~/.bashrc itself.
			name:    "login-profile-sources-bashrc",
			profile: "if [ -r \"$HOME/.bashrc\" ]; then . \"$HOME/.bashrc\"; fi\n",
			want:    1,
		},
		{
			// No login profile: a login shell never reads ~/.bashrc.
			name: "bashrc-only",
			want: 0,
		},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, runtimeDir := t.TempDir(), t.TempDir()
			if err := os.WriteFile(filepath.Join(home, ".bashrc"), []byte("printf 'user-bashrc\\n'; PS1='user-prompt> '\n"), 0600); err != nil {
				t.Fatal(err)
			}
			if test.profile != "" {
				if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(test.profile), 0600); err != nil {
					t.Fatal(err)
				}
			}
			env := []string{"HOME=" + home, "SHELL=" + program, "TMPDIR=" + runtimeDir, "LANG=en_US.UTF-8"}
			read := func(name string, cfg localshell.Config) string {
				t.Helper()
				shell, err := localshell.Start(cfg)
				if err != nil {
					t.Fatalf("start %s: %v", name, err)
				}
				t.Cleanup(shell.Close)
				var output strings.Builder
				deadline := time.Now().Add(8 * time.Second)
				quiet := time.Now().Add(500 * time.Millisecond)
				for time.Now().Before(deadline) {
					if data := shell.Drain(nil); len(data) > 0 {
						output.Write(data)
						quiet = time.Now().Add(500 * time.Millisecond)
					} else if time.Now().After(quiet) {
						break
					}
					time.Sleep(10 * time.Millisecond)
				}
				return output.String()
			}
			login := read("a login shell", localshell.Config{
				Shell: program, Args: []string{"-l", "-i"}, Dir: home, Env: env, Cols: 100, Rows: 24,
			})
			shim := read("the startup shim", localshell.Config{
				Shell: "/bin/sh", Args: IntegrationArgs(program, "startup-token"), Dir: home, Env: env, Cols: 100, Rows: 24,
			})
			if runs := strings.Count(login, "user-bashrc"); runs != test.want {
				t.Fatalf("the fixture's login shell ran ~/.bashrc %d time(s), want %d: %q", runs, test.want, login)
			}
			if runs := strings.Count(shim, "user-bashrc"); runs != test.want {
				t.Fatalf("the startup shim ran ~/.bashrc %d time(s); a login shell runs it %d time(s): %q", runs, test.want, shim)
			}
		})
	}
}

func TestIntegrationZshHistorySurvivesSessions(t *testing.T) {
	program, err := exec.LookPath("zsh")
	if err != nil {
		t.Skipf("zsh unavailable: %v", err)
	}
	for _, test := range []struct{ name, location, rc, login string }{
		{name: "default"},
		{name: "custom-zdotdir", location: "environment"},
		{name: "zshenv-zdotdir", location: ".zshenv"},
		{name: "custom-history", rc: `HISTFILE="$HOME/custom history"`},
		{name: "login-history", login: `HISTFILE="$HOME/login history"`},
		{name: "unset-history", rc: "unset HISTFILE\nsetopt NO_UNSET"},
		{name: "empty-history", rc: "HISTFILE="},
	} {
		t.Run(test.name, func(t *testing.T) {
			home, runtimeDir := t.TempDir(), t.TempDir()
			t.Setenv("HOME", home)
			t.Setenv("ZDOTDIR", "")
			os.Unsetenv("ZDOTDIR")
			t.Setenv("TERM_PROGRAM", "motty-history-test")
			dotdir := home
			if test.location != "" {
				dotdir = filepath.Join(home, "zsh files %'#")
				if err := os.Mkdir(dotdir, 0700); err != nil {
					t.Fatal(err)
				}
				if test.location == "environment" {
					t.Setenv("ZDOTDIR", dotdir)
				} else if err := os.WriteFile(filepath.Join(home, ".zshenv"), []byte("ZDOTDIR=\"$HOME/zsh files %'#\"\n"), 0600); err != nil {
					t.Fatal(err)
				}
			}
			profiles := map[string]string{
				".zshrc":  "_test_rc_histfile=${HISTFILE-}\nPS1='history-test> '\nHISTSIZE=200\nSAVEHIST=200\n" + test.rc + "\n",
				".zlogin": test.login + "\n",
			}
			for name, text := range profiles {
				if err := os.WriteFile(filepath.Join(dotdir, name), []byte(text), 0600); err != nil {
					t.Fatal(err)
				}
			}
			env := []string{"SHELL=" + program, "TMPDIR=" + runtimeDir, "TERM=xterm-256color", "LANG=en_US.UTF-8"}
			// Compare with an ordinary login shell on this machine, including
			// what the user's rc file saw before choosing its own history path.
			diagnostic := `printf '\036history=%s\037rc=%s\036' "${HISTFILE-}" "$_test_rc_histfile"`
			plain := exec.Command(program, "-lic", diagnostic)
			plain.Env = append(os.Environ(), env...)
			data, err := plain.CombinedOutput()
			if err != nil {
				t.Fatalf("ordinary zsh: %v: %q", err, data)
			}
			_, expected, ok := strings.Cut(string(data), "\x1ehistory=")
			if !ok {
				t.Fatalf("ordinary zsh did not report its history path: %q", data)
			}
			expected, _, ok = strings.Cut(expected, "\x1e")
			if !ok {
				t.Fatalf("incomplete history report: %q", data)
			}
			histfile, _, _ := strings.Cut(expected, "\x1frc=")
			for session := 0; session < 2; session++ {
				shell, err := localshell.Start(localshell.Config{
					Shell: "/bin/sh", Args: IntegrationArgs(program, "history-token"), Dir: home,
					Env: env, Cols: 120, Rows: 24,
				})
				if err != nil {
					t.Fatal(err)
				}
				t.Cleanup(shell.Close)
				screen := term.New(120, 24, 0)
				screen.SetPromptToken("history-token")
				ready, output := 0, ""
				screen.SetPromptReady(func() { ready++ })
				await := func(what string, done func() bool) {
					t.Helper()
					deadline := time.Now().Add(8 * time.Second)
					for time.Now().Before(deadline) {
						data := shell.Drain(nil)
						output += string(data)
						screen.Write(data)
						if done() {
							return
						}
						time.Sleep(5 * time.Millisecond)
					}
					t.Fatalf("session %d waiting for %s: %q", session, what, output)
				}
				run := func(input, want string) {
					t.Helper()
					before := ready
					output = ""
					shell.Write([]byte(input + "\r"))
					await(input, func() bool { return ready > before && strings.Contains(output, want) })
				}
				await("first prompt", func() bool { return ready > 0 })
				if session == 1 && histfile != "" {
					run("\x1b[A", "\x1emotty-history-marker\x1f")
				}
				run(diagnostic, "\x1ehistory="+expected+"\x1e")
				if session == 0 {
					run(`printf '\036motty-history-marker\037\n'`, "\x1emotty-history-marker\x1f")
					run("\x1b[A", "\x1emotty-history-marker\x1f")
				}
				// EOF saves history without adding an exit command for the next
				// session's Up key to recall instead of the marker.
				shell.Write([]byte("\x04"))
				select {
				case <-shell.Done():
				case <-time.After(8 * time.Second):
					t.Fatal("integrated zsh did not exit")
				}
				if shell.ExitStatus() != 0 {
					t.Fatalf("exit status = %d: %q", shell.ExitStatus(), shell.Drain(nil))
				}
				if histfile != "" {
					data, err := os.ReadFile(histfile)
					if err != nil || !strings.Contains(string(data), "motty-history-marker") {
						t.Fatalf("history was not saved to %q: %q, %v", histfile, data, err)
					}
				}
			}
			for name, want := range profiles {
				if data, err := os.ReadFile(filepath.Join(dotdir, name)); err != nil || string(data) != want {
					t.Fatalf("profile %s changed: %q, %v", name, data, err)
				}
			}
			if files, err := os.ReadDir(runtimeDir); err != nil || len(files) != 0 {
				t.Fatalf("session files leaked: %v, %v", files, err)
			}
		})
	}
}

func TestIntegrationRejectsUnsupportedShells(t *testing.T) {
	for _, shell := range []string{"", "bash", "/bin/fish", "/bin/bash\n", "/bin/bash;touch /tmp/no"} {
		if command := integrationCommand(shell); command != "" {
			t.Errorf("unsupported shell %q enabled integration", shell)
		}
	}
}

// Bash before 4.4 has no PS0, so it reports the start of a command line from a
// DEBUG trap. The trap path is forced here so that a current Bash exercises it,
// and macOS's Bash 3.2 takes it by itself. Each line typed must be announced
// once, nothing else may be, and the user's own DEBUG trap must still run.
func TestIntegrationDebugTrapReportsEachCommandLineOnce(t *testing.T) {
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	checkCommandStartReports(t, program, true)
}

// MOTTY_TEST_OLD_BASH names a real Bash before 4.4, such as the 4.2 of CentOS 7,
// which then starts the way it does for a user, with nothing forced.
func TestIntegrationRealOldBashReportsEachCommandLineOnce(t *testing.T) {
	program := os.Getenv("MOTTY_TEST_OLD_BASH")
	if program == "" {
		t.Skip("MOTTY_TEST_OLD_BASH names no Bash before 4.4")
	}
	checkCommandStartReports(t, program, false)
}

func checkCommandStartReports(t *testing.T, program string, forceTrap bool) {
	t.Helper()
	home, runtimeDir := t.TempDir(), t.TempDir()
	profile := "PS1='user-prompt> '\nPROMPT_COMMAND='printf \"user-hook\\n\"'\ntrap 'printf \"user-debug:%s\\n\" \"$BASH_COMMAND\"' DEBUG\n"
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	script := integrationScriptWith(program, forceTrap, "debug-trap-token")
	shell, err := localshell.Start(localshell.Config{
		Shell: "/bin/sh", Args: []string{"-c", script, "sh", program}, Dir: home,
		Env:  []string{"HOME=" + home, "SHELL=" + program, "TMPDIR=" + runtimeDir, "LANG=en_US.UTF-8"},
		Cols: 120, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	screen := term.New(120, 24, 0)
	screen.SetPromptToken("debug-trap-token")
	ready, starts, output := 0, 0, ""
	screen.SetPromptReady(func() { ready++ })
	screen.SetCommandStart(func() { starts++ })
	await := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			data := shell.Drain(nil)
			output += string(data)
			screen.Write(data)
			if done() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("waiting for %s (ready=%d starts=%d): %q", what, ready, starts, output)
	}
	// run sends input and waits for the prompts that follow it, then lets the
	// shell go quiet so that a stray extra report would be seen.
	run := func(input string, prompts int, text string) {
		t.Helper()
		before := ready
		shell.Write([]byte(input))
		await(input, func() bool { return ready >= before+prompts && strings.Contains(output, text) })
		time.Sleep(100 * time.Millisecond)
		data := shell.Drain(nil)
		output += string(data)
		screen.Write(data)
	}
	await("first prompt", func() bool { return ready > 0 })
	if starts != 1 {
		t.Fatalf("the shell did not announce command-start reporting once: %d", starts)
	}
	steps := []struct {
		name, input, text string
		prompts, starts   int
	}{
		{"one command", "echo marker-one\r", "marker-one\r\n", 1, 1},
		{"an empty line", "\r", "", 1, 0},
		{"typeahead", "echo marker-two\recho marker-three\r", "marker-three\r\n", 2, 2},
		{"a compound command", "for i in 1 2 3; do echo loop$i; done\r", "loop3\r\n", 1, 1},
		{"a pipeline", "echo piped | cat\r", "piped\r\n", 1, 1},
		{"a failing command", "false\r", "", 1, 1},
	}
	for _, step := range steps {
		before := starts
		run(step.input, step.prompts, step.text)
		if starts-before != step.starts {
			t.Fatalf("%s: %d command starts reported, want %d; output %q", step.name, starts-before, step.starts, output)
		}
	}
	run("echo status=$?\r", 1, "status=1\r\n")
	if !strings.Contains(output, "user-debug:echo marker-one\r\n") {
		t.Fatalf("the user's DEBUG trap did not run with its BASH_COMMAND: %q", output)
	}
	if !strings.Contains(output, "user-hook\r\n") {
		t.Fatalf("the user's PROMPT_COMMAND was lost: %q", output)
	}
	shell.Write([]byte("exit 5\r"))
	select {
	case <-shell.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("the shell did not exit")
	}
	if shell.ExitStatus() != 5 {
		t.Fatalf("exit status = %d, want 5", shell.ExitStatus())
	}
	if files, err := os.ReadDir(runtimeDir); err != nil || len(files) != 0 {
		t.Fatalf("session files leaked: %v, %v", files, err)
	}
}

// A user who exports PROMPT_COMMAND keeps that export when the integration
// wraps it, so su/sudo hand the value to a shell that has none of the motty
// functions (issue #1060). The injected entries must expand to nothing there
// instead of "command not found", while the user's own entries keep running and
// the integrated shell keeps reporting statuses.
func TestIntegrationExportedPromptCommandDoesNotBreakChildShell(t *testing.T) {
	checkExportedPromptCommand(t, "PS1='user-prompt> '\nexport PROMPT_COMMAND='printf \"user-hook\\n\"'\n")
}

// set -a (allexport) exports the gate along with PROMPT_COMMAND, so the flag
// has to be kept unexported explicitly; otherwise su's shell inherits it and
// the wrapped entries run again.
func TestIntegrationAllexportPromptCommandDoesNotBreakChildShell(t *testing.T) {
	checkExportedPromptCommand(t, "PS1='user-prompt> '\nset -a\nPROMPT_COMMAND='printf \"user-hook\\n\"'\n")
}

func checkExportedPromptCommand(t *testing.T, profile string) {
	t.Helper()
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	home, runtimeDir := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	shell, err := localshell.Start(localshell.Config{
		Shell: "/bin/sh", Args: []string{"-c", integrationScript(program, "leak-token"), "sh", program}, Dir: home,
		Env:  []string{"HOME=" + home, "SHELL=" + program, "TMPDIR=" + runtimeDir, "LANG=en_US.UTF-8"},
		Cols: 120, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	screen := term.New(120, 24, 0)
	screen.SetPromptToken("leak-token")
	ready, lastExit, output := 0, -1, ""
	screen.SetPromptReady(func() { ready++ })
	screen.SetCommandFinished(func(code int) { lastExit = code })
	await := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			data := shell.Drain(nil)
			output += string(data)
			screen.Write(data)
			if done() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("waiting for %s: %q", what, output)
	}
	await("first prompt", func() bool { return ready > 0 })
	before := ready
	shell.Write([]byte("false\r"))
	await("parent status", func() bool { return ready > before })
	if lastExit != 1 {
		t.Fatalf("integrated shell exit status = %d, want 1: %q", lastExit, output)
	}
	// The gate itself must not reach a child shell: with it set, the wrapped
	// entries would expand to functions the child does not have.
	shell.Write([]byte("bash --norc --noprofile -c 'printf \"CHILD_FLAG %s\\n\" \"${_motty_active-unset}\"'\r"))
	shell.Write([]byte("printf 'CHILD_PROBE\\r\\n'\r"))
	await("child flag", func() bool { return strings.Contains(output, "CHILD_PROBE\r\r\n") })
	if !strings.Contains(output, "CHILD_FLAG unset") {
		t.Fatalf("a child shell inherited _motty_active: %q", output)
	}
	// A plain child shell inherits the exported PROMPT_COMMAND but defines none
	// of the integration's functions, the way su's target shell does.
	childStart := len(output)
	shell.Write([]byte("bash --norc --noprofile -i\r"))
	shell.Write([]byte("printf 'CHILD_UP\\n'\r"))
	await("child shell", func() bool { return strings.Contains(output, "CHILD_UP\r\n") })
	child := output[childStart:]
	if strings.Contains(child, "command not found") {
		t.Fatalf("inherited PROMPT_COMMAND broke the child shell: %q", child)
	}
	if !strings.Contains(child, "user-hook\r\n") {
		t.Fatalf("the user's own PROMPT_COMMAND entry stopped running in the child: %q", child)
	}
	before = ready
	shell.Write([]byte("exit\r"))
	await("back in parent", func() bool { return ready > before })
	shell.Write([]byte("exit\r"))
	select {
	case <-shell.Done():
	case <-time.After(8 * time.Second):
		t.Fatal("the shell did not exit")
	}
}

// A site audit hook can make PROMPT_COMMAND readonly (issue #1031). The folder
// must still be reported for every prompt, and the audit hook must keep running.
func TestIntegrationReadonlyPromptCommandStillReportsFolders(t *testing.T) {
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	home, runtimeDir := t.TempDir(), t.TempDir()
	target := filepath.Join(home, "opt dir")
	if err := os.Mkdir(target, 0700); err != nil {
		t.Fatal(err)
	}
	profile := "PS1='user-prompt> '\nPROMPT_COMMAND='printf \"audit-hook\\n\"'\nreadonly PROMPT_COMMAND\n"
	if err := os.WriteFile(filepath.Join(home, ".bash_profile"), []byte(profile), 0600); err != nil {
		t.Fatal(err)
	}
	shell, err := localshell.Start(localshell.Config{
		Shell: "/bin/sh", Args: []string{"-c", integrationScript(program, "readonly-token"), "sh", program}, Dir: home,
		Env:  []string{"HOME=" + home, "SHELL=" + program, "TMPDIR=" + runtimeDir, "LANG=en_US.UTF-8"},
		Cols: 120, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	screen := term.New(120, 24, 0)
	screen.SetPromptToken("readonly-token")
	var folders []string
	screen.SetCwdReport(func(cwd term.Cwd) { folders = append(folders, cwd.Path) })
	output := ""
	await := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(8 * time.Second)
		for time.Now().Before(deadline) {
			data := shell.Drain(nil)
			output += string(data)
			screen.Write(data)
			if done() {
				return
			}
			time.Sleep(5 * time.Millisecond)
		}
		t.Fatalf("waiting for %s (folders %q): %q", what, folders, output)
	}
	reported := func(directory string) bool {
		for _, folder := range folders {
			if folder == directory || strings.HasSuffix(folder, directory) {
				return true
			}
		}
		return false
	}
	await("first folder", func() bool { return len(folders) > 0 })
	shell.Write([]byte("cd 'opt dir'\r"))
	await("folder after cd", func() bool { return reported("/opt dir") })
	if !strings.Contains(output, "audit-hook\r\n") {
		t.Fatalf("the readonly PROMPT_COMMAND stopped running: %q", output)
	}
	if strings.Contains(output, "readonly variable") {
		t.Fatalf("the integration assigned to the readonly PROMPT_COMMAND: %q", output)
	}
}

// A server whose login shell is nushell must be probed as Nushell and refused
// by UploadIntegration rather than receiving bash/zsh integration syntax.
func TestProbeShellRealNushell(t *testing.T) {
	program, err := exec.LookPath("nu")
	if err != nil {
		t.Skip("nu unavailable")
	}
	home := t.TempDir()
	server := startServer(t, func(server *testServer) {
		server.password, server.shell = "secret", echoShell
		server.version = "SSH-2.0-OpenSSH_9.6"
		server.command = func(command string, channel ssh.Channel) {
			defer channel.Close()
			cmd := exec.Command(program, "-c", command)
			cmd.Env = append(os.Environ(), "HOME="+home, "SHELL=/bin/bash")
			cmd.Stdout = channel
			cmd.Stderr = channel
			status := uint32(0)
			if err := cmd.Run(); err != nil {
				status = 1
			}
			_, _ = channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
		}
	})
	h := connect(t, server)
	w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24})
	w.until("welcome", func(output string) bool { return strings.Contains(output, "welcome") })
	path, kind := h.conn.ProbeShell(context.Background())
	if kind != shellkind.Nushell {
		t.Fatalf("ProbeShell = (%q, %v), want kind %v", path, kind, shellkind.Nushell)
	}
	if _, err := h.conn.UploadIntegration(context.Background(), path, "nu-token", ""); err == nil {
		t.Fatalf("UploadIntegration(%q) succeeded, want refusal for nushell", path)
	}
}
