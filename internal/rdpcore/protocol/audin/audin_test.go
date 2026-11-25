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
	opens := 0
	h := NewHandler(cap.send, func(rate uint32, channels uint16, frames uint32) (io.ReadCloser, error) {
		opens++
		if opens == 1 {
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
	if opens != 1 {
		t.Fatalf("opens = %d, want 1", opens)
	}

	// Same rate & channels at index 1 keeps r1 open.
	cap.clear()
	h.ProcessPDU(serverFormatChangePDU(1))
	if opens != 1 || r1.isClosed() {
		t.Fatalf("format change with identical PCM shape reopened reader (opens=%d, closed=%v)", opens, r1.isClosed())
	}
	if sent := cap.sent(); len(sent) != 1 || sent[0][0] != msgFormatChange || binary.LittleEndian.Uint32(sent[0][1:5]) != 1 {
		t.Fatalf("sent = %v, want FORMAT_CHANGE(1)", sent)
	}

	// Different rate & channels at index 2 closes r1 and opens r2.
	cap.clear()
	h.ProcessPDU(serverFormatChangePDU(2))
	if opens != 2 || !r1.isClosed() {
		t.Fatalf("format change to 16kHz mono did not replace reader (opens=%d, r1Closed=%v)", opens, r1.isClosed())
	}
	if sent := cap.sent(); len(sent) != 1 || sent[0][0] != msgFormatChange || binary.LittleEndian.Uint32(sent[0][1:5]) != 2 {
		t.Fatalf("sent = %v, want FORMAT_CHANGE(2)", sent)
	}
	h.Close()
	if !r2.isClosed() {
		t.Fatal("Close did not close r2")
	}
}

func TestShortAndMalformedPDUsDoNotPanic(t *testing.T) {
	cap := &capture{}
	h := NewHandler(cap.send, func(uint32, uint16, uint32) (io.ReadCloser, error) {
		return newFakeReader(), nil
	}, nil)
	for _, pdu := range [][]byte{
		nil,
		{msgVersion},
		{msgVersion, 1, 2},
		{msgFormats},
		{msgFormats, 0xff, 0xff, 0xff, 0xff, 0, 0, 0, 0},
		{msgOpen},
		{msgOpen, 0, 0, 0, 0, 99, 0, 0, 0},
		{msgFormatChange},
		{msgFormatChange, 99, 0, 0, 0},
		{0x7f, 1, 2, 3},
	} {
		h.ProcessPDU(pdu)
	}
	h.Close()
}
