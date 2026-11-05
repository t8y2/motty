package cliprdr

import (
	"bytes"
	"encoding/binary"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"
	"unicode/utf16"
)

// fileServer is a cliprdr handler that has finished its handshake with a server
// offering file transfer, and what it sent.
type fileServer struct {
	h    *Handler
	mu   sync.Mutex
	sent [][]byte
}

func newFileServer(t testing.TB, serverCaps uint32) *fileServer {
	t.Helper()
	server := &fileServer{}
	server.h = NewHandler(func(data []byte) error {
		server.mu.Lock()
		defer server.mu.Unlock()
		server.sent = append(server.sent, bytes.Clone(data))
		return nil
	}, testLog)
	server.h.ProcessPDU(encodeCapsPDU(CBCapsVersion2, serverCaps))
	server.h.ProcessPDU(encodePDU(CBMonitorReady, 0, nil))
	server.take()
	t.Cleanup(server.h.Close)
	return server
}

const allFileCaps = CBUseLongFormatNames | CBStreamFileclipEnabled | CBFileclipNoFilePaths | CBCanLockClipData | CBHugeFileSupportEnabled

// take returns the PDUs sent since the last call.
func (s *fileServer) take() [][]byte {
	s.mu.Lock()
	defer s.mu.Unlock()
	sent := s.sent
	s.sent = nil
	return sent
}

// last decodes the one PDU sent since the last call.
func (s *fileServer) last(t testing.TB) (msgType, flags uint16, payload []byte) {
	t.Helper()
	sent := s.take()
	if len(sent) != 1 {
		t.Fatalf("sent %d PDUs, want 1", len(sent))
	}
	msgType, flags, payload, err := decodePDU(sent[0])
	if err != nil {
		t.Fatal(err)
	}
	return msgType, flags, payload
}

func contentsRequest(stream, index, flags uint32, position uint64, size uint32, clipDataID *uint32) []byte {
	payload := make([]byte, 24, 28)
	binary.LittleEndian.PutUint32(payload[0:], stream)
	binary.LittleEndian.PutUint32(payload[4:], index)
	binary.LittleEndian.PutUint32(payload[8:], flags)
	binary.LittleEndian.PutUint32(payload[12:], uint32(position))
	binary.LittleEndian.PutUint32(payload[16:], uint32(position>>32))
	binary.LittleEndian.PutUint32(payload[20:], size)
	if clipDataID != nil {
		payload = binary.LittleEndian.AppendUint32(payload, *clipDataID)
	}
	return encodePDU(CBFileContentsRequest, 0, payload)
}

func lockPDU(kind uint16, id uint32) []byte {
	return encodePDU(kind, 0, binary.LittleEndian.AppendUint32(nil, id))
}

// descriptor is a decoded FILEDESCRIPTORW.
type descriptor struct {
	flags, attributes uint32
	written           uint64
	size              uint64
	name              string
}

func decodeFileList(t testing.TB, payload []byte) []descriptor {
	t.Helper()
	if len(payload) < 4 {
		t.Fatalf("file list is %d bytes", len(payload))
	}
	count := int(binary.LittleEndian.Uint32(payload))
	if len(payload) != 4+count*fileDescriptorSize {
		t.Fatalf("file list is %d bytes for %d entries, want %d", len(payload), count, 4+count*fileDescriptorSize)
	}
	list := make([]descriptor, count)
	for index := range list {
		raw := payload[4+index*fileDescriptorSize:]
		units := make([]uint16, 0, 32)
		for position := 0; position < fileNameUnits; position++ {
			unit := binary.LittleEndian.Uint16(raw[72+2*position:])
			if unit == 0 {
				break
			}
			units = append(units, unit)
		}
		list[index] = descriptor{
			flags:      binary.LittleEndian.Uint32(raw[0:]),
			attributes: binary.LittleEndian.Uint32(raw[36:]),
			written:    binary.LittleEndian.Uint64(raw[56:]),
			size:       uint64(binary.LittleEndian.Uint32(raw[64:]))<<32 | uint64(binary.LittleEndian.Uint32(raw[68:])),
			name:       string(utf16.Decode(units)),
		}
	}
	return list
}

func writeFile(t testing.TB, path, content string) {
	t.Helper()
	if err := os.MkdirAll(filepath.Dir(path), 0o700); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(path, []byte(content), 0o600); err != nil {
		t.Fatal(err)
	}
}

// copyFiles makes a tree, copies its two top-level entries and returns the
// server and the list it asked for.
func copyTree(t *testing.T) (*fileServer, string, []descriptor) {
	t.Helper()
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "中文 report.txt"), "hello, files")
	writeFile(t, filepath.Join(dir, "folder", "a.bin"), "0123456789")
	writeFile(t, filepath.Join(dir, "folder", "sub", "empty"), "")
	server := newFileServer(t, allFileCaps)
	offer, err := server.h.SetLocalFiles([]string{filepath.Join(dir, "中文 report.txt"), filepath.Join(dir, "folder")})
	if err != nil {
		t.Fatal(err)
	}
	if offer.Files != 3 || offer.Folders != 2 || offer.Bytes != 22 || offer.Skipped != 0 {
		t.Fatalf("offer = %+v", offer)
	}
	server.take()
	server.h.ProcessPDU(encodeFormatDataRequest(formatIDFileGroupDescriptorW))
	msgType, flags, payload := server.last(t)
	if msgType != CBFormatDataResponse || flags != CBResponseOK {
		t.Fatalf("file list response type=0x%04X flags=0x%04X", msgType, flags)
	}
	return server, dir, decodeFileList(t, payload)
}

func TestCapsOfferFileTransfer(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error { sent = append(sent, bytes.Clone(data)); return nil }, testLog)
	h.ProcessPDU(encodeCapsPDU(CBCapsVersion2, allFileCaps))
	h.ProcessPDU(encodePDU(CBMonitorReady, 0, nil))
	_, _, payload, err := decodePDU(sent[0])
	if err != nil {
		t.Fatal(err)
	}
	_, flags, err := decodeCapsPDU(payload)
	if err != nil {
		t.Fatal(err)
	}
	for name, flag := range map[string]uint32{"long names": CBUseLongFormatNames, "stream file clip": CBStreamFileclipEnabled, "no file paths": CBFileclipNoFilePaths, "lock": CBCanLockClipData, "huge files": CBHugeFileSupportEnabled} {
		if flags&flag == 0 {
			t.Errorf("the client caps 0x%08X lack %s", flags, name)
		}
	}
}

func TestCopiedFilesAreOfferedAsAFileList(t *testing.T) {
	server, dir, list := copyTree(t)
	// Order: each selection entry, a folder before what is in it, names sorted.
	want := []struct {
		name string
		dir  bool
		size uint64
	}{
		{"中文 report.txt", false, 12},
		{"folder", true, 0},
		{`folder\a.bin`, false, 10},
		{`folder\sub`, true, 0},
		{`folder\sub\empty`, false, 0},
	}
	if len(list) != len(want) {
		t.Fatalf("list has %d entries, want %d: %+v", len(list), len(want), list)
	}
	for index, entry := range want {
		got := list[index]
		isDir := got.attributes&attrDirectory != 0
		if got.name != entry.name || isDir != entry.dir || got.size != entry.size {
			t.Errorf("entry %d = %q dir=%v size=%d, want %q dir=%v size=%d", index, got.name, isDir, got.size, entry.name, entry.dir, entry.size)
		}
		if got.flags&fdAttributes == 0 || got.flags&fdWriteTime == 0 || (got.flags&fdFileSize != 0) == entry.dir {
			t.Errorf("entry %d flags 0x%08X do not say what the descriptor holds", index, got.flags)
		}
		if got.written == 0 {
			t.Errorf("entry %d has no write time", index)
		}
	}
	// The write time is a FILETIME of the file's own modification time.
	info, err := os.Stat(filepath.Join(dir, "中文 report.txt"))
	if err != nil {
		t.Fatal(err)
	}
	if want := fileTime(info.ModTime()); list[0].written != want {
		t.Errorf("write time %d, want %d", list[0].written, want)
	}
	_ = server
}

func TestFormatListNamesTheFileFormatsAndTextReturnsAfterwards(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a.txt"), "a")
	server := newFileServer(t, allFileCaps)
	if _, err := server.h.SetLocalFiles([]string{filepath.Join(dir, "a.txt")}); err != nil {
		t.Fatal(err)
	}
	msgType, _, payload := server.last(t)
	if msgType != CBFormatList {
		t.Fatalf("type 0x%04X, want a format list", msgType)
	}
	formats := decodeFormatListLong(payload)
	if len(formats) != 2 || formats[0].FormatName != FormatFileGroupDescriptorW || formats[1].FormatName != FormatFileContents {
		t.Fatalf("formats = %+v, want the file group descriptor and file contents", formats)
	}
	if formats[0].FormatID < 0xC000 || formats[1].FormatID < 0xC000 || formats[0].FormatID == formats[1].FormatID {
		t.Fatalf("registered format numbers = %d, %d", formats[0].FormatID, formats[1].FormatID)
	}

	// Copying text replaces the files: the list goes back to text and image, and
	// the server's reads of the old list are refused.
	if err := server.h.SetLocalClipboard("text"); err != nil {
		t.Fatal(err)
	}
	_, _, payload = server.last(t)
	if formats := decodeFormatListLong(payload); len(formats) != 2 || formats[0].FormatID != CFUnicodeText || formats[1].FormatID != CFDIB {
		t.Fatalf("after text the formats = %+v", formats)
	}
	server.h.ProcessPDU(contentsRequest(1, 0, FileContentsSize, 0, 8, nil))
	if _, flags, _ := server.last(t); flags != CBResponseFail {
		t.Fatalf("a request for files that are no longer copied got flags 0x%04X", flags)
	}
	server.h.ProcessPDU(encodeFormatDataRequest(formatIDFileGroupDescriptorW))
	if _, flags, _ := server.last(t); flags != CBResponseFail {
		t.Fatalf("a request for the old file list got flags 0x%04X", flags)
	}
}

func fileContents(t *testing.T, server *fileServer, request []byte) (ok bool, stream uint32, data []byte) {
	t.Helper()
	server.h.ProcessPDU(request)
	msgType, flags, payload := server.last(t)
	if msgType != CBFileContentsResponse {
		t.Fatalf("type 0x%04X, want a file contents response", msgType)
	}
	if len(payload) < 4 {
		t.Fatalf("response payload is %d bytes", len(payload))
	}
	return flags == CBResponseOK, binary.LittleEndian.Uint32(payload), payload[4:]
}

func TestFileContentsAreServedBySizeAndRange(t *testing.T) {
	server, _, _ := copyTree(t)
	ok, stream, data := fileContents(t, server, contentsRequest(7, 0, FileContentsSize, 0, 8, nil))
	if !ok || stream != 7 || binary.LittleEndian.Uint64(data) != 12 || len(data) != 8 {
		t.Fatalf("size: ok=%v stream=%d data=%v", ok, stream, data)
	}
	ok, stream, data = fileContents(t, server, contentsRequest(8, 0, FileContentsRange, 7, 100, nil))
	if !ok || stream != 8 || string(data) != "files" {
		t.Fatalf("range past the end: ok=%v stream=%d data=%q, want the last 5 bytes", ok, stream, data)
	}
	ok, _, data = fileContents(t, server, contentsRequest(9, 2, FileContentsRange, 2, 4, nil))
	if !ok || string(data) != "2345" {
		t.Fatalf("range in the folder's file: ok=%v data=%q", ok, data)
	}
	// An empty file has a size and no range.
	ok, _, data = fileContents(t, server, contentsRequest(10, 4, FileContentsSize, 0, 8, nil))
	if !ok || binary.LittleEndian.Uint64(data) != 0 {
		t.Fatalf("empty file size: ok=%v data=%v", ok, data)
	}
}

func TestFileContentsRefusesWhatItCannotAnswerExactly(t *testing.T) {
	server, _, _ := copyTree(t)
	cases := map[string][]byte{
		"an index outside the list":     contentsRequest(1, 99, FileContentsSize, 0, 8, nil),
		"the largest index":             contentsRequest(1, 0xFFFFFFFF, FileContentsSize, 0, 8, nil),
		"a folder":                      contentsRequest(1, 1, FileContentsRange, 0, 4, nil),
		"a size asked in the wrong way": contentsRequest(1, 0, FileContentsSize, 0, 4, nil),
		"both flags":                    contentsRequest(1, 0, FileContentsSize|FileContentsRange, 0, 8, nil),
		"no flag":                       contentsRequest(1, 0, 0, 0, 8, nil),
		"a range at the end":            contentsRequest(1, 0, FileContentsRange, 12, 4, nil),
		"a range past the end":          contentsRequest(1, 0, FileContentsRange, 1<<40, 4, nil),
		"a zero length":                 contentsRequest(1, 0, FileContentsRange, 0, 0, nil),
		"a length beyond the limit":     contentsRequest(1, 0, FileContentsRange, 0, maxContentsRequest+1, nil),
		"a lock that is not held":       contentsRequest(1, 0, FileContentsSize, 0, 8, func() *uint32 { id := uint32(5); return &id }()),
	}
	for name, request := range cases {
		t.Run(name, func(t *testing.T) {
			if ok, stream, data := fileContents(t, server, request); ok || stream != 1 || len(data) != 0 {
				t.Fatalf("ok=%v stream=%d data=%d bytes, want a failure that names the stream", ok, stream, len(data))
			}
		})
	}
	// A request shorter than its header is dropped without an answer.
	server.h.ProcessPDU(encodePDU(CBFileContentsRequest, 0, make([]byte, 23)))
	if sent := server.take(); len(sent) != 0 {
		t.Fatalf("a truncated request was answered with %d PDUs", len(sent))
	}
}

func TestAFileThatChangedAfterTheListIsRefused(t *testing.T) {
	server, dir, _ := copyTree(t)
	path := filepath.Join(dir, "中文 report.txt")

	// Same size, new modification time.
	later := time.Now().Add(time.Hour)
	if err := os.Chtimes(path, later, later); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := fileContents(t, server, contentsRequest(1, 0, FileContentsRange, 0, 4, nil)); ok {
		t.Fatal("a file with a new modification time was served")
	}
	// Different size.
	writeFile(t, path, "shorter")
	if ok, _, _ := fileContents(t, server, contentsRequest(2, 0, FileContentsRange, 0, 4, nil)); ok {
		t.Fatal("a file of a new size was served")
	}
	// Gone.
	if err := os.Remove(path); err != nil {
		t.Fatal(err)
	}
	if ok, _, _ := fileContents(t, server, contentsRequest(3, 0, FileContentsSize, 0, 8, nil)); ok {
		t.Fatal("a deleted file was served")
	}
	// The others are unaffected.
	if ok, _, data := fileContents(t, server, contentsRequest(4, 2, FileContentsRange, 0, 10, nil)); !ok || string(data) != "0123456789" {
		t.Fatalf("an untouched file: ok=%v data=%q", ok, data)
	}
}

func TestALockedListSurvivesANewCopyUntilUnlocked(t *testing.T) {
	server, dir, _ := copyTree(t)
	other := filepath.Join(dir, "other.txt")
	writeFile(t, other, "second selection")
	id := uint32(41)
	server.h.ProcessPDU(lockPDU(CBLockClipData, id))

	if _, err := server.h.SetLocalFiles([]string{other}); err != nil {
		t.Fatal(err)
	}
	server.take()
	// A request that names the lock reads the list that was locked...
	ok, _, data := fileContents(t, server, contentsRequest(1, 0, FileContentsRange, 0, 5, &id))
	if !ok || string(data) != "hello" {
		t.Fatalf("locked list: ok=%v data=%q, want the first selection's file", ok, data)
	}
	// ...and one that does not reads the new one.
	ok, _, data = fileContents(t, server, contentsRequest(2, 0, FileContentsRange, 0, 6, nil))
	if !ok || string(data) != "second" {
		t.Fatalf("current list: ok=%v data=%q, want the second selection's file", ok, data)
	}
	server.h.ProcessPDU(lockPDU(CBUnlockClipData, id))
	if ok, _, _ := fileContents(t, server, contentsRequest(3, 0, FileContentsSize, 0, 8, &id)); ok {
		t.Fatal("a list was served after its unlock")
	}
	// Unlocking what was never locked, and twice, is harmless.
	server.h.ProcessPDU(lockPDU(CBUnlockClipData, id))
	server.h.ProcessPDU(lockPDU(CBUnlockClipData, 12345))
}

func TestOnlyAFewListsCanBeLocked(t *testing.T) {
	server, dir, _ := copyTree(t)
	for id := uint32(0); id < maxLockedSnapshots+4; id++ {
		server.h.ProcessPDU(lockPDU(CBLockClipData, id))
		writeFile(t, filepath.Join(dir, fmt.Sprintf("n%d", id)), "x")
		if _, err := server.h.SetLocalFiles([]string{filepath.Join(dir, fmt.Sprintf("n%d", id))}); err != nil {
			t.Fatal(err)
		}
	}
	server.h.mu.Lock()
	held := len(server.h.locked)
	server.h.mu.Unlock()
	if held != maxLockedSnapshots {
		t.Fatalf("%d lists held, want the limit %d", held, maxLockedSnapshots)
	}
}

func TestFileOfferNeedsAReadyServerThatNegotiatedIt(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "a"), "a")
	paths := []string{filepath.Join(dir, "a")}

	notReady := NewHandler(func([]byte) error { return nil }, testLog)
	if _, err := notReady.SetLocalFiles(paths); !errors.Is(err, ErrNotReady) {
		t.Errorf("before the handshake: %v, want ErrNotReady", err)
	}
	textOnly := newFileServer(t, CBUseLongFormatNames)
	if _, err := textOnly.h.SetLocalFiles(paths); !errors.Is(err, ErrFileClipUnsupported) {
		t.Errorf("a server without file transfer: %v, want ErrFileClipUnsupported", err)
	}
	if textOnly.h.FileClipboardInForce() {
		t.Error("FileClipboardInForce with a server that did not offer it")
	}
	off := newFileServer(t, allFileCaps)
	if !off.h.FileClipboardInForce() {
		t.Error("FileClipboardInForce = false with file transfer negotiated")
	}
	off.h.SetEnabled(false)
	if _, err := off.h.SetLocalFiles(paths); !errors.Is(err, ErrClipboardDisabled) {
		t.Errorf("with sharing off: %v, want ErrClipboardDisabled", err)
	}
	if off.h.FileClipboardInForce() {
		t.Error("FileClipboardInForce with sharing off")
	}
	// Nothing was sent for any of them.
	for name, server := range map[string]*fileServer{"text only": textOnly, "off": off} {
		for _, pdu := range server.take() {
			if msgType, _, _, _ := decodePDU(pdu); msgType == CBFormatList {
				if formats := decodeFormatListLong(pdu[8:]); len(formats) > 0 && formats[0].FormatName == FormatFileGroupDescriptorW {
					t.Errorf("%s: files were announced", name)
				}
			}
		}
	}
}

func TestTurningSharingOffForgetsTheFiles(t *testing.T) {
	server, _, _ := copyTree(t)
	server.h.SetEnabled(false)
	server.take()
	server.h.SetEnabled(true)
	server.take()
	server.h.ProcessPDU(encodeFormatDataRequest(formatIDFileGroupDescriptorW))
	if _, flags, _ := server.last(t); flags != CBResponseFail {
		t.Fatalf("after sharing was switched off and on, the file list got flags 0x%04X", flags)
	}
}

func TestWhatIsLeftOutOfAnOfferIsCounted(t *testing.T) {
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "folder", "real.txt"), "x")
	link := filepath.Join(dir, "folder", "link")
	if err := os.Symlink(filepath.Join(dir, "folder", "real.txt"), link); err != nil {
		t.Skipf("symbolic links cannot be made here: %v", err)
	}
	missing := filepath.Join(dir, "gone")
	_, offer, err := buildSnapshot([]string{filepath.Join(dir, "folder"), missing}, true)
	if err != nil {
		t.Fatal(err)
	}
	if offer.Files != 1 || offer.Folders != 1 || offer.Skipped != 2 {
		t.Fatalf("offer = %+v, want 1 file, 1 folder, and the link and the missing path skipped", offer)
	}
	if offer.FirstSkipped != link {
		t.Fatalf("first skipped = %q, want the link %q", offer.FirstSkipped, link)
	}
	// A link the user selected is followed: they pointed at it.
	snapshot, offer, err := buildSnapshot([]string{link}, true)
	if err != nil || offer.Files != 1 || snapshot.entries[0].name != "link" || snapshot.entries[0].size != 1 {
		t.Fatalf("a selected link: %+v, %+v, %v", snapshot, offer, err)
	}
}

func TestNothingToOfferIsAnError(t *testing.T) {
	_, offer, err := buildSnapshot([]string{filepath.Join(t.TempDir(), "gone")}, true)
	if !errors.Is(err, ErrNothingToOffer) || offer.Skipped != 1 {
		t.Fatalf("err=%v offer=%+v", err, offer)
	}
	if _, _, err := buildSnapshot(nil, true); !errors.Is(err, ErrNothingToOffer) {
		t.Fatalf("no paths: %v", err)
	}
}

func TestTooManyFilesAreRefusedNotCut(t *testing.T) {
	old := offerEntryLimit
	offerEntryLimit = 5
	t.Cleanup(func() { offerEntryLimit = old })
	dir := t.TempDir()
	for index := 0; index < 6; index++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%d", index)), "x")
	}
	if _, offer, err := buildSnapshot([]string{dir}, true); !errors.Is(err, ErrTooManyFiles) || offer.FirstSkipped == "" {
		t.Fatalf("err = %v, offer = %+v, want ErrTooManyFiles", err, offer)
	}
	if _, _, err := buildSnapshot([]string{filepath.Join(dir, "f0"), filepath.Join(dir, "f1")}, true); err != nil {
		t.Fatalf("a selection within the limit: %v", err)
	}
}

func TestAPathTooLongForADescriptorIsRefusedWithItsName(t *testing.T) {
	dir := t.TempDir()
	long := filepath.Join(dir, "folder", strings.Repeat("长", maxNameUnits))
	if err := os.MkdirAll(filepath.Dir(long), 0o700); err != nil {
		t.Skipf("a long name cannot be made here: %v", err)
	}
	if err := os.WriteFile(long, []byte("x"), 0o600); err != nil {
		t.Skipf("a long name cannot be made here: %v", err)
	}
	_, offer, err := buildSnapshot([]string{filepath.Join(dir, "folder")}, true)
	if !errors.Is(err, ErrNameTooLong) || offer.FirstSkipped != long {
		t.Fatalf("err = %v, offer = %+v, want ErrNameTooLong naming the file", err, offer)
	}
}

func TestFilesOf4GiBNeedTheServersHugeFileSupport(t *testing.T) {
	old := hugeFileLimit
	hugeFileLimit = 10
	t.Cleanup(func() { hugeFileLimit = old })
	dir := t.TempDir()
	writeFile(t, filepath.Join(dir, "small"), "123456789")
	writeFile(t, filepath.Join(dir, "big"), "0123456789")
	_, offer, err := buildSnapshot([]string{dir}, false)
	if err != nil || offer.Files != 1 || offer.Skipped != 1 || filepath.Base(offer.FirstSkipped) != "big" {
		t.Fatalf("without huge file support: %+v, %v", offer, err)
	}
	if _, offer, err = buildSnapshot([]string{dir}, true); err != nil || offer.Files != 2 || offer.Skipped != 0 {
		t.Fatalf("with huge file support: %+v, %v", offer, err)
	}
	// A server that did not announce it gets the same through the handler.
	server := newFileServer(t, CBUseLongFormatNames|CBStreamFileclipEnabled)
	if offer, err := server.h.SetLocalFiles([]string{dir}); err != nil || offer.Skipped != 1 {
		t.Fatalf("handler without the flag: %+v, %v", offer, err)
	}
}

func TestNamesAreMadeSafeForWindows(t *testing.T) {
	cases := map[string]string{
		"plain.txt":    "plain.txt",
		"中文 name":      "中文 name",
		`back\slash`:   "back_slash",
		"a:b*c?d":      "a_b_c_d",
		`q"<>|`:        "q____",
		"tab\there":    "tab_here",
		"already_safe": "already_safe",
	}
	for in, want := range cases {
		if got := windowsName(in); got != want {
			t.Errorf("windowsName(%q) = %q, want %q", in, got, want)
		}
	}
	entries := []fileEntry{{name: "x", size: 1 << 33, mtime: time.Unix(0, 0)}}
	list := decodeFileList(t, encodeFileList(entries))
	if list[0].size != 1<<33 {
		t.Errorf("a size above 4 GiB reads back as %d", list[0].size)
	}
}

func TestFileTimeIsTheWindowsEpoch(t *testing.T) {
	// 1970-01-01 is 11644473600 seconds after 1601-01-01.
	if got := fileTime(time.Unix(0, 0)); got != 116444736000000000 {
		t.Errorf("fileTime(epoch) = %d", got)
	}
	if got := fileTime(time.Unix(1, 500)); got != 116444736010000005 {
		t.Errorf("fileTime(1s+500ns) = %d", got)
	}
	if fileTime(time.Time{}) != 0 || fileTime(time.Unix(-1<<40, 0)) != 0 {
		t.Error("a time that is not representable must be 0")
	}
}

// A copied file is read for each request and let go at its end: on Windows a
// file that is open cannot be deleted or renamed, and the user may do either to
// a file they copied while the paste is still waiting.
func TestNoCopiedFileIsHeldOpenBetweenRequests(t *testing.T) {
	server, dir, _ := copyTree(t)
	path := filepath.Join(dir, "中文 report.txt")
	if ok, _, _ := fileContents(t, server, contentsRequest(1, 0, FileContentsRange, 0, 4, nil)); !ok {
		t.Fatal("the file was not served")
	}
	if err := os.Rename(path, path+".moved"); err != nil {
		t.Fatalf("a file that was read cannot be renamed: %v", err)
	}
	if err := os.Remove(path + ".moved"); err != nil {
		t.Fatalf("a file that was read cannot be deleted: %v", err)
	}
	if ok, _, _ := fileContents(t, server, contentsRequest(2, 0, FileContentsRange, 0, 4, nil)); ok {
		t.Fatal("a file that was deleted was served")
	}
}

// The application's goroutine copies, replaces and drops lists while the
// connection's reads files for the server: run under -race.
func TestCopyingWhileTheServerReadsIsRaceFree(t *testing.T) {
	dir := t.TempDir()
	for index := 0; index < 4; index++ {
		writeFile(t, filepath.Join(dir, fmt.Sprintf("f%d", index)), strings.Repeat("x", 4096))
	}
	server := newFileServer(t, allFileCaps)
	var group sync.WaitGroup
	stop := make(chan struct{})
	group.Add(2)
	go func() { // the application
		defer group.Done()
		for index := 0; ; index++ {
			select {
			case <-stop:
				return
			default:
			}
			switch index % 3 {
			case 0:
				_, _ = server.h.SetLocalFiles([]string{filepath.Join(dir, fmt.Sprintf("f%d", index%4))})
			case 1:
				_ = server.h.SetLocalClipboard("text")
			default:
				server.h.SetEnabled(index%2 == 0)
			}
		}
	}()
	go func() { // the connection
		defer group.Done()
		for index := uint32(0); ; index++ {
			select {
			case <-stop:
				return
			default:
			}
			server.h.ProcessPDU(lockPDU(CBLockClipData, index%3))
			server.h.ProcessPDU(encodeFormatDataRequest(formatIDFileGroupDescriptorW))
			server.h.ProcessPDU(contentsRequest(index, 0, FileContentsRange, 0, 4096, nil))
			server.h.ProcessPDU(contentsRequest(index, 0, FileContentsRange, 0, 4096, func() *uint32 { id := index % 3; return &id }()))
			server.h.ProcessPDU(lockPDU(CBUnlockClipData, index%3))
		}
	}()
	time.Sleep(300 * time.Millisecond)
	close(stop)
	group.Wait()
	server.take()
}

func FuzzProcessPDUWithCopiedFiles(f *testing.F) {
	f.Add(contentsRequest(1, 0, FileContentsRange, 0, 16, nil))
	f.Add(contentsRequest(1, 1, FileContentsSize, 0, 8, nil))
	f.Add(lockPDU(CBLockClipData, 3))
	f.Add(encodeFormatDataRequest(formatIDFileGroupDescriptorW))
	f.Add([]byte{0x08, 0x00, 0x00, 0x00, 0xff, 0xff, 0xff, 0xff})
	dir := f.TempDir()
	writeFile(f, filepath.Join(dir, "a.txt"), "0123456789abcdef")
	writeFile(f, filepath.Join(dir, "d", "b.txt"), "xyz")
	server := newFileServer(f, allFileCaps)
	if _, err := server.h.SetLocalFiles([]string{filepath.Join(dir, "a.txt"), filepath.Join(dir, "d")}); err != nil {
		f.Fatal(err)
	}
	f.Fuzz(func(t *testing.T, pdu []byte) {
		server.h.ProcessPDU(pdu)
		for _, answer := range server.take() {
			if msgType, flags, payload, err := decodePDU(answer); err == nil && msgType == CBFileContentsResponse && flags == CBResponseOK && len(payload) > 4+maxContentsRequest {
				t.Fatalf("a response of %d bytes is beyond what one request may ask for", len(payload))
			}
		}
	})
}

func BenchmarkEncodeFileList(b *testing.B) {
	entries := make([]fileEntry, 1000)
	for index := range entries {
		entries[index] = fileEntry{name: fmt.Sprintf(`projects\viva\中文\file-%d.txt`, index), size: uint64(index) * 1024, mtime: time.Unix(1700000000, 0)}
	}
	b.ReportAllocs()
	b.SetBytes(int64(len(entries) * fileDescriptorSize))
	for b.Loop() {
		if len(encodeFileList(entries)) != 4+len(entries)*fileDescriptorSize {
			b.Fatal("short list")
		}
	}
}

func BenchmarkFileContentsRange64K(b *testing.B) {
	dir := b.TempDir()
	content := bytes.Repeat([]byte("0123456789abcdef"), 1<<20/16)
	if err := os.WriteFile(filepath.Join(dir, "big"), content, 0o600); err != nil {
		b.Fatal(err)
	}
	snapshot, _, err := buildSnapshot([]string{filepath.Join(dir, "big")}, true)
	if err != nil {
		b.Fatal(err)
	}
	request := fileContentsRequest{stream: 1, flags: FileContentsRange, size: 64 << 10}
	b.ReportAllocs()
	b.SetBytes(int64(request.size))
	position := uint64(0)
	for b.Loop() {
		request.position = position
		if data, ok := snapshot.contents(request); !ok || len(data) != 64<<10 {
			b.Fatal("short read")
		}
		position = (position + 64<<10) % uint64(len(content)-64<<10)
	}
}

func TestListDecodingHelperAgreesWithTheSpecLayout(t *testing.T) {
	// FILEDESCRIPTORW is 592 bytes: flags 4, reserved 32, attributes 4, reserved
	// 16, write time 8, size 8 (high, low), name 520.
	if fileDescriptorSize != 4+32+4+16+8+8+fileNameUnits*2 {
		t.Fatalf("descriptor size %d does not add up", fileDescriptorSize)
	}
	list := encodeFileList([]fileEntry{{name: "a", size: 0x0000000100000002, mtime: time.Unix(0, 0)}})
	raw := list[4:]
	if binary.LittleEndian.Uint32(raw[64:]) != 1 || binary.LittleEndian.Uint32(raw[68:]) != 2 {
		t.Fatalf("size high/low = %d/%d, want 1/2", binary.LittleEndian.Uint32(raw[64:]), binary.LittleEndian.Uint32(raw[68:]))
	}
	if !slices.Equal(raw[72:76], []byte{'a', 0, 0, 0}) {
		t.Fatalf("name bytes = %v", raw[72:76])
	}
}
