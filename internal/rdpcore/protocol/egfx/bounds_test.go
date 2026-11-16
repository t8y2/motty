package egfx

import (
	"encoding/binary"
	"log/slog"
	"testing"
)

// The server picks every size and rectangle in these PDUs, and MS-RDPEGFX
// carries them in 16 bits: a few bytes of CreateSurface used to be enough to
// allocate gigabytes. These tests hold the client to its surface and cache
// budgets, and to the
// surface each rectangle claims to be in.

func newTestHandler() *Handler {
	return NewHandler(func([]byte) error { return nil }, slog.New(slog.DiscardHandler))
}

func createSurfacePDU(id, width, height uint16) []byte {
	data := make([]byte, 7)
	binary.LittleEndian.PutUint16(data[0:2], id)
	binary.LittleEndian.PutUint16(data[2:4], width)
	binary.LittleEndian.PutUint16(data[4:6], height)
	data[6] = PixelFormatXRGB8888
	return data
}

func wireToSurfacePDU(id, codec, left, top, right, bottom uint16, bitmap []byte) []byte {
	data := make([]byte, 17+len(bitmap))
	binary.LittleEndian.PutUint16(data[0:2], id)
	binary.LittleEndian.PutUint16(data[2:4], codec)
	data[4] = PixelFormatXRGB8888
	binary.LittleEndian.PutUint16(data[5:7], left)
	binary.LittleEndian.PutUint16(data[7:9], top)
	binary.LittleEndian.PutUint16(data[9:11], right)
	binary.LittleEndian.PutUint16(data[11:13], bottom)
	binary.LittleEndian.PutUint32(data[13:17], uint32(len(bitmap)))
	copy(data[17:], bitmap)
	return data
}

func surfaceToCachePDU(surfaceID, slot, left, top, right, bottom uint16) []byte {
	data := make([]byte, 20)
	binary.LittleEndian.PutUint16(data[0:2], surfaceID)
	binary.LittleEndian.PutUint16(data[10:12], slot)
	binary.LittleEndian.PutUint16(data[12:14], left)
	binary.LittleEndian.PutUint16(data[14:16], top)
	binary.LittleEndian.PutUint16(data[16:18], right)
	binary.LittleEndian.PutUint16(data[18:20], bottom)
	return data
}

func TestCreateSurfaceRefusesSurfaceOverBudget(t *testing.T) {
	h := newTestHandler()

	// 12000x12000 is 549 MiB, more than the client grants, and small enough
	// that a regression would not exhaust the machine running the test.
	h.handleCreateSurface(createSurfacePDU(1, 12000, 12000))
	if len(h.surfaces) != 0 {
		t.Fatalf("created %d surfaces, want none", len(h.surfaces))
	}
	if h.surfaceBytes != 0 {
		t.Fatalf("surfaceBytes = %d, want 0", h.surfaceBytes)
	}

	// A surface a display can actually show is still created.
	h.handleCreateSurface(createSurfacePDU(2, 1920, 1080))
	if h.surfaces[2] == nil {
		t.Fatal("a 1920x1080 surface was not created")
	}
}

func TestSurfaceBudgetFollowsReplaceAndDelete(t *testing.T) {
	h := newTestHandler()

	h.handleCreateSurface(createSurfacePDU(1, 100, 100))
	if want := 100 * 100 * 4; h.surfaceBytes != want {
		t.Fatalf("surfaceBytes = %d, want %d", h.surfaceBytes, want)
	}

	// Replacing a surface must hand back the old buffer instead of counting both.
	h.handleCreateSurface(createSurfacePDU(1, 200, 50))
	if want := 200 * 50 * 4; h.surfaceBytes != want {
		t.Fatalf("after replace surfaceBytes = %d, want %d", h.surfaceBytes, want)
	}

	h.handleDeleteSurface([]byte{1, 0})
	if h.surfaceBytes != 0 {
		t.Fatalf("after delete surfaceBytes = %d, want 0", h.surfaceBytes)
	}
}

func TestResetGraphicsReturnsSurfaceBudget(t *testing.T) {
	h := newTestHandler()
	h.handleCreateSurface(createSurfacePDU(1, 64, 64))

	reset := make([]byte, 12)
	binary.LittleEndian.PutUint32(reset[0:4], 800)
	binary.LittleEndian.PutUint32(reset[4:8], 600)
	h.handleResetGraphics(reset)

	if len(h.surfaces) != 0 || h.surfaceBytes != 0 || h.cacheBytes != 0 {
		t.Fatalf("after reset: %d surfaces, %d surface bytes, %d cache bytes, want none", len(h.surfaces), h.surfaceBytes, h.cacheBytes)
	}
}

func TestBitmapCacheEvictsToStayWithinItsCount(t *testing.T) {
	h := newTestHandler()
	h.handleCreateSurface(createSurfacePDU(1, 1, 1))

	for slot := 0; slot <= maxCacheEntries; slot++ {
		h.handleSurfaceToCache(surfaceToCachePDU(1, uint16(slot), 0, 0, 1, 1))
	}

	if len(h.cache) != maxCacheEntries {
		t.Fatalf("cache holds %d entries, want %d", len(h.cache), maxCacheEntries)
	}
	// The 1x1 surface is counted against its own budget, not the cache's.
	if want := 4; h.surfaceBytes != want {
		t.Fatalf("surfaceBytes = %d, want %d", h.surfaceBytes, want)
	}
	if want := maxCacheEntries * 4; h.cacheBytes != want {
		t.Fatalf("cacheBytes = %d, want %d", h.cacheBytes, want)
	}

	// Evicting a slot hands its buffer back to the cache budget.
	h.handleEvictCacheEntry([]byte{0, 0})
	if want := (maxCacheEntries - 1) * 4; h.cacheBytes != want {
		t.Fatalf("after evict cacheBytes = %d, want %d", h.cacheBytes, want)
	}
}

// TestBitmapCacheEvictsToStayWithinItsBytes holds the cache to the 100 MB a
// client that did not ask for a small cache is allowed (MS-RDPEGFX 3.3.1.4).
func TestBitmapCacheEvictsToStayWithinItsBytes(t *testing.T) {
	h := newTestHandler()
	h.handleCreateSurface(createSurfacePDU(1, 1000, 1000))
	entry := 1000 * 1000 * 4 // one full-surface copy

	for slot := 0; slot < 40; slot++ {
		h.handleSurfaceToCache(surfaceToCachePDU(1, uint16(slot), 0, 0, 1000, 1000))
		if h.cacheBytes > maxCacheBytes {
			t.Fatalf("cache holds %d bytes, budget is %d", h.cacheBytes, maxCacheBytes)
		}
	}
	if want := maxCacheBytes / entry; len(h.cache) != want {
		t.Fatalf("cache holds %d entries, want %d", len(h.cache), want)
	}
	if h.surfaceBytes != entry {
		t.Fatalf("surfaceBytes = %d, want %d", h.surfaceBytes, entry)
	}
}

func TestWireToSurfaceRefusesRectangleOutsideSurface(t *testing.T) {
	h := newTestHandler()
	h.handleCreateSurface(createSurfacePDU(1, 100, 100))

	// A 200x200 rectangle on a 100x100 surface: the old code sized a buffer
	// from it before checking anything.
	h.handleWireToSurface1(wireToSurfacePDU(1, CodecUncompressed, 0, 0, 200, 200, make([]byte, 200*200*4)))
	if len(h.codecBuf) != 0 {
		t.Fatalf("sized a %d byte buffer from a rectangle outside the surface", len(h.codecBuf))
	}
	for _, px := range h.surfaces[1].Data {
		if px != 0xFF {
			t.Fatal("a rectangle outside the surface painted it")
		}
	}
}

func TestWireToSurfaceRefusesShortUncompressedBitmap(t *testing.T) {
	h := newTestHandler()
	h.handleCreateSurface(createSurfacePDU(1, 10, 10))

	// A 10x10 rectangle needs 400 bytes; four used to be read past the end.
	h.handleWireToSurface1(wireToSurfacePDU(1, CodecUncompressed, 0, 0, 10, 10, make([]byte, 4)))
	for _, px := range h.surfaces[1].Data {
		if px != 0xFF {
			t.Fatal("a short bitmap painted the surface")
		}
	}
}
