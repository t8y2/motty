package workbench

import (
	"bytes"
	"context"
	"errors"
	"io"
	"os"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"motty/internal/ai/gate"
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

// The remote keeps printing "rz waiting to receive" until the file is finished,
// so a live transfer and a sender that never connected look the same on the
// screen. Progress has to say which one it is while the call is still running.
func TestRZSZProgressShowsBytesWhileTheScreenStillWaits(t *testing.T) {
	const (
		hold = iota
		header
		pause
		run
	)
	payload := bytes.Repeat([]byte("x"), 48<<10)
	state, session, shell := transferState(t)
	session.term.live = true
	dir := t.TempDir()
	source := filepath.Join(dir, "note.bin")
	if err := os.WriteFile(source, payload, 0o644); err != nil {
		t.Fatal(err)
	}
	received := &bytes.Buffer{}
	remoteReceive := zmodem.NewReceiver(&collectBuffer{buf: received, name: new(string)})
	var holdMu sync.Mutex
	stage := hold
	var pending []byte
	shell.onWrite = func(p []byte) {
		holdMu.Lock()
		defer holdMu.Unlock()
		if stage == hold {
			return
		}
		remoteReceive.Feed(p)
		out := remoteReceive.TakeOutput(nil)
		switch stage {
		case header:
			if len(out) == 0 {
				return
			}
			stage = pause
			shell.say(string(out))
		case pause:
			pending = append(pending, out...)
		default:
			if len(out) > 0 {
				shell.say(string(out))
			}
		}
	}

	var reportMu sync.Mutex
	var reports []gate.ProgressReport
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ctx = gate.WithProgress(ctx, func(report gate.ProgressReport) {
		reportMu.Lock()
		reports = append(reports, report)
		reportMu.Unlock()
	})
	errCh := make(chan error, 1)
	go func() {
		_, err := state.ai.backend.TransferRZSZ(ctx, tools.RZSZRequest{
			Upload: true, LocalPath: source, TimeoutSeconds: 30,
		})
		errCh <- err
	}()

	waitUntil(t, state, "rz armed", func() bool { return session.rzsz != nil })
	waitForRZSZReport(t, &reportMu, &reports, func(report gate.ProgressReport) bool {
		return report.Progress == 0 && strings.Contains(report.Message, "waiting for the remote") && strings.Contains(report.Message, "0 bytes")
	})
	snap, ok := session.rzsz.load()
	if !ok || snap.Phase != gate.RZSZPhaseWaiting || snap.BytesDone != 0 || snap.BytesTotal != int64(len(payload)) {
		t.Fatalf("before the remote accepts: %+v ok=%v", snap, ok)
	}

	holdMu.Lock()
	stage = header
	holdMu.Unlock()
	shell.say(zrinit)
	waitUntil(t, state, "bytes moving", func() bool {
		if session.rzsz == nil {
			return false
		}
		snap, ok := session.rzsz.load()
		return ok && snap.Phase == gate.RZSZPhaseTransferring && snap.BytesDone > 0
	})
	moving := waitForRZSZReport(t, &reportMu, &reports, func(report gate.ProgressReport) bool {
		return report.Progress > 0 && strings.Contains(report.Message, "transferring")
	})
	if moving.Total != float64(len(payload)) || !moving.HasTotal {
		t.Fatalf("progress total: %+v", moving)
	}
	statusDone := make(chan struct{})
	var status gate.RZSZSnapshot
	var statusActive bool
	var statusErr error
	go func() {
		status, statusActive, statusErr = state.ai.backend.RZSZStatus(context.Background(), "")
		close(statusDone)
	}()
	waitUntil(t, state, "rzsz_status", func() bool {
		select {
		case <-statusDone:
			return true
		default:
			return false
		}
	})
	if statusErr != nil || !statusActive || status.Phase != gate.RZSZPhaseTransferring || status.BytesDone == 0 {
		t.Fatalf("status %+v active %v err %v", status, statusActive, statusErr)
	}

	holdMu.Lock()
	stage = run
	buf := append([]byte(nil), pending...)
	pending = nil
	holdMu.Unlock()
	if len(buf) > 0 {
		shell.say(string(buf))
	}
	waitUntil(t, state, "upload finished", func() bool {
		select {
		case err := <-errCh:
			if err != nil {
				t.Fatal(err)
			}
			return true
		default:
			return false
		}
	})
	if !bytes.Equal(received.Bytes(), payload) {
		t.Fatalf("remote received %d bytes, want %d", received.Len(), len(payload))
	}
	idle := make(chan bool, 1)
	go func() {
		_, active, err := state.ai.backend.RZSZStatus(context.Background(), "")
		if err != nil {
			idle <- true
			return
		}
		idle <- active
	}()
	var stillActive bool
	waitUntil(t, state, "rzsz_status idle", func() bool {
		select {
		case stillActive = <-idle:
			return true
		default:
			return false
		}
	})
	if stillActive {
		t.Fatal("rzsz_status still reports a transfer after it finished")
	}
}

func waitForRZSZReport(t *testing.T, mu *sync.Mutex, reports *[]gate.ProgressReport, match func(gate.ProgressReport) bool) gate.ProgressReport {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		mu.Lock()
		for _, report := range *reports {
			if match(report) {
				mu.Unlock()
				return report
			}
		}
		mu.Unlock()
		time.Sleep(10 * time.Millisecond)
	}
	mu.Lock()
	defer mu.Unlock()
	t.Fatalf("progress reports: %+v", *reports)
	return gate.ProgressReport{}
}

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

// TestAnArmedRZSZOwnsTheShell pins that a job waiting for the remote program
// holds the shell the way a running transfer does, and gives it back after.
func TestAnArmedRZSZOwnsTheShell(t *testing.T) {
	state, session, shell := transferState(t)
	session.term.live = true
	dir := t.TempDir()
	source := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	errCh := make(chan error, 1)
	go func() {
		_, err := state.ai.backend.TransferRZSZ(context.Background(), tools.RZSZRequest{
			Upload: true, LocalPath: source, TimeoutSeconds: 30,
		})
		errCh <- err
	}()
	waitUntil(t, state, "rz armed", func() bool { return session.rzsz != nil })
	if !session.term.transferHoldsInput {
		t.Fatal("an armed job does not hold the shell")
	}
	session.term.send([]byte("typed"))
	if strings.Contains(shell.typed(), "typed") {
		t.Fatal("typing reached the shell while rz was armed")
	}
	state.openXYSetup(session)
	if state.modal == modalXYTransfer {
		t.Fatal("the X/YMODEM dialog opened while rz was armed")
	}
	state.abortRZSZ(session.rzsz, errors.New("test is over"))
	select {
	case err := <-errCh:
		if err == nil {
			t.Fatal("an aborted call reported success")
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the call did not finish after the abort")
	}
	if session.rzsz != nil || session.term.transferHoldsInput {
		t.Fatal("the abort did not give the shell back")
	}
}

// TestACancelledClientLeavesTheRZSZTransferRunning pins the contract the tools
// state: a client timeout is not a failure, so the job keeps running and
// rzsz_status can still find it.
func TestACancelledClientLeavesTheRZSZTransferRunning(t *testing.T) {
	state, session, _ := transferState(t)
	session.term.live = true
	dir := t.TempDir()
	source := filepath.Join(dir, "note.txt")
	if err := os.WriteFile(source, []byte("payload"), 0o644); err != nil {
		t.Fatal(err)
	}
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	errCh := make(chan error, 1)
	go func() {
		_, err := state.ai.backend.TransferRZSZ(ctx, tools.RZSZRequest{
			Upload: true, LocalPath: source, TimeoutSeconds: 30,
		})
		errCh <- err
	}()
	waitUntil(t, state, "rz armed", func() bool { return session.rzsz != nil })
	cancel()
	select {
	case err := <-errCh:
		if !errors.Is(err, context.Canceled) {
			t.Fatalf("cancelled call = %v", err)
		}
	case <-time.After(5 * time.Second):
		t.Fatal("the cancelled call did not return")
	}
	if session.rzsz == nil {
		t.Fatal("a cancelled client ended the armed job")
	}
	state.abortRZSZ(session.rzsz, errors.New("test is over"))
}
