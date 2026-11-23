package rfb

import (
	"bytes"
	"encoding/binary"
	"slices"
	"testing"
)

func bitsOf(format PixelFormat) byte {
	switch format {
	case Pixel16:
		return 16
	case Pixel8:
		return 8
	}
	return 32
}

// declared is a format as its SetPixelFormat message states it, read back
// independently of the decoder: how wide each channel is and where it sits.
type declared struct {
	bits                            int
	redMax, greenMax, blueMax       int
	redShift, greenShift, blueShift uint
}

func declaredBy(message []byte) declared {
	return declared{
		bits:   int(message[4]),
		redMax: int(binary.BigEndian.Uint16(message[8:])), greenMax: int(binary.BigEndian.Uint16(message[10:])), blueMax: int(binary.BigEndian.Uint16(message[12:])),
		redShift: uint(message[14]), greenShift: uint(message[15]), blueShift: uint(message[16]),
	}
}

// pack is the wire pixel (little-endian) a server holding this colour sends,
// and what a viewer should show for it: each channel quantised to the declared
// width and scaled back to eight bits.
func (d declared) pack(blue, green, red byte) (wire []byte, shown [3]byte) {
	quantise := func(value byte, top int) int { return (int(value)*top + 127) / 255 }
	widen := func(value, top int) byte { return byte((value*255 + top/2) / top) }
	r, g, b := quantise(red, d.redMax), quantise(green, d.greenMax), quantise(blue, d.blueMax)
	value := uint32(r)<<d.redShift | uint32(g)<<d.greenShift | uint32(b)<<d.blueShift
	for index := 0; index < d.bits/8; index++ {
		wire = append(wire, byte(value>>(8*index)))
	}
	return wire, [3]byte{widen(b, d.blueMax), widen(g, d.greenMax), widen(r, d.redMax)}
}

var smallFormats = []PixelFormat{Pixel16, Pixel8}

func TestEveryPixelOfASmallFormatIsDecodedAsItsMessageDeclares(t *testing.T) {
	for _, format := range []PixelFormat{Pixel32, Pixel16, Pixel8} {
		d := declaredBy(format.message())
		if d.bits != int(bitsOf(format)) {
			t.Fatalf("format %d declares %d bits", format, d.bits)
		}
		if format == Pixel32 {
			continue // no conversion to check
		}
		for value := 0; value < 1<<d.bits; value++ {
			wire := []byte{byte(value), byte(value >> 8)}[:d.bits/8]
			r := (value >> d.redShift) & d.redMax
			g := (value >> d.greenShift) & d.greenMax
			b := (value >> d.blueShift) & d.blueMax
			want := [3]byte{byte((b*255 + d.blueMax/2) / d.blueMax), byte((g*255 + d.greenMax/2) / d.greenMax), byte((r*255 + d.redMax/2) / d.redMax)}
			blue, green, red := format.bgr(wire)
			if got := [3]byte{blue, green, red}; got != want {
				t.Fatalf("format %d pixel %#x = %v, want %v", format, value, got, want)
			}
		}
		// Full intensity must stay full intensity, and black black.
		if blue, green, red := format.bgr(slices.Repeat([]byte{0xff}, 2)); blue != 255 || green != 255 || red != 255 {
			t.Fatalf("format %d white = %d %d %d", format, blue, green, red)
		}
	}
}

// formatPicture draws every encoding at a small pixel format and checks the
// picture the sink got.
func formatPicture(t *testing.T, format PixelFormat, split bool) {
	d := declaredBy(format.message())
	pixel := func(blue, green, red byte) ([]byte, [3]byte) { return d.pack(blue, green, red) }
	repeat := func(wire []byte, n int) []byte { return slices.Repeat(wire, n) }

	rawWire, rawShown := pixel(10, 200, 90)
	rreBack, rreBackShown := pixel(255, 0, 0)
	rreFore, rreForeShown := pixel(0, 255, 0)
	hexBack, hexBackShown := pixel(30, 60, 120)
	hexColored, hexColoredShown := pixel(200, 100, 50)
	hexFore, hexForeShown := pixel(250, 250, 250)
	hexRaw, hexRawShown := pixel(33, 66, 99)
	zrleSolid, zrleSolidShown := pixel(90, 80, 70)
	zrleA, zrleAShown := pixel(5, 130, 250)
	zrleB, zrleBShown := pixel(240, 20, 130)

	// The ZRLE CPIXEL of a smaller format is the pixel itself.
	stream := newZRLEStream()
	config := Config{PixelFormat: format}
	var sink *canvas
	body := func(s *server, push func([]byte)) {
		push(update(rect(0, 0, 3, 2, EncodingRaw, repeat(rawWire, 6)...)))

		rre := append(u32(1), rreBack...)
		rre = append(rre, rreFore...)
		rre = append(rre, u16(1, 1, 2, 1)...)
		push(update(rect(4, 0, 4, 3, EncodingRRE, rre...)))

		// Hextile over 48x2: a tile with a background and a foreground subrectangle,
		// one that inherits the background and has a coloured subrectangle, and a
		// raw one.
		var tiles []byte
		tiles = append(tiles, hextileBackground|hextileForeground|hextileSubrects)
		tiles = append(tiles, hexBack...)
		tiles = append(tiles, hexFore...)
		tiles = append(tiles, 1, 0x00, 0x10) // x=0,y=0 w=2,h=1
		tiles = append(tiles, hextileSubrects|hextileColored, 1)
		tiles = append(tiles, hexColored...)
		tiles = append(tiles, 0x00, 0x10)
		tiles = append(tiles, hextileRaw)
		tiles = append(tiles, repeat(hexRaw, 16*2)...)
		push(update(rect(0, 4, 48, 2, EncodingHextile, tiles...)))

		// ZRLE over 70x2: a solid tile, then a 6-wide tile that is a palette of two.
		var zrle []byte
		zrle = append(zrle, 1)
		zrle = append(zrle, zrleSolid...)
		zrle = append(zrle, 2)
		zrle = append(zrle, zrleA...)
		zrle = append(zrle, zrleB...)
		zrle = append(zrle, 0x80, 0x80) // two rows: 6 bits each, first pixel of the row is entry 1
		push(update(stream.rect(0, 8, 70, 2, zrle)))
	}
	if split {
		sink = &canvas{}
		clientSide, serverSide := pair(t)
		s := &server{t: t, conn: serverSide, bits: bitsOf(format)}
		go s.handshake("008", 1, "", 80, 12)
		client, err := Connect(slowConn{clientSide}, config)
		if err != nil {
			t.Fatal(err)
		}
		done := make(chan error, 1)
		go func() { done <- client.Run(sink) }()
		s.readRequest()
		body(s, func(update []byte) { s.write(update); s.readRequest() })
		s.finish()
		wait(t, done)
	} else {
		sink = scenarioWith(t, config, 80, 12, body)
	}

	check := func(what string, x, y int, want [3]byte) {
		t.Helper()
		if got := sink.at(x, y); got != want {
			t.Errorf("format %d %s at (%d,%d) = %v, want %v", format, what, x, y, got, want)
		}
	}
	check("raw", 0, 0, rawShown)
	check("raw", 2, 1, rawShown)
	check("rre background", 4, 0, rreBackShown)
	check("rre subrectangle", 5, 1, rreForeShown)
	check("rre subrectangle", 6, 1, rreForeShown)
	check("rre background after subrectangle", 7, 1, rreBackShown)
	check("hextile background", 5, 4, hexBackShown)
	check("hextile foreground subrectangle", 1, 4, hexForeShown)
	check("hextile inherited background", 20, 5, hexBackShown)
	check("hextile coloured subrectangle", 17, 4, hexColoredShown)
	check("hextile raw", 32, 4, hexRawShown)
	check("hextile raw", 47, 5, hexRawShown)
	check("zrle solid", 0, 8, zrleSolidShown)
	check("zrle solid", 63, 9, zrleSolidShown)
	check("zrle palette entry 1", 64, 8, zrleBShown)
	check("zrle palette entry 0", 65, 8, zrleAShown)
}

func TestSmallPixelFormatsDecodeEveryEncoding(t *testing.T) {
	for _, format := range smallFormats {
		formatPicture(t, format, false)
	}
}

func TestSmallPixelFormatsSurviveInputSplitIntoSingleBytes(t *testing.T) {
	for _, format := range smallFormats {
		formatPicture(t, format, true)
	}
}

func TestClientAsksForTheConfiguredPixelFormat(t *testing.T) {
	for _, format := range []PixelFormat{Pixel32, Pixel16, Pixel8} {
		sentFormat := make(chan []byte, 1)
		_, err := connect(t, Config{PixelFormat: format}, func(s *server) {
			s.bits = bitsOf(format)
			s.handshake("008", 1, "", 10, 10)
			sentFormat <- s.format
		})
		if err != nil {
			t.Fatal(err)
		}
		sent := <-sentFormat
		if want := format.message(); !slices.Equal(sent, want) {
			t.Errorf("format %d: SetPixelFormat = %v, want %v", format, sent, want)
		}
	}
}

func TestPreferEncodingPutsOneFirstAndKeepsTheRest(t *testing.T) {
	for _, preferred := range []int32{EncodingZRLE, EncodingHextile, EncodingRRE, EncodingRaw} {
		order := PreferEncoding(preferred)
		if order[0] != preferred || len(order) != len(DefaultEncodings) {
			t.Fatalf("PreferEncoding(%d) = %v", preferred, order)
		}
		if !slices.Equal(slices.Sorted(slices.Values(order)), slices.Sorted(slices.Values(DefaultEncodings))) {
			t.Fatalf("PreferEncoding(%d) = %v changes the set of encodings", preferred, order)
		}
	}
	// Not an alternative to a pixel encoding: nothing to prefer.
	if order := PreferEncoding(EncodingCopyRect); !slices.Equal(order, DefaultEncodings) {
		t.Fatalf("PreferEncoding(CopyRect) = %v", order)
	}
	// What a client with a preference sends is what the server reads.
	offered := make(chan []int32, 1)
	_, err := connect(t, Config{Encodings: PreferEncoding(EncodingRaw)}, func(s *server) {
		offered <- s.handshake("008", 1, "", 10, 10)
	})
	if err != nil {
		t.Fatal(err)
	}
	if got := <-offered; got[0] != EncodingRaw {
		t.Fatalf("encodings on the wire = %v", got)
	}
}

func BenchmarkDecodeRaw(b *testing.B) {
	for _, format := range []PixelFormat{Pixel32, Pixel16, Pixel8} {
		b.Run(map[PixelFormat]string{Pixel32: "32", Pixel16: "16", Pixel8: "8"}[format], func(b *testing.B) {
			const width, height = 1024, 64
			wire := slices.Repeat([]byte{0x12}, width*height*format.size())
			body := rect(0, 0, width, height, EncodingRaw, wire...)[12:]
			client := clientOver(nil, width, height)
			client.format = format
			sink := &nullSink{t: b, width: width, height: height}
			b.SetBytes(int64(len(body)))
			b.ReportAllocs()
			for b.Loop() {
				client.r.Reset(bytes.NewReader(body))
				if err := client.decodeRaw(sink, 0, 0, width, height); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

func BenchmarkDecodeZRLE(b *testing.B) {
	for _, format := range []PixelFormat{Pixel32, Pixel16, Pixel8} {
		b.Run(map[PixelFormat]string{Pixel32: "32", Pixel16: "16", Pixel8: "8"}[format], func(b *testing.B) {
			const width, height = 1024, 64
			stream := newZRLEStream()
			var tiles []byte
			for left := 0; left < width; left += 64 {
				tw := min(64, width-left)
				tiles = append(tiles, 0) // raw mode
				for range tw * height {
					tiles = append(tiles, bytes.Repeat([]byte{0x12}, format.compressedSize())...)
				}
			}
			rectData := stream.rect(0, 0, width, height, tiles)
			body := rectData[12:] // skip 12-byte rect header
			client := clientOver(nil, width, height)
			client.format = format
			sink := &nullSink{t: b, width: width, height: height}
			b.SetBytes(int64(width * height * 4))
			b.ReportAllocs()
			for b.Loop() {
				client.zrle = zrleState{}
				client.r.Reset(bytes.NewReader(body))
				if err := client.decodeZRLE(sink, 0, 0, width, height); err != nil {
					b.Fatal(err)
				}
			}
		})
	}
}

// readQuality reads the SetPixelFormat and SetEncodings a client sends for
// SetQuality and returns the bits per pixel and the first encoding.
func (s *server) readQuality() (bits byte, first int32) {
	s.t.Helper()
	format := s.read(20)
	if format[0] != 0 {
		s.t.Fatalf("client sent message type %d, want SetPixelFormat", format[0])
	}
	header := s.read(4)
	if header[0] != 2 {
		s.t.Fatalf("client sent message type %d, want SetEncodings", header[0])
	}
	for index := range binary.BigEndian.Uint16(header[2:]) {
		encoding := int32(binary.BigEndian.Uint32(s.read(4)))
		if index == 0 {
			first = encoding
		}
	}
	return format[4], first
}

// A change of quality never overlaps a request that is outstanding: the server
// sends the reply to it in the old format, the client reads that in the old
// format, and only then sends the change, before its next request. The
// request after the change asks for the whole desktop.
func TestSetQualityWaitsForTheOutstandingReply(t *testing.T) {
	sink := &canvas{}
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 8, 2)
	client, err := Connect(clientSide, Config{})
	if err != nil {
		t.Fatal(err)
	}
	done := run(client, sink)
	s.readRequest() // outstanding

	if err := client.SetQuality(Pixel16, PreferEncoding(EncodingRaw)); err != nil {
		t.Fatal(err)
	}
	// The reply to the outstanding request is still 32 bits per pixel.
	s.write(update(rect(0, 0, 1, 1, EncodingRaw, 9, 8, 7, 0)))
	if bits, first := s.readQuality(); bits != 16 || first != EncodingRaw {
		t.Fatalf("change sent: %d bits, first encoding %d", bits, first)
	}
	if incremental, _, _ := s.readRequest(); incremental {
		t.Error("the request after a change of quality must ask for the whole desktop")
	}
	d := declaredBy(Pixel16.message())
	wire, shown := d.pack(10, 200, 90)
	s.write(update(rect(1, 0, 1, 1, EncodingRaw, wire...)))
	if incremental, _, _ := s.readRequest(); !incremental {
		t.Error("only the request right after the change is a full one")
	}
	s.finish()
	wait(t, done)
	if got := sink.at(0, 0); got != [3]byte{9, 8, 7} {
		t.Errorf("the reply made before the change = %v, want it read as 32 bits", got)
	}
	if got := sink.at(1, 0); got != shown {
		t.Errorf("the reply made after the change = %v, want %v", got, shown)
	}
}

// A hidden desktop has no request outstanding, so the change goes out at once,
// and the whole desktop is asked for when it is shown again.
func TestSetQualityOfAHiddenDesktopWaitsForNothing(t *testing.T) {
	sink := &canvas{}
	clientSide, serverSide := pair(t)
	s := &server{t: t, conn: serverSide}
	go s.handshake("008", 1, "", 8, 2)
	client, err := Connect(clientSide, Config{})
	if err != nil {
		t.Fatal(err)
	}
	client.SetActive(false)
	done := run(client, sink)
	s.readRequest()
	s.write(update(rect(0, 0, 1, 1, EncodingRaw, 1, 2, 3, 0))) // arrives while hidden: no request follows
	// Whether the client has read it is seen from outside by the next call: the
	// change is queued or sent at once, and either way arrives before any request.
	if err := client.SetQuality(Pixel8, nil); err != nil {
		t.Fatal(err)
	}
	if bits, first := s.readQuality(); bits != 8 || first != EncodingZRLE {
		t.Fatalf("change sent: %d bits, first encoding %d", bits, first)
	}
	go client.SetActive(true) // writes the request: the pipe has no buffer
	if incremental, _, _ := s.readRequest(); incremental {
		t.Error("showing the desktop after a change of quality must ask for all of it")
	}
	s.finish()
	wait(t, done)
}
