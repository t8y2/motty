package sftpclient

import (
	"context"
	"errors"
	"fmt"
	"io"
	"os"
	"time"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
	"motty/internal/sshconn"
)

var ErrSudoPasswordRequired = sshconn.ErrSudoPasswordRequired

const sudoSFTPCommand = "for server in /usr/lib/openssh/sftp-server /usr/lib/ssh/sftp-server /usr/libexec/openssh/sftp-server /usr/libexec/sftp-server /usr/local/libexec/sftp-server; do " +
	"if [ -x \"$server\" ]; then exec \"$server\"; fi; done; " +
	"echo sftp-server-not-found >&2; exit 127"

type sudoRemote struct {
	remote
	session *sshconn.SudoSession
}

func (client sudoRemote) Close() error {
	err := client.remote.Close()
	return errors.Join(err, client.session.Close())
}

func (client sudoRemote) preserveEditOwner(path string, original os.FileInfo) error {
	stat, ok := original.Sys().(*sftp.FileStat)
	if !ok || stat == nil {
		return errors.New("sftp: privileged edit cannot determine the original owner")
	}
	return client.Chown(path, int(stat.UID), int(stat.GID))
}

func OpenSudo(ctx context.Context, client *ssh.Client, password string) (Remote, error) {
	channel, err := sshconn.OpenSudo(ctx, client, sudoSFTPCommand, password)
	if err != nil {
		return nil, err
	}
	go io.Copy(io.Discard, channel.Stderr)
	readyCtx, cancel := context.WithTimeout(ctx, 20*time.Second)
	stop := context.AfterFunc(readyCtx, func() { _ = channel.Close() })
	opened, err := sftp.NewClientPipe(channel.Stdout, channel.Stdin)
	stop()
	handshakeErr := readyCtx.Err()
	cancel()
	if err != nil {
		_ = channel.Close()
		if handshakeErr != nil {
			return nil, handshakeErr
		}
		return nil, fmt.Errorf("sftp: privileged subsystem: %w", err)
	}
	if handshakeErr != nil {
		_ = opened.Close()
		_ = channel.Close()
		return nil, handshakeErr
	}
	return sudoRemote{remote: remote{Client: opened}, session: channel}, nil
}
