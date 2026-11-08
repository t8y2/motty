package sftpclient

import (
	"bufio"
	"context"
	"crypto/ed25519"
	"crypto/rand"
	"errors"
	"io"
	"net"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"

	"github.com/pkg/sftp"
	"golang.org/x/crypto/ssh"
)

func sudoTestClient(t *testing.T, askPassword bool) (*ssh.Client, string) {
	t.Helper()
	_, key, err := ed25519.GenerateKey(rand.Reader)
	if err != nil {
		t.Fatal(err)
	}
	signer, err := ssh.NewSignerFromKey(key)
	if err != nil {
		t.Fatal(err)
	}
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = listener.Close() })
	root := t.TempDir()
	if err := os.WriteFile(root+"/config.yaml", []byte("before\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	server := &ssh.ServerConfig{NoClientAuth: true}
	server.AddHostKey(signer)
	go func() {
		connection, acceptErr := listener.Accept()
		if acceptErr != nil {
			return
		}
		defer connection.Close()
		_, channels, requests, handshakeErr := ssh.NewServerConn(connection, server)
		if handshakeErr != nil {
			return
		}
		go ssh.DiscardRequests(requests)
		for incoming := range channels {
			if incoming.ChannelType() != "session" {
				_ = incoming.Reject(ssh.UnknownChannelType, "session required")
				continue
			}
			channel, requests, acceptErr := incoming.Accept()
			if acceptErr != nil {
				return
			}
			go func() {
				defer channel.Close()
				for request := range requests {
					if request.Type == "subsystem" {
						var subsystem struct{ Name string }
						if ssh.Unmarshal(request.Payload, &subsystem) != nil || subsystem.Name != "sftp" {
							_ = request.Reply(false, nil)
							return
						}
						_ = request.Reply(true, nil)
						service, serveErr := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(root), sftp.ReadOnly())
						if serveErr == nil {
							_ = service.Serve()
							_ = service.Close()
						}
						return
					}
					if request.Type != "exec" {
						_ = request.Reply(false, nil)
						continue
					}
					var command struct{ Value string }
					if err := ssh.Unmarshal(request.Payload, &command); err != nil || !strings.Contains(command.Value, "sftp-server") {
						_ = request.Reply(false, nil)
						return
					}
					_ = request.Reply(true, nil)
					if askPassword {
						_, _ = io.WriteString(channel.Stderr(), "motty-sudo-password:")
						password, readErr := bufio.NewReader(channel).ReadString('\n')
						if readErr != nil || password != "secret\n" {
							_, _ = io.WriteString(channel.Stderr(), "motty-sudo-password:")
							return
						}
					}
					_, _ = io.WriteString(channel.Stderr(), "motty-sudo-ready:")
					service, serveErr := sftp.NewServer(channel, sftp.WithServerWorkingDirectory(root))
					if serveErr == nil {
						_ = service.Serve()
						_ = service.Close()
					}
					return
				}
			}()
		}
	}()
	client, err := ssh.Dial("tcp", listener.Addr().String(), &ssh.ClientConfig{
		User: "test", Auth: []ssh.AuthMethod{ssh.Password("test")},
		HostKeyCallback: ssh.FixedHostKey(signer.PublicKey()),
	})
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = client.Close() })
	return client, root
}

func TestSudoSFTPNeverSendsProtocolBeforeAuthorization(t *testing.T) {
	for _, askPassword := range []bool{false, true} {
		t.Run(map[bool]string{false: "cached", true: "password"}[askPassword], func(t *testing.T) {
			client, _ := sudoTestClient(t, askPassword)
			remote, err := OpenSudo(context.Background(), client, "secret")
			if err != nil {
				t.Fatal(err)
			}
			defer remote.Close()
			file, err := remote.Open("config.yaml")
			if err != nil {
				t.Fatal(err)
			}
			defer file.Close()
			body, err := io.ReadAll(file)
			if err != nil || string(body) != "before\n" {
				t.Fatalf("file = %q, %v", body, err)
			}
		})
	}
}

func TestSudoSFTPRejectsMissingOrIncorrectPassword(t *testing.T) {
	client, _ := sudoTestClient(t, true)
	if _, err := OpenSudo(context.Background(), client, ""); !errors.Is(err, ErrSudoPasswordRequired) {
		t.Fatalf("no password: %v", err)
	}
	if _, err := OpenSudo(context.Background(), client, "incorrect"); err == nil || !strings.Contains(err.Error(), "refused") {
		t.Fatalf("incorrect password: %v", err)
	}
}

func TestSudoSFTPWorkerSavesAnOpenedDocument(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("privileged ownership changes require a POSIX SFTP server")
	}
	client, _ := sudoTestClient(t, true)
	worker := NewWorker(func() (Remote, error) {
		return OpenSudo(context.Background(), client, "secret")
	}, nil)
	defer worker.Close()
	if !worker.OpenText("config.yaml") {
		t.Fatal("open was not queued")
	}
	opened := waitResult(t, worker)
	if opened.Err != nil || opened.Document.Text != "before\n" {
		t.Fatalf("open = %+v", opened)
	}
	if !worker.SaveText("config.yaml", "after\n", opened.Document.Revision, false) {
		t.Fatal("save was not queued")
	}
	saved := waitResult(t, worker)
	if saved.Err != nil || saved.Document.Text != "after\n" {
		t.Fatalf("save = %+v", saved)
	}
	if !worker.OpenText("config.yaml") {
		t.Fatal("reopen was not queued")
	}
	if reopened := waitResult(t, worker); reopened.Err != nil || reopened.Document.Text != "after\n" {
		t.Fatalf("reopened = %+v", reopened)
	}
}

func TestSudoSFTPRecoversDeniedWriteAndPreservesModeAndRevision(t *testing.T) {
	if runtime.GOOS == "windows" {
		t.Skip("privileged ownership changes require a POSIX SFTP server")
	}
	client, root := sudoTestClient(t, true)
	ordinary, err := sftp.NewClient(client)
	if err != nil {
		t.Fatal(err)
	}
	file, err := ordinary.Open("config.yaml")
	if err != nil {
		t.Fatal(err)
	}
	_ = file.Close()
	if _, err := ordinary.OpenFile("config.yaml", os.O_WRONLY|os.O_TRUNC); err == nil {
		t.Fatal("ordinary connection unexpectedly wrote the protected file")
	}
	_ = ordinary.Close()
	worker := NewWorker(func() (Remote, error) { return OpenSudo(context.Background(), client, "secret") }, nil)
	defer worker.Close()
	if !worker.OpenText("config.yaml") {
		t.Fatal("open was not queued")
	}
	opened := waitResult(t, worker)
	if opened.Err != nil || opened.Document.Text != "before\n" {
		t.Fatalf("open = %+v", opened)
	}
	privileged, err := OpenSudo(context.Background(), client, "secret")
	if err != nil {
		t.Fatal(err)
	}
	before, err := privileged.(sudoRemote).Lstat("config.yaml")
	_ = privileged.Close()
	if err != nil {
		t.Fatal(err)
	}
	if !worker.SaveText("config.yaml", "after\n", opened.Document.Revision, false) {
		t.Fatal("save was not queued")
	}
	saved := waitResult(t, worker)
	if saved.Err != nil {
		t.Fatalf("privileged save = %+v", saved)
	}
	info, err := os.Stat(filepath.Join(root, "config.yaml"))
	if err != nil || info.Mode().Perm() != 0o644 {
		t.Fatalf("file mode after privileged save = %v, %v", info, err)
	}
	privileged, err = OpenSudo(context.Background(), client, "secret")
	if err != nil {
		t.Fatal(err)
	}
	after, err := privileged.(sudoRemote).Lstat("config.yaml")
	_ = privileged.Close()
	if err != nil {
		t.Fatal(err)
	}
	if prior, ok := before.Sys().(*sftp.FileStat); ok {
		current, ok := after.Sys().(*sftp.FileStat)
		if !ok || prior.UID != current.UID || prior.GID != current.GID {
			t.Fatalf("sudo save changed owner: before=%v after=%v", prior, current)
		}
	}
	if err := os.WriteFile(filepath.Join(root, "config.yaml"), []byte("external change\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	if !worker.SaveText("config.yaml", "must not win\n", saved.Document.Revision, false) {
		t.Fatal("conflicting save was not queued")
	}
	if conflict := waitResult(t, worker); !errors.Is(conflict.Err, ErrRemoteChanged) {
		t.Fatalf("stale revision = %+v", conflict)
	}
	if content, err := os.ReadFile(filepath.Join(root, "config.yaml")); err != nil || string(content) != "external change\n" {
		t.Fatalf("file after conflict = %q, %v", content, err)
	}
}
