//go:build !windows

package sshconn

import (
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
	"motty/internal/term"
)

func TestIntegrationRealSSHPTYTracksCD(t *testing.T) {
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
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
		server.password, server.shell = "secret", echoShell
		server.version = "SSH-2.0-OpenSSH_9.6" // probes run only on known implementations
		server.command = func(command string, channel ssh.Channel) {
			defer channel.Close()
			if command == shellIntegrationProbe {
				io.WriteString(channel, "\x1emotty-shell:"+program+"\x1f")
				channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{0}))
				return
			}
			cmd := exec.Command("/bin/sh", "-c", command)
			cmd.Dir = home
			cmd.Env = append(os.Environ(), "HOME="+home, "SHELL="+program)
			master, err := pty.Start(cmd)
			if err != nil {
				t.Errorf("SSH PTY: %v", err)
				return
			}
			defer master.Close()
			inputDone := make(chan struct{})
			go func() {
				io.Copy(master, channel)
				master.Close()
				close(inputDone)
			}()
			io.Copy(channel, master)
			status := uint32(0)
			if err := cmd.Wait(); err != nil {
				status = 1
			}
			channel.SendRequest("exit-status", false, ssh.Marshal(struct{ Status uint32 }{status}))
			channel.Close()
			<-inputDone
		}
	})
	h := connect(t, server)
	w := openShellWith(t, h, ShellConfig{Cols: 80, Rows: 24, IntegrateShell: true, IntegrationToken: "pty-test-token", StartDirectory: start})
	screen := term.New(80, 24, 0)
	screen.SetPromptToken("pty-test-token")
	ready, consumed := 0, 0
	screen.SetPromptReady(func() { ready++ })
	parse := func(output string) {
		screen.Write([]byte(output[consumed:]))
		consumed = len(output)
	}
	w.until("integrated SSH prompt", func(output string) bool { parse(output); return ready > 0 })
	if cwd, ok := screen.WorkingDirectory(); !ok || cwd.Path != start {
		t.Fatalf("the first prompt reports %q (ok %v), want the start directory %q", cwd.Path, ok, start)
	}
	w.shell.Write([]byte("cd -- '" + target + "'\r"))
	w.until("SSH cd report", func(output string) bool {
		parse(output)
		cwd, ok := screen.WorkingDirectory()
		return ready > 1 && ok && cwd.Path == target
	})
	w.shell.Write([]byte("exit\r"))
	w.waitDone()
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
