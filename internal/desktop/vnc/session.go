// Package vnc implements desktop.Session over the RFB protocol of package rfb.
package vnc

import (
	"context"
	"errors"
	"net"
	"sync"

	"motty/internal/desktop"
	"motty/internal/rfb"
)

// Session is a desktop.Session on a VNC connection.
type Session struct {
	client *rfb.Client
	frame  *desktop.Frame
	opts   desktop.Options
	info   desktop.Info

	done chan struct{}
	mu   sync.Mutex
	err  error
	// remote is the server's clipboard not yet taken, bells the rings not yet
	// counted.
	remote  string
	hasText bool
	bells   int
	closed  bool
}

var _ desktop.Session = (*Session)(nil)

// Open connects to address through dial and starts reading the server. It
// returns rfb.ErrAuthRequired when the server wants a password and opts has none,
// and rfb.ErrAuthFailed when the password was refused: the caller asks the user
// and opens again.
func Open(ctx context.Context, dial desktop.Dialer, address string, opts desktop.Options) (*Session, error) {
	conn, err := dial(ctx, "tcp", address)
	if err != nil {
		return nil, err
	}
	// The handshake has its own timeout; cancelling the context must end it too.
	stop := context.AfterFunc(ctx, func() { conn.Close() })
	config := rfb.Config{
		Username: opts.Username, Password: opts.Password,
		AskPassword: opts.AskPassword, AskCredentials: opts.AskCredentials,
	}
	if opts.VerifyServerKey != nil {
		config.VerifyServerKey = func(publicKeyDER []byte) error {
			return opts.VerifyServerKey(address, publicKeyDER)
		}
	}
	client, err := rfb.Connect(conn, config)
	if !stop() && err == nil {
		err = ctx.Err() // cancelled just as the handshake finished
	}
	if err != nil {
		conn.Close()
		if ctx.Err() != nil {
			err = ctx.Err()
		}
		return nil, err
	}
	session := &Session{client: client, opts: opts, done: make(chan struct{})}
	session.frame = desktop.NewFrame(opts.Notify)
	session.info = desktop.Info{Protocol: "VNC", Name: client.Name, Encrypted: opts.Encrypted || client.Encrypted}
	go session.run(conn)
	return session, nil
}

// sink gives rfb the frame to draw on, and takes the bell and the clipboard.
type sink struct {
	*desktop.Frame
	session *Session
}

func (s sink) Bell() {
	s.session.mu.Lock()
	s.session.bells++
	s.session.mu.Unlock()
	s.session.wake()
}

func (s sink) ServerText(text string) {
	if !s.session.opts.Clipboard {
		return
	}
	s.session.mu.Lock()
	s.session.remote, s.session.hasText = text, true
	s.session.mu.Unlock()
	s.session.wake()
}

func (session *Session) wake() {
	if session.opts.Notify != nil {
		session.opts.Notify()
	}
}

func (session *Session) run(conn net.Conn) {
	err := session.client.Run(sink{session.frame, session})
	conn.Close()
	session.mu.Lock()
	if session.closed {
		err = nil
	}
	session.err = err
	session.mu.Unlock()
	close(session.done)
	session.wake()
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

func (session *Session) SetActive(active bool) { session.client.SetActive(active) }

func (session *Session) Close() error {
	session.mu.Lock()
	session.closed = true
	session.mu.Unlock()
	return session.client.Close()
}

func (session *Session) RemoteClipboard() (string, bool) {
	session.mu.Lock()
	defer session.mu.Unlock()
	text, ok := session.remote, session.hasText
	session.remote, session.hasText = "", false
	return text, ok
}

func (session *Session) Bells() int {
	session.mu.Lock()
	defer session.mu.Unlock()
	bells := session.bells
	session.bells = 0
	return bells
}

func (session *Session) SetClipboard(text string) error {
	if !session.opts.Clipboard {
		return nil
	}
	return session.client.SendClipboard(text)
}

// RFB pointer button bits: the three buttons, then the wheel as button presses.
const (
	rfbLeft       = 1 << 0
	rfbMiddle     = 1 << 1
	rfbRight      = 1 << 2
	rfbWheelUp    = 1 << 3
	rfbWheelDown  = 1 << 4
	rfbWheelLeft  = 1 << 5
	rfbWheelRight = 1 << 6
)

func rfbButtons(buttons desktop.Buttons) uint8 {
	var mask uint8
	if buttons&desktop.ButtonLeft != 0 {
		mask |= rfbLeft
	}
	if buttons&desktop.ButtonMiddle != 0 {
		mask |= rfbMiddle
	}
	if buttons&desktop.ButtonRight != 0 {
		mask |= rfbRight
	}
	return mask
}

func (session *Session) SendPointer(x, y int, buttons desktop.Buttons) error {
	return session.client.SendPointer(x, y, rfbButtons(buttons))
}

// maxNotches bounds what one wheel gesture sends.
const maxNotches = 10

func (session *Session) SendWheel(x, y int, wheel desktop.Wheel, buttons desktop.Buttons) error {
	base := rfbButtons(buttons)
	notch := func(count int, negative, positive uint8) error {
		button := positive
		if count < 0 {
			button, count = negative, -count
		}
		for range min(count, maxNotches) {
			if err := session.client.SendPointer(x, y, base|button); err != nil {
				return err
			}
			if err := session.client.SendPointer(x, y, base); err != nil {
				return err
			}
		}
		return nil
	}
	if err := notch(wheel.Y, rfbWheelUp, rfbWheelDown); err != nil {
		return err
	}
	return notch(wheel.X, rfbWheelLeft, rfbWheelRight)
}

// SendKey sends a key by keysym. A repeat is not sent: the X server repeats a
// key it sees held, and an RFB server has no way to say the key was already
// down, so the repeat would type the character twice.
func (session *Session) SendKey(event desktop.KeyEvent) error {
	if event.Repeat {
		return nil
	}
	keysym := keysymFor(event)
	if keysym == 0 {
		return nil
	}
	return session.client.SendKey(keysym, event.Down)
}

// maxTyped bounds a SendText, which types a character at a time.
const maxTyped = 16 << 10

func (session *Session) SendText(text string) error {
	typed := 0
	for _, r := range text {
		if typed++; typed > maxTyped {
			return errors.New("vnc: text too long to type")
		}
		keysym := keysymFor(desktop.KeyEvent{Rune: r})
		if r == '\n' {
			keysym = keysymFor(desktop.KeyEvent{Key: desktop.KeyEnter})
		}
		if keysym == 0 {
			continue
		}
		if err := session.client.SendKey(keysym, true); err != nil {
			return err
		}
		if err := session.client.SendKey(keysym, false); err != nil {
			return err
		}
	}
	return nil
}
