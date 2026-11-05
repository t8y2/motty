package cliprdr

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"strings"
	"time"
	"unicode/utf16"
)

// File transfer over the clipboard (MS-RDPECLIP 2.2.5, 3.1.5): the side that
// copied files announces FileGroupDescriptorW and FileContents in its format
// list; the side that pastes asks for the descriptor list, then for each file's
// size and byte ranges. This file is the copying side, the one Motty is when the
// user copies files on this computer and pastes them in the remote desktop.

// PDU types of the file contents exchange.
const (
	CBFileContentsRequest  uint16 = 0x0008
	CBFileContentsResponse uint16 = 0x0009
)

// FileContentsRequest flags (MS-RDPECLIP 2.2.5.3).
const (
	FileContentsSize  uint32 = 0x00000001
	FileContentsRange uint32 = 0x00000002
)

// The registered clipboard format names of a file list. Their numbers are chosen
// here, in the application-defined range, and the peer learns them by name.
const (
	FormatFileGroupDescriptorW = "FileGroupDescriptorW"
	FormatFileContents         = "FileContents"

	formatIDFileGroupDescriptorW uint32 = 0xC001
	formatIDFileContents         uint32 = 0xC002
)

// FILEDESCRIPTORW (MS-RDPECLIP 2.2.5.2.3.1).
const (
	fileDescriptorSize = 592
	fileNameUnits      = 260 // cFileName, including the terminating NUL

	fdAttributes   uint32 = 0x00000004
	fdWriteTime    uint32 = 0x00000020
	fdFileSize     uint32 = 0x00000040
	fdProgressUI   uint32 = 0x00004000
	attrDirectory  uint32 = 0x00000010
	attrNormal     uint32 = 0x00000080
	maxNameUnits          = fileNameUnits - 1
	hugeFileSize   uint64 = 1 << 32
	filetimeOffset int64  = 11644473600 // seconds from 1601 to 1970
)

// Limits. They bound what a selection of files, or a server's requests, can make
// this side hold or read; they are not sizes a person is expected to reach.
const (
	// MaxOfferEntries is the most files and folders one offer lists, folders
	// included. A bigger selection is refused rather than cut: a paste that
	// silently lacks files is worse than one that did not start.
	MaxOfferEntries = 20000
	maxOfferDepth   = 32
	// maxContentsRequest is the most bytes one FILECONTENTS_REQUEST may ask for.
	// Windows asks for at most 64 KiB at a time.
	maxContentsRequest = 16 << 20
	// maxLockedSnapshots is how many lists the server may hold locked at once.
	maxLockedSnapshots = 8
)

// The limits a test lowers, so it need not make 20000 files or a 4 GiB one.
var (
	offerEntryLimit = MaxOfferEntries
	hugeFileLimit   = hugeFileSize
)

// Errors of an offer. For the ones about a particular file, the Offer returned
// with the error names it in FirstSkipped.
var (
	// ErrFileClipUnsupported: the server did not negotiate file transfer.
	ErrFileClipUnsupported = errors.New("the server does not take files through the clipboard")
	// ErrNotReady: the clipboard channel has not finished its handshake.
	ErrNotReady = errors.New("the clipboard channel is not ready")
	// ErrClipboardDisabled: clipboard sharing is switched off.
	ErrClipboardDisabled = errors.New("clipboard sharing is off")
	// ErrNothingToOffer: no readable file or folder is left to offer.
	ErrNothingToOffer = errors.New("there is no file to offer")
	// ErrTooManyFiles: the selection holds more than MaxOfferEntries entries.
	ErrTooManyFiles = fmt.Errorf("the selection holds more than %d files and folders", MaxOfferEntries)
	// ErrNameTooLong: a file's path inside its folder does not fit the 259
	// characters of a descriptor.
	ErrNameTooLong = errors.New("the file's path is too long to offer")
)

// Offer says what a SetLocalFiles put on the clipboard.
type Offer struct {
	// Files and Folders count what is offered; Bytes is the total size of the files.
	Files, Folders int
	Bytes          uint64
	// Skipped counts what was left out because it cannot be copied: a link, a
	// device, a socket, a file over 4 GiB the server cannot take, or an entry that
	// could not be read. FirstSkipped names the first such path; with an error it
	// names the path that caused it.
	Skipped      int
	FirstSkipped string
}

// fileEntry is one entry of a snapshot: a file or a folder as it was when the
// list was made.
type fileEntry struct {
	name  string // path inside the offer, `\` separated
	path  string // where it is on this computer
	size  uint64
	mtime time.Time
	dir   bool
}

// fileSnapshot is the list a paste works from. The list does not change after it
// is made: a file that has changed since is refused when it is read, so a paste
// never receives bytes that are neither the old content nor the new. No file is
// held open between requests, so a copied file can still be moved or deleted
// here (on Windows an open file cannot) while the paste waits.
type fileSnapshot struct {
	entries []fileEntry
	locks   int // Lock Clipboard Data PDUs holding it
}

// buildSnapshot lists the paths the user copied and the folders among them. The
// selection itself follows symbolic links (the user pointed at it); inside a
// folder a link is left out, so a folder cannot carry something from elsewhere
// into the paste. huge allows files of 4 GiB and more, which a server accepts
// only when it said it supports them.
func buildSnapshot(paths []string, huge bool) (*fileSnapshot, Offer, error) {
	snapshot := &fileSnapshot{}
	var offer Offer
	skip := func(path string) {
		offer.Skipped++
		if offer.FirstSkipped == "" {
			offer.FirstSkipped = path
		}
	}
	// refuse is the failure of an offer; the path that caused it is in FirstSkipped.
	var refused string
	add := func(entry fileEntry) error {
		if len(snapshot.entries) >= offerEntryLimit {
			refused = entry.path
			return ErrTooManyFiles
		}
		if len(utf16.Encode([]rune(entry.name))) > maxNameUnits {
			refused = entry.path
			return ErrNameTooLong
		}
		snapshot.entries = append(snapshot.entries, entry)
		if entry.dir {
			offer.Folders++
		} else {
			offer.Files++
			offer.Bytes += entry.size
		}
		return nil
	}
	var walk func(path, name string, info fs.FileInfo, depth int) error
	walk = func(path, name string, info fs.FileInfo, depth int) error {
		switch {
		case info.IsDir():
			if depth > maxOfferDepth {
				refused = path
				return ErrNameTooLong
			}
			if err := add(fileEntry{name: name, path: path, mtime: info.ModTime(), dir: true}); err != nil {
				return err
			}
			children, err := os.ReadDir(path)
			if err != nil {
				skip(path)
				return nil
			}
			for _, child := range children {
				childInfo, err := child.Info()
				childPath := filepath.Join(path, child.Name())
				if err != nil || !(childInfo.IsDir() || childInfo.Mode().IsRegular()) {
					skip(childPath)
					continue
				}
				if err := walk(childPath, name+`\`+windowsName(child.Name()), childInfo, depth+1); err != nil {
					return err
				}
			}
		case info.Mode().IsRegular():
			size := uint64(info.Size())
			if size >= hugeFileLimit && !huge {
				skip(path)
				return nil
			}
			return add(fileEntry{name: name, path: path, size: size, mtime: info.ModTime()})
		default:
			skip(path)
		}
		return nil
	}
	for _, path := range paths {
		info, err := os.Stat(path)
		if err != nil {
			skip(path)
			continue
		}
		if err := walk(path, windowsName(filepath.Base(path)), info, 0); err != nil {
			return nil, Offer{FirstSkipped: refused}, err
		}
	}
	if len(snapshot.entries) == 0 {
		return nil, offer, ErrNothingToOffer
	}
	return snapshot, offer, nil
}

// windowsName makes one path element something Windows can create: the
// characters it forbids in a name become underscores, and a backslash, which
// would otherwise start a new folder, is one of them.
func windowsName(element string) string {
	if !strings.ContainsAny(element, "\\:*?\"<>|\x00") && !hasControl(element) {
		return element
	}
	return strings.Map(func(r rune) rune {
		if strings.ContainsRune("\\:*?\"<>|", r) || r < 0x20 {
			return '_'
		}
		return r
	}, element)
}

func hasControl(value string) bool {
	for _, r := range value {
		if r < 0x20 {
			return true
		}
	}
	return false
}

// encodeFileList builds the CLIPRDR_FILELIST a Format Data Response carries: the
// number of entries and one FILEDESCRIPTORW each.
func encodeFileList(entries []fileEntry) []byte {
	list := make([]byte, 4+fileDescriptorSize*len(entries))
	binary.LittleEndian.PutUint32(list, uint32(len(entries)))
	for index, entry := range entries {
		descriptor := list[4+index*fileDescriptorSize : 4+(index+1)*fileDescriptorSize]
		flags := fdAttributes | fdWriteTime | fdProgressUI
		attributes := attrNormal
		if entry.dir {
			attributes = attrDirectory
		} else {
			flags |= fdFileSize
		}
		binary.LittleEndian.PutUint32(descriptor[0:], flags)
		// 4..36: clsid, sizel and pointl are unused (reserved1)
		binary.LittleEndian.PutUint32(descriptor[36:], attributes)
		// 40..56: creation and access times are unused (reserved2)
		binary.LittleEndian.PutUint64(descriptor[56:], fileTime(entry.mtime))
		binary.LittleEndian.PutUint32(descriptor[64:], uint32(entry.size>>32))
		binary.LittleEndian.PutUint32(descriptor[68:], uint32(entry.size))
		for position, unit := range utf16.Encode([]rune(entry.name)) {
			if position >= maxNameUnits {
				break
			}
			binary.LittleEndian.PutUint16(descriptor[72+2*position:], unit)
		}
	}
	return list
}

// fileTime is a time as a FILETIME: 100 ns ticks since 1601. A time before 1601
// or too far on for 64 bits is 0, which the peer reads as unknown.
func fileTime(t time.Time) uint64 {
	if t.IsZero() {
		return 0
	}
	seconds := t.Unix() + filetimeOffset
	if seconds < 0 || seconds > math.MaxInt64/10_000_000-1 {
		return 0
	}
	return uint64(seconds)*10_000_000 + uint64(t.Nanosecond()/100)
}

// fileContentsRequest is a decoded CB_FILECONTENTS_REQUEST.
type fileContentsRequest struct {
	stream      uint32
	index       uint32
	flags       uint32
	position    uint64
	size        uint32
	clipDataID  uint32
	hasClipData bool
}

// decodeFileContentsRequest parses the payload of a CB_FILECONTENTS_REQUEST:
// 24 bytes, and a clipDataId after them when the server holds a lock.
func decodeFileContentsRequest(payload []byte) (fileContentsRequest, bool) {
	if len(payload) < 24 {
		return fileContentsRequest{}, false
	}
	request := fileContentsRequest{
		stream:   binary.LittleEndian.Uint32(payload[0:]),
		index:    binary.LittleEndian.Uint32(payload[4:]),
		flags:    binary.LittleEndian.Uint32(payload[8:]),
		position: uint64(binary.LittleEndian.Uint32(payload[16:]))<<32 | uint64(binary.LittleEndian.Uint32(payload[12:])),
		size:     binary.LittleEndian.Uint32(payload[20:]),
	}
	if len(payload) >= 28 {
		request.clipDataID, request.hasClipData = binary.LittleEndian.Uint32(payload[24:]), true
	}
	return request, true
}

// encodeFileContentsResponse builds a CB_FILECONTENTS_RESPONSE: the stream the
// request named and, when it succeeded, the size or the bytes.
func encodeFileContentsResponse(stream uint32, ok bool, data []byte) []byte {
	payload := make([]byte, 4+len(data))
	binary.LittleEndian.PutUint32(payload, stream)
	copy(payload[4:], data)
	flags := CBResponseOK
	if !ok {
		flags = CBResponseFail
	}
	return encodePDU(CBFileContentsResponse, flags, payload)
}

// contents answers a request from the snapshot: the file's size as eight bytes,
// or the bytes of the range. It reports false for anything it must refuse: an
// index outside the list, a folder, a file that has changed since the list was
// made, a range past the end, a size that is too big to hold. The file is opened
// for the request and closed at its end.
func (s *fileSnapshot) contents(request fileContentsRequest) ([]byte, bool) {
	if int64(request.index) >= int64(len(s.entries)) {
		return nil, false
	}
	entry := s.entries[request.index]
	if entry.dir {
		return nil, false
	}
	switch request.flags {
	case FileContentsSize:
		if request.size != 8 {
			return nil, false
		}
	case FileContentsRange:
		if request.size == 0 || request.size > maxContentsRequest || request.position >= entry.size {
			return nil, false
		}
	default:
		return nil, false
	}
	file, err := os.Open(entry.path)
	if err != nil {
		return nil, false
	}
	defer file.Close()
	info, err := file.Stat()
	if err != nil || !info.Mode().IsRegular() || uint64(info.Size()) != entry.size || !info.ModTime().Equal(entry.mtime) {
		return nil, false
	}
	if request.flags == FileContentsSize {
		var size [8]byte
		binary.LittleEndian.PutUint64(size[:], entry.size)
		return size[:], true
	}
	length := min(uint64(request.size), entry.size-request.position)
	buffer := make([]byte, length)
	// The file has the size it had, so a short read means it is shrinking now.
	if _, err := file.ReadAt(buffer, int64(request.position)); err != nil {
		return nil, false
	}
	return buffer, true
}

// SetLocalFiles makes the files and folders at paths what this side's clipboard
// holds, and tells the server. The server pastes them when its user does: it
// asks for the list, then for the contents, so nothing is read from the files
// until then. The list is made now, on the caller's goroutine, and walks the
// folders among the paths, so a caller with a large selection runs it off its
// UI thread. A change to a file between now and the paste is not copied: that
// file's contents are refused (see fileSnapshot).
//
// It fails when file transfer is not in force: the channel is not ready, the
// server did not negotiate it, sharing is off. What was left out of the list
// is counted in the Offer.
func (h *Handler) SetLocalFiles(paths []string) (Offer, error) {
	h.mu.Lock()
	enabled, ready, caps := h.enabled, h.ready, h.serverCaps
	h.mu.Unlock()
	switch {
	case !enabled:
		return Offer{}, ErrClipboardDisabled
	case !ready:
		return Offer{}, ErrNotReady
	case caps&CBStreamFileclipEnabled == 0:
		return Offer{}, ErrFileClipUnsupported
	}
	snapshot, offer, err := buildSnapshot(paths, caps&CBHugeFileSupportEnabled != 0)
	if err != nil {
		return offer, err
	}
	h.mu.Lock()
	if !h.enabled {
		h.mu.Unlock()
		return Offer{}, ErrClipboardDisabled
	}
	h.dropFilesLocked()
	h.files = snapshot
	pdu := h.encodeFormatListLocked(h.advertisedFormatsLocked())
	h.mu.Unlock()
	return offer, h.sendFn(pdu)
}

// FileClipboardInForce reports whether the server negotiated file transfer, so a
// caller can tell a user before it tries.
func (h *Handler) FileClipboardInForce() bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.ready && h.enabled && h.serverCaps&CBStreamFileclipEnabled != 0
}

// dropFilesLocked forgets the copied files. A list the server still holds locked
// stays until its Unlock.
func (h *Handler) dropFilesLocked() {
	h.files = nil
}

// lock records that the server holds the current list: the pastes it is about to
// make read that list, whatever is copied meanwhile.
func (h *Handler) lock(id uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	if h.files == nil || len(h.locked) >= maxLockedSnapshots {
		return
	}
	if _, held := h.locked[id]; held {
		return
	}
	if h.locked == nil {
		h.locked = make(map[uint32]*fileSnapshot)
	}
	h.locked[id] = h.files
	h.files.locks++
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "lock clipboard data", slog.Int("clipDataId", int(id)))
}

// unlock releases a held list; one that is no longer the current list is
// forgotten with its last lock.
func (h *Handler) unlock(id uint32) {
	h.mu.Lock()
	defer h.mu.Unlock()
	snapshot, held := h.locked[id]
	if !held {
		return
	}
	delete(h.locked, id)
	snapshot.locks--
	h.log.LogAttrs(context.Background(), slog.LevelDebug, "unlock clipboard data", slog.Int("clipDataId", int(id)))
}

// handleFileContentsRequest answers the server's request for the size of a file
// or a range of its bytes, from the list the request names (the locked one it
// holds, else the current one). Anything it cannot answer is refused with a
// failure response, never a short or stale one.
func (h *Handler) handleFileContentsRequest(payload []byte) {
	request, ok := decodeFileContentsRequest(payload)
	if !ok {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "file contents request too short", slog.Int("bytes", len(payload)))
		return
	}
	h.mu.Lock()
	snapshot := h.files
	if request.hasClipData {
		snapshot = h.locked[request.clipDataID]
	}
	if !h.enabled {
		snapshot = nil
	}
	h.mu.Unlock()
	// The read is made outside the handler's lock: it waits for the disk, and
	// the application's goroutine takes that lock to copy something else.
	var data []byte
	if snapshot != nil {
		data, ok = snapshot.contents(request)
	}
	if snapshot == nil || !ok {
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "file contents request refused",
			slog.Int("index", int(request.index)), slog.Int("flags", int(request.flags)))
		h.sendResponse(encodeFileContentsResponse(request.stream, false, nil))
		return
	}
	h.sendResponse(encodeFileContentsResponse(request.stream, true, data))
}
