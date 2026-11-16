//go:build windows

package localshell

import (
	"context"
	"strings"
	"testing"
	"time"

	"motty/internal/sshconn"
	"motty/internal/term"
)

// A real distribution, where this Windows has one: the shell starts through
// wsl.exe, reports its folder as a Windows path, and follows a cd.
func TestWSLShellReportsItsFolderAsAWindowsPath(t *testing.T) {
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	choices := InstalledWSL(ctx)
	if len(choices) == 0 {
		t.Skip("no WSL distribution installed")
	}
	distro, _ := WSLDistro(choices[0].Path)
	shell, wakes := startWindows(t, Config{
		Shell: choices[0].Path, Cols: 120, Rows: 30,
		WSLScript: sshconn.WSLIntegrationScript("test-token", WSLDirectoryVariable),
		Env:       []string{WSLDirectoryVariable + "=/tmp"},
	})
	screen := term.New(120, 30, 0)
	screen.SetPromptToken("test-token")
	ready := 0
	screen.SetPromptReady(func() { ready++ })
	wait := func(what string, done func(path string) bool) {
		t.Helper()
		deadline := time.NewTimer(40 * time.Second)
		defer deadline.Stop()
		for {
			screen.Write(shell.Drain(nil))
			cwd, ok := screen.WorkingDirectory()
			if ok && ready > 0 && done(cwd.Path) {
				return
			}
			select {
			case <-wakes:
			case <-deadline.C:
				t.Fatalf("waiting for %s, folder %q; screen: %q", what, cwd.Path, screen.Text())
			}
		}
	}
	inDistro := func(linux string) func(string) bool {
		return func(path string) bool {
			path = strings.ToLower(path)
			for _, prefix := range wslUNCPrefixes {
				if path == strings.ToLower(prefix+distro)+strings.ReplaceAll(linux, "/", `\`) {
					return true
				}
			}
			return false
		}
	}
	wait("the start folder", inDistro("/tmp"))
	if !shell.Write([]byte("cd /usr/lib\r")) {
		t.Fatal("shell refused input")
	}
	wait("the folder after cd", inDistro(`/usr/lib`))
}
