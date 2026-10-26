package workbench

import (
	"bytes"
	"io"
	"math/rand"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/serial"
	"motty/internal/zmodem"
)

// zrqinit is what sz prints first: "rz\r" and a ZRQINIT hex header.
const zrqinit = "rz\r**\x18B00000000000000\r\x8a\x11"

type memWriter struct{ bytes.Buffer }

func (*memWriter) Close() error { return nil }

type collect struct{ files map[string]*memWriter }

func (c *collect) Open(info zmodem.FileInfo) (io.WriteCloser, error) {
	if c.files == nil {
		c.files = map[string]*memWriter{}
	}
	w := &memWriter{}
	c.files[info.Name] = w
	return w, nil
}

// transferState opens a serial tab whose "device" is the shell the test plays
// the remote end on.
func transferState(t *testing.T) (*State, *Session, *fakeShell) {
	t.Helper()
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)
	addStreamHost(state, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial, Line: serial.Default})
	openHost(state)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitUntil(t, state, "the shell", func() bool { return session.term.shell != nil })
	return state, session, streams.shells[0]
}

func TestAnIncomingTransferIsOfferedAndNothingIsSavedUntilTheUserChooses(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }

	random := rand.New(rand.NewSource(1))
	payload := make([]byte, 50000)
	random.Read(payload)
	sender := zmodem.NewSender([]zmodem.SendFile{
		{Name: "../../evil.bin", Size: int64(len(payload)), Source: bytes.NewReader(payload), Mode: 0o644},
		{Name: "second.txt", Size: 5, Source: strings.NewReader("hello")},
	}, zmodem.SendOptions{})
	shell.onWrite = func(p []byte) {
		sender.Feed(p)
		shell.say(string(sender.TakeOutput(nil)))
	}

	shell.say("before\r\n" + zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	if state.transfer.sending || state.transfer.phase != transferOffered {
		t.Fatalf("transfer = %+v", state.transfer)
	}
	if shell.typed() != "" {
		t.Fatalf("the terminal answered before the user chose: %q", shell.typed())
	}
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatal("files appeared before the user chose")
	}
	if !strings.Contains(screenText(state, 0), "before") || strings.Contains(screenText(state, 0), "B00000") {
		t.Fatalf("screen = %q: the header must not be shown", screenText(state, 0))
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTransferChoose); !ok {
		t.Fatal("no way to choose a folder")
	}

	state.Click(hitTransferChoose)
	waitUntil(t, state, "the transfer to end", func() bool { return state.transfer.phase == transferNone })
	got, err := os.ReadFile(filepath.Join(dir, "evil.bin"))
	if err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("evil.bin: %d bytes, %v", len(got), err)
	}
	if second, _ := os.ReadFile(filepath.Join(dir, "second.txt")); string(second) != "hello" {
		t.Fatalf("second.txt = %q", second)
	}
	if _, err := os.Stat(filepath.Join(dir, "..", "evil.bin")); err == nil {
		t.Fatal("a file was written outside the chosen folder")
	}
	if !strings.Contains(screenText(state, 0), "Received 2 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
	// The shell is the terminal's again.
	shell.say("$ ")
	waitUntil(t, state, "the prompt", func() bool { return strings.Contains(screenText(state, 0), "$") })
	_ = session
}

func TestAReceivedFileNeverReplacesAnExistingOne(t *testing.T) {
	state, _, shell := transferState(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine"), 0o644)
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	sender := zmodem.NewSender([]zmodem.SendFile{{Name: "a.txt", Size: 3, Source: strings.NewReader("new")}}, zmodem.SendOptions{})
	shell.onWrite = func(p []byte) { sender.Feed(p); shell.say(string(sender.TakeOutput(nil))) }
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the end", func() bool { return state.transfer.phase == transferNone })
	if kept, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(kept) != "mine" {
		t.Fatalf("a.txt = %q", kept)
	}
	if added, _ := os.ReadFile(filepath.Join(dir, "a (1).txt")); string(added) != "new" {
		t.Fatalf("a (1).txt = %q", added)
	}
}

func TestDecliningAnOfferTellsTheOtherEndToStop(t *testing.T) {
	state, _, shell := transferState(t)
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferDecline)
	if state.modal != modalNone || state.transfer.phase != transferNone {
		t.Fatalf("modal %d phase %d", state.modal, state.transfer.phase)
	}
	if !strings.HasPrefix(shell.typed(), "\x18\x18\x18\x18\x18") {
		t.Fatalf("sent %q", shell.typed())
	}
}

func TestAnOfferSplitAcrossReadsIsStillSeen(t *testing.T) {
	state, _, shell := transferState(t)
	for _, piece := range []string{"x*", "*\x18B0", "00000000000000\r\x8a\x11"} {
		shell.say(piece)
		state.Wake()
	}
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
}

func TestOrdinaryOutputIsNeverTakenForATransfer(t *testing.T) {
	state, _, shell := transferState(t)
	shell.say("**bold** \x18 and B00 and \x1b[1m**\x1b[0m\r\n")
	waitUntil(t, state, "the text", func() bool { return strings.Contains(screenText(state, 0), "bold") })
	if state.modal == modalFileTransfer || state.transfer.phase != transferNone {
		t.Fatal("ordinary output started a transfer")
	}
}

func TestSendingFilesWhenTheOtherEndIsReadyToReceive(t *testing.T) {
	state, _, shell := transferState(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "up.bin")
	payload := bytes.Repeat([]byte("0123456789abcdef"), 5000)
	os.WriteFile(path, payload, 0o644)
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }

	target := &collect{}
	receiver := zmodem.NewReceiver(target)
	shell.onWrite = func(p []byte) { receiver.Feed(p); shell.say(string(receiver.TakeOutput(nil))) }
	receiver.Feed([]byte(zrqinit)) // the receiver wakes up and says ZRINIT
	shell.say("waiting\r\n" + string(receiver.TakeOutput(nil)))
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	if !state.transfer.sending {
		t.Fatal("a ZRINIT should offer to send")
	}
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the end", func() bool { return state.transfer.phase == transferNone })
	if got := target.files["up.bin"]; got == nil || !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("the receiver got %v", got)
	}
	if !strings.Contains(screenText(state, 0), "Sent 1 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestCancellingARunningTransferEndsItAndRemovesTheUnfinishedFile(t *testing.T) {
	state, _, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	payload := make([]byte, 400000)
	sender := zmodem.NewSender([]zmodem.SendFile{{Name: "big.bin", Size: int64(len(payload)), Source: bytes.NewReader(payload)}}, zmodem.SendOptions{Window: 2048})
	// The remote answers only the first exchange, so the transfer stays running.
	answers := 0
	shell.onWrite = func(p []byte) {
		if answers < 3 {
			answers++
			sender.Feed(p)
			shell.say(string(sender.TakeOutput(nil)))
		}
	}
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the transfer to run", func() bool {
		return state.transfer.phase == transferRunning && state.transfer.progress.Name == "big.bin"
	})
	state.Click(hitTransferCancel)
	waitUntil(t, state, "the end", func() bool { return state.transfer.phase == transferNone })
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("an unfinished file was left behind: %v", entries)
	}
	if !strings.Contains(screenText(state, 0), "cancelled") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestAQuietTransferAsksAgainAndEventuallyGivesUp(t *testing.T) {
	state, _, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	var now int64
	state.options.Now = func() time.Duration { return time.Duration(now) }
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	if state.transfer.phase != transferRunning {
		t.Fatalf("phase = %d", state.transfer.phase)
	}
	for range 20 {
		now += int64(transferQuiet)
		state.Wake()
	}
	if state.transfer.phase != transferNone || !strings.Contains(screenText(state, 0), "failed") {
		t.Fatalf("phase %d, screen %q", state.transfer.phase, screenText(state, 0))
	}
}
