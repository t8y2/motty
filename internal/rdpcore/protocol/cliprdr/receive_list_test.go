package cliprdr

import (
	"bytes"
	"os"
	"testing"
	"time"
)

// fileListServer returns a server whose clipboard holds files, and a handler
// that has announced it.
func fileListServer(t *testing.T, files []fileEntry, data map[string][]byte) *receiveServer {
	t.Helper()
	server := newReceiveServer(t, files, data)
	server.handler.ProcessPDU(encodeCapsPDU(CBCapsVersion2, CBStreamFileclipEnabled|CBCanLockClipData))
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))
	return server
}

// requestFileList asks for the list and returns it with its version.
func requestFileList(t *testing.T, server *receiveServer) ([]RemoteFile, uint64) {
	t.Helper()
	var (
		files   []RemoteFile
		version uint64
		err     error
	)
	done := make(chan struct{})
	server.handler.OnRemoteFileList(func(list []RemoteFile, v uint64, listErr error) {
		files, version, err = list, v, listErr
		close(done)
	})
	if err := server.handler.RequestRemoteFileList(); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the file list never arrived")
	}
	if err != nil {
		t.Fatal(err)
	}
	return files, version
}

// TestTheRemoteFileListIsReadOnlyWhenAsked is the point of the deferred list:
// the descriptors arrive without a byte of file content, and every range read
// goes back to the server for exactly the bytes a paste asked for.
func TestTheRemoteFileListIsReadOnlyWhenAsked(t *testing.T) {
	big := bytes.Repeat([]byte("0123456789abcdef"), 10000) // 160000 bytes
	files := []fileEntry{
		{name: "big.bin", size: uint64(len(big))},
		{name: "folder", dir: true},
		{name: `folder\note.txt`, size: 5},
	}
	data := map[string][]byte{"big.bin": big, `folder\note.txt`: []byte("note!")}
	server := fileListServer(t, files, data)

	fileList, version := requestFileList(t, server)
	if len(fileList) != 3 || fileList[0].Name != "big.bin" || !fileList[1].Dir {
		t.Fatalf("the list is %+v", fileList)
	}
	server.mu.Lock()
	contents := server.contentsRequests
	server.mu.Unlock()
	if contents != 0 {
		t.Fatalf("%d file contents requests were made before a paste asked for them", contents)
	}

	first, err := server.handler.ReadRemoteFileRange(version, 0, 0, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(first, big[:64<<10]) {
		t.Fatalf("the first range is %d bytes, want the file's first 64 KiB", len(first))
	}
	second, err := server.handler.ReadRemoteFileRange(version, 0, 64<<10, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(second, big[64<<10:128<<10]) {
		t.Fatalf("the second range is %d bytes, want the next 64 KiB", len(second))
	}
	// A range that runs past the end is cut at the size the descriptor named.
	last, err := server.handler.ReadRemoteFileRange(version, 0, int64(len(big))-10, 64<<10)
	if err != nil {
		t.Fatal(err)
	}
	if !bytes.Equal(last, big[len(big)-10:]) {
		t.Fatalf("the last range is %d bytes, want the file's last 10", len(last))
	}
	if _, err := server.handler.ReadRemoteFileRange(version, 0, int64(len(big)), 8); err != nil {
		t.Fatalf("a range past the end: %v", err)
	}
	if _, err := server.handler.ReadRemoteFileRange(version, 1, 0, 4); err == nil {
		t.Fatal("a directory was read as a file")
	}

	server.mu.Lock()
	locked, requests, lockedRequests := server.locked, server.contentsRequests, server.contentsLocked
	server.mu.Unlock()
	if locked == 0 || requests != lockedRequests {
		t.Fatalf("%d of %d reads carried the lock %d", lockedRequests, requests, locked)
	}

	server.handler.ReleaseRemoteFileList(version)
	waitFor(t, "the lock to be released", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.unlocked == locked
	})
	if _, err := server.handler.ReadRemoteFileRange(version, 0, 0, 4); err == nil {
		t.Fatal("a released list was read")
	}
}

// TestAReplacedFileListIsNotReleasedByItsOldVersion keeps one clipboard change
// from tearing down the data a newer list still reads.
func TestAReplacedFileListIsNotReleasedByItsOldVersion(t *testing.T) {
	files := []fileEntry{{name: "a.txt", size: 3}}
	data := map[string][]byte{"a.txt": []byte("abc")}
	server := fileListServer(t, files, data)
	_, first := requestFileList(t, server)

	// The remote copies something else: the old list and its lock are let go.
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{{FormatID: CFUnicodeText}}))
	waitFor(t, "the first list's lock to be released", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.locked != 0 && server.unlocked == server.locked
	})
	server.handler.ReleaseRemoteFileList(first)
	if _, err := server.handler.ReadRemoteFileRange(first, 0, 0, 3); err == nil {
		t.Fatal("the replaced list was read")
	}

	// A new file list gets its own version and lock; releasing the old version
	// must leave them alone.
	server.handler.ProcessPDU(encodeFormatListShort([]FormatListEntry{
		{FormatID: formatIDFileGroupDescriptorW, FormatName: FormatFileGroupDescriptorW},
		{FormatID: formatIDFileContents, FormatName: FormatFileContents},
	}))
	_, second := requestFileList(t, server)
	if second == first {
		t.Fatal("the new list reused the old version")
	}
	server.handler.ReleaseRemoteFileList(first)
	if got, err := server.handler.ReadRemoteFileRange(second, 0, 0, 3); err != nil || string(got) != "abc" {
		t.Fatalf("the new list after releasing the old version = %q, %v", got, err)
	}
	server.handler.ReleaseRemoteFileList(second)
}

// TestDownloadingAListThatWasNotPromised keeps the files reachable when the
// caller cannot promise them: the same list is read into a staging directory.
func TestDownloadingAListThatWasNotPromised(t *testing.T) {
	files := []fileEntry{{name: "a.txt", size: 3}}
	server := fileListServer(t, files, map[string][]byte{"a.txt": []byte("abc")})
	_, version := requestFileList(t, server)

	var (
		got RemoteFiles
		err error
	)
	done := make(chan struct{})
	server.handler.OnRemoteFiles(func(files RemoteFiles, filesErr error) {
		got, err = files, filesErr
		close(done)
	})
	if err := server.handler.DownloadRemoteFiles(version); err != nil {
		t.Fatal(err)
	}
	select {
	case <-done:
	case <-time.After(5 * time.Second):
		t.Fatal("the files were never downloaded")
	}
	if err != nil || got.Files != 1 {
		t.Fatalf("downloaded %+v, %v", got, err)
	}
	defer os.RemoveAll(got.Dir)
	if body, err := os.ReadFile(got.Paths[0]); err != nil || string(body) != "abc" {
		t.Fatalf("the downloaded file is %q, %v", body, err)
	}
	waitFor(t, "the lock to be released after the download", func() bool {
		server.mu.Lock()
		defer server.mu.Unlock()
		return server.locked != 0 && server.unlocked == server.locked
	})
}

// TestAFileListRequestCarriesTheLock checks the lock is taken before the list is
// asked for, as Windows needs it to serve the contents at all.
func TestAFileListRequestCarriesTheLock(t *testing.T) {
	server := fileListServer(t, []fileEntry{{name: "a.txt", size: 1}}, map[string][]byte{"a.txt": []byte("a")})
	_, version := requestFileList(t, server)
	server.mu.Lock()
	locked := server.locked
	server.mu.Unlock()
	if locked == 0 {
		t.Fatal("the file list was asked for without locking the server's clipboard data")
	}
	if _, err := server.handler.ReadRemoteFileRange(version, 0, 0, 1); err != nil {
		t.Fatal(err)
	}
}
