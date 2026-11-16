package workbench

import (
	"context"
	"errors"

	"motty/internal/sshconn"
)

var errSudoCommandUnavailable = errors.New("sshconn: sudo is unavailable for this connection")

type sudoCommandRunner struct {
	connection Connection
	password   string
}

func commandRunnerFor(session *Session) monitorRunner {
	if !session.sudoCommands {
		return session.conn
	}
	return sudoCommandRunner{connection: session.conn, password: session.sudoPassword}
}

func (runner sudoCommandRunner) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	connection, ok := runner.connection.(interface {
		RunSudoCommand(context.Context, string, string, int) ([]byte, error)
	})
	if !ok {
		return nil, errSudoCommandUnavailable
	}
	return connection.RunSudoCommand(ctx, command, runner.password, limit)
}

func (runner sudoCommandRunner) Exec(ctx context.Context, command string, limit int) (sshconn.ExecResult, error) {
	connection, ok := runner.connection.(interface {
		ExecSudo(context.Context, string, string, int) (sshconn.ExecResult, error)
	})
	if !ok {
		return sshconn.ExecResult{}, errSudoCommandUnavailable
	}
	return connection.ExecSudo(ctx, command, runner.password, limit)
}

func (runner sudoCommandRunner) StreamCommand(ctx context.Context, command string, limit int, line func([]byte) bool) error {
	connection, ok := runner.connection.(interface {
		StreamSudoCommand(context.Context, string, string, int, func([]byte) bool) error
	})
	if !ok {
		return errSudoCommandUnavailable
	}
	return connection.StreamSudoCommand(ctx, command, runner.password, limit, line)
}
