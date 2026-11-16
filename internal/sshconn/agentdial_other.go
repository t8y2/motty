//go:build !windows

package sshconn

import (
	"errors"
	"net"
)

func dialAgentPipe(string) (net.Conn, error) {
	return nil, errors.New("named pipes exist only on Windows")
}
