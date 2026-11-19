// Package rdpsnd implements the audio output virtual channel of the Remote
// Desktop Protocol (MS-RDPEA) for receiving the server's sound over the
// "rdpsnd" static virtual channel or the AUDIO_PLAYBACK_DVC dynamic virtual
// channel (Windows 11 opens the latter).
//
// Only uncompressed PCM is negotiated. The server is sent the one format the
// caller can play, and a compressed format it offers is left unused; PCM keeps
// the client free of codecs and of the per-format state they need. ADPCM and
// the other compressed formats are not implemented.
package rdpsnd

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"sync"
	"time"
)

// PDU message types (MS-RDPEA 2.2.1).
const (
	sndcClose       byte = 0x01
	sndcWave        byte = 0x02
	sndcSetVolume   byte = 0x03
	sndcSetPitch    byte = 0x04
	sndcWaveConfirm byte = 0x05
	sndcTraining    byte = 0x06
	sndcFormats     byte = 0x07
	sndcQualityMode byte = 0x0C
	sndcWave2       byte = 0x0D
)

const (
	waveFormatPCM uint16 = 0x0001

	tssndcapsAlive  uint32 = 0x0001
	qualityModeHigh uint16 = 0x0002

	// sndProlog is msgType(1) + bPad(1) + bodySize(2).
	sndProlog = 4
	// formatsHeader is the fixed part of a Sound Formats body.
	formatsHeader = 20
	// audioFormatFixed is AUDIO_FORMAT without its variable extra data.
	audioFormatFixed = 18
	// maxClientVersion is the newest channel version this client implements.
	maxClientVersion = 8
	// qualityModeVersion is the version that requires a Quality Mode PDU.
	qualityModeVersion = 6
	// maxWaveBytes bounds one audio PDU. The server can send large blocks;
	// 1 MiB is far above any PCM block and keeps a malformed length from
	// asking for unbounded memory.
	maxWaveBytes = 1 << 20
)

// Output opens a player for the PCM the server will send. It is called during
// format negotiation, at most once per negotiated format, and the handler
// takes ownership of the writer: Close closes it. A nil Output asks the
// server for no audio.
type Output func(sampleRate uint32, channels uint16) (io.WriteCloser, error)

// Handler speaks the client side of the rdpsnd channel.
type Handler struct {
	send   func([]byte) error
	output Output
	log    *slog.Logger

	serverFormats []audioFormat
	version       uint16

	mu           sync.Mutex
	clientFormat *audioFormat
	out          io.WriteCloser
	closed       bool

	// pending holds the first wave PDU of a two-PDU SNDC_WAVE until its
	// continuation arrives.
	pending     bool
	pendingTS   uint16
	pendingNo   uint16
	pendingBlk  byte
	pendingHead [4]byte

	// audioBuf is the receive goroutine's scratch for the bytes of a wave
	// that span two PDUs. It is valid until the next PDU.
	audioBuf []byte
}

// audioFormat is AUDIO_FORMAT (MS-RDPEA 2.2.2.1).
type audioFormat struct {
	Tag           uint16
	Channels      uint16
	SampleRate    uint32
	AvgBytes      uint32
	BlockAlign    uint16
	BitsPerSample uint16
	Extra         []byte
}

// NewHandler returns a handler that writes before a PDU to send and opens a
// player through output. The logger may be nil.
func NewHandler(send func([]byte) error, output Output, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Handler{send: send, output: output, log: log}
}

// ProcessPDU handles one complete PDU from the rdpsnd channel.
func (h *Handler) ProcessPDU(data []byte) {
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed {
		return
	}
	if h.pending {
		h.waveContinuation(data)
		return
	}
	message, body, err := decode(data)
	if err != nil {
		h.log.Warn("rdpsnd: bad PDU", "error", err)
		return
	}
	switch message {
	case sndcFormats:
		h.serverFormatsPDU(body)
	case sndcTraining:
		h.training(body)
	case sndcWave:
		h.waveInfo(body)
	case sndcWave2:
		h.wave2(body)
	case sndcClose:
		h.closeChannel()
	case sndcQualityMode, sndcSetVolume, sndcSetPitch:
		// Quality is chosen by the server; volume and pitch are the local
		// system's, which this client does not override.
	default:
		h.log.Debug("rdpsnd: unknown PDU", "type", message, "bytes", len(body))
	}
}

// SetSendFn changes the channel replies go out on. The static rdpsnd channel
// and the AUDIO_PLAYBACK_DVC dynamic channel carry the same PDUs; a session
// may switch from one to the other, so the caller sets this before handing
// the handler data from a channel. It is called from the receive goroutine.
func (h *Handler) SetSendFn(send func([]byte) error) {
	h.send = send
}

// Reset forgets the negotiated format and releases the player, so that the
// channel can be negotiated again (for example when the server closes the
// dynamic channel and falls back to the static one).
func (h *Handler) Reset() {
	h.pending = false
	h.serverFormats = nil
	h.version = 0
	h.closePlayer()
}

// Close ends the channel for good and releases the player.
func (h *Handler) Close() {
	h.mu.Lock()
	h.closed = true
	h.mu.Unlock()
	h.closePlayer()
}

func (h *Handler) closeChannel() {
	h.closePlayer()
	h.clientFormat = nil
}

func (h *Handler) closePlayer() {
	h.mu.Lock()
	out := h.out
	h.out = nil
	h.clientFormat = nil
	h.mu.Unlock()
	if out != nil {
		out.Close()
	}
}

// setPlayer replaces the player, closing the one it replaces.
func (h *Handler) setPlayer(out io.WriteCloser, format *audioFormat) {
	h.mu.Lock()
	previous := h.out
	h.out = out
	h.clientFormat = format
	h.mu.Unlock()
	if previous != nil {
		previous.Close()
	}
}

func (h *Handler) serverFormatsPDU(body []byte) {
	if len(body) < formatsHeader {
		h.log.Warn("rdpsnd: formats PDU too short", "bytes", len(body))
		return
	}
	count := int(binary.LittleEndian.Uint16(body[14:16]))
	version := binary.LittleEndian.Uint16(body[17:19])
	h.version = min(version, maxClientVersion)

	h.serverFormats = h.serverFormats[:0]
	offset := formatsHeader
	for i := 0; i < count; i++ {
		format, used, err := parseFormat(body, offset)
		if err != nil {
			h.log.Warn("rdpsnd: unreadable server format", "index", i, "error", err)
			break
		}
		h.serverFormats = append(h.serverFormats, format)
		offset += used
	}

	h.closePlayer()
	chosen, writer := h.chooseFormat()
	if chosen != nil {
		h.setPlayer(writer, chosen)
		h.log.Debug("rdpsnd: negotiated PCM",
			"rate", chosen.SampleRate, "channels", chosen.Channels)
	}
	h.sendClientFormats()
	if h.version >= qualityModeVersion {
		h.sendQualityMode()
	}
}

// chooseFormat picks the best PCM format the caller's player accepts. Formats
// are tried from the best quality down, so a machine that cannot play one
// sample rate can still play another.
func (h *Handler) chooseFormat() (*audioFormat, io.WriteCloser) {
	candidates := make([]audioFormat, 0, len(h.serverFormats))
	for _, format := range h.serverFormats {
		if format.Tag != waveFormatPCM || format.BitsPerSample != 16 {
			continue
		}
		if format.Channels < 1 || format.Channels > 2 {
			continue
		}
		if format.SampleRate < 8000 || format.SampleRate > 192000 {
			continue
		}
		candidates = append(candidates, format)
	}
	// Higher score first: more channels, then a higher sample rate.
	for i := 1; i < len(candidates); i++ {
		for j := i; j > 0 && formatScore(candidates[j]) > formatScore(candidates[j-1]); j-- {
			candidates[j], candidates[j-1] = candidates[j-1], candidates[j]
		}
	}
	if h.output != nil {
		for i := range candidates {
			chosen := candidates[i]
			writer, err := h.output(chosen.SampleRate, chosen.Channels)
			if err == nil {
				chosen.AvgBytes = chosen.SampleRate * uint32(chosen.Channels) * 2
				chosen.BlockAlign = chosen.Channels * 2
				return &chosen, writer
			}
			h.log.Debug("rdpsnd: cannot play a server format",
				"rate", chosen.SampleRate, "channels", chosen.Channels, "error", err)
		}
	}
	return nil, nil
}

func formatScore(format audioFormat) int {
	return int(format.Channels)*1_000_000 + int(format.SampleRate)
}

func (h *Handler) sendClientFormats() {
	formats := []audioFormat(nil)
	if h.clientFormat != nil {
		formats = append(formats, *h.clientFormat)
	}
	size := formatsHeader
	for i := range formats {
		size += audioFormatFixed + len(formats[i].Extra)
	}
	body := make([]byte, size)
	binary.LittleEndian.PutUint32(body[0:4], tssndcapsAlive)
	binary.LittleEndian.PutUint16(body[14:16], uint16(len(formats)))
	binary.LittleEndian.PutUint16(body[17:19], h.version)
	offset := formatsHeader
	for i := range formats {
		offset += encodeFormat(body, offset, formats[i])
	}
	h.sendPDU(sndcFormats, body[:offset])
}

func (h *Handler) sendQualityMode() {
	var body [4]byte
	binary.LittleEndian.PutUint16(body[0:2], qualityModeHigh)
	h.sendPDU(sndcQualityMode, body[:])
}

// training echoes the server's training packet. PCM needs no training, but a
// server that sends it before choosing a format expects the confirmation.
func (h *Handler) training(body []byte) {
	if len(body) < 4 {
		return
	}
	h.sendPDU(sndcTraining, body[:4])
}

// waveInfo reads the header of a two-PDU SNDC_WAVE. The first four audio
// bytes travel in this PDU; the rest arrive in the continuation, whose own
// four-byte body prefix is padding to replace with them.
func (h *Handler) waveInfo(body []byte) {
	if len(body) < 12 {
		h.log.Warn("rdpsnd: wave info too short", "bytes", len(body))
		return
	}
	h.pending = true
	h.pendingTS = binary.LittleEndian.Uint16(body[0:2])
	h.pendingNo = binary.LittleEndian.Uint16(body[2:4])
	h.pendingBlk = body[4]
	copy(h.pendingHead[:], body[8:12])
}

func (h *Handler) waveContinuation(data []byte) {
	h.pending = false
	if len(data) < sndProlog {
		h.confirm(h.pendingTS, h.pendingBlk)
		return
	}
	body := data[sndProlog:]
	if len(body) < 4 || len(body) > maxWaveBytes {
		h.log.Warn("rdpsnd: unreadable wave continuation", "bytes", len(body))
		h.confirm(h.pendingTS, h.pendingBlk)
		return
	}
	h.audioBuf = grow(h.audioBuf, len(body))
	copy(h.audioBuf, body)
	copy(h.audioBuf[0:4], h.pendingHead[:])
	h.play(h.audioBuf, h.pendingTS, h.pendingNo, h.pendingBlk)
}

// wave2 reads a single SNDC_WAVE2 PDU (channel version 6 and newer).
func (h *Handler) wave2(body []byte) {
	if len(body) < 12 {
		h.log.Warn("rdpsnd: wave PDU too short", "bytes", len(body))
		return
	}
	timestamp := binary.LittleEndian.Uint16(body[0:2])
	formatNo := binary.LittleEndian.Uint16(body[2:4])
	block := body[4]
	audio := body[12:]
	if len(audio) > maxWaveBytes {
		h.log.Warn("rdpsnd: wave PDU too large", "bytes", len(audio))
		h.confirm(timestamp, block)
		return
	}
	h.play(audio, timestamp, formatNo, block)
}

// play confirms the PDU before writing — the server uses that first
// confirmation to measure network latency — then writes it and confirms again
// with the render time added, which the server uses to adapt.
func (h *Handler) play(audio []byte, timestamp, formatNo uint16, block byte) {
	start := time.Now()
	h.confirm(timestamp, block)
	h.mu.Lock()
	out := h.out
	negotiated := h.clientFormat != nil
	h.mu.Unlock()
	if !negotiated || formatNo != 0 {
		if len(audio) > 0 {
			h.log.Debug("rdpsnd: audio for an unoffered format", "format", formatNo)
		}
		h.confirm(timestamp, block)
		return
	}
	if out != nil && len(audio) > 0 {
		if _, err := out.Write(audio); err != nil {
			h.log.Warn("rdpsnd: player stopped", "error", err)
			h.closePlayer()
		}
	}
	elapsed := time.Since(start).Milliseconds()
	render := (uint32(timestamp) + uint32(elapsed)) & 0xFFFF
	h.confirm(uint16(render), block)
}

func (h *Handler) confirm(timestamp uint16, block byte) {
	var pdu [sndProlog + 4]byte
	pdu[0] = sndcWaveConfirm
	binary.LittleEndian.PutUint16(pdu[2:4], 4)
	binary.LittleEndian.PutUint16(pdu[4:6], timestamp)
	pdu[6] = block
	if err := h.send(pdu[:]); err != nil {
		h.log.Warn("rdpsnd: confirm failed", "error", err)
	}
}

func (h *Handler) sendPDU(message byte, body []byte) {
	buffer := make([]byte, sndProlog+len(body))
	buffer[0] = message
	binary.LittleEndian.PutUint16(buffer[2:4], uint16(len(body)))
	copy(buffer[sndProlog:], body)
	if err := h.send(buffer); err != nil {
		h.log.Warn("rdpsnd: send failed", "message", message, "error", err)
	}
}

func decode(data []byte) (byte, []byte, error) {
	if len(data) < sndProlog {
		return 0, nil, fmt.Errorf("PDU of %d bytes", len(data))
	}
	size := int(binary.LittleEndian.Uint16(data[2:4]))
	if sndProlog+size > len(data) {
		size = len(data) - sndProlog
	}
	return data[0], data[sndProlog : sndProlog+size], nil
}

func parseFormat(data []byte, offset int) (audioFormat, int, error) {
	if offset+audioFormatFixed > len(data) {
		return audioFormat{}, 0, fmt.Errorf("format header past the end")
	}
	format := audioFormat{
		Tag:           binary.LittleEndian.Uint16(data[offset:]),
		Channels:      binary.LittleEndian.Uint16(data[offset+2:]),
		SampleRate:    binary.LittleEndian.Uint32(data[offset+4:]),
		AvgBytes:      binary.LittleEndian.Uint32(data[offset+8:]),
		BlockAlign:    binary.LittleEndian.Uint16(data[offset+12:]),
		BitsPerSample: binary.LittleEndian.Uint16(data[offset+14:]),
	}
	extra := int(binary.LittleEndian.Uint16(data[offset+16:]))
	used := audioFormatFixed + extra
	if offset+used > len(data) {
		return audioFormat{}, 0, fmt.Errorf("format extra data past the end")
	}
	if extra > 0 {
		format.Extra = append([]byte(nil), data[offset+audioFormatFixed:offset+used]...)
	}
	return format, used, nil
}

func encodeFormat(data []byte, offset int, format audioFormat) int {
	binary.LittleEndian.PutUint16(data[offset:], format.Tag)
	binary.LittleEndian.PutUint16(data[offset+2:], format.Channels)
	binary.LittleEndian.PutUint32(data[offset+4:], format.SampleRate)
	binary.LittleEndian.PutUint32(data[offset+8:], format.AvgBytes)
	binary.LittleEndian.PutUint16(data[offset+12:], format.BlockAlign)
	binary.LittleEndian.PutUint16(data[offset+14:], format.BitsPerSample)
	binary.LittleEndian.PutUint16(data[offset+16:], uint16(len(format.Extra)))
	copy(data[offset+audioFormatFixed:], format.Extra)
	return audioFormatFixed + len(format.Extra)
}

func grow(buffer []byte, size int) []byte {
	if cap(buffer) < size {
		return make([]byte, size)
	}
	return buffer[:size]
}
