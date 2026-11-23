package rfb

import (
	"bytes"
	"io"
	"testing"
)

// chunkReader hands the stream over at most n bytes at a time, as an inflater does
// when its input arrives in pieces.
type chunkReader struct {
	r io.Reader
	n int
}

func (c chunkReader) Read(p []byte) (int, error) {
	return c.r.Read(p[:min(len(p), c.n)])
}

// zrleTiles is one 64x64 tile of every ZRLE kind, back to back, in the given format:
// the raw tile is larger than the reader's buffer and its pixels do not divide it,
// and the palette and run tiles carry long runs.
func zrleTiles(format PixelFormat) []byte {
	size := format.compressedSize()
	pixel := func(n int) []byte {
		out := make([]byte, size)
		for i := range out {
			out[i] = byte(n*7 + i*13 + 1)
		}
		return out
	}
	var tiles []byte
	tiles = append(tiles, 0) // raw
	for n := range 64 * 64 {
		tiles = append(tiles, pixel(n)...)
	}
	tiles = append(tiles, 1) // solid
	tiles = append(tiles, pixel(200)...)
	tiles = append(tiles, 2) // packed palette of two, 1 bit per pixel
	tiles = append(tiles, pixel(3)...)
	tiles = append(tiles, pixel(4)...)
	for row := range 64 {
		tiles = append(tiles, bytes.Repeat([]byte{byte(0xa5 ^ row)}, 8)...)
	}
	tiles = append(tiles, 128) // plain RLE: one run of 4096
	tiles = append(tiles, pixel(9)...)
	tiles = append(tiles, bytes.Repeat([]byte{255}, 16)...)
	tiles = append(tiles, 15)
	tiles = append(tiles, 130) // palette RLE: runs of 2000 and 2096
	tiles = append(tiles, pixel(5)...)
	tiles = append(tiles, pixel(6)...)
	tiles = append(tiles, 0x80)
	tiles = append(tiles, bytes.Repeat([]byte{255}, 7)...)
	tiles = append(tiles, 214)
	tiles = append(tiles, 0x81)
	tiles = append(tiles, bytes.Repeat([]byte{255}, 8)...)
	tiles = append(tiles, 55)
	return tiles
}

func decodeZRLETiles(t *testing.T, format PixelFormat, source io.Reader) [][]byte {
	t.Helper()
	var state zrleState
	reader := zrleReader{r: source, format: format}
	var decoded [][]byte
	for range 5 {
		if err := state.tileDecode(&reader, 64, 64); err != nil {
			t.Fatalf("tile %d: %v", len(decoded), err)
		}
		decoded = append(decoded, bytes.Clone(state.tile[:64*64*4]))
	}
	return decoded
}

// However the inflated stream is cut into reads, every tile decodes to what it does
// when the stream arrives whole: a pixel, a palette entry or a run length that
// straddles the reader's buffer must not change the picture.
func TestZRLETilesDecodeTheSameWhenTheStreamArrivesInPieces(t *testing.T) {
	for _, format := range []PixelFormat{Pixel32, Pixel16, Pixel8} {
		tiles := zrleTiles(format)
		want := decodeZRLETiles(t, format, bytes.NewReader(tiles))
		for _, n := range []int{1, 2, 3, 5, 4093, 4095, 4097} {
			got := decodeZRLETiles(t, format, chunkReader{bytes.NewReader(tiles), n})
			for i := range want {
				if !bytes.Equal(got[i], want[i]) {
					t.Errorf("format %v, %d-byte reads: tile %d differs from the whole-stream decode", format, n, i)
				}
			}
		}
	}
}
