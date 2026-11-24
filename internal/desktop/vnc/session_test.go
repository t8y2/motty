package vnc

import (
	"bytes"
	"context"
	"encoding/binary"
	"io"
	"net"
	"testing"
	"time"

	"motty/internal/desktop"
	"motty/internal/rfb"
)

// fakeServer speaks the part of RFB 3.8 (no security) a Session needs, and
// records the client's messages.
func fakeServer(t *testing.T, conn net.Conn) {
	t.Helper()
	conn.Write([]byte("RFB 003.008\n"))
	io.ReadFull(conn, make([]byte, 12))
	conn.Write([]byte{1, 1})
	io.ReadFull(conn, make([]byte, 1))
	conn.Write([]byte{0, 0, 0, 0})
	io.ReadFull(conn, make([]byte, 1))
	init := []byte{0, 4, 0, 2}
	init = append(init, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0, 0, 0, 0, 4)
	conn.Write(append(init, "demo"...))
	io.ReadFull(conn, make([]byte, 20+4+4*len(rfb.DefaultEncodings))) // pixel format and the encodings
}

func open(t *testing.T, opts desktop.Options) (*Session, net.Conn) {
	t.Helper()
	listener, err := net.Listen("tcp", "127.0.0.1:0")
	if err != nil {
		t.Fatal(err)
	}
	defer listener.Close()
	clientSide, err := net.Dial("tcp", listener.Addr().String())
	if err != nil {
		t.Fatal(err)
	}
	serverSide, err := listener.Accept()
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { clientSide.Close(); serverSide.Close() })
	go fakeServer(t, serverSide)
	session, err := Open(context.Background(), func(context.Context, string, string) (net.Conn, error) { return clientSide, nil }, "x:5900", opts)
	if err != nil {
		t.Fatal(err)
	}
	return session, serverSide
}

func TestSessionPresentsTheServersPictureAndSendsInput(t *testing.T) {
	woken := make(chan struct{}, 8)
	session, server := open(t, desktop.Options{Notify: func() { woken <- struct{}{} }, Clipboard: true, Encrypted: true})
	defer session.Close()
	if info := session.Info(); info.Protocol != "VNC" || info.Name != "demo" || !info.Encrypted {
		t.Fatalf("info = %+v", info)
	}
	io.ReadFull(server, make([]byte, 10)) // the first update request
	// A 4x2 raw rectangle of one color, a bell and a clipboard.
	update := []byte{0, 0, 0, 1, 0, 0, 0, 0, 0, 4, 0, 2, 0, 0, 0, 0}
	update = append(update, bytes.Repeat([]byte{10, 20, 30, 0}, 8)...)
	server.Write(update)
	server.Write([]byte{2})
	server.Write([]byte{3, 0, 0, 0, 0, 0, 0, 2, 'o', 'k'})
	io.ReadFull(server, make([]byte, 10)) // the next request, after the update
	deadline := time.After(5 * time.Second)
	for {
		if text, ok := session.RemoteClipboard(); ok {
			if text != "ok" {
				t.Fatalf("clipboard = %q", text)
			}
			break
		}
		select {
		case <-woken:
		case <-deadline:
			t.Fatal("no clipboard")
		}
	}
	var shown struct {
		w, h int
		pix  []byte
	}
	if !session.Present(presenter{func(w, h int) { shown.w, shown.h, shown.pix = w, h, make([]byte, w*h*4) }, func(x, y, w, h int, pix []byte, stride int) {
		for row := 0; row < h; row++ {
			copy(shown.pix[((y+row)*shown.w+x)*4:], pix[row*stride:][:w*4])
		}
	}}) || shown.w != 4 || shown.h != 2 || !bytes.Equal(shown.pix[:3], []byte{10, 20, 30}) {
		t.Fatalf("picture = %+v", shown)
	}
	if session.Bells() != 1 || session.Bells() != 0 {
		t.Fatal("bell count")
	}

	// Input: a wheel notch is a press and a release of the wheel button, a typed
	// character a key down and up of its keysym.
	go func() {
		session.SendWheel(3, 4, desktop.Wheel{Y: 1}, desktop.ButtonLeft)
		session.SendText("é€")
		session.SendKey(desktop.KeyEvent{Key: desktop.KeyF2, Down: true})
	}()
	read := func(n int) []byte {
		buffer := make([]byte, n)
		server.SetReadDeadline(time.Now().Add(5 * time.Second))
		if _, err := io.ReadFull(server, buffer); err != nil {
			t.Fatal(err)
		}
		return buffer
	}
	if got := read(12); !bytes.Equal(got, []byte{5, 1 | 16, 0, 3, 0, 4, 5, 1, 0, 3, 0, 4}) {
		t.Fatalf("wheel = %v", got)
	}
	keys := read(32)
	keysym := func(message []byte) (down bool, sym uint32) {
		return message[1] == 1, binary.BigEndian.Uint32(message[4:])
	}
	for index, want := range []struct {
		down bool
		sym  uint32
	}{{true, 0xe9}, {false, 0xe9}, {true, 0x010020ac}, {false, 0x010020ac}} {
		if down, sym := keysym(keys[index*8:]); down != want.down || sym != want.sym {
			t.Fatalf("key %d = %v %x, want %v %x", index, down, sym, want.down, want.sym)
		}
	}
	if down, sym := keysym(read(8)); !down || sym != 0xffbf {
		t.Fatalf("F2 = %v %x", down, sym)
	}

	session.Close()
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("Close did not end the session")
	}
	if err := session.Err(); err != nil {
		t.Fatalf("a requested close reported %v", err)
	}
}

type presenter struct {
	resize func(w, h int)
	update func(x, y, w, h int, pix []byte, stride int)
}

func (p presenter) Resize(w, h int) { p.resize(w, h) }
func (p presenter) Update(x, y, w, h int, pix []byte, stride int) {
	p.update(x, y, w, h, pix, stride)
}

func TestClipboardStaysLocalUnlessAllowed(t *testing.T) {
	session, server := open(t, desktop.Options{})
	defer session.Close()
	io.ReadFull(server, make([]byte, 10))
	server.Write([]byte{3, 0, 0, 0, 0, 0, 0, 2, 'o', 'k'})
	if err := session.SetClipboard("secret"); err != nil {
		t.Fatal(err)
	}
	// Nothing was sent: the next thing the server reads is the key we send now.
	go session.SendKey(desktop.KeyEvent{Rune: 'a', Down: true})
	server.SetReadDeadline(time.Now().Add(5 * time.Second))
	message := make([]byte, 8)
	if _, err := io.ReadFull(server, message); err != nil || message[0] != 4 {
		t.Fatalf("expected the key message, got %v (%v)", message, err)
	}
	time.Sleep(20 * time.Millisecond)
	if _, ok := session.RemoteClipboard(); ok {
		t.Fatal("the server's clipboard was kept although the host does not allow it")
	}
}

func TestServerHangupEndsTheSessionWithAnError(t *testing.T) {
	session, server := open(t, desktop.Options{})
	io.ReadFull(server, make([]byte, 10))
	server.Close()
	select {
	case <-session.Done():
	case <-time.After(5 * time.Second):
		t.Fatal("hangup not noticed")
	}
	if session.Err() == nil {
		t.Fatal("a dropped connection must report why")
	}
}

// A host's color depth and encoding preference are what the server is asked
// for in the first messages after ServerInit.
func TestSessionAsksTheServerForTheHostsQuality(t *testing.T) {
	for _, test := range []struct {
		opts     desktop.Options
		bits     byte
		encoding int32
	}{
		{desktop.Options{}, 32, 16},
		{desktop.Options{ColorDepth: 16, Encoding: desktop.EncodingRaw}, 16, 0},
		{desktop.Options{ColorDepth: 8, Encoding: desktop.EncodingHextile}, 8, 5},
		{desktop.Options{ColorDepth: 24}, 32, 16}, // not a VNC format: the default
	} {
		listener, err := net.Listen("tcp", "127.0.0.1:0")
		if err != nil {
			t.Fatal(err)
		}
		type asked struct {
			bits     byte
			encoding int32
		}
		got := make(chan asked, 1)
		go func() {
			conn, err := listener.Accept()
			if err != nil {
				return
			}
			defer conn.Close()
			conn.Write([]byte("RFB 003.008\n"))
			io.ReadFull(conn, make([]byte, 12))
			conn.Write([]byte{1, 1})
			io.ReadFull(conn, make([]byte, 1))
			conn.Write([]byte{0, 0, 0, 0})
			io.ReadFull(conn, make([]byte, 1))
			init := []byte{0, 4, 0, 2, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0, 0, 0, 0, 4}
			conn.Write(append(init, "demo"...))
			format := make([]byte, 20)
			io.ReadFull(conn, format)
			header := make([]byte, 4)
			io.ReadFull(conn, header)
			first := make([]byte, 4)
			io.ReadFull(conn, first)
			got <- asked{format[4], int32(binary.BigEndian.Uint32(first))}
		}()
		session, err := Open(context.Background(), func(ctx context.Context, network, address string) (net.Conn, error) {
			return (&net.Dialer{}).DialContext(ctx, network, listener.Addr().String())
		}, "x:5900", test.opts)
		if err != nil {
			t.Fatal(err)
		}
		if answer := <-got; answer.bits != test.bits || answer.encoding != test.encoding {
			t.Errorf("%+v asked for %d bits and encoding %d, want %d and %d", test.opts, answer.bits, answer.encoding, test.bits, test.encoding)
		}
		session.Close()
		listener.Close()
	}
}

func TestSessionFlushesOncePerUpdate(t *testing.T) {
	woken := make(chan struct{}, 16)
	session, server := open(t, desktop.Options{Notify: func() { woken <- struct{}{} }})
	defer session.Close()

	// Drain initial resize notify
	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("no initial notify")
	}

	io.ReadFull(server, make([]byte, 10)) // the first update request

	// A single FramebufferUpdate with 3 rectangles
	update := []byte{0, 0, 0, 3}
	// Rect 1 (1x1 at 0,0)
	update = append(update, 0, 0, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0)
	update = append(update, 1, 2, 3, 0)
	// Rect 2 (1x1 at 1,0)
	update = append(update, 0, 1, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0)
	update = append(update, 4, 5, 6, 0)
	// Rect 3 (1x1 at 2,0)
	update = append(update, 0, 2, 0, 0, 0, 1, 0, 1, 0, 0, 0, 0)
	update = append(update, 7, 8, 9, 0)

	server.Write(update)
	io.ReadFull(server, make([]byte, 10)) // the next request, after the update completes

	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("no notify after update")
	}

	// Ensure there are no additional spurious wakes from individual rects
	select {
	case <-woken:
		t.Fatal("spurious wake from individual rectangle instead of single flush")
	case <-time.After(50 * time.Millisecond):
	}
}

func TestSessionPointerSource(t *testing.T) {
	woken := make(chan struct{}, 16)
	session, server := open(t, desktop.Options{Notify: func() { woken <- struct{}{} }})
	defer session.Close()

	// Initial pointer shape is PointerSystem
	kind, shape, version := session.PointerShape()
	if kind != desktop.PointerSystem || version != 1 {
		t.Fatalf("initial pointer = %v, version %d; want PointerSystem, version 1", kind, version)
	}

	// Drain initial resize notify
	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("no initial notify")
	}

	io.ReadFull(server, make([]byte, 10)) // first FramebufferUpdateRequest

	// Send cursor update: 2x2 cursor at hotspot (1, 0)
	// Encoding -239 (0xFFFFFF11)
	update := []byte{0, 0, 0, 1}
	// x=1, y=0, w=2, h=2, encoding=-239
	update = append(update, 0, 1, 0, 0, 0, 2, 0, 2, 0xff, 0xff, 0xff, 0x11)
	// 4 pixels (32-bit: B, G, R, pad)
	update = append(update,
		1, 2, 3, 0,
		0, 0, 0, 0,
		0, 0, 0, 0,
		4, 5, 6, 0,
	)
	// 2 mask bytes (stride = (2+7)/8 = 1 byte):
	// row 0: pixel 0 opaque, pixel 1 transparent -> 0x80
	// row 1: pixel 0 transparent, pixel 1 opaque -> 0x40
	update = append(update, 0x80, 0x40)

	server.Write(update)
	io.ReadFull(server, make([]byte, 10)) // request after update

	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("no notify after cursor update")
	}

	kind, shape, version = session.PointerShape()
	if kind != desktop.PointerBitmap || version != 2 {
		t.Fatalf("pointer = %v, version %d; want PointerBitmap, version 2", kind, version)
	}
	if shape.Width != 2 || shape.Height != 2 || shape.HotX != 1 || shape.HotY != 0 {
		t.Fatalf("shape geometry = %dx%d at %d,%d; want 2x2 at 1,0", shape.Width, shape.Height, shape.HotX, shape.HotY)
	}
	wantBGRA := []byte{
		1, 2, 3, 255,
		0, 0, 0, 0,
		0, 0, 0, 0,
		4, 5, 6, 255,
	}
	if !bytes.Equal(shape.BGRA, wantBGRA) {
		t.Fatalf("shape BGRA = %v, want %v", shape.BGRA, wantBGRA)
	}

	// Send 0x0 cursor (hidden)
	hideUpdate := []byte{0, 0, 0, 1}
	hideUpdate = append(hideUpdate, 0, 0, 0, 0, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0x11)
	server.Write(hideUpdate)
	io.ReadFull(server, make([]byte, 10))

	select {
	case <-woken:
	case <-time.After(time.Second):
		t.Fatal("no notify after cursor hide")
	}

	kind, shape, version = session.PointerShape()
	if kind != desktop.PointerHidden || version != 3 {
		t.Fatalf("pointer = %v, version %d; want PointerHidden, version 3", kind, version)
	}

	session.Close()
	select {
	case <-session.Done():
	case <-time.After(time.Second):
		t.Fatal("session did not end after close")
	}
	kind, _, version = session.PointerShape()
	if kind != desktop.PointerSystem || version != 4 {
		t.Fatalf("pointer after close = %v, version %d; want PointerSystem, version 4", kind, version)
	}
}

func TestSessionSendPointerDeduplicates(t *testing.T) {
	session, server := open(t, desktop.Options{})
	defer session.Close()

	io.ReadFull(server, make([]byte, 10)) // first FramebufferUpdateRequest

	readPointerEvent := func() (x, y int, buttons uint8) {
		t.Helper()
		var msg [6]byte
		server.SetReadDeadline(time.Now().Add(time.Second))
		if _, err := io.ReadFull(server, msg[:]); err != nil {
			t.Fatalf("read pointer event: %v", err)
		}
		if msg[0] != 5 {
			t.Fatalf("message type = %d, want 5 (PointerEvent)", msg[0])
		}
		return int(binary.BigEndian.Uint16(msg[2:])), int(binary.BigEndian.Uint16(msg[4:])), msg[1]
	}

	// 1. Send first pointer event
	if err := session.SendPointer(100, 200, desktop.ButtonLeft); err != nil {
		t.Fatal(err)
	}
	x, y, buttons := readPointerEvent()
	if x != 100 || y != 200 || buttons != 1 {
		t.Fatalf("pointer event = %d,%d, btn %d; want 100,200, btn 1", x, y, buttons)
	}

	// 2. Send same pointer event again -> should be deduplicated (no wire write)
	if err := session.SendPointer(100, 200, desktop.ButtonLeft); err != nil {
		t.Fatal(err)
	}
	server.SetReadDeadline(time.Now().Add(50 * time.Millisecond))
	var dummy [1]byte
	if _, err := server.Read(dummy[:]); err == nil {
		t.Fatal("expected no data on wire for duplicate pointer event")
	}

	// 3. Send moved coordinate
	if err := session.SendPointer(105, 200, desktop.ButtonLeft); err != nil {
		t.Fatal(err)
	}
	x, y, buttons = readPointerEvent()
	if x != 105 || y != 200 || buttons != 1 {
		t.Fatalf("pointer event = %d,%d, btn %d; want 105,200, btn 1", x, y, buttons)
	}

	// 4. Send button change
	if err := session.SendPointer(105, 200, 0); err != nil {
		t.Fatal(err)
	}
	x, y, buttons = readPointerEvent()
	if x != 105 || y != 200 || buttons != 0 {
		t.Fatalf("pointer event = %d,%d, btn %d; want 105,200, btn 0", x, y, buttons)
	}
}
