// Package pdu implements RDP Share Control PDU framing (MS-RDPBCGR section 2.2.8.1).
package pdu

import (
	"context"
	"encoding/binary"
	"log/slog"

	"motty/internal/rdpcore/util"
)

// Share Control PDU types (low 4 bits of pduType field)
const (
	TypeDemandActive  uint16 = 0x0011 // type=1, version=0x0010
	TypeConfirmActive uint16 = 0x0013 // type=3, version=0x0010
	TypeDeactivateAll uint16 = 0x0016 // type=6, version=0x0010
	TypeData          uint16 = 0x0017 // type=7, version=0x0010
)

// Share Data PDU pduType2 values (inner PDU type within TypeData)
const (
	PDUType2Update              uint8 = 2
	PDUType2Control             uint8 = 20
	PDUType2Pointer             uint8 = 27
	PDUType2Input               uint8 = 28
	PDUType2Synchronize         uint8 = 31
	PDUType2FontList            uint8 = 39
	PDUType2FontMap             uint8 = 40
	PDUType2RefreshRect         uint8 = 33
	PDUType2SuppressOutput      uint8 = 35
	PDUType2SetErrorInfo        uint8 = 47
	PDUType2SaveSessionInfo     uint8 = 38 // 0x26
	PDUType2AutoReconnectStatus uint8 = 50 // 0x32
	PDUType2FrameAcknowledge    uint8 = 56 // 0x38
)

// ErrorInfoName returns a human-readable name for an error info code.
func ErrorInfoName(code uint32) string {
	switch code {
	case 0x00000000:
		return "ERRINFO_NONE"
	case 0x00000001:
		return "ERRINFO_RPC_INITIATED_DISCONNECT"
	case 0x00000002:
		return "ERRINFO_RPC_INITIATED_LOGOFF"
	case 0x00000003:
		return "ERRINFO_IDLE_TIMEOUT"
	case 0x00000004:
		return "ERRINFO_LOGON_TIMER_EXPIRED"
	case 0x00000005:
		return "ERRINFO_DISCONNECTED_BY_OTHER_CONNECTION"
	case 0x00000006:
		return "ERRINFO_OUT_OF_MEMORY"
	case 0x00000007:
		return "ERRINFO_SERVER_DENIED_CONNECTION"
	case 0x00000009:
		return "ERRINFO_SERVER_INSUFFICIENT_PRIVILEGES"
	case 0x0000000A:
		return "ERRINFO_SERVER_FRESH_CREDENTIALS_REQUIRED"
	case 0x0000000B:
		return "ERRINFO_RPC_INITIATED_DISCONNECT_BY_USER"
	case 0x0000000C:
		return "ERRINFO_LOGOFF_BY_USER"
	case 0x0000000F:
		return "ERRINFO_CLOSE_STACK_ON_DRIVER_NOT_READY"
	case 0x00000010:
		return "ERRINFO_SERVER_DWM_CRASH"
	case 0x00000011:
		return "ERRINFO_CLOSE_STACK_ON_DRIVER_FAILURE"
	case 0x00000012:
		return "ERRINFO_CLOSE_STACK_ON_DRIVER_IFACE_FAILURE"
	case 0x000010C9:
		return "ERRINFO_UNKNOWNPDUTYPE2"
	case 0x000010CA:
		return "ERRINFO_UNKNOWNPDUTYPE"
	case 0x000010CB:
		return "ERRINFO_DATAPDUSEQUENCE"
	case 0x000010E5:
		return "ERRINFO_CONFIRMACTIVEPDUTOOSHORT"
	case 0x0000112C:
		return "ERRINFO_BAD_FRAME_ACK_DATA"
	case 0x00001133:
		return "ERRINFO_VCDECODINGERROR"
	case 0x00001191:
		return "ERRINFO_UPDATESESSIONKEYFAILED"
	case 0x00001192:
		return "ERRINFO_DECRYPTFAILED"
	case 0x00001193:
		return "ERRINFO_ENCRYPTFAILED"
	default:
		return "ERRINFO_UNKNOWN"
	}
}

// Stream priority
const StreamLow uint8 = 1

// Update types (updateType field in TS_UPDATE_PDU)
const (
	UpdateOrders      uint16 = 0x0000
	UpdateBitmap      uint16 = 0x0001
	UpdatePalette     uint16 = 0x0002
	UpdateSynchronize uint16 = 0x0003
)

// TS_BITMAP_DATA flags
const (
	BitmapCompression      uint16 = 0x0001
	NoBitmapCompressionHdr uint16 = 0x0400
)

// Slow-path input event types
const (
	InputSynchronize uint16 = 0x0000
	InputScancode    uint16 = 0x0004
	InputUnicode     uint16 = 0x0005
	InputMouse       uint16 = 0x8001
	InputMouseX      uint16 = 0x8002
)

// Keyboard flags
const (
	KbdFlagsExtended uint16 = 0x0100
	// KbdFlagsDown marks a press of a key the server already has down: the
	// client's auto-repeat. A server does not repeat a held key by itself, so
	// every repeat carries this flag (MS-RDPBCGR 2.2.8.1.1.3.1.1.1).
	KbdFlagsDown    uint16 = 0x4000
	KbdFlagsRelease uint16 = 0x8000
)

// Mouse pointer flags (TS_POINTER_EVENT, InputMouse 0x8001)
const (
	PtrFlagsWheel         uint16 = 0x0200 // Vertical wheel rotation
	PtrFlagsHWheel        uint16 = 0x0400 // Horizontal wheel rotation
	PtrFlagsWheelNegative uint16 = 0x0100 // Negative rotation (down/left)
	PtrFlagsMove          uint16 = 0x0800
	PtrFlagsButton1       uint16 = 0x1000
	PtrFlagsButton2       uint16 = 0x2000
	PtrFlagsButton3       uint16 = 0x4000
	PtrFlagsDown          uint16 = 0x8000
)

// Extended mouse pointer flags (TS_POINTERX_EVENT, InputMouseX 0x8002)
// Only for X1/X2 (back/forward) buttons — NOT wheel.
const (
	PtrXFlagsButton1 uint16 = 0x0001 // X1 button (back)
	PtrXFlagsButton2 uint16 = 0x0002 // X2 button (forward)
	PtrXFlagsDown    uint16 = 0x8000 // Button press (without = release)
)

// Control PDU actions
const (
	ControlRequestControl uint16 = 0x0001
	ControlGrantedControl uint16 = 0x0002
	ControlCooperate      uint16 = 0x0004
)

// ShareControlHeader is the 6-byte header present at the start of every Share Control PDU.
type ShareControlHeader struct {
	TotalLength uint16 // total PDU size including this header
	PDUType     uint16 // low 4 bits = type, bits 4-15 = version (0x0010)
	PDUSource   uint16 // sender's MCS channel ID
}

// DemandActive represents the server's Demand Active PDU payload
// (after the Share Control Header).
type DemandActive struct {
	ShareID             uint32
	SourceDescriptorLen uint16
	CombinedCapsLen     uint16
	SourceDescriptor    []byte
	NumberCapabilities  uint16
	Pad2Octets          uint16
	CapabilitySets      []byte // raw concatenated capability sets
	SessionID           uint32
}

// ConfirmActive represents the client's Confirm Active PDU.
type ConfirmActive struct {
	ShareID             uint32
	SourceDescriptorLen uint16
	CombinedCapsLen     uint16
	SourceDescriptor    []byte
	NumberCapabilities  uint16
	Pad2Octets          uint16
	CapabilitySets      []byte // raw concatenated capability sets
}

// DecodeShareControlHeader parses a 6-byte Share Control Header from data.
// Returns the header and remaining data after it.
func DecodeShareControlHeader(log *slog.Logger, data []byte) (ShareControlHeader, []byte, error) {
	if len(data) < 6 {
		return ShareControlHeader{}, nil, errShortHeader
	}
	hdr := ShareControlHeader{
		TotalLength: binary.LittleEndian.Uint16(data[0:2]),
		PDUType:     binary.LittleEndian.Uint16(data[2:4]),
		PDUSource:   binary.LittleEndian.Uint16(data[4:6]),
	}
	log.LogAttrs(context.Background(), slog.LevelDebug, "share control header", util.Hex4("pduType", hdr.PDUType), slog.Int("totalLen", int(hdr.TotalLength)), slog.Int("pduSource", int(hdr.PDUSource)))
	return hdr, data[6:], nil
}

// DecodeDemandActive parses a Demand Active PDU from data that starts
// immediately after the Share Control Header.
func DecodeDemandActive(log *slog.Logger, data []byte) (*DemandActive, error) {
	// Minimum: shareID(4) + sourceDescLen(2) + combinedCapsLen(2) + sourceDesc(>=0)
	//          + numCaps(2) + pad(2) + sessionID(4) = 16 bytes minimum with 0-len sourceDesc
	if len(data) < 12 {
		return nil, errShortDemand
	}

	da := &DemandActive{}
	off := 0

	da.ShareID = binary.LittleEndian.Uint32(data[off : off+4])
	off += 4
	da.SourceDescriptorLen = binary.LittleEndian.Uint16(data[off : off+2])
	off += 2
	da.CombinedCapsLen = binary.LittleEndian.Uint16(data[off : off+2])
	off += 2

	sdLen := int(da.SourceDescriptorLen)
	if off+sdLen > len(data) {
		return nil, errShortDemand
	}
	da.SourceDescriptor = data[off : off+sdLen]
	off += sdLen

	// numberCapabilities(2) + pad(2)
	if off+4 > len(data) {
		return nil, errShortDemand
	}
	da.NumberCapabilities = binary.LittleEndian.Uint16(data[off : off+2])
	off += 2
	da.Pad2Octets = binary.LittleEndian.Uint16(data[off : off+2])
	off += 2

	capsLen := int(da.CombinedCapsLen)
	if off+capsLen > len(data) {
		// Use remaining data as capability sets
		capsLen = len(data) - off
	}

	if capsLen > 0 {
		da.CapabilitySets = data[off : off+capsLen]
		off += capsLen
	}

	// SessionID (4 bytes) — may not be present (optional per spec)
	if off+4 <= len(data) {
		da.SessionID = binary.LittleEndian.Uint32(data[off : off+4])
	}

	log.LogAttrs(context.Background(), slog.LevelDebug, "demand active", util.Hex8("shareID", da.ShareID), slog.Int("numCaps", int(da.NumberCapabilities)), slog.Int("capsLen", int(da.CombinedCapsLen)))
	return da, nil
}

// EncodeConfirmActive builds a complete Confirm Active PDU including
// the Share Control Header. Single allocation.
func EncodeConfirmActive(ca *ConfirmActive, pduSource uint16) []byte {
	sdLen := len(ca.SourceDescriptor)
	capsLen := len(ca.CapabilitySets)

	// Share Control Header (6) + shareID(4) + originatorId(2) + sourceDescLen(2)
	// + combinedCapsLen(2) + sourceDesc + numCaps(2) + pad(2) + capsSets
	totalLen := 6 + 4 + 2 + 2 + 2 + sdLen + 2 + 2 + capsLen

	buf := make([]byte, totalLen)
	off := 0

	// Share Control Header
	binary.LittleEndian.PutUint16(buf[off:], uint16(totalLen))
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], TypeConfirmActive)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], pduSource)
	off += 2

	// Confirm Active body (MS-RDPBCGR 2.2.1.13.2.1)
	binary.LittleEndian.PutUint32(buf[off:], ca.ShareID)
	off += 4
	binary.LittleEndian.PutUint16(buf[off:], 0x03EA) // originatorId: server channel ID
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], uint16(sdLen))
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], uint16(4+capsLen)) // numCaps(2) + pad(2) + capsSets
	off += 2
	copy(buf[off:], ca.SourceDescriptor)
	off += sdLen
	binary.LittleEndian.PutUint16(buf[off:], ca.NumberCapabilities)
	off += 2
	binary.LittleEndian.PutUint16(buf[off:], ca.Pad2Octets)
	off += 2
	copy(buf[off:], ca.CapabilitySets)

	return buf
}

// ShareDataHeader is the 12-byte header within a TypeData Share Control PDU.
type ShareDataHeader struct {
	ShareID            uint32
	StreamID           uint8
	UncompressedLength uint16
	PDUType2           uint8
	CompressedType     uint8  // compression type + flags (byte 9)
	CompressedLength   uint16 // compressed payload length (bytes 10-11)
}

// EncodeShareDataPDU builds a complete Share Control + Share Data PDU in a
// single allocation: Share Control Header (6) + Share Data Header (12) + payload.
func EncodeShareDataPDU(shareID uint32, pduType2 uint8, pduSource uint16, payload []byte) []byte {
	payloadLen := len(payload)
	totalLen := 18 + payloadLen
	buf := make([]byte, totalLen)

	// Share Control Header (6 bytes)
	binary.LittleEndian.PutUint16(buf[0:2], uint16(totalLen))
	binary.LittleEndian.PutUint16(buf[2:4], TypeData)
	binary.LittleEndian.PutUint16(buf[4:6], pduSource)

	// Share Data Header (12 bytes)
	binary.LittleEndian.PutUint32(buf[6:10], shareID)
	buf[10] = 0         // pad1
	buf[11] = StreamLow // streamID
	binary.LittleEndian.PutUint16(buf[12:14], uint16(payloadLen))
	buf[14] = pduType2                           // pduType2
	buf[15] = 0                                  // compressedType
	binary.LittleEndian.PutUint16(buf[16:18], 0) // compressedLength

	copy(buf[18:], payload)
	return buf
}

// DecodeShareDataHeader parses a 12-byte Share Data Header from data that
// starts immediately after the Share Control Header. Returns the header and
// the remaining payload.
func DecodeShareDataHeader(log *slog.Logger, data []byte) (ShareDataHeader, []byte, error) {
	if len(data) < 12 {
		return ShareDataHeader{}, nil, errShortShareData
	}
	hdr := ShareDataHeader{
		ShareID:            binary.LittleEndian.Uint32(data[0:4]),
		StreamID:           data[5],
		UncompressedLength: binary.LittleEndian.Uint16(data[6:8]),
		PDUType2:           data[8],
		CompressedType:     data[9],
		CompressedLength:   binary.LittleEndian.Uint16(data[10:12]),
	}
	log.LogAttrs(context.Background(), slog.LevelDebug, "share data header", util.Hex2("pduType2", hdr.PDUType2), util.Hex8("shareID", hdr.ShareID))
	return hdr, data[12:], nil
}

// EncodeSynchronize builds a 4-byte Synchronize PDU payload.
func EncodeSynchronize(targetUser uint16) []byte {
	buf := make([]byte, 4)
	binary.LittleEndian.PutUint16(buf[0:2], 1) // messageType = SYNCMSGTYPE_SYNC
	binary.LittleEndian.PutUint16(buf[2:4], targetUser)
	return buf
}

// EncodeControl builds an 8-byte Control PDU payload.
func EncodeControl(action uint16) []byte {
	buf := make([]byte, 8)
	binary.LittleEndian.PutUint16(buf[0:2], action)
	// grantID(u16)=0, controlID(u32)=0 — already zero
	return buf
}

// EncodeFontList builds an 8-byte Font List PDU payload.
func EncodeFontList() []byte {
	buf := make([]byte, 8)
	// numberFonts=0, totalNumFonts=0 — already zero
	binary.LittleEndian.PutUint16(buf[4:6], 0x0003) // listFlags
	binary.LittleEndian.PutUint16(buf[6:8], 0x0032) // entrySize
	return buf
}

// BitmapData is one rectangle from a Bitmap Update PDU (TS_BITMAP_DATA).
type BitmapData struct {
	DestLeft     uint16
	DestTop      uint16
	DestRight    uint16
	DestBottom   uint16
	Width        uint16
	Height       uint16
	BitsPerPixel uint16
	Flags        uint16
	Data         []byte // raw bitmap data (may be RLE-compressed)
}

// DecodeBitmapUpdateData parses a slow-path Bitmap Update PDU payload:
// updateType(u16) + numberRectangles(u16) + array of TS_BITMAP_DATA.
// Data slices reference the input buffer (no copy).
func DecodeBitmapUpdateData(data []byte) ([]BitmapData, error) {
	if len(data) < 4 {
		return nil, errShortBitmapUpdate
	}
	// updateType at [0:2] (caller already knows it's UpdateBitmap)
	return decodeBitmapRects(data, 2)
}

// DecodeFastPathBitmapUpdate parses a fast-path bitmap update payload:
// updateType(u16) + numberRectangles(u16) + array of TS_BITMAP_DATA.
// The fast-path bitmapUpdateData is a TS_UPDATE_BITMAP_DATA (same as slow-path).
// Data slices reference the input buffer (no copy).
func DecodeFastPathBitmapUpdate(data []byte) ([]BitmapData, error) {
	if len(data) < 4 {
		return nil, errShortBitmapUpdate
	}
	return decodeBitmapRects(data, 2)
}

// decodeBitmapRects parses numberRectangles(u16) at startOff, then the
// TS_BITMAP_DATA array starting at startOff+2. Shared by slow-path and
// fast-path decoders.
func decodeBitmapRects(data []byte, startOff int) ([]BitmapData, error) {
	if startOff+2 > len(data) {
		return nil, errShortBitmapUpdate
	}
	numRects := int(binary.LittleEndian.Uint16(data[startOff : startOff+2]))
	off := startOff + 2

	// Stack-backed array avoids heap allocation for typical updates (1-8 rects).
	var stack [8]BitmapData
	var rects []BitmapData
	if numRects <= len(stack) {
		rects = stack[:numRects]
	} else {
		rects = make([]BitmapData, numRects)
	}
	for i := range rects {
		// Each TS_BITMAP_DATA: 6 u16 fields (12 bytes) + flags(u16) + bitmapLength(u16) + bitmapDataStream
		if off+14 > len(data) {
			return nil, errShortBitmapUpdate
		}
		rects[i].DestLeft = binary.LittleEndian.Uint16(data[off : off+2])
		rects[i].DestTop = binary.LittleEndian.Uint16(data[off+2 : off+4])
		rects[i].DestRight = binary.LittleEndian.Uint16(data[off+4 : off+6])
		rects[i].DestBottom = binary.LittleEndian.Uint16(data[off+6 : off+8])
		rects[i].Width = binary.LittleEndian.Uint16(data[off+8 : off+10])
		rects[i].Height = binary.LittleEndian.Uint16(data[off+10 : off+12])
		rects[i].BitsPerPixel = binary.LittleEndian.Uint16(data[off+12 : off+14])

		if off+18 > len(data) {
			return nil, errShortBitmapUpdate
		}
		rects[i].Flags = binary.LittleEndian.Uint16(data[off+14 : off+16])
		bitmapLen := int(binary.LittleEndian.Uint16(data[off+16 : off+18]))
		off += 18

		if off+bitmapLen > len(data) {
			return nil, errShortBitmapUpdate
		}
		rects[i].Data = data[off : off+bitmapLen]
		off += bitmapLen
	}
	return rects, nil
}

// Input PDU sizes.
const (
	InputPDUHeaderSize = 4  // numEvents(u16) + pad(u16)
	ScancodeEventSize  = 12 // eventTime(u32) + messageType(u16) + flags(u16) + keyCode(u16) + pad(u16)
	UnicodeEventSize   = 12 // eventTime(u32) + messageType(u16) + flags(u16) + unicodeCode(u16) + pad(u16)
	MouseEventSize     = 12 // eventTime(u32) + messageType(u16) + flags(u16) + xPos(u16) + yPos(u16)
	MouseXEventSize    = 12 // eventTime(u32) + messageType(u16) + flags(u16) + xPos(u16) + yPos(u16)
)

// AppendInputPDUHeader appends a 4-byte Input PDU header to dst.
func AppendInputPDUHeader(dst []byte, numEvents uint16) []byte {
	var buf [InputPDUHeaderSize]byte
	binary.LittleEndian.PutUint16(buf[0:2], numEvents)
	return append(dst, buf[:]...)
}

// AppendScancodeEvent appends a 12-byte scancode input event to dst.
func AppendScancodeEvent(dst []byte, scancode, flags uint16) []byte {
	var buf [ScancodeEventSize]byte
	binary.LittleEndian.PutUint16(buf[4:6], InputScancode)
	binary.LittleEndian.PutUint16(buf[6:8], flags)
	binary.LittleEndian.PutUint16(buf[8:10], scancode)
	return append(dst, buf[:]...)
}

// AppendUnicodeEvent appends a 12-byte Unicode keyboard input event to dst.
// MS-RDPBCGR 2.2.8.1.1.3.1.1.1 - TS_UNICODE_KEYBOARD_EVENT.
func AppendUnicodeEvent(dst []byte, unicodeCode, flags uint16) []byte {
	var buf [UnicodeEventSize]byte
	binary.LittleEndian.PutUint16(buf[4:6], InputUnicode)
	binary.LittleEndian.PutUint16(buf[6:8], flags)
	binary.LittleEndian.PutUint16(buf[8:10], unicodeCode)
	return append(dst, buf[:]...)
}

// AppendMouseEvent appends a 12-byte mouse input event to dst.
func AppendMouseEvent(dst []byte, flags, x, y uint16) []byte {
	var buf [MouseEventSize]byte
	binary.LittleEndian.PutUint16(buf[4:6], InputMouse)
	binary.LittleEndian.PutUint16(buf[6:8], flags)
	binary.LittleEndian.PutUint16(buf[8:10], x)
	binary.LittleEndian.PutUint16(buf[10:12], y)
	return append(dst, buf[:]...)
}

// EncodeInputPDU builds an Input PDU payload: numEvents(u16) + pad(u16) + events.
func EncodeInputPDU(events []byte, numEvents uint16) []byte {
	buf := make([]byte, 0, InputPDUHeaderSize+len(events))
	buf = AppendInputPDUHeader(buf, numEvents)
	return append(buf, events...)
}

// EncodeScancodeEvent builds a 12-byte TS_INPUT_EVENT for a keyboard scancode.
func EncodeScancodeEvent(scancode, flags uint16) []byte {
	return AppendScancodeEvent(make([]byte, 0, ScancodeEventSize), scancode, flags)
}

// EncodeMouseEvent builds a 12-byte TS_INPUT_EVENT for a mouse event.
func EncodeMouseEvent(flags, x, y uint16) []byte {
	return AppendMouseEvent(make([]byte, 0, MouseEventSize), flags, x, y)
}

// AppendMouseXEvent appends a 12-byte extended mouse input event to dst.
// Used for wheel scrolling and X1/X2 (back/forward) buttons.
func AppendMouseXEvent(dst []byte, flags, x, y uint16) []byte {
	var buf [MouseXEventSize]byte
	binary.LittleEndian.PutUint16(buf[4:6], InputMouseX)
	binary.LittleEndian.PutUint16(buf[6:8], flags)
	binary.LittleEndian.PutUint16(buf[8:10], x)
	binary.LittleEndian.PutUint16(buf[10:12], y)
	return append(dst, buf[:]...)
}

// EncodeMouseXEvent builds a 12-byte TS_INPUT_EVENT for an extended mouse event.
func EncodeMouseXEvent(flags, x, y uint16) []byte {
	return AppendMouseXEvent(make([]byte, 0, MouseXEventSize), flags, x, y)
}

// EncodeRefreshRect builds a Refresh Rect PDU payload (MS-RDPBCGR 2.2.11.2):
// numberOfAreas(1) + pad3Octets(3) + areasToRefresh(8 each).
func EncodeRefreshRect(left, top, right, bottom uint16) []byte {
	buf := make([]byte, 12) // 1 + 3 + 8
	buf[0] = 1              // numberOfAreas
	// buf[1..3] = pad3Octets (zero)
	binary.LittleEndian.PutUint16(buf[4:6], left)
	binary.LittleEndian.PutUint16(buf[6:8], top)
	binary.LittleEndian.PutUint16(buf[8:10], right)
	binary.LittleEndian.PutUint16(buf[10:12], bottom)
	return buf
}

// EncodeSuppressOutput builds a Suppress Output PDU payload (MS-RDPBCGR 2.2.11.3.1):
// allowDisplayUpdates(1) + pad3Octets(3) [+ desktopRect(8) if allow].
// When allow is true, the full desktop rect is included to request a complete repaint.
func EncodeSuppressOutput(allow bool, left, top, right, bottom uint16) []byte {
	if !allow {
		buf := make([]byte, 4)
		// buf[0] = 0 (SUPPRESS_DISPLAY_UPDATES)
		return buf
	}
	buf := make([]byte, 12)
	buf[0] = 1 // ALLOW_DISPLAY_UPDATES
	binary.LittleEndian.PutUint16(buf[4:6], left)
	binary.LittleEndian.PutUint16(buf[6:8], top)
	binary.LittleEndian.PutUint16(buf[8:10], right)
	binary.LittleEndian.PutUint16(buf[10:12], bottom)
	return buf
}

// Sentinel errors (unexported, tested via the public API).
var (
	errShortHeader       = shortErr("share control header too short")
	errShortDemand       = shortErr("demand active PDU too short")
	errShortShareData    = shortErr("share data header too short")
	errShortBitmapUpdate = shortErr("bitmap update data too short")
)

type shortErr string

func (e shortErr) Error() string { return string(e) }
