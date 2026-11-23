package rfb

import (
	"bufio"
	"bytes"
	"io"
	"net"
	"testing"
	"time"
)

// nullSink accepts everything and checks that no call lies outside the picture:
// a decoder that writes outside the desktop would corrupt a real sink.
type nullSink struct {
	t             testing.TB
	width, height int
}

func (s *nullSink) inside(x, y, width, height int) {
	if x < 0 || y < 0 || width < 0 || height < 0 || x+width > s.width || y+height > s.height {
		s.t.Fatalf("drew %dx%d at %d,%d outside the %dx%d desktop", width, height, x, y, s.width, s.height)
	}
}
func (s *nullSink) Resize(width, height int) { s.width, s.height = width, height }
func (s *nullSink) Update(x, y, width, height int, pix []byte, stride int) {
	s.inside(x, y, width, height)
	if height > 0 && len(pix) < (height-1)*stride+width*4 {
		s.t.Fatalf("a %dx%d update with %d bytes at stride %d", width, height, len(pix), stride)
	}
}
func (s *nullSink) Copy(x, y, width, height, fromX, fromY int) {
	s.inside(x, y, width, height)
	s.inside(fromX, fromY, width, height)
}
func (s *nullSink) Fill(x, y, width, height int, _, _, _ byte) { s.inside(x, y, width, height) }
func (s *nullSink) Bell()                                      {}
func (s *nullSink) ServerText(string)                          {}

type discardConn struct{ net.Conn }

func (discardConn) Write(p []byte) (int, error)      { return len(p), nil }
func (discardConn) Close() error                     { return nil }
func (discardConn) SetDeadline(t time.Time) error    { return nil }
func (c discardConn) Read(p []byte) (int, error)     { return 0, io.EOF }
func (discardConn) LocalAddr() net.Addr              { return nil }
func (discardConn) RemoteAddr() net.Addr             { return nil }
func (discardConn) SetReadDeadline(time.Time) error  { return nil }
func (discardConn) SetWriteDeadline(time.Time) error { return nil }

func clientOver(data []byte, width, height int) *Client {
	return &Client{conn: discardConn{}, r: bufio.NewReader(bytes.NewReader(data)), width: width, height: height, active: true}
}

// FuzzMessages feeds arbitrary server bytes to the message loop of a connected
// client. Whatever they are, it must end (with an error) without panicking,
// without drawing outside the desktop and without holding more than the limits.
func FuzzMessages(f *testing.F) {
	stream := newZRLEStream()
	f.Add(update(rect(0, 0, 4, 4, EncodingRaw, pixels(16, 1, 2, 3)...)))
	f.Add(update(rect(1, 1, 4, 4, EncodingCopyRect, u16(0, 0)...)))
	f.Add(update(rect(0, 0, 8, 8, EncodingRRE, append(append(u32(1), pixels(2, 1, 2, 3)...), u16(1, 1, 2, 2)...)...)))
	f.Add(update(rect(0, 0, 20, 20, EncodingHextile, append([]byte{hextileBackground | hextileSubrects, 1, 2, 3, 0, 1}, 0x11, 0x22)...)))
	f.Add(update(stream.rect(0, 0, 8, 8, append([]byte{1}, cpixel(1, 2, 3)...))))
	f.Add(update(rect(0, 0, 8, 8, encodingDesktopSize)))
	f.Add([]byte{3, 0, 0, 0, 0, 0, 0, 2, 'h', 'i', 2, 1, 0, 0, 0, 0})
	f.Add(serverClipboard(allCaps, u32(20<<20)))
	f.Add(serverClipboard(clipProvide|clipUTF8, textProvide(f, "a\r\nb\x00")))
	f.Add(serverClipboard(clipNotify|clipUTF8, nil))
	f.Add(serverClipboard(clipPeek, nil))
	// The first byte picks the pixel format; a seed above is read as 32-bit
	// when it starts with 0, so the smaller formats get their own.
	for _, format := range []PixelFormat{Pixel16, Pixel8} {
		size := format.size()
		f.Add(append([]byte{byte(format)}, update(rect(0, 0, 4, 4, EncodingRaw, make([]byte, 16*size)...))...))
		f.Add(append([]byte{byte(format)}, update(rect(0, 0, 8, 8, EncodingRRE, append(append(u32(1), make([]byte, size)...), append(make([]byte, size), u16(1, 1, 2, 2)...)...)...))...))
		f.Add(append([]byte{byte(format)}, update(rect(0, 0, 20, 20, EncodingHextile, append([]byte{hextileRaw}, make([]byte, 16*16*size)...)...))...))
		f.Add(append([]byte{byte(format)}, update(newZRLEStream().rect(0, 0, 8, 8, append([]byte{1}, make([]byte, size)...)))...))
	}
	f.Fuzz(func(t *testing.T, data []byte) {
		format := PixelFormat(0)
		if len(data) > 0 {
			format, data = PixelFormat(data[0]%3), data[1:]
		}
		client := clientOver(data, 40, 30)
		client.format = format
		sink := &nullSink{t: t, width: 40, height: 30}
		for range 64 {
			kind, err := client.r.ReadByte()
			if err != nil {
				return
			}
			switch kind {
			case 0:
				err = client.update(sink)
			case 3:
				err = client.cutText(sink)
			default:
				return
			}
			if err != nil {
				return
			}
			sink.width, sink.height = client.width, client.height
		}
	})
}

// FuzzZRLETiles wraps arbitrary tile data in a valid zlib stream, which is what
// a hostile server would send, and decodes it.
func FuzzZRLETiles(f *testing.F) {
	f.Add([]byte{1, 1, 2, 3})
	f.Add([]byte{0, 1, 2, 3, 4, 5, 6, 7, 8, 9})
	f.Add([]byte{130, 1, 2, 3, 4, 5, 6, 0x80, 255, 255, 1, 0x80, 3})
	f.Add([]byte{5, 1, 1, 1, 2, 2, 2, 3, 3, 3, 4, 4, 4, 5, 5, 5, 0x12, 0x34})
	f.Fuzz(func(t *testing.T, tiles []byte) {
		stream := newZRLEStream()
		client := clientOver(append(stream.rect(0, 0, 70, 70, tiles)[12:], stream.rect(0, 0, 4, 4, tiles)[12:]...), 70, 70)
		sink := &nullSink{t: t, width: 70, height: 70}
		for range 2 {
			if err := client.decodeZRLE(sink, 0, 0, 70, 70); err != nil {
				return
			}
		}
	})
}

// FuzzHandshake feeds arbitrary bytes to the handshake.
func FuzzHandshake(f *testing.F) {
	f.Add([]byte("RFB 003.008\n\x01\x01\x00\x00\x00\x00\x00\x0a\x00\x0a\x20\x18\x00\x01\x00\xff\x00\xff\x00\xff\x10\x08\x00\x00\x00\x00\x00\x00\x00\x01x"))
	f.Add([]byte("RFB 003.003\n\x00\x00\x00\x02" + string(make([]byte, 16)) + "\x00\x00\x00\x00"))
	f.Fuzz(func(t *testing.T, data []byte) {
		a, b := net.Pipe()
		defer a.Close()
		defer b.Close()
		go func() { _, _ = io.Copy(io.Discard, b) }()
		go func() { _, _ = b.Write(data); b.Close() }()
		_, _ = Connect(a, Config{Password: "x", Timeout: time.Second})
	})
}
