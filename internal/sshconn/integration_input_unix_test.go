//go:build !windows

package sshconn

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"motty/internal/localshell"
	"motty/internal/term"
)

func TestIntegratedShellReportsActualLocaleAndConfirmedRepairFixesCJKEditing(t *testing.T) {
	for _, name := range []string{"bash", "/bin/bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skip("bash unavailable")
			}
			home := t.TempDir()
			profile := "export LC_ALL=C\nbind 'set convert-meta off'\nbind 'set input-meta on'\nbind 'set output-meta on'\nPS1='input-test> '\n"
			profilePath := filepath.Join(home, ".bash_profile")
			if filepath.Base(program) == "zsh" {
				profile = "export LC_ALL=C\nbindkey -e\nPS1='input-test> '\n"
				profilePath = filepath.Join(home, ".zshrc")
			}
			if err := os.WriteFile(profilePath, []byte(profile), 0600); err != nil {
				t.Fatal(err)
			}
			shell, err := localshell.Start(localshell.Config{Shell: "/bin/sh", Args: IntegrationArgs(program, "input-test"), Dir: home,
				Env: []string{"HOME=" + home, "SHELL=" + program, "LANG=en_US.UTF-8", "INPUTRC=/dev/null"}, Cols: 180, Rows: 24})
			if err != nil {
				t.Fatal(err)
			}
			t.Cleanup(func() {
				defer shell.Close()
				// Let the interactive child save history before its wrapper exits
				// and TempDir removes HOME; hanging up only waits for the wrapper.
				shell.Write([]byte("exit\r"))
				select {
				case <-shell.Done():
				case <-time.After(5 * time.Second):
					shell.Close()
					t.Error("input fixture shell did not exit gracefully")
				}
			})
			screen := term.New(180, 24, 0)
			screen.SetPromptToken("input-test")
			var environment term.InputEnvironment
			ready, reports, output := 0, 0, ""
			screen.SetInputEnvironment(func(value term.InputEnvironment) { environment = value; reports++ })
			screen.SetPromptReady(func() { ready++ })
			await := func(description string, done func() bool) {
				t.Helper()
				deadline := time.Now().Add(8 * time.Second)
				for time.Now().Before(deadline) {
					data := shell.Drain(nil)
					output += string(data)
					screen.Write(data)
					if done() {
						return
					}
					time.Sleep(time.Millisecond)
				}
				t.Fatalf("waiting for %s; environment=%+v output=%q", description, environment, output)
			}
			await("actual post-profile C locale", func() bool { return reports > 0 && ready > 0 })
			if environment.Charset != "other" || environment.Locale == "" {
				t.Fatalf("profile override ignored or no verified repair candidate: %+v", environment)
			}
			candidate := environment.Locale
			before := ready
			shell.Write([]byte("printf '__ORIGINAL__%s\\n' \"$LC_ALL\"\r"))
			await("detection without mutation", func() bool { return ready > before && strings.Contains(output, "__ORIGINAL__C\r\n") })
			probe := "printf '__EDIT__'; printf '%s' '\u3000\x7f-500\u5b57\x7ff' | od -An -tx1\r"
			before = ready
			shell.Write([]byte(probe))
			await("C byte-oriented edit", func() bool { return ready > before })
			if !strings.Contains(strings.Join(strings.Fields(output), " "), "e3 80 2d 35 30 30 e5 ad 66") {
				t.Fatalf("fixture did not reproduce residual UTF-8 bytes: %q", output)
			}
			before = ready
			shell.Write([]byte("builtin export LC_ALL='" + candidate + "'\r"))
			await("confirmed session repair", func() bool { return ready > before && environment.Charset == "utf8" })
			before, output = ready, ""
			shell.Write([]byte(probe))
			await("whole-character UTF-8 edit", func() bool { return ready > before })
			hexOutput := strings.Join(strings.Fields(output), " ")
			if !strings.Contains(hexOutput, "2d 35 30 30 66") || strings.Contains(hexOutput, "e3 80 2d") {
				t.Fatalf("UTF-8 repair did not fix actual readline editing: %q", output)
			}
			if data, err := os.ReadFile(profilePath); err != nil || string(data) != profile {
				t.Fatalf("session repair changed profile: %q %v", data, err)
			}
		})
	}
}

func TestInputEnvironmentProbePreservesErrexitWhenLocaleFails(t *testing.T) {
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	bin := t.TempDir()
	if err := os.WriteFile(filepath.Join(bin, "locale"), []byte("#!/bin/sh\nexit 1\n"), 0700); err != nil {
		t.Fatal(err)
	}
	script := "set -e\n" + strings.ReplaceAll(inputEnvironmentFunction, "__MOTTY_INPUT_TOKEN__", "input-test") + "\n__motty_input_report\nprintf '__SURVIVED__\\n'\n"
	command := exec.Command(program, "--noprofile", "--norc", "-c", script)
	command.Env = []string{"PATH=" + bin, "LC_ALL=C"}
	output, err := command.CombinedOutput()
	if err != nil || !strings.Contains(string(output), ";unknown;\a") || !strings.Contains(string(output), "__SURVIVED__") {
		t.Fatalf("read-only probe changed errexit/liveness: %q %v", output, err)
	}
}

func TestInputEnvironmentProbeReadsUnexportedShellLocale(t *testing.T) {
	for _, name := range []string{"/bin/bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("shell unavailable: %v", err)
			}
			script := "unset LC_ALL LC_CTYPE\nLC_ALL=C\n" + strings.ReplaceAll(inputEnvironmentFunction, "__MOTTY_INPUT_TOKEN__", "input-test") +
				"\n__motty_input_report\ntext='中文'\nprintf '__COUNT__%s\\n' \"${#text}\"\nprintf '__VALUE__%s\\n' \"$LC_ALL\"\n"
			command := exec.Command(program, "-c", script)
			home := t.TempDir()
			command.Env = []string{"PATH=" + os.Getenv("PATH"), "LANG=C.UTF-8", "HOME=" + home, "ZDOTDIR=" + home}
			output, err := command.CombinedOutput()
			if err != nil || !strings.Contains(string(output), ";input-test;other;") || !strings.Contains(string(output), "__COUNT__6") || !strings.Contains(string(output), "__VALUE__C") {
				t.Fatalf("unexported actual shell locale was ignored or changed: %q %v", output, err)
			}
		})
	}
}
