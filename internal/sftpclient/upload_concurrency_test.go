package sftpclient

import (
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"
)

// concurrentOpenRemote counts how many of a directory's files are open at
// once. A small file's round trips are what a folder of them costs, so the
// count is the property that keeps the transfer from being paced by one file.
type concurrentOpenRemote struct {
	remote
	hold time.Duration

	mu   sync.Mutex
	open int
	max  int
}

func (remote *concurrentOpenRemote) OpenTransferFile(name string, flags int) (TransferFile, error) {
	file, err := remote.remote.OpenTransferFile(name, flags)
	if err != nil {
		return nil, err
	}
	remote.mu.Lock()
	remote.open++
	if remote.open > remote.max {
		remote.max = remote.open
	}
	remote.mu.Unlock()
	time.Sleep(remote.hold)
	return &countedTransferFile{TransferFile: file, release: remote.release}, nil
}

func (remote *concurrentOpenRemote) release() {
	remote.mu.Lock()
	remote.open--
	remote.mu.Unlock()
}

func (remote *concurrentOpenRemote) maximum() int {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	return remote.max
}

type countedTransferFile struct {
	TransferFile
	once    sync.Once
	release func()
}

func (file *countedTransferFile) Close() error {
	file.once.Do(file.release)
	return file.TransferFile.Close()
}

func TestDirectoryUploadKeepsSeveralFilesInFlight(t *testing.T) {
	measure := &concurrentOpenRemote{hold: 5 * time.Millisecond}
	worker, root := newProtocolWorkerOn(t, nil, func(client remote) Remote {
		measure.remote = client
		return measure
	})
	local := filepath.Join(t.TempDir(), "many")
	if err := os.MkdirAll(local, 0o755); err != nil {
		t.Fatal(err)
	}
	const files = 24
	payload := []byte("payload")
	for index := 0; index < files; index++ {
		if err := os.WriteFile(filepath.Join(local, fmt.Sprintf("file-%02d.txt", index)), payload, 0o600); err != nil {
			t.Fatal(err)
		}
	}

	id, queued := worker.QueueUpload(local, "many", true, ExistingFail)
	result := queueResult(t, id, queued, worker)
	if result.Err != nil || result.Files != files || result.Bytes != int64(files*len(payload)) {
		t.Fatalf("upload result = %+v", result)
	}
	if got := measure.maximum(); got < 2 {
		t.Fatalf("the directory upload kept %d file in flight; its per-file round trips were serialized", got)
	}
	for _, index := range []int{0, files / 2, files - 1} {
		name := filepath.Join(root, "many", fmt.Sprintf("file-%02d.txt", index))
		if got, err := os.ReadFile(name); err != nil || string(got) != string(payload) {
			t.Fatalf("%s = %q, %v", name, got, err)
		}
	}
}
