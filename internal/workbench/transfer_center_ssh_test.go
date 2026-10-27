package workbench

import (
	"bytes"
	"fmt"
	"net"
	"os"
	"path"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// This opt-in check uses disposable files on the explicitly selected test hosts.
func TestTransferCenterSSHUploadDownload(t *testing.T) {
	endpoints := os.Getenv("MOTTY_TEST_SSH_ENDPOINTS")
	if endpoints == "" {
		t.Skip("set MOTTY_TEST_SSH_ENDPOINTS to test real SFTP connections")
	}
	_, options := newWorld(t)
	state := newTestState(t, options)
	for _, endpoint := range strings.Split(endpoints, ",") {
		t.Run(endpoint, func(t *testing.T) {
			host, portText, err := net.SplitHostPort(endpoint)
			if err != nil {
				t.Fatal(err)
			}
			port, err := strconv.ParseUint(portText, 10, 16)
			if err != nil {
				t.Fatal(err)
			}
			conn := sshconn.Dial(sshconn.Config{Host: host, Port: uint16(port), User: os.Getenv("MOTTY_TEST_SSH_USER"), IdentityFiles: []string{os.Getenv("MOTTY_TEST_SSH_KEY")}, KnownHostsPath: os.Getenv("MOTTY_TEST_SSH_KNOWN_HOSTS"), DialTimeout: 5 * time.Second})
			defer conn.Close()
			deadline := time.Now().Add(10 * time.Second)
			for conn.Status().Phase != sshconn.PhaseConnected {
				if time.Now().After(deadline) {
					t.Fatalf("SSH connection did not open: %+v", conn.Status())
				}
				time.Sleep(time.Millisecond)
			}
			view := &sftpView{transferServer: endpoint, transferAddress: endpoint}
			view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return openRemoteFiles(realConn{conn}, transferSFTP) }, nil)
			defer view.worker.Close()
			await := func() sftpclient.Result {
				t.Helper()
				select {
				case result := <-view.worker.Results():
					state.refreshSFTPTransfers(view)
					if result.Err != nil {
						t.Fatal(result.Err)
					}
					return result
				case <-time.After(10 * time.Second):
					t.Fatal("SFTP operation timed out")
				}
				return sftpclient.Result{}
			}
			view.worker.List("")
			view.path = await().Path
			name := fmt.Sprintf("motty-transfer-check-%d.bin", time.Now().UnixNano())
			remotePath := path.Join(view.path, name)
			defer func() { view.worker.RemoveFile(remotePath); await(); state.archiveTransfers(view) }()
			local := filepath.Join(t.TempDir(), name)
			payload := bytes.Repeat([]byte("Motty SFTP transfer center\n"), 81920)
			if err := os.WriteFile(local, payload, 0o600); err != nil {
				t.Fatal(err)
			}
			state.queueSFTPUpload(view, local, false)
			if !state.transfers.open {
				t.Fatal("upload did not open transfer center")
			}
			await()
			state.Click(hitTransfersClose)
			download := filepath.Join(t.TempDir(), name)
			state.queueSFTPDownload(view, sftpclient.Entry{Name: name, Size: int64(len(payload)), Mode: 0o600}, download)
			if !state.transfers.open {
				t.Fatal("download did not reopen transfer center")
			}
			await()
			actual, err := os.ReadFile(download)
			if err != nil || !bytes.Equal(payload, actual) {
				t.Fatalf("round trip changed data: %v", err)
			}
		})
	}
	if len(state.transfers.entries) != 2*len(strings.Split(endpoints, ",")) {
		t.Fatal("global history lost another server's tasks")
	}
	for _, entry := range state.transfers.entries {
		if entry.snapshot.State != sftpclient.TransferCompleted || !strings.Contains(entry.amount, "MB") {
			t.Fatalf("transfer=%+v", entry)
		}
	}
}
