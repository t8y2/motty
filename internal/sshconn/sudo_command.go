package sshconn

import (
	"bufio"
	"context"
	"errors"
	"fmt"
	"io"

	"golang.org/x/crypto/ssh"
)

func (conn *Conn) RunSudoCommand(ctx context.Context, command, password string, maxOutputBytes int) ([]byte, error) {
	data, exceeded, err := conn.runSudoCommand(ctx, command, password, maxOutputBytes, false)
	if exceeded {
		return data, fmt.Errorf("%w: limit %d bytes", ErrCommandOutputTooLarge, maxOutputBytes)
	}
	return data, err
}

func (conn *Conn) ExecSudo(ctx context.Context, command, password string, maxOutputBytes int) (ExecResult, error) {
	data, exceeded, err := conn.runSudoCommand(ctx, command, password, maxOutputBytes, true)
	result := ExecResult{Output: data, Truncated: exceeded}
	var exit *ssh.ExitError
	if errors.As(err, &exit) && exit.Signal() == "" {
		result.ExitCode = exit.ExitStatus()
		return result, nil
	}
	return result, err
}

func (conn *Conn) runSudoCommand(ctx context.Context, command, password string, limit int, withStderr bool) ([]byte, bool, error) {
	if limit <= 0 {
		return nil, false, errors.New("sshconn: command output limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return nil, false, err
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
	channel, err := OpenSudo(operationCtx, client, command, password)
	if err != nil {
		return nil, false, err
	}
	defer channel.Close()
	stop := context.AfterFunc(operationCtx, func() { _ = channel.Close() })
	defer stop()
	_ = channel.Stdin.Close()
	output := commandOutput{limit: limit}
	stdoutDone := make(chan struct{})
	stderrDone := make(chan struct{})
	go func() {
		_, _ = io.Copy(&output, channel.Stdout)
		close(stdoutDone)
	}()
	go func() {
		if withStderr {
			_, _ = io.Copy(&output, channel.Stderr)
		} else {
			_, _ = io.Copy(io.Discard, channel.Stderr)
		}
		close(stderrDone)
	}()
	err = channel.Session.Wait()
	<-stdoutDone
	<-stderrDone
	data, exceeded := output.result()
	if ctx.Err() != nil {
		return nil, exceeded, ctx.Err()
	}
	if conn.ctx != nil && conn.ctx.Err() != nil {
		return nil, exceeded, ErrCanceled
	}
	return data, exceeded, err
}

func (conn *Conn) StreamSudoCommand(ctx context.Context, command, password string, maxLineBytes int, line func([]byte) bool) error {
	if maxLineBytes <= 0 {
		return errors.New("sshconn: command line limit must be positive")
	}
	if err := ctx.Err(); err != nil {
		return err
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
	channel, err := OpenSudo(operationCtx, client, command, password)
	if err != nil {
		return err
	}
	defer channel.Close()
	stop := context.AfterFunc(operationCtx, func() { _ = channel.Close() })
	defer stop()
	_ = channel.Stdin.Close()
	go io.Copy(io.Discard, channel.Stderr)
	reader := bufio.NewReaderSize(channel.Stdout, max(maxLineBytes, 16))
	for {
		data, readErr := reader.ReadSlice('\n')
		if readErr == nil {
			if !line(data) {
				return nil
			}
			continue
		}
		if errors.Is(readErr, bufio.ErrBufferFull) {
			return fmt.Errorf("%w: limit %d bytes", ErrCommandOutputTooLarge, maxLineBytes)
		}
		break
	}
	if ctx.Err() != nil {
		return ctx.Err()
	}
	if conn.ctx != nil && conn.ctx.Err() != nil {
		return ErrCanceled
	}
	return channel.Session.Wait()
}
