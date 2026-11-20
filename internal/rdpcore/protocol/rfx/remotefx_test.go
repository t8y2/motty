package rfx

import (
	"log/slog"
	"os"
	"testing"
)

// remoteFXFrame decodes a CAVIDEO message from testdata into a picture.
func remoteFXFrame(t *testing.T, name string, width, height int) []byte {
	t.Helper()
	data, err := os.ReadFile("testdata/" + name)
	if err != nil {
		t.Fatal(err)
	}
	pixels := make([]byte, width*height*4)
	decoder := NewRemoteFXDecoder(slog.New(slog.DiscardHandler))
	area := 0
	err = decoder.Decode(data, func(x, y, w, h int, tile []byte, stride int) {
		if x < 0 || y < 0 || x+w > width || y+h > height {
			t.Fatalf("emitted rectangle %d,%d %dx%d outside the %dx%d picture", x, y, w, h, width, height)
		}
		area += w * h
		for row := 0; row < h; row++ {
			at := ((y+row)*width + x) * 4
			copy(pixels[at:at+w*4], tile[row*stride:row*stride+w*4])
		}
	})
	if err != nil {
		t.Fatalf("decode: %v", err)
	}
	if area != width*height {
		t.Fatalf("decoded %d pixels, want %d", area, width*height)
	}
	return pixels
}

// TestRemoteFXDecodeFrame decodes a frame Windows really sent over the RDP 8.0
// graphics pipeline, the one Windows 8 and Server 2012 offer. The payload was
// captured from a live server (a desktop with Explorer open) and holds the
// exact bytes the server sent; the assertions only guard that the codec
// produced the picture instead of the black hole an unsupported codec left.
func TestRemoteFXDecodeFrame(t *testing.T) {
	const width, height = 1280, 720
	pixels := remoteFXFrame(t, "remotefx-1280x720.bin", width, height)

	seen := map[[3]byte]struct{}{}
	for i := 0; i+3 < len(pixels); i += 4 {
		seen[[3]byte{pixels[i], pixels[i+1], pixels[i+2]}] = struct{}{}
	}
	if len(seen) < 1000 {
		t.Fatalf("the decoded frame has %d colours, want at least 1000: it is blank or wrongly decoded", len(seen))
	}
	// The desktop wallpaper is a blue picture and the taskbar at the bottom is
	// light. Both come from tiles, so they prove the colour conversion and the
	// tile placement, not just the message parser.
	at := (500*width + 100) * 4
	if pixels[at+2] <= pixels[at]+20 {
		t.Fatalf("wallpaper pixel is r=%d g=%d b=%d, want a blue pixel", pixels[at], pixels[at+1], pixels[at+2])
	}
	light := 0
	for x := 0; x < width; x++ {
		row := ((height - 20) * width) * 4
		if pixels[row+x*4] > 200 && pixels[row+x*4+1] > 200 && pixels[row+x*4+2] > 200 {
			light++
		}
	}
	if light < width/2 {
		t.Fatalf("the taskbar row holds %d light pixels of %d, want the taskbar", light, width)
	}
}

func TestRemoteFXEmptyTilesetIsFine(t *testing.T) {
	// Windows Server 2012 (not R2) can send an empty tile set.
	message := []byte{
		// SYNC: magic 0xCACCACCA, version 1.0.
		0xC0, 0xCC, 12, 0, 0, 0, 0xCA, 0xAC, 0xCC, 0xCA, 0x00, 0x01,
		// CONTEXT: codec 1, channel 0xFF, ctxId 0, 64x64 tiles, RLGR1.
		0xC3, 0xCC, 13, 0, 0, 0, 0x01, 0xFF, 0x00, 0x40, 0x00, 0x2A, 0x22,
		// FRAME_BEGIN.
		0xC4, 0xCC, 14, 0, 0, 0, 0x01, 0x00, 0x01, 0x00, 0x00, 0x00, 0x01, 0x00,
		// REGION: one 64x64 rectangle, one tile set.
		0xC6, 0xCC, 23, 0, 0, 0,
		0x01, 0x00, // codec, channel
		0x01,       // regionFlags
		0x01, 0x00, // numRects
		0, 0, 0, 0, // x, y
		64, 0, 64, 0, // width, height
		0xC1, 0xCA, // regionType
		0x01, 0x00, // numTileSets
		// TILESET with one quantization table and no tiles.
		0xC7, 0xCC, 27, 0, 0, 0,
		0x01, 0x00, // codec, channel
		0xC2, 0xCA, // subtype
		0x00, 0x00, // idx
		0x51, 0x44, // properties
		0x01,       // numQuant
		0x40,       // tileSize
		0x00, 0x00, // numTiles
		0x00, 0x00, 0x00, 0x00, // tilesDataSize
		0x66, 0x66, 0x77, 0x88, 0x98, // one quantization table
		// FRAME_END.
		0xC5, 0xCC, 8, 0, 0, 0, 0x01, 0x00,
	}

	decoder := NewRemoteFXDecoder(slog.New(slog.DiscardHandler))
	called := false
	if err := decoder.Decode(message, func(int, int, int, int, []byte, int) { called = true }); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if called {
		t.Fatal("an empty tile set emitted a rectangle")
	}
}

func TestRemoteFXTruncatedMessage(t *testing.T) {
	data, err := os.ReadFile("testdata/remotefx-1280x720.bin")
	if err != nil {
		t.Fatal(err)
	}
	decoder := NewRemoteFXDecoder(slog.New(slog.DiscardHandler))
	for _, size := range []int{3, 10, 40, len(data) / 2, len(data) - 1} {
		_ = decoder.Decode(data[:size], func(int, int, int, int, []byte, int) {})
	}
}

// TestRemoteFXRegionWithoutRects covers the spec's shortcut for a region that
// names no rectangle: the changed area is the whole channel (MS-RDPRFX
// 2.2.2.3.3), whose size came from the CHANNELS block.
func TestRemoteFXRegionWithoutRects(t *testing.T) {
	data, err := os.ReadFile("testdata/remotefx-1280x720.bin")
	if err != nil {
		t.Fatal(err)
	}
	// Keep the header blocks and the frame beginning, replace the region with
	// one that names no rectangle, and keep the tile set.
	region := []byte{
		0xC6, 0xCC, 15, 0, 0, 0, // WBT_REGION, 6-byte header and 9-byte body
		0x01, 0x00, // codec, channel
		0x01,       // regionFlags
		0x00, 0x00, // numRects
		0xC1, 0xCA, // regionType
		0x01, 0x00, // numTileSets
	}
	message := append(append([]byte{}, data[:84]...), region...)
	message = append(message, data[84:]...)

	area := 0
	decoder := NewRemoteFXDecoder(slog.New(slog.DiscardHandler))
	if err := decoder.Decode(message, func(x, y, w, h int, tile []byte, stride int) {
		if x < 0 || y < 0 || x+w > 1280 || y+h > 720 {
			t.Fatalf("emitted rectangle %d,%d %dx%d outside the channel", x, y, w, h)
		}
		area += w * h
	}); err != nil {
		t.Fatalf("decode: %v", err)
	}
	if area != 1280*720 {
		t.Fatalf("decoded %d pixels of the region-less frame, want %d", area, 1280*720)
	}
}

func BenchmarkRemoteFXDecode(b *testing.B) {
	data, err := os.ReadFile("testdata/remotefx-1280x720.bin")
	if err != nil {
		b.Fatal(err)
	}
	decoder := NewRemoteFXDecoder(slog.New(slog.DiscardHandler))
	b.ReportAllocs()
	b.SetBytes(int64(len(data)))
	for i := 0; i < b.N; i++ {
		if err := decoder.Decode(data, func(int, int, int, int, []byte, int) {}); err != nil {
			b.Fatal(err)
		}
	}
}
