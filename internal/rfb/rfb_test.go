package rfb

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"errors"
	"fmt"
	"io"
	"net"
	"strings"
	"sync"
	"testing"
	"time"
)

// canvas is a Sink that keeps the picture, to compare it with what was meant.
type canvas struct {
	mu            sync.Mutex
	width, height int
	pix           []byte // blue, green, red, pad
	bells         int
	text          string
	updates       int
}

func (c *canvas) Resize(width, height int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.width, c.height = width, height
	c.pix = make([]byte, width*height*4)
}

func (c *canvas) Update(x, y, width, height int, pix []byte, stride int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	c.updates++
	for row := 0; row < height; row++ {
		copy(c.pix[((y+row)*c.width+x)*4:], pix[row*stride:row*stride+width*4])
	}
}

func (c *canvas) Copy(x, y, width, height, fromX, fromY int) {
	c.mu.Lock()
	defer c.mu.Unlock()
	copyOf := append([]byte(nil), c.pix...)
	for row := 0; row < height; row++ {
		copy(c.pix[((y+row)*c.width+x)*4:], copyOf[((fromY+row)*c.width+fromX)*4:][:width*4])
	}
}

func (c *canvas) Fill(x, y, width, height int, blue, green, red byte) {
	c.mu.Lock()
	defer c.mu.Unlock()
	for row := 0; row < height; row++ {
		for column := 0; column < width; column++ {
			offset := ((y+row)*c.width + x + column) * 4
			c.pix[offset], c.pix[offset+1], c.pix[offset+2] = blue, green, red
		}
	}
}

func (c *canvas) Bell()                  { c.mu.Lock(); c.bells++; c.mu.Unlock() }
func (c *canvas) ServerText(text string) { c.mu.Lock(); c.text = text; c.mu.Unlock() }

func (c *canvas) at(x, y int) [3]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	offset := (y*c.width + x) * 4
	return [3]byte{c.pix[offset], c.pix[offset+1], c.pix[offset+2]}
}

// server is the other end of a connection, speaking just enough RFB to hand the
// client a prepared stream.
type server struct {
	t    *testing.T
	conn net.Conn
}

func (s *server) write(data ...[]byte) {
	s.t.Helper()
	for _, part := range data {
		if _, err := s.conn.Write(part); err != nil {
			s.t.Logf("server write: %v", err) // the client may have hung up on purpose
			return
		}
	}
}

func (s *server) read(n int) []byte {
	s.t.Helper()
	buffer := make([]byte, n)
	s.conn.SetReadDeadline(time.Now().Add(5 * time.Second))
	if _, err := io.ReadFull(s.conn, buffer); err != nil {
		s.t.Fatalf("server read: %v", err)
	}
	return buffer
}

func u16(values ...int) []byte {
	var out []byte
	for _, value := range values {
		out = binary.BigEndian.AppendUint16(out, uint16(value))
	}
	return out
}

func u32(values ...int) []byte {
	var out []byte
	for _, value := range values {
		out = binary.BigEndian.AppendUint32(out, uint32(value))
	}
	return out
}

// serverInit is the ServerInit message of a width x height desktop.
func serverInit(width, height int, name string) []byte {
	out := u16(width, height)
	out = append(out, 32, 24, 0, 1, 0, 255, 0, 255, 0, 255, 16, 8, 0, 0, 0, 0)
	out = append(out, u32(len(name))...)
	return append(out, name...)
}

// handshake8 runs the server side of an RFB 3.8 handshake with the given
// security types (1 None or 2 VNC authentication with the password) up to and
// including ServerInit, and returns the client's SetPixelFormat and
// SetEncodings messages.
func (s *server) handshake(version string, security byte, password string, width, height int) (encodings []int32) {
	announced := version
	switch version {
	case "009", "889":
		version = "008" // what the client falls back to
	case "005":
		version = "003"
	}
	s.write([]byte("RFB 003." + announced + "\n"))
	if got := s.read(12); string(got) != "RFB 003."+version+"\n" {
		s.t.Errorf("client version = %q, want RFB 003.%s", got, version)
	}
	switch version {
	case "003":
		s.write(u32(int(security)))
	default:
		s.write([]byte{1, security})
		if got := s.read(1)[0]; got != security {
			s.t.Errorf("client chose security %d, want %d", got, security)
		}
	}
	if security == 2 {
		challenge := [16]byte{1, 2, 3, 4, 5, 6, 7, 8, 9, 10, 11, 12, 13, 14, 15, 16}
		s.write(challenge[:])
		want := vncResponse(challenge, password)
		got := s.read(16)
		if !bytes.Equal(got, want[:]) {
			s.write(u32(1))
			if version == "008" {
				s.write(u32(len("nope")), []byte("nope"))
			}
			return nil
		}
		s.write(u32(0))
	} else if version == "008" {
		s.write(u32(0))
	}
	s.read(1) // ClientInit
	s.write(serverInit(width, height, "test desktop"))
	format := s.read(20)
	if format[0] != 0 || format[4] != 32 || format[7] != 1 || format[14] != 16 {
		s.t.Errorf("pixel format message = %v", format)
	}
	header := s.read(4)
	for range binary.BigEndian.Uint16(header[2:]) {
		encodings = append(encodings, int32(binary.BigEndian.Uint32(s.read(4))))
	}
	return encodings
}

func pair(t *testing.T) (client, serverSide net.Conn) {
	t.Helper()
	a, b := net.Pipe()
	t.Cleanup(func() { a.Close(); b.Close() })
	return a, b
}

func connect(t *testing.T, config Config, script func(*server)) (*Client, error) {
	t.Helper()
	clientSide, serverSide := pair(t)
	done := make(chan struct{})
	go func() {
		defer close(done)
		script(&server{t: t, conn: serverSide})
	}()
	// The scripted server logs through t, so it must be finished before the test
	// is: closing the connections ends any read or write it is blocked in.
	t.Cleanup(func() {
		clientSide.Close()
		serverSide.Close()
		<-done
	})
	client, err := Connect(clientSide, config)
	return client, err
}

// update is a FramebufferUpdate carrying the given rectangle bodies.
func update(rects ...[]byte) []byte {
	out := append([]byte{0, 0}, u16(len(rects))...)
	for _, rect := range rects {
		out = append(out, rect...)
	}
	return out
}

func rect(x, y, width, height int, encoding int32, body ...byte) []byte {
	out := u16(x, y, width, height)
	out = append(out, u32(int(encoding))...)
	return append(out, body...)
}

// run starts the client's read loop and returns when it ends.
func run(client *Client, sink Sink) <-chan error {
	done := make(chan error, 1)
	go func() { done <- client.Run(sink) }()
	return done
}

func wait(t *testing.T, done <-chan error) error {
	t.Helper()
	select {
	case err := <-done:
		return err
	case <-time.After(5 * time.Second):
		t.Fatal("the client did not stop")
		return nil
	}
}

// readRequest reads a FramebufferUpdateRequest from the client.
func (s *server) readRequest() (incremental bool, width, height int) {
	s.t.Helper()
	message := s.read(10)
	if message[0] != 3 {
		s.t.Fatalf("client sent message type %d, want an update request", message[0])
	}
	return message[1] == 1, int(binary.BigEndian.Uint16(message[6:])), int(binary.BigEndian.Uint16(message[8:]))
}

// finish ends a script: the client sees the connection close and Run returns.
func (s *server) finish() { s.conn.Close() }

func TestHandshakeVersionsAndSecurity(t *testing.T) {
	for _, test := range []struct {
		version  string
		security byte
		password string
	}{
		{"008", 1, ""}, {"008", 2, "secret"}, {"007", 1, ""}, {"007", 2, "secret"}, {"003", 1, ""}, {"003", 2, "secret"},
		{"009", 1, ""}, {"009", 2, "secret"}, // a newer server: speak 3.8
		{"005", 1, ""}, {"005", 2, "secret"}, // an older nonstandard minor: speak 3.3
		{"889", 1, ""}, {"889", 2, "secret"}, // a vendor minor with standard security: speak 3.8
	} {
		offered := make(chan []int32, 1)
		client, err := connect(t, Config{Password: test.password}, func(s *server) {
			offered <- s.handshake(test.version, test.security, test.password, 640, 480)
		})
		encodings := <-offered
		if err != nil {
			t.Fatalf("%s/%d: %v", test.version, test.security, err)
		}
		if width, height := client.Size(); width != 640 || height != 480 || client.Name != "test desktop" {
			t.Fatalf("%s/%d: %dx%d %q", test.version, test.security, width, height, client.Name)
		}
		if int(client.Security) != int(test.security) {
			t.Fatalf("security = %d", client.Security)
		}
		if len(encodings) != len(DefaultEncodings) || encodings[0] != EncodingZRLE {
			t.Fatalf("encodings = %v", encodings)
		}
	}
}

func TestWrongAndMissingPassword(t *testing.T) {
	for _, version := range []string{"003", "007", "008", "009"} {
		t.Run("wrong password/3."+version, func(t *testing.T) {
			_, err := connect(t, Config{Password: "wrong"}, func(s *server) {
				s.handshake(version, 2, "right", 10, 10)
			})
			if !errors.Is(err, ErrAuthFailed) {
				t.Fatalf("err = %v, want ErrAuthFailed", err)
			}
		})
	}
	_, err := connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 2})
		s.read(1)
		s.write(make([]byte, 16))
	})
	if !errors.Is(err, ErrAuthRequired) {
		t.Fatalf("err = %v, want ErrAuthRequired", err)
	}
}

func TestHandshake33RejectsExtendedSecurityWithoutRequestingCredentials(t *testing.T) {
	for _, security := range []int{5, 13, 0x102, -1} {
		t.Run(fmt.Sprint(security), func(t *testing.T) {
			_, err := connect(t, Config{
				Password: "cached password",
				AskPassword: func() (string, error) {
					t.Error("password requested for an unsupported 3.3 security type")
					return "", nil
				},
				AskCredentials: func() (string, string, error) {
					t.Error("credentials requested for an unsupported 3.3 security type")
					return "", "", nil
				},
			}, func(s *server) {
				s.write([]byte("RFB 003.003\n"))
				s.read(12)
				s.write(u32(security))
			})
			if !errors.Is(err, ErrUnsupported) {
				t.Fatalf("error=%v want unsupported security", err)
			}
		})
	}
}

func TestPasswordIsAskedOnTheSameConnection(t *testing.T) {
	asked := 0
	client, err := connect(t, Config{AskPassword: func() (string, error) { asked++; return "typed", nil }}, func(s *server) {
		s.handshake("008", 2, "typed", 10, 10)
	})
	if err != nil || asked != 1 || client.Security != 2 {
		t.Fatalf("err %v, asked %d times", err, asked)
	}
	// Cancelling the question ends the connection with the caller's error.
	cancelled := errors.New("cancelled")
	_, err = connect(t, Config{AskPassword: func() (string, error) { return "", cancelled }}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 2})
		s.read(1)
		s.write(make([]byte, 16))
	})
	if !errors.Is(err, cancelled) {
		t.Fatalf("err = %v", err)
	}
}

func TestServerRefusalReasonIsShownWithoutControlCharacters(t *testing.T) {
	_, err := connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{0}, u32(len("too many\x1b[31m clients")), []byte("too many\x1b[31m clients"))
	})
	var refusal *ServerError
	if !errors.As(err, &refusal) || refusal.Reason != "too many[31m clients" {
		t.Fatalf("err = %v", err)
	}
}

func TestServerRefusalUsesTheNegotiatedVersionSecurityFormat(t *testing.T) {
	for _, version := range []struct {
		banner, clientVersion string
		security              []byte
	}{
		{"RFB 003.003\n", "RFB 003.003\n", u32(0)},
		{"RFB 003.007\n", "RFB 003.007\n", []byte{0}},
		{"RFB 003.008\n", "RFB 003.008\n", []byte{0}},
		{"RFB 005.000\n", "RFB 003.008\n", []byte{0}},
	} {
		t.Run(version.banner[:11], func(t *testing.T) {
			_, err := connect(t, Config{}, func(s *server) {
				s.write([]byte(version.banner))
				if got := s.read(12); string(got) != version.clientVersion {
					t.Errorf("client version=%q want=%q", got, version.clientVersion)
				}
				s.write(version.security, u32(len("server unavailable")), []byte("server unavailable"))
			})
			var refusal *ServerError
			if !errors.As(err, &refusal) || refusal.Reason != "server unavailable" || errors.Is(err, ErrAuthFailed) {
				t.Fatalf("error=%v want server refusal", err)
			}
		})
	}
}

func TestNoneSecurity38FailureReadsTheReason(t *testing.T) {
	_, err := connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 1})
		s.read(1)
		s.write(u32(1), u32(len("access denied")), []byte("access denied"))
	})
	if !errors.Is(err, ErrAuthFailed) || !strings.Contains(err.Error(), "access denied") {
		t.Fatalf("error=%v want authentication failure with server reason", err)
	}
}

func TestVendorMinorRejectsPrivateSecurity(t *testing.T) {
	_, err := connect(t, Config{Password: "cached password"}, func(s *server) {
		s.write([]byte("RFB 003.889\n"))
		if got := s.read(12); string(got) != "RFB 003.008\n" {
			t.Errorf("client version=%q want 3.8", got)
		}
		s.write([]byte{1, 30})
	})
	if !errors.Is(err, ErrUnsupported) {
		t.Fatalf("error=%v want unsupported security", err)
	}
}

func TestRefusesWhatIsNotRFBAndOversizeDesktops(t *testing.T) {
	_, err := connect(t, Config{}, func(s *server) { s.write([]byte("SSH-2.0-OpenSSH_9.6\r\n")) })
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v", err)
	}
	_, err = connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 1})
		s.read(1)
		s.write(u32(0))
		s.read(1)
		s.write(serverInit(16000, 16000, "huge"))
	})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want a refusal of the oversize desktop", err)
	}
	_, err = connect(t, Config{}, func(s *server) {
		s.write([]byte("RFB 003.008\n"))
		s.read(12)
		s.write([]byte{1, 1})
		s.read(1)
		s.write(u32(0))
		s.read(1)
		header := serverInit(10, 10, "")
		copy(header[20:], u32(1<<30))
		s.write(header)
	})
	if !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want a refusal of the huge name", err)
	}
}

// pixels returns n pixels of one color in wire order.
func pixels(n int, blue, green, red byte) []byte {
	return bytes.Repeat([]byte{blue, green, red, 0}, n)
}

// scenario runs a client against a script that, after the handshake, sends
// updates, and returns the picture once the server has hung up. Each update is
// followed by waiting for the client's next request, which is how a test knows
// the update was applied.
func scenario(t *testing.T, width, height int, send func(s *server, push func(update []byte))) *canvas {
	t.Helper()
	sink := &canvas{}
	var client *Client
	var connectErr error
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go func() {
		s.handshake("008", 1, "", width, height)
	}()
	client, connectErr = Connect(clientSide, Config{})
	if connectErr != nil {
		t.Fatal(connectErr)
	}
	done := run(client, sink)
	incremental, w, h := s.readRequest()
	if incremental || w != width || h != height {
		t.Fatalf("first request: incremental=%v %dx%d", incremental, w, h)
	}
	send(s, func(update []byte) {
		s.write(update)
		if incremental, _, _ := s.readRequest(); !incremental {
			t.Error("the request after an update must be incremental")
		}
	})
	s.finish()
	if err := wait(t, done); err == nil {
		t.Fatal("Run returned nil")
	} else if errors.Is(err, ErrProtocol) {
		t.Fatalf("the client refused the script: %v", err)
	}
	return sink
}

func TestRawAndCopyRect(t *testing.T) {
	sink := scenario(t, 8, 4, func(s *server, push func([]byte)) {
		raw := append(pixels(4, 1, 2, 3), pixels(4, 9, 8, 7)...)
		push(update(rect(0, 0, 4, 2, EncodingRaw, bytes.Repeat(raw[:16], 2)...)))
		push(update(rect(4, 2, 4, 2, EncodingCopyRect, u16(0, 0)...)))
		push(update(rect(0, 3, 2, 1, EncodingRaw, pixels(2, 5, 5, 5)...)))
	})
	if got := sink.at(5, 3); got != [3]byte{1, 2, 3} {
		t.Fatalf("copied pixel = %v", got)
	}
	if got := sink.at(1, 3); got != [3]byte{5, 5, 5} {
		t.Fatalf("raw pixel = %v", got)
	}
}

func TestRREAndHextile(t *testing.T) {
	sink := scenario(t, 40, 20, func(s *server, push func([]byte)) {
		// RRE: a 10x10 rectangle of blue with a red 2x3 inside at (4,5).
		body := append(u32(1), pixels(1, 200, 0, 0)...)
		body = append(body, pixels(1, 0, 0, 200)...)
		body = append(body, u16(4, 5, 2, 3)...)
		push(update(rect(0, 0, 10, 10, EncodingRRE, body...)))

		// Hextile over 24x18: tile 0 solid with background, tile 1 with a colored
		// subrectangle, tile 2 (the second row's first) raw, then tiles that reuse
		// the background and foreground that were set before.
		var tiles []byte
		tiles = append(tiles, hextileBackground)
		tiles = append(tiles, pixels(1, 10, 20, 30)...)
		tiles = append(tiles, hextileSubrects|hextileColored, 1)
		tiles = append(tiles, pixels(1, 50, 60, 70)...)
		tiles = append(tiles, 0x21, 0x10) // x=2,y=1 w=2,h=1
		// second row, tile 0 (16x2): raw
		tiles = append(tiles, hextileRaw)
		tiles = append(tiles, pixels(32, 99, 98, 97)...)
		// second row, tile 1 (8x2): inherits the background, foreground subrect
		tiles = append(tiles, hextileForeground|hextileSubrects)
		tiles = append(tiles, pixels(1, 1, 1, 1)...)
		tiles = append(tiles, 1, 0x00, 0x00)
		push(update(rect(10, 0, 24, 18, EncodingHextile, tiles...)))
	})
	if sink.at(5, 5) != [3]byte{0, 0, 200} || sink.at(4, 7) != [3]byte{0, 0, 200} || sink.at(3, 5) != [3]byte{200, 0, 0} || sink.at(6, 5) != [3]byte{200, 0, 0} {
		t.Fatalf("RRE: %v %v", sink.at(5, 5), sink.at(3, 5))
	}
	if sink.at(10, 0) != [3]byte{10, 20, 30} || sink.at(10+16+2, 1) != [3]byte{50, 60, 70} || sink.at(10+16+4, 1) != [3]byte{10, 20, 30} {
		t.Fatalf("hextile row 1: %v %v", sink.at(10, 0), sink.at(28, 1))
	}
	if sink.at(10, 16) != [3]byte{99, 98, 97} || sink.at(26, 16) != [3]byte{1, 1, 1} || sink.at(27, 16) != [3]byte{10, 20, 30} {
		t.Fatalf("hextile row 2: %v %v %v", sink.at(10, 16), sink.at(26, 16), sink.at(27, 16))
	}
}

// zrleStream compresses ZRLE rectangle bodies the way a server does: one zlib
// stream for the whole connection, flushed at the end of every rectangle.
type zrleStream struct {
	buffer bytes.Buffer
	writer *zlib.Writer
}

func newZRLEStream() *zrleStream {
	stream := &zrleStream{}
	stream.writer = zlib.NewWriter(&stream.buffer)
	return stream
}

func (stream *zrleStream) rect(x, y, width, height int, tiles []byte) []byte {
	stream.buffer.Reset()
	stream.writer.Write(tiles)
	stream.writer.Flush()
	return rect(x, y, width, height, EncodingZRLE, append(u32(stream.buffer.Len()), stream.buffer.Bytes()...)...)
}

func cpixel(blue, green, red byte) []byte { return []byte{blue, green, red} }

func TestZRLEAllSubencodingsAcrossRectanglesOnOneStream(t *testing.T) {
	stream := newZRLEStream()
	sink := scenario(t, 130, 70, func(s *server, push func([]byte)) {
		// One 130x70 rectangle: a row of tiles 64, 64 and 2 wide, 64 high, then a
		// row 6 high.
		var tiles []byte
		tiles = append(tiles, 1) // (0,0) solid
		tiles = append(tiles, cpixel(11, 12, 13)...)
		tiles = append(tiles, 2) // (1,0) packed palette of two: 1 bit per pixel
		tiles = append(tiles, cpixel(1, 1, 1)...)
		tiles = append(tiles, cpixel(2, 2, 2)...)
		for range 64 {
			tiles = append(tiles, 0x80, 0, 0, 0, 0, 0, 0, 0) // the row's first pixel is palette entry 1
		}
		tiles = append(tiles, 0) // (2,0) raw, 2x64
		for range 2 * 64 {
			tiles = append(tiles, cpixel(7, 8, 9)...)
		}
		tiles = append(tiles, 128) // (0,1) plain RLE, 64x6 = 384 pixels in one run
		tiles = append(tiles, cpixel(21, 22, 23)...)
		tiles = append(tiles, 255, 128)
		tiles = append(tiles, 130) // (1,1) palette RLE with two entries: 64 pixels of 0, then 1, then run 319 of 0
		tiles = append(tiles, cpixel(31, 32, 33)...)
		tiles = append(tiles, cpixel(41, 42, 43)...)
		tiles = append(tiles, 0x80, 63)      // entry 0, run 64
		tiles = append(tiles, 1)             // entry 1, a single pixel
		tiles = append(tiles, 0x80, 255, 63) // entry 0, run 1+255+63 = 319
		tiles = append(tiles, 5)             // (2,1) packed palette of 5 entries: 4 bits per pixel, 2x6
		for index := range 5 {
			tiles = append(tiles, cpixel(byte(50+index), 0, 0)...)
		}
		for range 6 {
			tiles = append(tiles, 0x34) // pixels 3 and 4
		}
		push(update(stream.rect(0, 0, 130, 70, tiles)))
		// The second rectangle continues the same zlib stream.
		push(update(stream.rect(100, 10, 3, 1, append([]byte{1}, cpixel(61, 62, 63)...))))
	})
	checks := []struct {
		x, y int
		want [3]byte
	}{
		{0, 0, [3]byte{11, 12, 13}}, {63, 63, [3]byte{11, 12, 13}},
		{64, 0, [3]byte{2, 2, 2}}, {65, 0, [3]byte{1, 1, 1}}, {64, 5, [3]byte{2, 2, 2}},
		{128, 63, [3]byte{7, 8, 9}}, {129, 0, [3]byte{7, 8, 9}},
		{0, 64, [3]byte{21, 22, 23}}, {63, 69, [3]byte{21, 22, 23}},
		{64, 64, [3]byte{31, 32, 33}}, {64 + 63, 64, [3]byte{31, 32, 33}}, {64, 65, [3]byte{41, 42, 43}}, {64 + 1, 65, [3]byte{31, 32, 33}},
		{128, 64, [3]byte{53, 0, 0}}, {129, 64, [3]byte{54, 0, 0}},
		{100, 10, [3]byte{61, 62, 63}}, {102, 10, [3]byte{61, 62, 63}}, {103, 10, [3]byte{1, 1, 1}},
	}
	for _, check := range checks {
		if got := sink.at(check.x, check.y); got != check.want {
			t.Errorf("pixel (%d,%d) = %v, want %v", check.x, check.y, got, check.want)
		}
	}
}

func TestDesktopSizeClipboardBellAndRequestScheduling(t *testing.T) {
	sink := &canvas{}
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 20, 10)
	client, err := Connect(clientSide, Config{})
	if err != nil {
		t.Fatal(err)
	}
	done := run(client, sink)
	s.readRequest()

	// The server shrinks the desktop; the client asks for all of it again.
	s.write(update(rect(0, 0, 8, 6, encodingDesktopSize)))
	incremental, width, height := s.readRequest()
	if incremental || width != 8 || height != 6 {
		t.Fatalf("after a resize: incremental=%v %dx%d", incremental, width, height)
	}
	if sink.width != 8 || sink.height != 6 {
		t.Fatalf("sink size = %dx%d", sink.width, sink.height)
	}
	// A rectangle that was fine before the resize is outside now.
	s.write(update(rect(10, 0, 5, 5, EncodingRaw)))
	if err := wait(t, done); !errors.Is(err, ErrProtocol) {
		t.Fatalf("err = %v, want a protocol error for the rectangle outside the desktop", err)
	}
}

func TestClipboardBellAndInactiveClientAsksForNothing(t *testing.T) {
	sink := &canvas{}
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 20, 10)
	client, err := Connect(clientSide, Config{})
	if err != nil {
		t.Fatal(err)
	}
	done := run(client, sink)
	s.readRequest()
	s.write([]byte{2})
	s.write([]byte{3, 0, 0, 0}, u32(4), []byte("caf\xe9"))
	// A clipboard larger than the limit is read and dropped, not stored.
	big := bytes.Repeat([]byte("x"), MaxClipboard+1)
	s.write([]byte{3, 0, 0, 0}, u32(len(big)), big)
	client.SetActive(false)
	s.write(update(rect(0, 0, 1, 1, EncodingRaw, pixels(1, 4, 5, 6)...)))
	// Nothing was requested while inactive: the next bytes the server can read
	// are the ones sent when the client becomes active again.
	time.Sleep(50 * time.Millisecond)
	go client.SetActive(true)
	if incremental, _, _ := s.readRequest(); !incremental {
		t.Fatal("resuming must ask for what changed meanwhile")
	}
	sink.mu.Lock()
	text, bells := sink.text, sink.bells
	sink.mu.Unlock()
	if text != "café" || bells != 1 {
		t.Fatalf("clipboard %q, bells %d", text, bells)
	}
	s.finish()
	wait(t, done)
}

func TestInputMessages(t *testing.T) {
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 20, 10)
	client, err := Connect(clientSide, Config{})
	if err != nil {
		t.Fatal(err)
	}
	go func() {
		client.SendPointer(7, 9, 5)
		client.SendKey(0xff0d, true)
		client.SendClipboard("a\r\nb€")
	}()
	if got := s.read(6); !bytes.Equal(got, []byte{5, 5, 0, 7, 0, 9}) {
		t.Fatalf("pointer = %v", got)
	}
	if got := s.read(8); !bytes.Equal(got, []byte{4, 1, 0, 0, 0, 0, 0xff, 0x0d}) {
		t.Fatalf("key = %v", got)
	}
	if got := s.read(8 + 4); !bytes.Equal(got, append([]byte{6, 0, 0, 0, 0, 0, 0, 4}, "a\nb?"...)) {
		t.Fatalf("clipboard = %q", got)
	}
}

// slowConn delivers one byte per Read, the worst way a network can split what a
// server sent.
type slowConn struct{ net.Conn }

func (c slowConn) Read(p []byte) (int, error) { return c.Conn.Read(p[:min(len(p), 1)]) }

func TestEveryEncodingSurvivesInputSplitIntoSingleBytes(t *testing.T) {
	stream := newZRLEStream()
	sink := &canvas{}
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 40, 40)
	client, err := Connect(slowConn{clientSide}, Config{})
	if err != nil {
		t.Fatal(err)
	}
	done := run(client, sink)
	s.readRequest()
	var tiles []byte
	tiles = append(tiles, 1)
	tiles = append(tiles, cpixel(1, 2, 3)...)
	updates := [][]byte{
		update(rect(0, 0, 2, 1, EncodingRaw, pixels(2, 9, 9, 9)...)),
		update(rect(2, 0, 2, 1, EncodingCopyRect, u16(0, 0)...)),
		update(rect(0, 2, 4, 4, EncodingRRE, append(u32(0), pixels(1, 8, 8, 8)...)...)),
		update(rect(0, 8, 20, 20, EncodingHextile, append([]byte{hextileBackground}, append(pixels(1, 7, 7, 7), []byte{0, 0, 0}...)...)...)),
		update(stream.rect(20, 20, 8, 8, tiles)),
	}
	for _, message := range updates {
		s.write(message)
		s.readRequest()
	}
	s.finish()
	wait(t, done)
	if sink.at(3, 0) != [3]byte{9, 9, 9} || sink.at(1, 3) != [3]byte{8, 8, 8} || sink.at(5, 9) != [3]byte{7, 7, 7} || sink.at(27, 27) != [3]byte{1, 2, 3} {
		t.Fatal("a picture drawn from single-byte reads differs from the one meant")
	}
}

func TestMaliciousServerCannotMakeTheClientAllocateOrLoop(t *testing.T) {
	for name, body := range map[string][]byte{
		"rectangle outside":      update(rect(0, 0, 100, 100, EncodingRaw)),
		"unknown encoding":       update(rect(0, 0, 1, 1, 12345)),
		"copy from outside":      update(rect(0, 0, 4, 4, EncodingCopyRect, u16(30, 30)...)),
		"RRE count":              update(rect(0, 0, 4, 4, EncodingRRE, u32(1<<30)...)),
		"RRE subrect outside":    update(rect(0, 0, 4, 4, EncodingRRE, append(append(u32(1), pixels(2, 0, 0, 0)...), u16(3, 3, 5, 5)...)...)),
		"hextile subrect":        update(rect(0, 0, 16, 16, EncodingHextile, append([]byte{hextileSubrects, 1}, 0xf0, 0xff)...)),
		"ZRLE length":            update(rect(0, 0, 4, 4, EncodingZRLE, u32(1<<31)...)),
		"ZRLE not zlib":          update(rect(0, 0, 4, 4, EncodingZRLE, append(u32(8), 1, 2, 3, 4, 5, 6, 7, 8)...)),
		"cut text length":        {3, 0, 0, 0, 0xff, 0xff, 0xff, 0xff},
		"unknown message":        {99},
		"desktop size too large": update(rect(0, 0, 16000, 16000, encodingDesktopSize)),
	} {
		t.Run(name, func(t *testing.T) {
			sink := &canvas{}
			clientSide, serverSide := pair(t)
			s := &server{t: t, conn: serverSide}
			go s.handshake("008", 1, "", 30, 30)
			client, err := Connect(clientSide, Config{})
			if err != nil {
				t.Fatal(err)
			}
			done := run(client, sink)
			s.readRequest()
			go s.write(body)
			if err := wait(t, done); !errors.Is(err, ErrProtocol) {
				t.Fatalf("err = %v, want a protocol error", err)
			}
		})
	}
}

func TestZRLEBadTilesAreRefused(t *testing.T) {
	for name, tiles := range map[string][]byte{
		"palette index out of range": append(append([]byte{2}, pixels(0, 0, 0, 0)...), 0, 0),
		"run past the tile":          append(append([]byte{128}, cpixel(1, 1, 1)...), 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255, 255),
		"reserved subencoding":       {17},
	} {
		t.Run(name, func(t *testing.T) {
			stream := newZRLEStream()
			sink := &canvas{}
			clientSide, serverSide := pair(t)
			s := &server{t: t, conn: serverSide}
			go s.handshake("008", 1, "", 30, 30)
			client, err := Connect(clientSide, Config{})
			if err != nil {
				t.Fatal(err)
			}
			done := run(client, sink)
			s.readRequest()
			go s.write(update(stream.rect(0, 0, 4, 4, tiles)))
			if err := wait(t, done); err == nil {
				t.Fatal("a bad tile was accepted")
			}
		})
	}
}

func TestVNCResponseMatchesAnIndependentDESImplementation(t *testing.T) {
	// Computed with OpenSSL: des-ecb under the password "password" with the bits
	// of each key byte reversed, over the challenge 00 01 .. 0f.
	var challenge [16]byte
	for index := range challenge {
		challenge[index] = byte(index)
	}
	want := [16]byte{0xb8, 0x66, 0x92, 0x41, 0x25, 0xc8, 0xee, 0xbb, 0x9d, 0xeb, 0xc1, 0xdb, 0x61, 0xc5, 0x38, 0xe2}
	if got := vncResponse(challenge, "password"); got != want {
		t.Fatalf("response = %x, want %x", got, want)
	}
	if vncResponse(challenge, "password-and-more") != want {
		t.Fatal("bytes past the eighth changed the response")
	}
}
