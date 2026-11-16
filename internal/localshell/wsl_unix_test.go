//go:build !windows

package localshell

import (
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"motty/internal/sshconn"
	"motty/internal/term"
)

// fakeWSLPath stands in for the wslpath of a distribution called Fake: the
// Linux path /a/b is \\wsl.localhost\Fake\a\b, and a Windows path is given back
// as the Linux one without its drive, so the test's own folders can be named.
const fakeWSLPath = `#!/bin/sh
case $1 in
-w) printf '%s' '\\wsl.localhost\Fake'"$(printf '%s' "$2" | tr / '\\')" ;;
-u) printf '%s\n' "$(printf '%s' "${2#?:}" | tr '\\' /)" ;;
*) exit 2 ;;
esac
`

// fakeGetent answers for the passwd entry of the user whose login shell the
// test is about; the script asks for it with getent, as WSL's distributions have.
const fakeGetent = `#!/bin/sh
printf 'user:x:1000:1000::%s:%s\n' "$HOME" "$MOTTY_TEST_LOGIN_SHELL"
`

func fakeWindowsPath(linux string) string {
	return `\\wsl.localhost\Fake` + strings.ReplaceAll(linux, "/", `\`)
}

func TestWSLScriptStartsTheLoginShellAndReportsWindowsFolders(t *testing.T) {
	for _, name := range []string{"bash", "zsh"} {
		t.Run(name, func(t *testing.T) {
			program, err := exec.LookPath(name)
			if err != nil {
				t.Skipf("%s unavailable: %v", name, err)
			}
			home, runtimeDir, tools := t.TempDir(), t.TempDir(), t.TempDir()
			for name, content := range map[string]string{"wslpath": fakeWSLPath, "getent": fakeGetent} {
				if err := os.WriteFile(filepath.Join(tools, name), []byte(content), 0o700); err != nil {
					t.Fatal(err)
				}
			}
			startDir := filepath.Join(home, "start here")
			target := filepath.Join(home, "a b%'#;$()", "caf\u00e9")
			control := filepath.Join(home, "bell\athere")
			for _, dir := range []string{startDir, target, control} {
				if err := os.MkdirAll(dir, 0o700); err != nil {
					t.Fatal(err)
				}
			}
			// /etc/profile may reset PATH, so the stand-in is put on it by the user's
			// own startup file, the way a distribution has wslpath in /usr/bin.
			profile := "printf 'user-profile-loaded\\n'\nPS1='user-prompt> '\nPATH='" + tools + "':$PATH\n"
			file := ".bash_profile"
			if name == "zsh" {
				file = ".zshrc"
			}
			if err := os.WriteFile(filepath.Join(home, file), []byte(profile), 0o600); err != nil {
				t.Fatal(err)
			}

			script := sshconn.WSLIntegrationScript("wsl-test-token", WSLDirectoryVariable)
			if script == "" {
				t.Fatal("no script")
			}
			shell, wakes := start(t, Config{
				Shell: "/bin/sh", Args: []string{"-c", wslCommandLine(script), "sh"}, Dir: home, Cols: 120, Rows: 24,
				// The Windows folder is given in the other spelling on purpose.
				Env: []string{
					"HOME=" + home, "ZDOTDIR=" + home, "SHELL=/bin/sh", "MOTTY_TEST_LOGIN_SHELL=" + program, "TMPDIR=" + runtimeDir,
					"PATH=" + tools + string(os.PathListSeparator) + os.Getenv("PATH"), "LANG=en_US.UTF-8",
					WSLDirectoryVariable + "=" + fakeWindowsDrivePath(startDir),
				},
			})
			screen := term.New(120, 24, 0)
			screen.SetPromptToken("wsl-test-token")
			ready, output := 0, ""
			screen.SetPromptReady(func() { ready++ })
			await := func(what string, done func() bool) {
				t.Helper()
				deadline := time.Now().Add(10 * time.Second)
				for time.Now().Before(deadline) {
					data := shell.Drain(nil)
					output += string(data)
					screen.Write(data)
					if done() {
						return
					}
					select {
					case <-wakes:
					case <-time.After(20 * time.Millisecond):
					}
				}
				t.Fatalf("waiting for %s: %q", what, output)
			}
			reported := func(linux string) func() bool {
				return func() bool {
					cwd, ok := screen.WorkingDirectory()
					return ok && cwd.Path == fakeWindowsPath(linux)
				}
			}
			// The shell starts in the folder the environment named, not the home
			// folder, and the user's own startup file ran.
			await("first prompt in the start folder", func() bool { return ready > 0 && reported(startDir)() })
			if !strings.Contains(output, "user-profile-loaded") {
				t.Fatalf("the user's startup file did not run: %q", output)
			}
			if strings.Contains(output, WSLDirectoryVariable) {
				t.Fatalf("the start folder variable reached the shell: %q", output)
			}

			before := ready
			shell.Write([]byte("cd -- '" + strings.ReplaceAll(target, "'", "'\\''") + "'\r"))
			await("report after cd", func() bool { return ready > before && reported(target)() })

			// A folder whose name could end the report early is not reported at
			// all, and the old report stays.
			before = ready
			shell.Write([]byte("cd -- \"$HOME/\"bell* && case $PWD in *bell*) echo in-bell-folder-$((6*7)) ;; esac\r"))
			await("prompt in the unreportable folder", func() bool { return ready > before && strings.Contains(output, "in-bell-folder-42") })
			if cwd, _ := screen.WorkingDirectory(); cwd.Path != fakeWindowsPath(target) {
				t.Fatalf("a folder with a control character was reported as %q", cwd.Path)
			}

			shell.Write([]byte("exit 7\r"))
			select {
			case <-shell.Done():
			case <-time.After(8 * time.Second):
				t.Fatal("the shell did not exit")
			}
			if shell.ExitStatus() != 7 {
				t.Fatalf("exit status = %d, want 7", shell.ExitStatus())
			}
			if files, err := os.ReadDir(runtimeDir); err != nil || len(files) != 0 {
				t.Fatalf("session files leaked: %v, %v", files, err)
			}
		})
	}
}

// fakeWindowsDrivePath spells a folder as a path on a Windows drive, which the
// script turns into a Linux path with wslpath.
func fakeWindowsDrivePath(linux string) string {
	return `C:` + strings.ReplaceAll(linux, "/", `\`)
}

func TestWSLScriptFallsBackToAPlainLoginShellForOtherShells(t *testing.T) {
	home, tools := t.TempDir(), t.TempDir()
	if err := os.WriteFile(filepath.Join(tools, "getent"), []byte(fakeGetent), 0o700); err != nil {
		t.Fatal(err)
	}
	script := sshconn.WSLIntegrationScript("wsl-test-token", WSLDirectoryVariable)
	shell, wakes := start(t, Config{
		Shell: "/bin/sh", Args: []string{"-c", wslCommandLine(script), "sh"}, Dir: home,
		Env: []string{"HOME=" + home, "SHELL=/bin/sh", "MOTTY_TEST_LOGIN_SHELL=/bin/sh", "PATH=" + tools + string(os.PathListSeparator) + os.Getenv("PATH")},
	})
	shell.Write([]byte("echo plain-shell-$((6*7))\n"))
	until(t, shell, wakes, "plain-shell-42")
	shell.Write([]byte("exit 0\n"))
	// Close is asynchronous; let the child leave before TempDir removes its home.
	select {
	case <-shell.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("the plain login shell did not exit")
	}
}

func TestWSLCommandLineFallsBackToTheUsersShellWhenItCannotBeDecoded(t *testing.T) {
	line := strings.Replace(wslCommandLine("echo never\n"), "|base64 -d", "|false", 1)
	shell, wakes := start(t, Config{Shell: "/bin/sh", Args: []string{"-c", line, "sh"}, Env: []string{"SHELL=/bin/sh"}})
	shell.Write([]byte("echo fallback-shell-$((6*7))\n"))
	until(t, shell, wakes, "fallback-shell-42")
	shell.Write([]byte("exit 0\n"))
}
