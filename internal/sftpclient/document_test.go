package sftpclient

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"os"
	"path/filepath"
	"testing"
)

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
