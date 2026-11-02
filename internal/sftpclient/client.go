// Package sftpclient owns motty's remote-file transport and bounded worker.
package sftpclient

import (
	"fmt"
	"io"
	"os"

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

// Open starts one SFTP subsystem over an authenticated SSH connection.
func Open(client *ssh.Client) (Remote, error) {
	if client == nil {
		return nil, fmt.Errorf("sftp: SSH client is not connected")
	}
	opened, err := sftp.NewClient(client)
	if err != nil {
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
