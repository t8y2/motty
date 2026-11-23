package rfx

import (
	"encoding/binary"
	"fmt"
	"log/slog"
)

// RemoteFX (CAVIDEO, RDPGFX_CODECID_CAVIDEO) is the bitmap codec of the RDP 8.0
// graphics pipeline, the one Windows 8 and Server 2012 (not R2) offer: those
// servers cannot send the progressive codec that 8.1 and later use. A message
// is a sequence of RFX blocks; the headers (SYNC, CONTEXT, CODEC_VERSIONS,
// CHANNELS) state how the tiles are encoded, and every frame is a FRAME_BEGIN,
// one or more REGIONs with their TILESETs, and a FRAME_END. The tiles
// themselves are self-contained, so unlike the progressive decoder this one
// keeps no per-tile state: only the context's entropy mode carries over.
//
// MS-RDPRFX: TS_RFX_CODEC_CHANNELT (2.2.2.1.2), TS_RFX_CODEC_QUANT (2.2.2.1.5),
// TS_RFX_CONTEXT (2.2.2.2.4), TS_RFX_REGION (2.2.2.3.3), TS_RFX_TILESET
// (2.2.2.3.4), TS_RFX_TILE (2.2.2.3.4.1).

// RFX block types (MS-RDPRFX 2.2.2.1.1 and 2.2.2.1.2).
const (
	rfxBlockSync     = 0xCCC0
	rfxBlockVersions = 0xCCC1
	rfxBlockChannels = 0xCCC2
	rfxBlockContext  = 0xCCC3
	rfxBlockFrameBeg = 0xCCC4
	rfxBlockFrameEnd = 0xCCC5
	rfxBlockRegion   = 0xCCC6
	rfxBlockTileset  = 0xCCC7
	rfxBlockTile     = 0xCAC3

	rfxSubtypeTileset = 0xCAC2
	rfxRegionType     = 0xCAC1
)

// RFX entropy algorithms from the context's properties (MS-RDPRFX 2.2.2.2.4).
const (
	rfxEntropyRLGR1 = 1
	rfxEntropyRLGR3 = 4
)

// remoteFXQuantBands orders the ten quantization values of TS_RFX_CODEC_QUANT
// after the subbands they scale: the wire order is LL3, LH3, HL3, HH3, LH2,
// HL2, HH2, LH1, HL1, HH1, and this is the matching coefficient offset and
// element count (MS-RDPRFX 3.1.8.1.5).
var remoteFXQuantBands = [...]struct {
	offset int
	count  int
}{
	{offLL3, 64}, {offLH3, 64}, {offHL3, 64}, {offHH3, 64},
	{offLH2, 256}, {offHL2, 256}, {offHH2, 256},
	{offLH1, 1024}, {offHL1, 1024}, {offHH1, 1024},
}

// RemoteFXRect is one changed rectangle of a frame, in the message's own
// coordinates (that is, before the destination the caller placed it at).
type RemoteFXRect struct {
	X, Y, Width, Height int
}

// RemoteFXDecoder decodes CAVIDEO bitmap data. Its scratch buffers are reused
// between tiles and messages; it is not safe for concurrent use.
type RemoteFXDecoder struct {
	log *slog.Logger
	// entropy is the entropy algorithm the context named, 0 before a context
	// has been seen.
	entropy int
	// width and height are the channel's size, which stands in for the changed
	// rectangle when a region names none (MS-RDPRFX 2.2.2.3.3).
	width, height int
	// work holds the Y, Cb and Cr coefficients of the tile being decoded, with
	// dwt the scratch buffer the inverse transform needs.
	work [3][]int16
	dwt  []int16
	rgba []byte
}

// NewRemoteFXDecoder creates a decoder.
func NewRemoteFXDecoder(log *slog.Logger) *RemoteFXDecoder {
	buf := make([]int16, 3*4096)
	return &RemoteFXDecoder{
		log:  log,
		work: [3][]int16{buf[0:4096], buf[4096:8192], buf[8192:12288]},
		dwt:  make([]int16, 4096),
		rgba: make([]byte, 64*64*4),
	}
}

// Decode parses one CAVIDEO message and hands every tile rectangle it holds to
// emit, clipped to the frame's region rectangles: pixels outside a region are
// not valid (MS-RDPRFX 2.2.2.3.3). Coordinates are in the message's own space;
// the caller adds the destination it placed the message at.
func (d *RemoteFXDecoder) Decode(data []byte, emit func(x, y, w, h int, pixels []byte, stride int)) error {
	if len(data) == 0 {
		return nil
	}
	var rects []RemoteFXRect
	for off := 0; off+6 <= len(data); {
		blockType := binary.LittleEndian.Uint16(data[off:])
		blockLen := int(binary.LittleEndian.Uint32(data[off+2:]))
		if blockLen < 6 || off+blockLen > len(data) {
			return fmt.Errorf("rfx: block 0x%04X has length %d outside the %d-byte message", blockType, blockLen, len(data))
		}
		body := data[off+6 : off+blockLen]
		off += blockLen

		switch blockType {
		case rfxBlockContext:
			// codecId(1) + channelId(1) + ctxId(1) + tileSize(2) + properties(2)
			if len(body) < 7 {
				return fmt.Errorf("rfx: context block too short (%d bytes)", len(body))
			}
			properties := binary.LittleEndian.Uint16(body[5:7])
			switch entropy := int((properties >> 9) & 0x0F); entropy {
			case rfxEntropyRLGR1, rfxEntropyRLGR3:
				d.entropy = entropy
			default:
				return fmt.Errorf("rfx: unknown entropy algorithm 0x%X", entropy)
			}
		case rfxBlockChannels:
			// numChannels(1) + channels{channelId(1), width(2), height(2)}
			if len(body) >= 6 {
				d.width = int(binary.LittleEndian.Uint16(body[2:]))
				d.height = int(binary.LittleEndian.Uint16(body[4:]))
			}
		case rfxBlockRegion:
			parsed, err := parseRemoteFXRegion(body)
			if err != nil {
				return err
			}
			rects = parsed
			if len(rects) == 0 && d.width > 0 && d.height > 0 {
				// A region without rectangles encodes the whole channel.
				rects = []RemoteFXRect{{Width: d.width, Height: d.height}}
			}
		case rfxBlockTileset:
			if d.entropy == 0 {
				return fmt.Errorf("rfx: tileset before a context block")
			}
			if err := d.decodeTileset(body, rects, emit); err != nil {
				return err
			}
		case rfxBlockFrameBeg:
			rects = nil
		}
	}
	return nil
}

// parseRemoteFXRegion reads a REGION block body (which starts with the codec
// and channel ids): regionFlags(1) + numRects(2) + rects(8 each) +
// regionType(2) + numTilesets(2).
func parseRemoteFXRegion(body []byte) ([]RemoteFXRect, error) {
	if len(body) < 5 {
		return nil, fmt.Errorf("rfx: region block too short (%d bytes)", len(body))
	}
	numRects := int(binary.LittleEndian.Uint16(body[3:5]))
	need := 5 + numRects*8 + 4
	if need > len(body) {
		return nil, fmt.Errorf("rfx: region names %d rectangles but holds %d bytes", numRects, len(body))
	}
	rects := make([]RemoteFXRect, numRects)
	for i := range rects {
		at := 5 + i*8
		rects[i] = RemoteFXRect{
			X:      int(binary.LittleEndian.Uint16(body[at:])),
			Y:      int(binary.LittleEndian.Uint16(body[at+2:])),
			Width:  int(binary.LittleEndian.Uint16(body[at+4:])),
			Height: int(binary.LittleEndian.Uint16(body[at+6:])),
		}
	}
	if regionType := binary.LittleEndian.Uint16(body[5+numRects*8:]); regionType != rfxRegionType {
		return nil, fmt.Errorf("rfx: region type 0x%04X is not CBT_REGION", regionType)
	}
	return rects, nil
}

// decodeTileset decodes a TILESET block body (which starts with the codec and
// channel ids): subtype(2) + idx(2) + properties(2) + numQuant(1) +
// tileSize(1) + numTiles(2) + tilesDataSize(4) + quantVals(5 each) + tiles.
func (d *RemoteFXDecoder) decodeTileset(body []byte, rects []RemoteFXRect, emit func(x, y, w, h int, pixels []byte, stride int)) error {
	if len(body) < 16 {
		return fmt.Errorf("rfx: tileset block too short (%d bytes)", len(body))
	}
	if subtype := binary.LittleEndian.Uint16(body[2:]); subtype != rfxSubtypeTileset {
		return fmt.Errorf("rfx: tileset subtype 0x%04X is not CBT_TILESET", subtype)
	}
	numQuant := int(body[8])
	if numQuant < 1 || numQuant > 15 {
		return fmt.Errorf("rfx: tileset names %d quantization tables", numQuant)
	}
	numTiles := int(binary.LittleEndian.Uint16(body[10:12]))
	if numTiles == 0 {
		// Windows Server 2012 (not R2) can send an empty tile set.
		return nil
	}
	tilesDataSize := int(binary.LittleEndian.Uint32(body[12:16]))
	quantAt := 16
	if quantAt+numQuant*5 > len(body) {
		return fmt.Errorf("rfx: tileset truncates its %d quantization tables", numQuant)
	}
	quants := make([][10]uint8, numQuant)
	for i := range quants {
		q := body[quantAt+i*5 : quantAt+i*5+5]
		quants[i] = [10]uint8{
			q[0] & 0x0F, q[0] >> 4,
			q[1] & 0x0F, q[1] >> 4,
			q[2] & 0x0F, q[2] >> 4,
			q[3] & 0x0F, q[3] >> 4,
			q[4] & 0x0F, q[4] >> 4,
		}
	}
	off := quantAt + numQuant*5
	end := min(off+tilesDataSize, len(body))
	for i := 0; i < numTiles; i++ {
		if off+6 > end {
			return fmt.Errorf("rfx: tileset truncates at tile %d of %d", i, numTiles)
		}
		blockType := binary.LittleEndian.Uint16(body[off:])
		blockLen := int(binary.LittleEndian.Uint32(body[off+2:]))
		if blockType != rfxBlockTile {
			return fmt.Errorf("rfx: tile %d has block type 0x%04X", i, blockType)
		}
		if blockLen < 6+13 || off+blockLen > end {
			return fmt.Errorf("rfx: tile %d has length %d", i, blockLen)
		}
		if err := d.decodeTile(body[off:off+blockLen], quants, rects, emit); err != nil {
			return err
		}
		off += blockLen
	}
	return nil
}

// decodeTile decodes one TS_RFX_TILE block (header included) and emits its
// valid rectangles.
func (d *RemoteFXDecoder) decodeTile(block []byte, quants [][10]uint8, rects []RemoteFXRect, emit func(x, y, w, h int, pixels []byte, stride int)) error {
	tile := block[6:]
	quantIdxY := int(tile[0])
	quantIdxCb := int(tile[1])
	quantIdxCr := int(tile[2])
	xIdx := int(binary.LittleEndian.Uint16(tile[3:]))
	yIdx := int(binary.LittleEndian.Uint16(tile[5:]))
	yLen := int(binary.LittleEndian.Uint16(tile[7:]))
	cbLen := int(binary.LittleEndian.Uint16(tile[9:]))
	crLen := int(binary.LittleEndian.Uint16(tile[11:]))
	if quantIdxY >= len(quants) || quantIdxCb >= len(quants) || quantIdxCr >= len(quants) {
		return fmt.Errorf("rfx: tile names quantization table %d/%d/%d of %d", quantIdxY, quantIdxCb, quantIdxCr, len(quants))
	}
	data := tile[13:]
	if 13+yLen+cbLen+crLen > len(tile) {
		return fmt.Errorf("rfx: tile %d,%d truncates its components", xIdx, yIdx)
	}
	yData := data[:yLen]
	cbData := data[yLen : yLen+cbLen]
	crData := data[yLen+cbLen : yLen+cbLen+crLen]

	d.decodeComponent(yData, quants[quantIdxY], d.work[0])
	d.decodeComponent(cbData, quants[quantIdxCb], d.work[1])
	d.decodeComponent(crData, quants[quantIdxCr], d.work[2])
	inverseDWT(d.work[0], d.dwt)
	inverseDWT(d.work[1], d.dwt)
	inverseDWT(d.work[2], d.dwt)
	ycbcrToRGBA(d.rgba, d.work[0], d.work[1], d.work[2])

	left, top := xIdx*64, yIdx*64
	for _, r := range rects {
		x1 := max(left, r.X)
		y1 := max(top, r.Y)
		x2 := min(left+64, r.X+r.Width)
		y2 := min(top+64, r.Y+r.Height)
		if x1 >= x2 || y1 >= y2 {
			continue
		}
		srcOff := (y1-top)*64*4 + (x1-left)*4
		emit(x1, y1, x2-x1, y2-y1, d.rgba[srcOff:], 64*4)
	}
	return nil
}

// decodeComponent turns one encoded tile component into 4096 coefficients:
// entropy decode, differential decode of LL3, dequantization and the inverse
// transform (MS-RDPRFX 3.1.8.1).
func (d *RemoteFXDecoder) decodeComponent(data []byte, quant [10]uint8, coeffs []int16) {
	if d.entropy == rfxEntropyRLGR3 {
		rlgr3Decode(coeffs, data)
	} else {
		rlgr1Decode(coeffs, data)
	}
	differentialDecode(coeffs[offLL3:offLL3+64], 64)
	for i, band := range remoteFXQuantBands {
		shift := int(quant[i]) - 1
		if shift <= 0 {
			continue
		}
		for j := band.offset; j < band.offset+band.count; j++ {
			coeffs[j] <<= shift
		}
	}
}
