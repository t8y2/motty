//go:build windows

package rdp

import (
	"os"
	"sync"
	"syscall"
	"unsafe"
)

// The system chords the host keeps for itself: Alt+Tab, Alt+Esc, Ctrl+Esc
// (and Ctrl+Shift+Esc), the Win key and every chord it starts. mstsc and
// FreeRDP take them the same way, with a low-level keyboard hook that runs
// while the remote-desktop window is the foreground one, eats the chord before
// the host sees it, and forwards the raw scancode to the server.
//
// The hook is installed on the thread that calls SetSystemKeyCapture, which
// the workbench keeps to the platform message loop: Windows delivers a
// low-level hook through that thread's message pump, so the callback runs on
// the same thread as the workbench and needs no lock of its own. Forwarding
// happens on a goroutine of its own, because the callback must return fast
// (the host unhooks anything that stalls it) and a keyboard event can wait on
// the socket.

var (
	user32                   = syscall.NewLazyDLL("user32.dll")
	setWindowsHookExProc     = user32.NewProc("SetWindowsHookExW")
	unhookWindowsHookExProc  = user32.NewProc("UnhookWindowsHookEx")
	callNextHookExProc       = user32.NewProc("CallNextHookEx")
	getForegroundWindowProc  = user32.NewProc("GetForegroundWindow")
	getWindowThreadProcessId = user32.NewProc("GetWindowThreadProcessId")
)

const (
	whKeyboardLL  = 13
	wmKeyDown     = 0x0100
	wmKeyUp       = 0x0101
	wmSysKeyDown  = 0x0104
	wmSysKeyUp    = 0x0105
	llkhfExtended = 0x0001
	// eventsPending bounds the queue between the hook callback and the
	// forwarding goroutine: a live connection drains it in microseconds, and a
	// dead one leaves the keys stuck on the remote either way.
	eventsPending = 128
)

type kbdLLHookStruct struct {
	VkCode      uint32
	ScanCode    uint32
	Flags       uint32
	Time        uint32
	DwExtraInfo uintptr
}

// capturedKey is one keyboard event the hook took for the remote: the RDP
// scancode (0xE000-prefixed when extended) and its direction.
type capturedKey struct {
	code uint16
	down bool
}

// keyCapture is the hook and everything it feeds. The fields without a lock
// belong to the hook's thread (the message loop the hook was installed on).
type keyCapture struct {
	session *Session
	state   chordState
	// held are the keys the hook sent down and has not seen up for, by RDP
	// scancode, so an interrupted capture can release them on the remote.
	held map[uint16]bool
	// events hands captured keys to the forwarding goroutine.
	events chan capturedKey
	// send forwards one key; a nil send drops the key (tests replace it).
	send    func(capturedKey)
	forward sync.WaitGroup
	hook    uintptr
	// proc stays registered for the life of the process: Windows callback
	// handles cannot be released.
	proc uintptr
}

// forwardKey is the default send: the RDP connection.
func (capture *keyCapture) forwardKey(key capturedKey) {
	_ = capture.session.client.SendKeyboard(key.code, key.down)
}

// foregroundIsUs reports whether a window of this process is the foreground
// one: another application's window must keep every key, including its Alt+Tab.
var foregroundIsUs = func() bool {
	hwnd, _, _ := getForegroundWindowProc.Call()
	if hwnd == 0 {
		return false
	}
	var pid uint32
	getWindowThreadProcessId.Call(hwnd, uintptr(unsafe.Pointer(&pid)), 0)
	return pid == uint32(os.Getpid())
}

// setSystemKeyCaptureLocked turns the capture on or off; mu is held. The hook
// is installed on the calling thread, which must pump messages (see
// SetSystemKeyCapture).
func (session *Session) setSystemKeyCaptureLocked(capture bool) {
	if (session.capture != nil) == capture {
		return
	}
	if capture {
		c := &keyCapture{
			session: session,
			held:    make(map[uint16]bool),
			events:  make(chan capturedKey, eventsPending),
		}
		if c.start() {
			session.capture = c
		}
		return
	}
	c := session.capture
	session.capture = nil
	c.stop()
}

// stopSystemKeyCapture takes the capture out, if any, without holding mu while
// the forwarding goroutine drains.
func (session *Session) stopSystemKeyCapture() {
	session.mu.Lock()
	c := session.capture
	session.capture = nil
	session.mu.Unlock()
	if c != nil {
		c.stop()
	}
}

// which is where the hook will call back. It reports whether the hook went in.
func (capture *keyCapture) start() bool {
	capture.proc = syscall.NewCallback(capture.hookProc)
	hook, _, _ := setWindowsHookExProc.Call(whKeyboardLL, capture.proc, 0, 0)
	if hook == 0 {
		return false // without the hook the host keeps its chords, as it does today
	}
	capture.hook = hook
	if capture.send == nil {
		capture.send = capture.forwardKey
	}
	capture.forward.Add(1)
	go capture.drain()
	return true
}

// stop takes the hook out and releases what it still holds on the remote. The
// forwarding goroutine is drained first, so nothing is sent after it returns.
func (capture *keyCapture) stop() {
	unhookWindowsHookExProc.Call(capture.hook)
	for code := range capture.held {
		capture.events <- capturedKey{code: code, down: false}
	}
	close(capture.events)
	capture.forward.Wait()
}

// hookProc is the low-level keyboard hook: on the message-loop thread, for
// every keyboard event of the session.
func (capture *keyCapture) hookProc(nCode int32, wParam uintptr, lParam unsafe.Pointer) uintptr {
	next := func() uintptr {
		r, _, _ := callNextHookExProc.Call(0, uintptr(nCode), wParam, uintptr(lParam))
		return r
	}
	if nCode < 0 {
		return next()
	}
	if wParam != wmKeyDown && wParam != wmKeyUp && wParam != wmSysKeyDown && wParam != wmSysKeyUp {
		return next()
	}
	k := (*kbdLLHookStruct)(lParam)
	if !foregroundIsUs() {
		capture.state.capture(k.VkCode, wParam == wmKeyDown || wParam == wmSysKeyDown)
		return next()
	}
	down := wParam == wmKeyDown || wParam == wmSysKeyDown
	if !capture.state.capture(k.VkCode, down) {
		return next()
	}
	code := uint16(k.ScanCode)
	// The Win keys have no unextended set-1 code: a driver always sends them
	// extended (E0 5B/5C), but an injected event may forget the flag, and a bare
	// 0x5B means a different key to the server.
	extended := k.Flags&llkhfExtended != 0 || k.VkCode == vkLWin || k.VkCode == vkRWin
	if extended {
		code |= 0xE000
	}
	if down {
		capture.held[code] = true
	} else {
		delete(capture.held, code)
	}
	select {
	case capture.events <- capturedKey{code: code, down: down}:
	default:
		// The connection cannot keep up; the release on uninstall, or the
		// session ending, clears what this leaves held on the remote.
	}
	return 1
}

// drain forwards captured keys to the connection. SendKeyboard waits on the
// socket at times, which the hook callback must never do.
func (capture *keyCapture) drain() {
	defer capture.forward.Done()
	for key := range capture.events {
		if capture.send == nil {
			continue
		}
		capture.send(key)
	}
}
