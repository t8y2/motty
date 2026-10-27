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
		opened := requireQueuedResult(t, worker.OpenDocument(remotePath), worker, OperationOpenDocument)
		if opened.Err != nil || !bytes.Equal(opened.Data, test.before) {
			t.Fatalf("long-path open: %v", opened.Err)
		}
		saved := requireQueuedResult(t, worker.SaveDocument(remotePath, test.after, Revision(sha256.Sum256(opened.Data))), worker, OperationSaveDocument)
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
		if !worker.OpenDocument("document") {
			t.Fatal("open not queued")
		}
		opened := waitResult(t, worker)
		if opened.Err != nil || opened.Operation != OperationOpenDocument || !bytes.Equal(opened.Data, original) {
			t.Fatalf("raw open: %+v", opened)
		}
		revised := append(bytes.Clone(original), 'x')
		if !worker.SaveDocument("document", revised, Revision(sha256.Sum256(original))) {
			t.Fatal("save not queued")
		}
		revised[len(revised)-1] = 'y' // request owns its bytes, caller may reuse its buffer
		saved := waitResult(t, worker)
		actual, err := os.ReadFile(location)
		want := append(bytes.Clone(original), 'x')
		if err != nil || saved.Err != nil || !bytes.Equal(actual, want) || !bytes.Equal(saved.Data, want) {
			t.Fatalf("raw save: %x %+v %v", actual, saved, err)
		}
		if !worker.SaveDocument("document", []byte("must not overwrite"), Revision(sha256.Sum256(original))) {
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

func TestWorkerDocumentReadLimitAndCancellation(t *testing.T) {
	remote := &fakeRemote{closed: make(chan struct{}), files: map[string][]byte{"large": bytes.Repeat([]byte{'a'}, MaxPreviewFileBytes+1)}, chunk: 32768}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	worker.OpenDocument("large")
	if result := waitResult(t, worker); !errors.Is(result.Err, ErrPreviewFileTooLarge) {
		t.Fatal("unbounded raw read", result.Err)
	}
	if worker.SaveDocument("large", remote.files["large"], Revision{}) {
		t.Fatal("unbounded save queued")
	}
	blocked := &fakeRemote{closed: make(chan struct{}), opened: make(chan struct{}), block: true, files: map[string][]byte{"blocked": []byte("text")}}
	other := NewWorker(func() (Remote, error) { return blocked, nil }, nil)
	defer other.Close()
	other.OpenDocument("blocked")
	<-blocked.opened
	other.Cancel()
	if result := waitResult(t, other); !errors.Is(result.Err, ErrCanceled) {
		t.Fatal("canceled read succeeded", result.Err)
	}
}
