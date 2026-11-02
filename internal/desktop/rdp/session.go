// Package rdp implements desktop.Session over the RDP client of package
// rdpcore (a trimmed copy of an MIT-licensed library, see
// docs/remote-desktop.md).
package rdp

import (
	"context"
	"errors"
	"fmt"
	"math"
	"net"
	"strconv"
	"sync"
	"time"
	"unicode/utf16"

	"motty/internal/desktop"
	rdpcore "motty/internal/rdpcore"
)

// ErrAuthFailed is returned by Open when the server refused the account or the
// password: the caller asks the user and opens again.
var ErrAuthFailed = errors.New("rdp: the server refused the account or password")

// ErrAuthRequired is returned by Open when there is no password and no way to
// ask for one.
var ErrAuthRequired = errors.New("rdp: a password is required")

// Defaults of the picture a session asks for when the caller gives no size:
// the client chooses the size when it connects. The workbench gives the size of
// its pane and follows it through Resize; this is only for a caller that does
// not.
const (
	DefaultWidth  = 1600
	DefaultHeight = 900
	// minSize is the smallest side the server's display channel accepts.
	minSize = 200
	// maxSize bounds what a caller may ask for, and what a server may hand
	// back: the largest desktop the frame buffer is built for.
	maxSize = rdpcore.MaxDesktopSize
	// handshakeTimeout bounds everything up to the first picture: dialing, the
	// security negotiation, authentication and the capability exchange.
	handshakeTimeout = 45 * time.Second
	// maxTyped bounds a SendText, which types a character at a time.
	maxTyped = 16 << 10
)

// Session is a desktop.Session on an RDP connection.
type Session struct {
	client *rdpcore.Client
	// closeTransport closes the connection the library last dialed: when the
	// library ends a connection on its own account it leaves the socket open.
	closeTransport func()
	frame          *desktop.Frame
	opts           desktop.Options
	info           desktop.Info

	done chan struct{}

	mu      sync.Mutex
	err     error
	closed  bool
	remote  string
	hasText bool
	// buttons are the pointer buttons the server believes are down, so a change
	// is sent as the press or release it is; shift says whether Shift is held, so
	// a character that needs it on a US keyboard can add it by itself.
	buttons desktop.Buttons
	shift   bool
	// buffer is the receive goroutine's scratch for converted pixels.
	buffer []byte
}

var _ desktop.Session = (*Session)(nil)

// Open connects to address through dial and starts reading the server. It
// returns ErrAuthFailed when the server refused the account, and
// ErrAuthRequired when there is no password to try.
func Open(ctx context.Context, dial desktop.Dialer, address string, opts desktop.Options) (*Session, error) {
	host, portText, err := net.SplitHostPort(address)
	if err != nil {
		return nil, fmt.Errorf("rdp: address %q: %w", address, err)
	}
	port, err := strconv.Atoi(portText)
	if err != nil {
		return nil, fmt.Errorf("rdp: address %q: bad port", address)
	}
	if opts.Password == "" && opts.AskPassword == nil {
		return nil, ErrAuthRequired
	}
	width, height := opts.Width, opts.Height
	if width <= 0 || height <= 0 {
		width, height = DefaultWidth, DefaultHeight
	}
	width, height = clampSize(width, height)

	session := &Session{opts: opts, done: make(chan struct{})}
	session.frame = desktop.NewFrame(opts.Notify)
	session.frame.Resize(width, height)

	var connMu sync.Mutex
	var current net.Conn
	setDeadline := func(deadline time.Time) {
		connMu.Lock()
		defer connMu.Unlock()
		if current != nil {
			current.SetDeadline(deadline)
		}
	}
	config := rdpcore.DefaultOptions()
	config.Host, config.Port = host, port
	config.Username, config.Domain, config.Password = opts.Username, opts.Domain, opts.Password
	config.Width, config.Height = uint16(width), uint16(height)
	if percent := scalePercent(opts.Scale); percent != 100 {
		config.DesktopScaleFactor = percent
	}
	config.Clipboard = opts.Clipboard
	config.ConsoleSession = opts.Console
	config.GFX = true
	config.NoAVC = true // no H.264 decoder: the server then sends RemoteFX or ClearCodec
	handshakeEnds := time.Now().Add(handshakeTimeout)
	config.Dial = func(dialCtx context.Context, network, _ string) (net.Conn, error) {
		// The address the library asks for is the host's own name; the caller says
		// where the bytes really go (the SSH server's loopback, a proxy).
		conn, err := dial(dialCtx, network, address)
		if err != nil {
			return nil, err
		}
		conn.SetDeadline(handshakeEnds)
		connMu.Lock()
		current = conn
		connMu.Unlock()
		return conn, nil
	}
	// The user is asked while the handshake waits, so its timeout is suspended
	// for the question. The certificate comes first and the password after it,
	// as with an SSH host key: nothing secret is sent to a server that was not
	// accepted, and the library reads the password only when it authenticates.
	asking := func(ask func() error) error {
		setDeadline(time.Time{})
		err := ask()
		handshakeEnds = time.Now().Add(handshakeTimeout)
		setDeadline(handshakeEnds)
		return err
	}
	config.VerifyCertificate = func(der []byte) error {
		if opts.VerifyCertificate != nil {
			if err := asking(func() error { return opts.VerifyCertificate(address, der) }); err != nil {
				return err
			}
		}
		if config.Password != "" {
			return nil
		}
		return asking(func() error {
			password, err := opts.AskPassword()
			config.Password = password
			return err
		})
	}

	client, err := rdpcore.NewClient(config)
	if err != nil {
		return nil, err
	}
	session.client = client
	session.closeTransport = func() {
		connMu.Lock()
		defer connMu.Unlock()
		if current != nil {
			current.Close()
		}
	}
	client.OnBitmap(session.onBitmap)
	client.OnStridedBitmap(session.onStrided)
	client.OnResize(func(width, height int) {
		session.syncSize()
		_ = client.RefreshRect(0, 0, width, height)
	})
	client.OnDisconnect(func(err error) { session.finish(err) })
	if opts.Clipboard {
		client.OnClipboardUpdate(func(hasText, _ bool) {
			if hasText {
				_ = client.RequestClipboard()
			}
		})
		client.OnClipboardText(func(text string) {
			session.mu.Lock()
			session.remote, session.hasText = text, true
			session.mu.Unlock()
			session.wake()
		})
	}

	// Cancelling the context must end a handshake that is waiting on the network.
	stop := context.AfterFunc(ctx, func() { client.Close() })
	err = client.Connect()
	cancelled := !stop()
	if err == nil {
		setDeadline(time.Time{}) // the heartbeat watches the open connection
	}
	if err == nil && cancelled {
		err = ctx.Err()
	}
	if err != nil {
		client.Close()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, translate(err)
	}
	session.info = desktop.Info{Protocol: "RDP", Name: host, Encrypted: true}
	go func() {
		<-client.Done()
		session.finish(nil)
	}()
	return session, nil
}

// clampSize holds a requested desktop to what the server's display channel
// accepts: an even width, and sides between minSize and maxSize.
func clampSize(width, height int) (int, int) {
	return min(max(width, minSize), maxSize) &^ 1, min(max(height, minSize), maxSize)
}

// scaleLevels are the interface scales Windows offers, in percent.
var scaleLevels = [...]uint32{100, 125, 150, 175, 200, 225, 250, 300, 350, 400, 450, 500}

// scalePercent is the Windows scale level nearest to scale (a device pixel per
// logical pixel), 100 for zero or a scale below it.
func scalePercent(scale float32) uint32 {
	wanted := float64(scale) * 100
	nearest := scaleLevels[0]
	for _, level := range scaleLevels[1:] {
		if math.Abs(float64(level)-wanted) < math.Abs(float64(nearest)-wanted) {
			nearest = level
		}
	}
	return nearest
}

var _ desktop.Resizer = (*Session)(nil)

// CanResize reports whether the server has opened its display control channel.
func (session *Session) CanResize() bool { return session.client.CanResize() }

// Resize asks the server for a width x height desktop drawn at scale, keeping
// the session. The frame follows when the server has made the new desktop.
func (session *Session) Resize(width, height int, scale float32) error {
	width, height = clampSize(width, height)
	return session.client.ResizeDisplay(width, height, scalePercent(scale))
}

// translate turns the library's authentication failure into this package's.
func translate(err error) error {
	if errors.Is(err, rdpcore.ErrAuthFailed) {
		return fmt.Errorf("%w: %v", ErrAuthFailed, err)
	}
	return err
}

func (session *Session) wake() {
	if session.opts.Notify != nil {
		session.opts.Notify()
	}
}

// finish records why the connection ended, once, and closes Done.
func (session *Session) finish(err error) {
	session.mu.Lock()
	select {
	case <-session.done:
		session.mu.Unlock()
		return
	default:
	}
	if session.closed {
		err = nil
	}
	session.err = err
	close(session.done)
	session.mu.Unlock()
	session.closeTransport()
	session.wake()
}

// syncSize makes the frame the size of the library's screen, and when that
// changed it fills the new frame from the whole screen: a resize leaves the
// remote to repaint, which arrives as ordinary updates.
func (session *Session) syncSize() {
	w, h := session.client.FramebufferDims()
	if w <= 0 || h <= 0 {
		return
	}
	if fw, fh := session.frame.Size(); fw == w && fh == h {
		return
	}
	session.frame.Resize(w, h)
	session.copyRect(0, 0, w, h)
}

// onBitmap is called on the receive goroutine after the library drew into its
// framebuffer; the rectangle is read back from there.
func (session *Session) onBitmap(update *rdpcore.BitmapUpdate) {
	session.syncSize()
	session.copyRect(update.X, update.Y, update.Width, update.Height)
}

func (session *Session) copyRect(x, y, width, height int) {
	need := width * height * 4
	if width <= 0 || height <= 0 || need > maxSize*maxSize*4 {
		return
	}
	if cap(session.buffer) < need {
		session.buffer = make([]byte, need)
	}
	rx, ry, rw, rh := session.client.ReadRectBGRA(session.buffer[:need], x, y, width, height)
	if rw > 0 {
		session.frame.Update(rx, ry, rw, rh, session.buffer, rw*4)
	}
}

// onStrided is a picture update of the graphics pipeline: top-down RGBA rows
// stride bytes apart, which the library has also drawn into its framebuffer.
func (session *Session) onStrided(x, y, width, height int, data []byte, stride int) {
	session.syncSize()
	if width <= 0 || height <= 0 || width > maxSize || height > maxSize || stride < width*4 || len(data) < (height-1)*stride+width*4 {
		return
	}
	need := width * height * 4
	if cap(session.buffer) < need {
		session.buffer = make([]byte, need)
	}
	out := session.buffer[:need]
	for row := 0; row < height; row++ {
		src := data[row*stride:][:width*4]
		dst := out[row*width*4:][:width*4]
		for i := 0; i < len(src); i += 4 {
			dst[i], dst[i+1], dst[i+2], dst[i+3] = src[i+2], src[i+1], src[i], 0xFF
		}
	}
	session.frame.Update(x, y, width, height, out, width*4)
}

func (session *Session) Info() desktop.Info                 { return session.info }
func (session *Session) Size() (width, height int)          { return session.frame.Size() }
func (session *Session) Present(dst desktop.Presenter) bool { return session.frame.Present(dst) }
func (session *Session) Done() <-chan struct{}              { return session.done }

func (session *Session) Err() error {
	session.mu.Lock()
	defer session.mu.Unlock()
	return session.err
}

func (session *Session) Close() error {
	session.mu.Lock()
	session.closed = true
	session.mu.Unlock()
	return session.client.Close()
}

// SetActive suppresses the server's picture while nobody sees it, and asks for a
// full repaint when someone does.
func (session *Session) SetActive(active bool) {
	_ = session.client.SetOutputSuppressed(!active)
}

func (session *Session) RemoteClipboard() (string, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	text, ok := session.remote, session.hasText
	session.remote, session.hasText = "", false
	return text, ok
}

// Bells: RDP has no bell the client hears.
func (session *Session) Bells() int { return 0 }

func (session *Session) SetClipboard(text string) error {
	if !session.opts.Clipboard {
		return nil
	}
	return session.client.SetClipboard(text)
}

// RDP pointer event flags.
const (
	ptrMove   = 0x0800
	ptrDown   = 0x8000
	ptrLeft   = 0x1000
	ptrRight  = 0x2000
	ptrMiddle = 0x4000
)

var buttonFlags = [...]struct {
	button desktop.Buttons
	flag   uint16
}{{desktop.ButtonLeft, ptrLeft}, {desktop.ButtonRight, ptrRight}, {desktop.ButtonMiddle, ptrMiddle}}

// SendPointer sends a move, and a press or release for each button whose state
// changed since the last event.
func (session *Session) SendPointer(x, y int, buttons desktop.Buttons) error {
	x, y = clampCoordinate(x), clampCoordinate(y)
	session.mu.Lock()
	changed := session.buttons ^ buttons
	session.buttons = buttons
	session.mu.Unlock()
	if changed == 0 {
		return session.client.SendMouse(x, y, ptrMove)
	}
	for _, entry := range buttonFlags {
		if changed&entry.button == 0 {
			continue
		}
		flags := entry.flag
		if buttons&entry.button != 0 {
			flags |= ptrDown
		}
		if err := session.client.SendMouse(x, y, flags); err != nil {
			return err
		}
	}
	return nil
}

func clampCoordinate(value int) int { return max(0, min(value, 0xFFFF)) }

// maxNotches bounds what one wheel gesture sends.
const maxNotches = 10

func (session *Session) SendWheel(x, y int, wheel desktop.Wheel, _ desktop.Buttons) error {
	x, y = clampCoordinate(x), clampCoordinate(y)
	if wheel.Y != 0 {
		// A positive RDP rotation is away from the user, up; Wheel.Y is down.
		if err := session.client.SendMouseWheel(x, y, -max(-maxNotches, min(wheel.Y, maxNotches)), false); err != nil {
			return err
		}
	}
	if wheel.X != 0 {
		return session.client.SendMouseWheel(x, y, max(-maxNotches, min(wheel.X, maxNotches)), true)
	}
	return nil
}

// SendKey sends a key as a scancode. A character that has no key on a US
// keyboard is sent as a Unicode event instead.
func (session *Session) SendKey(event desktop.KeyEvent) error {
	if event.Rune == 0 {
		code := keyCode(event.Key)
		if code == 0 {
			return nil
		}
		if event.Key == desktop.KeyShift {
			session.mu.Lock()
			session.shift = event.Down
			session.mu.Unlock()
		}
		return session.client.SendKeyboard(code, event.Down)
	}
	if code, ok := runeCode(event.Rune); ok {
		session.mu.Lock()
		shift := session.shift
		session.mu.Unlock()
		// "!" arrives with Shift held when typed on a US keyboard; typed any other
		// way (a layout where it is unshifted) Shift is not, and is added.
		addShift := code.shift && !shift
		if addShift && event.Down {
			if err := session.client.SendKeyboard(0x2A, true); err != nil {
				return err
			}
		}
		err := session.client.SendKeyboard(code.code, event.Down)
		if addShift && !event.Down {
			if releaseErr := session.client.SendKeyboard(0x2A, false); err == nil {
				err = releaseErr
			}
		}
		return err
	}
	return session.typeRune(event.Rune, event.Down)
}

// typeRune sends one character as Unicode events (a surrogate pair above the
// Basic Multilingual Plane), a press or a release.
func (session *Session) typeRune(r rune, down bool) error {
	if r < 0x20 || r == 0x7f || r > 0x10ffff {
		return nil
	}
	high, low := utf16.EncodeRune(r)
	if high == 0xFFFD && low == 0xFFFD { // in the BMP
		return session.client.SendUnicode(uint16(r), down)
	}
	if err := session.client.SendUnicode(uint16(high), down); err != nil {
		return err
	}
	return session.client.SendUnicode(uint16(low), down)
}

func (session *Session) SendText(text string) error {
	typed := 0
	for _, r := range text {
		if typed++; typed > maxTyped {
			return errors.New("rdp: text too long to type")
		}
		if r == '\n' {
			if err := session.client.SendKeyboard(keyCode(desktop.KeyEnter), true); err != nil {
				return err
			}
			if err := session.client.SendKeyboard(keyCode(desktop.KeyEnter), false); err != nil {
				return err
			}
			continue
		}
		if err := session.typeRune(r, true); err != nil {
			return err
		}
		if err := session.typeRune(r, false); err != nil {
			return err
		}
	}
	return nil
}
