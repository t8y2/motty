// Package cliprdr implements the MS-RDPECLIP (Remote Desktop Protocol:
// Clipboard Virtual Channel Extension) for bidirectional clipboard
// support over the "cliprdr" static virtual channel.
//
// Supports CF_UNICODETEXT (format ID 13), CF_DIB (format ID 8) and files in
// both directions (FileGroupDescriptorW and FileContents).
package cliprdr

import (
	"context"
	"encoding/binary"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"motty/internal/rdpcore/util"
)

// PDU message types (MS-RDPECLIP 2.2.1).
const (
	CBMonitorReady       uint16 = 0x0001
	CBFormatList         uint16 = 0x0002
	CBFormatListResponse uint16 = 0x0003
	CBFormatDataRequest  uint16 = 0x0004
	CBFormatDataResponse uint16 = 0x0005
	CBTempDirectory      uint16 = 0x0006
	CBClipCaps           uint16 = 0x0007
	CBLockClipData       uint16 = 0x000A
	CBUnlockClipData     uint16 = 0x000B
)

// PDU flags.
const (
	CBResponseOK   uint16 = 0x0001
	CBResponseFail uint16 = 0x0002
)

// Capability set constants (MS-RDPECLIP 2.2.2.2.1).
const (
	CBCapsGeneral            uint16 = 0x0001
	CBCapsVersion2           uint32 = 0x00000002
	CBUseLongFormatNames     uint32 = 0x00000002
	CBStreamFileclipEnabled  uint32 = 0x00000004
	CBFileclipNoFilePaths    uint32 = 0x00000008
	CBCanLockClipData        uint32 = 0x00000010
	CBHugeFileSupportEnabled uint32 = 0x00000020
)

// Standard clipboard format IDs (MS-RDPECLIP 2.2.3.1).
const (
	CFDIB         uint32 = 8
	CFUnicodeText uint32 = 13
)

// PDU header size: msgType(2) + msgFlags(2) + dataLen(4).
const pduHeaderSize = 8

// Short format name entry size: formatId(4) + formatName[32] = 36.
const shortFormatEntrySize = 36

// FormatListEntry represents one clipboard format in a CB_FORMAT_LIST PDU.
type FormatListEntry struct {
	FormatID   uint32
	FormatName string
}

// encodePDU builds a cliprdr PDU: 8-byte header + payload.
func encodePDU(msgType, msgFlags uint16, payload []byte) []byte {
	buf := make([]byte, pduHeaderSize+len(payload))
	binary.LittleEndian.PutUint16(buf[0:2], msgType)
	binary.LittleEndian.PutUint16(buf[2:4], msgFlags)
	binary.LittleEndian.PutUint32(buf[4:8], uint32(len(payload)))
	copy(buf[pduHeaderSize:], payload)
	return buf
}

// decodePDU parses a cliprdr PDU header and returns the payload slice.
func decodePDU(data []byte) (msgType, msgFlags uint16, payload []byte, err error) {
	if len(data) < pduHeaderSize {
		return 0, 0, nil, fmt.Errorf("cliprdr: PDU too short (%d bytes)", len(data))
	}
	msgType = binary.LittleEndian.Uint16(data[0:2])
	msgFlags = binary.LittleEndian.Uint16(data[2:4])
	dataLen := binary.LittleEndian.Uint32(data[4:8])
	end := min(pduHeaderSize+int(dataLen), len(data))
	return msgType, msgFlags, data[pduHeaderSize:end], nil
}

// encodeCapsPDU builds a CB_CLIP_CAPS PDU (header + 16 bytes payload).
// Payload: cCapabilitiesSets(2) + pad(2) + capType(2) + length(2) + version(4) + generalFlags(4).
func encodeCapsPDU(version, flags uint32) []byte {
	var payload [16]byte
	binary.LittleEndian.PutUint16(payload[0:2], 1) // cCapabilitiesSets
	// payload[2:4] = pad (zero)
	binary.LittleEndian.PutUint16(payload[4:6], uint16(CBCapsGeneral)) // capType
	binary.LittleEndian.PutUint16(payload[6:8], 12)                    // length (capType+length+version+flags)
	binary.LittleEndian.PutUint32(payload[8:12], version)
	binary.LittleEndian.PutUint32(payload[12:16], flags)
	return encodePDU(CBClipCaps, 0, payload[:])
}

// decodeCapsPDU parses the payload of a CB_CLIP_CAPS PDU.
func decodeCapsPDU(payload []byte) (version, flags uint32, err error) {
	if len(payload) < 16 {
		return 0, 0, fmt.Errorf("cliprdr: caps payload too short (%d bytes)", len(payload))
	}
	version = binary.LittleEndian.Uint32(payload[8:12])
	flags = binary.LittleEndian.Uint32(payload[12:16])
	return version, flags, nil
}

// encodeFormatListLong builds a CB_FORMAT_LIST PDU using long format names.
// Each entry: formatId(4) + wszFormatName(null-terminated UTF-16LE).
// Standard formats with no custom name use just 2 null bytes for the name.
func encodeFormatListLong(formats []FormatListEntry) []byte {
	// Calculate payload size
	size := 0
	for _, f := range formats {
		size += 4 // formatId
		if f.FormatName == "" {
			size += 2 // empty null-terminated UTF-16LE
		} else {
			size += len(encodeUTF16LE(f.FormatName)) + 2 // name + null terminator
		}
	}
	payload := make([]byte, size)
	off := 0
	for _, f := range formats {
		binary.LittleEndian.PutUint32(payload[off:], f.FormatID)
		off += 4
		if f.FormatName == "" {
			// 2 null bytes (empty null-terminated UTF-16LE string)
			off += 2
		} else {
			nameBytes := encodeUTF16LE(f.FormatName)
			copy(payload[off:], nameBytes)
			off += len(nameBytes) + 2 // +2 for null terminator (already zero)
		}
	}
	return encodePDU(CBFormatList, 0, payload)
}

// encodeFormatListShort builds a CB_FORMAT_LIST PDU using short format names.
// Each entry: formatId(4) + formatName[32] = 36 bytes fixed.
// This is the legacy format used when CB_USE_LONG_FORMAT_NAMES is not negotiated.
func encodeFormatListShort(formats []FormatListEntry) []byte {
	payload := make([]byte, shortFormatEntrySize*len(formats))
	for i, f := range formats {
		off := i * shortFormatEntrySize
		binary.LittleEndian.PutUint32(payload[off:], f.FormatID)
		// formatName[32] is already zeroed; copy ASCII name if present
		if f.FormatName != "" {
			n := copy(payload[off+4:off+shortFormatEntrySize], f.FormatName)
			_ = n // null-padded by zeroed slice
		}
	}
	return encodePDU(CBFormatList, 0, payload)
}

// decodeFormatListLong parses a CB_FORMAT_LIST payload (long format names).
func decodeFormatListLong(payload []byte) []FormatListEntry {
	var entries []FormatListEntry
	off := 0
	for off+4 <= len(payload) {
		formatID := binary.LittleEndian.Uint32(payload[off:])
		off += 4
		// Find null terminator (2 zero bytes on u16 boundary)
		nameStart := off
		for off+1 < len(payload) {
			if payload[off] == 0 && payload[off+1] == 0 {
				break
			}
			off += 2
		}
		name := decodeUTF16LE(payload[nameStart:off])
		off += 2 // skip null terminator
		entries = append(entries, FormatListEntry{FormatID: formatID, FormatName: name})
	}
	return entries
}

// decodeFormatListShort parses a CB_FORMAT_LIST payload (short format names).
// Each entry: formatId(4) + formatName[32] = 36 bytes fixed.
func decodeFormatListShort(payload []byte) []FormatListEntry {
	var entries []FormatListEntry
	for off := 0; off+shortFormatEntrySize <= len(payload); off += shortFormatEntrySize {
		formatID := binary.LittleEndian.Uint32(payload[off:])
		// formatName is 32 bytes of null-padded ASCII
		nameBytes := payload[off+4 : off+shortFormatEntrySize]
		// Trim trailing nulls
		end := 0
		for end < len(nameBytes) && nameBytes[end] != 0 {
			end++
		}
		name := string(nameBytes[:end])
		entries = append(entries, FormatListEntry{FormatID: formatID, FormatName: name})
	}
	return entries
}

// encodeFormatDataRequest builds a CB_FORMAT_DATA_REQUEST PDU.
func encodeFormatDataRequest(formatID uint32) []byte {
	var payload [4]byte
	binary.LittleEndian.PutUint32(payload[:], formatID)
	return encodePDU(CBFormatDataRequest, 0, payload[:])
}

// encodeFormatDataResponse builds a CB_FORMAT_DATA_RESPONSE PDU.
func encodeFormatDataResponse(ok bool, data []byte) []byte {
	flags := CBResponseOK
	if !ok {
		flags = CBResponseFail
	}
	return encodePDU(CBFormatDataResponse, flags, data)
}

// Handler manages the cliprdr protocol over the "cliprdr" static virtual channel.
//
// PDUs arrive on the connection's read goroutine, while the Set and Request
// methods are called from the application's; mu guards the state they share.
// It is never held while a PDU is sent.
type Handler struct {
	sendFn func([]byte) error
	log    *slog.Logger

	mu                 sync.Mutex
	serverCaps         uint32                   // generalFlags from server
	useLongFormatNames bool                     // both sides negotiated CB_USE_LONG_FORMAT_NAMES
	ready              bool                     // true after monitor-ready handshake
	enabled            bool                     // runtime toggle; when false, suppress callbacks and outbound data
	localText          string                   // current local clipboard content (UTF-8)
	localImage         []byte                   // current local clipboard image (PNG-encoded)
	files              *fileSnapshot            // the files copied on this computer, when the clipboard holds them
	locked             map[uint32]*fileSnapshot // lists the server holds locked, by clipDataId
	remoteFormats      []FormatListEntry        // last format list from server
	remoteFileListID   uint32                   // the server's FileGroupDescriptorW format, 0 when it offered none
	remoteContentsID   uint32                   // the server's FileContents format
	remoteHasFiles     bool
	// The server's clipboard data may be locked while its files are read, so a
	// copy made meanwhile cannot change the list under a paste. remoteLockID is
	// the id sent in CB_LOCK_CLIPDATA, 0 when nothing is locked.
	remoteLockID   uint32
	remoteLockNext uint32
	// Format data requests have no id in the protocol, and a response carries
	// none. The server answers in order, so the oldest outstanding request is
	// the one a response belongs to. pendingData holds them in that order; the
	// four pending* fields mirror its head for the rest of this file.
	pendingData        []formatDataRequest
	pendingNextID      uint64
	pendingReq         bool
	pendingFormat      uint32
	pendingFiles       bool
	pendingGeneration  uint64
	onRemoteCopy       func(hasText, hasImage, hasFiles bool)
	onTextData         func(text string)
	onImageData        func(pngData []byte)
	onFilesData        func(RemoteFiles, error)
	fileResponses      chan fileContentsResponse
	fileStream         uint32 // last streamId used for a file contents request; guarded by downloadMu
	downloadSerial     uint64 // identifies the newest download; guarded by mu
	downloadClipID     uint32 // the lock the newest download reads; guarded by mu
	downloadMu         sync.Mutex
	downloadCancel     context.CancelFunc
	downloadGeneration uint64
}

// formatDataRequest is one CB_FORMAT_DATA_REQUEST whose response has not
// arrived yet. files marks a request for the server's file list; generation is
// the remote clipboard it belongs to, so a response to a list that has since
// changed is dropped instead of being applied to the newer clipboard.
type formatDataRequest struct {
	id         uint64
	format     uint32
	files      bool
	clipID     uint32 // the locked clipboard data the file list belongs to, 0 when unlocked
	generation uint64
	sent       time.Time
}

// encodeLockClipData builds a CB_LOCK_CLIPDATA or CB_UNLOCK_CLIPDATA PDU. The
// payload is the clipboard data's id.
func encodeLockClipData(kind uint16, id uint32) []byte {
	var payload [4]byte
	binary.LittleEndian.PutUint32(payload[:], id)
	return encodePDU(kind, 0, payload[:])
}

// Limits of the outstanding-request queue. The queue exists to match responses
// that arrive after the clipboard changed under them; a normal clipboard update
// has one request in flight, not dozens.
const (
	maxPendingDataRequests = 16
	// formatDataTimeout discards a request a server never answered, so one lost
	// response cannot shift every later one onto the wrong data type.
	formatDataTimeout = 60 * time.Second
)

// NewHandler creates a cliprdr handler.
// sendFn writes data to the "cliprdr" static virtual channel.
func NewHandler(sendFn func([]byte) error, log *slog.Logger) *Handler {
	return &Handler{
		sendFn: sendFn, log: log, enabled: true,
		fileResponses: make(chan fileContentsResponse, 8),
	}
}

// syncPendingLocked mirrors the oldest outstanding request into the pending*
// fields. It must be called whenever pendingData changes.
func (h *Handler) syncPendingLocked() {
	h.pendingReq = len(h.pendingData) > 0
	if !h.pendingReq {
		h.pendingFormat, h.pendingFiles, h.pendingGeneration = 0, false, 0
		return
	}
	head := h.pendingData[0]
	h.pendingFormat, h.pendingFiles, h.pendingGeneration = head.format, head.files, head.generation
}

// dropExpiredDataRequestsLocked forgets requests the server did not answer in
// time, so a later response is not taken for one of them.
func (h *Handler) dropExpiredDataRequestsLocked(now time.Time) {
	drop := 0
	for drop < len(h.pendingData) && now.Sub(h.pendingData[drop].sent) > formatDataTimeout {
		drop++
	}
	if drop == 0 {
		return
	}
	h.pendingData = append(h.pendingData[:0], h.pendingData[drop:]...)
	h.syncPendingLocked()
}

// enqueueDataRequestLocked records a request before it is sent. It reports
// false when too many requests are already outstanding; the caller then does
// not send it rather than let a response land on the wrong request.
func (h *Handler) enqueueDataRequestLocked(format uint32, files bool, clipID uint32) (formatDataRequest, bool) {
	now := time.Now()
	h.dropExpiredDataRequestsLocked(now)
	if len(h.pendingData) >= maxPendingDataRequests {
		return formatDataRequest{}, false
	}
	h.pendingNextID++
	request := formatDataRequest{
		id: h.pendingNextID, format: format, files: files,
		clipID: clipID, generation: h.downloadGeneration, sent: now,
	}
	h.pendingData = append(h.pendingData, request)
	h.syncPendingLocked()
	return request, true
}

// removeDataRequestLocked forgets a request that could not be sent.
func (h *Handler) removeDataRequestLocked(id uint64) {
	for index, request := range h.pendingData {
		if request.id == id {
			h.pendingData = append(h.pendingData[:index], h.pendingData[index+1:]...)
			break
		}
	}
	h.syncPendingLocked()
}

// popDataRequestLocked takes the request the next response belongs to. The
// fallback keeps the direct internal state tests set working.
func (h *Handler) popDataRequestLocked() (formatDataRequest, bool) {
	if len(h.pendingData) > 0 {
		request := h.pendingData[0]
		h.pendingData = h.pendingData[1:]
		h.syncPendingLocked()
		return request, true
	}
	if !h.pendingReq {
		return formatDataRequest{}, false
	}
	request := formatDataRequest{format: h.pendingFormat, files: h.pendingFiles, generation: h.pendingGeneration}
	h.pendingReq = false
	h.pendingFormat, h.pendingFiles, h.pendingGeneration = 0, false, 0
	return request, true
}

// SetEnabled toggles clipboard forwarding at runtime. When disabled the
// handler still ACKs format lists and rejects data requests (keeping the
// protocol valid) but suppresses all callbacks and outbound clipboard data.
// When re-enabled after being disabled, a fresh CB_FORMAT_LIST is sent to
// re-announce available formats.
func (h *Handler) SetEnabled(enabled bool) {
	h.mu.Lock()
	was := h.enabled
	h.enabled = enabled
	var cancel context.CancelFunc
	var unlock uint32
	if !enabled {
		h.dropFilesLocked()
		h.downloadGeneration++
		cancel = h.downloadCancel
		h.downloadCancel = nil
		unlock = h.takeRemoteLockLocked()
		// Responses to requests sent while sharing was on are not this side's
		// clipboard any more; re-enabling starts with a fresh format list.
		h.pendingData = h.pendingData[:0]
		h.syncPendingLocked()
	}
	ready := h.ready
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if unlock != 0 {
		h.sendResponse(encodeLockClipData(CBUnlockClipData, unlock))
	}
	if enabled && !was && ready {
		h.sendFormatList()
	}
}

// Enabled returns the current enabled state.
func (h *Handler) Enabled() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.enabled
}

// OnRemoteCopy sets the callback invoked when the server's clipboard changes.
// hasText is true if CF_UNICODETEXT is among the advertised formats.
// hasImage is true if CF_DIB is among the advertised formats.
func (h *Handler) OnRemoteCopy(fn func(hasText, hasImage, hasFiles bool)) {
	h.mu.Lock()
	h.onRemoteCopy = fn
	h.mu.Unlock()
}

// OnRemoteFiles sets the callback invoked when the files the server put on its
// clipboard have arrived on this computer. It runs on a goroutine of its own;
// an error says why they could not all be read.
func (h *Handler) OnRemoteFiles(fn func(RemoteFiles, error)) {
	h.mu.Lock()
	h.onFilesData = fn
	h.mu.Unlock()
}

// OnTextData sets the callback invoked when the server responds with clipboard text data.
func (h *Handler) OnTextData(fn func(text string)) {
	h.mu.Lock()
	h.onTextData = fn
	h.mu.Unlock()
}

// OnImageData sets the callback invoked when the server responds with
// clipboard image data. The pngData is PNG-encoded.
func (h *Handler) OnImageData(fn func(pngData []byte)) {
	h.mu.Lock()
	h.onImageData = fn
	h.mu.Unlock()
}

// Close forgets the copied files and the lists the server held. The handler is
// not used afterwards.
func (h *Handler) Close() {
	h.mu.Lock()
	h.dropFilesLocked()
	clear(h.locked)
	h.downloadGeneration++
	cancel := h.downloadCancel
	h.downloadCancel = nil
	unlock := h.takeRemoteLockLocked()
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if unlock != 0 {
		// Best effort: the connection may already be closing.
		h.sendFn(encodeLockClipData(CBUnlockClipData, unlock))
	}
}

// takeRemoteLockLocked forgets the remote clipboard data this side locked and
// returns its id, 0 when nothing was locked. The caller sends the unlock.
func (h *Handler) takeRemoteLockLocked() uint32 {
	id := h.remoteLockID
	h.remoteLockID = 0
	return id
}

// ProcessPDU dispatches an incoming cliprdr PDU.
func (h *Handler) ProcessPDU(data []byte) {
	msgType, msgFlags, payload, err := decodePDU(data)
	if err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "failed to decode PDU", slog.Any("error", err))
		return
	}
	switch msgType {
	case CBClipCaps:
		h.handleCaps(payload)
	case CBMonitorReady:
		h.handleMonitorReady()
	case CBFormatList:
		h.handleFormatList(payload)
	case CBFormatListResponse:
		h.handleFormatListResponse(msgFlags)
	case CBFormatDataRequest:
		h.handleFormatDataRequest(payload)
	case CBFormatDataResponse:
		h.handleFormatDataResponse(msgFlags, payload)
	case CBFileContentsRequest:
		h.handleFileContentsRequest(payload)
	case CBFileContentsResponse:
		h.handleFileContentsResponse(msgFlags, payload)
	case CBLockClipData:
		if len(payload) >= 4 {
			h.lock(binary.LittleEndian.Uint32(payload))
		}
	case CBUnlockClipData:
		if len(payload) >= 4 {
			h.unlock(binary.LittleEndian.Uint32(payload))
		}
	default:
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "unknown PDU type", util.Hex4("type", msgType))
	}
}

func (h *Handler) handleCaps(payload []byte) {
	version, flags, err := decodeCapsPDU(payload)
	if err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "failed to decode caps", slog.Any("error", err))
		return
	}
	h.mu.Lock()
	h.serverCaps = flags
	h.mu.Unlock()
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "server caps", slog.Int("version", int(version)), util.Hex8("flags", flags))
}

// clientCaps are the capabilities this side offers: long format names, and the
// file transfer ones. A capability is in force when the server offers it too.
const clientCaps = CBUseLongFormatNames | CBStreamFileclipEnabled | CBFileclipNoFilePaths | CBCanLockClipData | CBHugeFileSupportEnabled

func (h *Handler) handleMonitorReady() {
	h.mu.Lock()
	h.log.LogAttrs(context.Background(), slog.LevelInfo, "monitor ready", util.Hex8("serverCaps", h.serverCaps))
	h.ready = true

	// Negotiate long format names if server supports it (MS-RDPECLIP 2.2.2.2).
	// Modern Windows servers with CB_USE_LONG_FORMAT_NAMES silently reject
	// short format lists. Send CB_CLIP_CAPS to confirm our capabilities.
	if h.serverCaps&CBUseLongFormatNames != 0 {
		h.useLongFormatNames = true
	}
	h.mu.Unlock()

	// Send CB_CLIP_CAPS with our supported flags.
	capsPDU := encodeCapsPDU(CBCapsVersion2, clientCaps)
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "sending client caps", slog.Int("version", 2), util.Hex8("flags", clientCaps))
	if err := h.sendFn(capsPDU); err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send caps", slog.Any("error", err))
	}

	// Send the initial format list.
	h.sendFormatList()
}

// advertisedFormatsLocked returns the format list entries to advertise: the
// file formats while the clipboard holds copied files, otherwise text and image
// regardless of the current local content.
func (h *Handler) advertisedFormatsLocked() []FormatListEntry {
	if h.files != nil {
		return []FormatListEntry{
			{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
			{FormatID: formatIDFileContents, FormatName: FormatFileContents},
		}
	}
	return []FormatListEntry{
		{FormatID: CFUnicodeText},
		{FormatID: CFDIB},
	}
}

// formatListPDU is the CB_FORMAT_LIST for what the clipboard holds now.
func (h *Handler) formatListPDU() []byte {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.encodeFormatListLocked(h.advertisedFormatsLocked())
}

// sendFormatList sends a CB_FORMAT_LIST PDU with our advertised formats.
func (h *Handler) sendFormatList() {
	fmtList := h.formatListPDU()
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "sending format list", slog.Int("bytes", len(fmtList)))
	if err := h.sendFn(fmtList); err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send format list", slog.Any("error", err))
	}
}

// encodeFormatListLocked encodes using long or short format names based on negotiation.
func (h *Handler) encodeFormatListLocked(formats []FormatListEntry) []byte {
	if h.useLongFormatNames {
		return encodeFormatListLong(formats)
	}
	return encodeFormatListShort(formats)
}

// decodeFormatListLocked decodes using long or short format names based on negotiation.
func (h *Handler) decodeFormatListLocked(payload []byte) []FormatListEntry {
	if h.useLongFormatNames {
		return decodeFormatListLong(payload)
	}
	return decodeFormatListShort(payload)
}

func (h *Handler) handleFormatList(payload []byte) {
	h.mu.Lock()
	h.remoteFormats = h.decodeFormatListLocked(payload)
	formats := h.remoteFormats
	enabled := h.enabled
	callback := h.onRemoteCopy
	// The server's clipboard changed: a file list being read is now stale.
	cancel := h.downloadCancel
	h.downloadGeneration++
	unlock := h.takeRemoteLockLocked()
	h.mu.Unlock()
	if cancel != nil {
		cancel()
	}
	if unlock != 0 {
		// The list this side locked may still be read from, but the remote
		// clipboard is now something else; release it.
		h.sendResponse(encodeLockClipData(CBUnlockClipData, unlock))
	}
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "server format list", slog.Int("formats", len(formats)))

	// Always ACK the format list to keep the protocol valid.
	if err := h.sendFn(encodePDU(CBFormatListResponse, CBResponseOK, nil)); err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send format list response", slog.Any("error", err))
		return
	}

	// Suppress callback when disabled.
	if !enabled {
		return
	}

	hasText := false
	hasImage := false
	hasFiles := false
	fileListID, contentsID := uint32(0), uint32(0)
	for _, f := range formats {
		switch {
		case f.FormatID == CFUnicodeText:
			hasText = true
		case f.FormatID == CFDIB:
			hasImage = true
		case f.FormatName == FormatFileGroupDescriptorW:
			fileListID = f.FormatID
		case f.FormatName == FormatFileContents:
			contentsID = f.FormatID
		}
	}
	hasFiles = fileListID != 0 && contentsID != 0
	h.mu.Lock()
	h.remoteFileListID, h.remoteContentsID, h.remoteHasFiles = fileListID, contentsID, hasFiles
	h.mu.Unlock()
	if callback != nil {
		callback(hasText, hasImage, hasFiles)
	}
}

func (h *Handler) handleFormatListResponse(flags uint16) {
	if flags&CBResponseOK == 0 {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "server rejected our format list")
	}
}

func (h *Handler) handleFormatDataRequest(payload []byte) {
	if len(payload) < 4 {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "format data request too short")
		return
	}
	formatID := binary.LittleEndian.Uint32(payload[0:4])
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "server requests format", slog.Int("formatID", int(formatID)))

	h.mu.Lock()
	enabled, text, image, files := h.enabled, h.localText, h.localImage, h.files
	h.mu.Unlock()
	// The entries of a list never change, so the list is built outside the lock.
	var fileList []byte
	if enabled && formatID == formatIDFileGroupDescriptorW && files != nil {
		fileList = encodeFileList(files.entries)
	}

	// When disabled, reject all data requests.
	if !enabled {
		h.sendResponse(encodeFormatDataResponse(false, nil))
		return
	}

	switch {
	case formatID == CFUnicodeText && text != "":
		h.sendResponse(encodeFormatDataResponse(true, textToWire(text)))
	case formatID == CFDIB && len(image) > 0:
		dib, err := pngToDIB(image)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "pngToDIB failed", slog.Any("error", err))
			h.sendResponse(encodeFormatDataResponse(false, nil))
			return
		}
		h.sendResponse(encodeFormatDataResponse(true, dib))
	case fileList != nil:
		h.sendResponse(encodeFormatDataResponse(true, fileList))
	default:
		h.sendResponse(encodeFormatDataResponse(false, nil))
	}
}

// sendResponse sends a PDU that answers the server, logging a failure.
func (h *Handler) sendResponse(pdu []byte) {
	if err := h.sendFn(pdu); err != nil {
		h.log.LogAttrs(context.Background(), slog.LevelError, "failed to send response", slog.Any("error", err))
	}
}

func (h *Handler) handleFormatDataResponse(flags uint16, payload []byte) {
	h.mu.Lock()
	h.dropExpiredDataRequestsLocked(time.Now())
	request, pending := h.popDataRequestLocked()
	enabled, onImage, onText := h.enabled, h.onImageData, h.onTextData
	stale := request.files && request.generation != h.downloadGeneration
	h.mu.Unlock()

	if !pending {
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "format data response without a request")
		return
	}
	if stale {
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "stale file list response dropped")
		return
	}
	if flags&CBResponseOK == 0 {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "server returned format data error")
		if request.files {
			h.releaseRemoteLockIfIdle(request.clipID)
			h.finishRemoteFiles(request.generation, RemoteFiles{}, errFileListRefused)
		}
		return
	}

	// Suppress callback when disabled.
	if !enabled {
		return
	}

	if request.files {
		files, err := decodeRemoteFileList(payload)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelWarn, "server file list refused", slog.Any("error", err))
			h.releaseRemoteLockIfIdle(request.clipID)
			h.finishRemoteFiles(request.generation, RemoteFiles{}, err)
			return
		}
		h.startRemoteFiles(files, request.generation, request.clipID)
		return
	}

	switch request.format {
	case CFDIB:
		pngData, err := dibToPNG(payload)
		if err != nil {
			h.log.LogAttrs(context.Background(), slog.LevelError, "dibToPNG failed", slog.Any("error", err))
			return
		}
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "received image", slog.Int("pngBytes", len(pngData)))
		if onImage != nil {
			onImage(pngData)
		}
	default:
		text := textFromWire(payload)
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "received text", slog.Int("chars", len(text)))
		if onText != nil {
			onText(text)
		}
	}
}

// SetLocalClipboard updates the local clipboard text and notifies the server.
func (h *Handler) SetLocalClipboard(text string) error {
	h.mu.Lock()
	if !h.enabled {
		h.mu.Unlock()
		return nil
	}
	h.localText = text
	h.dropFilesLocked()
	pdu := h.encodeFormatListLocked(h.advertisedFormatsLocked())
	h.mu.Unlock()
	return h.sendFn(pdu)
}

// SetLocalImage updates the local clipboard image (PNG-encoded) and
// notifies the server that CF_DIB is available.
func (h *Handler) SetLocalImage(pngData []byte) error {
	h.mu.Lock()
	if !h.enabled {
		h.mu.Unlock()
		return nil
	}
	h.localImage = pngData
	h.dropFilesLocked()
	pdu := h.encodeFormatListLocked(h.advertisedFormatsLocked())
	h.mu.Unlock()
	return h.sendFn(pdu)
}

// RequestRemoteText sends a CB_FORMAT_DATA_REQUEST for CF_UNICODETEXT.
// The result is delivered asynchronously via the OnTextData callback.
func (h *Handler) RequestRemoteText() error {
	h.mu.Lock()
	if !h.enabled {
		h.mu.Unlock()
		return nil
	}
	request, queued := h.enqueueDataRequestLocked(CFUnicodeText, false, 0)
	h.mu.Unlock()
	if !queued {
		return nil
	}
	if err := h.sendFn(encodeFormatDataRequest(CFUnicodeText)); err != nil {
		h.mu.Lock()
		h.removeDataRequestLocked(request.id)
		h.mu.Unlock()
		return err
	}
	return nil
}

// RequestRemoteImage sends a CB_FORMAT_DATA_REQUEST for CF_DIB.
// The result is delivered asynchronously via the OnImageData callback
// after DIB→PNG conversion.
func (h *Handler) RequestRemoteImage() error {
	h.mu.Lock()
	if !h.enabled {
		h.mu.Unlock()
		return nil
	}
	request, queued := h.enqueueDataRequestLocked(CFDIB, false, 0)
	h.mu.Unlock()
	if !queued {
		return nil
	}
	if err := h.sendFn(encodeFormatDataRequest(CFDIB)); err != nil {
		h.mu.Lock()
		h.removeDataRequestLocked(request.id)
		h.mu.Unlock()
		return err
	}
	return nil
}
