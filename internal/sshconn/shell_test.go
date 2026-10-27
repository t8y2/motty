package sshconn

import (
	"bytes"
	"errors"
	"io"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// connect dials a test server and waits for the connection to be usable.
func connect(t *testing.T, server *testServer) *harness {
	t.Helper()
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	h.conn.TrustHostKey(false)
	h.phase(PhasePasswordPrompt)
	h.conn.SubmitPassword("secret")
	h.phase(PhaseConnected)
	return h
}

// shellOutput collects a shell's output until want is present or time runs out.
type shellWatcher struct {
	t       *testing.T
	shell   *Shell
	wakes   chan struct{}
	buffer  []byte
	scratch []byte
}

func openShell(t *testing.T, h *harness, cols, rows int) *shellWatcher {
	t.Helper()
	return openShellWith(t, h, ShellConfig{Cols: cols, Rows: rows})
}

func openShellWith(t *testing.T, h *harness, config ShellConfig) *shellWatcher {
	t.Helper()
	watcher := &shellWatcher{t: t, wakes: make(chan struct{}, 1)}
	config.Notify = func() {
		select {
		case watcher.wakes <- struct{}{}:
		default:
		}
	}
	shell, err := h.conn.OpenShell(config)
	if err != nil {
		t.Fatal(err)
	}
	watcher.shell = shell
	t.Cleanup(shell.Close)
	return watcher
}

func (w *shellWatcher) until(description string, ready func(output string) bool) string {
	w.t.Helper()
	deadline := time.After(10 * time.Second)
	for {
		w.buffer = w.shell.Drain(w.buffer)
		if ready(string(w.buffer)) {
			return string(w.buffer)
		}
		select {
		case <-w.wakes:
		case <-deadline:
			w.t.Fatalf("timed out waiting for %s; output so far %q", description, w.buffer)
		}
	}
}

func (w *shellWatcher) waitDone() {
	w.t.Helper()
	select {
	case <-w.shell.Done():
	case <-time.After(10 * time.Second):
		w.t.Fatal("the shell did not end")
	}
}

// echoShell greets, then echoes input in upper case until it reads "exit\r".
func echoShell(shell *fakeShell) {
	shell.Channel.Write([]byte("welcome\r\n$ "))
	buffer := make([]byte, 256)
	for {
		n, err := shell.Channel.Read(buffer)
		if err != nil {
			return
		}
		text := string(buffer[:n])
		if strings.Contains(text, "exit\r") {
			shell.exit(0)
			return
		}
		shell.Channel.Write([]byte(strings.ToUpper(text)))
	}
}

func passwordServer(t *testing.T, shell func(*fakeShell)) *testServer {
	return startServer(t, func(server *testServer) {
		server.password = "secret"
		server.shell = shell
	})
}

func TestShellRunsOnAPtyAndCarriesDataBothWays(t *testing.T) {
	server := passwordServer(t, echoShell)
	h := connect(t, server)
	w := openShell(t, h, 80, 24)

	w.until("the greeting", func(output string) bool { return strings.Contains(output, "welcome\r\n$ ") })
	term, cols, rows, env, _ := fakeShellState(server)
	if term != "xterm-256color" || cols != 80 || rows != 24 {
		t.Errorf("pty = %q %dx%d, want xterm-256color 80x24", term, cols, rows)
	}
	if !strings.Contains(strings.ToLower(env["LANG"]), "utf") {
		t.Errorf("LANG = %q, want a UTF-8 locale", env["LANG"])
	}

	if !w.shell.Write([]byte("ls\r")) {
		t.Fatal("Write refused")
	}
	w.until("the echo", func(output string) bool { return strings.HasSuffix(output, "LS\r") })

	w.shell.Resize(132, 43)
	w.shell.Resize(100, 30) // queued behind the first: only the last needs to arrive
	deadline := time.Now().Add(10 * time.Second)
	for {
		_, _, _, _, resizes := fakeShellState(server)
		if len(resizes) > 0 && resizes[len(resizes)-1] == [2]int{100, 30} {
			break
		}
		if time.Now().After(deadline) {
			t.Fatalf("window changes = %v, want ending in 100x30", resizes)
		}
		time.Sleep(5 * time.Millisecond)
	}

	w.shell.Write([]byte("exit\r"))
	w.waitDone()
	if err := w.shell.Err(); err != nil {
		t.Errorf("clean exit reported %v", err)
	}
	if w.shell.Write([]byte("x")) {
		t.Error("Write succeeded after the shell ended")
	}
}

func TestCommandShellUsesItsOwnPTYAndCarriesInteractiveInput(t *testing.T) {
	const command = "docker exec -it 0f1e2d3c4b5a /bin/sh"
	seen := make(chan string, 1)
	server := passwordServer(t, nil)
	server.command = func(got string, channel ssh.Channel) {
		seen <- got
		_, _ = channel.Write([]byte("inside$ "))
		buffer := make([]byte, 64)
		n, _ := channel.Read(buffer)
		_, _ = channel.Write(buffer[:n])
		_ = channel.Close()
	}
	h := connect(t, server)
	w := openShellWith(t, h, ShellConfig{Cols: 92, Rows: 30, Command: command})
	w.until("container prompt", func(output string) bool { return strings.Contains(output, "inside$ ") })
	if got := <-seen; got != command {
		t.Fatalf("executed %q, want %q", got, command)
	}
	term, cols, rows, env, _ := fakeShellState(server)
	if term != "xterm-256color" || cols != 92 || rows != 30 || !strings.Contains(strings.ToLower(env["LANG"]), "utf") {
		t.Fatalf("PTY=%s %dx%d environment=%v", term, cols, rows, env)
	}
	if !w.shell.Write([]byte("echo ok\r")) {
		t.Fatal("command PTY rejected input")
	}
	w.until("interactive echo", func(output string) bool { return strings.Contains(output, "echo ok\r") })
}

// fakeShellState reads the single shell session the server is running.
func fakeShellState(server *testServer) (term string, cols, rows int, env map[string]string, resizes [][2]int) {
	server.mu.Lock()
	shell := server.lastShell
	server.mu.Unlock()
	if shell == nil {
		return "", 0, 0, nil, nil
	}
	return shell.state()
}

func TestShellReportsTheExitStatus(t *testing.T) {
	server := passwordServer(t, func(shell *fakeShell) {
		shell.Channel.Write([]byte("bye\r\n"))
		shell.exit(3)
	})
	h := connect(t, server)
	w := openShell(t, h, 80, 24)
	w.waitDone()
	var exit *ssh.ExitError
	if err := w.shell.Err(); !errors.As(err, &exit) || exit.ExitStatus() != 3 {
		t.Fatalf("Err = %v, want exit status 3", err)
	}
	if output := w.until("the farewell", func(output string) bool { return strings.Contains(output, "bye") }); !strings.Contains(output, "bye\r\n") {
		t.Fatalf("output lost at exit: %q", output)
	}
}

func TestShellOutputIsBoundedAndNothingIsLost(t *testing.T) {
	const total = 8 << 20
	server := passwordServer(t, func(shell *fakeShell) {
		chunk := bytes.Repeat([]byte("0123456789abcdef"), 1024) // 16 KiB
		for sent := 0; sent < total; sent += len(chunk) {
			if _, err := shell.Channel.Write(chunk); err != nil {
				return
			}
		}
		shell.exit(0)
	})
	h := connect(t, server)
	w := openShell(t, h, 80, 24)

	received, peak := 0, 0
	deadline := time.After(30 * time.Second)
	for received < total {
		if pending := w.shell.Pending(); pending > peak {
			peak = pending
		}
		w.scratch = w.shell.Drain(w.scratch[:0])
		received += len(w.scratch)
		if len(w.scratch) == 0 {
			select {
			case <-w.wakes:
			case <-deadline:
				t.Fatalf("stalled after %d of %d bytes", received, total)
			}
		}
		time.Sleep(time.Millisecond) // a slow consumer
	}
	if received != total {
		t.Fatalf("received %d bytes, want %d", received, total)
	}
	if peak > maxPendingOutput+64*1024 {
		t.Fatalf("buffered up to %d bytes with a slow reader; the limit is %d", peak, maxPendingOutput)
	}
}

func TestClosingAShellWhoseOutputIsNeverReadReleasesItsGoroutines(t *testing.T) {
	var written atomic.Int64
	server := passwordServer(t, func(shell *fakeShell) {
		chunk := bytes.Repeat([]byte("x"), 32*1024)
		for {
			n, err := shell.Channel.Write(chunk)
			written.Add(int64(n))
			if err != nil {
				return
			}
		}
	})
	h := connect(t, server)
	w := openShell(t, h, 80, 24)
	// Nobody drains: the reader fills the buffer and blocks.
	deadline := time.Now().Add(10 * time.Second)
	for w.shell.Pending() < maxPendingOutput {
		if time.Now().After(deadline) {
			t.Fatalf("the buffer only reached %d bytes", w.shell.Pending())
		}
		time.Sleep(time.Millisecond)
	}
	w.shell.Close()
	w.waitDone()
}

func TestInputQueueIsBoundedAndKeepsOrder(t *testing.T) {
	var queue inputQueue
	queue.init()
	if !queue.put(inputItem{data: []byte("a")}) {
		t.Fatal("first put refused")
	}
	queue.putResize(10, 10)
	queue.putResize(20, 20)
	queue.put(inputItem{data: []byte("b")})
	items, ok := queue.take()
	if !ok || len(items) != 3 || string(items[0].data) != "a" || items[1].cols != 20 || string(items[2].data) != "b" {
		t.Fatalf("items = %+v ok=%v, want a, resize 20, b", items, ok)
	}
	if queue.put(inputItem{data: make([]byte, maxPendingInput+1)}) {
		t.Fatal("an input larger than the backlog limit was queued")
	}
	queue.close()
	if queue.put(inputItem{data: []byte("late")}) {
		t.Fatal("put succeeded on a closed queue")
	}
	if _, ok := queue.take(); ok {
		t.Fatal("take on a closed empty queue reported more")
	}
}

type dispatchProbe struct {
	wrote chan struct{}
	done  chan struct{}
}

func (probe *dispatchProbe) Write(p []byte) (int, error) {
	probe.wrote <- struct{}{}
	return len(p), nil
}

func (probe *dispatchProbe) Close() error {
	close(probe.done)
	return nil
}

// BenchmarkShellInputDispatch measures the application-side asynchronous hop
// from accepting one typed byte until the SSH writer goroutine receives it.
// It deliberately excludes network RTT and remote PTY echo.
func BenchmarkShellInputDispatch(b *testing.B) {
	shell := &Shell{}
	shell.input.init()
	probe := &dispatchProbe{wrote: make(chan struct{}), done: make(chan struct{})}
	go shell.sendLoop(probe)
	b.Cleanup(func() {
		shell.input.close()
		<-probe.done
	})
	b.ReportAllocs()
	for b.Loop() {
		if !shell.Write([]byte{'x'}) {
			b.Fatal("input was refused")
		}
		<-probe.wrote
	}
}

func TestOpenShellNeedsAConnectedConn(t *testing.T) {
	server := passwordServer(t, echoShell)
	h := dial(t, server, nil)
	h.phase(PhaseHostKeyPrompt)
	if _, err := h.conn.OpenShell(ShellConfig{Cols: 80, Rows: 24}); err == nil {
		t.Fatal("OpenShell succeeded before the connection was ready")
	}
}

var _ = io.EOF
