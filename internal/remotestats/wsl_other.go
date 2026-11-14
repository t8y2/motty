//go:build !windows

package remotestats

import (
	"context"
	"errors"
	"os/exec"
)

// startCommand refuses on a system without WSL. A distribution's shell only
// exists on Windows, so a saved reference that reaches here is reported rather
// than quietly sampling this computer.
func (WSL) startCommand(context.Context, string) (*exec.Cmd, error) {
	return nil, errors.New("remote stats: WSL runs only on Windows")
}
