// Package audin implements the audio input virtual channel of the Remote
// Desktop Protocol (MS-RDPEAI) for sending captured microphone audio from the
// client to the server over the "AUDIO_INPUT" dynamic virtual channel.
//
// Only uncompressed 16-bit PCM is negotiated. Compressed formats offered by the
// server (such as ADPCM or AAC) are omitted from the client's format reply, so
// the server selects a PCM format the local capture device can produce directly.
//
// Recording runs on a dedicated goroutine owned by the active stream so that
// blocking audio reads never stall the RDP receive loop, and closing the
// channel or connection unblocks and joins that goroutine promptly.
package audin

import (
	"encoding/binary"
	"fmt"
	"io"
	"log/slog"
	"sync"
)

// ChannelName is the dynamic virtual channel name for audio input redirection.
const ChannelName = "AUDIO_INPUT"

// PDU message types (MS-RDPEAI 2.2).
const (
	msgVersion      byte = 0x01 // MSG_SNDIN_VERSION
	msgFormats      byte = 0x02 // MSG_SNDIN_FORMATS
	msgOpen         byte = 0x03 // MSG_SNDIN_OPEN
	msgOpenReply    byte = 0x04 // MSG_SNDIN_OPEN_REPLY
	msgDataIncoming byte = 0x05 // MSG_SNDIN_DATA_INCOMING
	msgData         byte = 0x06 // MSG_SNDIN_DATA
	msgFormatChange byte = 0x07 // MSG_SNDIN_FORMATCHANGE
)

const (
	waveFormatPCM uint16 = 0x0001

	// audioFormatFixed is the fixed header size of AUDIO_FORMAT (without cbSize extra bytes).
	audioFormatFixed = 18
	// maxClientVersion is the newest MS-RDPEAI version this client speaks.
	maxClientVersion uint32 = 2
	// maxServerFormats bounds the format list a malformed server PDU may claim.
	maxServerFormats = 256
	// maxPacketBytes caps a single outgoing audio packet buffer.
	maxPacketBytes = 1 << 20

	hresultOK   uint32 = 0x00000000
	hresultFail uint32 = 0x80004005 // E_FAIL
)

// Input opens a capture stream for the 16-bit signed little-endian PCM format
// chosen by the server. framesPerPacket is the frame count the server asked for
// in MSG_SNDIN_OPEN. The handler reads whole frames from the returned
// ReadCloser and closes it when recording stops or the channel closes.
type Input func(sampleRate uint32, channels uint16, framesPerPacket uint32) (io.ReadCloser, error)

// audioFormat is AUDIO_FORMAT (MS-RDPEAI 2.2.1 / MS-RDPEA 2.2.2.1).
type audioFormat struct {
	Tag           uint16
	Channels      uint16
	SampleRate    uint32
	AvgBytes      uint32
	BlockAlign    uint16
	BitsPerSample uint16
	Extra         []byte
}

// Handler speaks the client side of the AUDIO_INPUT dynamic virtual channel.
type Handler struct {
	input Input
	log   *slog.Logger

	sendMu sync.Mutex
	send   func([]byte) error

	mu              sync.Mutex
	clientFormats   []audioFormat
	activeFormat    *audioFormat
	framesPerPacket uint32
	rec             *recorder
	closed          bool
}

// NewHandler creates an audin handler using send to transmit PDUs and input to
// open the local microphone when the server starts recording.
func NewHandler(send func([]byte) error, input Input, log *slog.Logger) *Handler {
	if log == nil {
		log = slog.New(slog.DiscardHandler)
	}
	return &Handler{send: send, input: input, log: log}
}

// SetSendFn replaces the function used to send PDUs to the server.
// Called when the server opens a new AUDIO_INPUT dynamic virtual channel.
func (h *Handler) SetSendFn(send func([]byte) error) {
	h.sendMu.Lock()
	h.send = send
	h.sendMu.Unlock()
}

// ProcessPDU handles one complete MS-RDPEAI PDU from the AUDIO_INPUT channel.
func (h *Handler) ProcessPDU(data []byte) {
	h.mu.Lock()
	closed := h.closed
	h.mu.Unlock()
	if closed || len(data) < 1 {
		return
	}
	msgType := data[0]
	body := data[1:]
	switch msgType {
	case msgVersion:
		h.handleVersion(body)
	case msgFormats:
		h.handleFormats(body)
	case msgOpen:
		h.handleOpen(body)
	case msgFormatChange:
		h.handleFormatChange(body)
	default:
		h.log.Debug("audin: unknown PDU", "type", msgType, "bytes", len(body))
	}
}

// Reset stops any active recording and forgets negotiated formats so the
// handler is ready if the server reopens AUDIO_INPUT later in the session.
func (h *Handler) Reset() {
	rec := h.detachRecorder(false)
	if rec != nil {
		rec.close()
	}
}

// Close ends audio input for good and waits for any active capture goroutine
// to exit.
func (h *Handler) Close() {
	rec := h.detachRecorder(true)
	if rec != nil {
		rec.close()
	}
}

func (h *Handler) detachRecorder(closePermanent bool) *recorder {
	h.mu.Lock()
	if closePermanent {
		h.closed = true
	}
	rec := h.rec
	h.rec = nil
	h.activeFormat = nil
	h.framesPerPacket = 0
	if !closePermanent {
		h.clientFormats = nil
	}
	h.mu.Unlock()
	return rec
}

func (h *Handler) handleVersion(body []byte) {
	if len(body) < 4 {
		h.log.Warn("audin: version PDU too short", "bytes", len(body))
		return
	}
	serverVersion := binary.LittleEndian.Uint32(body[0:4])
	clientVersion := min(serverVersion, maxClientVersion)
	var pdu [5]byte
	pdu[0] = msgVersion
	binary.LittleEndian.PutUint32(pdu[1:5], clientVersion)
	if err := h.sendPDU(pdu[:]); err != nil {
		h.log.Warn("audin: failed to send version", "error", err)
	}
}

func (h *Handler) handleFormats(body []byte) {
	if len(body) < 8 {
		h.log.Warn("audin: formats PDU too short", "bytes", len(body))
		return
	}
	numFormats := binary.LittleEndian.Uint32(body[0:4])
	maxPossible := uint32((len(body) - 8) / audioFormatFixed)
	count := min(numFormats, min(maxPossible, maxServerFormats))

	accepted := make([]audioFormat, 0, count)
	offset := 8
	for range count {
		format, used, err := parseFormat(body, offset)
		if err != nil {
			h.log.Warn("audin: unreadable server format", "error", err)
			break
		}
		offset += used
		if h.input != nil && acceptablePCM(format) {
			accepted = append(accepted, normalizePCM(format))
		}
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	h.clientFormats = accepted
	h.mu.Unlock()

	h.sendClientFormats(accepted)
}

func acceptablePCM(format audioFormat) bool {
	if format.Tag != waveFormatPCM || format.BitsPerSample != 16 {
		return false
	}
	if format.Channels < 1 || format.Channels > 2 {
		return false
	}
	if format.SampleRate < 8000 || format.SampleRate > 192000 {
		return false
	}
	if format.BlockAlign != 0 && format.BlockAlign != format.Channels*2 {
		return false
	}
	return true
}

func normalizePCM(format audioFormat) audioFormat {
	blockAlign := format.Channels * 2
	return audioFormat{
		Tag:           waveFormatPCM,
		Channels:      format.Channels,
		SampleRate:    format.SampleRate,
		AvgBytes:      format.SampleRate * uint32(blockAlign),
		BlockAlign:    blockAlign,
		BitsPerSample: 16,
	}
}

func (h *Handler) sendClientFormats(formats []audioFormat) {
	fmtBytes := 0
	for i := range formats {
		fmtBytes += audioFormatFixed + len(formats[i].Extra)
	}
	total := 1 + 8 + fmtBytes
	pdu := make([]byte, total)
	pdu[0] = msgFormats
	binary.LittleEndian.PutUint32(pdu[1:5], uint32(len(formats)))
	binary.LittleEndian.PutUint32(pdu[5:9], uint32(total))
	offset := 9
	for i := range formats {
		offset += encodeFormat(pdu, offset, formats[i])
	}

	// Windows RDP server expects MSG_SNDIN_DATA_INCOMING before MSG_SNDIN_FORMATS.
	incoming := [1]byte{msgDataIncoming}
	h.sendMu.Lock()
	send := h.send
	var err error
	if send != nil {
		if err = send(incoming[:]); err == nil {
			err = send(pdu[:offset])
		}
	}
	h.sendMu.Unlock()
	if err != nil {
		h.log.Warn("audin: failed to send client formats", "error", err)
	}
}

func (h *Handler) handleOpen(body []byte) {
	if len(body) < 8 {
		h.log.Warn("audin: open PDU too short", "bytes", len(body))
		return
	}
	framesPerPacket := binary.LittleEndian.Uint32(body[0:4])
	formatIdx := binary.LittleEndian.Uint32(body[4:8])

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if int(formatIdx) >= len(h.clientFormats) || h.input == nil {
		h.mu.Unlock()
		h.log.Warn("audin: invalid open request", "formatIdx", formatIdx)
		h.sendOpenReply(hresultFail)
		return
	}
	format := h.clientFormats[formatIdx]
	oldRec := h.rec
	h.rec = nil
	h.activeFormat = nil
	h.mu.Unlock()

	if oldRec != nil {
		oldRec.close()
	}

	frames := boundFramesPerPacket(framesPerPacket, format.SampleRate, format.Channels)
	reader, err := h.input(format.SampleRate, format.Channels, frames)
	if err != nil {
		h.log.Debug("audin: failed to open input device",
			"rate", format.SampleRate, "channels", format.Channels, "error", err)
		h.sendOpenReply(hresultFail)
		return
	}

	packetBytes := int(frames) * int(format.Channels) * 2
	rec := &recorder{
		in:          reader,
		packetBytes: packetBytes,
		send:        h.sendAudioPacket,
		log:         h.log,
		done:        make(chan struct{}),
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		reader.Close()
		return
	}
	chosen := format
	h.activeFormat = &chosen
	h.framesPerPacket = frames
	h.rec = rec
	h.mu.Unlock()

	h.log.Debug("audin: recording started",
		"rate", format.SampleRate, "channels", format.Channels, "framesPerPacket", frames)

	h.sendFormatChange(formatIdx)
	h.sendOpenReply(hresultOK)
	go rec.run()
}

func (h *Handler) handleFormatChange(body []byte) {
	if len(body) < 4 {
		h.log.Warn("audin: format change PDU too short", "bytes", len(body))
		return
	}
	formatIdx := binary.LittleEndian.Uint32(body[0:4])

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		return
	}
	if int(formatIdx) >= len(h.clientFormats) || h.input == nil {
		h.mu.Unlock()
		h.log.Warn("audin: invalid format change index", "formatIdx", formatIdx)
		return
	}
	nextFormat := h.clientFormats[formatIdx]
	prevFormat := h.activeFormat
	frames := h.framesPerPacket
	hasRec := h.rec != nil
	if hasRec && prevFormat != nil &&
		prevFormat.SampleRate == nextFormat.SampleRate &&
		prevFormat.Channels == nextFormat.Channels {
		// Only nominal bitrate or index changed; keep the active stream running
		// without interrupting capture.
		chosen := nextFormat
		h.activeFormat = &chosen
		h.mu.Unlock()
		h.sendFormatChange(formatIdx)
		return
	}
	oldRec := h.rec
	h.rec = nil
	h.activeFormat = nil
	h.mu.Unlock()

	if oldRec != nil {
		oldRec.close()
	}

	frames = boundFramesPerPacket(frames, nextFormat.SampleRate, nextFormat.Channels)
	reader, err := h.input(nextFormat.SampleRate, nextFormat.Channels, frames)
	if err != nil {
		h.log.Debug("audin: failed to reopen input device on format change",
			"rate", nextFormat.SampleRate, "channels", nextFormat.Channels, "error", err)
		return
	}

	packetBytes := int(frames) * int(nextFormat.Channels) * 2
	rec := &recorder{
		in:          reader,
		packetBytes: packetBytes,
		send:        h.sendAudioPacket,
		log:         h.log,
		done:        make(chan struct{}),
	}

	h.mu.Lock()
	if h.closed {
		h.mu.Unlock()
		reader.Close()
		return
	}
	chosen := nextFormat
	h.activeFormat = &chosen
	h.framesPerPacket = frames
	h.rec = rec
	h.mu.Unlock()

	h.sendFormatChange(formatIdx)
	go rec.run()
}

func boundFramesPerPacket(frames uint32, sampleRate uint32, channels uint16) uint32 {
	if frames == 0 {
		frames = max(sampleRate/100, 1)
	}
	frameBytes := uint32(channels) * 2
	maxFrames := maxPacketBytes / frameBytes
	if frames > sampleRate {
		frames = sampleRate
	}
	if frames > maxFrames {
		frames = maxFrames
	}
	return max(frames, 1)
}

func (h *Handler) sendOpenReply(result uint32) {
	var pdu [5]byte
	pdu[0] = msgOpenReply
	binary.LittleEndian.PutUint32(pdu[1:5], result)
	if err := h.sendPDU(pdu[:]); err != nil {
		h.log.Warn("audin: failed to send open reply", "error", err)
	}
}

func (h *Handler) sendFormatChange(formatIdx uint32) {
	var pdu [5]byte
	pdu[0] = msgFormatChange
	binary.LittleEndian.PutUint32(pdu[1:5], formatIdx)
	if err := h.sendPDU(pdu[:]); err != nil {
		h.log.Warn("audin: failed to send format change", "error", err)
	}
}

func (h *Handler) sendPDU(pdu []byte) error {
	h.sendMu.Lock()
	send := h.send
	var err error
	if send != nil {
		err = send(pdu)
	}
	h.sendMu.Unlock()
	return err
}

func (h *Handler) sendAudioPacket(pdu []byte) error {
	incoming := [1]byte{msgDataIncoming}
	h.sendMu.Lock()
	send := h.send
	var err error
	if send != nil {
		if err = send(incoming[:]); err == nil {
			err = send(pdu)
		}
	}
	h.sendMu.Unlock()
	return err
}

type recorder struct {
	in          io.ReadCloser
	packetBytes int
	send        func([]byte) error
	log         *slog.Logger

	mu     sync.Mutex
	closed bool
	done   chan struct{}
}

func (r *recorder) run() {
	defer close(r.done)
	defer r.in.Close()

	buf := make([]byte, 1+r.packetBytes)
	buf[0] = msgData

	for {
		n, err := io.ReadFull(r.in, buf[1:])
		if n > 0 && err == nil {
			if sendErr := r.send(buf); sendErr != nil {
				r.log.Debug("audin: send audio packet failed", "error", sendErr)
				return
			}
			continue
		}
		if err != nil {
			r.mu.Lock()
			closed := r.closed
			r.mu.Unlock()
			if !closed {
				r.log.Debug("audin: capture stopped", "error", err)
			}
			return
		}
	}
}

func (r *recorder) close() {
	r.mu.Lock()
	already := r.closed
	r.closed = true
	r.mu.Unlock()
	if !already {
		_ = r.in.Close()
	}
	<-r.done
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
