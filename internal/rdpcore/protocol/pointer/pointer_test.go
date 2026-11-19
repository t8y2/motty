package pointer

import (
	"encoding/binary"
	"log/slog"
	"testing"
)

func TestDecodeSystem(t *testing.T) {
	tests := []struct {
		name string
		data []byte
		want uint32
		err  bool
	}{
		{"null pointer", []byte{0, 0, 0, 0}, SystemPtrNull, false},
		{"default pointer", []byte{0x00, 0x7F, 0x00, 0x00}, SystemPtrDefault, false},
		{"too short", []byte{0, 0}, 0, true},
	}
	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got, err := DecodeSystem(tt.data)
			if tt.err && err == nil {
				t.Fatal("expected error")
			}
			if !tt.err && err != nil {
				t.Fatalf("unexpected error: %v", err)
			}
			if !tt.err && got != tt.want {
				t.Fatalf("got 0x%08X, want 0x%08X", got, tt.want)
			}
		})
	}
}

func TestDecodeCached(t *testing.T) {
	data := []byte{0x05, 0x00}
	idx, err := DecodeCached(data)
	if err != nil {
		t.Fatal(err)
	}
	if idx != 5 {
		t.Fatalf("got %d, want 5", idx)
	}

	_, err = DecodeCached([]byte{0x01})
	if err == nil {
		t.Fatal("expected error for short data")
	}
}

func TestDecodeColorPointer(t *testing.T) {
	// Build a 2x2 24bpp color pointer
	w, h := 2, 2

	// AND mask: 1bpp, 2-byte aligned rows. 2 pixels = 1 byte, padded to 2.
	// All AND=0 → all opaque
	andMask := []byte{0x00, 0x00, 0x00, 0x00} // 2 rows × 2 bytes

	// XOR mask: 24bpp, 2 pixels × 3 bytes = 6, padded to 6 (already even)
	// Bottom-up: row 0 in mask = bottom of image (y=1 on screen)
	xorMask := make([]byte, 2*6)
	// Row 0 (bottom): red, green
	xorMask[0], xorMask[1], xorMask[2] = 0, 0, 0xFF // pixel (0,1): B=0, G=0, R=255
	xorMask[3], xorMask[4], xorMask[5] = 0, 0xFF, 0 // pixel (1,1): B=0, G=255, R=0
	// Row 1 (top): blue, white
	xorMask[6], xorMask[7], xorMask[8] = 0xFF, 0, 0         // pixel (0,0): B=255, G=0, R=0
	xorMask[9], xorMask[10], xorMask[11] = 0xFF, 0xFF, 0xFF // pixel (1,0): white

	// Wire format: cacheIndex(2) hotX(2) hotY(2) w(2) h(2) andLen(2) xorLen(2) xorMask andMask
	data := make([]byte, 14+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[0:], 3)         // cacheIndex
	binary.LittleEndian.PutUint16(data[2:], 1)         // hotX
	binary.LittleEndian.PutUint16(data[4:], 0)         // hotY
	binary.LittleEndian.PutUint16(data[6:], uint16(w)) // width
	binary.LittleEndian.PutUint16(data[8:], uint16(h)) // height
	binary.LittleEndian.PutUint16(data[10:], uint16(len(andMask)))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(xorMask)))
	copy(data[14:], xorMask)
	copy(data[14+len(xorMask):], andMask)

	pu, _, err := DecodeColorPointer(slog.Default(), nil, data)
	if err != nil {
		t.Fatalf("DecodeColorPointer error: %v", err)
	}

	if pu.CacheIndex != 3 || pu.HotSpotX != 1 || pu.HotSpotY != 0 {
		t.Fatalf("header mismatch: idx=%d hot=(%d,%d)", pu.CacheIndex, pu.HotSpotX, pu.HotSpotY)
	}
	if pu.Width != 2 || pu.Height != 2 {
		t.Fatalf("size mismatch: %dx%d", pu.Width, pu.Height)
	}
	if len(pu.Data) != w*h*4 {
		t.Fatalf("data length: got %d, want %d", len(pu.Data), w*h*4)
	}

	// Check top-left pixel (0,0): should be blue (from row 1 of bottom-up data)
	// Row 1 of XOR = xorMask[6:12], pixel 0 = B=255, G=0, R=0 → RGBA: R=0, G=0, B=255, A=255
	r, g, b, a := pu.Data[0], pu.Data[1], pu.Data[2], pu.Data[3]
	if r != 0 || g != 0 || b != 0xFF || a != 0xFF {
		t.Fatalf("pixel (0,0): got RGBA(%d,%d,%d,%d), want (0,0,255,255)", r, g, b, a)
	}

	// Check top-right pixel (1,0): white
	r, g, b, a = pu.Data[4], pu.Data[5], pu.Data[6], pu.Data[7]
	if r != 0xFF || g != 0xFF || b != 0xFF || a != 0xFF {
		t.Fatalf("pixel (1,0): got RGBA(%d,%d,%d,%d), want (255,255,255,255)", r, g, b, a)
	}
}

func TestDecodeNewPointer(t *testing.T) {
	// 2x1 32bpp with per-pixel alpha
	w, h := 2, 1

	// AND mask: 1 row, all zeros (opaque)
	andMask := []byte{0x00, 0x00} // 2-byte aligned

	// XOR mask: 32bpp bottom-up, 1 row of 2 pixels × 4 bytes = 8
	xorMask := []byte{
		0x00, 0x00, 0xFF, 0xC0, // pixel 0: B=0, G=0, R=255, A=192
		0xFF, 0x00, 0x00, 0x80, // pixel 1: B=255, G=0, R=0, A=128
	}

	// Wire: xorBpp(2) + cacheIndex(2) hotX(2) hotY(2) w(2) h(2) andLen(2) xorLen(2) xor and
	data := make([]byte, 2+14+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[0:], 32) // xorBpp
	binary.LittleEndian.PutUint16(data[2:], 0)  // cacheIndex
	binary.LittleEndian.PutUint16(data[4:], 0)  // hotX
	binary.LittleEndian.PutUint16(data[6:], 0)  // hotY
	binary.LittleEndian.PutUint16(data[8:], uint16(w))
	binary.LittleEndian.PutUint16(data[10:], uint16(h))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(andMask)))
	binary.LittleEndian.PutUint16(data[14:], uint16(len(xorMask)))
	copy(data[16:], xorMask)
	copy(data[16+len(xorMask):], andMask)

	pu, _, err := DecodeNewPointer(slog.Default(), nil, data)
	if err != nil {
		t.Fatalf("DecodeNewPointer error: %v", err)
	}

	if len(pu.Data) != w*h*4 {
		t.Fatalf("data length: got %d, want %d", len(pu.Data), w*h*4)
	}

	// Pixel 0: R=255, G=0, B=0, A=192 (per-pixel alpha from XOR, AND=0)
	r, g, b, a := pu.Data[0], pu.Data[1], pu.Data[2], pu.Data[3]
	if r != 0xFF || g != 0 || b != 0 || a != 0xC0 {
		t.Fatalf("pixel 0: got RGBA(%d,%d,%d,%d), want (255,0,0,192)", r, g, b, a)
	}

	// Pixel 1: R=0, G=0, B=255, A=128
	r, g, b, a = pu.Data[4], pu.Data[5], pu.Data[6], pu.Data[7]
	if r != 0 || g != 0 || b != 0xFF || a != 0x80 {
		t.Fatalf("pixel 1: got RGBA(%d,%d,%d,%d), want (0,0,255,128)", r, g, b, a)
	}
}

// newPointerData builds a TS_POINTERATTRIBUTE for a w×h cursor at xorBpp.
func newPointerData(w, h, xorBpp int, xorMask, andMask []byte) []byte {
	data := make([]byte, 2+14+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[0:], uint16(xorBpp))
	binary.LittleEndian.PutUint16(data[8:], uint16(w))
	binary.LittleEndian.PutUint16(data[10:], uint16(h))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(andMask)))
	binary.LittleEndian.PutUint16(data[14:], uint16(len(xorMask)))
	copy(data[16:], xorMask)
	copy(data[16+len(xorMask):], andMask)
	return data
}

// An xrdp X cursor arrives at 32bpp with per-pixel alpha and an all-zero AND
// mask. An alpha of zero means transparent; treating it as opaque black paints
// a black square around the pointer.
func TestNewPointerAlphaZeroIsTransparent(t *testing.T) {
	xorMask := []byte{
		0x00, 0x00, 0x00, 0x00, // black, alpha 0 → transparent
		0xFF, 0xFF, 0xFF, 0xFF, // white, alpha 255 → opaque
	}
	pu, _, err := DecodeNewPointer(slog.Default(), nil, newPointerData(2, 1, 32, xorMask, []byte{0x00, 0x00}))
	if err != nil {
		t.Fatal(err)
	}
	if a := pu.Data[3]; a != 0 {
		t.Fatalf("pixel 0 alpha: got %d, want 0 (transparent)", a)
	}
	if r, g, b, a := pu.Data[4], pu.Data[5], pu.Data[6], pu.Data[7]; r != 0xFF || g != 0xFF || b != 0xFF || a != 0xFF {
		t.Fatalf("pixel 1: got RGBA(%d,%d,%d,%d), want (255,255,255,255)", r, g, b, a)
	}
}

// A 32bpp cursor whose XOR mask has no alpha channel still gets its
// transparency from the AND mask.
func TestNewPointerWithoutAlphaUsesMasks(t *testing.T) {
	xorMask := []byte{
		0x00, 0x00, 0xFF, 0x00, // red, alpha 0
		0x00, 0x00, 0x00, 0x00, // black, alpha 0
	}
	andMask := []byte{0x40, 0x00} // pixel 0 AND=0, pixel 1 AND=1
	pu, _, err := DecodeNewPointer(slog.Default(), nil, newPointerData(2, 1, 32, xorMask, andMask))
	if err != nil {
		t.Fatal(err)
	}
	if r, g, b, a := pu.Data[0], pu.Data[1], pu.Data[2], pu.Data[3]; r != 0xFF || g != 0 || b != 0 || a != 0xFF {
		t.Fatalf("pixel 0: got RGBA(%d,%d,%d,%d), want (255,0,0,255)", r, g, b, a)
	}
	if a := pu.Data[7]; a != 0 {
		t.Fatalf("pixel 1 alpha: got %d, want 0 (transparent)", a)
	}
}

func TestTransparency(t *testing.T) {
	// 2x1 24bpp: AND=1+XOR=black → transparent, AND=1+XOR=white → XOR-invert
	// (rendered as opaque black with white halo painted into transparent neighbors).
	w, h := 2, 1

	andMask := []byte{0xC0, 0x00} // bits: 11000000 → both pixels AND=1

	// XOR: 24bpp, row = 2 pixels × 3 bytes = 6
	xorMask := []byte{
		0x00, 0x00, 0x00, // pixel 0: black → transparent
		0xFF, 0xFF, 0xFF, // pixel 1: white → XOR-invert → opaque black
	}

	data := make([]byte, 14+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[0:], 0) // cacheIndex
	binary.LittleEndian.PutUint16(data[2:], 0) // hotX
	binary.LittleEndian.PutUint16(data[4:], 0) // hotY
	binary.LittleEndian.PutUint16(data[6:], uint16(w))
	binary.LittleEndian.PutUint16(data[8:], uint16(h))
	binary.LittleEndian.PutUint16(data[10:], uint16(len(andMask)))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(xorMask)))
	copy(data[14:], xorMask)
	copy(data[14+len(xorMask):], andMask)

	pu, _, err := DecodeColorPointer(slog.Default(), nil, data)
	if err != nil {
		t.Fatal(err)
	}

	// Pixel 0: originally transparent, but adjacent to the XOR-invert pixel →
	// painted opaque white by the halo pass.
	r, g, b, a := pu.Data[0], pu.Data[1], pu.Data[2], pu.Data[3]
	if r != 0xFF || g != 0xFF || b != 0xFF || a != 0xFF {
		t.Fatalf("pixel 0 (halo): got RGBA(%d,%d,%d,%d), want (255,255,255,255)", r, g, b, a)
	}
	// Pixel 1: AND=1, XOR=white → opaque black (XOR-invert)
	r, g, b, a = pu.Data[4], pu.Data[5], pu.Data[6], pu.Data[7]
	if r != 0 || g != 0 || b != 0 || a != 0xFF {
		t.Fatalf("pixel 1 (XOR-invert): got RGBA(%d,%d,%d,%d), want (0,0,0,255)", r, g, b, a)
	}
}

func TestDecodeLargePointer(t *testing.T) {
	// 1x1 32bpp large pointer (u32 lengths)
	w, h := 1, 1
	andMask := []byte{0x00, 0x00}
	xorMask := []byte{0x00, 0xFF, 0x00, 0xFF} // B=0, G=255, R=0, A=255

	// Wire: xorBpp(2) + cacheIndex(2) hotX(2) hotY(2) w(2) h(2) andLen(4) xorLen(4) xor and
	data := make([]byte, 2+10+8+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[0:], 32) // xorBpp
	binary.LittleEndian.PutUint16(data[2:], 7)  // cacheIndex
	binary.LittleEndian.PutUint16(data[4:], 0)
	binary.LittleEndian.PutUint16(data[6:], 0)
	binary.LittleEndian.PutUint16(data[8:], uint16(w))
	binary.LittleEndian.PutUint16(data[10:], uint16(h))
	binary.LittleEndian.PutUint32(data[12:], uint32(len(andMask)))
	binary.LittleEndian.PutUint32(data[16:], uint32(len(xorMask)))
	copy(data[20:], xorMask)
	copy(data[20+len(xorMask):], andMask)

	pu, _, err := DecodeLargePointer(slog.Default(), nil, data)
	if err != nil {
		t.Fatal(err)
	}
	if pu.CacheIndex != 7 {
		t.Fatalf("cache index: got %d, want 7", pu.CacheIndex)
	}
	// Pixel: R=0, G=255, B=0, A=255
	r, g, b, a := pu.Data[0], pu.Data[1], pu.Data[2], pu.Data[3]
	if r != 0 || g != 0xFF || b != 0 || a != 0xFF {
		t.Fatalf("pixel: got RGBA(%d,%d,%d,%d), want (0,255,0,255)", r, g, b, a)
	}
}

func TestMonochrome(t *testing.T) {
	// 8x1 monochrome (1bpp) cursor
	w, h := 8, 1

	// AND: 10101010 → alternating transparent/opaque
	andMask := []byte{0xAA, 0x00}
	// XOR: 11111111 → all white
	xorMask := []byte{0xFF, 0x00}

	data := make([]byte, 14+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[6:], uint16(w))
	binary.LittleEndian.PutUint16(data[8:], uint16(h))
	binary.LittleEndian.PutUint16(data[10:], uint16(len(andMask)))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(xorMask)))
	copy(data[14:], xorMask)
	copy(data[14+len(xorMask):], andMask)

	// Force 1bpp by using DecodeNewPointer with xorBpp=1
	newData := make([]byte, 2+len(data))
	binary.LittleEndian.PutUint16(newData[0:], 1) // xorBpp=1
	copy(newData[2:], data)

	pu, _, err := DecodeNewPointer(slog.Default(), nil, newData)
	if err != nil {
		t.Fatal(err)
	}

	if len(pu.Data) != w*h*4 {
		t.Fatalf("data len: got %d, want %d", len(pu.Data), w*h*4)
	}

	// AND=1, XOR=1: XOR-invert → opaque black for bits 0,2,4,6
	// AND=0, XOR=1: opaque white for bits 1,3,5,7
	// (No transparent pixels here, so the halo pass has nothing to paint.)
	for x := range w {
		r, g, b, a := pu.Data[x*4], pu.Data[x*4+1], pu.Data[x*4+2], pu.Data[x*4+3]
		if x%2 == 0 {
			// AND=1 (bit pattern 10101010), XOR=1 → opaque black
			if r != 0 || g != 0 || b != 0 || a != 0xFF {
				t.Fatalf("pixel %d (XOR-invert): got RGBA(%d,%d,%d,%d), want (0,0,0,255)", x, r, g, b, a)
			}
		} else {
			// AND=0, XOR=1 → opaque white
			if r != 0xFF || g != 0xFF || b != 0xFF || a != 0xFF {
				t.Fatalf("pixel %d: got RGBA(%d,%d,%d,%d), want (255,255,255,255)", x, r, g, b, a)
			}
		}
	}
}

func TestBufferReuse(t *testing.T) {
	// Verify that the buf parameter is reused across calls
	w, h := 1, 1
	andMask := []byte{0x00, 0x00}
	xorMask := []byte{0x00, 0x00, 0xFF} // B=0, G=0, R=255

	data := make([]byte, 14+len(xorMask)+len(andMask))
	binary.LittleEndian.PutUint16(data[6:], uint16(w))
	binary.LittleEndian.PutUint16(data[8:], uint16(h))
	binary.LittleEndian.PutUint16(data[10:], uint16(len(andMask)))
	binary.LittleEndian.PutUint16(data[12:], uint16(len(xorMask)))
	copy(data[14:], xorMask)
	copy(data[14+len(xorMask):], andMask)

	_, buf1, _ := DecodeColorPointer(slog.Default(), nil, data)
	_, buf2, _ := DecodeColorPointer(slog.Default(), buf1, data)
	if &buf1[0] != &buf2[0] {
		t.Fatal("buffer was not reused")
	}
}

// GNOME sends 32bpp cursors with an alpha channel and no AND mask.
func TestGNOMEAlphaPointerWithoutANDMask(t *testing.T) {
	xor := []byte{30, 20, 10, 255, 0, 0, 0, 0, 60, 50, 40, 128}
	rgba, _ := convertMasksToRGBA(nil, 3, 1, 32, xor, nil)
	want := []byte{10, 20, 30, 255, 0, 0, 0, 0, 40, 50, 60, 128}
	for i := range want {
		if rgba[i] != want[i] {
			t.Fatalf("byte %d: got %d, want %d", i, rgba[i], want[i])
		}
	}
}
