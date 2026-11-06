package localshell

import (
	"context"
	"errors"
	"io"
	"os"
	"os/exec"
	"time"

	"motty/internal/shellkind"
)

type AvailabilityRunner struct {
	Program string
	Dir     string
}

func (runner AvailabilityRunner) RunCommand(ctx context.Context, command string, limit int) ([]byte, error) {
	if command == "" || limit <= 0 || limit > 4096 {
		return nil, errors.New("localshell: invalid availability command")
	}
	program := runner.Program
	if program == "" {
		program = defaultShell()
	}
	var args []string
	switch shellkind.FromProgram(program) {
	case shellkind.POSIX:
		args = []string{"-c", command}
	case shellkind.PowerShell:
		args = []string{"-NoLogo", "-NoProfile", "-NonInteractive", "-Command", command}
	default:
		return nil, errors.New("localshell: unsupported shell for command detection")
	}
	process := exec.CommandContext(ctx, program, args...)
	process.WaitDelay = 100 * time.Millisecond
	prepareAvailabilityProcess(process)
	defer cleanupAvailabilityProcess(process)
	process.Env = os.Environ()
	process.Dir = runner.Dir
	output := &availabilityOutput{limit: limit}
	process.Stdout = output
	process.Stderr = io.Discard
	if err := process.Run(); err != nil {
		return nil, err
	}
	if output.truncated {
		return nil, errors.New("localshell: command detection output exceeded limit")
	}
	return output.data, nil
}

type availabilityOutput struct {
	data      []byte
	limit     int
	truncated bool
}

func (output *availabilityOutput) Write(data []byte) (int, error) {
	available := output.limit - len(output.data)
	if len(data) > available {
		output.truncated = true
	}
	output.data = append(output.data, data[:min(len(data), max(available, 0))]...)
	return len(data), nil
}
