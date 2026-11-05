//go:build !windows

package localshell

import (
	"errors"
	"os"
	"os/exec"
	"syscall"

	"github.com/creack/pty"
)

// defaultShell is the user's login shell, else sh.
func defaultShell() string {
	if program := os.Getenv("SHELL"); program != "" {
		return program
	}
	return "/bin/sh"
}

func startBackend(config Config) (backend, error) {
	program := config.Shell
	if program == "" {
		program = defaultShell()
	}
	args := config.Args
	if args == nil {
		args = []string{"-l"}
	}
	cmd := exec.Command(program, args...)
	cmd.Dir = config.Dir
	if cmd.Dir == "" {
		if home, err := os.UserHomeDir(); err == nil {
			cmd.Dir = home
		}
	}
	cmd.Env = append(os.Environ(), "TERM=xterm-256color", "COLORTERM=truecolor")
	cmd.Env = append(cmd.Env, config.Env...)
	cmd.Env = withUTF8Locale(cmd.Env)
	size := &pty.Winsize{Cols: uint16(max(config.Cols, 1)), Rows: uint16(max(config.Rows, 1))}
	master, err := pty.StartWithSize(cmd, size)
	if err != nil {
		return backend{}, err
	}
	return backend{
		io: master,
		resize: func(cols, rows int) {
			_ = pty.Setsize(master, &pty.Winsize{Cols: uint16(cols), Rows: uint16(rows)})
		},
		wait: func() (int, error) {
			err := cmd.Wait()
			var exit *exec.ExitError
			if errors.As(err, &exit) {
				if code := exit.ExitCode(); code > 0 {
					return code, nil
				}
				return 0, nil // ended by our own hangup or a signal: not a failure to report
			}
			return 0, err
		},
		hangup: func() { _ = cmd.Process.Signal(syscall.SIGHUP) },
		kill:   func() { _ = cmd.Process.Kill() },
	}, nil
}
