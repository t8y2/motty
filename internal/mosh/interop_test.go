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

	"motty/internal/term"
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

	// Typing character by character is where the receiver meets the sender's
	// prospective resend: two states may share a reference, and the second one
	// repeats content the first already showed. The screen must show each
	// character once.
	t.Run("typing shows each character once", func(t *testing.T) {
		client, pid := startMosh(t)
		defer killMosh(pid)

		display := term.New(80, 24, 0)
		display.Write(waitForOutput(t, client, 10*time.Second, "prompt", nil))

		// A short path keeps the command on one row, so the screen can be
		// counted without joining wrapped lines.
		runs := "/tmp/motty_mosh_typing_runs"
		os.Remove(runs)
		defer os.Remove(runs)
		typed := "echo x>>" + runs
		for _, r := range typed {
			client.SendInput([]byte(string(r)))
			pumpScreen(t, client, display, 40*time.Millisecond, "", nil)
		}
		client.SendInput([]byte("\r"))
		pumpScreen(t, client, display, 800*time.Millisecond, "", nil)

		waitForLines(t, runs, 1)
		time.Sleep(300 * time.Millisecond)
		if data, err := os.ReadFile(runs); err != nil || strings.Count(string(data), "\n") != 1 {
			t.Fatalf("command ran %q times, want 1 (%v)", data, err)
		}
		if got := screenCount(display, typed); got != 1 {
			t.Fatalf("the command line appears %d times on screen, want 1:\n%s", got, screenText(display))
		}
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

// pumpScreen drains the client into a terminal for at least d, so the screen
// the owner would show follows the client's updates. It stops early once
// marker appears when one is given.
func pumpScreen(t *testing.T, client *Client, display *term.Terminal, d time.Duration, what string, marker []byte) {
	t.Helper()
	deadline := time.Now().Add(d)
	var seen []byte
	buf := make([]byte, 8192)
	for time.Now().Before(deadline) {
		buf = client.Drain(buf[:0])
		if len(buf) > 0 {
			display.Write(buf)
			seen = append(seen, buf...)
			if marker != nil && bytes.Contains(seen, marker) {
				return
			}
		}
		select {
		case <-client.Done():
			if marker != nil {
				t.Fatalf("session ended while waiting for %s; err=%v", what, client.Err())
			}
			return
		case <-time.After(5 * time.Millisecond):
		}
	}
	if marker != nil && !bytes.Contains(seen, marker) {
		t.Fatalf("timed out waiting for %s; screen:\n%s", what, screenText(display))
	}
}

// screenText is every screen row, for a failure message.
func screenText(display *term.Terminal) string {
	_, rows := display.Size()
	var text strings.Builder
	for y := 0; y < rows; y++ {
		if line := strings.TrimRight(display.RowText(y), " "); line != "" {
			text.WriteString(line + "\n")
		}
	}
	return text.String()
}

// screenCount counts the rows whose text contains s.
func screenCount(display *term.Terminal, s string) int {
	_, rows := display.Size()
	count := 0
	for y := 0; y < rows; y++ {
		if strings.Contains(display.RowText(y), s) {
			count++
		}
	}
	return count
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
