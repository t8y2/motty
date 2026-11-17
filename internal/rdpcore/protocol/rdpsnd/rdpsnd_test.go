package rdpsnd

import (
	"bytes"
	"encoding/binary"
	"errors"
	"io"
	"sync"
	"testing"
)

type capture struct {
	mu     sync.Mutex
	pdus   [][]byte
	writer *captureWriter
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

type captureWriter struct {
	mu     sync.Mutex
	data   []byte
	closed bool
	fail   error
}

func (w *captureWriter) Write(p []byte) (int, error) {
	if w.fail != nil {
		return 0, w.fail
	}
	w.mu.Lock()
	w.data = append(w.data, p...)
	w.mu.Unlock()
	return len(p), nil
}

func (w *captureWriter) Close() error {
	w.mu.Lock()
	w.closed = true
	w.mu.Unlock()
	return nil
}

func (w *captureWriter) played() []byte {
	w.mu.Lock()
	defer w.mu.Unlock()
	return append([]byte(nil), w.data...)
}

func pdu(t *testing.T, message byte, body []byte) []byte {
	t.Helper()
	out := make([]byte, sndProlog+len(body))
	out[0] = message
	binary.LittleEndian.PutUint16(out[2:4], uint16(len(body)))
	copy(out[sndProlog:], body)
	return out
}

func serverFormats(t *testing.T, version uint16, formats ...audioFormat) []byte {
	t.Helper()
	size := formatsHeader
	for _, format := range formats {
		size += audioFormatFixed + len(format.Extra)
	}
	body := make([]byte, size)
	binary.LittleEndian.PutUint16(body[14:16], uint16(len(formats)))
	binary.LittleEndian.PutUint16(body[17:19], version)
	offset := formatsHeader
	for _, format := range formats {
		offset += encodeFormat(body, offset, format)
	}
	return pdu(t, sndcFormats, body[:offset])
}

func pcm(rate uint32, channels uint16) audioFormat {
	return audioFormat{
		Tag:           waveFormatPCM,
		Channels:      channels,
		SampleRate:    rate,
		AvgBytes:      rate * uint32(channels) * 2,
		BlockAlign:    channels * 2,
		BitsPerSample: 16,
	}
}

func wave2(t *testing.T, timestamp uint16, block byte, audio []byte) []byte {
	t.Helper()
	body := make([]byte, 12+len(audio))
	binary.LittleEndian.PutUint16(body[0:2], timestamp)
	// wFormatNo = 0: the one format the client offered.
	body[4] = block
	copy(body[12:], audio)
	return pdu(t, sndcWave2, body)
}

func newTestHandler(t *testing.T) (*Handler, *capture) {
	t.Helper()
	captured := &capture{writer: &captureWriter{}}
	handler := NewHandler(captured.send, func(sampleRate uint32, channels uint16) (io.WriteCloser, error) {
		return captured.writer, nil
	}, nil)
	return handler, captured
}

func TestNegotiatesHighestQualityPCM(t *testing.T) {
	handler, captured := newTestHandler(t)
	handler.ProcessPDU(serverFormats(t, 8,
		pcm(44100, 2),
		pcm(48000, 2),
		audioFormat{Tag: 0x0002, Channels: 2, SampleRate: 44100, BitsPerSample: 4, BlockAlign: 1024},
		pcm(192000, 1),
	))
	sent := captured.sent()
	if len(sent) < 1 || sent[0][0] != sndcFormats {
		t.Fatalf("first response is not a formats PDU: %v", sent)
	}
	body := sent[0][sndProlog:]
	if got := binary.LittleEndian.Uint32(body[0:4]); got != tssndcapsAlive {
		t.Errorf("flags = %#x, want alive", got)
	}
	if got := binary.LittleEndian.Uint16(body[14:16]); got != 1 {
		t.Fatalf("formats = %d, want 1", got)
	}
	if got := binary.LittleEndian.Uint16(body[17:19]); got != 8 {
		t.Errorf("version = %d, want 8", got)
	}
	format, _, err := parseFormat(body, formatsHeader)
	if err != nil {
		t.Fatal(err)
	}
	if format.Tag != waveFormatPCM || format.SampleRate != 48000 || format.Channels != 2 || format.BitsPerSample != 16 {
		t.Errorf("offered format = %+v, want PCM 48000 stereo 16-bit", format)
	}
	// Version 8 requires the quality mode PDU after the formats.
	if len(sent) < 2 || sent[1][0] != sndcQualityMode {
		t.Errorf("quality mode PDU missing: %v", sent)
	}
}

func TestNegotiatesOneChannelWhenThePlayerRefusesStereo(t *testing.T) {
	captured := &capture{writer: &captureWriter{}}
	handler := NewHandler(captured.send, func(rate uint32, channels uint16) (io.WriteCloser, error) {
		if channels != 1 {
			return nil, errors.New("stereo is not available")
		}
		return captured.writer, nil
	}, nil)
	handler.ProcessPDU(serverFormats(t, 5, pcm(48000, 2), pcm(44100, 1)))
	body := captured.sent()[0][sndProlog:]
	format, _, err := parseFormat(body, formatsHeader)
	if err != nil {
		t.Fatal(err)
	}
	if format.Channels != 1 || format.SampleRate != 44100 {
		t.Errorf("offered format = %+v, want mono 44100", format)
	}
}

func TestNoPlayerOffersNoFormats(t *testing.T) {
	captured := &capture{}
	handler := NewHandler(captured.send, nil, nil)
	handler.ProcessPDU(serverFormats(t, 8, pcm(48000, 2)))
	body := captured.sent()[0][sndProlog:]
	if got := binary.LittleEndian.Uint16(body[14:16]); got != 0 {
		t.Errorf("formats = %d, want 0", got)
	}
	// The quality mode PDU follows every formats response in version 6+,
	// even one that offers nothing.
	if len(captured.sent()) != 2 {
		t.Errorf("responses = %d, want formats and quality mode", len(captured.sent()))
	}
}

func TestWave2PlaysAndConfirmsTwice(t *testing.T) {
	handler, captured := newTestHandler(t)
	handler.ProcessPDU(serverFormats(t, 8, pcm(48000, 2)))
	captured.pdus = nil
	audio := []byte{1, 2, 3, 4, 5, 6, 7, 8}
	handler.ProcessPDU(wave2(t, 42, 7, audio))
	if got := captured.writer.played(); !bytes.Equal(got, audio) {
		t.Errorf("played %v, want %v", got, audio)
	}
	sent := captured.sent()
	if len(sent) != 2 {
		t.Fatalf("confirmations = %d, want 2", len(sent))
	}
	for i, want := range []uint16{42, 42} {
		if sent[i][0] != sndcWaveConfirm {
			t.Fatalf("response %d is not a confirmation", i)
		}
		if got := binary.LittleEndian.Uint16(sent[i][4:6]); i == 0 && got != want {
			t.Errorf("first confirmation timestamp = %d, want %d", got, want)
		}
		if got := sent[i][6]; got != 7 {
			t.Errorf("confirmation block = %d, want 7", got)
		}
	}
}

func TestTwoPartWaveIsReassembled(t *testing.T) {
	handler, captured := newTestHandler(t)
	handler.ProcessPDU(serverFormats(t, 4, pcm(44100, 2)))
	captured.pdus = nil
	audio := []byte{1, 2, 3, 4, 5, 6, 7, 8, 9}
	info := make([]byte, 12)
	binary.LittleEndian.PutUint16(info[0:2], 9)
	info[4] = 2
	copy(info[8:12], audio[:4])
	handler.ProcessPDU(pdu(t, sndcWave, info))
	handler.ProcessPDU(pdu(t, sndcWave, append([]byte{0, 0, 0, 0}, audio[4:]...)))
	if got := captured.writer.played(); !bytes.Equal(got, audio) {
		t.Errorf("played %v, want %v", got, audio)
	}
	if sent := captured.sent(); len(sent) != 2 {
		t.Errorf("confirmations = %d, want 2", len(sent))
	}
}

func TestCloseStopsThePlayer(t *testing.T) {
	handler, captured := newTestHandler(t)
	handler.ProcessPDU(serverFormats(t, 8, pcm(48000, 2)))
	handler.Close()
	captured.writer.mu.Lock()
	closed := captured.writer.closed
	captured.writer.mu.Unlock()
	if !closed {
		t.Error("player was not closed")
	}
	handler.Close()
}

func TestServerCloseAllowsRenegotiation(t *testing.T) {
	handler, captured := newTestHandler(t)
	handler.ProcessPDU(serverFormats(t, 8, pcm(48000, 2)))
	handler.ProcessPDU(pdu(t, sndcClose, nil))
	captured.writer.mu.Lock()
	closed := captured.writer.closed
	captured.writer.mu.Unlock()
	if !closed {
		t.Error("player was not closed by SNDC_CLOSE")
	}
	second := &captureWriter{}
	handler.output = func(uint32, uint16) (io.WriteCloser, error) { return second, nil }
	handler.ProcessPDU(serverFormats(t, 8, pcm(44100, 2)))
	handler.ProcessPDU(wave2(t, 1, 0, []byte{1, 2, 3, 4}))
	if got := second.played(); !bytes.Equal(got, []byte{1, 2, 3, 4}) {
		t.Errorf("renegotiated player got %v", got)
	}
}

func TestWriterFailureDoesNotStopTheChannel(t *testing.T) {
	captured := &capture{writer: &captureWriter{fail: errors.New("gone")}}
	handler := NewHandler(captured.send, func(uint32, uint16) (io.WriteCloser, error) {
		return captured.writer, nil
	}, nil)
	handler.ProcessPDU(serverFormats(t, 8, pcm(48000, 2)))
	captured.pdus = nil
	handler.ProcessPDU(wave2(t, 1, 0, []byte{1, 2, 3, 4}))
	if sent := captured.sent(); len(sent) != 2 {
		t.Errorf("confirmations = %d after a player failure, want 2", len(sent))
	}
	// The player is gone, but the channel still confirms later waves.
	captured.pdus = nil
	handler.ProcessPDU(wave2(t, 2, 0, []byte{5, 6, 7, 8}))
	if sent := captured.sent(); len(sent) != 2 {
		t.Errorf("confirmations = %d after a second wave, want 2", len(sent))
	}
}

func TestShortPDUsAreIgnored(t *testing.T) {
	handler, _ := newTestHandler(t)
	for _, data := range [][]byte{
		nil,
		{0x07},
		{0x07, 0, 0, 0},
		{0x0D, 0, 0, 5, 1},
	} {
		handler.ProcessPDU(data)
	}
}
