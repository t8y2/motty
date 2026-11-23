package sftpclient

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path"
	"path/filepath"
	"strings"
	"testing"
)

func TestWorkerSavesDocumentsWithLongPathsAndNames(t *testing.T) {
	worker, root := newProtocolWorker(t)
	directory := strings.Repeat("nested-directory/", 18)
	localDirectory := filepath.Join(root, filepath.FromSlash(directory))
	if err := os.MkdirAll(localDirectory, 0o700); err != nil {
		t.Fatal(err)
	}
	for _, test := range []struct {
		name          string
		before, after []byte
	}{
		{strings.Repeat("c", 250) + ".yml", []byte("名称=before\r\n"), []byte("名称=after\r\n")},
		{strings.Repeat("配", 83) + ".conf", []byte{0xff, 0xfe, 0x2d, 0x4e, 13, 0, 10, 0}, []byte{0xff, 0xfe, 0x87, 0x65, 13, 0, 10, 0}},
		{"配置.properties", []byte{0xd6, 0xd0, 13, 10}, []byte{0xce, 0xc4, 13, 10}},
	} {
		localPath := filepath.Join(localDirectory, test.name)
		if err := os.WriteFile(localPath, test.before, 0o600); err != nil {
			t.Fatal(err)
		}
		remotePath := path.Join(directory, test.name)
		opened := requireQueuedResult(t, worker.OpenDocument(remotePath, 1<<20), worker, OperationOpenDocument)
		if opened.Err != nil || !bytes.Equal(opened.Data, test.before) {
			t.Fatalf("long-path open: %v", opened.Err)
		}
		saved := requireQueuedResult(t, worker.SaveDocument(remotePath, test.after, Revision(sha256.Sum256(opened.Data)), 1<<20), worker, OperationSaveDocument)
		if saved.Err != nil {
			t.Fatalf("long-path save: %v", saved.Err)
		}
		actual, err := os.ReadFile(localPath)
		if err != nil || !bytes.Equal(actual, test.after) {
			t.Fatalf("long-path save changed encoded bytes: %x, %v", actual, err)
		}
	}
	leftovers, err := filepath.Glob(filepath.Join(localDirectory, ".motty.*.part"))
	if err != nil || len(leftovers) != 0 {
		t.Fatalf("temporary files leaked: %v, %v", leftovers, err)
	}
}

func TestWorkerDocumentBytesRemainUnchangedAndRejectStaleRevision(t *testing.T) {
	worker, root := newProtocolWorker(t)
	for _, original := range [][]byte{{0x89, 'P', 'N', 'G', 0, 0xff}, {0xff, 0xfe, 'a', 0, 13, 0, 10, 0}, {0xd6, 0xd0, 0xce, 0xc4, 13, 10}} {
		location := filepath.Join(root, "document")
		if err := os.WriteFile(location, original, 0600); err != nil {
			t.Fatal(err)
		}
		if !worker.OpenDocument("document", 1<<20) {
			t.Fatal("open not queued")
		}
		opened := waitResult(t, worker)
		if opened.Err != nil || opened.Operation != OperationOpenDocument || !bytes.Equal(opened.Data, original) {
			t.Fatalf("raw open: %+v", opened)
		}
		revised := append(bytes.Clone(original), 'x')
		if !worker.SaveDocument("document", revised, Revision(sha256.Sum256(original)), 1<<20) {
			t.Fatal("save not queued")
		}
		revised[len(revised)-1] = 'y' // request owns its bytes, caller may reuse its buffer
		saved := waitResult(t, worker)
		actual, err := os.ReadFile(location)
		want := append(bytes.Clone(original), 'x')
		if err != nil || saved.Err != nil || !bytes.Equal(actual, want) || !bytes.Equal(saved.Data, want) {
			t.Fatalf("raw save: %x %+v %v", actual, saved, err)
		}
		if !worker.SaveDocument("document", []byte("must not overwrite"), Revision(sha256.Sum256(original)), 1<<20) {
			t.Fatal("conflict not queued")
		}
		if conflict := waitResult(t, worker); !errors.Is(conflict.Err, ErrRemoteChanged) {
			t.Fatalf("stale revision accepted: %+v", conflict)
		}
		files, _ := filepath.Glob(filepath.Join(root, ".motty.*.part"))
		if len(files) != 0 {
			t.Fatal("temporary file leaked", files)
		}
	}
}

func TestWorkerDocumentReadHonorsTheCallersLimit(t *testing.T) {
	const limit = 8
	remote := &fakeRemote{closed: make(chan struct{}), files: map[string][]byte{"large": bytes.Repeat([]byte{'a'}, limit+1)}, chunk: 4}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	worker.OpenDocument("large", limit)
	if result := waitResult(t, worker); !errors.Is(result.Err, ErrPreviewFileTooLarge) {
		t.Fatalf("read past the caller's limit: %v", result.Err)
	}
	worker.OpenDocument("large", limit+1)
	if result := waitResult(t, worker); result.Err != nil || !bytes.Equal(result.Data, remote.files["large"]) {
		t.Fatalf("refused a document at the caller's limit: %v", result.Err)
	}
	blocked := &fakeRemote{closed: make(chan struct{}), opened: make(chan struct{}), block: true, files: map[string][]byte{"blocked": []byte("text")}}
	other := NewWorker(func() (Remote, error) { return blocked, nil }, nil)
	defer other.Close()
	other.OpenDocument("blocked", 1<<20)
	<-blocked.opened
	other.Cancel()
	if result := waitResult(t, other); !errors.Is(result.Err, ErrCanceled) {
		t.Fatal("canceled read succeeded", result.Err)
	}
}

func TestWorkerReadsCurrentDocumentForComparisonWithoutAdvancingRevision(t *testing.T) {
	worker, root := newProtocolWorker(t)
	location := filepath.Join(root, "document")
	original := []byte("before\n")
	if err := os.WriteFile(location, original, 0o600); err != nil {
		t.Fatal(err)
	}
	opened := requireQueuedResult(t, worker.OpenDocument("document", 1<<20), worker, OperationOpenDocument)
	if opened.Err != nil || !bytes.Equal(opened.Data, original) {
		t.Fatalf("open: %q, %v", opened.Data, opened.Err)
	}
	current := []byte("changed on the server\n")
	if err := os.WriteFile(location, current, 0o600); err != nil {
		t.Fatal(err)
	}
	read := requireQueuedResult(t, worker.ReadDocument("document"), worker, OperationReadDocument)
	if read.Err != nil || !bytes.Equal(read.Data, current) {
		t.Fatalf("comparison read: %q, %v", read.Data, read.Err)
	}
	// Reading for comparison must not make the original revision appear current.
	saved := requireQueuedResult(t, worker.SaveDocument("document", []byte("local\n"), Revision(sha256.Sum256(original)), 1<<20), worker, OperationSaveDocument)
	if !errors.Is(saved.Err, ErrRemoteChanged) {
		t.Fatalf("comparison read advanced the editor revision: %v", saved.Err)
	}
}

func TestWorkerReadDocumentUsesTheEditorSizeLimit(t *testing.T) {
	remote := &fakeRemote{
		closed: make(chan struct{}),
		files:  map[string][]byte{"large": bytes.Repeat([]byte{'a'}, MaxEditFileBytes+1)},
		chunk:  32768,
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	result := requireQueuedResult(t, worker.ReadDocument("large"), worker, OperationReadDocument)
	if !errors.Is(result.Err, ErrEditFileTooLarge) {
		t.Fatalf("comparison read escaped the editor size limit: %v", result.Err)
	}
}

// A document much larger than one SFTP packet is read through the pipelined
// path of a real server: the bytes must arrive whole and in order, an empty
// file must read as empty, and the caller's limit must hold.
func TestWorkerDocumentPipelinedReadKeepsBytesInOrderAndHonorsTheLimit(t *testing.T) {
	worker, root := newProtocolWorker(t)
	content := make([]byte, 3<<20+12345)
	for i := range content {
		content[i] = byte(i*2654435761>>13) | 0x20
	}
	for name, data := range map[string][]byte{"large.log": content, "empty.log": {}} {
		if err := os.WriteFile(filepath.Join(root, name), data, 0o600); err != nil {
			t.Fatal(err)
		}
		opened := requireQueuedResult(t, worker.OpenDocument(name, len(content)), worker, OperationOpenDocument)
		if opened.Err != nil || !bytes.Equal(opened.Data, data) {
			t.Fatalf("%s: read %d of %d bytes: %v", name, len(opened.Data), len(data), opened.Err)
		}
	}
	refused := requireQueuedResult(t, worker.OpenDocument("large.log", len(content)-1), worker, OperationOpenDocument)
	if !errors.Is(refused.Err, ErrPreviewFileTooLarge) {
		t.Fatalf("read past the limit: %v", refused.Err)
	}
}
