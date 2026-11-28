package rdp

import (
	"bytes"
	"encoding/binary"
	"io"
	"log/slog"
	"testing"

	"motty/internal/rdpcore/protocol/audin"
	"motty/internal/rdpcore/protocol/clearcodec"
	"motty/internal/rdpcore/protocol/egfx"
	"motty/internal/rdpcore/protocol/fastpath"
	"motty/internal/rdpcore/protocol/mppc"
	"motty/internal/rdpcore/protocol/nscodec"
	"motty/internal/rdpcore/protocol/rfx"
	"motty/internal/rdpcore/protocol/rle"
	"motty/internal/rdpcore/protocol/zgfx"
)

// The decoders read bytes from the server, which Motty does not trust: none of
// them may panic or allocate without bound, whatever it sends. These fuzz targets
// feed each of them the sizes and payloads of a hostile server; their seed
// corpus runs with the ordinary tests. Run longer with
//
//	go test ./internal/rdpcore -run '^$' -fuzz FuzzRLE -fuzztime 60s
//
// (and likewise for the others). A decoder that is handed a size is handed one
// the way the protocol hands it, 16 bits, before it has seen the payload.

var quiet = slog.New(slog.NewTextHandler(io.Discard, nil))

func sizes(a, b uint16) (int, int) { return int(a%512) + 1, int(b%512) + 1 }

func FuzzRLE(f *testing.F) {
	f.Add(uint16(64), uint16(64), uint8(16), []byte{0xF0, 0x01, 0x02, 0x03, 0x04})
	f.Add(uint16(8), uint16(8), uint8(32), []byte{0x00})
	f.Fuzz(func(t *testing.T, w, h uint16, bpp uint8, data []byte) {
		width, height := sizes(w, h)
		rle.Decompress(width, height, []int{8, 15, 16, 24, 32}[int(bpp)%5], data)
	})
}

func FuzzPlanar(f *testing.F) {
	f.Add(uint16(16), uint16(16), []byte{0x20, 0x00, 0x01, 0x02})
	f.Fuzz(func(t *testing.T, w, h uint16, data []byte) {
		width, height := sizes(w, h)
		rle.DecompressPlanar(nil, width, height, data)
	})
}

func FuzzNSCodec(f *testing.F) {
	f.Add(uint16(16), uint16(16), []byte{4, 0, 0, 0, 4, 0, 0, 0, 4, 0, 0, 0, 4, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, w, h uint16, data []byte) {
		width, height := sizes(w, h)
		nscodec.Decompress(nil, nil, width, height, data, quiet)
	})
}

func FuzzClearCodec(f *testing.F) {
	f.Add(uint16(16), uint16(16), []byte{0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, w, h uint16, data []byte) {
		width, height := sizes(w, h)
		clearcodec.New(quiet).Decompress(nil, width, height, data)
	})
}

func FuzzZGFX(f *testing.F) {
	f.Add([]byte{0xE0, 0x04, 0x01, 0x02, 0x03})
	f.Add([]byte{0xE1, 0x01, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		var d zgfx.Decompressor
		if out, err := d.Decompress(nil, data); err == nil && len(out) > 64<<20 {
			t.Fatalf("a %d byte message expanded to %d bytes", len(data), len(out))
		}
	})
}

func FuzzMPPC(f *testing.F) {
	f.Add([]byte{0x01, 0x02, 0x03}, uint8(0x21))
	f.Fuzz(func(t *testing.T, data []byte, kind uint8) {
		var d mppc.Decompressor
		d.Decompress(data, kind)
	})
}

func FuzzFastPath(f *testing.F) {
	f.Add([]byte{0x01, 0x04, 0x00, 0x00, 0x00, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		fastpath.DecodeUpdates(quiet, data)
	})
}

func FuzzRemoteFX(f *testing.F) {
	f.Add([]byte{0xC0, 0xCC, 0x0C, 0x00, 0x00, 0x00})
	f.Fuzz(func(t *testing.T, data []byte) {
		rfx.NewDecoder(quiet).Decode(data, 1)
	})
}

// FuzzGraphicsPipeline feeds the whole RDPGFX channel handler, which sets up
// surfaces from sizes the server chooses.
func FuzzGraphicsPipeline(f *testing.F) {
	create := make([]byte, 8+10)
	binary.LittleEndian.PutUint16(create[0:], 0x0009) // CREATE_SURFACE
	binary.LittleEndian.PutUint32(create[4:], uint32(len(create)))
	binary.LittleEndian.PutUint16(create[8:], 1)
	binary.LittleEndian.PutUint16(create[10:], 64)
	binary.LittleEndian.PutUint16(create[12:], 64)
	f.Add(append([]byte{0xE0, 0x04}, create...))
	f.Add(create)
	f.Fuzz(func(t *testing.T, data []byte) {
		handler := egfx.NewHandler(func([]byte) error { return nil }, quiet)
		handler.OnBitmap(func(uint16, int, int, int, int, []byte) {})
		handler.OnStridedBitmap(func(uint16, int, int, int, int, []byte, int) {})
		handler.ProcessPDU(data)
		handler.Close()
	})
}

// FuzzAudioInput feeds the MS-RDPEAI (AUDIO_INPUT) channel handler with
// untrusted server PDUs.
func FuzzAudioInput(f *testing.F) {
	formats := make([]byte, 1+8+18)
	formats[0] = 0x02 // MSG_SNDIN_FORMATS
	binary.LittleEndian.PutUint32(formats[1:5], 1)
	binary.LittleEndian.PutUint32(formats[5:9], uint32(len(formats)))
	binary.LittleEndian.PutUint16(formats[9:], 1)      // PCM
	binary.LittleEndian.PutUint16(formats[11:], 2)     // stereo
	binary.LittleEndian.PutUint32(formats[13:], 48000) // 48kHz
	binary.LittleEndian.PutUint32(formats[17:], 192000)
	binary.LittleEndian.PutUint16(formats[21:], 4)
	binary.LittleEndian.PutUint16(formats[23:], 16)
	open := make([]byte, 9)
	open[0] = 0x03 // MSG_SNDIN_OPEN
	binary.LittleEndian.PutUint32(open[1:5], 480)
	f.Add(formats, open)
	f.Add([]byte{0x01, 2, 0, 0, 0}, []byte{0x07, 0, 0, 0, 0})
	f.Fuzz(func(t *testing.T, pdu1, pdu2 []byte) {
		handler := audin.NewHandler(func([]byte) error { return nil }, func(uint32, uint16, uint32) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}, quiet)
		handler.ProcessPDU(pdu1)
		handler.ProcessPDU(pdu2)
		handler.Close()
	})
}
