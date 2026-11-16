//go:build windows

package localshell

import (
	"strings"
	"testing"
	"time"
)

func startWindows(t *testing.T, config Config) (*Shell, chan struct{}) {
	t.Helper()
	wakes := make(chan struct{}, 1)
	config.Notify = func() {
		select {
		case wakes <- struct{}{}:
		default:
		}
	}
	if config.Shell == "" {
		config.Shell = "cmd.exe" // present on every Windows, unlike PowerShell 7
	}
	if config.Cols == 0 {
		config.Cols, config.Rows = 80, 24
	}
	shell, err := Start(config)
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(shell.Close)
	return shell, wakes
}

func untilWindows(t *testing.T, shell *Shell, wakes chan struct{}, want string) string {
	t.Helper()
	var output []byte
	deadline := time.After(15 * time.Second)
	for {
		output = shell.Drain(output)
		if strings.Contains(string(output), want) {
			return string(output)
		}
		select {
		case <-wakes:
		case <-deadline:
			t.Fatalf("waiting for %q; output so far %q", want, output)
		}
	}
}

func TestConPTYRunsCommandsAndEchoesTheirOutput(t *testing.T) {
	shell, wakes := startWindows(t, Config{})
	if !shell.Write([]byte("echo conpty-%OS%\r\n")) {
		t.Fatal("Write refused")
	}
	untilWindows(t, shell, wakes, "conpty-Windows_NT")
}

func TestConPTYExitEndsTheShellWithItsStatus(t *testing.T) {
	shell, _ := startWindows(t, Config{})
	shell.Write([]byte("exit 3\r\n"))
	select {
	case <-shell.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("the shell did not end")
	}
	if shell.ExitStatus() != 3 {
		t.Fatalf("status = %d (%v), want 3", shell.ExitStatus(), shell.Err())
	}
	if shell.Write([]byte("x")) {
		t.Fatal("Write succeeded after the shell ended")
	}
}

func TestConPTYCloseEndsARunningProgram(t *testing.T) {
	shell, wakes := startWindows(t, Config{})
	shell.Write([]byte("ping -n 60 127.0.0.1\r\n"))
	untilWindows(t, shell, wakes, "TTL=")
	shell.Close()
	select {
	case <-shell.Done():
	case <-time.After(15 * time.Second):
		t.Fatal("closing did not end the shell")
	}
}

func TestConPTYStartsInTheRequestedDirectory(t *testing.T) {
	dir := t.TempDir()
	shell, wakes := startWindows(t, Config{Dir: dir})
	shell.Write([]byte("cd\r\n"))
	untilWindows(t, shell, wakes, dir)
}

func TestForegroundArgsNamesTheRunningCommand(t *testing.T) {
	shell, _ := startWindows(t, Config{})
	if !shell.Write([]byte("ping -n 30 127.0.0.1\r\n")) {
		t.Fatal("Write refused")
	}
	deadline := time.Now().Add(10 * time.Second)
	for time.Now().Before(deadline) {
		args, ok := shell.ForegroundArgs()
		for _, arg := range args {
			if ok && strings.Contains(strings.ToLower(arg), "ping") {
				return
			}
		}
		time.Sleep(50 * time.Millisecond)
	}
	args, ok := shell.ForegroundArgs()
	t.Fatalf("foreground = %q (ok %v), want ping", args, ok)
}

func TestForegroundArgsIsTheShellAtItsPrompt(t *testing.T) {
	shell, wakes := startWindows(t, Config{})
	shell.Write([]byte("echo ready\r\n"))
	untilWindows(t, shell, wakes, "ready")
	args, ok := shell.ForegroundArgs()
	if !ok || len(args) == 0 || !strings.Contains(strings.ToLower(args[0]), "cmd") {
		t.Fatalf("foreground = %q (ok %v), want cmd", args, ok)
	}
}
