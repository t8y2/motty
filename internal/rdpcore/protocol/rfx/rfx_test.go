package rfx

import (
	"encoding/binary"
	"log/slog"
	"math/bits"
	"testing"
)

func TestRLGR1DecodeZeroData(t *testing.T) {
	out := rlgr1Decode(make([]int16, 10), nil)
	if len(out) != 10 {
		t.Fatalf("expected 10 coefficients, got %d", len(out))
	}
	for i, v := range out {
		if v != 0 {
			t.Fatalf("coefficient[%d] = %d, want 0", i, v)
		}
	}
}

func TestBitsLen32(t *testing.T) {
	// Verify bits.Len32 gives the same results as the old lzcnt32:
	// bits.Len32(v) == 32 - lzcnt32(v)
	tests := []struct {
		v       uint32
		wantLen int // bits.Len32 result
	}{
		{0, 0},
		{1, 1},
		{2, 2},
		{3, 2},
		{0x80000000, 32},
		{0x7FFFFFFF, 31},
		{16, 5},
	}
	for _, tc := range tests {
		got := bits.Len32(tc.v)
		if got != tc.wantLen {
			t.Errorf("bits.Len32(%d) = %d, want %d", tc.v, got, tc.wantLen)
		}
	}
}

func TestRLGR3DecodeZeroData(t *testing.T) {
	out := rlgr3Decode(make([]int16, 10), nil)
	if len(out) != 10 {
		t.Fatalf("expected 10 coefficients, got %d", len(out))
	}
}

func TestInverseDWT1D(t *testing.T) {
	low := make([]int16, 4)
	high := make([]int16, 4)
	out := make([]int16, 8)
	inverseDWT1D(out, low, high, 4)
	for i, v := range out {
		if v != 0 {
			t.Fatalf("out[%d] = %d, want 0", i, v)
		}
	}
}

func TestInverseDWTIdentity(t *testing.T) {
	coeffs := make([]int16, 4096)
	result := inverseDWT(coeffs, make([]int16, 4096))
	if len(result) != 4096 {
		t.Fatalf("result length = %d, want 4096", len(result))
	}
}

func TestInverseDWTExtrapolateIdentity(t *testing.T) {
	coeffs := make([]int16, 4096)
	result := inverseDWTExtrapolate(coeffs, make([]int16, 4096))
	if len(result) != 4096 {
		t.Fatalf("result length = %d, want 4096", len(result))
	}
}

func TestInverseDWTExtrapolateUniform(t *testing.T) {
	// Set LL3 to uniform value, all other bands zero.
	// DWT should propagate DC uniformly: output = LL3 value everywhere.
	coeffs := make([]int16, 4096)
	for i := 4015; i < 4015+81; i++ {
		coeffs[i] = 100
	}
	result := inverseDWTExtrapolate(coeffs, make([]int16, 4096))
	center := result[32*64+32]
	t.Logf("center pixel = %d (want 100)", center)
	// Check several pixels
	for _, pos := range [][2]int{{0, 0}, {32, 32}, {63, 63}, {0, 63}, {63, 0}} {
		v := result[pos[1]*64+pos[0]]
		t.Logf("pixel(%d,%d) = %d", pos[0], pos[1], v)
	}
	if center < 90 || center > 110 {
		t.Fatalf("center pixel = %d, want ~100 (DWT amplifying by %.1fx)", center, float64(center)/100.0)
	}
}

func TestInverseDWTStandardUniform(t *testing.T) {
	// Same test for standard DWT: LL3 at offset 4032, size 64 (8x8)
	coeffs := make([]int16, 4096)
	for i := 4032; i < 4032+64; i++ {
		coeffs[i] = 100
	}
	result := inverseDWT(coeffs, make([]int16, 4096))
	center := result[32*64+32]
	t.Logf("center pixel = %d (want 100)", center)
	if center < 90 || center > 110 {
		t.Fatalf("center pixel = %d, want ~100 (DWT amplifying by %.1fx)", center, float64(center)/100.0)
	}
}

func TestExtBandCounts(t *testing.T) {
	tests := []struct {
		level      int
		wantL      int
		wantH      int
		wantOutput int
	}{
		{1, 33, 31, 64},
		{2, 17, 16, 33},
		{3, 9, 8, 17},
	}
	for _, tc := range tests {
		l := extBandL(tc.level)
		h := extBandH(tc.level)
		if l != tc.wantL {
			t.Errorf("extBandL(%d) = %d, want %d", tc.level, l, tc.wantL)
		}
		if h != tc.wantH {
			t.Errorf("extBandH(%d) = %d, want %d", tc.level, h, tc.wantH)
		}
		if l+h != tc.wantOutput {
			t.Errorf("extBandL(%d)+extBandH(%d) = %d, want %d", tc.level, tc.level, l+h, tc.wantOutput)
		}
	}
}

func TestExtrapolateSubbandSizes(t *testing.T) {
	// Verify extrapolate subband sizes sum to 4096
	total := 0
	for _, s := range extrapolateSubbands {
		total += s.count
	}
	if total != 4096 {
		t.Fatalf("extrapolate subband total = %d, want 4096", total)
	}

	// Verify offsets are contiguous
	off := 0
	for i, s := range extrapolateSubbands {
		if s.start != off {
			t.Errorf("subband[%d] start = %d, want %d", i, s.start, off)
		}
		off += s.count
	}
}

func TestProgressiveDecodeSyncOnly(t *testing.T) {
	dec := NewDecoder(slog.Default())

	buf := make([]byte, 14)
	binary.LittleEndian.PutUint16(buf[0:2], blockSync)
	binary.LittleEndian.PutUint32(buf[2:6], 14)
	binary.LittleEndian.PutUint32(buf[6:10], 0xCACCACCA)
	binary.LittleEndian.PutUint32(buf[10:14], 0x0100)

	regions, err := dec.Decode(buf, 1)
	if err != nil {
		t.Fatalf("Decode error: %v", err)
	}
	if len(regions) != 0 {
		t.Fatalf("expected 0 regions from sync-only stream, got %d", len(regions))
	}
}

func TestYCbCrToRGBA(t *testing.T) {
	y := make([]int16, 64*64)
	cb := make([]int16, 64*64)
	cr := make([]int16, 64*64)
	// Y=0 + 128 offset = 128 → should give R=G=B=128 (neutral gray)
	result := make([]byte, 64*64*4)
	ycbcrToRGBA(result, y, cb, cr)
	if len(result) != 64*64*4 {
		t.Fatalf("result length = %d, want %d", len(result), 64*64*4)
	}
	r, g, b := result[0], result[1], result[2]
	if r != 128 || g != 128 || b != 128 {
		t.Fatalf("pixel[0] = R=%d G=%d B=%d, want 128,128,128", r, g, b)
	}
}

func TestDifferentialDecode(t *testing.T) {
	coeffs := []int16{10, 5, -3, 2}
	differentialDecode(coeffs, 4)
	want := []int16{10, 15, 12, 14}
	for i, v := range coeffs {
		if v != want[i] {
			t.Fatalf("coeffs[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestDequantizeShiftStandard(t *testing.T) {
	coeffs := make([]int16, 4096)
	// Put a marker value of 1 at the start of each standard subband
	coeffs[offHL1] = 1
	coeffs[offLH1] = 1
	coeffs[offHH1] = 1
	coeffs[offHL2] = 1
	coeffs[offLH2] = 1
	coeffs[offHH2] = 1
	coeffs[offHL3] = 1
	coeffs[offLH3] = 1
	coeffs[offHH3] = 1
	coeffs[offLL3] = 1

	// Shift of 2 for all subbands (= quant 3, progQuant 0 → 3+0-1=2)
	shift := [10]uint8{2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	dequantizeShift(coeffs, shift, false)

	checks := []struct {
		off  int
		name string
	}{
		{offHL1, "HL1"},
		{offLH1, "LH1"},
		{offHH1, "HH1"},
		{offHL2, "HL2"},
		{offLH2, "LH2"},
		{offHH2, "HH2"},
		{offHL3, "HL3"},
		{offLH3, "LH3"},
		{offHH3, "HH3"},
		{offLL3, "LL3"},
	}
	for _, c := range checks {
		if coeffs[c.off] != 4 {
			t.Errorf("%s at offset %d = %d, want 4", c.name, c.off, coeffs[c.off])
		}
	}
}

func TestDequantizeShiftExtrapolate(t *testing.T) {
	coeffs := make([]int16, 4096)
	// Put marker values at extrapolate subband starts
	for _, s := range extrapolateSubbands {
		coeffs[s.start] = 1
	}

	shift := [10]uint8{2, 2, 2, 2, 2, 2, 2, 2, 2, 2}
	dequantizeShift(coeffs, shift, true)

	// All markers should be shifted to 4 (1 << 2)
	for i, s := range extrapolateSubbands {
		if coeffs[s.start] != 4 {
			t.Errorf("subband[%d] at offset %d = %d, want 4", i, s.start, coeffs[s.start])
		}
	}
}

func TestComputeShift(t *testing.T) {
	quant := [10]uint8{6, 6, 6, 6, 6, 6, 6, 6, 6, 6}
	progQuant := [10]uint8{1, 2, 0, 1, 0, 0, 0, 0, 0, 0}
	shift := computeShift(quant, progQuant)
	// shift[0] = 6+1-1 = 6
	// shift[1] = 6+2-1 = 7
	// shift[2] = 6+0-1 = 5
	// shift[3] = 6+1-1 = 6
	// rest = 6+0-1 = 5
	want := [10]uint8{6, 7, 5, 6, 5, 5, 5, 5, 5, 5}
	if shift != want {
		t.Errorf("computeShift = %v, want %v", shift, want)
	}
}

func TestParseQuantValues(t *testing.T) {
	data := []byte{0x66, 0x66, 0x66, 0x66, 0x66}
	q := parseQuantValues(data)
	for i, v := range q {
		if v != 6 {
			t.Errorf("quant[%d] = %d, want 6", i, v)
		}
	}
}

func TestFillSignBuffer(t *testing.T) {
	coeffs := []int16{5, -3, 0, 1, -1, 0}
	sign := fillSignBuffer(make([]int8, len(coeffs)), coeffs)
	want := []int8{1, -1, 0, 1, -1, 0}
	for i, v := range sign {
		if v != want[i] {
			t.Errorf("sign[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestSrlReadZeroRun(t *testing.T) {
	// SRL stream with a 0 bit (long zero run) when k=1 (kp=8, k=8/8=1)
	// This should produce a run of 2^1 = 2 zeros
	state := &srlState{kp: 8}
	srl := &bitReader{data: []byte{0x00}} // all zero bits

	// First call: bit=0, nz=2^1=2, nz--, return 0
	v := srlRead(srl, state, 2)
	if v != 0 {
		t.Fatalf("srlRead[0] = %d, want 0", v)
	}
	// Second call: nz=1, nz--, return 0
	v = srlRead(srl, state, 2)
	if v != 0 {
		t.Fatalf("srlRead[1] = %d, want 0", v)
	}
}

func TestUpgradeBlockLL(t *testing.T) {
	// LL band: reads directly from RAW, no sign routing
	current := make([]int16, 4)
	sign := make([]int8, 4)
	srl := &bitReader{}
	raw := &bitReader{data: []byte{0xA5}} // 10100101 → reads of 2 bits: 10=2, 10=2, 01=1, 01=1
	state := &srlState{kp: 8}

	upgradeBlock(current, sign, srl, raw, state, 1, 2, false) // shift=1, numBits=2, nonLL=false
	// current[0] += 2 << 1 = 4
	// current[1] += 2 << 1 = 4
	// current[2] += 1 << 1 = 2
	// current[3] += 1 << 1 = 2
	want := []int16{4, 4, 2, 2}
	for i, v := range current {
		if v != want[i] {
			t.Errorf("current[%d] = %d, want %d", i, v, want[i])
		}
	}
}

func TestExtrapolateDWTWithShift(t *testing.T) {
	// Simulate the actual decode pipeline: RLGR1 → dequantize → DWT
	// RLGR1 produces delta-encoded LL3: first value + deltas.
	// For uniform black: first=-2, all deltas=0 → diff decode → all -2 → shift <<6 → all -128
	coeffs := make([]int16, 4096)
	coeffs[4015] = -2 // delta-encoded: first value, rest = 0 (deltas)

	shift := [10]uint8{6, 6, 6, 7, 9, 9, 10, 9, 9, 10}
	dequantizeShift(coeffs, shift, true)

	t.Logf("LL3[0..4] after dequant+diffDecode: %v", coeffs[4015:4020])

	result := inverseDWTExtrapolate(coeffs, make([]int16, 4096))
	center := result[32*64+32]
	t.Logf("center pixel after DWT: %d (want -128)", center)

	if center < -150 || center > -100 {
		t.Fatalf("center pixel = %d, want ~-128 (amplification factor: %.1f)", center, float64(center)/-128.0)
	}
}

func TestExtrapolateDWTServerValues(t *testing.T) {
	// Test with value -64 as first LL3 coeff (what the server appears to send for black tiles)
	// With shift=6: -64 << 6 = -4096. After DWT, center should be -4096 (uniform).
	// In ycbcrToBGRX: -4096 + 128 = -3968 → clamps to 0 (black). Correct output but huge intermediate.
	coeffs := make([]int16, 4096)
	coeffs[4015] = -64

	shift := [10]uint8{6, 6, 6, 7, 9, 9, 10, 9, 9, 10}
	dequantizeShift(coeffs, shift, true)

	t.Logf("LL3[0..4] after dequant: %v", coeffs[4015:4020])

	result := inverseDWTExtrapolate(coeffs, make([]int16, 4096))
	center := result[32*64+32]
	t.Logf("center after DWT: %d", center)

	// If the server sends -64, it means the actual spatial value is -4096 (which clamps to black)
	// But for BLACK, Y should be -128, so something is off by 32x (2^5)
	t.Logf("If DWT correct: value=%d. Expected for black: -128. Ratio: %.1f", center, float64(center)/-128.0)
}

func TestExtrapolateDWTWithDiffDecode(t *testing.T) {
	// Test differential decode behavior for LL3.
	// If the first LL3 value is -2 and all others are 0 (delta-encoded),
	// differential decode should produce: [-2, -2, -2, ...] (running sum).
	coeffs := make([]int16, 4096)
	coeffs[4015] = -2 // first LL3 value, rest are 0 (deltas)

	// Manually run differential decode + shift for LL3
	differentialDecode(coeffs[4015:4015+81], 81)

	t.Logf("LL3 after diff decode [0..8]: %v", coeffs[4015:4024])

	// All values should be -2 (running sum of -2 + 0 + 0 + ...)
	for i := 4015; i < 4015+81; i++ {
		if coeffs[i] != -2 {
			t.Fatalf("LL3[%d] = %d, want -2", i-4015, coeffs[i])
		}
	}

	// Now shift by 6
	for i := 4015; i < 4015+81; i++ {
		coeffs[i] <<= 6
	}

	t.Logf("LL3 after shift [0..4]: %v", coeffs[4015:4020])

	// Should be -128
	if coeffs[4015] != -128 {
		t.Fatalf("LL3[0] after shift = %d, want -128", coeffs[4015])
	}

	result := inverseDWTExtrapolate(coeffs, make([]int16, 4096))
	center := result[32*64+32]
	t.Logf("center pixel = %d (want -128)", center)
	if center != -128 {
		t.Fatalf("center pixel = %d, want -128", center)
	}
}

func TestExtrapolateDWTHighFreq(t *testing.T) {
	// Test with non-zero high-frequency coefficients.
	// Set LL3=100 (uniform) and one HL1 value to 1.
	// The HL1 should contribute a small perturbation, not a massive amplification.
	coeffs := make([]int16, 4096)
	// LL3: all 100
	for i := 4015; i < 4015+81; i++ {
		coeffs[i] = 100
	}
	// One HL1 coefficient = 1 (at position 500 in the 1023-element HL1 band)
	coeffs[500] = 1

	result := inverseDWTExtrapolate(coeffs, make([]int16, 4096))
	center := result[32*64+32]
	t.Logf("center pixel with HL1[500]=1: %d (want ~100)", center)

	// Now test with large HL1 values (post-dequantize, shift=9 → 1<<9=512)
	coeffs2 := make([]int16, 4096)
	for i := 4015; i < 4015+81; i++ {
		coeffs2[i] = 100
	}
	// Simulate: RLGR1 value 1 << shift(9) = 512 for a few HL1 coefficients
	coeffs2[0] = 512   // HL1[0]
	coeffs2[100] = 512 // HL1[100]
	coeffs2[500] = 512 // HL1[500]

	result2 := inverseDWTExtrapolate(coeffs2, make([]int16, 4096))
	center2 := result2[32*64+32]
	t.Logf("center pixel with three HL1=512: %d (want ~100)", center2)

	// Test standard DWT for comparison
	coeffs3 := make([]int16, 4096)
	for i := 4032; i < 4032+64; i++ {
		coeffs3[i] = 100
	}
	coeffs3[0] = 512
	coeffs3[100] = 512
	coeffs3[500] = 512

	result3 := inverseDWT(coeffs3, make([]int16, 4096))
	center3 := result3[32*64+32]
	t.Logf("standard DWT center with three HL1=512: %d (want ~100)", center3)
}

func TestFusedDWTConvertMatchesSeparate(t *testing.T) {
	// Verify that inverseDWTExtAndConvert produces identical RGBA output
	// to separate inverseDWTExtrapolate + ycbcrToRGBA.
	const n = tileSize * tileSize

	// Create test coefficients: LL3 = -2 (maps to gray after DWT+ICT).
	makeCoeffs := func() []int16 {
		c := make([]int16, n)
		for i := 4015; i < n; i++ { // LL3 region for extrapolate
			c[i] = -2
		}
		// Add some high-freq noise
		c[100] = 5
		c[200] = -3
		c[3900] = 7
		return c
	}

	// Reference: separate DWT + color convert
	yRef, cbRef, crRef := makeCoeffs(), makeCoeffs(), makeCoeffs()
	tmp := make([]int16, 3*4096)
	yPixels := inverseDWTExtrapolate(yRef, tmp[:4096])
	cbPixels := inverseDWTExtrapolate(cbRef, tmp[:4096])
	crPixels := inverseDWTExtrapolate(crRef, tmp[:4096])
	ref := make([]byte, n*4)
	ycbcrToRGBA(ref, yPixels, cbPixels, crPixels)

	// Fused path
	yFused, cbFused, crFused := makeCoeffs(), makeCoeffs(), makeCoeffs()
	got := make([]byte, n*4)
	inverseDWTExtAndConvert(yFused, cbFused, crFused, tmp, got)

	for i := range ref {
		if got[i] != ref[i] {
			pixel := i / 4
			channel := i % 4
			t.Fatalf("mismatch at byte %d (pixel %d, channel %d): got %d, want %d",
				i, pixel, channel, got[i], ref[i])
		}
	}
}

// BenchmarkDecodeTileData measures the per-tile decode cost:
// RLGR1 → dequantize → DWT → YCbCr→BGRX.
// Use: go test -bench=BenchmarkDecodeTileData -benchmem ./protocol/rfx/
func BenchmarkDecodeTileData(b *testing.B) {
	// Generate pseudo-random RLGR1 input data.
	// Real tiles have ~200-600 bytes of RLGR-compressed data per component.
	rlgrData := make([]byte, 400)
	for i := range rlgrData {
		rlgrData[i] = byte((i*73 + 29) & 0xFF)
	}

	shift := [10]uint8{6, 6, 6, 7, 9, 9, 10, 9, 9, 10}
	dec := NewDecoder(slog.New(slog.NewTextHandler(
		discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError},
	)))

	// Pre-allocate tileCoeffs like Phase 1 does.
	buf := make([]int16, 3*4096)
	signs := make([]int8, 3*4096)
	tc := &tileCoeffs{
		y: buf[0:4096], cb: buf[4096:8192], cr: buf[8192:12288],
		ySign: signs[0:4096], cbSign: signs[4096:8192], crSign: signs[8192:12288],
	}

	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		dec.decodeTileData(&dec.work, tc, rlgrData, rlgrData, rlgrData, 0, 0,
			shift, shift, shift, true, false)
	}
}

// BenchmarkDecodeTileDataParallel measures tile decode throughput with GOMAXPROCS workers.
func BenchmarkDecodeTileDataParallel(b *testing.B) {
	rlgrData := make([]byte, 400)
	for i := range rlgrData {
		rlgrData[i] = byte((i*73 + 29) & 0xFF)
	}

	shift := [10]uint8{6, 6, 6, 7, 9, 9, 10, 9, 9, 10}

	b.ResetTimer()
	b.RunParallel(func(pb *testing.PB) {
		// Each goroutine gets its own Decoder (own work buffers).
		dec := NewDecoder(slog.New(slog.NewTextHandler(
			discardWriter{}, &slog.HandlerOptions{Level: slog.LevelError},
		)))
		buf := make([]int16, 3*4096)
		signs := make([]int8, 3*4096)
		tc := &tileCoeffs{
			y: buf[0:4096], cb: buf[4096:8192], cr: buf[8192:12288],
			ySign: signs[0:4096], cbSign: signs[4096:8192], crSign: signs[8192:12288],
		}

		for pb.Next() {
			dec.decodeTileData(&dec.work, tc, rlgrData, rlgrData, rlgrData, 0, 0,
				shift, shift, shift, true, false)
		}
	})
}

type discardWriter struct{}

func (discardWriter) Write(p []byte) (int, error) { return len(p), nil }

func TestInt16Wrap(t *testing.T) {
	// Verify int16() truncation gives 2's complement wraparound (not clamping).
	tests := []struct {
		v    int32
		want int16
	}{
		{0, 0},
		{100, 100},
		{-100, -100},
		{32767, 32767},
		{32768, -32768}, // wraps, not clamps
		{-32768, -32768},
		{-32769, 32767}, // wraps, not clamps
		{40000, -25536}, // 40000 - 65536 = -25536
	}
	for _, tc := range tests {
		got := int16(tc.v)
		if got != tc.want {
			t.Errorf("int16(%d) = %d, want %d", tc.v, got, tc.want)
		}
	}
}
