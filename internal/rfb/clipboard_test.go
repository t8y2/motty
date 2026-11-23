package rfb

import (
	"bytes"
	"compress/zlib"
	"encoding/binary"
	"io"
	"net"
	"slices"
	"testing"
	"time"
)

// serverClipboard is a ServerCutText of the extended clipboard.
func serverClipboard(flags uint32, payload []byte) []byte {
	message := []byte{3, 0, 0, 0}
	message = binary.BigEndian.AppendUint32(message, uint32(int32(-(4 + len(payload)))))
	message = binary.BigEndian.AppendUint32(message, flags)
	return append(message, payload...)
}

// textProvide is the payload of a Provide of text: a zlib stream of its length
// and its bytes.
func textProvide(t testing.TB, text string) []byte {
	t.Helper()
	var compressed bytes.Buffer
	writer := zlib.NewWriter(&compressed)
	writer.Write(u32(len(text)))
	writer.Write([]byte(text))
	if err := writer.Close(); err != nil {
		t.Fatal(err)
	}
	return compressed.Bytes()
}

// readClipboard reads a ClientCutText of the extended clipboard and returns its
// flags and what follows them.
func (s *server) readClipboard() (flags uint32, payload []byte) {
	s.t.Helper()
	header := s.read(8)
	length := int32(binary.BigEndian.Uint32(header[4:]))
	if header[0] != 6 || length > -4 {
		s.t.Fatalf("client sent %v, want an extended clipboard message", header)
	}
	body := s.read(int(-length))
	return binary.BigEndian.Uint32(body), body[4:]
}

func (s *server) serverCaps(flags uint32) {
	s.write(serverClipboard(flags, u32(20<<20)))
}

const allCaps = clipCaps | clipUTF8 | clipRequest | clipNotify | clipProvide

// The client asks for the extended clipboard, answers the server's capabilities
// with its own, and then sends text as UTF-8 when the server asks for it and
// takes the UTF-8 text the server provides, however the bytes arrive.
func TestExtendedClipboardCarriesUTF8BothWays(t *testing.T) {
	for name, wrap := range map[string]func(net.Conn) net.Conn{
		"whole":  func(conn net.Conn) net.Conn { return conn },
		"1 byte": func(conn net.Conn) net.Conn { return slowConn{conn} },
	} {
		t.Run(name, func(t *testing.T) {
			sink := &canvas{}
			clientSide, serverSide := pair(t)
			s := &server{t: t, conn: serverSide}
			offered := make(chan []int32, 1)
			go func() { offered <- s.handshake("008", 1, "", 20, 10) }()
			client, err := Connect(wrap(clientSide), Config{})
			if err != nil {
				t.Fatal(err)
			}
			if !slices.Contains(<-offered, encodingExtendedClipboard) {
				t.Fatal("the extended clipboard pseudo-encoding was not offered")
			}
			done := run(client, sink)
			s.readRequest()

			s.serverCaps(allCaps)
			if flags, _ := s.readClipboard(); flags&clipCaps == 0 || flags&clipUTF8 == 0 {
				t.Fatalf("client capabilities = %#x", flags)
			}

			text := "中文 注释\r\nSELECT '€';"
			go client.SendClipboard("中文 注释\nSELECT '€';")
			if flags, payload := s.readClipboard(); flags != clipNotify|clipUTF8 || len(payload) != 0 {
				t.Fatalf("announcement = %#x %v", flags, payload)
			}
			s.write(serverClipboard(clipRequest|clipUTF8, nil))
			flags, payload := s.readClipboard()
			if flags != clipProvide|clipUTF8 {
				t.Fatalf("answer = %#x", flags)
			}
			stream, err := zlib.NewReader(bytes.NewReader(payload))
			if err != nil {
				t.Fatal(err)
			}
			got, _ := io.ReadAll(stream)
			if want := append(append(u32(len(text)+1), text...), 0); !bytes.Equal(got, want) {
				t.Fatalf("provided %q, want %q", got, want)
			}

			s.write(serverClipboard(clipNotify|clipUTF8, nil))
			if flags, _ := s.readClipboard(); flags != clipRequest|clipUTF8 {
				t.Fatalf("request = %#x", flags)
			}
			s.write(serverClipboard(clipProvide|clipUTF8, textProvide(t, "你好\r\nworld\x00")))
			deadline := time.Now().Add(5 * time.Second)
			for {
				sink.mu.Lock()
				text := sink.text
				sink.mu.Unlock()
				if text == "你好\nworld" {
					break
				}
				if time.Now().After(deadline) {
					t.Fatalf("server text = %q", text)
				}
				time.Sleep(time.Millisecond)
			}
			s.finish()
			wait(t, done)
		})
	}
}

// The pseudo-encoding number is the protocol's, not ours: a server only offers
// the extended clipboard to a client that names it exactly.
func TestExtendedClipboardEncodingNumber(t *testing.T) {
	number := encodingExtendedClipboard
	if got := uint32(number); got != 0xC0A1E5CE {
		t.Fatalf("extended clipboard pseudo-encoding = %#x, want 0xC0A1E5CE", got)
	}
}

// Text given before the server has had its say on the extended clipboard is not
// sent as Latin-1: a server that speaks UTF-8 gets it as UTF-8 once it says so,
// and one that does not gets Latin-1 when its first update shows it never will.
func TestClipboardGivenEarlyWaitsForCapabilities(t *testing.T) {
	t.Run("server speaks UTF-8", func(t *testing.T) {
		clientSide, serverSide := pair(t)
		s := &server{t: t, conn: serverSide}
		go s.handshake("008", 1, "", 20, 10)
		client, err := Connect(clientSide, Config{})
		if err != nil {
			t.Fatal(err)
		}
		done := run(client, &canvas{})
		s.readRequest()
		if err := client.SendClipboard("中文"); err != nil {
			t.Fatal(err)
		}
		s.serverCaps(allCaps)
		if flags, _ := s.readClipboard(); flags&clipCaps == 0 {
			t.Fatalf("client capabilities = %#x", flags)
		}
		if flags, _ := s.readClipboard(); flags != clipNotify|clipUTF8 {
			t.Fatalf("announcement = %#x", flags)
		}
		s.write(serverClipboard(clipRequest|clipUTF8, nil))
		flags, payload := s.readClipboard()
		if flags != clipProvide|clipUTF8 {
			t.Fatalf("answer = %#x", flags)
		}
		stream, err := zlib.NewReader(bytes.NewReader(payload))
		if err != nil {
			t.Fatal(err)
		}
		got, _ := io.ReadAll(stream)
		if want := append(append(u32(len("中文")+1), "中文"...), 0); !bytes.Equal(got, want) {
			t.Fatalf("provided %q, want %q", got, want)
		}
		s.finish()
		wait(t, done)
	})
	t.Run("server does not", func(t *testing.T) {
		clientSide, serverSide := pair(t)
		s := &server{t: t, conn: serverSide}
		go s.handshake("008", 1, "", 20, 10)
		client, err := Connect(clientSide, Config{})
		if err != nil {
			t.Fatal(err)
		}
		done := run(client, &canvas{})
		s.readRequest()
		client.SendClipboard("old")
		client.SendClipboard("a€")
		s.write(update(rect(0, 0, 1, 1, EncodingRaw, pixels(1, 4, 5, 6)...)))
		if got := s.read(8 + 2); !bytes.Equal(got, append([]byte{6, 0, 0, 0, 0, 0, 0, 2}, "a?"...)) {
			t.Fatalf("clipboard = %v", got)
		}
		s.finish()
		wait(t, done)
	})
}

// A server that cannot announce, request and provide UTF-8 gets no capabilities
// back, and the client keeps to Latin-1 for it.
func TestExtendedClipboardNeedsEveryAction(t *testing.T) {
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
	s.serverCaps(clipCaps | clipUTF8 | clipProvide)
	s.write([]byte{2}) // a bell, after which the capabilities have been handled
	go client.SendClipboard("a€")
	if got := s.read(8 + 2); !bytes.Equal(got, append([]byte{6, 0, 0, 0, 0, 0, 0, 2}, "a?"...)) {
		t.Fatalf("clipboard = %v", got)
	}
	s.finish()
	wait(t, done)
}

// A Peek is answered with what the client has to offer.
func TestExtendedClipboardPeek(t *testing.T) {
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 20, 10)
	client, err := Connect(clientSide, Config{})
	if err != nil {
		t.Fatal(err)
	}
	done := run(client, &canvas{})
	s.readRequest()
	s.serverCaps(allCaps)
	s.readClipboard()
	s.write(serverClipboard(clipPeek, nil))
	if flags, _ := s.readClipboard(); flags != clipNotify {
		t.Fatalf("nothing to offer, but flags = %#x", flags)
	}
	go client.SendClipboard("x")
	s.readClipboard()
	s.write(serverClipboard(clipPeek, nil))
	if flags, _ := s.readClipboard(); flags != clipNotify|clipUTF8 {
		t.Fatalf("text to offer, but flags = %#x", flags)
	}
	s.finish()
	wait(t, done)
}

func TestExtendedClipboardRefusesWhatItCannotRead(t *testing.T) {
	tooLong := -(maxCutText + 1)
	huge := binary.BigEndian.AppendUint32(nil, uint32(int32(tooLong)))
	for name, message := range map[string][]byte{
		"too short":  append([]byte{3, 0, 0, 0}, u32(-3)...),
		"too long":   append([]byte{3, 0, 0, 0}, huge...),
		"bad stream": serverClipboard(clipProvide|clipUTF8, []byte("not zlib")),
		"short list": serverClipboard(clipCaps|clipUTF8, nil),
	} {
		client := clientOver(message[1:], 40, 30)
		if err := client.cutText(&nullSink{t: t}); err == nil {
			t.Errorf("%s: no error", name)
		}
	}
}
