package sshconn

import (
	"net"

	"github.com/Microsoft/go-winio"
)

// dialAgentPipe opens the agent's named pipe with overlapped I/O, so that a
// pending read does not hold up the write of the next request.
func dialAgentPipe(address string) (net.Conn, error) {
	timeout := agentDialTimeout
	return winio.DialPipe(address, &timeout)
}
