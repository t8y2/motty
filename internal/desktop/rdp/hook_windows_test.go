//go:build windows

package rdp

import (
	"runtime"
	"syscall"
	"testing"
	"time"
	"unsafe"
)

var (
	postThreadMessageProc = user32.NewProc("PostThreadMessageW")
	getMessageProc        = user32.NewProc("GetMessageW")
	sendInputProc         = user32.NewProc("SendInput")
	getCurrentThreadId    = syscall.NewLazyDLL("kernel32.dll").NewProc("GetCurrentThreadId")
)

const (
	wmQuit            = 0x0012
	inputKeyboard     = 1
	keyEventfUp       = 0x0002
	keyEventfExtended = 0x0001
)

// keyboardInput is Win32's INPUT for one keyboard event: a DWORD type, the
// padding that aligns the union, the KEYBDINPUT fields, and the tail padding
// that brings it to the size of MOUSEINPUT, the largest union arm.
type keyboardInput struct {
	Type        uint32
	_           uint32
	WVk         uint16
	WScan       uint16
	DwFlags     uint32
	Time        uint32
	_           uint32
	DwExtraInfo uintptr
	_           [8]byte
}

// sendInput presses or releases a key by its virtual-key code and set-1
// scancode, the way the keyboard driver would: an extended key carries its
// flag, without which the hook would see it unextended. It reports whether the
// event was injected.
func sendInput(vk, scan uint16, extended, up bool) bool {
	flags := uint32(0)
	if extended {
		flags |= keyEventfExtended
	}
	if up {
		flags |= keyEventfUp
	}
	event := keyboardInput{Type: inputKeyboard, WVk: vk, WScan: scan, DwFlags: flags}
	n, _, _ := sendInputProc.Call(1, uintptr(unsafe.Pointer(&event)), unsafe.Sizeof(event))
	return n == 1
}

// TestHookTakesSystemChords injects real keyboard events through SendInput and
// expects the hook to take the host's chords and nothing else: the Win key and
// its chord, not the plain key beside them.
func TestHookTakesSystemChords(t *testing.T) {
	// The test process is usually not the foreground one; the production
	// check is exercised by hand on a real window.
	outer := foregroundIsUs
	foregroundIsUs = func() bool { return true }
	t.Cleanup(func() { foregroundIsUs = outer })

	forwarded := make(chan capturedKey, 16)
	capture := &keyCapture{
		held:   make(map[uint16]bool),
		events: make(chan capturedKey, eventsPending),
		send:   func(key capturedKey) { forwarded <- key },
	}

	// The hook is installed on a thread of its own that pumps messages,
	// the way the platform message loop would.
	started := make(chan bool, 1)
	pumpDone := make(chan struct{})
	var thread uintptr
	go func() {
		runtime.LockOSThread()
		ok := capture.start()
		started <- ok
		if !ok {
			close(pumpDone)
			return
		}
		thread, _, _ = getCurrentThreadId.Call()
		var msg [6]uintptr
		for {
			n, _, _ := getMessageProc.Call(uintptr(unsafe.Pointer(&msg[0])), 0, 0, 0)
			if int32(n) <= 0 {
				break
			}
		}
		close(pumpDone)
	}()
	if !<-started {
		t.Skip("the keyboard hook could not be installed")
	}
	defer func() {
		capture.stop()
		postThreadMessageProc.Call(thread, wmQuit, 0, 0)
		<-pumpDone
	}()

	if !sendInput(0x5B, 0x5B, true, false) { // left Win down, an extended key
		t.Skip("SendInput is unavailable in this session")
	}
	sendInput(0x52, 0x13, false, false) // R down
	sendInput(0x52, 0x13, false, true)  // R up
	sendInput(0x5B, 0x5B, true, true)   // Win up
	// A plain key the hook must leave with the host.
	sendInput(0x41, 0x1E, false, false) // A down
	sendInput(0x41, 0x1E, false, true)  // A up

	want := []capturedKey{
		{code: 0xE05B, down: true},  // Win down
		{code: 0x13, down: true},    // R down
		{code: 0x13, down: false},   // R up
		{code: 0xE05B, down: false}, // Win up
	}
	for index, expected := range want {
		select {
		case got := <-forwarded:
			if got != expected {
				t.Fatalf("event %d: got 0x%04X down=%v, want 0x%04X down=%v",
					index, got.code, got.down, expected.code, expected.down)
			}
		case <-time.After(5 * time.Second):
			t.Fatalf("event %d (0x%04X) never arrived", index, expected.code)
		}
	}
	// The plain A must not have been taken.
	select {
	case got := <-forwarded:
		t.Fatalf("the hook took a key it should leave: 0x%04X down=%v", got.code, got.down)
	case <-time.After(300 * time.Millisecond):
	}
	if len(capture.held) != 0 {
		t.Fatalf("keys left held on the remote: %v", capture.held)
	}
}
