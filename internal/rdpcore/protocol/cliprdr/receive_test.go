package cliprdr

import (
	"encoding/binary"
	"errors"
	"os"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

// announceFileFormats puts the handler in the state the server leaves it in
// after its clipboard came to hold files.
func announceFileFormats(h *Handler) {
	h.ProcessPDU(encodeCapsPDU(CBCapsVersion2, CBStreamFileclipEnabled|CBCanLockClipData))
	h.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))
}

func announceTextFormat(h *Handler) {
	h.ProcessPDU(encodeFormatListShort([]FormatListEntry{{FormatID: CFUnicodeText}}))
}

// waitFor polls cond for a short time; it is for a message sent from another
// goroutine, not a substitute for a timeout on the protocol.
func waitFor(t *testing.T, what string, cond func() bool) {
	t.Helper()
	deadline := time.Now().Add(2 * time.Second)
	for time.Now().Before(deadline) {
		if cond() {
			return
		}
		time.Sleep(5 * time.Millisecond)
	}
	t.Fatalf("timed out waiting for %s", what)
}

// receiveServer answers the client's requests for the files its clipboard
// holds, as a Windows server does.
type receiveServer struct {
	handler  *Handler
	files    []fileEntry
	data     map[string][]byte
	mu       sync.Mutex
	sent     []uint16
	locked   uint32 // the id the client locked, 0 when it did not
	unlocked uint32 // the id the client unlocked
	// contentsRequests counts the file contents requests and how many carried
	// the lock id.
	contentsRequests, contentsLocked int
}

func newReceiveServer(t *testing.T, files []fileEntry, data map[string][]byte) *receiveServer {
	t.Helper()
	server := &receiveServer{files: files, data: data}
	server.handler = NewHandler(server.send, testLog)
	return server
}

func (server *receiveServer) send(pdu []byte) error {
	msgType, flags, payload, err := decodePDU(pdu)
	if err != nil {
		return err
	}
	server.mu.Lock()
	server.sent = append(server.sent, msgType)
	server.mu.Unlock()
	switch msgType {
	case CBLockClipData, CBUnlockClipData:
		if len(payload) >= 4 {
			id := binary.LittleEndian.Uint32(payload)
			server.mu.Lock()
			if msgType == CBLockClipData {
				server.locked = id
			} else {
				server.unlocked = id
			}
			server.mu.Unlock()
		}
	case CBFormatDataRequest:
		if len(payload) >= 4 && binary.LittleEndian.Uint32(payload) == formatIDFileGroupDescriptorW {
			server.handler.ProcessPDU(encodeFormatDataResponse(true, encodeFileList(server.files)))
		} else {
			server.handler.ProcessPDU(encodeFormatDataResponse(false, nil))
		}
	case CBFileContentsRequest:
		server.answerContents(flags, payload)
	}
	return nil
}

func (server *receiveServer) answerContents(_ uint16, payload []byte) {
	request, ok := decodeFileContentsRequest(payload)
	server.mu.Lock()
	locked := server.locked
	server.contentsRequests++
	if request.hasClipData && request.clipDataID == locked {
		server.contentsLocked++
	}
	server.mu.Unlock()
	if !ok || int(request.index) >= len(server.files) || (locked != 0 && (!request.hasClipData || request.clipDataID != locked)) {
		server.handler.ProcessPDU(encodeFileContentsResponse(request.stream, false, nil))
		return
	}
	entry := server.files[request.index]
	content := server.data[entry.name]
	switch request.flags {
	case FileContentsSize:
		size := make([]byte, 8)
		binary.LittleEndian.PutUint64(size, uint64(len(content)))
		server.handler.ProcessPDU(encodeFileContentsResponse(request.stream, true, size))
	case FileContentsRange:
		if request.position > uint64(len(content)) || request.position+uint64(request.size) > uint64(len(content)) {
			server.handler.ProcessPDU(encodeFileContentsResponse(request.stream, false, nil))
			return
		}
		server.handler.ProcessPDU(encodeFileContentsResponse(request.stream, true, content[request.position:request.position+uint64(request.size)]))
	default:
		server.handler.ProcessPDU(encodeFileContentsResponse(request.stream, false, nil))
	}
}

func TestRemoteFilesAreReadIntoTheirTopLevelEntries(t *testing.T) {
	files := []fileEntry{
		{name: "hello 中文.txt", size: 5},
		{name: "folder", dir: true},
		{name: `folder\inner.txt`, size: 7},
	}
	data := map[string][]byte{
		"hello 中文.txt":     []byte("hello"),
		`folder\inner.txt`: []byte("inside!"),
	}
	server := newReceiveServer(t, files, data)
	server.handler.ProcessPDU(encodeCapsPDU(CBCapsVersion2, CBStreamFileclipEnabled|CBCanLockClipData))
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))

	var got RemoteFiles
	var gotErr error
	done := make(chan struct{})
	server.handler.OnRemoteFiles(func(files RemoteFiles, err error) {
		got, gotErr = files, err
		close(done)
	})
	var hasFiles bool
	server.handler.OnRemoteCopy(func(_, _, files bool) { hasFiles = files })
	// The format list above already ran; ask again through the same state.
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))
	if !hasFiles {
		t.Fatal("the file formats were not announced")
	}
	if err := server.handler.RequestRemoteFiles(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the files never arrived")
	}
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	defer os.RemoveAll(got.Dir)
	if got.Files != 2 || got.Folders != 1 || got.Bytes != 12 || got.Skipped != 0 {
		t.Fatalf("received %+v", got)
	}
	want := []string{filepath.Join(got.Dir, "folder"), filepath.Join(got.Dir, "hello 中文.txt")}
	if !slices.Equal(got.Paths, want) {
		t.Fatalf("paths = %q, want %q", got.Paths, want)
	}
	inner, err := os.ReadFile(filepath.Join(got.Dir, "folder", "inner.txt"))
	if err != nil || string(inner) != "inside!" {
		t.Fatalf("inner = %q, %v", inner, err)
	}
	server.mu.Lock()
	locked, requests, lockedRequests := server.locked, server.contentsRequests, server.contentsLocked
	server.mu.Unlock()
	if locked == 0 {
		t.Fatal("the server's clipboard data was not locked")
	}
	if requests == 0 || lockedRequests != requests {
		t.Fatalf("%d of %d file contents requests carried the lock", lockedRequests, requests)
	}
	waitFor(t, "the lock to be released", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.unlocked == locked
	})
}

func TestRemoteFilesCanBeReadWithoutLocking(t *testing.T) {
	files := []fileEntry{{name: "a.txt", size: 3}}
	server := newReceiveServer(t, files, map[string][]byte{"a.txt": []byte("abc")})
	server.handler.ProcessPDU(encodeCapsPDU(CBCapsVersion2, CBStreamFileclipEnabled))
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))
	var got RemoteFiles
	var gotErr error
	done := make(chan struct{})
	server.handler.OnRemoteFiles(func(files RemoteFiles, err error) {
		got, gotErr = files, err
		close(done)
	})
	if err := server.handler.RequestRemoteFiles(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the files never arrived")
	}
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	defer os.RemoveAll(got.Dir)
	server.mu.Lock()
	locked, requests, lockedRequests := server.locked, server.contentsRequests, server.contentsLocked
	server.mu.Unlock()
	if locked != 0 || lockedRequests != 0 {
		t.Fatalf("the server did not offer locking, but the client locked %d and tagged %d requests", locked, lockedRequests)
	}
	if requests == 0 {
		t.Fatal("no file contents were requested")
	}
}

func TestRemoteFileNamesCannotLeaveTheirDirectory(t *testing.T) {
	files := []fileEntry{
		{name: `..\escape.txt`, size: 1},
		{name: `folder\..\..\escape.txt`, size: 1},
		{name: "safe.txt", size: 3},
	}
	data := map[string][]byte{
		`..\escape.txt`:           []byte("x"),
		`folder\..\..\escape.txt`: []byte("x"),
		"safe.txt":                []byte("yes"),
	}
	server := newReceiveServer(t, files, data)
	server.handler.ProcessPDU(encodeCapsPDU(CBCapsVersion2, CBStreamFileclipEnabled|CBCanLockClipData))
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))
	var got RemoteFiles
	var gotErr error
	done := make(chan struct{})
	server.handler.OnRemoteFiles(func(files RemoteFiles, err error) {
		got, gotErr = files, err
		close(done)
	})
	if err := server.handler.RequestRemoteFiles(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the files never arrived")
	}
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	defer os.RemoveAll(got.Dir)
	if got.Files != 1 || got.Skipped != 2 || len(got.Paths) != 1 {
		t.Fatalf("received %+v", got)
	}
	if _, err := os.Stat(filepath.Join(filepath.Dir(got.Dir), "escape.txt")); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("a path escaped the staging directory: %v", err)
	}
}

func TestFileNamesUseWindowsRulesWhereWindowsDoes(t *testing.T) {
	cases := map[string]string{
		"report.pdf":       "report.pdf",
		"name.with.dots":   "name.with.dots",
		"COM1.tar.gz":      "_COM1.tar.gz",
		"con.txt":          "_con.txt",
		"LPT9":             "_LPT9",
		"a:b?.txt":         "a_b_.txt",
		"trail. ":          "trail",
		"nul":              "_nul",
		"<control\x01name": "_control_name",
	}
	for name, want := range cases {
		if got := localFileNameFor(name, true); got != want {
			t.Errorf("localFileNameFor(%q, windows) = %q, want %q", name, got, want)
		}
	}
	if got := localFileNameFor("a/b", false); got != "a_b" {
		t.Errorf("localFileNameFor(%q, unix) = %q, want a separator replaced", "a/b", got)
	}
	if got := localFileNameFor("CON", false); got != "CON" {
		t.Errorf("localFileNameFor(%q, unix) = %q, want the name unchanged", "CON", got)
	}
}

func TestAStaleFileListDoesNotBecomeTheNextText(t *testing.T) {
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		sent = append(sent, append([]byte(nil), data...))
		return nil
	}, testLog)
	announceFileFormats(h)
	if err := h.RequestRemoteFiles(); err != nil {
		t.Fatal(err)
	}
	// The remote clipboard changed before the server answered the file-list
	// request, and the application asked for the new text in the meantime.
	announceTextFormat(h)
	if err := h.RequestRemoteText(); err != nil {
		t.Fatal(err)
	}
	var text string
	var filesCalled bool
	h.OnTextData(func(value string) { text = value })
	h.OnRemoteFiles(func(RemoteFiles, error) { filesCalled = true })

	// The answer to the first request arrives first; it belongs to the old
	// clipboard and must not be read as the file list or as the text.
	h.ProcessPDU(encodeFormatDataResponse(true, encodeFileList([]fileEntry{{name: "old.txt", size: 4}})))
	h.ProcessPDU(encodeFormatDataResponse(true, textToWire("new text")))
	if text != "new text" {
		t.Fatalf("text = %q, want the second response", text)
	}
	if filesCalled {
		t.Fatal("a stale file list started a download")
	}
}

func TestResponsesAreMatchedToTheirRequests(t *testing.T) {
	var mu sync.Mutex
	var sent [][]byte
	h := NewHandler(func(data []byte) error {
		mu.Lock()
		sent = append(sent, append([]byte(nil), data...))
		mu.Unlock()
		return nil
	}, testLog)
	// Text was requested first, then the clipboard came to hold files and their
	// list was requested before the text answer arrived.
	if err := h.RequestRemoteText(); err != nil {
		t.Fatal(err)
	}
	announceFileFormats(h)
	if err := h.RequestRemoteFiles(); err != nil {
		t.Fatal(err)
	}
	var text string
	var got RemoteFiles
	var gotErr error
	done := make(chan struct{})
	h.OnTextData(func(value string) { text = value })
	h.OnRemoteFiles(func(files RemoteFiles, err error) {
		got, gotErr = files, err
		close(done)
	})
	h.ProcessPDU(encodeFormatDataResponse(true, textToWire("first answer")))
	if text != "first answer" {
		t.Fatalf("text = %q, want the first answer", text)
	}
	if gotErr != nil || len(got.Paths) != 0 {
		t.Fatalf("the text answer was read as files: %+v, %v", got, gotErr)
	}
	h.ProcessPDU(encodeFormatDataResponse(true, encodeFileList([]fileEntry{{name: "empty.txt", size: 0}})))
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the file list was never read")
	}
	if gotErr != nil {
		t.Fatal(gotErr)
	}
	defer os.RemoveAll(got.Dir)
	if got.Files != 1 || !slices.Equal(got.Paths, []string{filepath.Join(got.Dir, "empty.txt")}) {
		t.Fatalf("received %+v", got)
	}
	// The file list was the second request, so the server's answer order is the
	// order the application asked in: one text request then one file request.
	mu.Lock()
	count := len(sent)
	mu.Unlock()
	if count < 2 {
		t.Fatalf("sent %d requests", count)
	}
}

func TestARequestThatCouldNotBeSentLeavesNothingPending(t *testing.T) {
	h := NewHandler(func([]byte) error { return errors.New("the channel is closed") }, testLog)
	var text string
	h.OnTextData(func(value string) { text = value })
	if err := h.RequestRemoteText(); err == nil {
		t.Fatal("the send failure was not reported")
	}
	if h.pendingReq || len(h.pendingData) != 0 {
		t.Fatalf("pending = %v, %d", h.pendingReq, len(h.pendingData))
	}
	// With nothing outstanding, an unsolicited answer must not become the
	// clipboard's text.
	h.ProcessPDU(encodeFormatDataResponse(true, textToWire("stray")))
	if text != "" {
		t.Fatalf("an unsolicited response became text %q", text)
	}

	announceFileFormats(h)
	if err := h.RequestRemoteFiles(); err == nil {
		t.Fatal("the file-list send failure was not reported")
	}
	if h.pendingReq || len(h.pendingData) != 0 {
		t.Fatalf("pending after a file-list send failure = %v, %d", h.pendingReq, len(h.pendingData))
	}
}

func TestAFileListTheServerRefusesReleasesItsLock(t *testing.T) {
	var mu sync.Mutex
	var locked, unlocked uint32
	var h *Handler
	h = NewHandler(func(data []byte) error {
		msgType, _, payload, err := decodePDU(data)
		if err != nil {
			return err
		}
		switch msgType {
		case CBLockClipData:
			mu.Lock()
			locked = binary.LittleEndian.Uint32(payload)
			mu.Unlock()
		case CBUnlockClipData:
			mu.Lock()
			unlocked = binary.LittleEndian.Uint32(payload)
			mu.Unlock()
		case CBFormatDataRequest:
			h.ProcessPDU(encodeFormatDataResponse(false, nil))
		}
		return nil
	}, testLog)
	announceFileFormats(h)
	var gotErr error
	h.OnRemoteFiles(func(_ RemoteFiles, err error) { gotErr = err })
	if err := h.RequestRemoteFiles(); err != nil {
		t.Fatal(err)
	}
	if gotErr == nil {
		t.Fatal("a refused file list was not reported")
	}
	mu.Lock()
	defer mu.Unlock()
	if locked == 0 {
		t.Fatal("the clipboard data was not locked before the list request")
	}
	if unlocked != locked {
		t.Fatalf("unlocked %d, want the locked id %d", unlocked, locked)
	}
}
