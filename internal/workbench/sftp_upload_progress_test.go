//go:build windows || darwin || linux

package workbench

import (
	"bytes"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	"motty/internal/sftpclient"
)

type progressUploadRemote struct {
	*workbenchSFTPRemote
	gate chan struct{}
}

func (remote *progressUploadRemote) OpenTransferFile(path string, flags int) (sftpclient.TransferFile, error) {
	file, err := remote.workbenchSFTPRemote.OpenTransferFile(path, flags)
	return &progressUploadFile{TransferFile: file, gate: remote.gate}, err
}

type progressUploadFile struct {
	sftpclient.TransferFile
	gate   chan struct{}
	writes int
}

func (file *progressUploadFile) Write(data []byte) (int, error) {
	file.writes++
	if file.writes == 2 {
		<-file.gate
	}
	return file.TransferFile.Write(data)
}

func TestSFTPWindowUploadShowsProgressBeforeCompletion(t *testing.T) {
	state, w, remote, native, directory := dualSFTPFixture(t, 1)
	view := native.windows[0].view
	payload := bytes.Repeat([]byte("upload-progress"), 32768)
	name := "progress.bin"
	if err := os.WriteFile(filepath.Join(directory, name), payload, 0600); err != nil {
		t.Fatal(err)
	}
	w.owner.listLocal(w.view, directory)
	dualWait(t, state, w, view, func() bool { return !w.view.localBusy && len(w.view.lfolder.rows) == 4 })
	gate := make(chan struct{})
	var once sync.Once
	release := func() { once.Do(func() { close(gate) }) }
	defer release()
	w.view.worker.Close()
	backend := &progressUploadRemote{workbenchSFTPRemote: remote, gate: gate}
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return backend, nil }, w.invalidate)
	if err := view.RightClickLabel(name); err != nil {
		t.Fatal(err)
	}
	started := time.Now()
	dualClickMenuLabel(t, view, w.windowMenuRowLabel(sftpWindowMenuActions.At(sftpWindowActionIndex(hitSFTPSend))))
	if time.Since(started) > time.Second {
		t.Fatal("upload menu blocked the UI")
	}
	if w.queueCount() != 1 {
		t.Fatal("upload was not immediately added to the queue")
	}
	dualWait(t, state, w, view, func() bool {
		return len(w.view.transfers) == 1 && w.view.transfers[0].BytesDone > 0
	})
	job := w.view.transfers[0]
	if job.State != sftpclient.TransferRunning || job.BytesDone >= job.BytesTotal {
		t.Fatal("queue skipped intermediate upload progress")
	}
	dualHit(t, view, sftpWindowQueueRows.At(state.transfers.entries[0].id))
	release()
	dualWait(t, state, w, view, func() bool { return w.view.transfers[0].State == sftpclient.TransferCompleted })
	remote.mu.Lock()
	actual := append([]byte(nil), remote.files["/home/test/"+name]...)
	remote.mu.Unlock()
	if !bytes.Equal(actual, payload) {
		t.Fatal("uploaded content differs")
	}
}
