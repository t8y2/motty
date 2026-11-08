package rfx

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"runtime"
	"sync"

	sloghex "motty/internal/rdpcore/util"
)

// tilePixelPool pools 64x64 RGBA pixel buffers (16384 bytes each) for decoded tiles.
// Tiles are returned to the pool after EGFX blits them to the surface.
var tilePixelPool = sync.Pool{
	New: func() any {
		b := make([]byte, tileSize*tileSize*4)
		return &b
	},
}

// GetTileBuffer returns a 64x64 RGBA pixel buffer from the pool.
func GetTileBuffer() []byte { return *tilePixelPool.Get().(*[]byte) }

// ReleaseTileBuffer returns a tile pixel buffer to the pool.
func ReleaseTileBuffer(b []byte) {
	if cap(b) >= tileSize*tileSize*4 {
		b = b[:tileSize*tileSize*4]
		tilePixelPool.Put(&b)
	}
}

// levelTrace is a custom slog level for verbose per-tile diagnostics.
const levelTrace = slog.LevelDebug - 4

// Block type constants (MS-RDPEGFX 2.2.4.2).
const (
	blockSync       = 0xCCC0
	blockCtx        = 0xCCC1
	blockFrameBegin = 0xCCC2
	blockFrameEnd   = 0xCCC3
	blockRegion     = 0xCCC4
	blockTileSimple = 0xCCC5
	blockTileFirst  = 0xCCC6
	blockTileUpgr   = 0xCCC7
)

// Region represents a decoded progressive region with destination rects and tiles.
type Region struct {
	Rects [][4]uint16 // left, top, right, bottom (converted from x,y,width,height)
	Tiles []DecodedTile
}

// DecodedTile is a fully decoded tile with RGBA pixel data.
type DecodedTile struct {
	X, Y int    // pixel position (tileIdx * 64)
	Data []byte // 64x64 RGBA pixels (top-down, 64*64*4 = 16384 bytes)
}

// tileWorkBuf holds pre-allocated scratch buffers for tile decoding.
// Tiles are decoded sequentially, so a single set is safe.
type tileWorkBuf struct {
	coeffs [3][]int16 // 3x 4096 for RLGR output (Y, Cb, Cr)
	sign   [3][]int16 // 3x 4096 for sign buffers
	dwtTmp []int16    // 4096 for DWT temporary
}

// tileWorker holds per-worker scratch buffers for parallel tile decoding.
type tileWorker struct {
	work tileWorkBuf
}

// tileJob describes a single tile to decode in the parallel pipeline.
type tileJob struct {
	kind        uint16 // blockTileSimple/First/Upgr
	body        []byte
	quants      [][10]uint8
	progQuants  []progQuantEntry
	extrapolate bool
	idx         int         // position in results slice
	tc          *tileCoeffs // pre-allocated (simple/first) or existing (upgrade)
}

// Decoder handles RemoteFX Progressive codec decoding.
type Decoder struct {
	log      *slog.Logger
	contexts map[uint32]*contextState
	work     tileWorkBuf   // sequential fallback
	pool     []*tileWorker // parallel workers
}

type contextState struct {
	tiles map[[2]int]*tileCoeffs // [xIdx, yIdx] → stored coefficients
}

type tileCoeffs struct {
	y           []int16   // 4096 Y coefficients (post-dequantize, pre-DWT)
	cb          []int16   // 4096 Cb coefficients
	cr          []int16   // 4096 Cr coefficients
	ySign       []int16   // 4096 sign values (+1, -1, or 0)
	cbSign      []int16   // 4096 sign values
	crSign      []int16   // 4096 sign values
	yBitPos     [10]uint8 // per-subband bit position
	cbBitPos    [10]uint8
	crBitPos    [10]uint8
	extrapolate bool   // which DWT path was used
	pixels      []byte // cached 64x64 RGBA output (16384 bytes), per MS-RDPEGFX 3.3.3.3
}

// progQuantEntry holds a progressive quantization table entry.
// Each entry has a quality byte and per-channel quant values.
type progQuantEntry struct {
	quality uint8
	y       [10]uint8
	cb      [10]uint8
	cr      [10]uint8
}

// Subband layout for dequantization.
type subbandInfo struct {
	start, count int
	quantIdx     int
}

// Standard (non-extrapolate) subband layout.
var standardSubbands = [10]subbandInfo{
	{offHL1, 1024, 7}, {offLH1, 1024, 8}, {offHH1, 1024, 9},
	{offHL2, 256, 4}, {offLH2, 256, 5}, {offHH2, 256, 6},
	{offHL3, 64, 1}, {offLH3, 64, 2}, {offHH3, 64, 3},
	{offLL3, 64, 0},
}

// Extrapolate subband layout (progressive codec).
var extrapolateSubbands = [10]subbandInfo{
	{0, 1023, 7}, {1023, 1023, 8}, {2046, 961, 9},
	{3007, 272, 4}, {3279, 272, 5}, {3551, 256, 6},
	{3807, 72, 1}, {3879, 72, 2}, {3951, 64, 3},
	{4015, 81, 0},
}

// initWorkBuf allocates scratch buffers for tile decoding.
// Single allocation for all buffers: 3×4096 (coeffs) + 3×4096 (sign) + 3×4096 (dwtTmp) = 36864.
// dwtTmp is 3×4096 to hold per-component L/H during fused level 1 DWT+convert.
func initWorkBuf(w *tileWorkBuf) {
	buf := make([]int16, 9*4096)
	for i := range w.coeffs {
		w.coeffs[i] = buf[i*4096 : (i+1)*4096]
	}
	for i := range w.sign {
		w.sign[i] = buf[(3+i)*4096 : (4+i)*4096]
	}
	w.dwtTmp = buf[6*4096 : 9*4096]
}

// NewDecoder creates a new Progressive codec decoder.
func NewDecoder(log *slog.Logger) *Decoder {
	n := runtime.NumCPU()
	if n > 16 {
		n = 16
	}
	d := &Decoder{
		log:      log,
		contexts: make(map[uint32]*contextState),
	}
	initWorkBuf(&d.work)
	for i := 0; i < n; i++ {
		w := &tileWorker{}
		initWorkBuf(&w.work)
		d.pool = append(d.pool, w)
	}
	return d
}

// ClearContext removes all stored tile state for a given context ID.
// Called when encoding context is deleted (DeleteEncodingCtx) or surface is reset.
func (d *Decoder) ClearContext(id uint32) {
	delete(d.contexts, id)
}

// ClearAllContexts removes all stored tile state across all contexts.
// A fresh graphics channel starts with no progressive state. ResetGraphics does
// not: Windows continues with UPGRADE tiles for a recreated surface without
// repeating SYNC and CONTEXT.
func (d *Decoder) ClearAllContexts() {
	clear(d.contexts)
}

// GetCachedTiles returns cached pixel data for all progressive tiles overlapping
// the given pixel rectangle. The returned DecodedTile.Data slices point to internal
// cache buffers — callers must NOT modify or release them.
func (d *Decoder) GetCachedTiles(contextID uint32, left, top, right, bottom int) []DecodedTile {
	ctx := d.contexts[contextID]
	if ctx == nil {
		return nil
	}
	tileLeft := left / 64
	tileTop := top / 64
	tileRight := (right + 63) / 64
	tileBottom := (bottom + 63) / 64

	var result []DecodedTile
	for yIdx := tileTop; yIdx < tileBottom; yIdx++ {
		for xIdx := tileLeft; xIdx < tileRight; xIdx++ {
			tc := ctx.tiles[[2]int{xIdx, yIdx}]
			if tc != nil && tc.pixels != nil {
				result = append(result, DecodedTile{
					X:    xIdx * 64,
					Y:    yIdx * 64,
					Data: tc.pixels,
				})
			}
		}
	}
	return result
}

// Decode parses RFX Progressive codec data and returns decoded regions.
func (d *Decoder) Decode(data []byte, contextID uint32) ([]Region, error) {
	if len(data) < 6 {
		return nil, fmt.Errorf("rfx: data too short (%d bytes)", len(data))
	}

	var regions []Region
	off := 0

	for off+6 <= len(data) {
		blockType := binary.LittleEndian.Uint16(data[off:])
		blockLen := int(binary.LittleEndian.Uint32(data[off+2:]))
		if blockLen < 6 || off+blockLen > len(data) {
			break
		}

		blockBody := data[off+6 : off+blockLen]

		switch blockType {
		case blockSync:
			if len(blockBody) >= 8 {
				magic := binary.LittleEndian.Uint32(blockBody[0:4])
				if magic != 0xCACCACCA {
					return nil, fmt.Errorf("rfx: bad sync magic 0x%08X", magic)
				}
			}

		case blockCtx:
			if _, ok := d.contexts[contextID]; !ok {
				d.contexts[contextID] = &contextState{
					tiles: make(map[[2]int]*tileCoeffs),
				}
			}

		case blockFrameBegin:
			// frameIndex(4) + regionCount(2) — skip

		case blockFrameEnd:
			// empty

		case blockRegion:
			region, err := d.decodeRegion(blockBody, contextID)
			if err != nil {
				d.log.LogAttrs(context.Background(), slog.LevelError, "region decode error", slog.Any("err", err))
			} else {
				regions = append(regions, region)
			}
		}

		off += blockLen
	}

	return regions, nil
}

// decodeRegion parses an RFX_PROGRESSIVE_REGION block.
func (d *Decoder) decodeRegion(data []byte, contextID uint32) (Region, error) {
	var region Region

	// tileSize(1) + numRects(2) + numQuants(1) + numProgQuants(1) + flags(1) + numTiles(2) + tileDataSize(4) = 12
	if len(data) < 12 {
		return region, fmt.Errorf("region header too short")
	}

	// tileSize := data[0]
	numRects := int(binary.LittleEndian.Uint16(data[1:3]))
	numQuants := int(data[3])
	numProgQuants := int(data[4])
	flags := data[5]
	extrapolate := flags&0x01 != 0
	numTiles := int(binary.LittleEndian.Uint16(data[6:8]))
	// tileDataSize := binary.LittleEndian.Uint32(data[8:12])

	off := 12

	// Read destination rects: TS_RFX_RECT = {x(2), y(2), width(2), height(2)}.
	// Convert to {left, top, right, bottom} for clipping.
	region.Rects = make([][4]uint16, numRects)
	for i := 0; i < numRects; i++ {
		if off+8 > len(data) {
			return region, fmt.Errorf("truncated rects")
		}
		x := binary.LittleEndian.Uint16(data[off:])
		y := binary.LittleEndian.Uint16(data[off+2:])
		w := binary.LittleEndian.Uint16(data[off+4:])
		h := binary.LittleEndian.Uint16(data[off+6:])
		region.Rects[i] = [4]uint16{x, y, x + w, y + h}
		off += 8
	}

	// Read quantization tables: 5 bytes each = 10 nibbles for 10 subbands
	// Nibble order per MS-RDPRFX 2.2.2.1.5:
	// Byte 0: LL3(low), HL3(high) | Byte 1: LH3(low), HH3(high)
	// Byte 2: HL2(low), LH2(high) | Byte 3: HH2(low), HL1(high)
	// Byte 4: LH1(low), HH1(high)
	quants := make([][10]uint8, numQuants)
	for i := 0; i < numQuants; i++ {
		if off+5 > len(data) {
			break
		}
		quants[i] = parseQuantValues(data[off:])
		off += 5
	}

	// Read progressive quantization tables: 16 bytes each
	// quality(1) + yQuant(5) + cbQuant(5) + crQuant(5)
	progQuants := make([]progQuantEntry, numProgQuants)
	for i := 0; i < numProgQuants; i++ {
		if off+16 > len(data) {
			break
		}
		progQuants[i].quality = data[off]
		progQuants[i].y = parseQuantValues(data[off+1:])
		progQuants[i].cb = parseQuantValues(data[off+6:])
		progQuants[i].cr = parseQuantValues(data[off+11:])
		off += 16
	}

	if off > len(data) {
		return region, fmt.Errorf("truncated quants")
	}

	// Ensure context exists
	ctx := d.contexts[contextID]
	if ctx == nil {
		ctx = &contextState{tiles: make(map[[2]int]*tileCoeffs)}
		d.contexts[contextID] = ctx
	}

	// ── Phase 1: Parse tile headers and pre-allocate state (sequential) ──

	type parsedTile struct {
		tileType uint16
		body     []byte
	}
	parsed := make([]parsedTile, 0, numTiles)
	for i := 0; i < numTiles; i++ {
		if off+6 > len(data) {
			break
		}
		tileType := binary.LittleEndian.Uint16(data[off:])
		tileBlockLen := int(binary.LittleEndian.Uint32(data[off+2:]))
		if tileBlockLen < 6 || off+tileBlockLen > len(data) {
			break
		}
		parsed = append(parsed, parsedTile{tileType, data[off+6 : off+tileBlockLen]})
		off += tileBlockLen
	}

	// Build jobs, pre-allocating map entries so workers never mutate the map.
	jobs := make([]tileJob, 0, len(parsed))
	results := make([]DecodedTile, len(parsed))
	ok := make([]bool, len(parsed))
	var nSimple, nFirst, nUpgr int

	for i, pt := range parsed {
		if len(pt.body) < 7 {
			continue // can't read xIdx/yIdx
		}
		key := [2]int{
			int(binary.LittleEndian.Uint16(pt.body[3:5])),
			int(binary.LittleEndian.Uint16(pt.body[5:7])),
		}

		switch pt.tileType {
		case blockTileSimple:
			nSimple++
			tc := d.ensureTileCoeffs(ctx, key)
			jobs = append(jobs, tileJob{
				kind: pt.tileType, body: pt.body, quants: quants,
				progQuants: progQuants, extrapolate: extrapolate,
				idx: i, tc: tc,
			})
		case blockTileFirst:
			nFirst++
			tc := d.ensureTileCoeffs(ctx, key)
			jobs = append(jobs, tileJob{
				kind: pt.tileType, body: pt.body, quants: quants,
				progQuants: progQuants, extrapolate: extrapolate,
				idx: i, tc: tc,
			})
		case blockTileUpgr:
			nUpgr++
			existing := ctx.tiles[key]
			if existing == nil {
				d.log.LogAttrs(context.Background(), slog.LevelWarn, "upgrade tile has no base",
					slog.Int("xIdx", key[0]), slog.Int("yIdx", key[1]))
				continue
			}
			jobs = append(jobs, tileJob{
				kind: pt.tileType, body: pt.body, quants: quants,
				progQuants: progQuants, extrapolate: extrapolate,
				idx: i, tc: existing,
			})
		default:
			d.log.LogAttrs(context.Background(), slog.LevelWarn, "unknown tile type",
				sloghex.Hex4("type", pt.tileType))
		}
	}

	// ── Phase 2: Decode tiles (parallel for ≥4 tiles, sequential otherwise) ──

	if len(jobs) < 4 {
		for _, job := range jobs {
			dt, dok := d.decodeTileJob(&d.work, job)
			results[job.idx] = dt
			ok[job.idx] = dok
		}
	} else {
		var wg sync.WaitGroup
		nWorkers := min(len(d.pool), len(jobs))
		for w := 0; w < nWorkers; w++ {
			wg.Add(1)
			go func(worker *tileWorker, start int) {
				defer wg.Done()
				for i := start; i < len(jobs); i += nWorkers {
					dt, dok := d.decodeTileJob(&worker.work, jobs[i])
					results[jobs[i].idx] = dt
					ok[jobs[i].idx] = dok
				}
			}(d.pool[w], w)
		}
		wg.Wait()
	}

	// ── Phase 3: Collect results ──

	nFail := 0
	region.Tiles = make([]DecodedTile, 0, len(jobs))
	for _, job := range jobs {
		if ok[job.idx] {
			region.Tiles = append(region.Tiles, results[job.idx])
		} else {
			nFail++
		}
	}

	d.log.LogAttrs(context.Background(), slog.LevelDebug, "region decoded",
		slog.Int("ctx", int(contextID)), slog.Bool("extrapolate", extrapolate),
		slog.Int("rects", numRects), slog.Int("quants", numQuants), slog.Int("progQuants", numProgQuants),
		slog.Int("simple", nSimple), slog.Int("first", nFirst), slog.Int("upgr", nUpgr), slog.Int("fail", nFail))
	for ri, r := range region.Rects {
		d.log.LogAttrs(context.Background(), slog.LevelDebug, "region rect",
			slog.Int("ri", ri), slog.Int("left", int(r[0])), slog.Int("top", int(r[1])),
			slog.Int("right", int(r[2])), slog.Int("bottom", int(r[3])))
	}

	return region, nil
}

// ensureTileCoeffs returns the existing tileCoeffs for key, or allocates a new one.
func (d *Decoder) ensureTileCoeffs(ctx *contextState, key [2]int) *tileCoeffs {
	tc := ctx.tiles[key]
	if tc == nil {
		buf := make([]int16, 6*4096)
		tc = &tileCoeffs{
			y:      buf[0:4096],
			cb:     buf[4096:8192],
			cr:     buf[8192:12288],
			ySign:  buf[12288:16384],
			cbSign: buf[16384:20480],
			crSign: buf[20480:24576],
		}
		ctx.tiles[key] = tc
	}
	return tc
}

// decodeTileJob dispatches a tile job to the appropriate decode method.
func (d *Decoder) decodeTileJob(w *tileWorkBuf, job tileJob) (DecodedTile, bool) {
	switch job.kind {
	case blockTileSimple:
		return d.decodeTileSimple(w, job.tc, job.body, job.quants, job.progQuants, job.extrapolate)
	case blockTileFirst:
		return d.decodeTileFirst(w, job.tc, job.body, job.quants, job.progQuants, job.extrapolate)
	case blockTileUpgr:
		return d.decodeTileUpgrade(w, job.tc, job.body, job.quants, job.progQuants, job.extrapolate)
	}
	return DecodedTile{}, false
}

// parseQuantValues parses 5 bytes of packed nibble quant values into 10 uint8s.
// Parses packed nibble quant values per MS-RDPRFX 2.2.2.1.5.
func parseQuantValues(data []byte) [10]uint8 {
	return [10]uint8{
		data[0] & 0x0F, // [0] LL3
		data[0] >> 4,   // [1] HL3
		data[1] & 0x0F, // [2] LH3
		data[1] >> 4,   // [3] HH3
		data[2] & 0x0F, // [4] HL2
		data[2] >> 4,   // [5] LH2
		data[3] & 0x0F, // [6] HH2
		data[3] >> 4,   // [7] HL1
		data[4] & 0x0F, // [8] LH1
		data[4] >> 4,   // [9] HH1
	}
}

// computeShift computes the per-subband dequantization shift: quant + progQuant - 1.
// Computes per-subband dequantization shift: quant + progQuant - 1.
func computeShift(quant, progQuant [10]uint8) [10]uint8 {
	var shift [10]uint8
	for i := range shift {
		v := int(quant[i]) + int(progQuant[i]) - 1
		if v < 0 {
			v = 0
		}
		shift[i] = uint8(v)
	}
	return shift
}

// getProgQuant returns the progressive quant values for a given quality.
// quality=0xFF → all zeros (full quality, no progressive reduction).
func getProgQuant(progQuants []progQuantEntry, quality uint8) (y, cb, cr [10]uint8) {
	if quality == 0xFF {
		return // all zeros
	}
	if int(quality) < len(progQuants) {
		pq := progQuants[quality]
		return pq.y, pq.cb, pq.cr
	}
	return // all zeros as fallback
}

// decodeTileSimple decodes an RFX_PROGRESSIVE_TILE_SIMPLE.
func (d *Decoder) decodeTileSimple(w *tileWorkBuf, tc *tileCoeffs, data []byte, quants [][10]uint8,
	progQuants []progQuantEntry, extrapolate bool) (DecodedTile, bool) {
	// quantIdxY(1) + quantIdxCb(1) + quantIdxCr(1) + xIdx(2) + yIdx(2) + flags(1)
	// + yLen(2) + cbLen(2) + crLen(2) + tailLen(2) = 16
	if len(data) < 16 {
		return DecodedTile{}, false
	}

	quantIdxY := int(data[0])
	quantIdxCb := int(data[1])
	quantIdxCr := int(data[2])
	xIdx := int(binary.LittleEndian.Uint16(data[3:5]))
	yIdx := int(binary.LittleEndian.Uint16(data[5:7]))
	// flags := data[7]
	yLen := int(binary.LittleEndian.Uint16(data[8:10]))
	cbLen := int(binary.LittleEndian.Uint16(data[10:12]))
	crLen := int(binary.LittleEndian.Uint16(data[12:14]))
	// tailLen := int(binary.LittleEndian.Uint16(data[14:16]))

	off := 16
	yData := safeSlice(data, off, yLen)
	off += yLen
	cbData := safeSlice(data, off, cbLen)
	off += cbLen
	crData := safeSlice(data, off, crLen)

	qY := getQuant(quants, quantIdxY)
	qCb := getQuant(quants, quantIdxCb)
	qCr := getQuant(quants, quantIdxCr)

	// TILE_SIMPLE: quality=0xFF → progQuant all zeros → shift = quant - 1
	pqY, pqCb, pqCr := getProgQuant(progQuants, 0xFF)
	shiftY := computeShift(qY, pqY)
	shiftCb := computeShift(qCb, pqCb)
	shiftCr := computeShift(qCr, pqCr)

	return d.decodeTileData(w, tc, yData, cbData, crData, xIdx, yIdx,
		shiftY, shiftCb, shiftCr, extrapolate, false)
}

// decodeTileFirst decodes an RFX_PROGRESSIVE_TILE_FIRST (first progressive pass).
func (d *Decoder) decodeTileFirst(w *tileWorkBuf, tc *tileCoeffs, data []byte, quants [][10]uint8,
	progQuants []progQuantEntry, extrapolate bool) (DecodedTile, bool) {
	// quantIdxY(1) + quantIdxCb(1) + quantIdxCr(1) + xIdx(2) + yIdx(2) + flags(1)
	// + quality(1) + yLen(2) + cbLen(2) + crLen(2) + tailLen(2) = 17
	if len(data) < 17 {
		return DecodedTile{}, false
	}

	quantIdxY := int(data[0])
	quantIdxCb := int(data[1])
	quantIdxCr := int(data[2])
	xIdx := int(binary.LittleEndian.Uint16(data[3:5]))
	yIdx := int(binary.LittleEndian.Uint16(data[5:7]))
	flags := data[7]
	quality := data[8]
	yLen := int(binary.LittleEndian.Uint16(data[9:11]))
	cbLen := int(binary.LittleEndian.Uint16(data[11:13]))
	crLen := int(binary.LittleEndian.Uint16(data[13:15]))
	// tailLen := int(binary.LittleEndian.Uint16(data[15:17]))

	off := 17
	yData := safeSlice(data, off, yLen)
	off += yLen
	cbData := safeSlice(data, off, cbLen)
	off += cbLen
	crData := safeSlice(data, off, crLen)

	qY := getQuant(quants, quantIdxY)
	qCb := getQuant(quants, quantIdxCb)
	qCr := getQuant(quants, quantIdxCr)

	pqY, pqCb, pqCr := getProgQuant(progQuants, quality)
	shiftY := computeShift(qY, pqY)
	shiftCb := computeShift(qCb, pqCb)
	shiftCr := computeShift(qCr, pqCr)

	if d.log.Enabled(context.Background(), levelTrace) {
		d.log.Log(context.Background(), levelTrace, "FIRST tile",
			"xIdx", xIdx, "yIdx", yIdx,
			"flags", fmt.Sprintf("0x%02X", flags), "quality", quality,
			"qY", qY, "shiftY", shiftY,
			"yLen", yLen, "cbLen", cbLen, "crLen", crLen)
	}

	coeffDiff := flags&0x01 != 0
	return d.decodeTileData(w, tc, yData, cbData, crData, xIdx, yIdx,
		shiftY, shiftCb, shiftCr, extrapolate, coeffDiff)
}

// decodeTileData is the common path for TILE_SIMPLE and TILE_FIRST.
// shift values are pre-computed: shift = quant + progQuant - 1.
// coeffDiff: when true, add new coefficients to existing tile state (RFX_TILE_DIFFERENCE).
// tc is the pre-allocated tileCoeffs entry (from Phase 1).
func (d *Decoder) decodeTileData(w *tileWorkBuf, tc *tileCoeffs, yData, cbData, crData []byte, xIdx, yIdx int,
	shiftY, shiftCb, shiftCr [10]uint8, extrapolate, coeffDiff bool) (DecodedTile, bool) {

	// Two paths depending on coeffDiff:
	// Non-coeffDiff: decode RLGR directly into tc.*, signs into tc.*Sign,
	//   dequantize in-place, then copy tc→w.coeffs for DWT (DWT destroys input).
	// CoeffDiff: decode into w.coeffs (need work buffer to accumulate),
	//   then copy to tc.
	var yCoeffs, cbCoeffs, crCoeffs []int16
	if !coeffDiff {
		// Decode directly into tileCoeffs storage
		yCoeffs = rlgr1Decode(tc.y, yData)
		cbCoeffs = rlgr1Decode(tc.cb, cbData)
		crCoeffs = rlgr1Decode(tc.cr, crData)

		// Diagnostic: raw RLGR output before any processing
		if extrapolate && d.log.Enabled(context.Background(), levelTrace) {
			d.log.Log(context.Background(), levelTrace, "RLGR tile coefficients",
				"xIdx", xIdx, "yIdx", yIdx,
				"Y_LL3[4015:4021]", fmt.Sprint(yCoeffs[4015:4021]),
				"Y_HL1[0:5]", fmt.Sprint(yCoeffs[0:5]),
				"Y_HH1[2046:2051]", fmt.Sprint(yCoeffs[2046:2051]),
				"yHex", fmt.Sprintf("%X", yData[:min(20, len(yData))]))
			var nzCount int
			var firstNZ int = -1
			for i, v := range yCoeffs {
				if v != 0 {
					nzCount++
					if firstNZ < 0 {
						firstNZ = i
					}
				}
			}
			d.log.Log(context.Background(), levelTrace, "RLGR tile stats",
				"xIdx", xIdx, "yIdx", yIdx, "nonZero", nzCount, "firstNZ", firstNZ)
		}

		// Signs directly into tc sign buffers (no intermediate copy)
		fillSignBuffer(tc.ySign, yCoeffs)
		fillSignBuffer(tc.cbSign, cbCoeffs)
		fillSignBuffer(tc.crSign, crCoeffs)

		if extrapolate {
			dequantizeShift(yCoeffs, shiftY, true)
			dequantizeShift(cbCoeffs, shiftCb, true)
			dequantizeShift(crCoeffs, shiftCr, true)
			if d.log.Enabled(context.Background(), levelTrace) {
				d.log.Log(context.Background(), levelTrace, "DEQUANT tile",
					"xIdx", xIdx, "yIdx", yIdx,
					"Y_LL3[4015:4021]", fmt.Sprint(yCoeffs[4015:4021]),
					"Y_HL1[0:5]", fmt.Sprint(yCoeffs[0:5]))
			}
		} else {
			differentialDecode(yCoeffs[offLL3:offLL3+64], 64)
			differentialDecode(cbCoeffs[offLL3:offLL3+64], 64)
			differentialDecode(crCoeffs[offLL3:offLL3+64], 64)

			dequantizeShift(yCoeffs, shiftY, false)
			dequantizeShift(cbCoeffs, shiftCb, false)
			dequantizeShift(crCoeffs, shiftCr, false)
		}

		// Copy tc→w.coeffs for DWT (DWT destroys input)
		copy(w.coeffs[0], tc.y)
		copy(w.coeffs[1], tc.cb)
		copy(w.coeffs[2], tc.cr)
		yCoeffs = w.coeffs[0]
		cbCoeffs = w.coeffs[1]
		crCoeffs = w.coeffs[2]
	} else {
		// coeffDiff path: decode into work buffers, accumulate into tc
		yCoeffs = rlgr1Decode(w.coeffs[0], yData)
		cbCoeffs = rlgr1Decode(w.coeffs[1], cbData)
		crCoeffs = rlgr1Decode(w.coeffs[2], crData)

		// Diagnostic: raw RLGR output before any processing
		if extrapolate && d.log.Enabled(context.Background(), levelTrace) {
			d.log.Log(context.Background(), levelTrace, "RLGR tile coefficients",
				"xIdx", xIdx, "yIdx", yIdx,
				"Y_LL3[4015:4021]", fmt.Sprint(yCoeffs[4015:4021]),
				"Y_HL1[0:5]", fmt.Sprint(yCoeffs[0:5]),
				"Y_HH1[2046:2051]", fmt.Sprint(yCoeffs[2046:2051]),
				"yHex", fmt.Sprintf("%X", yData[:min(20, len(yData))]))
			var nzCount int
			var firstNZ int = -1
			for i, v := range yCoeffs {
				if v != 0 {
					nzCount++
					if firstNZ < 0 {
						firstNZ = i
					}
				}
			}
			d.log.Log(context.Background(), levelTrace, "RLGR tile stats",
				"xIdx", xIdx, "yIdx", yIdx, "nonZero", nzCount, "firstNZ", firstNZ)
		}

		// Capture signs into work sign buffers
		fillSignBuffer(w.sign[0], yCoeffs)
		fillSignBuffer(w.sign[1], cbCoeffs)
		fillSignBuffer(w.sign[2], crCoeffs)

		if extrapolate {
			dequantizeShift(yCoeffs, shiftY, true)
			dequantizeShift(cbCoeffs, shiftCb, true)
			dequantizeShift(crCoeffs, shiftCr, true)
			if d.log.Enabled(context.Background(), levelTrace) {
				d.log.Log(context.Background(), levelTrace, "DEQUANT tile",
					"xIdx", xIdx, "yIdx", yIdx,
					"Y_LL3[4015:4021]", fmt.Sprint(yCoeffs[4015:4021]),
					"Y_HL1[0:5]", fmt.Sprint(yCoeffs[0:5]))
			}
		} else {
			differentialDecode(yCoeffs[offLL3:offLL3+64], 64)
			differentialDecode(cbCoeffs[offLL3:offLL3+64], 64)
			differentialDecode(crCoeffs[offLL3:offLL3+64], 64)

			dequantizeShift(yCoeffs, shiftY, false)
			dequantizeShift(cbCoeffs, shiftCb, false)
			dequantizeShift(crCoeffs, shiftCr, false)
		}

		// Accumulate into existing tile state
		for i := range yCoeffs {
			yCoeffs[i] += tc.y[i]
			cbCoeffs[i] += tc.cb[i]
			crCoeffs[i] += tc.cr[i]
		}
		d.log.LogAttrs(context.Background(), slog.LevelDebug, "coeffDiff accumulated", slog.Int("xIdx", int(xIdx)), slog.Int("yIdx", int(yIdx)))

		// Store accumulated coefficients. Signs come from the raw RLGR delta
		// (captured into w.sign before dequantize/accumulate per MS-RDPEGFX 2.2.4.2.1.5.1).
		// The stored pre-accumulation values are used as delta signs.
		// The server encodes UPGRADE bitstreams using these delta signs.
		copy(tc.y, yCoeffs)
		copy(tc.cb, cbCoeffs)
		copy(tc.cr, crCoeffs)
		copy(tc.ySign, w.sign[0])
		copy(tc.cbSign, w.sign[1])
		copy(tc.crSign, w.sign[2])
	}

	// Store bitPos and extrapolate flag
	tc.yBitPos = shiftY
	tc.cbBitPos = shiftCb
	tc.crBitPos = shiftCr
	tc.extrapolate = extrapolate
	// Fix bitPos: we need quant + progQuant, not quant + progQuant - 1.
	// shiftX = quant + progQuant - 1, so bitPos = shiftX + 1 for each element.
	for i := range tc.yBitPos {
		tc.yBitPos[i]++
		tc.cbBitPos[i]++
		tc.crBitPos[i]++
	}

	// Inverse DWT → spatial domain → RGBA
	buf := GetTileBuffer()
	if extrapolate {
		inverseDWTExtAndConvert(yCoeffs, cbCoeffs, crCoeffs, w.dwtTmp, buf)
	} else {
		yPixels := inverseDWT(yCoeffs, w.dwtTmp)
		cbPixels := inverseDWT(cbCoeffs, w.dwtTmp)
		crPixels := inverseDWT(crCoeffs, w.dwtTmp)
		ycbcrToRGBA(buf, yPixels, cbPixels, crPixels)
	}

	// Cache decoded pixels for SurfaceToSurface re-blit (MS-RDPEGFX 3.3.3.3)
	if tc.pixels == nil {
		tc.pixels = make([]byte, tileSize*tileSize*4)
	}
	copy(tc.pixels, buf)

	// Diagnostic: first pixel and center pixel
	if d.log.Enabled(context.Background(), levelTrace) {
		mid := 32*64 + 32
		d.log.Log(context.Background(), levelTrace, "FIRST/SIMPLE tile pixels",
			"xIdx", xIdx, "yIdx", yIdx,
			"posX", xIdx*64, "posY", yIdx*64,
			"px[0,0]", fmt.Sprintf("BGRX(%d,%d,%d,%d)", buf[0], buf[1], buf[2], buf[3]),
			"px[32,32]", fmt.Sprintf("BGRX(%d,%d,%d,%d)", buf[mid*4], buf[mid*4+1], buf[mid*4+2], buf[mid*4+3]))
	}

	return DecodedTile{
		X:    xIdx * 64,
		Y:    yIdx * 64,
		Data: buf,
	}, true
}

// fillSignBuffer fills dst with sign values from coefficients.
// sign[i] = +1 if coeff > 0, -1 if coeff < 0, 0 if coeff == 0.
// dst must have len >= len(coeffs). Returns dst[:len(coeffs)].
// Uses branchless bit arithmetic to avoid branch mispredictions.
func fillSignBuffer(dst, coeffs []int16) []int16 {
	sign := dst[:len(coeffs)]
	n := len(coeffs)
	i := 0
	for ; i+3 < n; i += 4 {
		v0, v1, v2, v3 := coeffs[i], coeffs[i+1], coeffs[i+2], coeffs[i+3]
		sign[i] = (v0 >> 15) | int16(uint16(-v0)>>15)
		sign[i+1] = (v1 >> 15) | int16(uint16(-v1)>>15)
		sign[i+2] = (v2 >> 15) | int16(uint16(-v2)>>15)
		sign[i+3] = (v3 >> 15) | int16(uint16(-v3)>>15)
	}
	for ; i < n; i++ {
		v := coeffs[i]
		sign[i] = (v >> 15) | int16(uint16(-v)>>15)
	}
	return sign
}

// decodeTileUpgrade decodes an RFX_PROGRESSIVE_TILE_UPGRADE using SRL+RAW encoding.
// existing is the pre-looked-up tileCoeffs from Phase 1 (guaranteed non-nil).
func (d *Decoder) decodeTileUpgrade(w *tileWorkBuf, existing *tileCoeffs, data []byte, quants [][10]uint8,
	progQuants []progQuantEntry, extrapolate bool) (DecodedTile, bool) {
	// RFX_PROGRESSIVE_TILE_UPGRADE (MS-RDPEGFX 2.2.4.4.3):
	// quantIdxY(1) + quantIdxCb(1) + quantIdxCr(1) + xIdx(2) + yIdx(2) + quality(1)
	// + ySrlLen(2) + yRawLen(2) + cbSrlLen(2) + cbRawLen(2) + crSrlLen(2) + crRawLen(2) = 20
	// NOTE: No flags byte in upgrade tiles (unlike TILE_FIRST which has flags at offset 7).
	if len(data) < 20 {
		return DecodedTile{}, false
	}

	quantIdxY := int(data[0])
	quantIdxCb := int(data[1])
	quantIdxCr := int(data[2])
	xIdx := int(binary.LittleEndian.Uint16(data[3:5]))
	yIdx := int(binary.LittleEndian.Uint16(data[5:7]))
	quality := data[7]
	ySrlLen := int(binary.LittleEndian.Uint16(data[8:10]))
	yRawLen := int(binary.LittleEndian.Uint16(data[10:12]))
	cbSrlLen := int(binary.LittleEndian.Uint16(data[12:14]))
	cbRawLen := int(binary.LittleEndian.Uint16(data[14:16]))
	crSrlLen := int(binary.LittleEndian.Uint16(data[16:18]))
	crRawLen := int(binary.LittleEndian.Uint16(data[18:20]))

	off := 20
	ySrl := safeSlice(data, off, ySrlLen)
	off += ySrlLen
	yRaw := safeSlice(data, off, yRawLen)
	off += yRawLen
	cbSrl := safeSlice(data, off, cbSrlLen)
	off += cbSrlLen
	cbRaw := safeSlice(data, off, cbRawLen)
	off += cbRawLen
	crSrl := safeSlice(data, off, crSrlLen)
	off += crSrlLen
	crRaw := safeSlice(data, off, crRawLen)

	qY := getQuant(quants, quantIdxY)
	qCb := getQuant(quants, quantIdxCb)
	qCr := getQuant(quants, quantIdxCr)

	pqY, pqCb, pqCr := getProgQuant(progQuants, quality)

	// newBitPos = quant + progQuant (element-wise)
	var newYBitPos, newCbBitPos, newCrBitPos [10]uint8
	for i := 0; i < 10; i++ {
		newYBitPos[i] = qY[i] + pqY[i]
		newCbBitPos[i] = qCb[i] + pqCb[i]
		newCrBitPos[i] = qCr[i] + pqCr[i]
	}

	// numBits = oldBitPos - newBitPos (how many new bits of precision)
	var yNumBits, cbNumBits, crNumBits [10]uint8
	for i := 0; i < 10; i++ {
		if existing.yBitPos[i] > newYBitPos[i] {
			yNumBits[i] = existing.yBitPos[i] - newYBitPos[i]
		}
		if existing.cbBitPos[i] > newCbBitPos[i] {
			cbNumBits[i] = existing.cbBitPos[i] - newCbBitPos[i]
		}
		if existing.crBitPos[i] > newCrBitPos[i] {
			crNumBits[i] = existing.crBitPos[i] - newCrBitPos[i]
		}
	}
	// shift = quant + progQuant - 1
	shiftY := computeShift(qY, pqY)
	shiftCb := computeShift(qCb, pqCb)
	shiftCr := computeShift(qCr, pqCr)

	// Upgrade each component and validate bitstream consumption
	ySrlUsed, yRawUsed := upgradeComponent(existing.y, existing.ySign, ySrl, yRaw, shiftY, yNumBits, extrapolate)
	cbSrlUsed, cbRawUsed := upgradeComponent(existing.cb, existing.cbSign, cbSrl, cbRaw, shiftCb, cbNumBits, extrapolate)
	crSrlUsed, crRawUsed := upgradeComponent(existing.cr, existing.crSign, crSrl, crRaw, shiftCr, crNumBits, extrapolate)

	if ySrlUsed != ySrlLen || yRawUsed != yRawLen {
		d.log.LogAttrs(context.Background(), slog.LevelWarn, "UPGRADE Y bitstream mismatch",
			slog.Int("xIdx", xIdx), slog.Int("yIdx", yIdx),
			slog.Int("srlExpect", ySrlLen), slog.Int("srlUsed", ySrlUsed),
			slog.Int("rawExpect", yRawLen), slog.Int("rawUsed", yRawUsed))
	}
	if cbSrlUsed != cbSrlLen || cbRawUsed != cbRawLen {
		d.log.LogAttrs(context.Background(), slog.LevelWarn, "UPGRADE Cb bitstream mismatch",
			slog.Int("xIdx", xIdx), slog.Int("yIdx", yIdx),
			slog.Int("srlExpect", cbSrlLen), slog.Int("srlUsed", cbSrlUsed),
			slog.Int("rawExpect", cbRawLen), slog.Int("rawUsed", cbRawUsed))
	}
	if crSrlUsed != crSrlLen || crRawUsed != crRawLen {
		d.log.LogAttrs(context.Background(), slog.LevelWarn, "UPGRADE Cr bitstream mismatch",
			slog.Int("xIdx", xIdx), slog.Int("yIdx", yIdx),
			slog.Int("srlExpect", crSrlLen), slog.Int("srlUsed", crSrlUsed),
			slog.Int("rawExpect", crRawLen), slog.Int("rawUsed", crRawUsed))
	}

	// Update stored bit positions
	existing.yBitPos = newYBitPos
	existing.cbBitPos = newCbBitPos
	existing.crBitPos = newCrBitPos

	// DWT decode from a copy of current coefficients (DWT modifies in-place).
	// Use work buffers as scratch to avoid allocating copies.
	yC := w.coeffs[0]
	cbC := w.coeffs[1]
	crC := w.coeffs[2]
	copy(yC, existing.y)
	copy(cbC, existing.cb)
	copy(crC, existing.cr)

	buf := GetTileBuffer()
	if existing.extrapolate {
		inverseDWTExtAndConvert(yC, cbC, crC, w.dwtTmp, buf)
	} else {
		yPixels := inverseDWT(yC, w.dwtTmp)
		cbPixels := inverseDWT(cbC, w.dwtTmp)
		crPixels := inverseDWT(crC, w.dwtTmp)
		ycbcrToRGBA(buf, yPixels, cbPixels, crPixels)
	}

	// Cache decoded pixels for SurfaceToSurface re-blit (MS-RDPEGFX 3.3.3.3)
	if existing.pixels == nil {
		existing.pixels = make([]byte, tileSize*tileSize*4)
	}
	copy(existing.pixels, buf)

	// Diagnostic: center pixel
	if d.log.Enabled(context.Background(), levelTrace) {
		mid := 32*64 + 32
		d.log.Log(context.Background(), levelTrace, "UPGRADE tile pixels",
			"xIdx", xIdx, "yIdx", yIdx,
			"posX", xIdx*64, "posY", yIdx*64,
			"px[32,32]", fmt.Sprintf("BGRX(%d,%d,%d,%d)", buf[mid*4], buf[mid*4+1], buf[mid*4+2], buf[mid*4+3]))
	}

	return DecodedTile{
		X:    xIdx * 64,
		Y:    yIdx * 64,
		Data: buf,
	}, true
}

// upgradeComponent applies SRL+RAW upgrade data to a component's stored coefficients.
// Returns (srlConsumed, rawConsumed) byte counts for validation.
func upgradeComponent(current, sign []int16, srlData, rawData []byte,
	shift, numBits [10]uint8, extrapolate bool) (int, int) {

	var bands *[10]subbandInfo
	if extrapolate {
		bands = &extrapolateSubbands
	} else {
		bands = &standardSubbands
	}

	srl := bitReader{data: srlData}
	raw := bitReader{data: rawData}
	state := srlState{kp: 8}

	// Process non-LL bands (0..8) with sign routing
	for i := 0; i < 9; i++ {
		b := bands[i]
		nb := numBits[b.quantIdx]
		s := shift[b.quantIdx]
		if nb < 1 {
			continue
		}
		upgradeBlock(current[b.start:b.start+b.count], sign[b.start:b.start+b.count],
			&srl, &raw, &state, s, nb, true)
	}

	// Process LL3 band (index 9) — direct RAW read, no sign routing
	b := bands[9]
	nb := numBits[b.quantIdx]
	s := shift[b.quantIdx]
	if nb >= 1 {
		upgradeBlock(current[b.start:b.start+b.count], sign[b.start:b.start+b.count],
			&srl, &raw, &state, s, nb, false)
	}

	// Compute consumed bytes per MS-RDPEGFX 2.2.4.2.1.5.1 upgrade tile state:
	// 1. Align each bitstream to byte boundary
	// 2. If SRL has exactly 1 trailing byte, consume it
	// 3. Compare (position + 7) / 8 against expected lengths
	rawConsumed := raw.pos*8 - int(raw.bits)
	if rawConsumed%8 != 0 {
		rawConsumed += 8 - rawConsumed%8
	}
	rawBytes := rawConsumed / 8

	srlConsumed := srl.pos*8 - int(srl.bits)
	if srlConsumed%8 != 0 {
		srlConsumed += 8 - srlConsumed%8
	}
	srlRemain := len(srlData)*8 - srlConsumed
	if srlRemain == 8 {
		srlConsumed += 8
	}
	srlBytes := srlConsumed / 8

	return srlBytes, rawBytes
}

// srlState holds the adaptive Golomb state for SRL decoding.
type srlState struct {
	kp   uint32 // adaptive parameter (kp/8 = k)
	nz   int    // remaining zero count
	mode bool   // false=zero-encoding, true=value-encoding
}

// upgradeBlock processes one sub-band of upgrade data.
func upgradeBlock(current, sign []int16, srl, raw *bitReader, state *srlState,
	shift, numBits uint8, nonLL bool) {

	for i := range current {
		if !nonLL {
			// LL3: read directly from RAW stream
			input := rawShift(raw, numBits)
			current[i] += int16(input) << shift
			continue
		}

		// Non-LL sub-bands: route based on sign
		if sign[i] != 0 {
			// Previously non-zero: read magnitude from RAW, apply stored sign
			input := rawShift(raw, numBits)
			current[i] += sign[i] * (int16(input) << shift)
		} else {
			// Previously zero: read sign+magnitude from SRL
			val := srlRead(srl, state, numBits)
			if val > 0 {
				sign[i] = 1
			} else if val < 0 {
				sign[i] = -1
			}
			current[i] += int16(val) << shift
		}
	}
}

// rawShift reads numBits bits from the RAW bitstream as an unsigned value.
func rawShift(raw *bitReader, numBits uint8) uint32 {
	return raw.readBits(uint32(numBits))
}

// srlRead reads one coefficient from the SRL stream using adaptive Golomb coding.
// Returns a signed value: positive, negative, or zero.
func srlRead(srl *bitReader, state *srlState, numBits uint8) int16 {
	// If we're in a zero run, return 0
	if state.nz > 0 {
		state.nz--
		return 0
	}

	k := state.kp / 8

	if !state.mode {
		// Zero encoding mode
		bit := srl.readBit()
		if bit == 0 {
			// Long zero run: nz = 2^k
			state.nz = 1 << k
			state.kp += 4
			if state.kp > 80 {
				state.kp = 80
			}
			state.nz--
			return 0
		}

		// Short zero run: read k bits
		state.mode = true
		state.nz = 0
		if k > 0 {
			state.nz = int(srl.readBits(k))
		}
		if state.nz > 0 {
			state.nz--
			return 0
		}
		// Fall through to value encoding if nz was 0
	}

	// Value encoding mode
	state.mode = false

	// Read sign bit (1 = negative, 0 = positive)
	signBit := srl.readBit()

	// Update kp
	if state.kp < 6 {
		state.kp = 0
	} else {
		state.kp -= 6
	}

	// If only 1 bit of precision, magnitude is always 1
	if numBits == 1 {
		if signBit != 0 {
			return -1
		}
		return 1
	}

	// Read unary-encoded magnitude: count zeros until 1, starting from mag=1
	mag := uint32(1)
	max := uint32((1 << numBits) - 1)
	for mag < max {
		bit := srl.readBit()
		if bit != 0 {
			break
		}
		mag++
	}

	if signBit != 0 {
		return -int16(mag)
	}
	return int16(mag)
}

// differentialDecode applies running-sum to delta-encoded LL3 coefficients.
func differentialDecode(coeffs []int16, n int) {
	if n > len(coeffs) {
		n = len(coeffs)
	}
	for i := 1; i < n; i++ {
		coeffs[i] += coeffs[i-1]
	}
}

// ycbcrToRGBA converts 64x64 YCbCr (int16) to RGBA byte buffer.
// RFX uses the ICT (Irreversible Component Transform):
//
//	R = Y + 1.402 * Cr
//	G = Y - 0.344136 * Cb - 0.714136 * Cr
//	B = Y + 1.772 * Cb
//
// The decoder left-shifts coefficients by (quant + progQuant - 1) during
// dequantize. The encoder right-shifts by (quant + progQuant - 6), leaving
// a net scale factor of 2^5 = 32. We compensate by adding 4096
// (= 128 << 5) to Y and right-shifting by 5 after the ICT multiply.
// ICT coefficients are scaled by 2^16 (matching FreeRDP prim_colors.c
// row 16: {91916, 46819, 22527, 115992}):
//
//	Cr_R  = 91916  (1.402525 * 2^16)
//	Cr_G  = 46819  (0.714401 * 2^16)
//	Cb_G  = 22527  (0.343730 * 2^16)
//	Cb_B  = 115992 (1.769905 * 2^16)
func ycbcrToRGBA(out []byte, y, cb, cr []int16) {
	const n = tileSize * tileSize // 4096, divisible by 4
	// BCE hints: prove all indices are in-bounds
	_ = out[n*4-1]
	_ = y[n-1]
	_ = cb[n-1]
	_ = cr[n-1]

	for i := 0; i < n; i += 4 {
		yy0 := (int32(y[i]) + 4096) << 16
		cb0 := int32(cb[i])
		cr0 := int32(cr[i])
		yy1 := (int32(y[i+1]) + 4096) << 16
		cb1 := int32(cb[i+1])
		cr1 := int32(cr[i+1])
		yy2 := (int32(y[i+2]) + 4096) << 16
		cb2 := int32(cb[i+2])
		cr2 := int32(cr[i+2])
		yy3 := (int32(y[i+3]) + 4096) << 16
		cb3 := int32(cb[i+3])
		cr3 := int32(cr[i+3])

		out[i*4] = clampByte((yy0 + cr0*91916) >> 21)
		out[i*4+1] = clampByte((yy0 - cb0*22527 - cr0*46819) >> 21)
		out[i*4+2] = clampByte((yy0 + cb0*115992) >> 21)
		out[i*4+3] = 0xFF
		out[i*4+4] = clampByte((yy1 + cr1*91916) >> 21)
		out[i*4+5] = clampByte((yy1 - cb1*22527 - cr1*46819) >> 21)
		out[i*4+6] = clampByte((yy1 + cb1*115992) >> 21)
		out[i*4+7] = 0xFF
		out[i*4+8] = clampByte((yy2 + cr2*91916) >> 21)
		out[i*4+9] = clampByte((yy2 - cb2*22527 - cr2*46819) >> 21)
		out[i*4+10] = clampByte((yy2 + cb2*115992) >> 21)
		out[i*4+11] = 0xFF
		out[i*4+12] = clampByte((yy3 + cr3*91916) >> 21)
		out[i*4+13] = clampByte((yy3 - cb3*22527 - cr3*46819) >> 21)
		out[i*4+14] = clampByte((yy3 + cb3*115992) >> 21)
		out[i*4+15] = 0xFF
	}
}

func clampByte(v int32) byte {
	if uint32(v) <= 255 {
		return byte(v)
	}
	// v < 0 → v>>31 is -1, ^(-1) = 0, & 0xFF = 0
	// v > 255 → v>>31 is 0, ^0 = -1, & 0xFF = 255
	return byte(^(v >> 31) & 0xFF)
}

// dequantizeShift applies pre-computed shift values to DWT coefficients.
// For extrapolate mode, LL3 differential decode is done BEFORE the LL3 shift,
// but AFTER non-LL shifts (per MS-RDPRFX progressive decode sequence).
func dequantizeShift(coeffs []int16, shift [10]uint8, extrapolate bool) {
	if len(coeffs) < 4096 {
		return
	}
	c := coeffs[:4096:4096] // single bounds check, enables BCE for all subband access

	var bands *[10]subbandInfo
	if extrapolate {
		bands = &extrapolateSubbands
	} else {
		bands = &standardSubbands
	}

	if extrapolate {
		// Extrapolate: shift non-LL bands first, then differential decode LL3, then shift LL3
		for i := 0; i < 9; i++ { // bands [0..8] are non-LL
			b := bands[i]
			s := shift[b.quantIdx]
			if s == 0 {
				continue
			}
			for j := b.start; j < b.start+b.count; j++ {
				c[j] <<= s
			}
		}
		// LL3 differential decode at extrapolate offset
		differentialDecode(c[4015:4015+81], 81)
		// LL3 shift
		b := bands[9] // LL3
		s := shift[b.quantIdx]
		if s > 0 {
			for j := b.start; j < b.start+b.count; j++ {
				c[j] <<= s
			}
		}
	} else {
		// Standard: all bands shifted (differential decode already done by caller)
		for _, b := range bands {
			s := shift[b.quantIdx]
			if s == 0 {
				continue
			}
			for j := b.start; j < b.start+b.count; j++ {
				c[j] <<= s
			}
		}
	}
}

func getQuant(quants [][10]uint8, idx int) [10]uint8 {
	if idx < len(quants) {
		return quants[idx]
	}
	return [10]uint8{}
}

func safeSlice(data []byte, off, length int) []byte {
	if off >= len(data) || length <= 0 {
		return nil
	}
	end := off + length
	if end > len(data) {
		end = len(data)
	}
	return data[off:end]
}
