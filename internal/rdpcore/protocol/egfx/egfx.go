// Package egfx implements the RDPGFX Graphics Pipeline Extension (MS-RDPEGFX).
//
// RDPGFX provides modern display codec support over a dynamic virtual channel,
// including ClearCodec, Planar, and Uncompressed pixel formats. All server data
// arrives wrapped in ZGFX compression.
package egfx

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"time"

	"motty/internal/rdpcore/protocol/rfx"
	"motty/internal/rdpcore/protocol/rle"
	"motty/internal/rdpcore/protocol/zgfx"
	sloghex "motty/internal/rdpcore/util"
)

// levelTrace is a custom log level below Debug for very verbose diagnostics.
const levelTrace = slog.LevelDebug - 4

// ChannelName is the dynamic virtual channel name for RDPGFX.
const ChannelName = "Microsoft::Windows::RDS::Graphics"

// PDU command IDs (MS-RDPEGFX 2.2.2).
const (
	CmdWireToSurface1           = 0x0001
	CmdWireToSurface2           = 0x0002
	CmdDeleteEncodingCtx        = 0x0003
	CmdSolidFill                = 0x0004
	CmdSurfaceToSurface         = 0x0005
	CmdSurfaceToCache           = 0x0006
	CmdCacheToSurface           = 0x0007
	CmdEvictCacheEntry          = 0x0008
	CmdCreateSurface            = 0x0009
	CmdDeleteSurface            = 0x000A
	CmdStartFrame               = 0x000B
	CmdEndFrame                 = 0x000C
	CmdFrameAcknowledge         = 0x000D
	CmdResetGraphics            = 0x000E
	CmdMapSurfaceToOutput       = 0x000F
	CmdCapsAdvertise            = 0x0012
	CmdCapsConfirm              = 0x0013
	CmdMapSurfaceToWindow       = 0x0015
	CmdQoEFrameAcknowledge      = 0x0016 // MS-RDPEGFX 2.2.3.2 (v10+)
	CmdMapSurfaceToScaledOutput = 0x0017
)

// Codec IDs (MS-RDPEGFX 2.2.2.1).
const (
	CodecUncompressed = 0x0000
	CodecRemoteFX     = 0x0003 // CAVIDEO
	CodecClearCodec   = 0x0008
	CodecProgressive  = 0x0009 // CAPROGRESSIVE (RemoteFX Progressive, v8.1+)
	CodecPlanar       = 0x000A
	CodecAVC420       = 0x000B
	CodecAlpha        = 0x000C
	CodecAVC444       = 0x000E
	CodecAVC444v2     = 0x000F
)

// Pixel formats.
const (
	PixelFormatXRGB8888 = 0x20
	PixelFormatARGB8888 = 0x21
)

// Capability versions.
const (
	CapVersion8   = 0x00080004
	CapVersion81  = 0x00080105
	CapVersion10  = 0x000A0002
	CapVersion101 = 0x000A0100
	CapVersion102 = 0x000A0200
	CapVersion103 = 0x000A0301
	CapVersion104 = 0x000A0400
	CapVersion105 = 0x000A0502
	CapVersion106 = 0x000A0600
	CapVersion107 = 0x000A0701
)

// Capability flags.
const (
	FlagThinsClient   = 0x00000001
	FlagSmallCache    = 0x00000002
	FlagAVC420Enabled = 0x00000010 // v8.1: enable AVC420
	FlagAVCDisabled   = 0x00000020
	FlagAVCThinclient = 0x00000040 // v10.3+: thin client AVC
)

// ClearCodecDecoder is the interface for a ClearCodec decoder.
type ClearCodecDecoder interface {
	Decompress(dst []byte, width, height int, src []byte) ([]byte, error)
	ResetState() // reset sequence number on ResetGraphics (MS-RDPEGFX 3.1.8.1.1)
}

// H264Region describes a quality region for an H.264 frame (MS-RDPEGFX 2.2.4.4).
type H264Region struct {
	Left, Top, Right, Bottom uint16
	QPVal, QualityVal        byte
}

// H264Frame carries H.264 NAL data and metadata for decoding.
type H264Frame struct {
	SurfaceID                    uint16
	CodecMode                    byte // 0=AVC420, 1=AVC444-luma, 2=AVC444-chroma
	AVC444v2                     bool // true when codec is RDPGFX_CODECID_AVC444v2 (chroma packing differs)
	Left, Top, Right, Bottom     int  // destRect from WireToSurface1
	OutputOriginX, OutputOriginY int  // screen-space origin from MapSurfaceToOutput
	Regions                      []H264Region
	NALData                      []byte
}

// Surface represents an RDPGFX surface (top-down RGBA 32bpp).
type Surface struct {
	ID     uint16
	Width  uint16
	Height uint16
	Data   []byte // top-down RGBA 32bpp pixel buffer
}

// cacheEntry stores a cached bitmap for SurfaceToCache/CacheToSurface.
type cacheEntry struct {
	data   []byte // top-down RGBA pixels
	width  int
	height int
}

// maxPixelBytes bounds the pixel buffers the server can make the client hold.
//
// Every buffer here is sized from numbers the server chooses, and MS-RDPEGFX
// carries them in 16 bits: a 20-byte CreateSurface PDU can name a surface of
// 60471 by 12338 pixels, which is 2.8 GiB, and 256 surfaces fit in the count
// limit. The session frame buffer tops out at 7680x7680 (236 MiB, see
// maxSize in internal/desktop/rdp), and a surface may be larger than the output
// it is scaled into, so the budget admits a few of those and no more.
const maxPixelBytes = 512 << 20

// maxCacheEntries bounds how many bitmaps the cache holds: the server picks the
// slots, and each entry is a copy of a surface region.
const maxCacheEntries = 8192

// dirtyRect represents a single dirty rectangle on a surface.
type dirtyRect struct {
	surfID                   uint16
	left, top, right, bottom int
}

// Handler manages RDPGFX protocol state.
type Handler struct {
	log             *slog.Logger
	sendFn          func([]byte) error
	zgfx            zgfx.Decompressor
	clearcodec      ClearCodecDecoder
	surfaces        map[uint16]*Surface
	outputMap       map[uint16][2]int      // surfaceID → (screenX, screenY)
	cache           map[uint16]*cacheEntry // slot → cached bitmap
	pixelBytes      int                    // surfaces and cache entries, see maxPixelBytes
	framesDecoded   uint32
	curFrameID      uint32
	onBitmap        func(surfID uint16, x, y, w, h int, data []byte)
	onStridedBitmap func(surfID uint16, x, y, w, h int, data []byte, stride int) // strided surface data
	onResetGraphics func(w, h int)
	onBeginPaint    func()           // called before EndFrame flush loop
	onEndPaint      func()           // called after EndFrame flush loop
	onH264Frame     func(*H264Frame) // H.264 pass-through callback
	progressive     *rfx.Decoder     // RemoteFX Progressive codec decoder
	avcEnabled      bool             // advertise AVC support in CAPS_ADVERTISE

	// Negotiated cap version from CAPS_CONFIRM. Used to gate v10+ features
	// like RDPGFX_QOE_FRAME_ACKNOWLEDGE_PDU. Zero until caps are confirmed.
	negotiatedCapVersion uint32
	// Wall-clock millisecond timestamp captured at handleStartFrame. Used
	// as the timestamp field of the QoE ack and to compute timeDiffSE.
	frameStartMs int64

	// Frame batching: accumulate individual dirty rects during
	// StartFrame..EndFrame and emit each independently at EndFrame.
	// Per-rect (not bounding-box) tracking prevents inflation when
	// unrelated regions are updated in the same frame.
	inFrame         bool
	frameDirtyRects []dirtyRect
	frameH264       []*H264Frame // H.264 frames batched during StartFrame..EndFrame

	// Asynchronous frame ACK — decouples ACK sending from writeMu
	// contention so the receive goroutine isn't blocked waiting to write.
	ackCh chan []byte
	done  chan struct{} // closed by Close() to stop the ack goroutine

	// Reusable buffers
	decompBuf   []byte // ZGFX decompression output
	codecBuf    []byte // codec decompression output
	blitTmpBuf  []byte // temporary buffer for overlap-safe surface-to-surface blit
	callbackBuf []byte // reusable buffer for onBitmap callback data

}

// NewHandler creates an RDPGFX handler.
// sendFn is called to write data on the RDPGFX dynamic virtual channel.
func NewHandler(sendFn func([]byte) error, log *slog.Logger) *Handler {
	h := &Handler{
		log:         log,
		sendFn:      sendFn,
		surfaces:    make(map[uint16]*Surface),
		outputMap:   make(map[uint16][2]int),
		cache:       make(map[uint16]*cacheEntry),
		progressive: rfx.NewDecoder(log),
		ackCh:       make(chan []byte, 32),
		done:        make(chan struct{}),
	}
	// Send frame ACKs asynchronously — decoupled from writeMu so the
	// receive goroutine isn't blocked waiting for a write slot.
	go func() {
		for {
			select {
			case buf := <-h.ackCh:
				if err := h.sendFn(buf); err != nil {
					h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send frame ack", slog.Any("err", err))
					return
				}
			case <-h.done:
				return
			}
		}
	}()
	return h
}

// Close stops the background ACK goroutine and releases resources.
// Must be called when the handler is no longer needed (e.g. on disconnect).
func (h *Handler) Close() {
	select {
	case <-h.done:
	default:
		close(h.done)
	}
}

// fitsPixelBytes reports whether n more pixel bytes fit in the budget.
func (h *Handler) fitsPixelBytes(n int) bool {
	return n >= 0 && h.pixelBytes+n <= maxPixelBytes
}

// givePixelBytes returns a buffer to the budget when it is dropped.
func (h *Handler) givePixelBytes(n int) {
	h.pixelBytes -= n
	if h.pixelBytes < 0 {
		h.pixelBytes = 0
	}
}

// rectInside reports whether a rectangle named by a PDU lies within its
// surface. The client sizes buffers from the rectangle, so one the server
// placed outside its own surface must be refused rather than trusted.
func rectInside(s *Surface, left, top, right, bottom int) bool {
	return s != nil && left >= 0 && top >= 0 && left < right && top < bottom &&
		right <= int(s.Width) && bottom <= int(s.Height)
}

// SetClearCodecDecoder sets the ClearCodec decoder used for CodecClearCodec payloads.
func (h *Handler) SetClearCodecDecoder(d ClearCodecDecoder) {
	h.clearcodec = d
}

// OnBitmap sets the callback for bitmap updates.
// Data is top-down RGBA 32bpp for the given surface rectangle.
func (h *Handler) OnBitmap(fn func(surfID uint16, x, y, w, h int, data []byte)) {
	h.onBitmap = fn
}

// OnStridedBitmap sets the callback for strided bitmap updates.
// Data points directly into the surface buffer with the given stride (bytes per row).
// This avoids an intermediate copy compared to OnBitmap.
func (h *Handler) OnStridedBitmap(fn func(surfID uint16, x, y, w, h int, data []byte, stride int)) {
	h.onStridedBitmap = fn
}

// OnResetGraphics sets the callback for graphics reset (resolution change).
func (h *Handler) OnResetGraphics(fn func(w, h int)) {
	h.onResetGraphics = fn
}

// OnBeginPaint sets the callback invoked before the EndFrame flush loop.
// The display can use this to acquire a lock so all per-rect emissions
// within a single frame are applied atomically.
func (h *Handler) OnBeginPaint(fn func()) {
	h.onBeginPaint = fn
}

// OnEndPaint sets the callback invoked after the EndFrame flush loop.
func (h *Handler) OnEndPaint(fn func()) {
	h.onEndPaint = fn
}

// OnH264Frame sets the callback for H.264 pass-through frames.
// When set, AVC420/AVC444 data is forwarded as raw NAL units instead of decoded.
func (h *Handler) OnH264Frame(fn func(*H264Frame)) {
	h.onH264Frame = fn
}

// SetAVCEnabled controls whether AVC (H.264) codecs are advertised in CAPS_ADVERTISE.
func (h *Handler) SetAVCEnabled(v bool) {
	h.avcEnabled = v
}

// notifyBitmap records a display-dirty region. During a frame (between
// StartFrame and EndFrame), dirty rects are accumulated individually and
// flushed at EndFrame. Each rect is emitted independently, preventing
// bounding-box inflation when source and destination regions are far apart
// (MS-RDPEGFX 3.3.5 — per-rect invalidation). Outside of frames, the
// callback fires immediately so interactive updates are not delayed.
func (h *Handler) notifyBitmap(surfID uint16, x, y, w, hh int) {
	if h.onBitmap == nil && h.onStridedBitmap == nil {
		return
	}
	r := x + w
	b := y + hh
	if !h.inFrame {
		h.emitBitmapRect(surfID, x, y, r, b)
		return
	}
	h.frameDirtyRects = append(h.frameDirtyRects, dirtyRect{surfID, x, y, r, b})
}

// emitBitmapRect sends a rectangle of surface pixels to the display callback.
func (h *Handler) emitBitmapRect(surfID uint16, left, top, right, bottom int) {
	surf := h.surfaces[surfID]
	if surf == nil {
		return
	}
	// Clamp to surface bounds
	if right > int(surf.Width) {
		right = int(surf.Width)
	}
	if bottom > int(surf.Height) {
		bottom = int(surf.Height)
	}
	w := right - left
	hh := bottom - top
	if w <= 0 || hh <= 0 {
		return
	}
	origin := h.outputMap[surfID]
	dstStride := int(surf.Width) * 4

	// Strided path: pass surface data directly, no intermediate copy.
	if h.onStridedBitmap != nil {
		srcOff := top*dstStride + left*4
		h.onStridedBitmap(surfID, origin[0]+left, origin[1]+top, w, hh, surf.Data[srcOff:], dstStride)
	}

	// Legacy path: copy into contiguous buffer for callers that need it.
	if h.onBitmap != nil {
		rectSize := w * hh * 4
		if cap(h.callbackBuf) < rectSize {
			h.callbackBuf = make([]byte, rectSize)
		}
		h.callbackBuf = h.callbackBuf[:rectSize]
		for row := range hh {
			srcOff := (top+row)*dstStride + left*4
			copy(h.callbackBuf[row*w*4:(row+1)*w*4], surf.Data[srcOff:srcOff+w*4])
		}
		h.onBitmap(surfID, origin[0]+left, origin[1]+top, w, hh, h.callbackBuf)
	}
}

// Capability flags (MS-RDPEGFX 2.2.3).
const (
	CapsFlagAVCDisabled = 0x00000020 // No H.264 support (v10.0+)
)

// SendCapsAdvertise sends a CAPS_ADVERTISE PDU advertising supported RDPGFX versions.
// Advertises v10.7 through v8.0 (highest first) so the server picks the best it supports.
// When avcEnabled is true, v10.x sets omit AVC_DISABLED and v8.1 sets AVC420_ENABLED.
func (h *Handler) SendCapsAdvertise() error {
	type capEntry struct {
		version uint32
		flags   uint32
	}

	v10flags := uint32(CapsFlagAVCDisabled)
	v81flags := uint32(0)
	if h.avcEnabled {
		v10flags = 0 // omit AVC_DISABLED → server may send H.264
		v81flags = FlagAVC420Enabled
	}

	caps := []capEntry{
		{CapVersion107, v10flags},
		{CapVersion106, v10flags},
		{CapVersion105, v10flags},
		{CapVersion104, v10flags},
		{CapVersion103, v10flags},
		{CapVersion102, v10flags},
		{CapVersion10, v10flags},
		{CapVersion81, v81flags},
		{CapVersion8, 0},
	}

	const capsetSize = 12 // version(4) + capsDataLength(4) + flags(4)
	pduLen := uint32(8 + 2 + len(caps)*capsetSize)
	buf := make([]byte, pduLen)
	binary.LittleEndian.PutUint16(buf[0:2], CmdCapsAdvertise)
	binary.LittleEndian.PutUint32(buf[4:8], pduLen)
	binary.LittleEndian.PutUint16(buf[8:10], uint16(len(caps)))
	off := 10
	for _, c := range caps {
		binary.LittleEndian.PutUint32(buf[off:], c.version)
		binary.LittleEndian.PutUint32(buf[off+4:], 4)
		binary.LittleEndian.PutUint32(buf[off+8:], c.flags)
		off += capsetSize
	}
	return h.sendFn(buf)
}

// ProcessPDU handles raw data received on the RDPGFX DVC channel.
// The data is first decompressed via ZGFX, then parsed as RDPGFX PDUs.
// Runs synchronously on the receive goroutine so TCP backpressure naturally
// throttles the server when the client can't keep up.
func (h *Handler) ProcessPDU(data []byte) {
	var err error
	h.decompBuf, err = h.zgfx.Decompress(h.decompBuf[:0], data)
	if err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "ZGFX decompress error", slog.Any("err", err))
		return
	}

	// Parse one or more RDPGFX PDUs from the decompressed buffer
	buf := h.decompBuf
	for len(buf) >= 8 {
		cmdId := binary.LittleEndian.Uint16(buf[0:2])
		// flags := binary.LittleEndian.Uint16(buf[2:4])
		pduLen := binary.LittleEndian.Uint32(buf[4:8])
		if pduLen < 8 || int(pduLen) > len(buf) {
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "PDU length invalid", slog.Int("pduLen", int(pduLen)), slog.Int("bufLen", len(buf)))
			break
		}
		pduData := buf[8:pduLen] // payload after 8-byte header
		h.handleCommand(cmdId, pduData)
		buf = buf[pduLen:]
	}
}

// egfxCmdName returns a human-readable name for an RDPGFX command ID.
func egfxCmdName(cmdId uint16) string {
	switch cmdId {
	case CmdWireToSurface1:
		return "WireToSurface1"
	case CmdWireToSurface2:
		return "WireToSurface2"
	case CmdDeleteEncodingCtx:
		return "DeleteEncodingCtx"
	case CmdSolidFill:
		return "SolidFill"
	case CmdSurfaceToSurface:
		return "SurfaceToSurface"
	case CmdSurfaceToCache:
		return "SurfaceToCache"
	case CmdCacheToSurface:
		return "CacheToSurface"
	case CmdEvictCacheEntry:
		return "EvictCacheEntry"
	case CmdCreateSurface:
		return "CreateSurface"
	case CmdDeleteSurface:
		return "DeleteSurface"
	case CmdStartFrame:
		return "StartFrame"
	case CmdEndFrame:
		return "EndFrame"
	case CmdFrameAcknowledge:
		return "FrameAcknowledge"
	case CmdResetGraphics:
		return "ResetGraphics"
	case CmdMapSurfaceToOutput:
		return "MapSurfaceToOutput"
	case CmdCapsAdvertise:
		return "CapsAdvertise"
	case CmdCapsConfirm:
		return "CapsConfirm"
	case CmdMapSurfaceToWindow:
		return "MapSurfaceToWindow"
	case CmdQoEFrameAcknowledge:
		return "QoEFrameAcknowledge"
	case CmdMapSurfaceToScaledOutput:
		return "MapSurfaceToScaledOutput"
	default:
		return "Unknown"
	}
}

func (h *Handler) handleCommand(cmdId uint16, data []byte) {
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "command received", slog.String("cmd", egfxCmdName(cmdId)), sloghex.Hex4("cmdId", cmdId), slog.Int("len", len(data)))
	switch cmdId {
	case CmdCreateSurface:
		h.handleCreateSurface(data)
	case CmdDeleteSurface:
		h.handleDeleteSurface(data)
	case CmdMapSurfaceToOutput:
		h.handleMapSurfaceToOutput(data)
	case CmdMapSurfaceToScaledOutput:
		h.handleMapSurfaceToScaledOutput(data)
	case CmdStartFrame:
		h.handleStartFrame(data)
	case CmdEndFrame:
		h.handleEndFrame(data)
	case CmdWireToSurface1:
		h.handleWireToSurface1(data)
	case CmdResetGraphics:
		h.handleResetGraphics(data)
	case CmdCapsConfirm:
		h.handleCapsConfirm(data)
	case CmdSolidFill:
		h.handleSolidFill(data)
	case CmdSurfaceToSurface:
		h.handleSurfaceToSurface(data)
	case CmdCacheToSurface:
		h.handleCacheToSurface(data)
	case CmdSurfaceToCache:
		h.handleSurfaceToCache(data)
	case CmdEvictCacheEntry:
		h.handleEvictCacheEntry(data)
	case CmdWireToSurface2:
		h.handleWireToSurface2(data)
	case CmdDeleteEncodingCtx:
		h.handleDeleteEncodingCtx(data)
	default:
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "unhandled command", sloghex.Hex4("cmdId", cmdId), slog.Int("len", len(data)))
	}
}

func (h *Handler) handleCreateSurface(data []byte) {
	if len(data) < 7 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	w := binary.LittleEndian.Uint16(data[2:4])
	hh := binary.LittleEndian.Uint16(data[4:6])
	// pixFmt := data[6]

	// Cap surface count and pixel bytes to prevent unbounded memory growth.
	const maxSurfaces = 256
	old, replacing := h.surfaces[surfId]
	if !replacing && len(h.surfaces) >= maxSurfaces {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "surface limit reached, ignoring create",
			slog.Int("limit", maxSurfaces), slog.Int("surfaceId", int(surfId)))
		return
	}

	// Count in 64 bits: the product of two 16-bit sides overflows int on the
	// 32-bit targets.
	need := int64(w) * int64(hh) * 4
	// The surface replaces the one already under its id, so that buffer goes
	// back to the budget before the new one is counted.
	held := int64(h.pixelBytes)
	if replacing {
		held -= int64(len(old.Data))
	}
	if held+need > maxPixelBytes {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "surface exceeds the client's pixel budget, ignoring create",
			slog.Int("surfaceId", int(surfId)), slog.Int("width", int(w)), slog.Int("height", int(hh)),
			slog.Int64("bytes", need), slog.Int("budget", maxPixelBytes))
		return
	}

	pixBuf := make([]byte, int(need))
	// Fill with 0xFF (opaque white) per MS-RDPEGFX 3.3.5.2.
	// Ensures no pixel has alpha=0, which would cause transparency artifacts
	// if the framebuffer is blitted to the screen before every pixel is painted.
	for i := range pixBuf {
		pixBuf[i] = 0xFF
	}
	if replacing {
		h.givePixelBytes(len(old.Data))
	}
	h.pixelBytes += int(need)
	h.surfaces[surfId] = &Surface{
		ID:     surfId,
		Width:  w,
		Height: hh,
		Data:   pixBuf,
	}
	h.log.LogAttrs(context.Background(), slog.LevelInfo, "surface created", slog.Int("surfaceId", int(surfId)), slog.Int("width", int(w)), slog.Int("height", int(hh)))
}

func (h *Handler) handleDeleteSurface(data []byte) {
	if len(data) < 2 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	if surf := h.surfaces[surfId]; surf != nil {
		h.givePixelBytes(len(surf.Data))
	}
	delete(h.surfaces, surfId)
	delete(h.outputMap, surfId)
	h.progressive.ClearContext(uint32(surfId))
	h.log.LogAttrs(context.Background(), slog.LevelInfo, "surface deleted", slog.Int("surfaceId", int(surfId)))
}

func (h *Handler) handleMapSurfaceToOutput(data []byte) {
	if len(data) < 12 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	// reserved := binary.LittleEndian.Uint16(data[2:4])
	outputOriginX := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	outputOriginY := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	h.outputMap[surfId] = [2]int{outputOriginX, outputOriginY}
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "map surface to output", slog.Int("surfaceId", int(surfId)), slog.Int("x", outputOriginX), slog.Int("y", outputOriginY))
}

func (h *Handler) handleMapSurfaceToScaledOutput(data []byte) {
	// MS-RDPEGFX 2.2.2.18: RDPGFX_MAP_SURFACE_TO_SCALED_OUTPUT_PDU
	// surfaceId(2) + reserved(2) + outputOriginX(4) + outputOriginY(4) + targetWidth(4) + targetHeight(4) = 20
	if len(data) < 20 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	outputOriginX := int(int32(binary.LittleEndian.Uint32(data[4:8])))
	outputOriginY := int(int32(binary.LittleEndian.Uint32(data[8:12])))
	targetWidth := binary.LittleEndian.Uint32(data[12:16])
	targetHeight := binary.LittleEndian.Uint32(data[16:20])
	h.outputMap[surfId] = [2]int{outputOriginX, outputOriginY}
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "map surface to scaled output",
		slog.Int("surfaceId", int(surfId)),
		slog.Int("x", outputOriginX), slog.Int("y", outputOriginY),
		slog.Uint64("targetWidth", uint64(targetWidth)),
		slog.Uint64("targetHeight", uint64(targetHeight)),
	)
}

// parseAVC420Metablock parses an RDPGFX_AVC420_BITMAP_STREAM (MS-RDPEGFX 2.2.4.4).
// Returns region rects, quant/quality values, and remaining NAL unit data.
// If dst is non-nil and has sufficient capacity, it is reused to avoid allocation.
func parseAVC420Metablock(dst []H264Region, data []byte) ([]H264Region, []byte, error) {
	if len(data) < 4 {
		return nil, nil, fmt.Errorf("AVC420 metablock too short: %d", len(data))
	}
	numRegions := binary.LittleEndian.Uint32(data[0:4])
	off := 4

	// Each region rect: 8 bytes (left/top/right/bottom as u16 LE)
	// Each quant/quality: 2 bytes (qpVal:u8, qualityVal:u8)
	need := int(numRegions)*8 + int(numRegions)*2
	if len(data)-off < need {
		return nil, nil, fmt.Errorf("AVC420 metablock truncated: need %d, have %d", need, len(data)-off)
	}

	n := int(numRegions)
	if cap(dst) >= n {
		dst = dst[:n]
	} else {
		dst = make([]H264Region, n)
	}
	for i := range dst {
		dst[i].Left = binary.LittleEndian.Uint16(data[off:])
		dst[i].Top = binary.LittleEndian.Uint16(data[off+2:])
		dst[i].Right = binary.LittleEndian.Uint16(data[off+4:])
		dst[i].Bottom = binary.LittleEndian.Uint16(data[off+6:])
		off += 8
	}
	for i := range dst {
		dst[i].QPVal = data[off]
		dst[i].QualityVal = data[off+1]
		off += 2
	}

	return dst, data[off:], nil
}

// parseAVC444 parses an RDPGFX_AVC444_BITMAP_STREAM (MS-RDPEGFX 2.2.4.5).
// Returns one or two H264Frames depending on the LC field.
func parseAVC444(data []byte, surfID uint16, left, top, right, bottom int, v2 bool) ([]*H264Frame, error) {
	if len(data) < 4 {
		return nil, fmt.Errorf("AVC444 data too short: %d", len(data))
	}
	tmp := binary.LittleEndian.Uint32(data[0:4])
	cbAvc420First := int(tmp & 0x3FFFFFFF)
	lc := (tmp >> 30) & 0x03

	if lc == 3 {
		return nil, fmt.Errorf("AVC444 invalid LC value 3")
	}

	rest := data[4:]

	switch lc {
	case 0: // both luma + chroma
		if cbAvc420First < 4 {
			return nil, fmt.Errorf("AVC444 LC=0 cbAvc420First too small: %d", cbAvc420First)
		}
		// cbAvc420EncodedBitstream1 measures the entire first AVC420 stream
		// (metablock headers + NAL data) from the byte after the uint32 header.
		// Stream 2 follows immediately after (MS-RDPEGFX 2.2.4.5).
		if len(rest) < cbAvc420First {
			return nil, fmt.Errorf("AVC444 LC=0 first stream truncated")
		}
		stream1 := rest[:cbAvc420First]
		stream2 := rest[cbAvc420First:]

		regions1, nal1, err := parseAVC420Metablock(nil, stream1)
		if err != nil {
			return nil, fmt.Errorf("AVC444 luma metablock: %w", err)
		}
		regions2, nal2, err := parseAVC420Metablock(nil, stream2)
		if err != nil {
			return nil, fmt.Errorf("AVC444 chroma metablock: %w", err)
		}
		nal1Copy := make([]byte, len(nal1))
		copy(nal1Copy, nal1)
		nal2Copy := make([]byte, len(nal2))
		copy(nal2Copy, nal2)
		return []*H264Frame{
			{SurfaceID: surfID, CodecMode: 1, AVC444v2: v2, Left: left, Top: top, Right: right, Bottom: bottom, Regions: regions1, NALData: nal1Copy},
			{SurfaceID: surfID, CodecMode: 2, AVC444v2: v2, Left: left, Top: top, Right: right, Bottom: bottom, Regions: regions2, NALData: nal2Copy},
		}, nil

	case 1: // luma only
		regions, nal, err := parseAVC420Metablock(nil, rest)
		if err != nil {
			return nil, fmt.Errorf("AVC444 luma-only metablock: %w", err)
		}
		nalCopy := make([]byte, len(nal))
		copy(nalCopy, nal)
		return []*H264Frame{
			{SurfaceID: surfID, CodecMode: 1, AVC444v2: v2, Left: left, Top: top, Right: right, Bottom: bottom, Regions: regions, NALData: nalCopy},
		}, nil

	case 2: // chroma only
		regions, nal, err := parseAVC420Metablock(nil, rest)
		if err != nil {
			return nil, fmt.Errorf("AVC444 chroma-only metablock: %w", err)
		}
		nalCopy := make([]byte, len(nal))
		copy(nalCopy, nal)
		return []*H264Frame{
			{SurfaceID: surfID, CodecMode: 2, AVC444v2: v2, Left: left, Top: top, Right: right, Bottom: bottom, Regions: regions, NALData: nalCopy},
		}, nil
	}
	return nil, nil
}

func (h *Handler) handleStartFrame(data []byte) {
	if len(data) < 8 {
		return
	}
	// timestamp := binary.LittleEndian.Uint32(data[0:4])
	h.curFrameID = binary.LittleEndian.Uint32(data[4:8])
	// Capture decode start for the QoE frame ack (MS-RDPEGFX 2.2.3.2). The
	// server uses (timeDiffSE, timeDiffEDR) to tune Progressive RFX quality.
	h.frameStartMs = time.Now().UnixMilli()
	h.inFrame = true
	h.frameDirtyRects = h.frameDirtyRects[:0]
	clear(h.frameH264)
	h.frameH264 = h.frameH264[:0]
}

func (h *Handler) handleEndFrame(data []byte) {
	if len(data) < 4 {
		return
	}
	frameID := binary.LittleEndian.Uint32(data[0:4])
	h.framesDecoded++
	h.inFrame = false

	// Send Frame Acknowledge BEFORE display callbacks. The server only
	// needs to know we decoded the frame — rendering can lag behind without
	// stalling the server's frame pipeline.
	// RDPGFX_FRAME_ACKNOWLEDGE_PDU: header(8) + queueDepth(4) + frameId(4) + totalDecoded(4) = 20
	ackBuf := make([]byte, 20)
	binary.LittleEndian.PutUint16(ackBuf[0:2], CmdFrameAcknowledge)
	binary.LittleEndian.PutUint32(ackBuf[4:8], 20) // pduLength
	// queueDepth = QUEUE_DEPTH_UNAVAILABLE (0x00000000): client decodes synchronously
	// so the queue depth is effectively zero on each ack. Sending 0xFFFFFFFF would
	// be SUSPEND_FRAME_ACKNOWLEDGEMENT, which disables server-side adaptive quality
	// throttling for Progressive RFX and other EGFX codecs.
	binary.LittleEndian.PutUint32(ackBuf[8:12], 0x00000000)
	binary.LittleEndian.PutUint32(ackBuf[12:16], frameID)
	binary.LittleEndian.PutUint32(ackBuf[16:20], h.framesDecoded)
	select {
	case h.ackCh <- ackBuf:
	default:
		if err := h.sendFn(ackBuf); err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send frame ack", slog.Any("err", err))
		}
	}

	// Flush each accumulated dirty rect as an independent display update.
	// Measure display callback duration for the QoE timeDiffEDR field.
	displayStart := time.Now()
	if h.onBeginPaint != nil {
		h.onBeginPaint()
	}
	for _, dr := range h.frameDirtyRects {
		h.emitBitmapRect(dr.surfID, dr.left, dr.top, dr.right, dr.bottom)
	}
	for _, f := range h.frameH264 {
		if h.onH264Frame != nil {
			h.onH264Frame(f)
		}
	}
	if h.onEndPaint != nil {
		h.onEndPaint()
	}
	displayMs := time.Since(displayStart).Milliseconds()
	h.frameDirtyRects = h.frameDirtyRects[:0]
	clear(h.frameH264)
	h.frameH264 = h.frameH264[:0]

	// QoE Frame Acknowledge (MS-RDPEGFX 2.2.3.2). Only valid on cap version
	// 10 and above; sending on v8/v8.1 is a protocol error. Provides decode
	// + display latency so the server can adapt Progressive RFX quality
	// tiers to actual client performance.
	if h.negotiatedCapVersion >= CapVersion10 && h.frameStartMs != 0 {
		h.sendQoEFrameAck(frameID, displayMs)
	}
}

// sendQoEFrameAck builds and dispatches a RDPGFX_QOE_FRAME_ACKNOWLEDGE_PDU.
// Layout: header(8) + frameId(4) + timestamp(4) + timeDiffSE(2) + timeDiffEDR(2) = 20.
//
//	timestamp:    low 32 bits of frameStartMs (matches FreeRDP semantics)
//	timeDiffSE:   total ms from handleStartFrame to now (decode + display)
//	timeDiffEDR:  ms spent in display callbacks (clamped to u16)
func (h *Handler) sendQoEFrameAck(frameID uint32, displayMs int64) {
	diffSE := time.Now().UnixMilli() - h.frameStartMs
	if diffSE < 0 || diffSE > 65000 {
		diffSE = 0
	}
	if displayMs < 0 || displayMs > 65000 {
		displayMs = 0
	}
	qoeBuf := make([]byte, 20)
	binary.LittleEndian.PutUint16(qoeBuf[0:2], CmdQoEFrameAcknowledge)
	binary.LittleEndian.PutUint32(qoeBuf[4:8], 20) // pduLength
	binary.LittleEndian.PutUint32(qoeBuf[8:12], frameID)
	binary.LittleEndian.PutUint32(qoeBuf[12:16], uint32(h.frameStartMs))
	binary.LittleEndian.PutUint16(qoeBuf[16:18], uint16(diffSE))
	binary.LittleEndian.PutUint16(qoeBuf[18:20], uint16(displayMs))
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "QoE frame ack",
		slog.Int64("frameId", int64(frameID)),
		slog.Int64("timeDiffSE", diffSE),
		slog.Int64("timeDiffEDR", displayMs))
	select {
	case h.ackCh <- qoeBuf:
	default:
		if err := h.sendFn(qoeBuf); err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send QoE frame ack", slog.Any("err", err))
		}
	}
}

func (h *Handler) handleWireToSurface1(data []byte) {
	// RDPGFX_WIRE_TO_SURFACE_PDU_1:
	//   surfaceId(2) + codecId(2) + pixelFormat(1) + destRect(8: left,top,right,bottom as u16)
	//   + bitmapDataLength(4) + bitmapData(...)
	if len(data) < 17 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	codecId := binary.LittleEndian.Uint16(data[2:4])
	// pixFmt := data[4]
	left := int(binary.LittleEndian.Uint16(data[5:7]))
	top := int(binary.LittleEndian.Uint16(data[7:9]))
	right := int(binary.LittleEndian.Uint16(data[9:11]))
	bottom := int(binary.LittleEndian.Uint16(data[11:13]))
	bitmapLen := binary.LittleEndian.Uint32(data[13:17])
	if int(bitmapLen) > len(data)-17 {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface1 bitmap data truncated")
		return
	}
	bitmapData := data[17 : 17+bitmapLen]

	w := right - left
	hh := bottom - top
	if w <= 0 || hh <= 0 {
		return
	}

	surf := h.surfaces[surfId]
	if surf == nil {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface1 unknown surface", slog.Int("surfaceId", int(surfId)))
		return
	}
	if !rectInside(surf, left, top, right, bottom) {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface1 rectangle outside its surface",
			slog.Int("surfaceId", int(surfId)), slog.Int("surfaceWidth", int(surf.Width)), slog.Int("surfaceHeight", int(surf.Height)),
			slog.Int("left", left), slog.Int("top", top), slog.Int("right", right), slog.Int("bottom", bottom))
		return
	}

	h.log.LogAttrs(context.Background(), slog.LevelDebug, "WireToSurface1", slog.Int("surfaceId", int(surfId)), sloghex.Hex4("codecId", codecId),
		slog.Int("left", left), slog.Int("top", top), slog.Int("right", right), slog.Int("bottom", bottom), slog.Int("width", w), slog.Int("height", hh), slog.Int("bitmapLen", int(bitmapLen)))

	var pixels []byte

	switch codecId {
	case CodecUncompressed:
		// Wire format is BGRX; convert to RGBA in-place using codecBuf.
		need := w * hh * 4
		if len(bitmapData) < need {
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface1 uncompressed bitmap shorter than its rectangle",
				slog.Int("surfaceId", int(surfId)), slog.Int("width", w), slog.Int("height", hh),
				slog.Int("bytes", len(bitmapData)), slog.Int("want", need))
			return
		}
		if cap(h.codecBuf) >= need {
			h.codecBuf = h.codecBuf[:need]
		} else {
			h.codecBuf = make([]byte, need)
		}
		for i := 0; i < need; i += 4 {
			h.codecBuf[i] = bitmapData[i+2]   // R
			h.codecBuf[i+1] = bitmapData[i+1] // G
			h.codecBuf[i+2] = bitmapData[i]   // B
			h.codecBuf[i+3] = 0xFF
		}
		pixels = h.codecBuf

	case CodecPlanar:
		// RDP 6.0 Planar Codec. In EGFX, planar wire data is already
		// top-down (FreeRDP gfx.c passes vFlip=FALSE). DecompressPlanar
		// preserves wire order, so no flip is needed here. The bottom-up
		// flip is only required for legacy bitmap updates (client.go).
		var err error
		h.codecBuf, err = rle.DecompressPlanar(h.codecBuf[:0], w, hh, bitmapData)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "planar decompress error", slog.Any("err", err))
			return
		}
		pixels = h.codecBuf

	case CodecClearCodec:
		if h.clearcodec == nil {
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "ClearCodec data but no decoder set")
			return
		}
		var err error
		h.codecBuf, err = h.clearcodec.Decompress(h.codecBuf[:0], w, hh, bitmapData)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "ClearCodec decompress error", slog.Any("err", err))
			return
		}
		pixels = h.codecBuf

		// Sample pixels for color diagnostics
		if w > 0 && hh > 0 {
			midY := hh / 2
			midX := w / 2
			midOff := midY*w*4 + midX*4
			if midOff+4 <= len(pixels) {
				h.log.Log(context.Background(), levelTrace, "ClearCodec output midPixel",
					"width", w, "height", hh,
					"r", pixels[midOff], "g", pixels[midOff+1], "b", pixels[midOff+2], "a", pixels[midOff+3])
			}
		}

	case CodecAVC420:
		if h.onH264Frame == nil {
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "AVC420 data but no H.264 callback set")
			return
		}
		regions, nalData, err := parseAVC420Metablock(nil, bitmapData)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "AVC420 parse error", slog.Any("err", err))
			return
		}
		// Make a copy of the NAL data since bitmapData references the decompression buffer.
		nalCopy := make([]byte, len(nalData))
		copy(nalCopy, nalData)
		origin := h.outputMap[surfId]
		frame := &H264Frame{
			SurfaceID:     surfId,
			CodecMode:     0, // AVC420
			Left:          left,
			Top:           top,
			Right:         right,
			Bottom:        bottom,
			OutputOriginX: origin[0],
			OutputOriginY: origin[1],
			Regions:       regions,
			NALData:       nalCopy,
		}
		if h.inFrame {
			h.frameH264 = append(h.frameH264, frame)
		} else {
			h.onH264Frame(frame)
		}
		return

	case CodecAVC444, CodecAVC444v2:
		if h.onH264Frame == nil {
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "AVC444 data but no H.264 callback set")
			return
		}
		frames, err := parseAVC444(bitmapData, surfId, left, top, right, bottom, codecId == CodecAVC444v2)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "AVC444 parse error", slog.Any("err", err))
			return
		}
		avc444Origin := h.outputMap[surfId]
		for _, f := range frames {
			f.OutputOriginX = avc444Origin[0]
			f.OutputOriginY = avc444Origin[1]
			h.log.LogAttrs(context.Background(), slog.LevelDebug, "AVC444 frame",
				slog.Int("codecMode", int(f.CodecMode)),
				slog.Bool("v2", f.AVC444v2),
				slog.Int("nalLen", len(f.NALData)),
				slog.Int("regions", len(f.Regions)))
		}
		if h.inFrame {
			h.frameH264 = append(h.frameH264, frames...)
		} else {
			for _, f := range frames {
				h.onH264Frame(f)
			}
		}
		return

	default:
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "unsupported codec", sloghex.Hex4("codecId", codecId))
		return
	}

	// Write decoded pixels to surface (top-down RGBA)
	srcStride := w * 4
	dstStride := int(surf.Width) * 4

	if len(pixels) < srcStride*hh {
		return
	}

	for row := 0; row < hh; row++ {
		dstOff := (top+row)*dstStride + left*4
		srcOff := row * srcStride
		if dstOff+srcStride <= len(surf.Data) {
			copy(surf.Data[dstOff:dstOff+w*4], pixels[srcOff:srcOff+w*4])
		}
	}

	h.notifyBitmap(surfId, left, top, w, hh)
}

func (h *Handler) handleWireToSurface2(data []byte) {
	// RDPGFX_WIRE_TO_SURFACE_PDU_2 (MS-RDPEGFX 2.2.2.2):
	//   surfaceId(2) + codecId(2) + codecContextId(4) + pixelFormat(1)
	//   + bitmapDataLength(4) + bitmapData(...)
	if len(data) < 13 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	codecId := binary.LittleEndian.Uint16(data[2:4])
	ctxId := binary.LittleEndian.Uint32(data[4:8])
	// pixFmt := data[8]
	bitmapLen := binary.LittleEndian.Uint32(data[9:13])

	if codecId != CodecProgressive {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface2 unsupported codec", sloghex.Hex4("codecId", codecId),
			slog.Int("surfaceId", int(surfId)), slog.Int("ctxId", int(ctxId)), slog.Int("bitmapLen", int(bitmapLen)))
		return
	}

	if int(bitmapLen) > len(data)-13 {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface2 bitmap data truncated")
		return
	}
	bitmapData := data[13 : 13+bitmapLen]

	surf := h.surfaces[surfId]
	if surf == nil {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "WireToSurface2 unknown surface", slog.Int("surfaceId", int(surfId)))
		return
	}

	// Progressive tile state is stored per surface (not per codecContextId).
	// The server increments codecContextId per PDU, but coeffDiff tiles reference
	// prior state within the same surface.
	regions, err := h.progressive.Decode(bitmapData, uint32(surfId))
	if err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "progressive decode error", slog.Int("surfaceId", int(surfId)), slog.Int("ctxId", int(ctxId)), slog.Any("err", err))
		return
	}

	// Log decoded regions and tile coordinates for diagnostics.
	for ri, region := range regions {
		for _, r := range region.Rects {
			h.log.LogAttrs(context.Background(), slog.LevelDebug, "WireToSurface2 region rect",
				slog.Int("surfaceId", int(surfId)), slog.Int("region", ri),
				slog.Int("left", int(r[0])), slog.Int("top", int(r[1])),
				slog.Int("right", int(r[2])), slog.Int("bottom", int(r[3])))
		}
		for _, tile := range region.Tiles {
			h.log.LogAttrs(context.Background(), slog.LevelDebug, "WireToSurface2 tile",
				slog.Int("surfaceId", int(surfId)), slog.Int("region", ri),
				slog.Int("tileX", tile.X), slog.Int("tileY", tile.Y))
		}
	}

	// Write decoded tiles to surface, clipped to region rects.
	// Region rects define the valid output area — tiles on boundaries must be
	// clipped to avoid overwriting adjacent content (e.g. window pixels placed
	// by a prior SurfaceToSurface).
	//
	// Per MS-RDPEGFX 3.3.8.1, ALL cached tiles overlapping region rects must
	// be rendered — not only tiles decoded in this PDU. The server may split
	// a progressive region across multiple WTS2 PDUs within a frame, sending
	// each tile once but expecting the client to blit cached tiles from prior
	// decodes against later region rects (e.g. tile at tileX=832 decoded in
	// the first WTS2 must also be blitted for the second WTS2's region rect
	// that covers x=852..896).
	dstStride := int(surf.Width) * 4
	for _, region := range regions {
		// Blit freshly decoded tiles and track which tile positions were decoded.
		decodedSet := make(map[[2]int]struct{}, len(region.Tiles))
		for _, tile := range region.Tiles {
			decodedSet[[2]int{tile.X / 64, tile.Y / 64}] = struct{}{}
			tileRight := tile.X + 64
			tileBottom := tile.Y + 64
			for _, r := range region.Rects {
				rl, rt, rr, rb := int(r[0]), int(r[1]), int(r[2]), int(r[3])
				cl := tile.X
				if rl > cl {
					cl = rl
				}
				ct := tile.Y
				if rt > ct {
					ct = rt
				}
				cr := tileRight
				if rr < cr {
					cr = rr
				}
				cb := tileBottom
				if rb < cb {
					cb = rb
				}
				if cl < cr && ct < cb {
					h.blitTileClipped(surf, dstStride, surfId, tile.X, tile.Y, tile.Data,
						cl, ct, cr, cb)
				}
			}
			rfx.ReleaseTileBuffer(tile.Data)
		}

		// Blit cached tiles that overlap region rects but weren't decoded above.
		// Compute bounding box of all region rects to limit cache lookup scope.
		var bbL, bbT, bbR, bbB int
		for i, r := range region.Rects {
			rl, rt, rr, rb := int(r[0]), int(r[1]), int(r[2]), int(r[3])
			if i == 0 {
				bbL, bbT, bbR, bbB = rl, rt, rr, rb
			} else {
				if rl < bbL {
					bbL = rl
				}
				if rt < bbT {
					bbT = rt
				}
				if rr > bbR {
					bbR = rr
				}
				if rb > bbB {
					bbB = rb
				}
			}
		}
		cached := h.progressive.GetCachedTiles(uint32(surfId), bbL, bbT, bbR, bbB)
		for _, ct := range cached {
			key := [2]int{ct.X / 64, ct.Y / 64}
			if _, ok := decodedSet[key]; ok {
				continue // already blitted from freshly decoded tiles
			}
			ctRight := ct.X + 64
			ctBottom := ct.Y + 64
			for _, r := range region.Rects {
				rl, rt, rr, rb := int(r[0]), int(r[1]), int(r[2]), int(r[3])
				cl := ct.X
				if rl > cl {
					cl = rl
				}
				ctp := ct.Y
				if rt > ctp {
					ctp = rt
				}
				cr := ctRight
				if rr < cr {
					cr = rr
				}
				cb := ctBottom
				if rb < cb {
					cb = rb
				}
				if cl < cr && ctp < cb {
					h.blitTileClipped(surf, dstStride, surfId, ct.X, ct.Y, ct.Data,
						cl, ctp, cr, cb)
				}
			}
		}
	}
}

// blitTile writes a 64x64 RGBA tile to the surface at (tileX, tileY) and emits the bitmap callback.
func (h *Handler) blitTile(surf *Surface, dstStride int, surfId uint16, tileX, tileY int, tileData []byte) {
	tw := 64
	th := 64
	// Clamp to surface bounds
	if tileX+tw > int(surf.Width) {
		tw = int(surf.Width) - tileX
	}
	if tileY+th > int(surf.Height) {
		th = int(surf.Height) - tileY
	}
	if tw <= 0 || th <= 0 {
		return
	}

	srcStride := 64 * 4
	for row := 0; row < th; row++ {
		dstOff := (tileY+row)*dstStride + tileX*4
		srcOff := row * srcStride
		copyLen := tw * 4
		if dstOff+copyLen <= len(surf.Data) && srcOff+copyLen <= len(tileData) {
			copy(surf.Data[dstOff:dstOff+copyLen], tileData[srcOff:srcOff+copyLen])
		}
	}

	h.notifyBitmap(surfId, tileX, tileY, tw, th)
}

func (h *Handler) handleResetGraphics(data []byte) {
	if len(data) < 12 {
		return
	}
	w := int(binary.LittleEndian.Uint32(data[0:4]))
	hh := int(binary.LittleEndian.Uint32(data[4:8]))
	// monitorCount := binary.LittleEndian.Uint32(data[8:12])
	// followed by monitor definitions, pad to 340 bytes

	// Clear all surfaces and output maps. The progressive decoder is kept:
	// its tile context is not part of the graphics output, and Windows keeps
	// sending UPGRADE tiles for a surface that it recreates after the reset
	// without repeating the first pass's SYNC and CONTEXT blocks.
	// NOTE: bitmap cache (h.cache) is NOT cleared — the server expects
	// cached bitmaps to survive ResetGraphics and uses CacheToSurface
	// immediately after creating the new surface.
	for _, surf := range h.surfaces {
		h.givePixelBytes(len(surf.Data))
	}
	clear(h.surfaces)
	clear(h.outputMap)
	h.frameDirtyRects = h.frameDirtyRects[:0]
	clear(h.frameH264)
	h.frameH264 = h.frameH264[:0]
	h.inFrame = false
	// Reset ClearCodec sequence number per MS-RDPEGFX 3.1.8.1.1.
	// Required by MS-RDPEGFX 3.1.8.1.1 on graphics reset.
	if h.clearcodec != nil {
		h.clearcodec.ResetState()
	}

	h.log.LogAttrs(context.Background(), slog.LevelInfo, "graphics reset", slog.Int("width", w), slog.Int("height", hh))

	if h.onResetGraphics != nil {
		h.onResetGraphics(w, hh)
	}
}

func (h *Handler) handleCapsConfirm(data []byte) {
	if len(data) < 12 {
		return
	}
	version := binary.LittleEndian.Uint32(data[0:4])
	// capsDataLength := binary.LittleEndian.Uint32(data[4:8])
	// capsData/flags := binary.LittleEndian.Uint32(data[8:12])
	h.negotiatedCapVersion = version
	h.log.LogAttrs(context.Background(), slog.LevelInfo, "caps confirmed", sloghex.Hex8("version", version))
}

func (h *Handler) handleSolidFill(data []byte) {
	// RDPGFX_SOLID_FILL_PDU:
	//   surfaceId(2) + fillPixel(4: B,G,R,A) + fillRectCount(2) + rects(8 each)
	if len(data) < 8 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	b := data[2]
	g := data[3]
	r := data[4]
	// a := data[5]
	rectCount := binary.LittleEndian.Uint16(data[6:8])

	surf := h.surfaces[surfId]
	if surf == nil {
		return
	}

	h.log.LogAttrs(context.Background(), slog.LevelDebug, "SolidFill", slog.Int("surfaceId", int(surfId)), slog.Int("b", int(b)), slog.Int("g", int(g)), slog.Int("r", int(r)), slog.Int("rectCount", int(rectCount)))

	dstStride := int(surf.Width) * 4
	off := 8
	for i := 0; i < int(rectCount); i++ {
		if off+8 > len(data) {
			break
		}
		left := int(binary.LittleEndian.Uint16(data[off : off+2]))
		top := int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
		right := int(binary.LittleEndian.Uint16(data[off+4 : off+6]))
		bottom := int(binary.LittleEndian.Uint16(data[off+6 : off+8]))
		off += 8

		w := right - left
		hh := bottom - top
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "SolidFill rect", slog.Int("index", i), slog.Int("left", left), slog.Int("top", top), slog.Int("right", right), slog.Int("bottom", bottom), slog.Int("width", w), slog.Int("height", hh))
		for row := 0; row < hh; row++ {
			rowOff := (top+row)*dstStride + left*4
			for col := 0; col < w; col++ {
				px := rowOff + col*4
				if px+4 <= len(surf.Data) {
					surf.Data[px] = r
					surf.Data[px+1] = g
					surf.Data[px+2] = b
					surf.Data[px+3] = 0xFF
				}
			}
		}

		h.notifyBitmap(surfId, left, top, w, hh)
	}
}

func (h *Handler) handleSurfaceToSurface(data []byte) {
	// RDPGFX_SURFACE_TO_SURFACE_PDU:
	//   surfIdSrc(2) + surfIdDst(2) + srcRect(8) + destPtsCount(2) + destPts(4 each)
	if len(data) < 14 {
		return
	}
	srcSurfId := binary.LittleEndian.Uint16(data[0:2])
	dstSurfId := binary.LittleEndian.Uint16(data[2:4])
	srcLeft := int(binary.LittleEndian.Uint16(data[4:6]))
	srcTop := int(binary.LittleEndian.Uint16(data[6:8]))
	srcRight := int(binary.LittleEndian.Uint16(data[8:10]))
	srcBottom := int(binary.LittleEndian.Uint16(data[10:12]))
	destPtsCount := binary.LittleEndian.Uint16(data[12:14])

	srcSurf := h.surfaces[srcSurfId]
	dstSurf := h.surfaces[dstSurfId]
	if srcSurf == nil || dstSurf == nil {
		return
	}
	if !rectInside(srcSurf, srcLeft, srcTop, srcRight, srcBottom) {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "SurfaceToSurface source rectangle outside its surface",
			slog.Int("srcSurf", int(srcSurfId)), slog.Int("surfaceWidth", int(srcSurf.Width)), slog.Int("surfaceHeight", int(srcSurf.Height)),
			slog.Int("srcLeft", srcLeft), slog.Int("srcTop", srcTop), slog.Int("srcRight", srcRight), slog.Int("srcBottom", srcBottom))
		return
	}

	w := srcRight - srcLeft
	hh := srcBottom - srcTop
	if w <= 0 || hh <= 0 {
		return
	}

	h.log.LogAttrs(context.Background(), slog.LevelDebug, "SurfaceToSurface",
		slog.Int("srcSurf", int(srcSurfId)), slog.Int("dstSurf", int(dstSurfId)),
		slog.Int("srcLeft", srcLeft), slog.Int("srcTop", srcTop),
		slog.Int("srcRight", srcRight), slog.Int("srcBottom", srcBottom),
		slog.Int("width", w), slog.Int("height", hh),
		slog.Int("destPtsCount", int(destPtsCount)))

	sameSurface := srcSurfId == dstSurfId
	srcStride := int(srcSurf.Width) * 4
	dstStride := int(dstSurf.Width) * 4
	rowBytes := w * 4

	// For same-surface blits, copy source rect to temp buffer first to handle overlap.
	var srcData []byte
	var tmpStride int
	if sameSurface {
		need := rowBytes * hh
		if cap(h.blitTmpBuf) < need {
			h.blitTmpBuf = make([]byte, need)
		}
		h.blitTmpBuf = h.blitTmpBuf[:need]
		clear(h.blitTmpBuf)
		for row := 0; row < hh; row++ {
			srcOff := (srcTop+row)*srcStride + srcLeft*4
			if srcOff+rowBytes <= len(srcSurf.Data) {
				copy(h.blitTmpBuf[row*rowBytes:], srcSurf.Data[srcOff:srcOff+rowBytes])
			}
		}
		srcData = h.blitTmpBuf
		tmpStride = rowBytes
	} else {
		srcData = srcSurf.Data
		tmpStride = srcStride
	}

	off := 14
	for i := 0; i < int(destPtsCount); i++ {
		if off+4 > len(data) {
			break
		}
		dstX := int(binary.LittleEndian.Uint16(data[off : off+2]))
		dstY := int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
		off += 4

		h.log.LogAttrs(context.Background(), slog.LevelDebug, "SurfaceToSurface dest",
			slog.Int("dstX", dstX), slog.Int("dstY", dstY),
			slog.Int("dstRight", dstX+w), slog.Int("dstBottom", dstY+hh))

		for row := 0; row < hh; row++ {
			var srcOff int
			if sameSurface {
				srcOff = row * tmpStride
			} else {
				srcOff = (srcTop+row)*tmpStride + srcLeft*4
			}
			dOff := (dstY+row)*dstStride + dstX*4
			if srcOff+rowBytes <= len(srcData) && dOff+rowBytes <= len(dstSurf.Data) {
				copy(dstSurf.Data[dOff:dOff+rowBytes], srcData[srcOff:srcOff+rowBytes])
			}
		}

		h.notifyBitmap(dstSurfId, dstX, dstY, w, hh)
	}
	// The server repaints exposed source areas via WTS1/WTS2/CacheToSurface
	// commands in this frame; each calls notifyBitmap for its own region.
	// No source marking needed — per-rect dirty tracking prevents the
	// bounding box inflation that caused ghost artifacts with the old
	// single-bounding-box approach.
}

func (h *Handler) handleSurfaceToCache(data []byte) {
	// RDPGFX_SURFACE_TO_CACHE_PDU:
	//   surfaceId(2) + cacheKey(8) + cacheSlot(2) + rectSrc(8: left,top,right,bottom as u16)
	if len(data) < 20 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	// cacheKey at data[2:10] — not needed for cache storage
	cacheSlot := binary.LittleEndian.Uint16(data[10:12])
	left := int(binary.LittleEndian.Uint16(data[12:14]))
	top := int(binary.LittleEndian.Uint16(data[14:16]))
	right := int(binary.LittleEndian.Uint16(data[16:18]))
	bottom := int(binary.LittleEndian.Uint16(data[18:20]))

	surf := h.surfaces[surfId]
	if surf == nil {
		return
	}
	if !rectInside(surf, left, top, right, bottom) {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "SurfaceToCache rectangle outside its surface",
			slog.Int("surfaceId", int(surfId)), slog.Int("surfaceWidth", int(surf.Width)), slog.Int("surfaceHeight", int(surf.Height)),
			slog.Int("left", left), slog.Int("top", top), slog.Int("right", right), slog.Int("bottom", bottom))
		return
	}

	w := right - left
	hh := bottom - top
	if w <= 0 || hh <= 0 {
		return
	}

	srcStride := int(surf.Width) * 4
	rowBytes := w * 4
	pixBuf := make([]byte, rowBytes*hh)
	for row := 0; row < hh; row++ {
		srcOff := (top+row)*srcStride + left*4
		if srcOff+rowBytes <= len(surf.Data) {
			copy(pixBuf[row*rowBytes:], surf.Data[srcOff:srcOff+rowBytes])
		}
	}

	// Cap cache entries and pixel bytes to prevent unbounded memory growth: the
	// old entry under this slot and then other entries make room.
	if old := h.cache[cacheSlot]; old != nil {
		h.givePixelBytes(len(old.data))
		delete(h.cache, cacheSlot)
	}
	for len(h.cache) >= maxCacheEntries || !h.fitsPixelBytes(len(pixBuf)) {
		evicted := false
		for k, entry := range h.cache {
			h.givePixelBytes(len(entry.data))
			delete(h.cache, k)
			evicted = true
			break
		}
		if !evicted {
			break
		}
	}
	if !h.fitsPixelBytes(len(pixBuf)) {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "bitmap cache would exceed the client's pixel budget, not caching",
			slog.Int("cacheSlot", int(cacheSlot)), slog.Int("bytes", len(pixBuf)), slog.Int("budget", maxPixelBytes))
		return
	}
	h.pixelBytes += len(pixBuf)
	h.cache[cacheSlot] = &cacheEntry{data: pixBuf, width: w, height: hh}

	h.log.LogAttrs(context.Background(), slog.LevelDebug, "SurfaceToCache",
		slog.Int("surfaceId", int(surfId)), slog.Int("cacheSlot", int(cacheSlot)),
		slog.Int("left", left), slog.Int("top", top), slog.Int("right", right), slog.Int("bottom", bottom),
		slog.Int("width", w), slog.Int("height", hh))
}

func (h *Handler) handleCacheToSurface(data []byte) {
	// RDPGFX_CACHE_TO_SURFACE_PDU:
	//   cacheSlot(2) + surfaceId(2) + destPtsCount(2) + destPts(4 each)
	if len(data) < 6 {
		return
	}
	cacheSlot := binary.LittleEndian.Uint16(data[0:2])
	surfId := binary.LittleEndian.Uint16(data[2:4])
	destPtsCount := int(binary.LittleEndian.Uint16(data[4:6]))

	entry := h.cache[cacheSlot]
	if entry == nil || entry.data == nil {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "CacheToSurface cache slot is nil", slog.Int("cacheSlot", int(cacheSlot)))
		return
	}

	surf := h.surfaces[surfId]
	if surf == nil {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "CacheToSurface surface not found", slog.Int("surfaceId", int(surfId)))
		return
	}

	w := entry.width
	hh := entry.height
	dstStride := int(surf.Width) * 4
	rowBytes := w * 4

	off := 6
	for i := 0; i < destPtsCount; i++ {
		if off+4 > len(data) {
			break
		}
		dstX := int(binary.LittleEndian.Uint16(data[off : off+2]))
		dstY := int(binary.LittleEndian.Uint16(data[off+2 : off+4]))
		off += 4
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "CacheToSurface",
			slog.Int("cacheSlot", int(cacheSlot)),
			slog.Int("surfaceId", int(surfId)),
			slog.Int("width", w), slog.Int("height", hh),
			slog.Int("dstX", dstX), slog.Int("dstY", dstY))

		for row := 0; row < hh; row++ {
			srcOff := row * rowBytes
			dOff := (dstY+row)*dstStride + dstX*4
			if dOff+rowBytes <= len(surf.Data) && srcOff+rowBytes <= len(entry.data) {
				copy(surf.Data[dOff:dOff+rowBytes], entry.data[srcOff:srcOff+rowBytes])
			}
		}

		h.notifyBitmap(surfId, dstX, dstY, w, hh)
	}
}

func (h *Handler) handleDeleteEncodingCtx(data []byte) {
	// RDPGFX_DELETE_ENCODING_CONTEXT_PDU:
	//   surfaceId(2) + codecContextId(4)
	if len(data) < 6 {
		return
	}
	surfId := binary.LittleEndian.Uint16(data[0:2])
	ctxId := binary.LittleEndian.Uint32(data[2:6])
	// Progressive tile state is keyed by surfaceId; we cannot selectively
	// clear by codecContextId. This is intentionally a no-op to avoid
	// destroying tile coefficients needed by other encoding contexts.
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "encoding context deleted",
		slog.Int("surfaceId", int(surfId)), slog.Int("ctxId", int(ctxId)))
}

// DumpSurfaces returns a snapshot of all surfaces as top-down RGBA pixel data.
// Each entry maps surfaceID to (width, height, []byte).
func (h *Handler) DumpSurfaces() map[uint16]struct {
	Width, Height int
	Data          []byte
} {
	result := make(map[uint16]struct {
		Width, Height int
		Data          []byte
	}, len(h.surfaces))
	for id, surf := range h.surfaces {
		w := int(surf.Width)
		hh := int(surf.Height)
		snap := make([]byte, len(surf.Data))
		copy(snap, surf.Data)
		result[id] = struct {
			Width, Height int
			Data          []byte
		}{w, hh, snap}
	}
	return result
}

// blitTileClipped writes a 64x64 RGBA tile to the surface, clipped to the given rect,
// and emits the bitmap callback for the clipped region.
func (h *Handler) blitTileClipped(surf *Surface, dstStride int, surfId uint16,
	tileX, tileY int, tileData []byte, clipLeft, clipTop, clipRight, clipBottom int) {
	// Tile covers [tileX, tileX+64) x [tileY, tileY+64)
	tileRight := tileX + 64
	tileBottom := tileY + 64

	// Intersect tile rect with clip rect
	x1 := tileX
	if clipLeft > x1 {
		x1 = clipLeft
	}
	y1 := tileY
	if clipTop > y1 {
		y1 = clipTop
	}
	x2 := tileRight
	if clipRight < x2 {
		x2 = clipRight
	}
	y2 := tileBottom
	if clipBottom < y2 {
		y2 = clipBottom
	}

	// Clamp to surface bounds
	if x2 > int(surf.Width) {
		x2 = int(surf.Width)
	}
	if y2 > int(surf.Height) {
		y2 = int(surf.Height)
	}

	w := x2 - x1
	hh := y2 - y1
	if w <= 0 || hh <= 0 {
		return
	}

	srcStride := 64 * 4
	for row := 0; row < hh; row++ {
		srcOff := (y1-tileY+row)*srcStride + (x1-tileX)*4
		dstOff := (y1+row)*dstStride + x1*4
		copyLen := w * 4
		if dstOff+copyLen <= len(surf.Data) && srcOff+copyLen <= len(tileData) {
			copy(surf.Data[dstOff:dstOff+copyLen], tileData[srcOff:srcOff+copyLen])
		}
	}

	h.notifyBitmap(surfId, x1, y1, w, hh)
}

func (h *Handler) handleEvictCacheEntry(data []byte) {
	if len(data) < 2 {
		return
	}
	cacheSlot := binary.LittleEndian.Uint16(data[0:2])
	if entry := h.cache[cacheSlot]; entry != nil {
		h.givePixelBytes(len(entry.data))
	}
	delete(h.cache, cacheSlot)
}
