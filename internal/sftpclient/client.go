// Package sftpclient owns motty's remote-file transport and bounded worker.
package sftpclient

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strings"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

// Remote is the SFTP operations the transfer worker uses. It is intentionally
// defined at the point of use so tests can provide a deterministic server.
type Remote interface {
	Getwd() (string, error)
	ReadDir(path string) ([]os.FileInfo, error)
	Open(path string) (io.ReadCloser, error)
	Close() error
}

// TransferFile is the seekable remote handle needed for safe resume and
// same-directory staged replacement.
type TransferFile interface {
	io.Reader
	io.Writer
	io.Seeker
	io.Closer
	Stat() (os.FileInfo, error)
	Sync() error
	Truncate(size int64) error
}

type transferRemote interface {
	Remote
	Lstat(path string) (os.FileInfo, error)
	OpenTransferFile(path string, flags int) (TransferFile, error)
	Mkdir(path string) error
	Rename(oldPath, newPath string) error
	PosixRename(oldPath, newPath string) error
	Remove(path string) error
	Chmod(path string, mode os.FileMode) error
}

type remote struct {
	*sftp.Client
}

// ErrChannelNoise marks a file channel that opened but carried text before the
// protocol began: a login banner or an error line the server's session setup
// writes to every new session once the connection has a shell. Neither SFTP nor
// SCP can be spoken over such a channel, though the same account usually can on
// a connection with no shell.
var ErrChannelNoise = errors.New("the server wrote text on the file channel before the protocol began")

// IsChannelNoise reports that err comes from a file channel carrying text ahead
// of the protocol, found while the backend was starting.
func IsChannelNoise(err error) bool {
	return errors.Is(err, ErrChannelNoise)
}

// Open starts one SFTP subsystem over an authenticated SSH connection.
func Open(client *ssh.Client) (Remote, error) {
	if client == nil {
		return nil, fmt.Errorf("sftp: SSH client is not connected")
	}
	opened, err := sftp.NewClient(client)
	if err != nil {
		// Any text ahead of the version packet reads as a length far past the
		// limit, and the library reports only that: it keeps no unexported type
		// to match, so the message is the one signal there is.
		if strings.Contains(err.Error(), "packet too long") {
			return nil, fmt.Errorf("sftp: start subsystem: %w: %w", ErrChannelNoise, err)
		}
		return nil, fmt.Errorf("sftp: start subsystem: %w", err)
	}
	return remote{Client: opened}, nil
}

func (client remote) Open(path string) (io.ReadCloser, error) {
	return client.Client.Open(path)
}

func (client remote) OpenTransferFile(path string, flags int) (TransferFile, error) {
	return client.Client.OpenFile(path, flags)
}

func (client remote) Create(path string) (io.WriteCloser, error) {
	return client.Client.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
}

// CreateFile creates an empty file and fails if the path already exists: the
// server enforces O_EXCL, so a file created between a caller's check and this
// call is never truncated.
func (client remote) CreateFile(path string) error {
	file, err := client.Client.OpenFile(path, os.O_WRONLY|os.O_CREATE|os.O_EXCL)
	if err != nil {
		return err
	}
	return file.Close()
}

// Chown changes a path's numeric owner and group. Servers enforce root for
// this; the elevated subsystem is what makes it usable on a normal account.
func (client remote) Chown(path string, uid, gid uint32) error {
	return client.Client.Chown(path, int(uid), int(gid))
}
