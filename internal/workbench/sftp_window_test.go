//go:build windows || darwin || linux

package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"os"
	"path/filepath"
	"runtime"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/headless"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/sftpclient"
)

type sftpNativeHost struct {
	windows []*sftpNativeWindow
	scale   float64
}
type sftpNativeWindow struct {
	view   *headless.Session
	closed func()
	done   bool
	shows  int
}

func (h *sftpNativeHost) OpenWindow(config app.Config, canvas *ui.Canvas, closed func()) (app.WindowControl, error) {
	view, err := headless.Open(config, canvas, headless.Options{Scale: h.scale})
	if err != nil {
		return nil, err
	}
	w := &sftpNativeWindow{view: view, closed: closed}
	h.windows = append(h.windows, w)
	return w, nil
}
func (h *sftpNativeHost) AddStatusItem(app.StatusItemConfig) (app.StatusItemControl, error) {
	return nil, errors.New("not used")
}
func (h *sftpNativeHost) Quit() {
	for _, w := range h.windows {
		w.Close()
	}
}
func (w *sftpNativeWindow) Close() {
	if !w.done {
		w.done = true
		w.view.Close()
		w.closed()
	}
}
func (w *sftpNativeWindow) Hide()                  {}
func (w *sftpNativeWindow) Show()                  { w.shows++ }
func (w *sftpNativeWindow) Frame() (ui.Rect, bool) { return ui.Rect{Width: 1200, Height: 800}, true }

func dualSFTPFixture(t *testing.T, scale float64) (*State, *sftpWindow, *workbenchSFTPRemote, *sftpNativeHost, string) {
	t.Helper()
	state, source, _, remote := openTestSFTP(t)
	host := &sftpNativeHost{scale: scale}
	app.UseApplicationRunner(func(start func(app.Host) error) error { return start(host) })
	t.Cleanup(func() { host.Quit(); app.UseApplicationRunner(nil) })
	var w *sftpWindow
	if err := app.RunApplication(func(application *app.Application) error {
		state.options.OpenContentWindow = application.OpenContentWindow
		state.openSFTPWindow(source)
		w = state.sftpWindows[source.ID]
		return nil
	}); err != nil {
		t.Fatal(err)
	}
	if w == nil {
		t.Fatal("no native transfer window")
	}
	localDir := t.TempDir()
	if err := os.WriteFile(filepath.Join(localDir, "中文上传.yml"), []byte("name: 中文\n"), 0600); err != nil {
		t.Fatal(err)
	}
	os.Mkdir(filepath.Join(localDir, "destination"), 0700)
	os.Mkdir(filepath.Join(localDir, "tree"), 0700)
	os.WriteFile(filepath.Join(localDir, "tree", "leaf.txt"), []byte("nested file"), 0600)
	w.navigate(true, localDir)
	dualWait(t, state, w, host.windows[0].view, func() bool { return !w.view.busy && len(w.view.rows) == 2 && w.view.lfolder.path == localDir })
	return state, w, remote, host, localDir
}

func dualWait(t *testing.T, state *State, w *sftpWindow, view *headless.Session, done func() bool) {
	t.Helper()
	deadline := time.Now().Add(4 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if err := view.Driver().Pump(); err != nil {
			t.Fatal(err)
		}
		if done() {
			view.Rest()
			return
		}
		time.Sleep(time.Millisecond)
	}
	t.Fatalf("dual pane did not settle: busy=%t status=%s error=%s", w.view.busy, w.view.status, w.view.errorText)
}

func dualRow(t *testing.T, w *sftpWindow, local bool, name string) ui.ID {
	t.Helper()
	for i, row := range w.folder(local).rows {
		if row.entry.Name == name {
			return sftpWindowRows[sideIndex(local)].At(i)
		}
	}
	t.Fatal("missing row", name)
	return ui.ID{}
}
func dualHit(t *testing.T, view *headless.Session, id ui.ID) ui.Rect {
	t.Helper()
	for _, h := range view.Snapshot().Hits {
		if h.ID == id && !h.Block {
			return h.Bounds
		}
	}
	t.Fatal("missing hit", id)
	return ui.Rect{}
}
func dualDrag(t *testing.T, view *headless.Session, source, target ui.Rect) {
	t.Helper()
	x, y := source.X+30, source.Y+source.Height/2
	for _, step := range []func() error{func() error { return view.Press(x, y) }, func() error { return view.Move(x+10, y+2) }, func() error { return view.Move(target.X+30, target.Y+target.Height/2) }, func() error { return view.Release(target.X+30, target.Y+target.Height/2) }} {
		if err := step(); err != nil {
			t.Fatal(err)
		}
	}
}

func TestSFTPWindowNativeDragTransfersBothWaysAndKeepsConnection(t *testing.T) {
	state, w, remote, host, localDir := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	sourceID := w.session.ID
	state.activeSessionID = 0 // returning home must not retarget the independent window
	dualDrag(t, view, dualHit(t, view, dualRow(t, w, true, "中文上传.yml")), dualHit(t, view, dualRow(t, w, false, "docs")))
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 1 && !sftpTransfersActive(w.view.transfers) })
	remote.mu.Lock()
	uploaded := bytes.Clone(remote.files["/home/test/docs/中文上传.yml"])
	remote.mu.Unlock()
	if string(uploaded) != "name: 中文\n" {
		t.Fatalf("upload missed the folder target: %q %+v", uploaded, w.view.transfers)
	}
	dualDrag(t, view, dualHit(t, view, dualRow(t, w, false, "notes.txt")), dualHit(t, view, dualRow(t, w, true, "destination")))
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 2 && !sftpTransfersActive(w.view.transfers) })
	actual, err := os.ReadFile(filepath.Join(localDir, "destination", "notes.txt"))
	if err != nil || string(actual) != "hello world" {
		t.Fatalf("download missed target: %q %v", actual, err)
	}
	if w.session.ID != sourceID || w.view.path != "/home/test" {
		t.Fatal("drag changed locked source or navigated the drop folder")
	}
	if err := view.ClickLabel("tree"); err != nil {
		t.Fatal(err)
	}
	w.queueSelection(true)
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 3 && !sftpTransfersActive(w.view.transfers) })
	remote.mu.Lock()
	nested := string(remote.files["/home/test/tree/leaf.txt"])
	remote.mu.Unlock()
	if nested != "nested file" {
		t.Fatal("recursive directory upload failed", w.view.errorText)
	}
	dualWait(t, state, w, view, func() bool { return !w.view.busy })
	remoteFolder := dualHit(t, view, dualRow(t, w, false, "docs"))
	view.Click(remoteFolder.X+25, remoteFolder.Y+10)
	w.queueSelection(false)
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 4 && !sftpTransfersActive(w.view.transfers) })
	guide, err := os.ReadFile(filepath.Join(localDir, "docs", "guide.md"))
	if err != nil || string(guide) != "guide\n" {
		t.Fatal("recursive directory download failed", err)
	}
	if dir := os.Getenv("MOTTY_SFTP_WINDOW_SCREENSHOT_DIR"); dir != "" {
		os.MkdirAll(dir, 0700)
		view.Rest()
		view.Screenshot(filepath.Join(dir, "dual-transfer.png"))
	}
}

func TestSFTPWindowEscapeAndSameSideDropDoNotTransfer(t *testing.T) {
	state, w, _, host, _ := dualSFTPFixture(t, 1.5)
	view := host.windows[0].view
	file := dualHit(t, view, dualRow(t, w, true, "中文上传.yml"))
	dualDrag(t, view, file, w.ui.list[0])
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("same-side drop queued a transfer")
	}
	view.Press(file.X+20, file.Y+10)
	view.Move(w.ui.list[1].X+20, w.ui.list[1].Y+100)
	view.Keys("escape")
	view.Release(w.ui.list[1].X+20, w.ui.list[1].Y+100)
	if len(w.view.worker.Transfers(nil)) != 0 || w.drag.active {
		t.Fatal("Escape failed to cancel drag")
	}
	state.activeSessionID = w.session.ID
	source, _ := state.sessionByIDPointer(w.session.ID)
	state.openSFTPWindow(source)
	if len(host.windows) != 1 || host.windows[0].shows != 1 {
		t.Fatal("repeated entry created another window")
	}
}

func TestSFTPWindowConflictNeedsConfirmation(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	file := filepath.Join(dir, "notes.txt")
	os.WriteFile(file, []byte("new content"), 0600)
	w.queueEntries(true, []sftpclient.Entry{{Name: "notes.txt"}}, dir, w.view.path)
	dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogOverwrite })
	remote.mu.Lock()
	before := string(remote.files["/home/test/notes.txt"])
	remote.mu.Unlock()
	if before != "hello world" {
		t.Fatal("conflict overwrote without confirmation")
	}
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 1 && !sftpTransfersActive(w.view.transfers) })
	remote.mu.Lock()
	after := string(remote.files["/home/test/notes.txt"])
	remote.mu.Unlock()
	if after != "new content" {
		t.Fatal("confirmed replacement failed", after, w.view.errorText)
	}
}

type stalledSFTPRemote struct {
	*workbenchSFTPRemote
	opened, release     chan struct{}
	openOnce, closeOnce sync.Once
}

func (r *stalledSFTPRemote) OpenTransferFile(name string, flags int) (sftpclient.TransferFile, error) {
	if flags&(os.O_WRONLY|os.O_RDWR) != 0 {
		r.openOnce.Do(func() { close(r.opened) })
		<-r.release
		return nil, os.ErrClosed
	}
	return r.workbenchSFTPRemote.OpenTransferFile(name, flags)
}
func (r *stalledSFTPRemote) Close() error { r.closeOnce.Do(func() { close(r.release) }); return nil }

func TestSFTPWindowCloseConfirmsAndDoesNotCloseSourceConnection(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1.5)
	view := host.windows[0].view
	w.view.worker.Close()
	stalled := &stalledSFTPRemote{workbenchSFTPRemote: remote, opened: make(chan struct{}), release: make(chan struct{})}
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return stalled, nil }, w.window.Invalidate)
	w.queueEntries(true, []sftpclient.Entry{{Name: "中文上传.yml"}}, dir, w.view.path)
	select {
	case <-stalled.opened:
	case <-time.After(3 * time.Second):
		t.Fatal("upload did not start")
	}
	w.window.RequestClose()
	view.Rest()
	if !w.closeConfirm || w.window.Closed() {
		t.Fatal("active transfers were closed without confirmation")
	}
	w.click(hitSFTPWindowCloseCancel)
	if w.closeConfirm || w.closed {
		t.Fatal("cancel closed the transfer window")
	}
	quit := false
	if !state.ConfirmDocumentClose(func() { quit = true }) || quit {
		t.Fatal("application quit bypassed active transfer confirmation")
	}
	w.click(hitSFTPWindowCloseConfirm)
	select {
	case <-w.view.worker.Done():
	case <-time.After(3 * time.Second):
		t.Fatal("window worker leaked after close")
	}
	if !w.closed || !quit || len(state.sftpWindows) != 0 {
		t.Fatal("close chain did not finish")
	}
	source, _ := state.sessionByIDPointer(w.session.ID)
	conn := source.conn.(*fakeConn)
	conn.mu.Lock()
	canceled := conn.canceled
	conn.mu.Unlock()
	if canceled != 0 || source.files == nil || source.files.worker == nil {
		t.Fatal("closing SFTP canceled the shared source connection")
	}
}

func TestSFTPWindowLayoutAndEntryAtDisplayScales(t *testing.T) {
	for _, scale := range []float64{1, 1.25, 1.5, 1.75, 2} {
		t.Run(fmt.Sprintf("%.2f", scale), func(t *testing.T) {
			state, w, _, host, _ := dualSFTPFixture(t, scale)
			state.setLanguage(i18n.Chinese)
			view := host.windows[0].view
			for _, light := range []bool{false, true} {
				if light {
					state.setAppearance(appearanceLight)
				}
				for _, size := range []ui.Size{{Width: 640, Height: 360}, {Width: 820, Height: 500}, {Width: 1200, Height: 800}} {
					if err := view.Resize(int(size.Width), int(size.Height)); err != nil {
						t.Fatal(err)
					}
					view.Rest()
					for _, bounds := range w.ui.list {
						if bounds.Width < 150 || bounds.Height < 60 || bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.Width > size.Width || bounds.Y+bounds.Height > size.Height {
							t.Fatal("pane escaped viewport", scale, size, bounds)
						}
					}
					if w.ui.list[0].X+w.ui.list[0].Width > w.ui.list[1].X {
						t.Fatal("file lists overlap")
					}
				}
			}
			var canvas ui.Canvas
			state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
			var entry, close ui.Rect
			for _, hit := range canvas.Hits() {
				local, ok := tree.Local(contextMount, hit.ID)
				if !ok {
					continue
				}
				if local == hitSFTPDetach {
					entry = hit.Bounds
				}
				if local == hitSFTPClose {
					close = hit.Bounds
				}
			}
			if entry.Width == 0 || close.Width == 0 || entry.Y != close.Y || entry.X+entry.Width > close.X {
				t.Fatal("independent-window entry is not left of the close button", entry, close)
			}
			if dir := os.Getenv("MOTTY_SFTP_WINDOW_SCREENSHOT_DIR"); dir != "" {
				os.MkdirAll(dir, 0700)
				view.Screenshot(filepath.Join(dir, fmt.Sprintf("dual-%.2f.png", scale)))
			}
		})
	}
}

func TestSFTPWindowMultiSelectQueueScrollAndSystemDrop(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 2)
	view := host.windows[0].view
	for index := range 18 {
		if err := os.WriteFile(filepath.Join(dir, fmt.Sprintf("batch-%02d.txt", index)), []byte("batch content"), 0600); err != nil {
			t.Fatal(err)
		}
	}
	w.navigate(true, dir)
	view.Rest()
	file := dualHit(t, view, dualRow(t, w, true, "batch-00.txt"))
	view.Click(file.X+25, file.Y+10)
	modifier := ui.ModifierControl
	if runtime.GOOS == "darwin" {
		modifier = ui.ModifierMeta
	}
	if handled, err := view.Driver().ShortcutChord(modifier, 'a'); err != nil || !handled {
		t.Fatal("select all failed", err)
	}
	blankTarget := w.ui.list[1]
	blankTarget.Y += blankTarget.Height - 30
	blankTarget.Height = 20
	dualDrag(t, view, file, blankTarget)
	dualWait(t, state, w, view, func() bool {
		return len(w.view.transfers) == 21 && !sftpTransfersActive(w.view.transfers) && !w.view.busy && !w.view.refreshAfterTransfers
	})
	if err := view.Resize(640, 360); err != nil {
		t.Fatal(err)
	}
	view.Rest()
	if w.ui.queueBar.MaxOffsetY <= 0 {
		t.Fatal("batch queue has no scrollbar")
	}
	bounds := w.ui.queueBounds
	if err := view.Scroll(ui.ScrollEvent{X: bounds.X + 20, Y: bounds.Y + 20, DeltaY: 10000}); err != nil {
		t.Fatal(err)
	}
	if w.ui.queueOffset != w.ui.queueBar.MaxOffsetY {
		t.Fatal("queue failed to reach older/later items")
	}
	dualHit(t, view, sftpWindowQueueRows.At(20))
	for _, bounds := range w.ui.list {
		if bounds.Y+bounds.Height > w.ui.size.Height {
			t.Fatal("queue pushed file list outside the window")
		}
	}
	external := filepath.Join(t.TempDir(), "external.txt")
	os.WriteFile(external, []byte("external drop"), 0600)
	target := dualHit(t, view, dualRow(t, w, false, "docs"))
	if accepted, err := view.Drop(ui.FileDropEvent{Phase: ui.FileDropDrop, X: target.X + 25, Y: target.Y + 10, Paths: []string{external}}); err != nil || !accepted {
		t.Fatal("native system drop refused", err)
	}
	dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 22 && !sftpTransfersActive(w.view.transfers) })
	remote.mu.Lock()
	actual := string(remote.files["/home/test/docs/external.txt"])
	remote.mu.Unlock()
	if actual != "external drop" {
		t.Fatal("system drop missed remote folder", w.view.transfers[len(w.view.transfers)-1])
	}
}

func TestSFTPWindowRefreshCannotRetargetAnActiveDrag(t *testing.T) {
	for _, external := range []bool{false, true} {
		t.Run(fmt.Sprintf("system-drop-%t", external), func(t *testing.T) {
			state, w, remote, host, dir := dualSFTPFixture(t, 1.5)
			view := host.windows[0].view
			target := dualHit(t, view, dualRow(t, w, false, "docs"))
			x, y := target.X+25, target.Y+10
			if external {
				if accepted, err := view.Drop(ui.FileDropEvent{Phase: ui.FileDropOver, X: x, Y: y}); err != nil || !accepted {
					t.Fatal("system drag refused", err)
				}
			} else {
				source := dualHit(t, view, dualRow(t, w, true, "中文上传.yml"))
				view.Press(source.X+25, source.Y+10)
				view.Move(x, y)
			}
			entries := []sftpclient.Entry{{Name: "aaa", Mode: os.ModeDir | 0755}}
			for _, row := range w.view.rows {
				entries = append(entries, row.entry)
			}
			state.showSFTPListing(w.view, w.view.path, entries)
			view.Rest()
			if w.view.rows[0].entry.Name != "docs" {
				t.Fatal("refresh moved the highlighted destination during a drag")
			}
			if external {
				view.Drop(ui.FileDropEvent{Phase: ui.FileDropDrop, X: x, Y: y, Paths: []string{filepath.Join(dir, "中文上传.yml")}})
			} else {
				view.Release(x, y)
			}
			if w.view.rows[0].entry.Name != "aaa" {
				t.Fatal("deferred refresh was not applied on release")
			}
			dualWait(t, state, w, view, func() bool { return len(w.view.transfers) == 1 && !sftpTransfersActive(w.view.transfers) })
			remote.mu.Lock()
			actual := string(remote.files["/home/test/docs/中文上传.yml"])
			remote.mu.Unlock()
			if actual != "name: 中文\n" {
				t.Fatal("refresh changed the captured destination")
			}
		})
	}
}

func TestSFTPWindowPreviewAndAsyncLocalPathUseWindowContext(t *testing.T) {
	state, w, remote, host, dir := dualSFTPFixture(t, 1.25)
	view := host.windows[0].view
	notes := dualHit(t, view, dualRow(t, w, false, "notes.txt"))
	if err := view.DoubleClick(notes.X+30, notes.Y+10); err != nil {
		t.Fatal(err)
	}
	dualWait(t, state, w, view, func() bool { return len(host.windows) == 2 })
	var document *fileDocument
	for _, d := range state.fileWindows.documents {
		document = d
	}
	if document == nil || document.original != "hello world" || document.source != w.session.Title+" · "+w.session.Address {
		t.Fatal("preview did not bind the transfer window's source")
	}
	document.window.Close()
	unsupported := filepath.Join(dir, "unsupported.pdf")
	os.WriteFile(unsupported, []byte("%PDF-1.7"), 0600)
	w.navigate(true, dir)
	view.Rest()
	for index, row := range w.view.lfolder.rows {
		if row.entry.Name == "unsupported.pdf" {
			w.openEntry(true, index)
		}
	}
	dualWait(t, state, w, view, func() bool { return len(w.notices.items) > 0 })
	if len(state.fileNotices.items) != 0 {
		t.Fatal("file-open error appeared in the wrong window")
	}
	large := filepath.Join(dir, "中文目录")
	os.Mkdir(large, 0700)
	for index := range 513 {
		os.WriteFile(filepath.Join(large, fmt.Sprintf("file-%03d.conf", index)), nil, 0600)
	}
	pathField := dualHit(t, view, sftpWindowPath[0])
	view.Click(pathField.X+30, pathField.Y+12)
	modifier := ui.ModifierControl
	if runtime.GOOS == "darwin" {
		modifier = ui.ModifierMeta
	}
	view.Driver().ShortcutChord(modifier, 'a')
	view.Type(large)
	view.Keys("enter")
	if w.pendingLocal == "" {
		t.Fatal("large folder was not marked pending")
	}
	before := len(w.view.worker.Transfers(nil))
	w.queueEntries(true, []sftpclient.Entry{{Name: "中文上传.yml"}}, dir, w.view.path)
	if len(w.view.worker.Transfers(nil)) != before {
		t.Fatal("pending directory allowed transfer to stale target")
	}
	dualWait(t, state, w, view, func() bool {
		return w.pendingLocal == "" && w.view.lfolder.path == large && len(w.view.lfolder.rows) == 513
	})
	remote.mu.Lock()
	stillOriginal := string(remote.files["/home/test/notes.txt"])
	remote.mu.Unlock()
	if stillOriginal != "hello world" {
		t.Fatal("preview/navigation changed remote content")
	}
}
