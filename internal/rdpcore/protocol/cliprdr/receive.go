package cliprdr

import (
	"context"
	"encoding/binary"
	"errors"
	"fmt"
	"log/slog"
	"math"
	"os"
	"path/filepath"
	"runtime"
	"sort"
	"strings"
	"time"
	"unicode/utf16"
)

// Files come from the server's clipboard the other way round from SetLocalFiles:
// the server announces FileGroupDescriptorW and FileContents, this side asks for
// the descriptor list, then reads each file by size and byte ranges into a
// directory of its own. The caller puts the top-level entries of that directory
// on the local clipboard.

// RemoteFile is one entry of a file list the server's clipboard holds.
type RemoteFile struct {
	// Name is the path inside the copied selection, `\` separated.
	Name     string
	Size     uint64
	HasSize  bool
	Modified time.Time
	Dir      bool
}

// RemoteFiles are the files the server copied, saved on this computer under Dir.
type RemoteFiles struct {
	Dir   string
	Paths []string // the top-level entries, to put on the local clipboard
	Files int
	// Folders counts the folders in the list, Bytes the bytes of the files.
	Folders int
	Bytes   uint64
	// Skipped counts entries that could not be saved (an unsafe name, a
	// protocol error, a disk error), so the local clipboard is never presented
	// as complete when it is not.
	Skipped int
}

// Why a remote file list may fail before any file arrives.
var (
	errFileListRefused     = errors.New("the server refused its file list")
	errFileContentsRefused = errors.New("the server refused a file's contents")
	errFileResponseTimeout = errors.New("the server did not answer for the file's contents")
)

// How long one size or range request may wait for the server. A chunk is at
// most 64 KiB, so this only bounds a server that has stopped answering.
const fileResponseTimeout = 60 * time.Second

// fileStagePrefix names the directories this side makes for received files.
const fileStagePrefix = "motty-rdp-clipboard-"

// fileContentsResponse is one CB_FILECONTENTS_RESPONSE, handed to the
// download goroutine that asked for it.
type fileContentsResponse struct {
	stream uint32
	ok     bool
	data   []byte
}

// RequestRemoteFiles asks the server for the file list its clipboard holds. The
// list, and then the files themselves, arrive through OnRemoteFiles. It fails
// when the server did not advertise file transfer or the clipboard is off.
func (h *Handler) RequestRemoteFiles() error {
	h.mu.Lock()
	enabled, hasFiles, id := h.enabled, h.remoteHasFiles, h.remoteFileListID
	canLock := h.serverCaps&CBCanLockClipData != 0
	lockID := h.remoteLockID
	newLock := false
	if enabled && hasFiles && canLock && lockID == 0 {
		h.remoteLockNext++
		if h.remoteLockNext == 0 {
			h.remoteLockNext++
		}
		lockID = h.remoteLockNext
		h.remoteLockID, newLock = lockID, true
	}
	var request formatDataRequest
	queued := false
	if enabled && hasFiles {
		request, queued = h.enqueueDataRequestLocked(id, true, lockID)
	}
	h.mu.Unlock()
	if !enabled {
		return ErrClipboardDisabled
	}
	if !hasFiles {
		return ErrFileClipUnsupported
	}
	if !queued {
		if newLock {
			h.forgetRemoteLock(lockID)
		}
		return nil
	}
	// Locking first makes the server keep this selection while the list and its
	// files are read, even if the remote copies something else meanwhile.
	if newLock {
		if err := h.sendFn(encodeLockClipData(CBLockClipData, lockID)); err != nil {
			h.mu.Lock()
			h.removeDataRequestLocked(request.id)
			h.mu.Unlock()
			h.forgetRemoteLock(lockID)
			return err
		}
	}
	if err := h.sendFn(encodeFormatDataRequest(id)); err != nil {
		// The request was not sent, so no response will name it; leaving it
		// queued would make the next response look like this one's.
		h.mu.Lock()
		h.removeDataRequestLocked(request.id)
		h.mu.Unlock()
		h.releaseRemoteLockIfIdle(lockID)
		return err
	}
	return nil
}

// forgetRemoteLock drops the id without telling the server; the lock was never
// sent, or its unlock is already on the way.
func (h *Handler) forgetRemoteLock(id uint32) {
	h.mu.Lock()
	if h.remoteLockID == id {
		h.remoteLockID = 0
	}
	h.mu.Unlock()
}

// releaseRemoteLockIfIdle tells the server it may let the data go, unless the
// newest download still reads it; that download unlocks when it ends.
func (h *Handler) releaseRemoteLockIfIdle(id uint32) {
	if id == 0 {
		return
	}
	h.mu.Lock()
	release := h.remoteLockID == id && h.downloadClipID != id
	if release {
		h.remoteLockID = 0
	}
	h.mu.Unlock()
	if release {
		h.sendResponse(encodeLockClipData(CBUnlockClipData, id))
	}
}

// finishRemoteFiles hands an error, or a finished download, to the caller off
// the connection's read goroutine.
func (h *Handler) finishRemoteFiles(generation uint64, files RemoteFiles, err error) {
	h.mu.Lock()
	callback := h.onFilesData
	stale := h.downloadGeneration != generation
	h.mu.Unlock()
	if stale {
		// The remote clipboard changed while the files were being read. What
		// arrived belongs to a selection that is no longer on the clipboard.
		if files.Dir != "" {
			_ = os.RemoveAll(files.Dir)
		}
		return
	}
	if callback != nil {
		callback(files, err)
	}
}

// startRemoteFiles saves the list on a goroutine of its own. A list arriving
// meanwhile cancels it, so a stale download never replaces a newer clipboard.
func (h *Handler) startRemoteFiles(files []RemoteFile, generation uint64, clipID uint32) {
	h.mu.Lock()
	if h.downloadGeneration != generation {
		h.mu.Unlock()
		return
	}
	callback := h.onFilesData
	if callback == nil {
		h.mu.Unlock()
		h.releaseRemoteLockIfIdle(clipID)
		return
	}
	// A repeated request for the same clipboard (a duplicate format list does
	// not change its contents) would start a second download of the same list;
	// stop the first. receiveRemoteFiles serializes them with downloadMu.
	if h.downloadCancel != nil {
		h.downloadCancel()
	}
	ctx, cancel := context.WithCancel(context.Background())
	h.downloadCancel = cancel
	h.downloadSerial++
	h.downloadClipID = clipID
	serial := h.downloadSerial
	h.mu.Unlock()
	go h.receiveRemoteFiles(ctx, cancel, generation, serial, clipID, files)
}

// receiveRemoteFiles reads every file of the list into a new directory and
// reports the top-level entries. It is the only download at a time.
func (h *Handler) receiveRemoteFiles(ctx context.Context, cancel context.CancelFunc, generation uint64, serial uint64, clipID uint32, files []RemoteFile) {
	defer cancel()
	defer h.finishDownload(serial, clipID)
	h.downloadMu.Lock()
	defer h.downloadMu.Unlock()
	h.mu.Lock()
	current := h.downloadGeneration == generation
	h.mu.Unlock()
	if !current || ctx.Err() != nil {
		return
	}
	root, err := os.MkdirTemp("", fileStagePrefix)
	if err != nil {
		h.finishRemoteFiles(generation, RemoteFiles{}, fmt.Errorf("make a directory for the remote files: %w", err))
		return
	}
	result := RemoteFiles{Dir: root}
	tops := make(map[string]bool)
	for index, entry := range files {
		if ctx.Err() != nil || h.generationChanged(generation) {
			_ = os.RemoveAll(root)
			return
		}
		rel, ok := localFilePath(entry.Name)
		if !ok {
			result.Skipped++
			continue
		}
		path := filepath.Join(root, rel)
		if entry.Dir {
			if err := os.MkdirAll(path, 0o700); err != nil {
				h.log.LogAttrs(context.Background(), slog.LevelDebug, "remote file folder refused", slog.String("name", entry.Name), slog.Any("error", err))
				result.Skipped++
				continue
			}
			result.Folders++
		} else {
			written, err := h.receiveRemoteFile(ctx, root, rel, index, entry, clipID)
			if err != nil {
				h.log.LogAttrs(context.Background(), slog.LevelDebug, "remote file refused", slog.String("name", entry.Name), slog.Any("error", err))
				result.Skipped++
				continue
			}
			result.Files++
			result.Bytes += written
		}
		top := strings.SplitN(rel, string(filepath.Separator), 2)[0]
		tops[top] = true
	}
	if h.generationChanged(generation) || ctx.Err() != nil {
		_ = os.RemoveAll(root)
		return
	}
	for top := range tops {
		path := filepath.Join(root, top)
		if _, err := os.Stat(path); err == nil {
			result.Paths = append(result.Paths, path)
		}
	}
	sort.Strings(result.Paths)
	if len(result.Paths) == 0 {
		_ = os.RemoveAll(root)
		h.finishRemoteFiles(generation, RemoteFiles{}, errors.New("the remote clipboard's files could not be read"))
		return
	}
	h.finishRemoteFiles(generation, result, nil)
}

func (h *Handler) generationChanged(generation uint64) bool {
	h.mu.Lock()
	defer h.mu.Unlock()
	return h.downloadGeneration != generation
}

// finishDownload unlocks the remote data this download read, unless a newer
// download has taken over the same lock or the clipboard change already did.
func (h *Handler) finishDownload(serial uint64, clipID uint32) {
	h.mu.Lock()
	release := clipID != 0 && h.downloadSerial == serial && h.remoteLockID == clipID
	if release {
		h.remoteLockID = 0
	}
	if h.downloadSerial == serial {
		h.downloadCancel = nil
		h.downloadClipID = 0
	}
	h.mu.Unlock()
	if release {
		h.sendResponse(encodeLockClipData(CBUnlockClipData, clipID))
	}
}

// receiveRemoteFile writes one file and returns how many bytes it read.
func (h *Handler) receiveRemoteFile(ctx context.Context, root, rel string, index int, entry RemoteFile, clipID uint32) (uint64, error) {
	path := filepath.Join(root, rel)
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		return 0, err
	}
	file, err := os.OpenFile(path, os.O_CREATE|os.O_EXCL|os.O_WRONLY, 0o600)
	if err != nil {
		return 0, err
	}
	failed := true
	defer func() {
		_ = file.Close()
		if failed {
			_ = os.Remove(path)
		}
	}()
	size := entry.Size
	if !entry.HasSize {
		data, err := h.requestFileContents(ctx, index, FileContentsSize, 0, 8, clipID)
		if err != nil {
			return 0, err
		}
		if len(data) != 8 {
			return 0, fmt.Errorf("a file size of %d bytes", len(data))
		}
		size = binary.LittleEndian.Uint64(data)
	}
	if size > math.MaxInt64 {
		return 0, fmt.Errorf("a file of %d bytes", size)
	}
	for offset := uint64(0); offset < size; {
		length := min(uint64(64<<10), size-offset)
		data, err := h.requestFileContents(ctx, index, FileContentsRange, offset, uint32(length), clipID)
		if err != nil {
			return 0, err
		}
		if uint64(len(data)) != length {
			return 0, fmt.Errorf("a chunk of %d bytes for %d", len(data), length)
		}
		if _, err := file.WriteAt(data, int64(offset)); err != nil {
			return 0, err
		}
		offset += length
	}
	if !entry.Modified.IsZero() {
		_ = os.Chtimes(path, entry.Modified, entry.Modified)
	}
	failed = false
	return size, nil
}

// requestFileContents asks for one file's size or a range of its bytes and
// waits for the response that names that request. Requests are serial: the
// clipboard channel carries no other file transfer at the same time.
func (h *Handler) requestFileContents(ctx context.Context, index int, flags uint32, position uint64, size uint32, clipID uint32) ([]byte, error) {
	h.drainFileResponses()
	// The streamId is echoed in the response. A unique one per request keeps an
	// answer to a canceled transfer, which the server still sends, from being
	// read as the answer to this one.
	stream := h.nextFileStream()
	length := 24
	if clipID != 0 {
		length = 28
	}
	payload := make([]byte, length)
	binary.LittleEndian.PutUint32(payload[0:], stream)
	binary.LittleEndian.PutUint32(payload[4:], uint32(index))
	binary.LittleEndian.PutUint32(payload[8:], flags)
	binary.LittleEndian.PutUint32(payload[12:], uint32(position))
	binary.LittleEndian.PutUint32(payload[16:], uint32(position>>32))
	binary.LittleEndian.PutUint32(payload[20:], size)
	if clipID != 0 {
		binary.LittleEndian.PutUint32(payload[24:], clipID)
	}
	if err := h.sendFn(encodePDU(CBFileContentsRequest, 0, payload)); err != nil {
		return nil, err
	}
	timer := time.NewTimer(fileResponseTimeout)
	defer timer.Stop()
	for {
		select {
		case response := <-h.fileResponses:
			if response.stream != stream {
				h.log.LogAttrs(context.Background(), slog.LevelDebug, "stale file contents response", slog.Int("stream", int(response.stream)))
				continue
			}
			if !response.ok {
				return nil, errFileContentsRefused
			}
			return response.data, nil
		case <-ctx.Done():
			return nil, ctx.Err()
		case <-timer.C:
			return nil, errFileResponseTimeout
		}
	}
}

// nextFileStream returns a streamId not used by the previous request. Only the
// download goroutine calls it, and downloads are serialized by downloadMu.
func (h *Handler) nextFileStream() uint32 {
	h.fileStream++
	if h.fileStream == 0 { // zero is a valid id, but wrapping to it is confusing
		h.fileStream++
	}
	return h.fileStream
}

func (h *Handler) drainFileResponses() {
	for {
		select {
		case <-h.fileResponses:
		default:
			return
		}
	}
}

// handleFileContentsResponse delivers the server's answer to the download that
// asked for it. A response nobody waits for is stale and dropped.
func (h *Handler) handleFileContentsResponse(flags uint16, payload []byte) {
	if len(payload) < 4 {
		h.log.LogAttrs(context.Background(), slog.LevelWarn, "file contents response too short", slog.Int("bytes", len(payload)))
		return
	}
	response := fileContentsResponse{
		stream: binary.LittleEndian.Uint32(payload[0:4]),
		ok:     flags&CBResponseOK != 0,
	}
	if response.ok {
		response.data = append([]byte(nil), payload[4:]...)
	}
	select {
	case h.fileResponses <- response:
	default:
		h.log.LogAttrs(context.Background(), slog.LevelDebug, "unrequested file contents response", slog.Int("stream", int(response.stream)))
	}
}

// decodeRemoteFileList parses a CLIPRDR_FILELIST: the number of entries and one
// FILEDESCRIPTORW each. Unsafe or unreadable entries are represented too; the
// downloader refuses them by name and counts them as skipped.
func decodeRemoteFileList(payload []byte) ([]RemoteFile, error) {
	if len(payload) < 4 {
		return nil, errors.New("a file list shorter than its count")
	}
	count := int(binary.LittleEndian.Uint32(payload[0:4]))
	if count <= 0 || count > MaxOfferEntries {
		return nil, fmt.Errorf("a file list of %d entries", count)
	}
	if 4+count*fileDescriptorSize > len(payload) {
		return nil, fmt.Errorf("a file list of %d entries in %d bytes", count, len(payload))
	}
	files := make([]RemoteFile, 0, count)
	for index := 0; index < count; index++ {
		descriptor := payload[4+index*fileDescriptorSize : 4+(index+1)*fileDescriptorSize]
		flags := binary.LittleEndian.Uint32(descriptor[0:4])
		attributes := binary.LittleEndian.Uint32(descriptor[36:40])
		if attributes&0x00000400 != 0 { // FILE_ATTRIBUTE_REPARSE_POINT
			files = append(files, RemoteFile{Name: decodeFileName(descriptor[72:])})
			continue
		}
		entry := RemoteFile{
			Name:     decodeFileName(descriptor[72:]),
			Dir:      attributes&attrDirectory != 0,
			HasSize:  flags&fdFileSize != 0,
			Modified: timeFromFileTime(binary.LittleEndian.Uint64(descriptor[56:64])),
		}
		if entry.HasSize {
			entry.Size = uint64(binary.LittleEndian.Uint32(descriptor[64:68]))<<32 | uint64(binary.LittleEndian.Uint32(descriptor[68:72]))
		}
		files = append(files, entry)
	}
	return files, nil
}

func decodeFileName(data []byte) string {
	units := make([]uint16, 0, len(data)/2)
	for offset := 0; offset+1 < len(data); offset += 2 {
		unit := binary.LittleEndian.Uint16(data[offset:])
		if unit == 0 {
			break
		}
		units = append(units, unit)
	}
	return string(utf16.Decode(units))
}

func timeFromFileTime(value uint64) time.Time {
	if value == 0 {
		return time.Time{}
	}
	seconds := int64(value / 10_000_000)
	nanoseconds := int64(value%10_000_000) * 100
	return time.Unix(seconds-filetimeOffset, nanoseconds)
}

// localFilePath turns a descriptor's relative name into a path inside root.
// It refuses anything that could leave root or that Windows cannot name.
func localFilePath(name string) (string, bool) {
	name = strings.ReplaceAll(name, "\\", "/")
	if name == "" || strings.HasPrefix(name, "/") {
		return "", false
	}
	parts := strings.Split(name, "/")
	if len(parts) > maxOfferDepth {
		return "", false
	}
	cleaned := make([]string, len(parts))
	for index, part := range parts {
		if part == "" || part == "." || part == ".." {
			return "", false
		}
		part = localFileName(part)
		if part == "" || part == "." || part == ".." {
			return "", false
		}
		cleaned[index] = part
	}
	path := filepath.Join(cleaned...)
	if filepath.IsAbs(path) || filepath.VolumeName(path) != "" {
		return "", false
	}
	return path, true
}

// localFileName makes one path element something this computer can create. On
// Windows that means replacing its forbidden characters; elsewhere only a NUL
// and the separator, neither of which is already a separate element.
func localFileName(name string) string {
	if runtime.GOOS != "windows" {
		return localFileNameFor(name, false)
	}
	return localFileNameFor(name, true)
}

// localFileNameFor is localFileName with the naming rules of windows chosen by
// the caller, so the Windows rules are testable elsewhere.
func localFileNameFor(name string, windows bool) string {
	if !windows {
		return strings.Map(func(r rune) rune {
			if r == 0 || r == '/' {
				return '_'
			}
			return r
		}, name)
	}
	name = strings.Map(func(r rune) rune {
		if r < 0x20 || strings.ContainsRune(`<>:"/\|?*`, r) {
			return '_'
		}
		return r
	}, name)
	name = strings.TrimRight(name, ". ")
	if name == "" {
		return "_"
	}
	// Windows reserves a device name even when it carries an extension, and
	// treats everything before the first dot as the base (`COM1.tar.gz` is a
	// device too); trailing dots and spaces were already removed above.
	base := strings.ToUpper(name)
	if dot := strings.IndexByte(base, '.'); dot >= 0 {
		base = base[:dot]
	}
	switch base {
	case "CON", "PRN", "AUX", "NUL", "COM1", "COM2", "COM3", "COM4", "COM5", "COM6", "COM7", "COM8", "COM9",
		"LPT1", "LPT2", "LPT3", "LPT4", "LPT5", "LPT6", "LPT7", "LPT8", "LPT9":
		return "_" + name
	}
	return name
}
