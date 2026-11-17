package sshconn

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"sync"

	"golang.org/x/crypto/ssh"
)

// ErrCommandOutputTooLarge reports that a remote command wrote more stdout
// than its caller agreed to retain.
var ErrCommandOutputTooLarge = errors.New("sshconn: command output too large")

// RunCommand runs one command on the authenticated server and returns its
// stdout. maxOutputBytes bounds retained output; extra bytes are discarded and
// reported as ErrCommandOutputTooLarge when the command ends. Canceling ctx
// closes only this command session, not the SSH connection.
func (conn *Conn) RunCommand(ctx context.Context, command string, maxOutputBytes int) ([]byte, error) {
	data, exceeded, err := conn.runCommand(ctx, command, maxOutputBytes, false)
	if exceeded {
		return data, fmt.Errorf("%w: limit %d bytes", ErrCommandOutputTooLarge, maxOutputBytes)
	}
	if errors.Is(err, ErrCanceled) || ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil, err
	}
	return data, err
}

// RunCommandInput runs one command with input on its stdin, and returns its
// stdout. The payload travels over the session channel rather than a pty, so
// it is not bounded by the terminal input buffer. maxOutputBytes bounds
// retained output as in RunCommand.
func (conn *Conn) RunCommandInput(ctx context.Context, command string, input []byte, maxOutputBytes int) ([]byte, error) {
	if err := commandLimit(maxOutputBytes); err != nil {
		return nil, err
	}
	output := &commandOutput{limit: maxOutputBytes}
	err := conn.captureCommand(ctx, command, input, output, nil)
	data, exceeded := output.result()
	if exceeded {
		return data, fmt.Errorf("%w: limit %d bytes", ErrCommandOutputTooLarge, maxOutputBytes)
	}
	if errors.Is(err, ErrCanceled) || ctx.Err() != nil && errors.Is(err, ctx.Err()) {
		return nil, err
	}
	return data, err
}

// ExecResult is how a command run by Exec ended.
type ExecResult struct {
	// Output is what the command wrote to stdout and stderr, in the order the
	// server delivered it, cut at the limit given to Exec.
	Output []byte
	Stdout []byte
	Stderr []byte
	// Truncated reports that the command wrote more than the limit.
	Truncated bool
	// ExitCode is the remote exit status. It is meaningful only when Exec
	// returned a nil error.
	ExitCode int
}

// Exec runs one command on the authenticated server the way a script would:
// there is no terminal, and nothing of the shell persists between calls. It
// returns the command's combined stdout and stderr, bounded by maxOutputBytes,
// and its exit status; a command that exits non-zero is a result, not an error.
// A command killed by a signal (an *ssh.ExitError naming it), a lost connection
// or a canceled ctx is an error,
// and what the command had printed by then is still returned. Canceling ctx
// closes only this command session, not the SSH connection.
func (conn *Conn) Exec(ctx context.Context, command string, maxOutputBytes int) (ExecResult, error) {
	if err := commandLimit(maxOutputBytes); err != nil {
		return ExecResult{}, err
	}
	output := &commandOutput{limit: maxOutputBytes, separate: true}
	err := conn.captureCommand(ctx, command, nil, output, commandErrorOutput{output})
	result := output.execResult()
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.Signal() == "" {
		result.ExitCode = exit.ExitStatus()
		return result, nil
	}
	return result, err
}

// ExecStream runs one command on the authenticated server the way Exec does,
// but hands its combined stdout and stderr to write as the server delivers
// them instead of keeping them. write may be called from more than one
// goroutine; a caller that is not prepared for that must serialise it.
// ExecStream returns only when the command ends: a non-zero exit status is a
// result, while a command killed by a signal, a lost connection or a canceled
// ctx is an error. Canceling ctx closes only this command session, not the SSH
// connection.
func (conn *Conn) ExecStream(ctx context.Context, command string, write io.Writer) (ExecResult, error) {
	err := conn.captureCommand(ctx, command, nil, write, write)
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.Signal() == "" {
		return ExecResult{ExitCode: exit.ExitStatus()}, nil
	}
	return ExecResult{}, err
}

// runCommand starts command and waits for it. The output is stdout, plus
// stderr when withStderr, and is returned even when the command did not finish.
func (conn *Conn) runCommand(ctx context.Context, command string, maxOutputBytes int, withStderr bool) (data []byte, exceeded bool, err error) {
	if err := commandLimit(maxOutputBytes); err != nil {
		return nil, false, err
	}
	output := &commandOutput{limit: maxOutputBytes}
	var stderr io.Writer
	if withStderr {
		stderr = commandErrorOutput{output}
	}
	err = conn.captureCommand(ctx, command, nil, output, stderr)
	data, exceeded = output.result()
	return data, exceeded, err
}

// commandLimit rejects a command output limit that would retain nothing.
func commandLimit(maxOutputBytes int) error {
	if maxOutputBytes <= 0 {
		return errors.New("sshconn: command output limit must be positive")
	}
	return nil
}

// captureCommand starts command and waits for it, writing its stdout to stdout
// and, when stderr is not nil, its stderr to stderr. It returns how the command
// ended: nil, an *ssh.ExitError for a non-zero status or a signal, or the
// caller's or the connection's error when the attempt was canceled.
func (conn *Conn) captureCommand(ctx context.Context, command string, input []byte, stdout, stderr io.Writer) error {
	if command == "" {
		return errors.New("sshconn: empty command")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	if conn.ctx != nil {
		select {
		case <-conn.ctx.Done():
			return ErrCanceled
		default:
		}
	}
	client := conn.Client()
	if client == nil {
		return errors.New("sshconn: not connected")
	}
	operationCtx, cancel := context.WithCancel(ctx)
	defer cancel()
	if conn.ctx != nil {
		stop := context.AfterFunc(conn.ctx, cancel)
		defer stop()
	}
	cancelError := func() error {
		if conn.ctx != nil && conn.ctx.Err() != nil {
			return ErrCanceled
		}
		return ctx.Err()
	}
	session, err := openCommandSession(operationCtx, client)
	if err != nil {
		if operationCtx.Err() != nil {
			return cancelError()
		}
		return err
	}
	defer func() {
		if operationCtx.Err() != nil {
			go func() {
				_ = session.Signal(ssh.SIGTERM)
				session.Close()
			}()
		} else {
			session.Close()
		}
	}()

	session.Stdout = stdout
	if stderr != nil {
		session.Stderr = stderr
	}
	if len(input) > 0 {
		// The command session carries the payload, not the pty: a pty's
		// canonical input buffer is a few kilobytes and would drop the rest.
		session.Stdin = bytes.NewReader(input)
	}
	if operationCtx.Err() != nil {
		return cancelError()
	}
	started := make(chan error, 1)
	go func() {
		if err := operationCtx.Err(); err != nil {
			started <- err
			return
		}
		started <- session.Start(command)
	}()
	select {
	case err = <-started:
	case <-operationCtx.Done():
		return cancelError()
	}
	if err != nil {
		return err
	}
	waited := make(chan error, 1)
	go func() { waited <- session.Wait() }()

	select {
	case err = <-waited:
	case <-operationCtx.Done():
		return cancelError()
	}
	return err
}

// SSH channel-open and exec acknowledgments can block independently of command
// execution. Cancellation must reach the caller during establishment too. A
// late channel is closed rather than leaked into a timed-out request.
func openCommandSession(ctx context.Context, client *ssh.Client) (*ssh.Session, error) {
	type result struct {
		session *ssh.Session
		err     error
	}
	opened := make(chan result)
	go func() {
		session, err := client.NewSession()
		select {
		case opened <- result{session, err}:
		case <-ctx.Done():
			if session != nil {
				session.Close()
			}
		}
	}()
	select {
	case result := <-opened:
		return result.session, result.err
	case <-ctx.Done():
		return nil, ctx.Err()
	}
}

// commandOutput remains bounded even when a hostile command ignores its
// documented output contract. Session may copy stdout from its own goroutine.
type commandOutput struct {
	mu       sync.Mutex
	data     []byte
	stdout   []byte
	stderr   []byte
	separate bool
	limit    int
	exceeded bool
}

func (output *commandOutput) Write(data []byte) (int, error) {
	return output.write(data, false)
}

type commandErrorOutput struct{ output *commandOutput }

func (output commandErrorOutput) Write(data []byte) (int, error) {
	return output.output.write(data, true)
}

func (output *commandOutput) write(data []byte, stderr bool) (int, error) {
	written := len(data)
	output.mu.Lock()
	remaining := output.limit - len(output.data)
	if remaining > 0 {
		retained := data[:min(remaining, len(data))]
		output.data = append(output.data, retained...)
		if output.separate {
			if stderr {
				output.stderr = append(output.stderr, retained...)
			} else {
				output.stdout = append(output.stdout, retained...)
			}
		}
	}
	if len(data) > remaining {
		output.exceeded = true
	}
	output.mu.Unlock()
	return written, nil
}

func (output *commandOutput) result() ([]byte, bool) {
	output.mu.Lock()
	defer output.mu.Unlock()
	return append([]byte(nil), output.data...), output.exceeded
}

func (output *commandOutput) execResult() ExecResult {
	output.mu.Lock()
	defer output.mu.Unlock()
	return ExecResult{Output: append([]byte(nil), output.data...), Stdout: append([]byte(nil), output.stdout...), Stderr: append([]byte(nil), output.stderr...), Truncated: output.exceeded}
}

// StreamCommand runs one long-lived command on the authenticated server and
// calls line with each stdout line, newline included, as it arrives. The slice
// is valid only during the call. It returns nil once line reports false, which
// ends the command's session, and ErrCommandOutputTooLarge for a line longer
// than maxLineBytes. When the command ends by itself the result is its exit
// status. Canceling ctx closes only this command session.
func (conn *Conn) StreamCommand(ctx context.Context, command string, maxLineBytes int, line func([]byte) bool) error {
	return conn.streamCommand(ctx, command, maxLineBytes, false, line)
}

// StreamCommandSkippingLongLines drains oversized lines without buffering them
// or interrupting the command. It delivers only complete in-limit lines and
// the final in-limit partial line, if any.
func (conn *Conn) StreamCommandSkippingLongLines(ctx context.Context, command string, maxLineBytes int, line func([]byte) bool) error {
	return conn.streamCommand(ctx, command, maxLineBytes, true, line)
}

func (conn *Conn) streamCommand(ctx context.Context, command string, maxLineBytes int, skipLongLines bool, line func([]byte) bool) error {
	if command == "" {
		return errors.New("sshconn: empty command")
	}
	if maxLineBytes <= 0 {
		return errors.New("sshconn: command line limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	operation, cancel := context.WithCancel(ctx)
	defer cancel()
	if conn.ctx != nil {
		if conn.ctx.Err() != nil {
			return ErrCanceled
		}
		defer context.AfterFunc(conn.ctx, cancel)()
	}
	canceled := func() error {
		if conn.ctx != nil && conn.ctx.Err() != nil {
			return ErrCanceled
		}
		return ctx.Err()
	}
	client := conn.Client()
	if client == nil {
		return errors.New("sshconn: not connected")
	}
	session, err := openCommandSession(operation, client)
	if err != nil {
		if operation.Err() != nil {
			return canceled()
		}
		return err
	}
	closeSession := func() {
		_ = session.Signal(ssh.SIGTERM)
		_ = session.Close()
	}
	stop := context.AfterFunc(operation, closeSession)
	defer func() {
		if stop() {
			go closeSession()
		}
	}()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	started := make(chan error, 1)
	go func() {
		if err := operation.Err(); err != nil {
			started <- err
			return
		}
		started <- session.Start(command)
	}()
	select {
	case err := <-started:
		if operation.Err() != nil {
			return canceled()
		}
		if err != nil {
			return err
		}
	case <-operation.Done():
		return canceled()
	}

	reader := bufio.NewReaderSize(stdout, max(maxLineBytes, 16))
	discarding := false
	for {
		data, err := reader.ReadSlice('\n')
		if operation.Err() != nil {
			return canceled()
		}
		full := errors.Is(err, bufio.ErrBufferFull)
		if full || len(data) > maxLineBytes {
			if !skipLongLines {
				return fmt.Errorf("%w: limit %d bytes", ErrCommandOutputTooLarge, maxLineBytes)
			}
			discarding = true
		}
		if full {
			continue
		}
		if !discarding && len(data) != 0 && !line(data) {
			return nil
		}
		discarding = false
		if err != nil {
			break
		}
	}
	waited := make(chan error, 1)
	go func() { waited <- session.Wait() }()
	select {
	case err := <-waited:
		if operation.Err() != nil {
			return canceled()
		}
		return err
	case <-operation.Done():
		return canceled()
	}
}
