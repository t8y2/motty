package workbench

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

func mustSession(t *testing.T, state *State, id uint32) *Session {
	t.Helper()
	session, ok := state.sessionByIDPointer(id)
	if !ok {
		t.Fatalf("session %d is gone", id)
	}
	return session
}

// secondServer opens another host's terminal and file panel in state, with its
// own in-memory file server, and returns the session and that server.
func secondServer(t *testing.T, state *State, world *world) (*Session, *workbenchSFTPRemote) {
	t.Helper()
	state.activeSection = sectionHosts
	for _, host := range state.hosts {
		if host.ID != state.selectedHostID {
			state.selectedHostID = host.ID
			break
		}
	}
	fake := connect(t, state, world)
	fake.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.ServerVersion = "SSH-2.0-Test"
		s.Method = "password"
	})
	state.Wake()
	fake.shell(t).say("\x1b[0m")
	state.Wake()
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("the second host has no session")
	}
	remote := &workbenchSFTPRemote{
		cwd:     "/home/test",
		entries: map[string][]os.FileInfo{"/home/test": nil},
		files:   map[string][]byte{},
	}
	fake.mu.Lock()
	fake.sftp = remote
	fake.mu.Unlock()
	state.openSFTP(session)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	return session, remote
}

func TestSFTPRemoteCopyPasteBetweenServers(t *testing.T) {
	state, world, _, _ := liveSession(t)
	// The first server is the terminal's own file panel.
	firstID := state.sessions[0].ID
	firstRemote := &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test": {workbenchFileInfo{name: "notes.txt", size: 11, mode: 0o644, when: time.Unix(1_700_000_000, 0)}},
		},
		files: map[string][]byte{"/home/test/notes.txt": []byte("hello world")},
	}
	world.conns[0].mu.Lock()
	world.conns[0].sftp = firstRemote
	world.conns[0].mu.Unlock()
	first := mustSession(t, state, firstID)
	state.openSFTP(first)
	waitSFTP(t, state, first, func(view *sftpView) bool { return !view.busy && len(view.rows) == 1 })
	sourceView := first.files

	second, secondRemote := secondServer(t, state, world)
	destView := second.files
	sourceView.selectRow(sftpRowIndex(t, sourceView, "notes.txt"), 0)
	sourceView.local = false
	if copied, err := state.copySFTPSelection(sourceView); !copied || err != nil {
		t.Fatalf("copy = %t, %v", copied, err)
	}
	destView.local = false
	if !state.pasteSFTPClipboard(destView) {
		t.Fatal("paste did not queue a server-to-server copy")
	}
	waitSFTP(t, state, second, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	secondRemote.mu.Lock()
	body := string(secondRemote.files["/home/test/notes.txt"])
	partial := secondRemote.files["/home/test/.motty.notes.txt.part"]
	secondRemote.mu.Unlock()
	if body != "hello world" || partial != nil {
		t.Fatalf("destination file = %q, partial = %q", body, partial)
	}
	snapshot := destView.transfers[0]
	if snapshot.Operation != sftpclient.OperationRemoteCopy {
		t.Fatalf("transfer operation = %v", snapshot.Operation)
	}
	var described bool
	for index := range state.transfers.entries {
		entry := &state.transfers.entries[index]
		if entry.snapshot.Operation != sftpclient.OperationRemoteCopy {
			continue
		}
		if entry.sourceServer == "" || entry.sourceAddress == "" || entry.server == "" {
			t.Fatalf("transfer center entry misses one end: %+v", entry)
		}
		if entry.title != "notes.txt" {
			t.Fatalf("entry title = %q", entry.title)
		}
		described = true
	}
	if !described {
		t.Fatal("the transfer center has no remote-copy entry")
	}
	if queued, rejected := state.queueRemoteCopies(destView, state.sftpRemoteClipboard().copySource(), "/home/test", "/home/test", []sftpclient.Entry{{Name: "..", Mode: 0o644}}); queued != 0 || rejected != 1 {
		t.Fatalf("unsafe entry name: queued %d, rejected %d", queued, rejected)
	}
	if len(sourceView.outgoingCopies) != 0 {
		t.Fatalf("the source lease outlived the copy: %+v", sourceView.outgoingCopies)
	}
}

func TestSFTPRemoteCopyDragBetweenWindows(t *testing.T) {
	state, world, fake, _ := liveSession(t)
	state.sftpOpenMode = sftpOpenWindow
	firstID := state.sessions[0].ID
	firstRemote := &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test": {workbenchFileInfo{name: "notes.txt", size: 11, mode: 0o644, when: time.Unix(1_700_000_000, 0)}},
		},
		files: map[string][]byte{"/home/test/notes.txt": []byte("hello world")},
	}
	fake.mu.Lock()
	fake.sftp = firstRemote
	fake.mu.Unlock()
	// The second host gets its terminal and file server; its window opens below.
	state.activeSection = sectionHosts
	for _, host := range state.hosts {
		if host.ID != state.selectedHostID {
			state.selectedHostID = host.ID
			break
		}
	}
	fakeB := connect(t, state, world)
	fakeB.set(func(s *sshconn.Status) {
		s.Phase = sshconn.PhaseConnected
		s.ServerVersion = "SSH-2.0-Test"
		s.Method = "password"
	})
	state.Wake()
	fakeB.shell(t).say("\x1b[0m")
	state.Wake()
	second, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		t.Fatal("the second host has no session")
	}
	secondRemote := &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test":          {workbenchFileInfo{name: "existing", mode: os.ModeDir | 0o755}},
			"/home/test/existing": nil,
		},
		files: map[string][]byte{},
	}
	fakeB.mu.Lock()
	fakeB.sftp = secondRemote
	fakeB.mu.Unlock()

	host := &sftpNativeHost{scale: 1}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	if err := app.RunApplication(func(application *app.Application) error {
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openSFTPWindow(mustSession(t, state, firstID))
		state.openSFTPWindow(second)
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	firstWindow, secondWindow := state.sftpWindows[firstID], state.sftpWindows[second.ID]
	if firstWindow == nil || secondWindow == nil || firstWindow == secondWindow {
		t.Fatal("both servers need a window")
	}
	firstWindow.frame = func() (gfx.Rect, bool) { return gfx.Rect{X: 0, Y: 0, Width: 1200, Height: 800}, true }
	secondWindow.frame = func() (gfx.Rect, bool) { return gfx.Rect{X: 1200, Y: 0, Width: 1200, Height: 800}, true }
	firstWindow.visible = func() bool { return true }
	secondWindow.visible = func() bool { return true }
	pumpSFTPWindow(t, state, firstWindow, host.windows[0].view, func() bool { return !firstWindow.view.busy && firstWindow.pendingLocal == "" })
	localDir := t.TempDir()
	for _, name := range []string{"project.zip", "notes.txt"} {
		if err := os.WriteFile(filepath.Join(localDir, name), []byte(name), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	secondWindow.navigate(true, localDir)
	pumpSFTPWindow(t, state, secondWindow, host.windows[1].view, func() bool { return !secondWindow.view.busy && secondWindow.pendingLocal == "" })

	dir := firstWindow.folder(false).path
	firstWindow.drag = sftpWindowDrag{
		active: true, fromLocal: false, sourceDir: dir,
		entries: []sftpclient.Entry{{Name: "notes.txt", Size: 11, Mode: 0o644}},
	}
	target := gfx.Point{X: 1200 + secondWindow.ui.list[sideIndex(false)].X + 30, Y: secondWindow.ui.list[sideIndex(false)].Y + 10}
	if got := firstWindow.owner.fileDropWindowAt(target, firstWindow); got != secondWindow {
		t.Fatalf("window at %+v = %p, want %p", target, got, secondWindow)
	}
	if local, _, row, ok := secondWindow.dropTargetAny(target.X-1200, target.Y); !ok || local || row != 0 {
		t.Fatalf("dropTargetAny = local %t row %d ok %t (lists %+v, connected %t, busy %t, nav %t)", local, row, ok, secondWindow.ui.list, secondWindow.connected(), secondWindow.view.busy, secondWindow.navigationBusy())
	}
	firstWindow.trackFileDrag(gfx.PointerEvent{
		Kind: gfx.PointerMove, X: target.X, Y: target.Y, ScreenX: target.X, ScreenY: target.Y,
		Viewport: gfx.Size{Width: 1200, Height: 800},
	})
	if !secondWindow.foreign.active || secondWindow.foreign.local {
		t.Fatalf("the second window did not light its remote pane: %+v", secondWindow.foreign)
	}
	firstWindow.dropFileDrag()
	firstWindow.cancelDrag()
	if secondWindow.foreign.active {
		t.Fatal("the drop highlight survived the release")
	}
	pumpSFTPWindow(t, state, secondWindow, host.windows[1].view, func() bool {
		return len(secondWindow.view.transfers) == 1 && secondWindow.view.transfers[0].State == sftpclient.TransferCompleted
	})
	secondRemote.mu.Lock()
	body := string(secondRemote.files["/home/test/existing/notes.txt"])
	secondRemote.mu.Unlock()
	if body != "hello world" {
		t.Fatalf("destination file = %q", body)
	}
}

func TestSFTPRemoteCopyDropOnFileTab(t *testing.T) {
	state, world, _, _ := liveSession(t)
	state.sftpOpenMode = sftpOpenTab
	firstID := state.sessions[0].ID
	firstRemote := &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test": {workbenchFileInfo{name: "notes.txt", size: 11, mode: 0o644, when: time.Unix(1_700_000_000, 0)}},
		},
		files: map[string][]byte{"/home/test/notes.txt": []byte("hello world")},
	}
	world.conns[0].mu.Lock()
	world.conns[0].sftp = firstRemote
	world.conns[0].mu.Unlock()
	first := mustSession(t, state, firstID)
	state.openSFTP(first)
	waitSFTP(t, state, first, func(view *sftpView) bool { return !view.busy && len(view.rows) == 1 })

	second, secondRemote := secondServer(t, state, world)
	state.openSFTPWindow(first)
	state.openSFTPWindow(second)
	sourceWindow := state.sftpWindows[first.ID]
	targetWindow := state.sftpWindows[second.ID]
	if sourceWindow == nil || targetWindow == nil {
		t.Fatal("both servers need a file tab")
	}
	state.SetWindow(func() (gfx.Rect, bool) { return gfx.Rect{Width: 1400, Height: 900}, true }, func() bool { return true })
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) && (sourceWindow.view.busy || targetWindow.view.busy) {
		state.Wake()
		time.Sleep(time.Millisecond)
	}
	if sourceWindow.view.busy || targetWindow.view.busy {
		t.Fatal("the file tabs did not finish listing")
	}
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
	var tabBox gfx.Rect
	for _, tab := range state.chrome.bar.tabs {
		if tab.fileSession == second.ID {
			tabBox = tab.box.Rect()
			break
		}
	}
	if tabBox.Width <= 0 {
		t.Fatal("the second file tab has no box")
	}
	point := gfx.Point{X: tabBox.X + tabBox.Width/2, Y: tabBox.Y + tabBox.Height/2}
	sourceWindow.drag = sftpWindowDrag{
		active: true, fromLocal: false, sourceDir: sourceWindow.folder(false).path,
		entries: []sftpclient.Entry{{Name: "notes.txt", Size: 11, Mode: 0o644}},
	}
	sourceWindow.trackFileDrag(gfx.PointerEvent{
		Kind: gfx.PointerMove, X: point.X, Y: point.Y, ScreenX: point.X, ScreenY: point.Y,
		Viewport: gfx.Size{Width: 1400, Height: 900},
	})
	if state.foreignFileTab != second.ID {
		t.Fatalf("the tab highlight = %d, want %d", state.foreignFileTab, second.ID)
	}
	sourceWindow.dropFileDrag()
	sourceWindow.cancelDrag()
	if state.foreignFileTab != 0 {
		t.Fatal("the tab highlight survived the release")
	}
	deadline = time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if len(targetWindow.view.transfers) == 1 && targetWindow.view.transfers[0].State == sftpclient.TransferCompleted {
			break
		}
		time.Sleep(time.Millisecond)
	}
	secondRemote.mu.Lock()
	body := string(secondRemote.files["/home/test/notes.txt"])
	secondRemote.mu.Unlock()
	if body != "hello world" {
		t.Fatalf("destination file = %q (transfers %+v)", body, targetWindow.view.transfers)
	}
}

func pumpSFTPWindow(t *testing.T, state *State, window *sftpWindow, view *headless.Session, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		if done() {
			_ = view.Rest()
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("window did not settle: busy=%t status=%s error=%s transfers=%+v", window.view.busy, window.view.status, window.view.errorText, window.view.transfers)
}
