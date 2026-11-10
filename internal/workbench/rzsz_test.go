package workbench

import (
	"bytes"
	"context"
	"io"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"motty/internal/ai/tools"
	"motty/internal/shellkind"
	"motty/internal/zmodem"
)

// zrinit is the header lrzsz rz sends while it waits for a file.
const zrinit = "**\x18B0100000023be50\r\x8a\x11"

func TestRZSZCommandQuoting(t *testing.T) {
	got, err := rzszShellCommand(true, "a'b", false)
	if err != nil || got != `cd -- 'a'\''b' && rz -b -p` {
		t.Fatalf("upload command %q, %v", got, err)
	}
	if got, err = rzszShellCommand(true, "", true); err != nil || got != "rz -b -y" {
		t.Fatalf("overwrite command %q, %v", got, err)
	}
	if got, err = rzszShellCommand(false, "my file", false); err != nil || got != "sz -b -- 'my file'" {
		t.Fatalf("download command %q, %v", got, err)
	}
	if _, err = rzszShellCommand(false, "bad\nname", false); err == nil {
		t.Fatal("a newline in the remote path was accepted")
	}
}

func TestRZSZRefusesTheWrongShellAndARelativePath(t *testing.T) {
	state, session, shell := transferState(t)
	session.term.live = true
	session.term.shell = kindShell{shell, shellkind.PowerShell}
	_, err := state.beginRZSZ(session, tools.RZSZRequest{Upload: true, LocalPath: `C:\a.txt`})
	if err == nil || !strings.Contains(err.Error(), "POSIX") {
		t.Fatalf("powershell: %v", err)
	}
	session.term.shell = shell
	_, err = state.beginRZSZ(session, tools.RZSZRequest{Upload: true, LocalPath: "note.txt"})
	if err == nil || !strings.Contains(err.Error(), "absolute") {
		t.Fatalf("relative: %v", err)
	}
}

func TestRZSZUploadAndDownloadRoundTrip(t *testing.T) {
	payload := []byte("payload-1")
	state, session, shell := transferState(t)
	session.term.live = true
	dir := t.TempDir()
	source := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	received := &bytes.Buffer{}
	remoteReceive := zmodem.NewReceiver(&collectBuffer{buf: received, name: new(string)})
	var announced bool
	shell.onWrite = func(p []byte) {
		if !announced && bytes.Contains(p, []byte("rz -b")) {
			announced = true
			shell.say(zrinit)
			return
		}
		remoteReceive.Feed(p)
		if out := remoteReceive.TakeOutput(nil); len(out) > 0 {
			shell.say(string(out))
		}
	}
	waitRZSZ(t, state, func() error {
		_, err := state.ai.backend.TransferRZSZ(context.Background(), tools.RZSZRequest{
			Upload: true, LocalPath: source, TimeoutSeconds: 30,
		})
		return err
	})
	if !bytes.Equal(received.Bytes(), payload) {
		t.Fatalf("remote received %q", received.Bytes())
	}
	if typed := shell.typed(); !strings.Contains(typed, "rz -b -p\n") {
		t.Fatalf("typed %q", typed)
	}

	destDir := filepath.Join(dir, "down")
	if err := os.Mkdir(destDir, 0o755); err != nil {
		t.Fatal(err)
	}
	remoteSend := zmodem.NewSender([]zmodem.SendFile{{
		Name: "note.txt", Size: int64(len(payload)), Source: bytes.NewReader(payload),
	}}, zmodem.SendOptions{})
	announced = false
	shell.onWrite = func(p []byte) {
		if !announced && bytes.Contains(p, []byte("sz -b")) {
			announced = true
			shell.say(zrqinit)
			return
		}
		remoteSend.Feed(p)
		if out := remoteSend.TakeOutput(nil); len(out) > 0 {
			shell.say(string(out))
		}
	}
	waitRZSZ(t, state, func() error {
		status, err := state.ai.backend.TransferRZSZ(context.Background(), tools.RZSZRequest{
			LocalPath: destDir, Remote: "note.txt", TimeoutSeconds: 30,
		})
		if err != nil {
			return err
		}
		if status.Bytes != int64(len(payload)) || status.RemoteName != "note.txt" {
			t.Errorf("status %+v", status)
		}
		return nil
	})
	got, err := os.ReadFile(filepath.Join(destDir, "note.txt"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("downloaded %q, %v", got, err)
	}
	if _, err := os.Stat(filepath.Join(destDir, "note.txt.motty-partial")); !os.IsNotExist(err) {
		t.Fatal("partial file was left behind")
	}
}

type collectBuffer struct {
	buf  *bytes.Buffer
	name *string
}

func (c *collectBuffer) Open(info zmodem.FileInfo) (io.WriteCloser, error) {
	*c.name = info.Name
	return nopBuf{c.buf}, nil
}

type nopBuf struct{ *bytes.Buffer }

func (nopBuf) Close() error { return nil }

func waitRZSZ(t *testing.T, state *State, run func() error) {
	t.Helper()
	errCh := make(chan error, 1)
	go func() { errCh <- run() }()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatal(err)
			}
			return
		default:
		}
		time.Sleep(2 * time.Millisecond)
	}
	t.Fatal("timed out waiting for rz/sz")
}
