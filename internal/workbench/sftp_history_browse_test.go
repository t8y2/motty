package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestSFTPHistoryPairedBrowseKeepsBothTimelinesAndBack(t *testing.T) {
	state, window, _, host, root := dualSFTPFixture(t, 1)
	preview := host.windows[0].view
	child := filepath.Join(root, "docs")
	if err := os.Mkdir(child, 0700); err != nil {
		t.Fatal(err)
	}
	window.toggleBrowseSync()
	window.navigate(false, "/home/test/docs")
	dualWait(t, state, window, preview, func() bool {
		return !window.navigationBusy() && window.view.lfolder.path == child
	})
	for side, folder := range []*sftpFolder{&window.view.lfolder, &window.view.sftpFolder} {
		if len(folder.history.entries) < 2 || folder.history.entries[folder.history.cursor].path != folder.path {
			t.Fatalf("paired browse did not record side %d: path=%q history=%+v", side, folder.path, folder.history)
		}
	}
	if !state.navigateSFTPHistory(window.view, false, -1) {
		t.Fatal("paired Back unavailable")
	}
	dualWait(t, state, window, preview, func() bool {
		return !window.navigationBusy() && window.view.path == "/home/test" && window.view.lfolder.path == root
	})
	if !window.browse.enabled || len(window.view.worker.Transfers(nil)) != 0 {
		t.Fatal("history bypassed paired navigation or changed transfers")
	}
}

func TestSFTPHistoryPairedRemoteEscapeRetiresRequest(t *testing.T) {
	for _, mode := range []string{"source", "counterpart", "profile"} {
		t.Run(mode, func(t *testing.T) {
			state, window, remote, host, root := dualSFTPFixture(t, 1)
			child := filepath.Join(root, "docs")
			if err := os.Mkdir(child, 0700); err != nil {
				t.Fatal(err)
			}
			window.view.worker.Close()
			<-window.view.worker.Done()
			gate := make(chan struct{})
			gated := &historyGateRemote{workbenchSFTPRemote: remote, started: make(chan int, 1), gates: []chan struct{}{gate}}
			released := false
			t.Cleanup(func() {
				if !released {
					close(gate)
				}
			})
			window.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
			before := window.view.history
			if mode == "profile" {
				window.openPairedBookmark(sftpBookmark{local: child, path: "/home/test/docs"})
			} else {
				window.toggleBrowseSync()
				if mode == "counterpart" {
					window.navigate(true, child)
				} else {
					window.navigate(false, "/home/test/docs")
				}
			}
			select {
			case <-gated.started:
			case <-time.After(4 * time.Second):
				t.Fatal("remote directory read did not start")
			}
			if window.browse.pending == nil {
				t.Fatal("fixture did not start a paired request")
			}
			preview := host.windows[0].view
			if err := preview.Rest(); err != nil {
				t.Fatal(err)
			}
			dualClickID(t, preview, sftpWindowPath[1])
			if err := preview.Driver().Key(gfx.KeyEscape, 0); err != nil {
				t.Fatal(err)
			}
			close(gate)
			released = true
			historyUntil(t, func() bool { return len(window.view.worker.Results()) > 0 })
			state.pumpSFTP(&window.session)
			if window.browse.pending != nil || window.navigationBusy() || window.browse.enabled {
				t.Fatalf("Escape left %s request locked or synchronization unconfirmed: pending=%+v busy=%v", mode, window.browse.pending, window.navigationBusy())
			}
			if window.view.path != "/home/test" || !reflect.DeepEqual(before, window.view.history) {
				t.Fatal("canceled remote path or late reply changed history")
			}
			window.openPairedBookmark(sftpBookmark{local: root, path: "/home/test"})
			dualWait(t, state, window, preview, func() bool {
				state.pumpSFTP(&window.session)
				return !window.navigationBusy()
			})
			if window.view.path != "/home/test" || window.view.lfolder.path != root {
				t.Fatal("canceled pair blocked the next profile navigation")
			}
		})
	}
}

func TestSFTPHistoryPairedAsyncLocalEscapeRetiresRequest(t *testing.T) {
	for _, counterpart := range []bool{false, true} {
		t.Run(fmt.Sprintf("counterpart-%t", counterpart), func(t *testing.T) {
			state, window, _, host, root := dualSFTPFixture(t, 1)
			child := filepath.Join(root, "docs")
			if err := os.Mkdir(child, 0700); err != nil {
				t.Fatal(err)
			}
			for index := range 514 {
				if err := os.WriteFile(filepath.Join(child, fmt.Sprintf("row-%04d", index)), nil, 0600); err != nil {
					t.Fatal(err)
				}
			}
			before := window.view.lfolder.history
			window.toggleBrowseSync()
			if counterpart {
				window.navigate(false, "/home/test/docs")
				historyUntil(t, func() bool {
					state.pumpSFTP(&window.session)
					return window.pendingLocal == child
				})
			} else {
				window.navigate(true, child)
			}
			if window.pendingLocal != child || window.browse.pending == nil {
				t.Fatal("fixture did not start an asynchronous local paired request")
			}
			window.key(gfx.KeyEvent{Target: sftpWindowPath[0], Key: gfx.KeyEscape})
			historyUntil(t, func() bool {
				state.ai.inbox.mu.Lock()
				defer state.ai.inbox.mu.Unlock()
				return len(state.ai.inbox.queue) > 0
			})
			state.drainAI()
			if window.browse.pending != nil || window.navigationBusy() || window.browse.enabled {
				t.Fatalf("local Escape left paired request locked: pending=%+v busy=%v", window.browse.pending, window.navigationBusy())
			}
			if window.view.lfolder.path != root || !reflect.DeepEqual(before, window.view.lfolder.history) {
				t.Fatal("canceled local publication changed history")
			}
			window.navigate(true, filepath.Join(root, "tree"))
			dualWait(t, state, window, host.windows[0].view, func() bool { return !window.navigationBusy() })
			if window.view.lfolder.path != filepath.Join(root, "tree") {
				t.Fatal("canceled pair blocked the next local navigation")
			}
		})
	}
}
