package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"strings"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/ui"
	"motty/internal/xymodem"
	"motty/internal/zmodem"
)

// remote wires an engine to the fake shell as the program on the other side.
func remote(shell *fakeShell, engine interface {
	Feed([]byte)
	TakeOutput([]byte) []byte
}) {
	shell.onWrite = func(p []byte) {
		engine.Feed(p)
		if out := engine.TakeOutput(nil); len(out) > 0 {
			shell.say(string(out))
		}
	}
}

func startXY(t *testing.T, state *State, session *Session, mode xymodem.Mode, sending bool) {
	t.Helper()
	state.openXYSetup(session)
	if state.modal != modalXYTransfer {
		t.Fatalf("modal = %d", state.modal)
	}
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, id := range []ui.ID{hitXYProtocolX, hitXYProtocol1K, hitXYProtocolY, hitXYSend, hitXYReceive, hitXYStart} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("setup control %d is not painted", id)
		}
	}
	state.Click([]ui.ID{hitXYProtocolX, hitXYProtocol1K, hitXYProtocolY}[mode])
	if sending {
		state.Click(hitXYSend)
	} else {
		state.Click(hitXYReceive)
	}
	state.Click(hitXYStart)
}

func TestYmodemReceiveIntoAFolderFromTheTerminalMenu(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	state.options.OpenDirectory = func(app.OpenDirectoryOptions) (string, error) { return dir, nil }
	payload := bytes.Repeat([]byte("ymodem "), 3000)
	remote(shell, xymodem.NewSender(xymodem.YMODEM, []zmodem.SendFile{
		{Name: "../up/one.bin", Size: int64(len(payload)), Source: bytes.NewReader(payload)},
		{Name: "two.txt", Size: 3, Source: strings.NewReader("two")},
	}))

	// The menu offers it.
	state.terminalMenu.pointer = ui.Point{X: 600, Y: 300}
	state.openTerminalMenu(session)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTerminalMenuTransfer); !ok {
		t.Fatal("the terminal menu has no file transfer entry")
	}
	state.Click(hitTerminalMenuTransfer)
	if state.modal != modalXYTransfer {
		t.Fatalf("modal = %d", state.modal)
	}
	state.closeModal()

	startXY(t, state, session, xymodem.YMODEM, false)
	waitUntil(t, state, "the transfer to end", func() bool { return state.transfer.phase == transferNone })
	if got, _ := os.ReadFile(filepath.Join(dir, "one.bin")); !bytes.Equal(got, payload) {
		t.Fatalf("one.bin is %d bytes", len(got))
	}
	if got, _ := os.ReadFile(filepath.Join(dir, "two.txt")); string(got) != "two" {
		t.Fatalf("two.txt = %q", got)
	}
	if !strings.Contains(screenText(state, 0), "Received 2 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestXmodemReceiveSavesUnderTheNameTheUserChose(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	chosen := filepath.Join(dir, "boot.img")
	os.WriteFile(chosen, []byte("old contents that are longer than the new ones"), 0o644)
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return chosen, nil }
	data := bytes.Repeat([]byte("x1"), 300)
	remote(shell, xymodem.NewSender(xymodem.XMODEM1K, []zmodem.SendFile{{Name: "ignored", Size: int64(len(data)), Source: bytes.NewReader(data)}}))
	startXY(t, state, session, xymodem.XMODEM1K, false)
	waitUntil(t, state, "the transfer to end", func() bool { return state.transfer.phase == transferNone })
	if got, _ := os.ReadFile(chosen); !bytes.Equal(got, data) {
		t.Fatalf("saved %d bytes, want %d", len(got), len(data))
	}
}

func TestYmodemSendToTheOtherSide(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	path := filepath.Join(dir, "firmware.bin")
	payload := bytes.Repeat([]byte{1, 2, 3, 4, 5}, 4000)
	os.WriteFile(path, payload, 0o644)
	state.options.OpenFiles = func(app.OpenFileOptions) ([]string, error) { return []string{path}, nil }
	target := &collect{}
	receiver := xymodem.NewReceiver(xymodem.YMODEM, "", target)
	remote(shell, receiver)
	shell.say(string(receiver.TakeOutput(nil))) // rb asks with 'C'
	startXY(t, state, session, xymodem.YMODEM, true)
	waitUntil(t, state, "the transfer to end", func() bool { return state.transfer.phase == transferNone })
	if got := target.files["firmware.bin"]; got == nil || !bytes.Equal(got.Bytes(), payload) {
		t.Fatalf("the other side got %v", got)
	}
	if !strings.Contains(screenText(state, 0), "Sent 1 files") {
		t.Fatalf("screen = %q", screenText(state, 0))
	}
}

func TestCancellingAnXmodemReceiveRemovesTheUnfinishedFile(t *testing.T) {
	state, session, shell := transferState(t)
	dir := t.TempDir()
	chosen := filepath.Join(dir, "partial.bin")
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return chosen, nil }
	data := bytes.Repeat([]byte("z"), 5000)
	sender := xymodem.NewSender(xymodem.XMODEM, []zmodem.SendFile{{Name: "n", Size: int64(len(data)), Source: bytes.NewReader(data)}})
	answers := 0
	shell.onWrite = func(p []byte) {
		if answers < 6 { // the sender goes quiet part way
			answers++
			sender.Feed(p)
			shell.say(string(sender.TakeOutput(nil)))
		}
	}
	startXY(t, state, session, xymodem.XMODEM, false)
	waitUntil(t, state, "some data", func() bool { return state.transfer.progress.Done > 0 })
	state.Click(hitTransferCancel)
	waitUntil(t, state, "the end", func() bool { return state.transfer.phase == transferNone })
	if _, err := os.Stat(chosen); err == nil {
		t.Fatal("a cancelled XMODEM receive left its file")
	}
}

func TestSetupIsRefusedWhileATransferRuns(t *testing.T) {
	state, session, _ := transferState(t)
	state.transfer.phase = transferRunning
	state.openXYSetup(session)
	if state.modal == modalXYTransfer {
		t.Fatal("a second transfer was offered")
	}
}
