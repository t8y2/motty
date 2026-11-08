package sshconn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
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
	output := &commandOutput{limit: maxOutputBytes, separate: true}
	_, _, err := conn.captureCommand(ctx, command, maxOutputBytes, true, output)
	result := output.execResult()
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.Signal() == "" {
		result.ExitCode = exit.ExitStatus()
		return result, nil
	}
	return result, err
}

// runCommand starts command and waits for it. The output is stdout, plus
// stderr when withStderr, and is returned even when the command did not finish.
func (conn *Conn) runCommand(ctx context.Context, command string, maxOutputBytes int, withStderr bool) (data []byte, exceeded bool, err error) {
	return conn.captureCommand(ctx, command, maxOutputBytes, withStderr, &commandOutput{limit: maxOutputBytes})
}

func (conn *Conn) captureCommand(ctx context.Context, command string, maxOutputBytes int, withStderr bool, output *commandOutput) (data []byte, exceeded bool, err error) {
	if command == "" {
		return nil, false, errors.New("sshconn: empty command")
	}
	if maxOutputBytes <= 0 {
		return nil, false, errors.New("sshconn: command output limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
	}
	if conn.ctx != nil {
		select {
		case <-conn.ctx.Done():
			return nil, false, ErrCanceled
		default:
		}
	}
	client := conn.Client()
	if client == nil {
		return nil, false, errors.New("sshconn: not connected")
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
			return nil, false, cancelError()
		}
		return nil, false, err
	}
	defer func() {
		if operationCtx.Err() != nil {
			go session.Close()
		} else {
			session.Close()
		}
	}()

	session.Stdout = output
	if withStderr {
		session.Stderr = commandErrorOutput{output}
	}
	if operationCtx.Err() != nil {
		return nil, false, cancelError()
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
		return nil, false, cancelError()
	}
	if err != nil {
		return nil, false, err
	}
	waited := make(chan error, 1)
	go func() { waited <- session.Wait() }()

	select {
	case err = <-waited:
	case <-operationCtx.Done():
		data, exceeded = output.result()
		return data, exceeded, cancelError()
	}
	data, exceeded = output.result()
	return data, exceeded, err
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
	if command == "" {
		return errors.New("sshconn: empty command")
	}
	if maxLineBytes <= 0 {
		return errors.New("sshconn: command line limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	var connDone <-chan struct{}
	if conn.ctx != nil {
		connDone = conn.ctx.Done()
		select {
		case <-connDone:
			return ErrCanceled
		default:
		}
	}
	client := conn.Client()
	if client == nil {
		return errors.New("sshconn: not connected")
	}
	session, err := client.NewSession()
	if err != nil {
		return err
	}
	defer session.Close()
	stdout, err := session.StdoutPipe()
	if err != nil {
		return err
	}
	if err := session.Start(command); err != nil {
		return err
	}
	finished := make(chan struct{})
	defer close(finished)
	go func() {
		select {
		case <-ctx.Done():
		case <-connDone:
		case <-finished:
			return
		}
		_ = session.Close()
	}()

	reader := bufio.NewReaderSize(stdout, max(maxLineBytes, 16))
	for {
		data, err := reader.ReadSlice('\n')
		if err == nil {
			if !line(data) {
				return nil
			}
			continue
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			return fmt.Errorf("%w: limit %d bytes", ErrCommandOutputTooLarge, maxLineBytes)
		}
		break
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	select {
	case <-connDone:
		return ErrCanceled
	default:
	}
	return session.Wait()
}
