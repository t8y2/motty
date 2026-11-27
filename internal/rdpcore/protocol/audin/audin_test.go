package audin

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
	"time"
)

type capture struct {
	mu   sync.Mutex
	pdus [][]byte
}

func (c *capture) send(pdu []byte) error {
	c.mu.Lock()
	c.pdus = append(c.pdus, append([]byte(nil), pdu...))
	c.mu.Unlock()
	return nil
}

func (c *capture) sent() [][]byte {
	c.mu.Lock()
	defer c.mu.Unlock()
	return append([][]byte(nil), c.pdus...)
}

func (c *capture) clear() {
	c.mu.Lock()
	c.pdus = nil
	c.mu.Unlock()
}

type fakeReader struct {
	mu     sync.Mutex
	chunks chan []byte
	cur    []byte
	closed bool
}

func newFakeReader() *fakeReader {
	return &fakeReader{chunks: make(chan []byte, 16)}
}

func (r *fakeReader) push(p []byte) {
	r.chunks <- append([]byte(nil), p...)
}

func (r *fakeReader) Read(p []byte) (int, error) {
	for len(r.cur) == 0 {
		chunk, ok := <-r.chunks
		if !ok {
			return 0, io.EOF
		}
		r.cur = chunk
	}
	n := copy(p, r.cur)
	r.cur = r.cur[n:]
	return n, nil
}

func (r *fakeReader) Close() error {
	r.mu.Lock()
	if !r.closed {
		r.closed = true
		close(r.chunks)
	}
	r.mu.Unlock()
	return nil
}

func (r *fakeReader) isClosed() bool {
	r.mu.Lock()
	defer r.mu.Unlock()
	return r.closed
}

func eventually(t *testing.T, what string, ok func() bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for !ok() {
		if time.Now().After(deadline) {
			t.Fatalf("timed out waiting for %s", what)
		}
		time.Sleep(time.Millisecond)
	}
}

func pcmFormat(rate uint32, channels uint16) audioFormat {
	blockAlign := channels * 2
	return audioFormat{
		Tag:           waveFormatPCM,
		Channels:      channels,
		SampleRate:    rate,
		AvgBytes:      rate * uint32(blockAlign),
		BlockAlign:    blockAlign,
		BitsPerSample: 16,
	}
}

func serverFormatsPDU(formats ...audioFormat) []byte {
	fmtBytes := 0
	for _, f := range formats {
		fmtBytes += audioFormatFixed + len(f.Extra)
	}
	pdu := make([]byte, 1+8+fmtBytes)
	pdu[0] = msgFormats
	binary.LittleEndian.PutUint32(pdu[1:5], uint32(len(formats)))
	binary.LittleEndian.PutUint32(pdu[5:9], uint32(len(pdu)))
	off := 9
	for _, f := range formats {
		off += encodeFormat(pdu, off, f)
	}
	return pdu[:off]
}

func serverVersionPDU(v uint32) []byte {
	var pdu [5]byte
	pdu[0] = msgVersion
	binary.LittleEndian.PutUint32(pdu[1:5], v)
	return pdu[:]
}

func serverOpenPDU(framesPerPacket, formatIdx uint32) []byte {
	var pdu [9]byte
	pdu[0] = msgOpen
	binary.LittleEndian.PutUint32(pdu[1:5], framesPerPacket)
	binary.LittleEndian.PutUint32(pdu[5:9], formatIdx)
	return pdu[:]
}

func serverFormatChangePDU(formatIdx uint32) []byte {
	var pdu [5]byte
	pdu[0] = msgFormatChange
	binary.LittleEndian.PutUint32(pdu[1:5], formatIdx)
	return pdu[:]
}

func TestVersionClampsToSupportedMax(t *testing.T) {
	cap := &capture{}
	h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
		return newFakeReader(), nil
	}, nil)
	h.ProcessPDU(serverVersionPDU(5))
	sent := cap.sent()
	if len(sent) != 1 || sent[0][0] != msgVersion {
		t.Fatalf("sent = %v, want single version PDU", sent)
	}
	if got := binary.LittleEndian.Uint32(sent[0][1:5]); got != maxClientVersion {
		t.Fatalf("version = %d, want %d", got, maxClientVersion)
	}
}

func TestFormatsFiltersToPCM16AndPrecedesWithDataIncoming(t *testing.T) {
	cap := &capture{}
	h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
		return newFakeReader(), nil
	}, nil)
	h.ProcessPDU(serverFormatsPDU(
		audioFormat{Tag: 0x0002, Channels: 2, SampleRate: 44100, BitsPerSample: 4, BlockAlign: 1024}, // MS-ADPCM
		pcmFormat(44100, 2),
		audioFormat{Tag: waveFormatPCM, Channels: 1, SampleRate: 44100, BitsPerSample: 8, BlockAlign: 1}, // 8-bit PCM
		pcmFormat(16000, 1),
	))
	sent := cap.sent()
	if len(sent) != 2 {
		t.Fatalf("sent %d PDUs, want 2 (DATA_INCOMING + FORMATS)", len(sent))
	}
	if len(sent[0]) != 1 || sent[0][0] != msgDataIncoming {
		t.Fatalf("first PDU = %v, want MSG_SNDIN_DATA_INCOMING", sent[0])
	}
	formatsPDU := sent[1]
	if formatsPDU[0] != msgFormats {
		t.Fatalf("second PDU type = %#x, want msgFormats", formatsPDU[0])
	}
	if got := binary.LittleEndian.Uint32(formatsPDU[1:5]); got != 2 {
		t.Fatalf("NumFormats = %d, want 2", got)
	}
	if got := binary.LittleEndian.Uint32(formatsPDU[5:9]); got != uint32(len(formatsPDU)) {
		t.Fatalf("cbSizeFormatsPacket = %d, want %d", got, len(formatsPDU))
	}
	f0, used0, err := parseFormat(formatsPDU, 9)
	if err != nil {
		t.Fatal(err)
	}
	f1, _, err := parseFormat(formatsPDU, 9+used0)
	if err != nil {
		t.Fatal(err)
	}
	if f0.SampleRate != 44100 || f0.Channels != 2 || f1.SampleRate != 16000 || f1.Channels != 1 {
		t.Fatalf("unexpected accepted formats: %+v, %+v", f0, f1)
	}
}

func TestOpenStreamsAudioPacketsAndClosesOnReset(t *testing.T) {
	cap := &capture{}
	reader := newFakeReader()
	var openedRate uint32
	var openedCh uint16
	var openedFrames uint32
	h := NewHandler(cap.send, func(rate uint32, channels uint16, framesPerPacket uint32) (io.ReadCloser, error) {
		openedRate, openedCh, openedFrames = rate, channels, framesPerPacket
		return reader, nil
	}, nil)
	h.ProcessPDU(serverFormatsPDU(pcmFormat(48000, 1)))
	cap.clear()

	// 4 frames * 1 channel * 2 bytes = 8 bytes per packet.
	h.ProcessPDU(serverOpenPDU(4, 0))
	eventually(t, "open response PDUs", func() bool { return len(cap.sent()) >= 2 })
	if openedRate != 48000 || openedCh != 1 || openedFrames != 4 {
		t.Fatalf("opened (%d, %d, %d), want (48000, 1, 4)", openedRate, openedCh, openedFrames)
	}
	sent := cap.sent()
	if len(sent) != 2 || sent[0][0] != msgFormatChange || sent[1][0] != msgOpenReply {
		t.Fatalf("open response PDUs = %v, want FORMAT_CHANGE then OPEN_REPLY", sent)
	}
	if got := binary.LittleEndian.Uint32(sent[1][1:5]); got != hresultOK {
		t.Fatalf("open reply HRESULT = %#x, want 0", got)
	}

	cap.clear()
	wantPCM := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	// Push in two partial reads to verify full-packet assembly.
	reader.push(wantPCM[:3])
	reader.push(wantPCM[3:])

	eventually(t, "audio packet sent", func() bool { return len(cap.sent()) >= 2 })
	pkt := cap.sent()
	if len(pkt[0]) != 1 || pkt[0][0] != msgDataIncoming {
		t.Fatalf("first audio PDU = %v, want DATA_INCOMING", pkt[0])
	}
	if pkt[1][0] != msgData || !bytes.Equal(pkt[1][1:], wantPCM) {
		t.Fatalf("audio data = %v, want msgData + %v", pkt[1], wantPCM)
	}

	h.Reset()
	if !reader.isClosed() {
		t.Fatal("Reset did not close the active input reader")
	}
}

func TestOpenFailureRepliesWithErrorHRESULT(t *testing.T) {
	cap := &capture{}
	h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
		return nil, errors.New("no microphone")
	}, nil)
	h.ProcessPDU(serverFormatsPDU(pcmFormat(44100, 2)))
	cap.clear()

	h.ProcessPDU(serverOpenPDU(441, 0))
	eventually(t, "open failure reply", func() bool { return len(cap.sent()) >= 1 })
	sent := cap.sent()
	if len(sent) != 1 || sent[0][0] != msgOpenReply {
		t.Fatalf("sent = %v, want single OPEN_REPLY", sent)
	}
	if got := binary.LittleEndian.Uint32(sent[0][1:5]); got != hresultFail {
		t.Fatalf("open reply HRESULT = %#x, want %#x", got, hresultFail)
	}
}

func TestFormatChangeKeepsReaderWhenShapeUnchangedAndReopensWhenChanged(t *testing.T) {
	cap := &capture{}
	r1 := newFakeReader()
	r2 := newFakeReader()
	var opens sync.Mutex
	openCount := 0
	h := NewHandler(cap.send, func(rate uint32, channels uint16, frames uint32) (io.ReadCloser, error) {
		opens.Lock()
		openCount++
		n := openCount
		opens.Unlock()
		if n == 1 {
			return r1, nil
		}
		return r2, nil
	}, nil)
	h.ProcessPDU(serverFormatsPDU(
		pcmFormat(48000, 2),
		pcmFormat(48000, 2),
		pcmFormat(16000, 1),
	))
	h.ProcessPDU(serverOpenPDU(480, 0))
	eventually(t, "initial open reply", func() bool { return len(cap.sent()) >= 4 })

	// Same rate & channels at index 1 keeps r1 open.
	cap.clear()
	h.ProcessPDU(serverFormatChangePDU(1))
	opens.Lock()
	gotOpens := openCount
	opens.Unlock()
	if gotOpens != 1 || r1.isClosed() {
		t.Fatalf("format change with identical PCM shape reopened reader (opens=%d, closed=%v)", gotOpens, r1.isClosed())
	}
	if sent := cap.sent(); len(sent) != 1 || sent[0][0] != msgFormatChange || binary.LittleEndian.Uint32(sent[0][1:5]) != 1 {
		t.Fatalf("sent = %v, want FORMAT_CHANGE(1)", sent)
	}

	// Different rate & channels at index 2 closes r1 and opens r2 asynchronously.
	cap.clear()
	h.ProcessPDU(serverFormatChangePDU(2))
	eventually(t, "format change reopen", func() bool { return len(cap.sent()) >= 1 && r1.isClosed() })
	opens.Lock()
	gotOpens = openCount
	opens.Unlock()
	if gotOpens != 2 || !r1.isClosed() {
		t.Fatalf("format change to 16kHz mono did not replace reader (opens=%d, r1Closed=%v)", gotOpens, r1.isClosed())
	}
	if sent := cap.sent(); len(sent) != 1 || sent[0][0] != msgFormatChange || binary.LittleEndian.Uint32(sent[0][1:5]) != 2 {
		t.Fatalf("sent = %v, want FORMAT_CHANGE(2)", sent)
	}
	h.Close()
	if !r2.isClosed() {
		t.Fatal("Close did not close r2")
	}
}

func TestOpenDoesNotBlockReceiveLoopWhileInputOpens(t *testing.T) {
	cap := &capture{}
	entered := make(chan struct{})
	gate := make(chan struct{})
	reader := newFakeReader()
	h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
		close(entered)
		<-gate
		return reader, nil
	}, nil)
	h.ProcessPDU(serverFormatsPDU(pcmFormat(48000, 1)))
	cap.clear()

	done := make(chan struct{})
	go func() {
		h.ProcessPDU(serverOpenPDU(480, 0))
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("ProcessPDU(MSG_SNDIN_OPEN) blocked waiting for input device open")
	}
	select {
	case <-entered:
	case <-time.After(time.Second):
		t.Fatal("input callback was not invoked")
	}

	// Other PDUs (e.g., version) continue flowing while permission prompt is open.
	h.ProcessPDU(serverVersionPDU(2))
	eventually(t, "version response while open is pending", func() bool { return len(cap.sent()) == 1 })

	// Closing before the device finishes opening must not deadlock and must
	// close the reader once the pending open returns.
	h.Close()
	close(gate)
	eventually(t, "cancelled pending open closes reader", reader.isClosed)
	if sent := cap.sent(); len(sent) != 1 || sent[0][0] != msgVersion {
		t.Fatalf("unexpected open replies after Close: %v", sent)
	}
}

func TestProcessPDUUnpackingAndMalformedInputs(t *testing.T) {
	t.Run("truncated fixed headers send no reply", func(t *testing.T) {
		cap := &capture{}
		h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}, nil)
		defer h.Close()

		for n := range 4 {
			h.ProcessPDU(append([]byte{msgVersion}, make([]byte, n)...))
		}
		for n := range 8 {
			h.ProcessPDU(append([]byte{msgFormats}, make([]byte, n)...))
		}
		for n := range 8 {
			h.ProcessPDU(append([]byte{msgOpen}, make([]byte, n)...))
		}
		for n := range 4 {
			h.ProcessPDU(append([]byte{msgFormatChange}, make([]byte, n)...))
		}
		h.ProcessPDU(nil)
		h.ProcessPDU([]byte{0x7f, 1, 2, 3})

		if sent := cap.sent(); len(sent) != 0 {
			t.Fatalf("truncated/unknown PDUs sent %d replies, want 0: %v", len(sent), sent)
		}
	})

	t.Run("formats extra bytes and trailing truncation preserve earlier valid formats", func(t *testing.T) {
		cap := &capture{}
		h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}, nil)
		defer h.Close()

		// Format 0: non-PCM with 6 extra bytes (must advance offset cleanly).
		f0 := audioFormat{Tag: 0x0002, Channels: 2, SampleRate: 44100, BitsPerSample: 4, BlockAlign: 1024, Extra: []byte{1, 2, 3, 4, 5, 6}}
		// Format 1: valid 16-bit PCM with BlockAlign=0 (normalized to Channels*2).
		f1 := audioFormat{Tag: waveFormatPCM, Channels: 2, SampleRate: 48000, BitsPerSample: 16, BlockAlign: 0}
		// Format 2: invalid BlockAlign / channels / sample rate (filtered out).
		f2 := audioFormat{Tag: waveFormatPCM, Channels: 2, SampleRate: 48000, BitsPerSample: 16, BlockAlign: 8}
		f3 := audioFormat{Tag: waveFormatPCM, Channels: 3, SampleRate: 48000, BitsPerSample: 16, BlockAlign: 6}
		f4 := audioFormat{Tag: waveFormatPCM, Channels: 1, SampleRate: 4000, BitsPerSample: 16, BlockAlign: 2}
		// Format 5: valid PCM followed by a truncated trailing format header.
		f5 := pcmFormat(16000, 1)

		pdu := serverFormatsPDU(f0, f1, f2, f3, f4, f5)
		// Claim 0xFFFFFFFF formats and append a partial 10-byte format header.
		binary.LittleEndian.PutUint32(pdu[1:5], 0xffffffff)
		pdu = append(pdu, make([]byte, 10)...)

		h.ProcessPDU(pdu)
		sent := cap.sent()
		if len(sent) != 2 || sent[0][0] != msgDataIncoming || sent[1][0] != msgFormats {
			t.Fatalf("sent = %v, want DATA_INCOMING + FORMATS", sent)
		}
		if got := binary.LittleEndian.Uint32(sent[1][1:5]); got != 2 {
			t.Fatalf("accepted formats = %d, want 2", got)
		}

		// Overflowing cbSize in extra data stops parsing cleanly without panic.
		cap.clear()
		badExtra := serverFormatsPDU(pcmFormat(44100, 1), pcmFormat(16000, 1))
		// Corrupt cbSize of second format to 500 bytes (past end of PDU).
		binary.LittleEndian.PutUint16(badExtra[9+audioFormatFixed+16:], 500)
		h.ProcessPDU(badExtra)
		sent = cap.sent()
		if len(sent) != 2 || binary.LittleEndian.Uint32(sent[1][1:5]) != 1 {
			t.Fatalf("badExtra accepted = %v, want 1 format", sent)
		}
	})

	t.Run("open and format change bounds and extreme framesPerPacket", func(t *testing.T) {
		cap := &capture{}
		var gotFrames []uint32
		var mu sync.Mutex
		h := NewHandler(cap.send, func(_ uint32, _ uint16, frames uint32) (io.ReadCloser, error) {
			mu.Lock()
			gotFrames = append(gotFrames, frames)
			mu.Unlock()
			return io.NopCloser(bytes.NewReader(nil)), nil
		}, nil)
		defer h.Close()

		h.ProcessPDU(serverFormatsPDU(pcmFormat(48000, 2)))
		cap.clear()

		// Out-of-range uint32 index (including 0xFFFFFFFF) replies E_FAIL.
		h.ProcessPDU(serverOpenPDU(480, 0xffffffff))
		if sent := cap.sent(); len(sent) != 1 || sent[0][0] != msgOpenReply || binary.LittleEndian.Uint32(sent[0][1:5]) != hresultFail {
			t.Fatalf("sent = %v, want OPEN_REPLY(E_FAIL)", sent)
		}

		// Out-of-range format change is ignored without panic.
		cap.clear()
		h.ProcessPDU(serverFormatChangePDU(0xffffffff))
		if sent := cap.sent(); len(sent) != 0 {
			t.Fatalf("invalid format change sent %v, want none", sent)
		}

		// framesPerPacket = 0 falls back to 10ms (480 frames at 48kHz).
		h.ProcessPDU(serverOpenPDU(0, 0))
		eventually(t, "open with 0 framesPerPacket", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(gotFrames) >= 1
		})
		// framesPerPacket = 0xFFFFFFFF is clamped to 1s (48000 frames at 48kHz).
		h.ProcessPDU(serverOpenPDU(0xffffffff, 0))
		eventually(t, "open with max framesPerPacket", func() bool {
			mu.Lock()
			defer mu.Unlock()
			return len(gotFrames) >= 2
		})

		mu.Lock()
		defer mu.Unlock()
		if gotFrames[0] != 480 || gotFrames[1] != 48000 {
			t.Fatalf("clamped framesPerPacket = %v, want [480 48000]", gotFrames)
		}
	})
}

func FuzzProcessPDU(f *testing.F) {
	f.Add(serverVersionPDU(2), serverFormatsPDU(pcmFormat(48000, 2)), serverOpenPDU(480, 0))
	f.Add(serverFormatsPDU(pcmFormat(16000, 1)), serverOpenPDU(0, 0), serverFormatChangePDU(0))
	f.Add([]byte{msgFormats, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0}, []byte{msgOpen, 0, 0, 0, 0, 0xff, 0xff, 0xff, 0xff}, []byte{msgFormatChange, 1, 2})

	f.Fuzz(func(t *testing.T, pdu1, pdu2, pdu3 []byte) {
		h := NewHandler(func([]byte) error { return nil }, func(uint32, uint16, uint32) (io.ReadCloser, error) {
			return io.NopCloser(bytes.NewReader(nil)), nil
		}, nil)
		h.ProcessPDU(pdu1)
		h.ProcessPDU(pdu2)
		h.ProcessPDU(pdu3)
		h.Reset()
		h.Close()
	})
}
