//go:build !windows

package capture

import (
	"errors"
	"fmt"
	"os"
	"os/exec"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/creack/pty"

	"motty/internal/shellkind"
	"motty/internal/term"
)

// pwshSession is a real interactive PowerShell on a pty, with Motty's own
// terminal emulator in the middle: PSReadLine redraws the line and asks the
// terminal where the cursor is, so a fixed script of answers would not behave
// as a user's terminal does. Commands run one after the other in one session,
// as they do in a terminal tab.
type pwshSession struct {
	t      *testing.T
	pty    *os.File
	screen *term.Terminal
	mu     sync.Mutex
	data   []byte
}

func startPwsh(t *testing.T, cols int) *pwshSession {
	t.Helper()
	path, err := exec.LookPath("pwsh")
	if err != nil {
		t.Skip("pwsh not installed")
	}
	cmd := exec.Command(path, "-NoLogo", "-NoProfile", "-NoExit", "-Command", "function global:prompt { 'PSX> ' }")
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "NO_COLOR=1")
	cmd.Dir = t.TempDir()
	master, err := pty.StartWithSize(cmd, &pty.Winsize{Cols: uint16(cols), Rows: 40})
	if err != nil {
		t.Skipf("no pty: %v", err)
	}
	s := &pwshSession{t: t, pty: master, screen: term.New(cols, 40, 1000)}
	s.screen.SetReply(func(p []byte) { _, _ = master.Write(p) })
	go func() {
		buf := make([]byte, 4096)
		for {
			n, err := master.Read(buf)
			if n > 0 {
				s.mu.Lock()
				s.data = append(s.data, buf[:n]...)
				s.mu.Unlock()
			}
			if err != nil {
				return
			}
		}
	}()
	t.Cleanup(func() {
		_ = cmd.Process.Kill()
		_, _ = cmd.Process.Wait()
		master.Close()
	})
	s.waitForPrompt(30 * time.Second)
	return s
}

func (s *pwshSession) take() []byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	out := s.data
	s.data = nil
	return out
}

// waitForPrompt reads until the screen ends in a PowerShell prompt.
func (s *pwshSession) waitForPrompt(limit time.Duration) {
	s.t.Helper()
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		if chunk := s.take(); len(chunk) > 0 {
			_, _ = s.screen.Write(chunk)
		}
		lines := strings.Split(strings.TrimRight(s.screen.Text(), " \n"), "\n")
		if last := strings.TrimSpace(lines[len(lines)-1]); last == "PSX>" {
			return
		}
	}
	s.t.Fatalf("no prompt; screen:\n%s", s.screen.Text())
}

// run types one wrapped command and feeds the shell's output through a Tap, and
// what the Tap lets through to the terminal, until the marker arrives.
func (s *pwshSession) run(command string, limit time.Duration) (Result, string, error) {
	s.t.Helper()
	nonce := NewNonce()
	wrapped, err := WrapFor(shellkind.PowerShell, command, nonce)
	if err != nil {
		s.t.Fatal(err)
	}
	tp := NewTap(nonce, 1<<16)
	tp.SetEcho(wrapped)
	start := s.screen.Text()
	if _, err := s.pty.Write(wrapped); err != nil {
		s.t.Fatal(err)
	}
	var raw strings.Builder
	for deadline := time.Now().Add(limit); time.Now().Before(deadline); time.Sleep(5 * time.Millisecond) {
		chunk := s.take()
		if len(chunk) == 0 {
			continue
		}
		raw.Write(chunk)
		display, done := tp.Feed(chunk)
		_, _ = s.screen.Write(display)
		if done {
			s.waitForPrompt(10 * time.Second)
			return tp.Result(), strings.TrimPrefix(s.screen.Text(), start), nil
		}
	}
	return tp.Result(), s.screen.Text(), errors.New("no marker; raw output " + raw.String())
}

func TestPowerShellIntegration(t *testing.T) {
	// PSReadLine redraws a line that wraps differently at every width.
	for _, cols := range []int{120, 60, 30} {
		t.Run(fmt.Sprintf("%d columns", cols), func(t *testing.T) { testPowerShell(t, startPwsh(t, cols)) })
	}
}

func testPowerShell(t *testing.T, s *pwshSession) {
	t.Helper()
	tests := []struct {
		name, cmd, output string
		code              int
	}{
		{"echo", "Write-Output hi", "hi", 0},
		{"native success", "uname -s | Out-Null; Write-Output done", "done", 0},
		{"native exit code", "sh -c 'exit 7'", "", 7},
		{"stale native code is cleared", "Write-Output ok", "ok", 0},
		{"failed cmdlet", "Get-Item /no/such/motty/path 2>$null", "", 1},
		{"multi-line", "Write-Output a\nWrite-Output b", "a\nb", 0},
		{"multi-line ends failing", "Write-Output a\nsh -c 'exit 3'", "a", 3},
		{"trailing comment", "Write-Output hi # a comment", "hi", 0},
		{"comment only line", "Write-Output x\n# done", "x", 0},
		{"pipeline", "'b','a' | Sort-Object", "a\nb", 0},
		{"here-string", "@'\nline1\nline2\n'@", "line1\nline2", 0},
		{"script block", "foreach ($i in 1..3) {\n  Write-Output $i\n}", "1\n2\n3", 0},
		{"trailing backslash is a path", `Write-Output C:\`, `C:\`, 0},
		{"trailing backtick continuation is dropped", "Write-Output hi `", "hi", 0},
		{"no trailing newline output", "[Console]::Out.Write('abc')", "abc", 0},
		{"empty", "", "", 0},
		{"variables persist", "$motty = 41", "", 0},
		{"variables persist across commands", "$motty + 1", "42", 0},
		{"terminating error still completes", "throw 'boom'", "~boom", 1},
		{"unicode output", "Write-Output 'héllo ✓'", "héllo ✓", 0},
		{"native high status", "sh -c 'exit 255'", "", 255},
	}
	for _, tc := range tests {
		r, shown, err := s.run(tc.cmd, 30*time.Second)
		if err != nil {
			t.Fatalf("%s: %v; screen:\n%s", tc.name, err, shown)
		}
		// A leading "~" means the output only has to contain the text: how
		// PowerShell words an error differs between its versions.
		matches := r.Output == tc.output
		if text, ok := strings.CutPrefix(tc.output, "~"); ok {
			matches = strings.Contains(r.Output, text)
		}
		if !matches || r.ExitCode != tc.code || !r.Done || r.Truncated {
			t.Errorf("%s: got %+v; want output %q code %d\nscreen:\n%s", tc.name, r, tc.output, tc.code, shown)
		}
	}
}
