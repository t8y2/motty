package sshconn

import (
	"bufio"
	"bytes"
	"context"
	"errors"
	"fmt"
	"io"
	"strings"
	"time"

	"golang.org/x/crypto/ssh"
)

var ErrSudoPasswordRequired = errors.New("sshconn: sudo requires a password")

const sudoPrompt = "motty-sudo-password:"
const sudoReady = "motty-sudo-ready:"

type SudoSession struct {
	Session *ssh.Session
	Stdin   io.WriteCloser
	Stdout  io.Reader
	Stderr  io.Reader
}

func (channel *SudoSession) Close() error { return channel.Session.Close() }

func sudoCommand(command string) string {
	startup := "printf '%s' " + shellQuote(sudoReady) + " >&2; exec sh -c " + shellQuote(command)
	return "sudo -S -p " + shellQuote(sudoPrompt) + " -- sh -c " + shellQuote(startup)
}

func OpenSudo(ctx context.Context, client *ssh.Client, command, password string) (*SudoSession, error) {
	if client == nil {
		return nil, errors.New("sshconn: SSH client is not connected")
	}
	if command == "" {
		return nil, errors.New("sshconn: empty sudo command")
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	readyCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	session, err := openCommandSession(readyCtx, client)
	if err != nil {
		if readyCtx.Err() != nil {
			return nil, readyCtx.Err()
		}
		return nil, err
	}
	handedOff := false
	defer func() {
		if !handedOff {
			_ = session.Close()
		}
	}()
	stdin, err := session.StdinPipe()
	if err != nil {
		return nil, err
	}
	stdout, err := session.StdoutPipe()
	if err != nil {
		return nil, err
	}
	stderr, err := session.StderrPipe()
	if err != nil {
		return nil, err
	}
	stop := context.AfterFunc(readyCtx, func() { _ = session.Close() })
	defer stop()
	started := make(chan error, 1)
	go func() { started <- session.Start(sudoCommand(command)) }()
	select {
	case err = <-started:
		if err != nil {
			return nil, err
		}
	case <-readyCtx.Done():
		return nil, readyCtx.Err()
	}
	reader := bufio.NewReader(stderr)
	output := make([]byte, 0, 128)
	sentPassword := false
	for len(output) < 4096 {
		character, readErr := reader.ReadByte()
		if readErr != nil {
			if readyCtx.Err() != nil {
				return nil, readyCtx.Err()
			}
			return nil, fmt.Errorf("sshconn: sudo could not start: %s: %w", output, readErr)
		}
		output = append(output, character)
		if bytes.HasSuffix(output, []byte(sudoReady)) {
			if readyCtx.Err() != nil {
				return nil, readyCtx.Err()
			}
			handedOff = true
			return &SudoSession{Session: session, Stdin: stdin, Stdout: stdout, Stderr: reader}, nil
		}
		if bytes.HasSuffix(output, []byte(sudoPrompt)) {
			if sentPassword {
				return nil, errors.New("sshconn: sudo password was refused")
			}
			if password == "" {
				return nil, ErrSudoPasswordRequired
			}
			if strings.ContainsAny(password, "\x00\r\n") {
				return nil, errors.New("sshconn: sudo password contains a control character")
			}
			if _, err = io.WriteString(stdin, password+"\n"); err != nil {
				return nil, err
			}
			sentPassword = true
			output = output[:0]
		}
	}
	return nil, errors.New("sshconn: sudo startup response too large")
}
