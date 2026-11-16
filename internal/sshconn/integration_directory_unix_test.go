//go:build !windows

package sshconn

import (
	"os"
	"os/exec"
	"path/filepath"
	"testing"
	"time"

	"motty/internal/localshell"
	"motty/internal/term"
)

// startIntegratedIn starts the integration command the way a server runs it, in
// a home directory of its own, and returns where the shell first reports being.
func startIntegratedIn(t *testing.T, program, home, directory string) (string, bool) {
	t.Helper()
	shell, err := localshell.Start(localshell.Config{
		Shell: "/bin/sh", Args: []string{"-c", integrationCommandIn(program, directory, "dir-test-token")}, Dir: home,
		Env:  []string{"HOME=" + home, "ZDOTDIR=" + home, "SHELL=" + program, "TMPDIR=" + t.TempDir(), "LANG=en_US.UTF-8"},
		Cols: 120, Rows: 24,
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	screen := term.New(120, 24, 0)
	screen.SetPromptToken("dir-test-token")
	ready := false
	screen.SetPromptReady(func() { ready = true })
	deadline := time.Now().Add(8 * time.Second)
	for time.Now().Before(deadline) {
		screen.Write(shell.Drain(nil))
		if cwd, ok := screen.WorkingDirectory(); ok && ready {
			return cwd.Path, true
		}
		time.Sleep(5 * time.Millisecond)
	}
	return "", false
}

// The directory a split pane starts in travels as an argument, so a name made
// of quotes, spaces and expansions is still only a name, and a directory the
// server no longer has leaves the shell in its home.
func TestIntegrationStartsInTheDirectoryGiven(t *testing.T) {
	program, err := exec.LookPath("bash")
	if err != nil {
		t.Skip("bash unavailable")
	}
	// A shell that starts in a directory reports its real path, which differs from
	// a symlinked temporary directory such as macOS's /var.
	home, err := filepath.EvalSymlinks(t.TempDir())
	if err != nil {
		t.Fatal(err)
	}
	awkward := filepath.Join(home, "it's $(touch pwned) `x` \"q\" dir")
	if err := os.Mkdir(awkward, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, testCase := range []struct{ name, directory, want string }{
		{"awkward name", awkward, awkward},
		{"missing directory", filepath.Join(home, "gone"), home},
		{"relative path", "relative", home},
		{"none", "", home},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			got, ok := startIntegratedIn(t, program, home, testCase.directory)
			if !ok || got != testCase.want {
				t.Fatalf("shell reported %q (ok %v), want %q", got, ok, testCase.want)
			}
		})
	}
	if _, err := os.Stat(filepath.Join(home, "pwned")); err == nil {
		t.Fatal("the directory name was run as code")
	}
}
