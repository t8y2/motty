// Package pointer decodes RDP pointer (cursor) update PDUs and converts
// AND/XOR mask data into top-down RGBA pixel buffers, with the pixels that
// invert the screen kept apart: such a pixel has no color of its own.
//
// Supports fast-path update codes (0x5–0xC) and slow-path pointer message
// types (MS-RDPBCGR 2.2.9.1.1.4).
package pointer

import (
	"context"
	"encoding/binary"
	"log/slog"
)

// Fast-path pointer update codes (bits 0-3 of updateHeader).
const (
	UpdatePtrNull     byte = 0x05
	UpdatePtrDefault  byte = 0x06
	UpdatePtrPosition byte = 0x08
	UpdatePtrColor    byte = 0x09
	UpdatePtrCached   byte = 0x0A
	UpdatePtrNew      byte = 0x0B
	UpdatePtrLarge    byte = 0x0C
)

// Slow-path pointer message types (TS_POINTER_PDU messageType field).
const (
	MsgPtrPosition uint16 = 0x0001
	MsgPtrSystem   uint16 = 0x0003
	MsgPtrColor    uint16 = 0x0006
	MsgPtrNew      uint16 = 0x0007
	MsgPtrCached   uint16 = 0x0008
	MsgPtrLarge    uint16 = 0x0009
)

// System pointer types.
const (
	SystemPtrNull    uint32 = 0x00000000
	SystemPtrDefault uint32 = 0x00007F00
)

// PointerUpdate holds the decoded cursor shape after AND/XOR → RGBA conversion.
type PointerUpdate struct {
	CacheIndex uint16
	HotSpotX   uint16
	HotSpotY   uint16
	Width      uint16
	Height     uint16
	Data       []byte // top-down RGBA, Width*Height*4 bytes
	// Invert marks the pixels that show the inverse of what is behind them
	// instead of a color of their own: the remote's monochrome inverting
	// pointer. It is top-down, one entry per pixel, and nil when no pixel
	// inverts; Data's pixel there is transparent (all zero), because a color
	// would be a guess at the picture the remote means to invert.
	Invert []bool
}

// DecodeSystem decodes a TS_SYSTEMPOINTERATTRIBUTE (4 bytes → system pointer type).
func DecodeSystem(data []byte) (uint32, error) {
	if len(data) < 4 {
		return 0, errShort
	}
	return binary.LittleEndian.Uint32(data[:4]), nil
}

// DecodeCached decodes a TS_CACHEDPOINTERATTRIBUTE (2-byte cache index).
func DecodeCached(data []byte) (uint16, error) {
	if len(data) < 2 {
		return 0, errShort
	}
	return binary.LittleEndian.Uint16(data[:2]), nil
}

// DecodeColorPointer decodes a TS_COLORPOINTERATTRIBUTE (24bpp fixed).
// buf is a reusable RGBA buffer; the returned []byte is the (possibly grown) buf.
func DecodeColorPointer(log *slog.Logger, buf, data []byte) (PointerUpdate, []byte, error) {
	return decodeColorFields(log, buf, data, 24, false)
}

// DecodeNewPointer decodes a TS_POINTERATTRIBUTE: xorBpp(u16) prefix + color pointer fields.
func DecodeNewPointer(log *slog.Logger, buf, data []byte) (PointerUpdate, []byte, error) {
	if len(data) < 2 {
		return PointerUpdate{}, buf, errShort
	}
	xorBpp := int(binary.LittleEndian.Uint16(data[:2]))
	return decodeColorFields(log, buf, data[2:], xorBpp, false)
}

// DecodeLargePointer decodes a TS_LARGE_POINTER_ATTRIBUTE: xorBpp(u16) prefix + color fields with u32 lengths.
func DecodeLargePointer(log *slog.Logger, buf, data []byte) (PointerUpdate, []byte, error) {
	if len(data) < 2 {
		return PointerUpdate{}, buf, errShort
	}
	xorBpp := int(binary.LittleEndian.Uint16(data[:2]))
	return decodeColorFields(log, buf, data[2:], xorBpp, true)
}

// decodeColorFields parses the common color pointer wire format:
//
//	cacheIndex(u16) hotX(u16) hotY(u16) w(u16) h(u16) andLen(u16/u32) xorLen(u16/u32) xorMask[] andMask[]
func decodeColorFields(log *slog.Logger, buf, data []byte, xorBpp int, large bool) (PointerUpdate, []byte, error) {
	// Minimum header: 5 × u16 = 10, plus andLen/xorLen fields
	minHdr := 10
	if large {
		minHdr += 8 // 2 × u32
	} else {
		minHdr += 4 // 2 × u16
	}
	if len(data) < minHdr {
		return PointerUpdate{}, buf, errShort
	}

	pu := PointerUpdate{
		CacheIndex: binary.LittleEndian.Uint16(data[0:2]),
		HotSpotX:   binary.LittleEndian.Uint16(data[2:4]),
		HotSpotY:   binary.LittleEndian.Uint16(data[4:6]),
		Width:      binary.LittleEndian.Uint16(data[6:8]),
		Height:     binary.LittleEndian.Uint16(data[8:10]),
	}
	// Clamp hotspot within cursor bounding box
	if pu.Width > 0 && pu.HotSpotX > pu.Width-1 {
		pu.HotSpotX = pu.Width - 1
	}
	if pu.Height > 0 && pu.HotSpotY > pu.Height-1 {
		pu.HotSpotY = pu.Height - 1
	}

	off := 10
	var andLen, xorLen int
	if large {
		andLen = int(binary.LittleEndian.Uint32(data[off : off+4]))
		xorLen = int(binary.LittleEndian.Uint32(data[off+4 : off+8]))
		off += 8
	} else {
		andLen = int(binary.LittleEndian.Uint16(data[off : off+2]))
		xorLen = int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
		off += 4
	}

	if off+xorLen+andLen > len(data) {
		return PointerUpdate{}, buf, errShort
	}
	xorMask := data[off : off+xorLen]
	andMask := data[off+xorLen : off+xorLen+andLen]

	rgba, invert, buf := convertMasksToRGBA(buf, int(pu.Width), int(pu.Height), xorBpp, xorMask, andMask)
	pu.Data, pu.Invert = rgba, invert
	log.LogAttrs(context.Background(), slog.LevelDebug, "pointer decoded", slog.Int("width", int(pu.Width)), slog.Int("height", int(pu.Height)), slog.Int("hotX", int(pu.HotSpotX)), slog.Int("hotY", int(pu.HotSpotY)), slog.Int("cacheIdx", int(pu.CacheIndex)))
	return pu, buf, nil
}

// convertMasksToRGBA converts AND/XOR cursor masks to top-down RGBA and the
// pixels that invert the screen.
//
// AND mask: 1bpp, MSB-first, rows 2-byte aligned.
// XOR mask: variable bpp, rows 2-byte aligned.
// bpp>=8: bottom-up (flip Y). bpp==1: top-down (no flip).
//
// AND=0 → the XOR color, opaque (alpha 0xFF).
// AND=1 + XOR=black → transparent (alpha 0x00).
// AND=1 + XOR=non-black → an inverting pixel, marked in the returned mask and
// transparent in RGBA. Windows sends its inverting pointer schemes (the
// "Windows Inverted" cursors, whose body shows the inverse of whatever is
// behind it) this way, at 1bpp and at 24bpp alike: the AND mask keeps the
// screen and the XOR bit asks to invert it.
// 32bpp special: when the XOR mask carries an alpha channel it is the
// authority on transparency, with the AND mask able only to hide a pixel; a
// server that sends 32bpp with no alpha channel at all falls back to the masks.
func convertMasksToRGBA(buf []byte, w, h, bpp int, xorMask, andMask []byte) (rgba []byte, invert []bool, outBuf []byte) {
	needed := w * h * 4
	if cap(buf) < needed {
		buf = make([]byte, needed)
	}
	rgba = buf[:needed]

	// markInvert records a pixel that shows the inverse of the screen, making
	// the mask on the first one: most cursors have no such pixel at all.
	markInvert := func(index int) {
		if invert == nil {
			invert = make([]bool, w*h)
		}
		invert[index] = true
	}

	// xrdp sends an X cursor at 32bpp with per-pixel alpha and an all-zero AND
	// mask; reading alpha only where it is nonzero turns the transparent
	// border into an opaque black square. Any alpha at all means the channel
	// is present, so every zero is a transparent pixel. (Motty divergence:
	// upstream trusted alpha only where it was nonzero.)
	alphaData := false
	if bpp == 32 {
		for i := 3; i < len(xorMask); i += 4 {
			if xorMask[i] != 0 {
				alphaData = true
				break
			}
		}
	}

	// AND mask stride: 1bpp, rows 2-byte aligned
	andStride := ((w + 7) / 8)
	andStride = (andStride + 1) &^ 1

	// XOR mask stride: variable bpp, rows 2-byte aligned
	var xorStride int
	if bpp >= 8 {
		xorStride = w * (bpp / 8)
		xorStride = (xorStride + 1) &^ 1
	} else {
		// 1bpp XOR mask
		xorStride = ((w + 7) / 8)
		xorStride = (xorStride + 1) &^ 1
	}

	for y := 0; y < h; y++ {
		// AND mask is always top-down for the rows we read, but the
		// convention depends on bpp. For bpp>=8 the masks are bottom-up
		// (like DIB), for 1bpp they're top-down.
		var andRow, xorRow []byte
		if bpp >= 8 {
			// Bottom-up: row 0 of the mask = bottom row on screen
			srcY := h - 1 - y
			if srcY*andStride < len(andMask) {
				andRow = andMask[srcY*andStride:]
			}
			if srcY*xorStride < len(xorMask) {
				xorRow = xorMask[srcY*xorStride:]
			}
		} else {
			if y*andStride < len(andMask) {
				andRow = andMask[y*andStride:]
			}
			if y*xorStride < len(xorMask) {
				xorRow = xorMask[y*xorStride:]
			}
		}

		dstOff := y * w * 4
		for x := 0; x < w; x++ {
			di := dstOff + x*4
			// Read AND bit
			andBit := byte(1) // default transparent if andMask too short
			if andRow != nil && x/8 < len(andRow) {
				andBit = (andRow[x/8] >> (7 - uint(x%8))) & 1
			}

			var r, g, b, a byte
			switch {
			case bpp == 32 && alphaData && xorRow != nil && x*4+3 < len(xorRow):
				b = xorRow[x*4]
				g = xorRow[x*4+1]
				r = xorRow[x*4+2]
				// GNOME omits the AND mask for alpha cursors.
				if len(andMask) == 0 || andBit == 0 {
					a = xorRow[x*4+3] // per-pixel alpha
				} else {
					a = 0 // transparent
				}
			case bpp == 32 && xorRow != nil && x*4+3 < len(xorRow):
				// 32bpp data without an alpha channel: the masks decide, as a
				// 24bpp cursor's do.
				b = xorRow[x*4]
				g = xorRow[x*4+1]
				r = xorRow[x*4+2]
				switch {
				case andBit == 0:
					a = 0xFF
				case r == 0 && g == 0 && b == 0:
					a = 0
				default:
					// An inverting pixel carries no color of its own.
					r, g, b, a = 0, 0, 0, 0
					markInvert(y*w + x)
				}
			case bpp == 24 && xorRow != nil && x*3+2 < len(xorRow):
				b = xorRow[x*3]
				g = xorRow[x*3+1]
				r = xorRow[x*3+2]
				switch {
				case andBit == 0:
					a = 0xFF
				case r == 0 && g == 0 && b == 0:
					a = 0
				default:
					// An inverting pixel carries no color of its own.
					r, g, b, a = 0, 0, 0, 0
					markInvert(y*w + x)
				}
			case bpp == 16 && xorRow != nil && x*2+1 < len(xorRow):
				pixel := uint16(xorRow[x*2]) | uint16(xorRow[x*2+1])<<8
				if andBit == 0 {
					r5 := byte((pixel >> 11) & 0x1F)
					g6 := byte((pixel >> 5) & 0x3F)
					b5 := byte(pixel & 0x1F)
					r = (r5 << 3) | (r5 >> 2)
					g = (g6 << 2) | (g6 >> 4)
					b = (b5 << 3) | (b5 >> 2)
					a = 0xFF
				} else if pixel == 0 {
					a = 0
				} else {
					// An inverting pixel carries no color of its own.
					r, g, b, a = 0, 0, 0, 0
					markInvert(y*w + x)
				}
			case bpp == 1:
				// Monochrome: XOR is 1bpp
				xorBit := byte(0)
				if xorRow != nil && x/8 < len(xorRow) {
					xorBit = (xorRow[x/8] >> (7 - uint(x%8))) & 1
				}
				if andBit == 0 {
					if xorBit == 0 {
						r, g, b, a = 0, 0, 0, 0xFF // black opaque
					} else {
						r, g, b, a = 0xFF, 0xFF, 0xFF, 0xFF // white opaque
					}
				} else {
					if xorBit == 0 {
						a = 0 // transparent
					} else {
						// An inverting pixel carries no color of its own.
						r, g, b, a = 0, 0, 0, 0
						markInvert(y*w + x)
					}
				}
			default:
				// Unknown/missing data: transparent
				a = 0
			}

			rgba[di] = r
			rgba[di+1] = g
			rgba[di+2] = b
			rgba[di+3] = a
		}
	}

	return rgba, invert, buf
}

type shortErr string

func (e shortErr) Error() string { return string(e) }

var errShort = shortErr("pointer data too short")
