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
	"github.com/vivaui/viva/gfx"
	"motty/internal/serial"
	"motty/internal/zmodem"
)

// zrqinit is what sz prints first: "rz\r" and a ZRQINIT hex header.
const (
	zrqinitHeader = "**\x18B00000000000000\r\x8a\x11"
	zrqinit       = "rz\r" + zrqinitHeader
)

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
	if session.transfer.sending || session.transfer.phase != transferOffered {
		t.Fatalf("transfer = %+v", session.transfer)
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
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTransferChoose); !ok {
		t.Fatal("no way to choose a folder")
	}

	state.Click(hitTransferChoose)
	waitUntil(t, state, "the transfer to end", func() bool { return session.transfer.phase == transferNone })
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

func TestAnIncomingTransferQuickSaveUsesTheConfiguredDownloadFolder(t *testing.T) {
	state, session, shell := transferState(t)
	destination := filepath.Join(t.TempDir(), "ssh-inbox")
	state.sftpDownloadDirectory = destination
	payload := "into the setting"
	sender := zmodem.NewSender([]zmodem.SendFile{
		{Name: "from-server.txt", Size: int64(len(payload)), Source: strings.NewReader(payload), Mode: 0o644},
	}, zmodem.SendOptions{})
	shell.onWrite = func(p []byte) {
		sender.Feed(p)
		shell.say(string(sender.TakeOutput(nil)))
	}
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTransferDownloads); !ok {
		t.Fatal("the offer has no way to save into the configured download folder")
	}
	state.Click(hitTransferDownloads)
	waitUntil(t, state, "the transfer to end", func() bool { return session.transfer.phase == transferNone })
	got, err := os.ReadFile(filepath.Join(destination, "from-server.txt"))
	if err != nil || string(got) != payload {
		t.Fatalf("received file = %q, %v", got, err)
	}
}

func TestAReceivedFileNeverReplacesAnExistingOne(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	os.WriteFile(filepath.Join(dir, "a.txt"), []byte("mine"), 0o644)
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	sender := zmodem.NewSender([]zmodem.SendFile{{Name: "a.txt", Size: 3, Source: strings.NewReader("new")}}, zmodem.SendOptions{})
	shell.onWrite = func(p []byte) { sender.Feed(p); shell.say(string(sender.TakeOutput(nil))) }
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	if kept, _ := os.ReadFile(filepath.Join(dir, "a.txt")); string(kept) != "mine" {
		t.Fatalf("a.txt = %q", kept)
	}
	if added, _ := os.ReadFile(filepath.Join(dir, "a (1).txt")); string(added) != "new" {
		t.Fatalf("a (1).txt = %q", added)
	}
}

func TestDecliningAnOfferTellsTheOtherEndToStop(t *testing.T) {
	state, session, shell := transferState(t)
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferDecline)
	if state.modal != modalNone || session.transfer.phase != transferNone {
		t.Fatalf("modal %d phase %d", state.modal, session.transfer.phase)
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
	state, session, shell := transferState(t)
	shell.say("**bold** \x18 and B00 and \x1b[1m**\x1b[0m\r\n")
	waitUntil(t, state, "the text", func() bool { return strings.Contains(screenText(state, 0), "bold") })
	if state.modal == modalFileTransfer || session.transfer.phase != transferNone {
		t.Fatal("ordinary output started a transfer")
	}
}

func TestSendingFilesWhenTheOtherEndIsReadyToReceive(t *testing.T) {
	state, session, shell := transferState(t)
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
	if !session.transfer.sending {
		t.Fatal("a ZRINIT should offer to send")
	}
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	if got := target.files["up.bin"]; got == nil || !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("the receiver got %v", got)
	}
	if !strings.Contains(screenText(state, 0), "Sent 1 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestARunningTransferKeepsGoingWhenTheRestOfTheWindowIsUsed(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	payload := make([]byte, 400000)
	sender := zmodem.NewSender([]zmodem.SendFile{{Name: "big.bin", Size: int64(len(payload)), Source: bytes.NewReader(payload)}}, zmodem.SendOptions{Window: 2048})
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
		return session.transfer.phase == transferRunning && session.transfer.progress.Name == "big.bin"
	})
	if state.modal != modalNone {
		t.Fatalf("a running transfer took the window: modal %d", state.modal)
	}
	if !session.term.transferHoldsInput {
		t.Fatal("the terminal still accepts input during the transfer")
	}
	before := shell.typed()
	if session.term.send([]byte("ls\n")) || shell.typed() != before {
		t.Fatal("a keystroke was written into the transfer")
	}
	state.Click(hitTransferDismiss)
	state.Click(hitTransferClose)
	state.Click(hitHosts)
	if session.transfer.phase != transferRunning || state.activeSection != sectionHosts {
		t.Fatalf("using the window changed phase %d section %d", session.transfer.phase, state.activeSection)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	cancelID := hitTransferCancelFor.At(int(session.ID))
	var cancel, dismiss bool
	for _, hit := range canvas.Hits() {
		switch hit.ID {
		case cancelID:
			cancel = true
		case hitTransferDismiss:
			dismiss = true
		}
	}
	if !cancel || dismiss {
		t.Fatalf("progress card hits: cancel %v dismiss %v", cancel, dismiss)
	}
	state.Click(cancelID)
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	if session.term.transferHoldsInput {
		t.Fatal("the terminal stayed silent after the transfer")
	}
}

// A receiver repeats its ZRINIT every few seconds until it is answered, so a
// file chosen after a while leaves several copies waiting. Only the newest one
// is still being asked; answering each would send the file header twice, and a
// receiver takes the second for a second file.
func TestARepeatedSendOfferIsAnsweredOnce(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "up.bin")
	payload := []byte("one file, once")
	os.WriteFile(path, payload, 0o644)
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }

	target := &collect{}
	remote := zmodem.NewReceiver(target)
	shell.onWrite = func(p []byte) { remote.Feed(p); shell.say(string(remote.TakeOutput(nil))) }
	remote.Feed([]byte(zrqinit))
	ready := append([]byte(nil), remote.TakeOutput(nil)...)
	if len(ready) == 0 {
		t.Fatal("the receiver did not ask for the file")
	}
	// The user decides slowly enough that the request is repeated.
	shell.say("waiting\r\n" + string(ready) + string(ready))
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })

	var sent bytes.Buffer
	shell.onWrite = func(p []byte) {
		sent.Write(p)
		remote.Feed(p)
		shell.say(string(remote.TakeOutput(nil)))
	}
	state.Click(hitTransferChoose)
	if got := countFileHeaders(sent.Bytes()); got != 1 {
		t.Fatalf("the receiver was told about %d files: %q", got, sent.Bytes())
	}
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	if got := target.files["up.bin"]; got == nil || !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("the receiver got %v", got)
	}
}

// The same from the other side: sz repeats its ZRQINIT while the user decides,
// and answering each copy makes the sender offer the file again, which lands as
// a duplicate download.
func TestARepeatedReceiveOfferIsAnsweredOnce(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	payload := []byte("one file, once")
	sender := zmodem.NewSender([]zmodem.SendFile{
		{Name: "down.bin", Size: int64(len(payload)), Source: bytes.NewReader(payload), Mode: 0o644},
	}, zmodem.SendOptions{})
	shell.onWrite = func(p []byte) { sender.Feed(p); shell.say(string(sender.TakeOutput(nil))) }
	shell.say("rz\r" + zrqinitHeader + zrqinitHeader)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })

	var sent bytes.Buffer
	shell.onWrite = func(p []byte) {
		sent.Write(p)
		sender.Feed(p)
		shell.say(string(sender.TakeOutput(nil)))
	}
	state.Click(hitTransferChoose)
	if got := bytes.Count(sent.Bytes(), []byte("**\x18B01")); got != 1 {
		t.Fatalf("the sender was answered %d times: %q", got, sent.Bytes())
	}
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	if got, err := os.ReadFile(filepath.Join(dir, "down.bin")); err != nil || !bytes.Equal(got, payload) {
		t.Fatalf("down.bin = %q, %v", got, err)
	}
}

// countFileHeaders counts ZFILE headers, the frame that offers one file.
func countFileHeaders(data []byte) int {
	return bytes.Count(data, []byte{'*', 0x18, 'A', 4}) + bytes.Count(data, []byte{'*', 0x18, 'C', 4})
}

func TestCancellingARunningTransferEndsItAndRemovesTheUnfinishedFile(t *testing.T) {
	state, session, shell := transferState(t)
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
		return session.transfer.phase == transferRunning && session.transfer.progress.Name == "big.bin"
	})
	state.Click(hitTransferCancel)
	waitUntil(t, state, "the end", func() bool { return session.transfer.phase == transferNone })
	if entries, _ := os.ReadDir(dir); len(entries) != 0 {
		t.Fatalf("an unfinished file was left behind: %v", entries)
	}
	if !strings.Contains(screenText(state, 0), "cancelled") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestAQuietTransferAsksAgainAndEventuallyGivesUp(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	var now int64
	state.options.Now = func() time.Duration { return time.Duration(now) }
	shell.say(zrqinit)
	waitUntil(t, state, "the offer", func() bool { return state.modal == modalFileTransfer })
	state.Click(hitTransferChoose)
	if session.transfer.phase != transferRunning {
		t.Fatalf("phase = %d", session.transfer.phase)
	}
	for range 20 {
		now += int64(transferQuiet)
		state.Wake()
	}
	if session.transfer.phase != transferNone || !strings.Contains(screenText(state, 0), "failed") {
		t.Fatalf("phase %d, screen %q", session.transfer.phase, screenText(state, 0))
	}
}

func TestAnotherTabCanTransferWhileOneIsAlreadyRunning(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	state.options.Invalidate = func() {}
	streams := &streamWorld{}
	streams.install(state)
	t.Cleanup(func() {
		for index := range state.sessions {
			if state.sessions[index].transfer.phase != transferNone {
				state.abandonTransfer(&state.sessions[index])
			}
		}
	})
	addStreamHost(state, Host{Label: "Console", Address: "/dev/ttyUSB0", Protocol: protocolSerial, Line: serial.Default})
	openHost(state)
	firstID := state.activeSessionID
	waitUntil(t, state, "the first shell", func() bool {
		session, ok := state.sessionByIDPointer(firstID)
		return ok && session.term != nil && session.term.shell != nil
	})
	shell := streams.shells[0]
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	payload := make([]byte, 400000)
	sender := zmodem.NewSender([]zmodem.SendFile{{Name: "big.bin", Size: int64(len(payload)), Source: bytes.NewReader(payload)}}, zmodem.SendOptions{Window: 2048})
	answers := 0
	shell.onWrite = func(p []byte) {
		if answers < 3 {
			answers++
			sender.Feed(p)
			shell.say(string(sender.TakeOutput(nil)))
		}
	}
	shell.say(zrqinit)
	waitUntil(t, state, "the first offer", func() bool { return state.modal == modalFileTransfer && state.modalSession == firstID })
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the first transfer to run", func() bool {
		session, ok := state.sessionByIDPointer(firstID)
		return ok && session.transfer.phase == transferRunning
	})

	addStreamHost(state, Host{Label: "Console 2", Address: "/dev/ttyUSB1", Protocol: protocolSerial, Line: serial.Default})
	state.Click(hitHosts)
	openHost(state)
	secondID := state.activeSessionID
	if secondID == firstID {
		t.Fatal("the second host did not open its own tab")
	}
	waitUntil(t, state, "the second shell", func() bool {
		session, ok := state.sessionByIDPointer(secondID)
		return ok && session.term != nil && session.term.shell != nil
	})
	if len(streams.shells) < 2 {
		t.Fatal("the second shell was not opened")
	}
	streams.shells[1].say(zrqinit)
	waitUntil(t, state, "the second offer", func() bool {
		session, ok := state.sessionByIDPointer(secondID)
		return ok && session.transfer.phase == transferOffered && state.modal == modalFileTransfer && state.modalSession == secondID
	})
	first, ok := state.sessionByIDPointer(firstID)
	if !ok || first.transfer.phase != transferRunning {
		t.Fatalf("the first transfer stopped")
	}
	state.Click(hitTransferChoose)
	waitUntil(t, state, "the second transfer to run", func() bool {
		session, ok := state.sessionByIDPointer(secondID)
		return ok && session.transfer.phase == transferRunning
	})
	first, ok = state.sessionByIDPointer(firstID)
	if !ok || first.transfer.phase != transferRunning {
		t.Fatal("accepting the second transfer stopped the first")
	}
	if state.modal != modalNone {
		t.Fatalf("both transfers running still held a dialog: modal %d", state.modal)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1200, Height: 800})
	firstHit := hitTransferCancelFor.At(int(firstID))
	secondHit := hitTransferCancelFor.At(int(secondID))
	var sawFirst, sawSecond bool
	for _, hit := range canvas.Hits() {
		switch hit.ID {
		case firstHit:
			sawFirst = true
		case secondHit:
			sawSecond = true
		}
	}
	if !sawFirst || !sawSecond {
		t.Fatalf("progress cards: first %v second %v", sawFirst, sawSecond)
	}
}
