package sftpclient

import (
	"os"
	"strconv"
	"testing"
	"time"

	"golang.org/x/crypto/ssh"
)

// BenchmarkOpenDocumentOverDelayedSSH opens a remote file the way a double
// click in the file browser does, through a real SSH and SFTP session to a
// loopback tools/e2esshd with a fixed delay in each direction. It runs only
// when a server is named:
//
//	cd tools/e2esshd && go build -o /tmp/e2esshd . && /tmp/e2esshd 2395 &
//	MOTTY_E2E_SSHD=127.0.0.1:2395 MOTTY_E2E_DELAY=25ms MOTTY_E2E_SIZE=4 \
//	    go test ./internal/sftpclient -run '^$' -bench OpenDocument -benchtime 3x
//
// MOTTY_E2E_DELAY is the one-way delay and MOTTY_E2E_SIZE the file size in MiB.
func BenchmarkOpenDocumentOverDelayedSSH(b *testing.B) {
	target := os.Getenv("MOTTY_E2E_SSHD")
	if target == "" {
		b.Skip("MOTTY_E2E_SSHD is not set")
	}
	delay := 25 * time.Millisecond
	if text := os.Getenv("MOTTY_E2E_DELAY"); text != "" {
		parsed, err := time.ParseDuration(text)
		if err != nil {
			b.Fatal(err)
		}
		delay = parsed
	}
	sizeMiB := 4
	if text := os.Getenv("MOTTY_E2E_SIZE"); text != "" {
		parsed, err := strconv.Atoi(text)
		if err != nil {
			b.Fatal(err)
		}
		sizeMiB = parsed
	}
	address := delayedProxy(b, target, delay)
	sshClient, err := ssh.Dial("tcp", address, &ssh.ClientConfig{
		User:            "bench",
		Auth:            []ssh.AuthMethod{ssh.Password("secret")},
		HostKeyCallback: ssh.InsecureIgnoreHostKey(),
		Timeout:         10 * time.Second,
	})
	if err != nil {
		b.Fatal(err)
	}
	defer sshClient.Close()
	client, err := Open(sshClient)
	if err != nil {
		b.Fatal(err)
	}
	content := make([]byte, sizeMiB<<20)
	for i := range content {
		content[i] = byte(i*2654435761>>13) | 0x20
	}
	name := "open-document-" + strconv.Itoa(sizeMiB) + ".log"
	file, err := client.(transferRemote).OpenTransferFile(name, os.O_WRONLY|os.O_CREATE|os.O_TRUNC)
	if err != nil {
		b.Fatal(err)
	}
	if _, err := file.Write(content); err != nil {
		b.Fatal(err)
	}
	if err := file.Close(); err != nil {
		b.Fatal(err)
	}
	worker := NewWorker(func() (Remote, error) { return Open(sshClient) }, nil)
	defer worker.Close()

	b.SetBytes(int64(len(content)))
	b.ReportAllocs()
	for b.Loop() {
		if !worker.OpenDocument(name, 64<<20) {
			b.Fatal("open not queued")
		}
		select {
		case result := <-worker.Results():
			if result.Err != nil || len(result.Data) != len(content) {
				b.Fatalf("read %d bytes: %v", len(result.Data), result.Err)
			}
		case <-time.After(5 * time.Minute):
			b.Fatal("open did not finish")
		}
	}
}
