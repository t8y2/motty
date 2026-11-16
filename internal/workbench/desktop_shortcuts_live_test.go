//go:build windows

package workbench

// A live probe of the whole system-shortcut chain on a real window: the workbench
// state in a real viva window, a fake remote desktop that records the keys it is
// sent, real SendInput chords, and the real low-level hook behind the window's
// CaptureSystemShortcuts. It is the machine-level check of what issue #115 made
// Motty take the Windows key, its chords and Alt+Tab for a remote desktop, and it
// answers where a regression sits when the user's shortcuts stop reaching the
// remote again:
//
//   - no capture(true) ask        -> motty never asked (desktopHoldsTheKeyboard)
//   - capture asked, no keys      -> the window ate them but motty dropped them
//   - keys arrived, foreground moved -> the hook did not eat the chord (viva)
//
//   MOTTY_LIVE_SHORTCUTS=1 go test ./internal/workbench -run TestLiveSystemShortcuts -count=1 -v
//
// It presses Win+F2 (harmless locally) and Alt+Tab (focus moves away only if the
// chain is broken), so it runs only on request.

import (
	"context"
	"os"
	"runtime"
	"slices"
	"sync"
	"syscall"
	"testing"
	"time"
	"unsafe"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"motty/internal/desktop"
)

var liveUser32 = syscall.NewLazyDLL("user32.dll")

var (
	liveFindWindow = liveUser32.NewProc("FindWindowW")
	liveSwitchTo   = liveUser32.NewProc("SwitchToThisWindow")
	liveForeground = liveUser32.NewProc("GetForegroundWindow")
	liveSendInput  = liveUser32.NewProc("SendInput")
	liveAsyncKey   = liveUser32.NewProc("GetAsyncKeyState")
)

// liveKeyInput is INPUT for one keyboard event, padded the way the viva hook
// test pads it so the same struct reads true on 64-bit Windows.
type liveKeyInput struct {
	Type  uint32
	_     uint32
	Vk    uint16
	Scan  uint16
	Flags uint32
	Time  uint32
	_     uint32
	Extra uintptr
	_     [8]byte
}

func liveSendKeys(t *testing.T, keys ...struct {
	vk   uint16
	down bool
}) {
	t.Helper()
	for _, key := range keys {
		event := liveKeyInput{Type: 1, Vk: key.vk}
		if !key.down {
			event.Flags = 2 // KEYEVENTF_KEYUP
		}
		if sent, _, _ := liveSendInput.Call(1, uintptr(unsafe.Pointer(&event)), unsafe.Sizeof(event)); sent != 1 {
			t.Fatalf("SendInput refused vk %x", key.vk)
		}
		time.Sleep(40 * time.Millisecond)
	}
}

// recordingDesktop is a followingDesktop that keeps the keys it is sent.
type recordingDesktop struct {
	*followingDesktop
	mu   sync.Mutex
	keys []desktop.KeyEvent
}

func (s *recordingDesktop) SendKey(event desktop.KeyEvent) error {
	s.mu.Lock()
	already := len(s.keys)
	s.keys = append(s.keys, event)
	s.mu.Unlock()
	if os.Getenv("MOTTY_KEY_TRACE") != "" && already < 4 {
		buf := make([]byte, 4096)
		n := runtime.Stack(buf, false)
		println("KEYTRACE SendKey:", int(event.Key), "down:", event.Down, "\n", string(buf[:n]))
	}
	return nil
}

func (s *recordingDesktop) sent() []desktop.KeyEvent {
	s.mu.Lock()
	defer s.mu.Unlock()
	return append([]desktop.KeyEvent(nil), s.keys...)
}

func TestLiveSystemShortcutsReachARemoteDesktop(t *testing.T) {
	if os.Getenv("MOTTY_LIVE_SHORTCUTS") == "" {
		t.Skip("set MOTTY_LIVE_SHORTCUTS=1 to drive a real window with real keys")
	}
	const title = "motty live shortcuts 8f41"
	invalidator := new(app.Invalidator)
	_, options := newWorld(t)
	options.Invalidator = invalidator
	state := newTestState(t, options)
	state.options.Invalidate = invalidator.Invalidate
	state.connectionReconnect = reconnectOff
	desk := &recordingDesktop{followingDesktop: newFollowingDesktop(1600, 900, false)}
	state.dialDesktop = func(context.Context, desktopRequest) (desktop.Session, error) {
		return desk, nil
	}

	// Open the RDP host the way a user does, before the window exists: once it
	// does, the window's thread owns the state, and a foreign goroutine must
	// not touch it. The window's first Wake adopts the dialing session.
	addStreamHost(state, Host{Label: "Win", Address: "win.example.com", User: "alice", Protocol: protocolRDP})
	openHost(state)

	var captureMu sync.Mutex
	var captureAsks []bool
	ready := make(chan *app.Window, 1)
	done := make(chan error, 1)
	go func() {
		done <- app.RunApplication(func(application *app.Application) error {
			var canvas gfx.Canvas
			window, err := application.OpenWindow(app.Config{
				Title: title, Width: 1100, Height: 750,
				Tree: Tree(), Invalidator: invalidator,
				Paint: state.Paint, OnResize: state.Resize,
				OnPointer: state.Pointer, OnKey: state.Key,
				OnShortcut: state.Shortcut, OnScroll: state.Scroll,
				TextInput: func(context app.TextInputContext) *app.TextInput {
					if context.Terminal {
						return state.TerminalInput(context)
					}
					editor := state.EditorFor(context.ID)
					if editor == nil {
						return nil
					}
					return &app.TextInput{Editor: editor, NavigationKeys: context.NavigationKeys}
				},
				OnWake: func() { state.Wake() },
			}, &canvas)
			if err != nil {
				return err
			}
			// Everything below runs on the window's thread before its first
			// repaint, so the state is not shared yet.
			state.SetWindow(window.Frame, func() bool { return !window.Hidden() && !window.Closed() })
			state.SetShortcutCapture(func(want bool) error {
				captureMu.Lock()
				captureAsks = append(captureAsks, want)
				captureMu.Unlock()
				return window.CaptureSystemShortcuts(want)
			})
			ready <- window
			return nil
		})
	}()
	var window *app.Window
	select {
	case window = <-ready:
	case err := <-done:
		t.Fatalf("the application exited early: %v", err)
	case <-time.After(15 * time.Second):
		t.Fatal("the window never opened")
	}
	chordsSent := false
	defer func() {
		if chordsSent {
			for _, vk := range []uintptr{0x5b, 0x5c, 0xa4, 0xa5, 0x09, 0x71} {
				if held, _, _ := liveAsyncKey.Call(vk); held&0x8000 != 0 {
					event := liveKeyInput{Type: 1, Vk: uint16(vk), Flags: 2}
					liveSendInput.Call(1, uintptr(unsafe.Pointer(&event)), unsafe.Sizeof(event))
					time.Sleep(20 * time.Millisecond)
				}
			}
		}
		window.Close()
		select {
		case err := <-done:
			if err != nil {
				t.Errorf("the application failed: %v", err)
			}
		case <-time.After(15 * time.Second):
			t.Error("the application did not exit")
		}
	}()

	waitFor := func(what string, done func() bool) {
		t.Helper()
		deadline := time.Now().Add(15 * time.Second)
		for time.Now().Before(deadline) {
			captureMu.Lock()
			asked := len(captureAsks) > 0 && captureAsks[len(captureAsks)-1]
			captureMu.Unlock()
			if asked && done() {
				return
			}
			time.Sleep(20 * time.Millisecond)
		}
		captureMu.Lock()
		t.Fatalf("timed out waiting for %s (capture asks %v, keys sent %d)", what, captureAsks, len(desk.sent()))
		captureMu.Unlock()
	}
	waitFor("the desktop to be shown", func() bool { width, _ := desk.Size(); return width > 0 })
	before := desk.sent()

	pointer16, err := syscall.UTF16PtrFromString(title)
	if err != nil {
		t.Fatal(err)
	}
	handle, _, _ := liveFindWindow.Call(0, uintptr(unsafe.Pointer(pointer16)))
	if handle == 0 {
		t.Fatal("FindWindow could not find the probe window")
	}
	liveSwitchTo.Call(handle, 1)
	foregroundIsOurs := func() bool { current, _, _ := liveForeground.Call(); return current == handle }
	deadline := time.Now().Add(5 * time.Second)
	for !foregroundIsOurs() && time.Now().Before(deadline) {
		time.Sleep(50 * time.Millisecond)
	}
	if !foregroundIsOurs() {
		t.Skip("this session lets no window take the foreground")
	}
	time.Sleep(400 * time.Millisecond) // let the hook land on the window's thread

	type press = struct {
		vk   uint16
		down bool
	}
	const vkLeftWin, vkAlt, vkTab, vkF2 = 0x5b, 0xa4, 0x09, 0x71

	// Win+F2: harmless to the local shell, so a broken chain leaves no mess.
	chordsSent = true
	liveSendKeys(t, press{vkLeftWin, true}, press{vkF2, true}, press{vkF2, false}, press{vkLeftWin, false})
	time.Sleep(400 * time.Millisecond)
	keys := desk.sent()[len(before):]
	t.Logf("after Win+F2 the desktop was sent: %v", keys)
	captureMu.Lock()
	t.Logf("capture asks so far: %v", captureAsks)
	captureMu.Unlock()
	want := []desktop.KeyEvent{
		{Key: desktop.KeyMeta, Down: true}, {Key: desktop.KeyF1 + 1, Down: true},
		{Key: desktop.KeyF1 + 1}, {Key: desktop.KeyMeta},
	}
	if !slices.Equal(keys, want) {
		t.Errorf("Win+F2 remote keys = %v, want %v", keys, want)
	}

	// Alt+Tab: the chain keeps both the foreground window and sends Tab over.
	afterChord := len(desk.sent())
	liveSendKeys(t, press{vkAlt, true}, press{vkTab, true}, press{vkTab, false}, press{vkAlt, false})
	time.Sleep(400 * time.Millisecond)
	keys = desk.sent()[afterChord:]
	t.Logf("after Alt+Tab the desktop was sent: %v", keys)
	want = []desktop.KeyEvent{
		{Key: desktop.KeyAlt, Down: true}, {Key: desktop.KeyTab, Down: true},
		{Key: desktop.KeyTab}, {Key: desktop.KeyAlt},
	}
	if !slices.Equal(keys, want) {
		t.Errorf("Alt+Tab remote keys = %v, want %v", keys, want)
	}
	if !foregroundIsOurs() {
		t.Error("Alt+Tab moved the focus away: the hook did not eat the chord")
	}
}
