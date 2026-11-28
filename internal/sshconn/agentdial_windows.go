//go:build windows

package sshconn

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/Microsoft/go-winio"
)

const (
	cygwinSocketPrefix  = "!<socket >"
	maxCygwinSocketSize = 256
)

type cygwinSocket struct {
	port   uint16
	secret [16]byte
}

// dialAgentPipe opens the agent's named pipe with overlapped I/O, so that a
// pending read does not hold up the write of the next request.
func dialAgentPipe(address string) (net.Conn, error) {
	timeout := agentDialTimeout
	return winio.DialPipe(address, &timeout)
}

// dialAgentSocket connects to a Unix socket on Windows: either a Cygwin/MSYS2
// emulated socket file (created by Git for Windows ssh-agent) or a native
// WinSock AF_UNIX socket.
func dialAgentSocket(address string) (net.Conn, error) {
	if spec, ok, err := readCygwinSocket(address); err != nil {
		return nil, err
	} else if ok {
		return dialCygwinSocket(spec)
	}
	return net.DialTimeout("unix", address, agentDialTimeout)
}

func readCygwinSocket(address string) (cygwinSocket, bool, error) {
	info, err := os.Stat(address)
	if err != nil || !info.Mode().IsRegular() || info.Size() <= 0 || info.Size() > maxCygwinSocketSize {
		return cygwinSocket{}, false, nil
	}
	file, err := os.Open(address)
	if err != nil {
		return cygwinSocket{}, false, nil
	}
	defer file.Close()
	data, err := io.ReadAll(io.LimitReader(file, maxCygwinSocketSize+1))
	if err != nil || len(data) > maxCygwinSocketSize || !bytes.HasPrefix(data, []byte(cygwinSocketPrefix)) {
		return cygwinSocket{}, false, nil
	}
	spec, err := parseCygwinSocket(data)
	if err != nil {
		return cygwinSocket{}, false, err
	}
	return spec, true, nil
}

// parseCygwinSocket parses a Cygwin/MSYS2 AF_UNIX socket file of the form:
// "!<socket >PORT s AAAAAAAA-BBBBBBBB-CCCCCCCC-DDDDDDDD\x00".
func parseCygwinSocket(data []byte) (cygwinSocket, error) {
	text := strings.TrimRight(string(data), "\x00\r\n\t ")
	rest, ok := strings.CutPrefix(text, cygwinSocketPrefix)
	if !ok {
		return cygwinSocket{}, errors.New("not a Cygwin socket")
	}
	fields := strings.Fields(rest)
	if len(fields) != 3 {
		return cygwinSocket{}, errors.New("invalid Cygwin socket descriptor")
	}
	port, err := strconv.ParseUint(fields[0], 10, 16)
	if err != nil || port == 0 {
		return cygwinSocket{}, errors.New("invalid Cygwin socket port")
	}
	if fields[1] != "s" {
		return cygwinSocket{}, fmt.Errorf("unsupported Cygwin socket type %q", fields[1])
	}
	parts := strings.Split(fields[2], "-")
	if len(parts) != 4 {
		return cygwinSocket{}, errors.New("invalid Cygwin socket secret")
	}
	var spec cygwinSocket
	spec.port = uint16(port)
	for index, part := range parts {
		if len(part) == 0 || len(part) > 8 {
			return cygwinSocket{}, errors.New("invalid Cygwin socket secret")
		}
		word, err := strconv.ParseUint(part, 16, 32)
		if err != nil {
			return cygwinSocket{}, errors.New("invalid Cygwin socket secret")
		}
		binary.LittleEndian.PutUint32(spec.secret[index*4:(index+1)*4], uint32(word))
	}
	return spec, nil
}

// dialCygwinSocket connects to a Cygwin/MSYS2 local socket over loopback TCP and
// completes the 16-byte secret and 12-byte peer-credential handshake.
func dialCygwinSocket(spec cygwinSocket) (net.Conn, error) {
	address := net.JoinHostPort("127.0.0.1", strconv.Itoa(int(spec.port)))
	conn, err := net.DialTimeout("tcp", address, agentDialTimeout)
	if err != nil {
		return nil, err
	}
	if err := handshakeCygwinSocket(conn, spec.secret); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}

func handshakeCygwinSocket(conn net.Conn, secret [16]byte) error {
	if err := conn.SetDeadline(time.Now().Add(agentDialTimeout)); err != nil {
		return err
	}
	if err := writeFull(conn, secret[:]); err != nil {
		return err
	}
	var peerSecret [16]byte
	if _, err := io.ReadFull(conn, peerSecret[:]); err != nil {
		return err
	}
	if !bytes.Equal(peerSecret[:], secret[:]) {
		return errors.New("Cygwin socket secret mismatch")
	}
	var cred [12]byte
	binary.LittleEndian.PutUint32(cred[0:4], uint32(os.Getpid()))
	if err := writeFull(conn, cred[:]); err != nil {
		return err
	}
	var peerCred [12]byte
	if _, err := io.ReadFull(conn, peerCred[:]); err != nil {
		return err
	}
	return conn.SetDeadline(time.Time{})
}

func writeFull(writer io.Writer, data []byte) error {
	for len(data) > 0 {
		n, err := writer.Write(data)
		if err != nil {
			return err
		}
		if n == 0 {
			return io.ErrShortWrite
		}
		data = data[n:]
	}
	return nil
}
