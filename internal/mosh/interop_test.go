//go:build !windows

package mosh

import (
	"bytes"
	"context"
	"net"
	"os"
	"os/exec"
	"path/filepath"
	"regexp"
	"runtime"
	"strconv"
	"strings"
	"syscall"
	"testing"
	"time"
)

// The wire format and the shutdown handshake are only proven against the
// reference implementation. These tests start a real mosh-server on the
// loopback interface; they skip when it is not installed (the CI Linux job and
// the Linux test machine install it).
func TestAgainstRealMoshServer(t *testing.T) {
	if _, err := exec.LookPath("mosh-server"); err != nil {
		t.Skip("mosh-server is not installed")
	}

	t.Run("command, resize and close", func(t *testing.T) {
		client, pid := startMosh(t)
		defer killMosh(pid)

		waitForOutput(t, client, 10*time.Second, "prompt", nil)
		client.SendInput([]byte("echo MOTTY_MOSH_OK\r"))
		waitForOutput(t, client, 10*time.Second, "echo", []byte("MOTTY_MOSH_OK"))

		// The same keystrokes must reach the shell once. The file is the
		// execution count; screen text cannot tell an echo from a redraw.
		runs := filepath.Join(t.TempDir(), "runs")
		client.SendInput([]byte("echo run >> " + runs + "\r"))
		waitForLines(t, runs, 1)
		time.Sleep(300 * time.Millisecond)
		if data, err := os.ReadFile(runs); err != nil || strings.Count(string(data), "\n") != 1 {
			t.Fatalf("command ran %q times, want 1 (%v)", data, err)
		}

		client.Resize(100, 30)
		client.SendInput([]byte("printf 'SIZE:%s\\n' \"$(stty size)\"\r"))
		waitForOutput(t, client, 10*time.Second, "resize", []byte("30 100"))

		client.Close()
		select {
		case <-client.Done():
		case <-time.After(5 * time.Second):
			t.Fatal("client did not end its shutdown")
		}
		waitProcessGone(t, pid, 5*time.Second)
	})

	t.Run("server exit ends the session", func(t *testing.T) {
		client, pid := startMosh(t)
		defer killMosh(pid)

		waitForOutput(t, client, 10*time.Second, "prompt", nil)
		client.SendInput([]byte("exit\r"))
		select {
		case <-client.Done():
		case <-time.After(10 * time.Second):
			t.Fatal("client did not end after the shell exited")
		}
		if err := client.Err(); err != nil {
			t.Fatalf("session ended with error: %v", err)
		}
		waitProcessGone(t, pid, 5*time.Second)
	})
}

var (
	connectLine = regexp.MustCompile(`(?m)^MOSH CONNECT (\d+) ([A-Za-z0-9/+]{22})\s*$`)
	detachedPID = regexp.MustCompile(`\[mosh-server detached, pid = (\d+)\]`)
)

// startMosh runs the reference mosh-server and returns a client connected to
// it plus the server's pid, so the test can prove the session really ended.
func startMosh(t *testing.T) (*Client, int) {
	t.Helper()
	locale := "en_US.UTF-8"
	if runtime.GOOS == "linux" {
		locale = "C.UTF-8"
	}
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "mosh-server", "new", "-p", "0", "-c", "256")
	cmd.Env = append(os.Environ(), "LANG="+locale, "LC_ALL="+locale, "TERM=xterm-256color")
	output, err := cmd.CombinedOutput()
	if err != nil {
		t.Fatalf("mosh-server: %v\n%s", err, output)
	}
	connect := connectLine.FindSubmatch(output)
	if connect == nil {
		t.Fatalf("no MOSH CONNECT in mosh-server output:\n%s", output)
	}
	port, err := strconv.Atoi(string(connect[1]))
	if err != nil {
		t.Fatal(err)
	}
	key := string(connect[2])
	pid := 0
	if match := detachedPID.FindSubmatch(output); match != nil {
		pid, _ = strconv.Atoi(string(match[1]))
	}

	conn, err := net.ListenPacket("udp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	server := &net.UDPAddr{IP: net.IPv4(127, 0, 0, 1), Port: port}
	client, err := Dial(Config{
		Conn:   conn,
		Server: server,
		Key:    key,
		Cols:   80,
		Rows:   24,
	})
	if err != nil {
		conn.Close()
		t.Fatal(err)
	}
	return client, pid
}

// waitForOutput drains the client until it has seen marker, or until it has
// seen any output when marker is nil (the server produced its first screen).
func waitForOutput(t *testing.T, client *Client, timeout time.Duration, what string, marker []byte) []byte {
	t.Helper()
	deadline := time.Now().Add(timeout)
	var seen []byte
	buf := make([]byte, 4096)
	for time.Now().Before(deadline) {
		buf = client.Drain(buf[:0])
		seen = append(seen, buf...)
		if len(seen) > 64*1024 {
			seen = seen[len(seen)-64*1024:]
		}
		if marker == nil {
			if len(seen) > 0 {
				return seen
			}
		} else if bytes.Contains(seen, marker) {
			return seen
		}
		select {
		case <-client.Done():
			t.Fatalf("session ended while waiting for %s; err=%v; output=%q", what, client.Err(), seen)
		case <-time.After(20 * time.Millisecond):
		}
	}
	t.Fatalf("timed out waiting for %s; output=%q", what, seen)
	return seen
}

func waitProcessGone(t *testing.T, pid int, timeout time.Duration) {
	t.Helper()
	if pid == 0 {
		return
	}
	deadline := time.Now().Add(timeout)
	for time.Now().Before(deadline) {
		process, err := os.FindProcess(pid)
		if err != nil {
			return
		}
		if err := process.Signal(syscall.Signal(0)); err != nil {
			return // ESRCH: the server is gone
		}
		time.Sleep(50 * time.Millisecond)
	}
	t.Fatalf("mosh-server %d is still running after the session ended", pid)
}

func killMosh(pid int) {
	if pid == 0 {
		return
	}
	if process, err := os.FindProcess(pid); err == nil {
		_ = process.Kill()
	}
}

// waitForLines waits for at least want complete lines in a file the shell on the
// other side writes.
func waitForLines(t *testing.T, path string, want int) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		if data, err := os.ReadFile(path); err == nil && strings.Count(string(data), "\n") >= want {
			return
		}
		time.Sleep(20 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %d lines in %s", want, path)
}
