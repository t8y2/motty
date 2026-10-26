// Package localshell runs the user's login shell on a pseudo-terminal of this
// machine. It has the shape of a remote shell (see sshconn.Shell): output is
// drained by the window thread, input is queued without blocking it, and the
// owner is woken when something arrives. The pseudo-terminal itself is a small
// per-platform backend: a pty on Unix, ConPTY on Windows.
package localshell

import (
	"errors"
	"fmt"
	"github.com/vivaui/viva/leaks"
	"io"
	"sync"
	"time"
)

const (
	// maxPendingOutput is how much output may wait for the window thread before
	// the reader pauses, which in turn pauses the program through the pty.
	maxPendingOutput = 1 << 20
	// maxPendingInput is how much typed or pasted input may wait to be written.
	maxPendingInput = 4 << 20
)

// Config describes the shell to start.
type Config struct {
	// Cols and Rows are the terminal size in cells.
	Cols, Rows int
	// Shell is the program to run. Empty means the platform's default shell.
	Shell string
	// Dir is the working directory. Empty means the user's home directory.
	Dir string
	// Env is added to the environment the shell inherits.
	Env []string
	// Notify is called after output arrives and when the shell ends, from any
	// goroutine; it must not block.
	Notify func()
}

// backend is one platform's pseudo-terminal and the process on it.
type backend struct {
	// io is the terminal's master side: read what the program prints, write what
	// the user types.
	io io.ReadWriteCloser
	// resize tells the program the terminal size in cells.
	resize func(cols, rows int)
	// wait blocks until the program has ended and returns its exit status.
	wait func() (status int, err error)
	// hangup asks the program to end; kill makes sure it does.
	hangup, kill func()
}

// ExitError is why a shell ended with a nonzero status. It has the same
// ExitStatus method as an SSH exit error, so both are reported alike.
type ExitError struct{ Code int }

func (err *ExitError) Error() string   { return fmt.Sprintf("shell exited with status %d", err.Code) }
func (err *ExitError) ExitStatus() int { return err.Code }

// Shell is a running local shell.
type Shell struct {
	b      backend
	notify func()
	done   chan struct{}
	// readDone is closed when the reader has seen the end of the terminal.
	readDone chan struct{}

	mu       sync.Mutex
	space    *sync.Cond
	data     []byte
	finished bool
	exitErr  error

	input   chan []byte
	pending int
	closed  bool
}

// liveShells counts shells that were started and not yet closed: each holds a
// pty and three goroutines until Close.
var liveShells = leaks.New("local shells")

// Start launches the shell. It returns once the process is running.
func Start(config Config) (*Shell, error) {
	b, err := startBackend(config)
	if err != nil {
		return nil, err
	}
	liveShells.Acquire()
	shell := &Shell{b: b, notify: config.Notify, done: make(chan struct{}), readDone: make(chan struct{}), input: make(chan []byte, 256)}
	shell.space = sync.NewCond(&shell.mu)
	go shell.readLoop()
	go shell.writeLoop()
	go shell.wait()
	return shell, nil
}

func (shell *Shell) wake() {
	if shell.notify != nil {
		shell.notify()
	}
}

// readLoop moves the program's output into the buffer. It blocks while the
// buffer is full, which stops reading the pty and so slows the program.
func (shell *Shell) readLoop() {
	defer close(shell.readDone)
	buffer := make([]byte, 32<<10)
	for {
		n, err := shell.b.io.Read(buffer)
		if n > 0 {
			shell.mu.Lock()
			for len(shell.data) >= maxPendingOutput && !shell.finished {
				shell.space.Wait()
			}
			first := len(shell.data) == 0
			shell.data = append(shell.data, buffer[:n]...)
			shell.mu.Unlock()
			if first {
				shell.wake() // only the first chunk after a drain needs to wake the owner
			}
		}
		if err != nil {
			return
		}
	}
}

func (shell *Shell) writeLoop() {
	for data := range shell.input {
		shell.mu.Lock()
		shell.pending -= len(data)
		shell.mu.Unlock()
		if _, err := shell.b.io.Write(data); err != nil {
			return
		}
	}
}

func (shell *Shell) wait() {
	status, err := shell.b.wait()
	if err == nil && status != 0 {
		err = &ExitError{Code: status}
	}
	// The reader sees the end of the terminal once the last holder is gone; give
	// it time to deliver what the shell printed on its way out. A program that
	// left a child behind still holding the terminal does not keep us waiting.
	select {
	case <-shell.readDone:
	case <-time.After(readDrainGrace):
	}
	shell.mu.Lock()
	shell.exitErr = err
	shell.finished = true
	shell.mu.Unlock()
	shell.space.Broadcast()
	close(shell.done)
	shell.wake()
}

// Drain appends the output received since the last call to dst. It never blocks.
func (shell *Shell) Drain(dst []byte) []byte {
	shell.mu.Lock()
	dst = append(dst, shell.data...)
	if cap(shell.data) > 4*maxPendingOutput {
		shell.data = nil
	} else {
		shell.data = shell.data[:0]
	}
	shell.mu.Unlock()
	shell.space.Broadcast()
	return dst
}

// Write queues input for the program. It reports false when the shell has ended
// or the backlog is full; it never blocks the caller.
func (shell *Shell) Write(p []byte) bool {
	if len(p) == 0 {
		return true
	}
	shell.mu.Lock()
	defer shell.mu.Unlock()
	if shell.closed || shell.finished || shell.pending+len(p) > maxPendingInput {
		return false
	}
	select {
	case shell.input <- append([]byte(nil), p...):
		shell.pending += len(p)
		return true
	default:
		return false
	}
}

// Resize tells the program the terminal size in cells.
func (shell *Shell) Resize(cols, rows int) {
	if cols < 1 || rows < 1 {
		return
	}
	shell.b.resize(cols, rows)
}

// Done is closed once the shell has ended and its output is complete.
func (shell *Shell) Done() <-chan struct{} { return shell.done }

// Err is why the shell ended: nil for a clean exit or a hangup, an *ExitError for
// a nonzero status. Only meaningful after Done is closed.
func (shell *Shell) Err() error {
	shell.mu.Lock()
	defer shell.mu.Unlock()
	return shell.exitErr
}

// ExitStatus is the exit code of a shell that ended with one.
func (shell *Shell) ExitStatus() int {
	var exit *ExitError
	if errors.As(shell.Err(), &exit) {
		return exit.Code
	}
	return 0
}

// Close hangs the shell up and releases its goroutines. It does not wait.
func (shell *Shell) Close() {
	shell.mu.Lock()
	if shell.closed {
		shell.mu.Unlock()
		return
	}
	shell.closed = true
	liveShells.Release()
	close(shell.input)
	shell.mu.Unlock()
	shell.b.hangup()
	timer := time.AfterFunc(killGrace, shell.b.kill)
	go func() {
		<-shell.done
		timer.Stop()
	}()
	_ = shell.b.io.Close()
	shell.mu.Lock()
	shell.finished = true
	shell.mu.Unlock()
	shell.space.Broadcast()
}

// killGrace is how long a shell gets to leave after a hangup before it is killed.
const killGrace = 500 * time.Millisecond

// readDrainGrace is the longest the end of a shell waits for its last output.
const readDrainGrace = 300 * time.Millisecond
