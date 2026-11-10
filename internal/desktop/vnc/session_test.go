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
	io.ReadFull(conn, make([]byte, 20+4+4*6)) // pixel format and six encodings
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
	if info := session.Info(); info.ProtocolEncrypted || info.Security != "None" || info.Authentication != "none" ||
		info.SecurityTypes != "None" || info.SecuritySubtypes != "" || info.Cipher != "" || info.TLSVersion != "" {
		t.Fatalf("SSH protection was confused with protocol encryption: %+v", info)
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
