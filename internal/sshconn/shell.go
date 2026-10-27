package sshconn

import (
	"errors"
	"io"
	"os"
	"strings"
	"sync"

	"golang.org/x/crypto/ssh"
	"golang.org/x/crypto/ssh/agent"

	"motty/internal/x11fwd"
)

// Limits that keep a runaway program or a huge paste from claiming memory.
const (
	// maxPendingOutput is how much output may wait for the window thread before
	// the SSH reader blocks, which pushes back on the server through TCP.
	maxPendingOutput = 1 << 20
	// maxPendingInput is how much typed or pasted input may wait to be sent.
	maxPendingInput = 4 << 20
)

// ShellConfig describes an interactive shell with a pseudo-terminal.
type ShellConfig struct {
	// Cols and Rows are the terminal size in cells.
	Cols, Rows int
	// Term is the TERM value. Empty means xterm-256color.
	Term string
	// Command starts in this PTY instead of the remote login shell when nonempty.
	Command string
	// Notify is called after output arrives and when the shell ends. It may be
	// called from any goroutine and must not block; the owner then calls Drain
	// on its own thread.
	Notify func()
	// X11, when set, asks the server to forward X11 connections of this
	// session to the display it describes. Forwarding that cannot be set up
	// never stops the shell; see Shell.X11Note.
	X11 *x11fwd.Environ
	// AgentForward, when set, asks the server to let programs in this shell
	// reach the local ssh-agent. Forwarding that cannot be set up never stops
	// the shell; see Shell.AgentForwardNote.
	AgentForward bool
}

// Shell is a remote shell on a pty. Reading is done by Drain from one thread;
// Write and Resize never block on the network, so a window thread may call
// them.
type Shell struct {
	session *ssh.Session
	notify  func()
	done    chan struct{}

	output outputBuffer
	input  inputQueue

	mu      sync.Mutex
	exitErr error

	x11    *x11fwd.Grant
	x11Err error

	agentForwardErr error
}

// OpenShell starts the user's login shell on a pty. The connection must be
// connected (see Status). It returns once the shell is running.
func (conn *Conn) OpenShell(config ShellConfig) (*Shell, error) {
	client := conn.Client()
	if client == nil {
		return nil, errors.New("sshconn: not connected")
	}
	session, err := client.NewSession()
	if err != nil {
		return nil, err
	}
	// Servers usually accept LANG; without a UTF-8 locale the shell would
	// mangle multibyte input. A refusal is harmless.
	_ = session.Setenv("LANG", utf8Locale())
	term := config.Term
	if term == "" {
		term = "xterm-256color"
	}
	modes := ssh.TerminalModes{ssh.ECHO: 1, ssh.TTY_OP_ISPEED: 38400, ssh.TTY_OP_OSPEED: 38400}
	if err := session.RequestPty(term, max(config.Rows, 1), max(config.Cols, 1), modes); err != nil {
		session.Close()
		return nil, err
	}
	shell := &Shell{session: session, notify: config.Notify, done: make(chan struct{})}
	if config.X11 != nil {
		shell.x11, shell.x11Err = conn.requestX11(session, *config.X11)
	}
	if config.AgentForward {
		// The session request names this shell only; the channel handler on
		// the client is shared by every forwarding session on the connection.
		if err := conn.forwardAgent(); err != nil {
			shell.agentForwardErr = err
		} else if err := agent.RequestAgentForwarding(session); err != nil {
			shell.agentForwardErr = err
		}
	}
	shell.output.init(shell.wake)
	shell.input.init()
	session.Stdout = outputWriter{shell}
	session.Stderr = outputWriter{shell}
	stdin, err := session.StdinPipe()
	if err != nil {
		shell.releaseX11()
		session.Close()
		return nil, err
	}
	var startErr error
	if config.Command == "" {
		startErr = session.Shell()
	} else {
		startErr = session.Start(config.Command)
	}
	if startErr != nil {
		shell.releaseX11()
		session.Close()
		return nil, startErr
	}
	go shell.sendLoop(stdin)
	go func() {
		err := session.Wait()
		shell.releaseX11()
		shell.input.close()
		shell.output.finish()
		shell.mu.Lock()
		shell.exitErr = err
		shell.mu.Unlock()
		close(shell.done)
		shell.wake()
	}()
	return shell, nil
}

func utf8Locale() string {
	for _, name := range []string{"LC_ALL", "LC_CTYPE", "LANG"} {
		if value := os.Getenv(name); strings.Contains(strings.ToLower(value), "utf-8") || strings.Contains(strings.ToLower(value), "utf8") {
			return value
		}
	}
	return "en_US.UTF-8"
}

func (shell *Shell) wake() {
	if shell.notify != nil {
		shell.notify()
	}
}

// Done is closed once the remote shell has ended and its output is complete in
// the buffer Drain reads from.
func (shell *Shell) Done() <-chan struct{} { return shell.done }

// Err is why the shell ended: nil for a clean exit, an *ssh.ExitError for a
// nonzero status, another error if the channel broke. It is only meaningful
// after Done is closed.
func (shell *Shell) Err() error {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return shell.exitErr
}

// Drain appends the output received since the last call to dst and returns it.
// It never blocks.
func (shell *Shell) Drain(dst []byte) []byte { return shell.output.drain(dst) }

// Pending reports how many bytes of output wait to be drained.
func (shell *Shell) Pending() int { return shell.output.size() }

// Write queues input for the shell. It reports false when the shell has ended
// or the input backlog is full (the data was not queued).
func (shell *Shell) Write(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	return shell.input.put(inputItem{data: append([]byte(nil), p...)})
}

// Resize tells the remote pty the new terminal size. Resizes queued behind
// each other collapse into the last one.
func (shell *Shell) Resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	shell.input.putResize(cols, rows)
}

// Close ends the session and releases its goroutines. It does not wait for the
// remote side.
func (shell *Shell) Close() {
	shell.releaseX11()
	shell.input.close()
	shell.output.finish() // a reader blocked on a full buffer must not outlive the shell
	shell.session.Close()
}

// sendLoop writes queued input and window changes to the server, in order.
func (shell *Shell) sendLoop(stdin io.WriteCloser) {
	defer stdin.Close()
	for {
		items, ok := shell.input.take()
		for _, item := range items {
			if item.data != nil {
				if _, err := stdin.Write(item.data); err != nil {
					shell.input.close()
					return
				}
				continue
			}
			if err := shell.session.WindowChange(item.rows, item.cols); err != nil {
				shell.input.close()
				return
			}
		}
		if !ok {
			return
		}
	}
}

// outputWriter feeds the session's stdout and stderr into the output buffer.
type outputWriter struct{ shell *Shell }

func (writer outputWriter) Write(p []byte) (int, error) {
	writer.shell.output.put(p)
	return len(p), nil
}

// outputBuffer holds output between the SSH reader goroutine and the window
// thread. The reader blocks while the buffer is full.
type outputBuffer struct {
	mu       sync.Mutex
	space    *sync.Cond
	data     []byte
	finished bool
	wake     func()
}

func (buffer *outputBuffer) init(wake func()) {
	buffer.space = sync.NewCond(&buffer.mu)
	buffer.wake = wake
}

func (buffer *outputBuffer) put(p []byte) {
	buffer.mu.Lock()
	for len(buffer.data) >= maxPendingOutput && !buffer.finished {
		buffer.space.Wait()
	}
	first := len(buffer.data) == 0
	buffer.data = append(buffer.data, p...)
	buffer.mu.Unlock()
	if first {
		buffer.wake() // only the first chunk after a drain needs to wake the owner
	}
}

func (buffer *outputBuffer) drain(dst []byte) []byte {
	buffer.mu.Lock()
	dst = append(dst, buffer.data...)
	if cap(buffer.data) > 4*maxPendingOutput {
		buffer.data = nil // do not hold on to a spike
	} else {
		buffer.data = buffer.data[:0]
	}
	buffer.mu.Unlock()
	buffer.space.Broadcast()
	return dst
}

func (buffer *outputBuffer) size() int {
	buffer.mu.Lock()
	defer buffer.mu.Unlock()
	return len(buffer.data)
}

// finish releases a reader that is blocked on a full buffer.
func (buffer *outputBuffer) finish() {
	buffer.mu.Lock()
	buffer.finished = true
	buffer.mu.Unlock()
	buffer.space.Broadcast()
}

// inputItem is bytes for the shell, or (when data is nil) a window change.
type inputItem struct {
	data       []byte
	cols, rows int
}

// inputQueue carries input from the window thread to the sender goroutine.
type inputQueue struct {
	mu     sync.Mutex
	ready  *sync.Cond
	items  []inputItem
	bytes  int
	closed bool
}

func (queue *inputQueue) init() { queue.ready = sync.NewCond(&queue.mu) }

func (queue *inputQueue) put(item inputItem) bool {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed || queue.bytes+len(item.data) > maxPendingInput {
		return false
	}
	queue.items = append(queue.items, item)
	queue.bytes += len(item.data)
	queue.ready.Signal()
	return true
}

func (queue *inputQueue) putResize(cols, rows int) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	if queue.closed {
		return
	}
	if last := len(queue.items) - 1; last >= 0 && queue.items[last].data == nil {
		queue.items[last] = inputItem{cols: cols, rows: rows}
	} else {
		queue.items = append(queue.items, inputItem{cols: cols, rows: rows})
	}
	queue.ready.Signal()
}

// take waits for input and returns it; ok is false once the queue is closed
// and empty.
func (queue *inputQueue) take() (items []inputItem, ok bool) {
	queue.mu.Lock()
	defer queue.mu.Unlock()
	for len(queue.items) == 0 && !queue.closed {
		queue.ready.Wait()
	}
	items, queue.items, queue.bytes = queue.items, nil, 0
	return items, !(queue.closed && len(items) == 0)
}

func (queue *inputQueue) close() {
	queue.mu.Lock()
	queue.closed = true
	queue.mu.Unlock()
	queue.ready.Broadcast()
}
