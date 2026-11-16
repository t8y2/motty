package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"reflect"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

type historyGateRemote struct {
	*workbenchSFTPRemote
	mu      sync.Mutex
	gates   []chan struct{}
	started chan int
	reads   int
}

func TestSFTPHistoryManualBranchAndAutoFollow(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	if len(view.history.entries) != 1 || view.history.entries[0].path != view.path {
		t.Fatal("initial folder did not seed history")
	}
	state.navigateSFTPDirectory(view, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if !state.navigateSFTPHistory(view, false, -1) {
		t.Fatal("Back unavailable")
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test" })
	before := append([]string(nil), view.history.recent...)
	state.openSFTPDirectory(view, "/automatic")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/automatic" })
	if !reflect.DeepEqual(before, view.history.recent) || len(view.history.entries) != 2 {
		t.Fatal("auto-follow changed manual history")
	}
	if !state.navigateSFTPHistory(view, false, -1) {
		t.Fatal("Back from automatic folder unavailable")
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test" })
	state.navigateSFTPDirectory(view, "/new-manual")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/new-manual" })
	if len(view.history.entries) != 2 || view.history.entries[1].path != "/new-manual" || state.navigateSFTPHistory(view, false, 1) {
		t.Fatal("manual navigation did not trim forward branch")
	}
}

func TestSFTPHistoryLocalRestoreVisibleNamesAndBounds(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	view.local = true
	root := t.TempDir()
	for _, name := range []string{"a", "b", "c"} {
		if err := os.WriteFile(filepath.Join(root, name), nil, 0600); err != nil {
			t.Fatal(err)
		}
	}
	if err := state.navigateLocalDirectory(view, root); err != nil {
		t.Fatal(err)
	}
	folder := &view.lfolder
	folder.visibleRows = 1
	folder.selectRow(1, 0)
	folder.first = 1
	away := t.TempDir()
	state.navigateLocalDirectory(view, away)
	folder.filterInputEditor().Insert("a")
	state.navigateSFTPHistory(view, true, -1)
	if folder.path != root || len(folder.rows) != 1 || folder.rows[0].selected || folder.selected != -1 || folder.anchor != -1 || folder.first != 0 {
		t.Fatalf("restored invisible selection: %+v", folder)
	}
	folder.clearFilter()
	for i := range 65 {
		dir := filepath.Join(away, "d", fmt.Sprintf("d%03d", i))
		if err := os.MkdirAll(dir, 0700); err != nil {
			t.Fatal(err)
		}
		state.navigateLocalDirectory(view, dir)
	}
	if len(folder.history.entries) != 50 || len(folder.history.recent) != 10 {
		t.Fatalf("unbounded history %d recent %d", len(folder.history.entries), len(folder.history.recent))
	}
}

func (r *historyGateRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	if directory != "/home/test/docs" {
		return r.workbenchSFTPRemote.ReadDir(directory)
	}
	r.mu.Lock()
	i := r.reads
	r.reads++
	r.mu.Unlock()
	r.started <- i
	<-r.gates[i]
	return r.workbenchSFTPRemote.ReadDir(directory)
}

func TestSFTPHistoryCanceledSamePathDoesNotAcceptOlderResult(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	<-view.worker.Done()
	gated := &historyGateRemote{workbenchSFTPRemote: remote, started: make(chan int, 3)}
	for range 3 {
		gated.gates = append(gated.gates, make(chan struct{}))
	}
	var closed [3]bool
	release := func(i int) {
		if !closed[i] {
			close(gated.gates[i])
			closed[i] = true
		}
	}
	t.Cleanup(func() {
		for i := range 3 {
			release(i)
		}
	})
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return gated, nil }, nil)
	submit := func() {
		view.pathEditing = true
		*view.pathInput() = *text.NewEditor("/home/test/docs")
		state.submitSFTPPath(view)
	}
	cancel := func() { state.sftpPathKey(gfx.KeyEvent{Target: hitSFTPPath, Key: gfx.KeyEscape}) }
	view.pathEditing = true
	submit()
	<-gated.started
	cancel()
	submit()
	cancel()
	submit()
	release(0)
	<-gated.started
	state.pumpSFTP(session)
	release(1)
	<-gated.started
	deadline := time.Now().Add(time.Second)
	for time.Now().Before(deadline) {
		state.pumpSFTP(session)
		if view.path != "/home/test" {
			t.Fatalf("canceled same-path result accepted as newest request: %q", view.path)
		}
		if len(view.worker.Results()) == 0 {
			break
		}
	}
	release(2)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
}

func historyUntil(t *testing.T, condition func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		if condition() {
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatal("history operation did not reach expected stage")
}

type historyLinkRemote struct {
	*workbenchSFTPRemote
	mu      sync.Mutex
	calls   int
	started chan int
	gates   [2]chan struct{}
}

func (r *historyLinkRemote) ReadLink(name string) (string, error) {
	r.mu.Lock()
	i := r.calls
	r.calls++
	r.mu.Unlock()
	r.started <- i
	<-r.gates[i]
	return r.workbenchSFTPRemote.ReadLink(name)
}

func TestSFTPHistoryOldFollowUpCannotReplaceNewPreparation(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	view.worker.Close()
	<-view.worker.Done()
	r := &historyLinkRemote{workbenchSFTPRemote: remote, started: make(chan int, 2), gates: [2]chan struct{}{make(chan struct{}), make(chan struct{})}}
	var once [2]sync.Once
	release := func(i int) { once[i].Do(func() { close(r.gates[i]) }) }
	t.Cleanup(func() { release(0); release(1) })
	view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return r, nil }, nil)
	infos := []os.FileInfo{workbenchFileInfo{name: "old", mode: 0600}, workbenchFileInfo{name: "link", mode: os.ModeSymlink | 0777}}
	for i := 0; i < 513; i++ {
		infos = append(infos, workbenchFileInfo{name: fmt.Sprintf("row-%03d", i), mode: 0600})
	}
	remote.mu.Lock()
	remote.entries["/same"] = infos
	remote.links = map[string]string{"/same/link": "old"}
	remote.mu.Unlock()
	state.navigateSFTPDirectory(view, "/same")
	<-r.started
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/same" })
	remote.mu.Lock()
	newer := append([]os.FileInfo(nil), infos...)
	newer[0] = workbenchFileInfo{name: "new", mode: 0600}
	remote.entries["/same"] = newer
	remote.mu.Unlock()
	state.listSFTP(view, "/same")
	<-r.started
	historyUntil(t, func() bool { state.pumpSFTP(session); return view.preparingPath == "/same" })
	release(0)
	historyUntil(t, func() bool { return len(view.worker.Results()) > 0 })
	state.pumpSFTP(session)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	if sftpRowNameIndex(view.rows, "new") < 0 || sftpRowNameIndex(view.rows, "old") >= 0 {
		t.Fatal("old same-path link follow-up replaced newer prepared rows")
	}
}

func TestSFTPHistoryCacheRefreshFailureAndCanceledPreparation(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	v := session.files
	state.navigateSFTPDirectory(v, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	state.navigateSFTPHistory(v, false, -1)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	state.navigateSFTPDirectory(v, "/home/test/docs") // accepted cached display, then passive revalidation
	if v.busy || len(v.history.entries) != 2 {
		t.Fatal("cache did not accept exactly once")
	}
	waitSFTP(t, state, session, func(v *sftpView) bool { return len(v.prefetching) == 0 })
	before := append([]string(nil), v.history.recent...)
	state.listSFTP(v, v.path)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	if len(v.history.entries) != 2 || !reflect.DeepEqual(before, v.history.recent) {
		t.Fatal("refresh changed history")
	}
	remote.mu.Lock()
	for i := 0; i < 600; i++ {
		remote.entries["/large"] = append(remote.entries["/large"], workbenchFileInfo{name: fmt.Sprintf("row-%03d", i), mode: 0600})
	}
	remote.mu.Unlock()
	state.navigateSFTPDirectory(v, "/large")
	historyUntil(t, func() bool { state.pumpSFTP(session); return v.preparingPath == "/large" })
	state.cancelSFTPNavigation(v)
	state.Wake()
	if v.path != "/home/test/docs" || v.busy || len(v.history.entries) != 2 || !reflect.DeepEqual(before, v.history.recent) {
		t.Fatal("canceled row preparation committed history")
	}
	state.navigateSFTPDirectory(v, "/home/test")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	index := v.history.cursor
	local := t.TempDir()
	state.navigateLocalDirectory(v, local)
	localBefore := append([]string(nil), v.lfolder.history.recent...)
	if state.navigateLocalDirectory(v, filepath.Join(local, "missing")) == nil {
		t.Fatal("missing local folder succeeded")
	}
	if v.history.cursor != index || !reflect.DeepEqual(localBefore, v.lfolder.history.recent) {
		t.Fatal("failure or other side moved cursor/MRU")
	}
}

func TestSFTPHistoryLocalPathEscapeCancelsPendingListing(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	v := session.files
	v.local = true
	state.sftpPathInput = true
	root := t.TempDir()
	state.navigateLocalDirectory(v, root)
	before := append([]string(nil), v.lfolder.history.recent...)
	large := localManyFiles(t, localListingInlineEntries+1)
	*v.lfolder.pathInput() = *text.NewEditor(large)
	state.submitSFTPPath(v)
	if v.lfolder.navigation.id == 0 {
		t.Fatal("local listing was not pending")
	}
	state.sftpPathKey(gfx.KeyEvent{Target: hitSFTPPath, Key: gfx.KeyEscape})
	historyUntil(t, func() bool {
		state.ai.inbox.mu.Lock()
		defer state.ai.inbox.mu.Unlock()
		return len(state.ai.inbox.queue) > 0
	})
	state.Wake()
	if v.lfolder.path != root || !reflect.DeepEqual(before, v.lfolder.history.recent) {
		t.Fatal("Escape accepted a pending local path into history")
	}
}

func BenchmarkSFTPHistoryMore(b *testing.B) {
	state, session, _, _ := openTestSFTP(b)
	for i := 0; i < 10; i++ {
		session.files.history.touch(fmt.Sprintf("/history/path-%d", i))
	}
	session.files.headerMenuOpen = true
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

type historyHeldReader struct {
	sftpclient.TransferFile
	gate    <-chan struct{}
	started chan<- struct{}
	once    sync.Once
}

func (r *historyHeldReader) Read(p []byte) (int, error) {
	r.once.Do(func() { close(r.started); <-r.gate })
	return r.TransferFile.Read(p)
}

type historyTransferRemote struct {
	*historyGateRemote
	gate    <-chan struct{}
	started chan<- struct{}
}

func (r *historyTransferRemote) OpenTransferFile(name string, flags int) (sftpclient.TransferFile, error) {
	reader, err := r.workbenchSFTPRemote.OpenTransferFile(name, flags)
	if err == nil && name == "/home/test/notes.txt" {
		return &historyHeldReader{TransferFile: reader, gate: r.gate, started: r.started}, nil
	}
	return reader, err
}

func TestSFTPHistoryCancelJoinedPrefetchPreservesUnrelatedTransfer(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	v := session.files
	v.worker.Close()
	<-v.worker.Done()
	gated := &historyGateRemote{workbenchSFTPRemote: remote, started: make(chan int, 2), gates: []chan struct{}{make(chan struct{}), make(chan struct{})}}
	transferGate, transferStarted := make(chan struct{}), make(chan struct{})
	r := &historyTransferRemote{historyGateRemote: gated, gate: transferGate, started: transferStarted}
	var once [3]sync.Once
	release := func(i int) {
		once[i].Do(func() {
			if i == 2 {
				close(transferGate)
			} else {
				close(gated.gates[i])
			}
		})
	}
	t.Cleanup(func() {
		for i := range 3 {
			release(i)
		}
	})
	v.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return r, nil }, nil)
	id, ok := v.worker.QueueDownload("/home/test/notes.txt", filepath.Join(t.TempDir(), "notes"), false, sftpclient.ExistingFail)
	if !ok {
		t.Fatal("transfer rejected")
	}
	select {
	case <-transferStarted:
	case <-time.After(3 * time.Second):
		t.Fatalf("transfer did not enter read: %+v", v.worker.Transfers(nil))
	}
	state.prefetchSFTP(v, "/home/test/docs")
	<-gated.started
	state.navigateSFTPDirectory(v, "/home/test/docs")
	if v.waiting == "" {
		t.Fatal("navigation did not join prefetch")
	}
	state.cancelSFTPNavigation(v)
	state.navigateSFTPDirectory(v, "/home/test/docs")
	release(0)
	<-gated.started
	state.pumpSFTP(session)
	if v.path != "/home/test" || len(v.history.entries) != 1 {
		t.Fatal("canceled prefetch consumed newer history intent")
	}
	for _, snapshot := range v.worker.Transfers(nil) {
		if snapshot.ID == id && snapshot.State == sftpclient.TransferCanceled {
			t.Fatal("navigation cancellation canceled unrelated transfer")
		}
	}
	release(1)
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy && v.path == "/home/test/docs" })
	if len(v.history.entries) != 2 {
		t.Fatal("fresh accepted navigation did not commit once")
	}
	release(2)
	waitTransfersDone(t, state, session, 1)
}

func TestSFTPHistoryDisconnectedRejectsNavigation(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	v := session.files
	state.navigateSFTPDirectory(v, "/home/test/docs")
	waitSFTP(t, state, session, func(v *sftpView) bool { return !v.busy })
	session.phase = sshconn.PhaseClosed
	if state.navigateSFTPHistory(v, false, -1) || state.navigateSFTPRecent(v, false, 1) {
		t.Fatal("disconnected sidebar accepted history navigation")
	}
}
