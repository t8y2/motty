package remotestats

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
)

// ErrOutputTooLarge reports a command that wrote more than its caller agreed
// to read.
var ErrOutputTooLarge = errors.New("remote stats: command output too large")

// Local runs the monitor's fixed commands on this computer, for a terminal that
// is not a server. It is the same protocol and the same commands as a remote
// host of this system gets; only the way a command is started differs, so a
// local terminal needs no sampler of its own. Each call starts one process
// tree, and canceling the context ends all of it.
type Local struct{}

// StreamCommand runs command and hands each stdout line, newline included, to
// line until it reports false, the command ends or ctx is canceled. A line
// longer than maxLineBytes ends the command with ErrOutputTooLarge. A command
// that stops on its own returns its exit error, or nil for a clean exit.
func (Local) StreamCommand(ctx context.Context, command string, maxLineBytes int, line func([]byte) bool) error {
	if command == "" {
		return errors.New("remote stats: empty command")
	}
	if maxLineBytes <= 0 {
		return errors.New("remote stats: command line limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return err
	}
	parent := ctx
	ctx, stop := context.WithCancel(parent)
	defer stop()
	process := localCommand(ctx, command)
	stdout, err := process.StdoutPipe()
	if err != nil {
		return err
	}
	if err := process.Start(); err != nil {
		return err
	}
	reader := bufio.NewReaderSize(stdout, max(maxLineBytes, 16))
	var readErr error
	stopped := false
	for {
		data, err := reader.ReadSlice('\n')
		if err == nil {
			if !line(data) {
				stopped = true
				break
			}
			continue
		}
		if errors.Is(err, bufio.ErrBufferFull) {
			readErr = fmt.Errorf("%w: limit %d bytes", ErrOutputTooLarge, maxLineBytes)
		}
		break
	}
	// Nothing of the command may outlive this call. One that ended by itself is
	// only waited for: canceling would turn its clean exit into an error.
	if stopped || readErr != nil {
		stop()
	}
	waitErr := process.Wait()
	switch {
	case readErr != nil:
		return readErr
	case stopped:
		return nil
	case parent.Err() != nil:
		return parent.Err()
	}
	return waitErr
}

// RunCommand runs command to completion and returns its stdout, at most
// maxOutputBytes of it; more is ErrOutputTooLarge with what fit.
func (Local) RunCommand(ctx context.Context, command string, maxOutputBytes int) ([]byte, error) {
	if command == "" {
		return nil, errors.New("remote stats: empty command")
	}
	if maxOutputBytes <= 0 {
		return nil, errors.New("remote stats: command output limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	process := localCommand(ctx, command)
	output := boundedBuffer{limit: maxOutputBytes}
	process.Stdout = &output
	err := process.Run()
	if output.exceeded {
		return output.buffer.Bytes(), fmt.Errorf("%w: limit %d bytes", ErrOutputTooLarge, maxOutputBytes)
	}
	if ctxErr := ctx.Err(); ctxErr != nil {
		return nil, ctxErr
	}
	return output.buffer.Bytes(), err
}

// boundedBuffer keeps the first limit bytes and notes that there were more. It
// does not embed bytes.Buffer, whose ReadFrom would let a copy bypass Write.
type boundedBuffer struct {
	buffer   bytes.Buffer
	limit    int
	exceeded bool
}

func (buffer *boundedBuffer) Write(data []byte) (int, error) {
	written := len(data)
	if remaining := buffer.limit - buffer.buffer.Len(); remaining < len(data) {
		buffer.exceeded = true
		data = data[:max(remaining, 0)]
	}
	buffer.buffer.Write(data)
	return written, nil
}
