// Package desktop is what a remote-desktop protocol offers the workbench,
// whichever protocol it is: a picture that changes in rectangles, and a way to
// send the pointer, keys and clipboard back. A backend (package vnc today, RDP
// next) implements Session; the workbench shows any Session in the same tab, with
// no knowledge of the protocol under it. See docs/remote-desktop.md.
package desktop

import (
	"context"
	"errors"
	"net"
	"time"
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
//
// Repeat marks a press that repeats a key the remote already has down: the
// local system's auto-repeat. A backend whose server repeats a held key by
// itself ignores it; one whose server waits for the client (RDP) sends the key
// down again and says that it was already down.
type KeyEvent struct {
	Key    Key
	Rune   rune
	Typed  rune
	Down   bool
	Repeat bool
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

// FileClipboard is a Session that can give files from this computer to the
// remote system's clipboard, so that the remote user pastes them: RDP does, as
// a clipboard file transfer; VNC has no such transfer and does not implement it.
type FileClipboard interface {
	Session
	// SetClipboardFiles makes the files and folders at paths what the remote
	// clipboard holds. The list is made before it returns, which walks folders, so
	// a caller does not call it from a goroutine that paints. What the remote then
	// pastes is read from this computer when it does. The error is one of the
	// Err values below or a failure of the connection; the FileOffer comes back
	// with it so the path that caused it can be named.
	SetClipboardFiles(paths []string) (FileOffer, error)
}

// FileReceiver is a Session that can take files the remote system put on its
// clipboard. The files are saved beside this computer's own clipboard, so the
// user can paste them where they are working.
type FileReceiver interface {
	Session
	// RemoteFiles returns the files the remote clipboard delivered since the
	// previous call, if any. Paths names the top-level entries to put on the
	// local clipboard; Err says why none or only some arrived.
	RemoteFiles() (ReceivedFiles, bool)
}

// ReceivedFiles is a remote file list saved on this computer.
type ReceivedFiles struct {
	// Paths are the top-level files and folders, absolute and local.
	Paths []string
	// Files and Folders count what arrived; Bytes is their total size.
	Files, Folders int
	Bytes          uint64
	// Skipped counts entries that could not be saved.
	Skipped int
	// Err reports a failure, including one that left only some entries saved.
	Err error
}

// FileOffer says what SetClipboardFiles put on the remote clipboard.
type FileOffer struct {
	// Files and Folders count what is offered; Bytes is the total size of the files.
	Files, Folders int
	Bytes          uint64
	// Skipped counts what could not be offered (links, devices, files the server
	// cannot take, unreadable entries) and FirstSkipped names the first. With an
	// error, FirstSkipped names the path that caused it.
	Skipped      int
	FirstSkipped string
}

// Why SetClipboardFiles refuses.
var (
	// ErrFilesUnsupported: the server does not take files through the clipboard.
	ErrFilesUnsupported = errors.New("the remote system does not take files through the clipboard")
	// ErrClipboardOff: the host shares no clipboard.
	ErrClipboardOff = errors.New("clipboard sharing is off for this host")
	// ErrClipboardNotReady: the clipboard channel is still starting.
	ErrClipboardNotReady = errors.New("the clipboard channel is not ready")
	// ErrNothingToOffer: none of the paths can be offered.
	ErrNothingToOffer = errors.New("there is nothing to offer")
	// ErrTooManyFiles: the selection is larger than one offer holds (MaxOfferFiles).
	ErrTooManyFiles = errors.New("the selection holds too many files")
	// ErrPathTooLong: a path inside the selection does not fit what the remote can name.
	ErrPathTooLong = errors.New("a path in the selection is too long")
)

// MaxOfferFiles is the most files and folders one offer holds, as RDP counts them.
const MaxOfferFiles = 20000

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

// LatencySource reports the remote server's last network RTT measurement.
// A zero timestamp means the server has not supplied a measurement.
type LatencySource interface {
	NetworkRTT() (time.Duration, time.Time)
}

// PointerKind is what a session's remote system last said about its pointer.
type PointerKind uint8

const (
	// PointerNone: the session has not named a pointer shape at all.
	PointerNone PointerKind = iota
	// PointerHidden: the remote system hides its pointer (full-screen video
	// and the like); the viewer hides its own and paints nothing.
	PointerHidden
	// PointerSystem: the remote asks for the local system's ordinary pointer.
	PointerSystem
	// PointerBitmap: the remote paints its pointer with PointerShape's bitmap.
	PointerBitmap
)

// PointerShape is a pointer as the remote system paints it: BGRA rows, top
// down, the color not multiplied by alpha, with the hotspot that names which
// pixel of it is the pointer's point.
type PointerShape struct {
	Width, Height int
	HotX, HotY    int
	BGRA          []byte
}

// PointerSource is a Session whose remote system names its pointer's shape.
// The workbench has the system draw the bitmap as the local pointer over the
// picture, the way a native client of that system would.
type PointerSource interface {
	Session
	// PointerShape returns what the remote last said about its pointer, the
	// shape itself when the kind is PointerBitmap, and a version that changes
	// whenever the answer does. The BGRA bytes belong to the caller: a session
	// makes new ones for every shape and never writes to them again.
	PointerShape() (PointerKind, PointerShape, int)
}

// Options are what a backend needs to connect that is not in its own address.
type Options struct {
	// Password is the secret the server asks for, empty if none was saved.
	Password string
	// AskPassword is called while connecting if the server wants a secret and
	// Password is empty. It may block until the user has answered; the same
	// connection then goes on. An error ends the attempt.
	AskPassword func() (string, error)
	// AskCredentials obtains a VNC RSA-AES account and its password, separately
	// from a legacy VNC password.
	AskCredentials func() (username, password string, err error)
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
	// Username and Domain are the account a protocol with accounts signs in as.
	// VNC account authentication uses Username; Domain belongs to RDP.
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
	// VerifyCertificate checks an RDP server's DER certificate before any
	// secret is sent. address is the user-entered name used for pinning.
	// A callback error ends the connection. Nil is only used by tests.
	VerifyCertificate func(address string, der []byte) error
	// VerifyServerKey checks a RealVNC server's canonical PKIX RSA public key
	// before the client sends credentials. Nil does not trust an RSA-AES server.
	VerifyServerKey func(address string, publicKeyDER []byte) error
}

// Dialer opens the byte stream to the server: a direct TCP connection, or a
// channel through an SSH connection, or one through a proxy.
type Dialer func(ctx context.Context, network, address string) (net.Conn, error)
