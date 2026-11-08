package sftpclient

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func TestWorkerOpensUTF8TextAcrossEverySplitBoundary(t *testing.T) {
	content := []byte("\xef\xbb\xbfalpha\n中文😀\n")
	for chunk := 1; chunk <= len(content); chunk++ {
		t.Run(strings.Repeat("x", chunk), func(t *testing.T) {
			remote := &fakeRemote{
				closed: make(chan struct{}), chunk: chunk,
				files: map[string][]byte{"/remote/notes.txt": content},
			}
			worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
			defer worker.Close()
			if !worker.OpenText("/remote/notes.txt") {
				t.Fatal("open was not queued")
			}
			result := waitResult(t, worker)
			if result.Err != nil || result.Operation != OperationOpenText {
				t.Fatalf("result = %+v", result)
			}
			if result.Document.Text != "alpha\n中文😀\n" || !result.Document.UTF8BOM {
				t.Fatalf("document = %+v", result.Document)
			}
			if result.Document.Revision != Revision([32]byte{0}) && result.Document.Revision != Revision([32]byte{}) {
				return
			}
			t.Fatal("revision is empty")
		})
	}
}

func TestTextDocumentRejectsEncodingAndSizeBoundaries(t *testing.T) {
	valid := strings.Repeat("a", MaxEditFileBytes)
	encoded, err := encodeTextDocument(valid, false)
	if err != nil || len(encoded) != MaxEditFileBytes {
		t.Fatalf("exact limit = %d, %v", len(encoded), err)
	}
	for _, testCase := range []struct {
		name    string
		content []byte
		err     error
	}{
		{name: "invalid utf8", content: []byte{0xff}, err: ErrUnsupportedEncoding},
		{name: "utf16 bom", content: []byte{0xff, 0xfe, 'a', 0}, err: ErrUnsupportedEncoding},
		{name: "nul", content: []byte("a\x00b"), err: ErrUnsupportedEncoding},
		{name: "oversize", content: bytes.Repeat([]byte("a"), MaxEditFileBytes+1), err: ErrEditFileTooLarge},
	} {
		t.Run(testCase.name, func(t *testing.T) {
			remote := &fakeRemote{closed: make(chan struct{}), files: map[string][]byte{"bad": testCase.content}, chunk: 7}
			worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
			defer worker.Close()
			worker.OpenText("bad")
			if result := waitResult(t, worker); !errors.Is(result.Err, testCase.err) {
				t.Fatalf("error = %v, want %v", result.Err, testCase.err)
			}
		})
	}
	if _, err := encodeTextDocument(valid, true); !errors.Is(err, ErrEditFileTooLarge) {
		t.Fatalf("BOM boundary error = %v", err)
	}
}

func TestWorkerCancelInterruptsRemoteTextOpen(t *testing.T) {
	remote := &fakeRemote{
		closed: make(chan struct{}), opened: make(chan struct{}), block: true,
		files: map[string][]byte{"blocked.txt": []byte("unused")},
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	if !worker.OpenText("blocked.txt") {
		t.Fatal("open was not queued")
	}
	<-remote.opened
	worker.Cancel()
	if result := waitResult(t, worker); !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("cancel result = %+v", result)
	}
}

func TestWorkerSavesTextAtomicallyAndDetectsConflict(t *testing.T) {
	worker, root := newProtocolWorker(t)
	remotePath := filepath.Join(root, "notes.txt")
	if err := os.WriteFile(remotePath, append([]byte{}, append(utf8ByteOrderMark, []byte("before\n")...)...), 0o640); err != nil {
		t.Fatal(err)
	}
	if !worker.OpenText("notes.txt") {
		t.Fatal("open was not queued")
	}
	opened := waitResult(t, worker)
	if opened.Err != nil || opened.Document.Text != "before\n" || !opened.Document.UTF8BOM {
		t.Fatalf("opened = %+v", opened)
	}
	if !worker.SaveText("notes.txt", "after\n中文\n", opened.Document.Revision, opened.Document.UTF8BOM) {
		t.Fatal("save was not queued")
	}
	saved := waitResult(t, worker)
	if saved.Err != nil || saved.Operation != OperationSaveText || saved.Document.Text != "after\n中文\n" {
		t.Fatalf("saved = %+v", saved)
	}
	content, err := os.ReadFile(remotePath)
	if err != nil || !bytes.Equal(content, append(append([]byte{}, utf8ByteOrderMark...), []byte("after\n中文\n")...)) {
		t.Fatalf("remote = %q, %v", content, err)
	}
	// Windows has no permission bits to preserve.
	if info, err := os.Stat(remotePath); err != nil || runtime.GOOS != "windows" && info.Mode().Perm() != 0o640 {
		t.Fatalf("mode = %+v, %v", info, err)
	}
	matches, err := filepath.Glob(filepath.Join(root, ".motty.*.part"))
	if err != nil || len(matches) != 0 {
		t.Fatalf("temporary files = %v, %v", matches, err)
	}

	if err := os.WriteFile(remotePath, []byte("external change\n"), 0o640); err != nil {
		t.Fatal(err)
	}
	if !worker.SaveText("notes.txt", "must not win\n", saved.Document.Revision, saved.Document.UTF8BOM) {
		t.Fatal("conflicting save was not queued")
	}
	conflict := waitResult(t, worker)
	if !errors.Is(conflict.Err, ErrRemoteChanged) {
		t.Fatalf("conflict = %+v", conflict)
	}
	if content, err = os.ReadFile(remotePath); err != nil || string(content) != "external change\n" {
		t.Fatalf("remote changed after conflict = %q, %v", content, err)
	}
}

func TestWorkerTreatsRemoteSizeChangesAsConflicts(t *testing.T) {
	worker, root := newProtocolWorker(t)
	remotePath := filepath.Join(root, "boundary.txt")
	if err := os.WriteFile(remotePath, []byte("before"), 0o600); err != nil {
		t.Fatal(err)
	}
	worker.OpenText("boundary.txt")
	opened := waitResult(t, worker)
	if opened.Err != nil {
		t.Fatal(opened.Err)
	}
	if err := os.WriteFile(remotePath, bytes.Repeat([]byte("x"), MaxEditFileBytes+1), 0o600); err != nil {
		t.Fatal(err)
	}
	worker.SaveText("boundary.txt", "mine", opened.Document.Revision, false)
	if result := waitResult(t, worker); !errors.Is(result.Err, ErrRemoteChanged) {
		t.Fatalf("oversize conflict = %+v", result)
	}
}

// skipUnlessPermissionsApply skips where directory permission bits do not stop
// the SFTP server's process: Windows has none, and root ignores them.
func skipUnlessPermissionsApply(t *testing.T) {
	t.Helper()
	if runtime.GOOS == "windows" || os.Geteuid() == 0 {
		t.Skip("directory permission bits are not enforced for this user or platform")
	}
}

func TestWorkerSavesTextInPlaceWhereDirectoryIsReadOnly(t *testing.T) {
	skipUnlessPermissionsApply(t)
	worker, root := newProtocolWorker(t)
	directory := filepath.Join(root, "etc")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	remotePath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(remotePath, []byte("before\n"), 0o664); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o755) })
	before, err := os.Stat(remotePath)
	if err != nil {
		t.Fatal(err)
	}

	worker.OpenText("etc/config.yaml")
	opened := waitResult(t, worker)
	if opened.Err != nil {
		t.Fatal(opened.Err)
	}
	worker.SaveText("etc/config.yaml", "after\n", opened.Document.Revision, false)
	saved := waitResult(t, worker)
	if saved.Err != nil || saved.Document.Text != "after\n" {
		t.Fatalf("saved = %+v", saved)
	}
	if content, err := os.ReadFile(remotePath); err != nil || string(content) != "after\n" {
		t.Fatalf("remote = %q, %v", content, err)
	}
	after, err := os.Stat(remotePath)
	if err != nil || !os.SameFile(before, after) || after.Mode().Perm() != before.Mode().Perm() {
		t.Fatalf("in-place save must keep the file's identity and mode: %v %v, %v", before, after, err)
	}
	if matches, _ := filepath.Glob(filepath.Join(directory, ".motty.*.part")); len(matches) != 0 {
		t.Fatalf("temporary files = %v", matches)
	}

	// The fallback still refuses to overwrite a change made after the open.
	if err := os.WriteFile(remotePath, []byte("external change\n"), 0o664); err != nil {
		t.Fatal(err)
	}
	worker.SaveText("etc/config.yaml", "must not win\n", saved.Document.Revision, false)
	if conflict := waitResult(t, worker); !errors.Is(conflict.Err, ErrRemoteChanged) {
		t.Fatalf("conflict = %+v", conflict)
	}
	if content, err := os.ReadFile(remotePath); err != nil || string(content) != "external change\n" {
		t.Fatalf("remote changed after conflict = %q, %v", content, err)
	}
}

func TestWorkerKeepsPermissionErrorWhereFileAndDirectoryAreReadOnly(t *testing.T) {
	skipUnlessPermissionsApply(t)
	worker, root := newProtocolWorker(t)
	directory := filepath.Join(root, "etc")
	if err := os.Mkdir(directory, 0o755); err != nil {
		t.Fatal(err)
	}
	remotePath := filepath.Join(directory, "config.yaml")
	if err := os.WriteFile(remotePath, []byte("before\n"), 0o444); err != nil {
		t.Fatal(err)
	}
	if err := os.Chmod(directory, 0o555); err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = os.Chmod(directory, 0o755) })

	worker.OpenText("etc/config.yaml")
	opened := waitResult(t, worker)
	if opened.Err != nil {
		t.Fatal(opened.Err)
	}
	worker.SaveText("etc/config.yaml", "after\n", opened.Document.Revision, false)
	if result := waitResult(t, worker); !errors.Is(result.Err, os.ErrPermission) {
		t.Fatalf("error = %v, want a permission error", result.Err)
	}
	if content, err := os.ReadFile(remotePath); err != nil || string(content) != "before\n" {
		t.Fatalf("remote = %q, %v", content, err)
	}
}

func BenchmarkDecodeTextDocument(b *testing.B) {
	content := bytes.Repeat([]byte("line of UTF-8 中文\n"), 512)
	b.ReportAllocs()
	b.SetBytes(int64(len(content)))
	for b.Loop() {
		if _, err := decodeTextDocument(content); err != nil {
			b.Fatal(err)
		}
	}
}
