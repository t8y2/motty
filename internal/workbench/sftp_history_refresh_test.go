package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"
	"time"

	"motty/internal/sftpclient"
)

func TestSFTPHistorySamePathRefreshPreservesWindowScroll(t *testing.T) {
	for _, mode := range []string{"local", "remote", "download"} {
		t.Run(mode, func(t *testing.T) {
			state, window, remote, host, directory := dualSFTPFixture(t, 1)
			preview := host.windows[0].view
			for index := range 80 {
				name := filepath.Join(directory, fmt.Sprintf("row-%03d", index))
				if err := os.WriteFile(name, nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			remote.mu.Lock()
			for index := range 80 {
				remote.entries[window.view.path] = append(remote.entries[window.view.path], workbenchFileInfo{
					name: fmt.Sprintf("remote-%03d", index), mode: 0600,
				})
			}
			remote.mu.Unlock()
			window.refresh(true)
			window.refresh(false)
			dualWait(t, state, window, preview, func() bool {
				return !window.view.busy && window.pendingLocal == "" && len(window.view.rows) > 70
			})
			side := 0
			if mode == "remote" {
				side = 1
			}
			want := 20*sftpRowHeight + 0.5
			window.ui.offset[side] = want
			window.folder(side == 0).first = 20
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			if window.ui.offset[side] != want {
				t.Fatal("fixture did not reach the fractional scroll position")
			}
			destination := filepath.Join(directory, "downloaded-notes.txt")
			switch mode {
			case "local":
				window.refresh(true)
			case "remote":
				window.refresh(false)
			case "download":
				if _, ok := window.view.worker.QueueDownload("/home/test/notes.txt", destination, false, sftpclient.ExistingFail); !ok {
					t.Fatal("download rejected")
				}
				window.localRefresh = true
			}
			dualWait(t, state, window, preview, func() bool {
				return !window.view.busy && window.pendingLocal == "" && !window.localRefresh && !sftpTransfersActive(window.view.transfers)
			})
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			if mode == "download" {
				if data, err := os.ReadFile(destination); err != nil || string(data) != "hello world" {
					t.Fatalf("download did not complete: data=%q err=%v", data, err)
				}
			}
			if got := window.ui.offset[side]; got != want {
				t.Fatalf("same-path %s refresh moved scroll from %.1f to %.1f", mode, want, got)
			}
		})
	}
}

func TestSFTPHistoryNavigationCancelCompletesFeedback(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	<-view.worker.Done()
	gated := &historyGateRemote{workbenchSFTPRemote: remote, started: make(chan int, 1), gates: []chan struct{}{make(chan struct{})}}
	released := false
	t.Cleanup(func() {
		if !released {
			close(gated.gates[0])
		}
	})
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
	originalPath := view.path
	state.navigateSFTPDirectory(view, "/home/test/docs")
	select {
	case <-gated.started:
	case <-time.After(4 * time.Second):
		t.Fatal("directory read did not start")
	}
	state.cancelSFTPOperation(view)
	if view.busy || view.status != state.messages.SFTPCanceled {
		t.Fatalf("retired navigation did not finish cancel feedback: busy=%v status=%q", view.busy, view.status)
	}
	close(gated.gates[0])
	released = true
	historyUntil(t, func() bool { return len(view.worker.Results()) > 0 })
	state.pumpSFTP(session)
	if view.busy || view.path != originalPath || view.status != state.messages.SFTPCanceled {
		t.Fatalf("stale result changed canceled navigation: path=%q status=%q busy=%v", view.path, view.status, view.busy)
	}
}

func TestSFTPHistoryExtractionCancelKeepsAsynchronousFeedback(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.busy, view.extracting, view.operation = true, true, sftpclient.OperationList
	canceled := false
	view.cancelExtract = func() { canceled = true }
	state.cancelSFTPOperation(view)
	if !canceled || !view.busy || view.status != state.messages.SFTPCanceling {
		t.Fatalf("extraction cancellation lost its asynchronous state: canceled=%v busy=%v status=%q", canceled, view.busy, view.status)
	}
}
