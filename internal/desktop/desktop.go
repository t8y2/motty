// Package desktop is what a remote-desktop protocol offers the workbench,
// whichever protocol it is: a picture that changes in rectangles, and a way to
// send the pointer, keys and clipboard back. A backend (package vnc today, RDP
// next) implements Session; the workbench shows any Session in the same tab, with
// no knowledge of the protocol under it. See docs/remote-desktop.md.
package desktop

import (
	"context"
	"net"
)

// Rect is a region of the remote picture in pixels.
type Rect struct{ X, Y, Width, Height int }

// Empty reports whether the rectangle covers nothing.
func (r Rect) Empty() bool { return r.Width <= 0 || r.Height <= 0 }

// Union is the smallest rectangle holding both.
func (r Rect) Union(other Rect) Rect {
	switch {
	case r.Empty():
		return other
	case other.Empty():
		return r
	}
	left, top := min(r.X, other.X), min(r.Y, other.Y)
	right, bottom := max(r.X+r.Width, other.X+other.Width), max(r.Y+r.Height, other.Y+other.Height)
	return Rect{left, top, right - left, bottom - top}
}

// Buttons is the set of pointer buttons held.
type Buttons uint8

const (
	ButtonLeft Buttons = 1 << iota
	ButtonMiddle
	ButtonRight
)

// Wheel is a scroll of whole notches: positive Y is down (toward newer
// content), positive X is right.
type Wheel struct{ X, Y int }

// Modifiers is a set of held modifier keys.
type Modifiers uint8

const (
	ModShift Modifiers = 1 << iota
	ModControl
	ModAlt
	// ModMeta is the Command key on a Mac, the Windows key elsewhere: what the
	// remote system calls Super, Windows or Command.
	ModMeta
)

// Key names a key that is not a character. Characters are the Rune of a
// KeyEvent.
type Key uint16

const (
	KeyNone Key = iota
	KeyEnter
	KeyTab
	KeyBackspace
	KeyEscape
	KeyDelete
	KeyInsert
	KeyHome
	KeyEnd
	KeyPageUp
	KeyPageDown
	KeyLeft
	KeyRight
	KeyUp
	KeyDown
	KeyF1 // KeyF1..KeyF12 are consecutive
	KeyF2
	KeyF3
	KeyF4
	KeyF5
	KeyF6
	KeyF7
	KeyF8
	KeyF9
	KeyF10
	KeyF11
	KeyF12
	KeyShift
	KeyControl
	KeyAlt
	KeyMeta
	KeyCapsLock
	KeyMenu
)

// KeyEvent is one key going down or up. Exactly one of Key and Rune is set. A
// Rune is the character the key stands for without modifiers applied ("c" for
// Ctrl+C, "a" for what the user typed as "A" with Shift held: the modifiers are
// sent as their own KeyShift, KeyControl events around it), except that
// printable text from an input method arrives as the committed character itself
// (see Session.SendText).
//
// Typed is the character the press types with the modifiers held ("A" for Shift
// and a, "!" for Shift and 1 on a US layout), zero when it is the Rune or the
// system cannot say. A backend that names characters (VNC) sends it instead of
// the Rune; a backend that names keys (RDP) ignores it, the remote applies the
// modifiers itself.
type KeyEvent struct {
	Key   Key
	Rune  rune
	Typed rune
	Down  bool
}

// Info describes a connection for display.
type Info struct {
	// Protocol is "VNC" or "RDP".
	Protocol string
	// Name is what the remote system calls itself, if it says.
	Name string
	// Encrypted reports whether the traffic is protected between here and the
	// server: false for a plain VNC connection, true through an SSH tunnel's or
	// RDP's own encryption.
	Encrypted bool
}

// Presenter receives the picture. ui.Surface implements it.
type Presenter interface {
	// Resize sets the picture's size; its content is undefined until updated.
	Resize(width, height int)
	// Update copies a width x height rectangle of BGRA pixels with its top-left
	// at (x, y), rows stride bytes apart. The fourth byte of a pixel is not
	// meaningful. pix is valid only during the call.
	Update(x, y, width, height int, pix []byte, stride int)
}

// Session is one live connection to a remote desktop. Methods may be called from
// any goroutine except Present, which belongs to the goroutine that paints and
// draws the picture.
type Session interface {
	// Info describes the connection.
	Info() Info
	// Size is the picture's current size, zero until the first Present has run.
	Size() (width, height int)
	// Present brings dst up to date: Resize if the size changed since the last
	// call, then Update for the rectangles that changed, and returns whether it
	// did anything. A backend keeps the full picture and a list of changed
	// rectangles, so the cost of a call follows what changed and not the picture's
	// size.
	Present(dst Presenter) bool
	// Done is closed when the connection has ended; Err then says why (nil for a
	// close requested by Close).
	Done() <-chan struct{}
	Err() error

	// SendPointer reports the pointer at (x, y) of the picture with the buttons
	// held.
	SendPointer(x, y int, buttons Buttons) error
	// SendWheel scrolls at (x, y).
	SendWheel(x, y int, wheel Wheel, buttons Buttons) error
	// SendKey presses or releases a key.
	SendKey(KeyEvent) error
	// SendText types text that is not a key: what an input method committed, a
	// paste. The backend sends it however the protocol types characters.
	SendText(text string) error
	// SetClipboard gives the remote system text for its clipboard.
	SetClipboard(text string) error
	// RemoteClipboard returns the text the remote system last put on its clipboard
	// since the previous call, if any.
	RemoteClipboard() (text string, ok bool)
	// Bells returns how many times the remote system rang its bell since the
	// previous call.
	Bells() int

	// SetActive tells the backend whether anyone can see the picture. While it is
	// false a backend asks for no more picture data.
	SetActive(active bool)
	// Close ends the connection. It is safe to call more than once.
	Close() error
}

// Resizer is a Session whose server changes the desktop's size while the
// session runs, so that the picture can be kept the size of the pane and shown
// pixel for pixel, as a window of the remote machine would be.
type Resizer interface {
	Session
	// CanResize reports whether the server accepts Resize now. A server that
	// does not (or not yet) leaves the picture the size it was opened at.
	CanResize() bool
	// Resize asks for a width x height desktop, in device pixels, whose
	// interface is drawn at scale (see Options.Scale). The new size arrives
	// through Present, when the server has made it.
	Resize(width, height int, scale float32) error
}

// Options are what a backend needs to connect that is not in its own address.
type Options struct {
	// Password is the secret the server asks for, empty if none was saved.
	Password string
	// AskPassword is called while connecting if the server wants a secret and
	// Password is empty. It may block until the user has answered; the same
	// connection then goes on. An error ends the attempt.
	AskPassword func() (string, error)
	// Notify is called, from any goroutine, when the session has something new:
	// a changed picture, a clipboard, the end of the connection. It must not
	// block.
	Notify func()
	// Encrypted says the stream the Dialer returns is already protected, as a
	// channel through an SSH connection is: the backend reports it in Info.
	Encrypted bool
	// Clipboard allows the clipboard to cross in both directions. Off, the
	// backend neither sends the local clipboard nor keeps the remote's.
	Clipboard bool
	// Console asks an RDP server for its console (administrative) session
	// instead of a new one, as mstsc /admin does. Windows decides whether the
	// request is granted: the account still needs the right to log on, and what
	// happens when someone is already at the console is its decision, not the
	// client's. VNC has no such session.
	Console bool
	// Username and Domain are the account a protocol with accounts (RDP) signs
	// in as; VNC has none.
	Username, Domain string
	// Width and Height are the size, in device pixels, of the desktop to ask for
	// when the protocol lets the client choose it (RDP); zero lets the backend
	// choose.
	Width, Height int
	// Scale is the device pixels the client shows per logical pixel (1 for a
	// 100% display, 1.25 for 125%). A backend that can tell the server draws
	// its interface at that scale (RDP), so that text and controls come out the
	// size the user's own display makes them; zero is 1.
	Scale float32
	// VerifyCertificate is called with the DER certificate a server presents over
	// TLS (RDP), after the handshake and before any secret is sent. A server's
	// self-signed certificate cannot be checked against an authority, so the
	// caller decides, typically by pinning: an error ends the attempt. address is
	// what the user typed (the name the pin is kept under), not necessarily where
	// the bytes went. Nil accepts any certificate, which only tests do.
	VerifyCertificate func(address string, der []byte) error
}

// Dialer opens the byte stream to the server: a direct TCP connection, or a
// channel through an SSH connection, or one through a proxy.
type Dialer func(ctx context.Context, network, address string) (net.Conn, error)
