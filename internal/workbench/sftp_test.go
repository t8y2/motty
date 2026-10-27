package workbench

import (
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

type workbenchSFTPRemote struct {
	mu           sync.Mutex
	cwd          string
	entries      map[string][]os.FileInfo
	files        map[string][]byte
	closed       bool
	uploadClosed bool
	reads        []string          // every folder listed, in order
	readDelay    time.Duration     // how long a listing takes
	linkDelay    time.Duration     // how long resolving one link takes
	links        map[string]string // symbolic links: path to target
}

func (remote *workbenchSFTPRemote) Symlink(target, link string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.links == nil {
		remote.links = map[string]string{}
	}
	remote.links[link] = target
	remote.putEntryLocked(path.Dir(link), workbenchFileInfo{name: path.Base(link), mode: os.ModeSymlink | 0o777, when: time.Unix(1_700_002_000, 0)})
	return nil
}

func (remote *workbenchSFTPRemote) ReadLink(link string) (string, error) {
	remote.mu.Lock()
	target, ok := remote.links[link]
	delay := remote.linkDelay
	remote.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay) // a network round trip
	}
	if ok {
		return target, nil
	}
	return "", os.ErrNotExist
}

func (remote *workbenchSFTPRemote) Stat(name string) (os.FileInfo, error) {
	remote.mu.Lock()
	target, link := remote.links[name]
	remote.mu.Unlock()
	if link {
		if !path.IsAbs(target) {
			target = path.Join(path.Dir(name), target)
		}
		return remote.Stat(target)
	}
	return remote.Lstat(name)
}

func (remote *workbenchSFTPRemote) Getwd() (string, error) { return remote.cwd, nil }
func (remote *workbenchSFTPRemote) ReadDir(path string) ([]os.FileInfo, error) {
	remote.mu.Lock()
	remote.reads = append(remote.reads, path)
	delay := remote.readDelay
	remote.mu.Unlock()
	if delay > 0 {
		time.Sleep(delay) // a network round trip
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	return remote.entries[path], nil
}

// readCount is how many times a folder has been listed.
func (remote *workbenchSFTPRemote) readCount(path string) int {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	count := 0
	for _, read := range remote.reads {
		if read == path {
			count++
		}
	}
	return count
}
func (remote *workbenchSFTPRemote) Open(path string) (io.ReadCloser, error) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	content, ok := remote.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	return io.NopCloser(bytes.NewReader(content)), nil
}

type workbenchUploadWriter struct {
	remote *workbenchSFTPRemote
	path   string
	bytes.Buffer
}

type workbenchTransferFile struct {
	remote *workbenchSFTPRemote
	path   string
	data   []byte
	offset int64
	write  bool
	closed bool
}

func (file *workbenchTransferFile) Read(destination []byte) (int, error) {
	if file.offset >= int64(len(file.data)) {
		return 0, io.EOF
	}
	count := copy(destination, file.data[file.offset:])
	file.offset += int64(count)
	return count, nil
}

func (file *workbenchTransferFile) Write(source []byte) (int, error) {
	if !file.write {
		return 0, os.ErrPermission
	}
	end := file.offset + int64(len(source))
	if end > int64(len(file.data)) {
		file.data = append(file.data, make([]byte, end-int64(len(file.data)))...)
	}
	copy(file.data[file.offset:end], source)
	file.offset = end
	return len(source), nil
}

func (file *workbenchTransferFile) Seek(offset int64, whence int) (int64, error) {
	next := offset
	switch whence {
	case io.SeekCurrent:
		next = file.offset + offset
	case io.SeekEnd:
		next = int64(len(file.data)) + offset
	}
	if next < 0 {
		return 0, os.ErrInvalid
	}
	file.offset = next
	return next, nil
}

func (file *workbenchTransferFile) Close() error {
	if file.closed {
		return nil
	}
	file.closed = true
	if file.write {
		return file.persist()
	}
	return nil
}

func (file *workbenchTransferFile) Stat() (os.FileInfo, error) {
	return workbenchFileInfo{name: path.Base(file.path), size: int64(len(file.data)), mode: 0o644, when: time.Unix(1_700_001_000, 0)}, nil
}

func (file *workbenchTransferFile) Sync() error {
	if file.write {
		return file.persist()
	}
	return nil
}

func (file *workbenchTransferFile) Truncate(size int64) error {
	if !file.write || size < 0 {
		return os.ErrPermission
	}
	if size < int64(len(file.data)) {
		file.data = file.data[:size]
	} else if size > int64(len(file.data)) {
		file.data = append(file.data, make([]byte, size-int64(len(file.data)))...)
	}
	if file.offset > size {
		file.offset = size
	}
	return nil
}

func (file *workbenchTransferFile) persist() error {
	file.remote.mu.Lock()
	defer file.remote.mu.Unlock()
	parent := path.Dir(file.path)
	if _, ok := file.remote.entries[parent]; !ok {
		return os.ErrNotExist
	}
	content := append([]byte(nil), file.data...)
	file.remote.files[file.path] = content
	file.remote.putEntryLocked(parent, workbenchFileInfo{name: path.Base(file.path), size: int64(len(content)), mode: 0o644, when: time.Unix(1_700_001_000, 0)})
	file.remote.uploadClosed = true
	return nil
}

func (writer *workbenchUploadWriter) Close() error {
	writer.remote.mu.Lock()
	defer writer.remote.mu.Unlock()
	parent := path.Dir(writer.path)
	if _, ok := writer.remote.entries[parent]; !ok {
		return os.ErrNotExist
	}
	content := append([]byte(nil), writer.Bytes()...)
	writer.remote.files[writer.path] = content
	writer.remote.putEntryLocked(parent, workbenchFileInfo{name: path.Base(writer.path), size: int64(len(content)), mode: 0o644, when: time.Unix(1_700_001_000, 0)})
	writer.remote.uploadClosed = true
	return nil
}

func (remote *workbenchSFTPRemote) Create(remotePath string) (io.WriteCloser, error) {
	return &workbenchUploadWriter{remote: remote, path: remotePath}, nil
}

func (remote *workbenchSFTPRemote) OpenTransferFile(remotePath string, flags int) (sftpclient.TransferFile, error) {
	remote.mu.Lock()
	content, exists := remote.files[remotePath]
	remote.mu.Unlock()
	if flags&os.O_EXCL != 0 && exists {
		return nil, os.ErrExist
	}
	if flags == os.O_RDONLY && !exists {
		return nil, os.ErrNotExist
	}
	return &workbenchTransferFile{
		remote: remote, path: remotePath, data: append([]byte(nil), content...), write: flags != os.O_RDONLY,
	}, nil
}

func (remote *workbenchSFTPRemote) Lstat(remotePath string) (os.FileInfo, error) {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if children, ok := remote.entries[remotePath]; ok {
		_ = children
		return workbenchFileInfo{name: path.Base(remotePath), mode: os.ModeDir | 0o755, when: time.Unix(1_700_001_100, 0)}, nil
	}
	for _, item := range remote.entries[path.Dir(remotePath)] {
		if item.Name() == path.Base(remotePath) {
			return item, nil
		}
	}
	return nil, os.ErrNotExist
}

func (remote *workbenchSFTPRemote) PosixRename(oldPath, newPath string) error {
	return remote.Rename(oldPath, newPath)
}

func (remote *workbenchSFTPRemote) Mkdir(remotePath string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if _, ok := remote.entries[remotePath]; ok {
		return os.ErrExist
	}
	parent := path.Dir(remotePath)
	if _, ok := remote.entries[parent]; !ok {
		return os.ErrNotExist
	}
	remote.entries[remotePath] = []os.FileInfo{}
	remote.putEntryLocked(parent, workbenchFileInfo{name: path.Base(remotePath), mode: os.ModeDir | 0o755, when: time.Unix(1_700_001_100, 0)})
	return nil
}

func (remote *workbenchSFTPRemote) Rename(oldPath, newPath string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	entry, ok := remote.removeEntryLocked(path.Dir(oldPath), path.Base(oldPath))
	if !ok {
		return os.ErrNotExist
	}
	if _, ok := remote.entries[path.Dir(newPath)]; !ok {
		remote.putEntryLocked(path.Dir(oldPath), entry)
		return os.ErrNotExist
	}
	entry.name = path.Base(newPath)
	remote.putEntryLocked(path.Dir(newPath), entry)
	if content, ok := remote.files[oldPath]; ok {
		delete(remote.files, oldPath)
		remote.files[newPath] = content
	}
	if children, ok := remote.entries[oldPath]; ok {
		delete(remote.entries, oldPath)
		remote.entries[newPath] = children
	}
	return nil
}

func (remote *workbenchSFTPRemote) Remove(remotePath string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if _, ok := remote.files[remotePath]; !ok {
		return os.ErrNotExist
	}
	delete(remote.files, remotePath)
	remote.removeEntryLocked(path.Dir(remotePath), path.Base(remotePath))
	return nil
}

func (remote *workbenchSFTPRemote) RemoveDirectory(remotePath string) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	children, ok := remote.entries[remotePath]
	if !ok {
		return os.ErrNotExist
	}
	if len(children) != 0 {
		return errors.New("directory not empty")
	}
	delete(remote.entries, remotePath)
	remote.removeEntryLocked(path.Dir(remotePath), path.Base(remotePath))
	return nil
}

func (remote *workbenchSFTPRemote) Chmod(remotePath string, mode os.FileMode) error {
	remote.mu.Lock()
	defer remote.mu.Unlock()
	parent, name := path.Dir(remotePath), path.Base(remotePath)
	entries := remote.entries[parent]
	for index, item := range entries {
		if item.Name() != name {
			continue
		}
		info := item.(workbenchFileInfo)
		info.mode = info.mode.Type() | mode.Perm()
		entries[index] = info
		return nil
	}
	return os.ErrNotExist
}

func (remote *workbenchSFTPRemote) putEntryLocked(parent string, next workbenchFileInfo) {
	entries := remote.entries[parent]
	for index, item := range entries {
		if item.Name() == next.name {
			entries[index] = next
			remote.entries[parent] = entries
			return
		}
	}
	remote.entries[parent] = append(entries, next)
}

func (remote *workbenchSFTPRemote) removeEntryLocked(parent, name string) (workbenchFileInfo, bool) {
	entries := remote.entries[parent]
	for index, item := range entries {
		if item.Name() != name {
			continue
		}
		info := item.(workbenchFileInfo)
		remote.entries[parent] = append(entries[:index], entries[index+1:]...)
		return info, true
	}
	return workbenchFileInfo{}, false
}
func (remote *workbenchSFTPRemote) Close() error {
	remote.mu.Lock()
	remote.closed = true
	remote.mu.Unlock()
	return nil
}

type workbenchFileInfo struct {
	name string
	size int64
	mode os.FileMode
	when time.Time
}

func (info workbenchFileInfo) Name() string       { return info.name }
func (info workbenchFileInfo) Size() int64        { return info.size }
func (info workbenchFileInfo) Mode() os.FileMode  { return info.mode }
func (info workbenchFileInfo) ModTime() time.Time { return info.when }
func (info workbenchFileInfo) IsDir() bool        { return info.mode.IsDir() }
func (info workbenchFileInfo) Sys() any           { return nil }

func openTestSFTP(t testing.TB) (*State, *Session, *fakeConn, *workbenchSFTPRemote) {
	t.Helper()
	state, _, fake, _ := liveSession(t)
	remote := &workbenchSFTPRemote{
		cwd: "/home/test",
		entries: map[string][]os.FileInfo{
			"/home/test": {
				workbenchFileInfo{name: "notes.txt", size: 11, mode: 0o644, when: time.Unix(1_700_000_000, 0)},
				workbenchFileInfo{name: "docs", mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_100, 0)},
			},
			"/home/test/docs": {
				workbenchFileInfo{name: "guide.md", size: 7, mode: 0o644, when: time.Unix(1_700_000_200, 0)},
			},
		},
		files: map[string][]byte{
			"/home/test/notes.txt":     []byte("hello world"),
			"/home/test/docs/guide.md": []byte("guide\n"),
		},
	}
	fake.mu.Lock()
	fake.sftp = remote
	fake.mu.Unlock()
	state.Click(hitSFTPOpen)
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == 2 })
	t.Cleanup(func() { state.closeSFTP(session) })
	return state, session, fake, remote
}

func waitSFTP(t testing.TB, state *State, session *Session, ready func(*sftpView) bool) {
	t.Helper()
	deadline := time.Now().Add(5 * time.Second)
	for time.Now().Before(deadline) {
		state.Wake()
		if session.files != nil && ready(session.files) {
			return
		}
		time.Sleep(time.Millisecond)
	}
	if session.files == nil {
		t.Fatal("SFTP state did not settle: no file view")
	}
	t.Fatalf("SFTP state did not settle: path=%q rows=%d busy=%t waiting=%q prefetching=%d",
		session.files.path, len(session.files.rows), session.files.busy, session.files.waiting, len(session.files.prefetching))
}

func sftpRowIndex(t testing.TB, view *sftpView, name string) int {
	t.Helper()
	for index := range view.rows {
		if view.rows[index].entry.Name == name {
			return index
		}
	}
	t.Fatalf("SFTP row %q not found in %+v", name, view.rows)
	return -1
}

func clickSFTPRow(t testing.TB, state *State, view *sftpView, name string) ui.ID {
	t.Helper()
	index := sftpRowIndex(t, view, name)
	id := hitSFTPEntryList.At(index - view.first)
	state.Click(id)
	return id
}

func replaceSFTPDialogText(t testing.TB, state *State, value string) {
	t.Helper()
	editor := state.EditorFor(hitSFTPDialogField)
	if editor == nil {
		t.Fatal("SFTP dialog field has no editor")
	}
	editor.SetSelection(text.Range{End: editor.RuneCount()})
	editor.Insert(value)
}

func openSFTPTextEditor(t testing.TB, state *State, session *Session, name string) *sftpView {
	t.Helper()
	view := session.files
	clickSFTPRow(t, state, view, name)
	state.Click(hitSFTPOpenEntry)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.editPath != "" })
	return session.files
}

func replaceSFTPText(t testing.TB, state *State, value string) {
	t.Helper()
	editor := state.EditorFor(hitSFTPEditor)
	if editor == nil {
		t.Fatal("SFTP text editor is unavailable")
	}
	editor.SetSelection(text.Range{End: editor.RuneCount()})
	editor.Insert(value)
}

func TestSFTPBrowseAndDownload(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	if view.path != "/home/test" || view.rows[0].entry.Name != "docs" || view.rows[1].entry.Name != "notes.txt" {
		t.Fatalf("initial listing = %q %+v", view.path, view.rows)
	}

	rowID := clickSFTPRow(t, state, view, "docs")
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: rowID}); !result.Handled || !result.Repaint {
		t.Fatalf("open directory key result = %+v", result)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if len(view.rows) != 1 || view.rows[0].entry.Name != "guide.md" {
		t.Fatalf("docs listing = %+v", view.rows)
	}
	state.Click(hitSFTPUp)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })

	target := filepath.Join(t.TempDir(), "notes.txt")
	var requested app.SaveFileOptions
	state.options.SaveFile = func(options app.SaveFileOptions) (string, error) {
		requested = options
		return target, nil
	}
	state.Click(hitSFTPEntryList.At(1))
	state.Click(hitSFTPDownload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferCompleted
	})
	content, err := os.ReadFile(target)
	if err != nil || string(content) != "hello world" {
		t.Fatalf("download = %q, %v", content, err)
	}
	if requested.SuggestedName != "notes.txt" || requested.DefaultExtension != "txt" {
		t.Fatalf("save options = %+v", requested)
	}
}

// A long line of a remote file wraps in the editor instead of running off its
// edge, and Up and Down go by the rows it wrapped to.
func TestSFTPTextEditorWrapsLongLinesAndMovesByRow(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	replaceSFTPText(t, state, strings.Repeat("a long line of configuration that wraps ", 12)+"\nshort")
	canvas, _ := paintAt(state, ui.Size{Width: 1400, Height: 900})
	box := panelHit(t, canvas, contextID(hitSFTPEditor))
	rows := 0
	for _, text := range canvas.Texts() {
		if text.Bounds.Y > box.Bounds.Y && text.Bounds.Y < box.Bounds.Y+box.Bounds.Height && text.Bounds.X >= box.Bounds.X && text.Bounds.X < box.Bounds.X+box.Bounds.Width {
			rows++
			if got := canvas.TextWidthWeight(text.Value, 13, ui.WeightRegular); got > box.Bounds.Width {
				t.Errorf("row %q is %v wide in an editor %v wide", text.Value, got, box.Bounds.Width)
			}
		}
	}
	if rows < 3 {
		t.Fatalf("the file's long line took %d rows", rows)
	}
	end := view.editEditor.Caret()
	if result := state.Key(ui.KeyEvent{Key: ui.KeyUp, Target: contextID(hitSFTPEditor)}); !result.Handled || view.editEditor.Caret() >= end {
		t.Fatalf("Up from the end: caret %d -> %d, %+v", end, view.editEditor.Caret(), result)
	}
}

func TestSFTPRemoteTextEditorOpensAndSavesAtomically(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	if view.editPath != "/home/test/notes.txt" || view.editEditor.String() != "hello world" {
		t.Fatalf("editor = path %q value %q", view.editPath, view.editEditor.String())
	}
	if editor := state.EditorFor(hitSFTPEditor); editor != &view.editEditor {
		t.Fatalf("resolved editor = %p, want %p", editor, &view.editEditor)
	}

	replaceSFTPText(t, state, "saved remotely\n中文\n")
	state.Click(hitSFTPEditSave)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && !sftpEditorDirty(view) && view.status == state.messages.SFTPEditSaved
	})
	remote.mu.Lock()
	content := append([]byte(nil), remote.files[view.editPath]...)
	temporaryCount := 0
	for remotePath := range remote.files {
		if strings.Contains(remotePath, ".edit-") {
			temporaryCount++
		}
	}
	remote.mu.Unlock()
	if string(content) != "saved remotely\n中文\n" || temporaryCount != 0 {
		t.Fatalf("remote content = %q, temporaries = %d", content, temporaryCount)
	}

	state.Click(hitSFTPEditBack)
	if view.editPath != "" || state.EditorFor(hitSFTPEditor) != nil {
		t.Fatalf("editor stayed open: %+v", view)
	}
}

func TestSFTPRemoteTextEditorPreservesLocalEditsOnConflict(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	replaceSFTPText(t, state, "my local edits\n")
	remote.mu.Lock()
	remote.files[view.editPath] = []byte("server changed\n")
	remote.putEntryLocked(view.path, workbenchFileInfo{
		name: path.Base(view.editPath), size: int64(len("server changed\n")), mode: 0o644, when: time.Unix(1_700_002_000, 0),
	})
	remote.mu.Unlock()

	state.Click(hitSFTPEditSave)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.dialog == sftpDialogRemoteChanged })
	if view.editEditor.String() != "my local edits\n" || view.errorText != state.messages.SFTPEditConflict {
		t.Fatalf("conflict editor = %q error = %q", view.editEditor.String(), view.errorText)
	}
	state.Click(hitSFTPDialogCancel)
	if view.dialog != sftpDialogNone || view.editEditor.String() != "my local edits\n" {
		t.Fatalf("keeping edits changed state: %+v", view)
	}

	state.Click(hitSFTPEditReload)
	if view.dialog != sftpDialogReloadEdit {
		t.Fatalf("reload confirmation = %v", view.dialog)
	}
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && view.dialog == sftpDialogNone && view.editEditor.String() == "server changed\n"
	})
}

func TestSFTPRemoteTextEditorConfirmsDiscard(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	replaceSFTPText(t, state, "unsaved")
	state.Click(hitSFTPEditBack)
	if view.dialog != sftpDialogDiscardEdit || view.editPath == "" {
		t.Fatalf("discard confirmation = %+v", view)
	}
	state.Click(hitSFTPDialogCancel)
	if view.editEditor.String() != "unsaved" {
		t.Fatalf("cancel discard lost edits: %q", view.editEditor.String())
	}
	state.Click(hitSFTPEditBack)
	state.Click(hitSFTPDialogSubmit)
	if view.editPath != "" || view.dialog != sftpDialogNone {
		t.Fatalf("discard did not close editor: %+v", view)
	}
}

func TestSFTPRemoteTextEditorIsDestroyedOnDisconnect(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	replaceSFTPText(t, state, "sensitive unsaved draft")
	worker := view.worker
	fake.set(func(status *sshconn.Status) {
		status.Phase = sshconn.PhaseClosed
		status.Err = errors.New("connection lost")
	})
	state.Wake()
	if session.files != nil || state.EditorFor(hitSFTPEditor) != nil {
		t.Fatalf("disconnect retained SFTP editor: %+v", session.files)
	}
	select {
	case <-worker.Done():
	case <-time.After(time.Second):
		t.Fatal("disconnect did not stop the SFTP worker")
	}
}

func TestSFTPRemoteTextEditorIsDestroyedWhenSessionCloses(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := openSFTPTextEditor(t, state, session, "notes.txt")
	replaceSFTPText(t, state, "unsaved session draft")
	worker := view.worker
	state.closeSession(session.ID)
	if _, ok := state.sessionByID(session.ID); ok || state.EditorFor(hitSFTPEditor) != nil {
		t.Fatal("closed session retained its SFTP editor")
	}
	select {
	case <-worker.Done():
	case <-time.After(time.Second):
		t.Fatal("closing the session did not stop the SFTP worker")
	}
}

func TestSFTPRemoteTextEditorPaintsStableControls(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	openSFTPTextEditor(t, state, session, "notes.txt")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if _, ok := canvas.NextFrame(); ok {
		t.Fatal("idle SFTP editor requested another frame")
	}
	for _, id := range []ui.ID{hitSFTPEditor, hitSFTPEditSave, hitSFTPEditBack, hitSFTPEditReload} {
		if _, ok := hitByID(&canvas, contextID(id)); !ok {
			t.Fatalf("missing editor hit %d", id)
		}
	}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("SFTP editor steady-state paint allocations = %v", allocations)
	}
}

func TestSFTPUploadUsesOpenFilesAndRefreshesDirectory(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	localPath := filepath.Join(t.TempDir(), "upload.bin")
	if err := os.WriteFile(localPath, []byte("uploaded payload"), 0o600); err != nil {
		t.Fatal(err)
	}
	var requested app.OpenFileOptions
	state.options.OpenFiles = func(options app.OpenFileOptions) ([]string, error) {
		requested = options
		return []string{localPath}, nil
	}

	state.Click(hitSFTPUpload)
	if session.files.busy || !strings.Contains(session.files.status, "upload.bin") {
		t.Fatalf("upload did not enter queue: %+v", session.files)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "upload.bin") >= 0
	})
	if requested.Title != state.messages.SFTPUploadTitle || !requested.AllowMultiple {
		t.Fatalf("open file options = %+v", requested)
	}
	remote.mu.Lock()
	content := append([]byte(nil), remote.files["/home/test/upload.bin"]...)
	closed := remote.uploadClosed
	remote.mu.Unlock()
	if string(content) != "uploaded payload" || !closed {
		t.Fatalf("uploaded content = %q, closed = %v", content, closed)
	}
	if !strings.Contains(session.files.status, "/home/test/upload.bin") {
		t.Fatalf("upload completion status = %q", session.files.status)
	}
}

func TestSFTPMultiSelectDownloadsFilesAndDirectories(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	destination := t.TempDir()
	state.options.OpenDirectory = func(options app.OpenDirectoryOptions) (string, error) {
		if options.Title != state.messages.SFTPDownloadDirectoryTitle {
			t.Fatalf("directory title = %q", options.Title)
		}
		return destination, nil
	}
	clickSFTPRow(t, state, view, "docs")
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	if len(view.transfers) != 2 || view.busy {
		t.Fatalf("queued transfers = %+v, busy = %t", view.transfers, view.busy)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 2 && view.transfers[0].State == sftpclient.TransferCompleted && view.transfers[1].State == sftpclient.TransferCompleted
	})
	if content, err := os.ReadFile(filepath.Join(destination, "notes.txt")); err != nil || string(content) != "hello world" {
		t.Fatalf("notes download = %q, %v", content, err)
	}
	if content, err := os.ReadFile(filepath.Join(destination, "docs", "guide.md")); err != nil || string(content) != "guide\n" {
		t.Fatalf("directory download = %q, %v", content, err)
	}
}

func TestSFTPUploadsMultipleFilesAndDirectory(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	first := filepath.Join(t.TempDir(), "first.txt")
	second := filepath.Join(t.TempDir(), "second.txt")
	if err := os.WriteFile(first, []byte("first"), 0o600); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(second, []byte("second"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.OpenFiles = func(options app.OpenFileOptions) ([]string, error) {
		if !options.AllowMultiple {
			t.Fatal("multi-file upload did not request multiple selection")
		}
		return []string{first, second}, nil
	}
	state.Click(hitSFTPUpload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 2 && view.transfers[0].State == sftpclient.TransferCompleted && view.transfers[1].State == sftpclient.TransferCompleted && !view.busy
	})

	directory := filepath.Join(t.TempDir(), "bundle")
	if err := os.MkdirAll(filepath.Join(directory, "nested"), 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(directory, "nested", "data.bin"), []byte("nested"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.OpenDirectory = func(options app.OpenDirectoryOptions) (string, error) {
		if options.Title != state.messages.SFTPUploadDirectoryTitle {
			t.Fatalf("upload directory title = %q", options.Title)
		}
		return directory, nil
	}
	state.Click(hitSFTPUploadDirectory)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return len(view.transfers) == 3 && view.transfers[2].State == sftpclient.TransferCompleted && !view.busy
	})
	remote.mu.Lock()
	firstContent := string(remote.files["/home/test/first.txt"])
	secondContent := string(remote.files["/home/test/second.txt"])
	nestedContent := string(remote.files["/home/test/bundle/nested/data.bin"])
	remote.mu.Unlock()
	if firstContent != "first" || secondContent != "second" || nestedContent != "nested" {
		t.Fatalf("uploaded contents = %q, %q, %q", firstContent, secondContent, nestedContent)
	}
}

func TestSFTPOverwriteRequiresConfirmationThenResumes(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	target := filepath.Join(t.TempDir(), "notes.txt")
	if err := os.WriteFile(target, []byte("do not replace yet"), 0o600); err != nil {
		t.Fatal(err)
	}
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return target, nil }
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDownload)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return state.transfers.open && len(view.transfers) == 1 && view.transfers[0].State == sftpclient.TransferFailed
	})
	if content, err := os.ReadFile(target); err != nil || string(content) != "do not replace yet" {
		t.Fatalf("destination changed before confirmation: %q, %v", content, err)
	}
	state.Click(hitSFTPClose)
	if session.files != view {
		t.Fatal("closing the browser detached a retryable transfer")
	}
	state.Click(hitTransfersReplace.At(state.transfers.entries[0].id))
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.dialog == sftpDialogNone && view.transfers[0].State == sftpclient.TransferCompleted
	})
	if content, err := os.ReadFile(target); err != nil || string(content) != "hello world" {
		t.Fatalf("confirmed replacement = %q, %v", content, err)
	}
}

func sftpRowIndexOrNegative(view *sftpView, name string) int {
	for index := range view.rows {
		if view.rows[index].entry.Name == name {
			return index
		}
	}
	return -1
}

func TestSFTPCreateRenameChmodAndRefresh(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files

	state.Click(hitSFTPNewDirectory)
	if view.dialog != sftpDialogCreateDirectory || state.EditorFor(hitSFTPDialogField) == nil {
		t.Fatalf("create dialog = %+v", view)
	}
	replaceSFTPDialogText(t, state, "drafts")
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitSFTPDialogField}); !result.Handled {
		t.Fatalf("create Enter = %+v", result)
	}
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "drafts") >= 0
	})
	remote.mu.Lock()
	_, directoryCreated := remote.entries["/home/test/drafts"]
	remote.mu.Unlock()
	if !directoryCreated {
		t.Fatal("directory was not created")
	}

	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPRename)
	if view.dialog != sftpDialogRename || view.dialogEditor.String() != "/home/test/notes.txt" {
		t.Fatalf("rename dialog = %+v", view)
	}
	replaceSFTPDialogText(t, state, "renamed.txt")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "renamed.txt") >= 0
	})
	remote.mu.Lock()
	_, renamed := remote.files["/home/test/renamed.txt"]
	_, oldRemains := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !renamed || oldRemains {
		t.Fatalf("rename state: renamed=%v old=%v", renamed, oldRemains)
	}

	clickSFTPRow(t, state, view, "renamed.txt")
	state.Click(hitSFTPChmod)
	if view.dialogEditor.String() != "644" {
		t.Fatalf("chmod initial value = %q", view.dialogEditor.String())
	}
	replaceSFTPDialogText(t, state, "888")
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogChmod || view.dialogError != state.messages.SFTPErrModeInvalid {
		t.Fatalf("invalid chmod dialog = %+v", view)
	}
	replaceSFTPDialogText(t, state, "600")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		index := sftpRowIndexOrNegative(view, "renamed.txt")
		return !view.busy && index >= 0 && view.rows[index].entry.Mode.Perm() == 0o600
	})
	if !strings.Contains(view.status, "600") {
		t.Fatalf("chmod completion status = %q", view.status)
	}
}

func TestSFTPDeleteRequiresConfirmationAndDistinguishesDirectories(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files

	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPDelete)
	if view.dialog != sftpDialogDelete || view.dialogEntry.IsDir() {
		t.Fatalf("file delete dialog = %+v", view)
	}
	var dialogCanvas ui.Canvas
	state.Paint(&dialogCanvas, ui.Size{Width: 1400, Height: 900})
	if dialogCanvas.InitialFocus() != hitSFTPDialogCancel || dialogCanvas.TrappedFocusScope() != dialogScope {
		t.Fatalf("delete dialog focus=%d scope=%d", dialogCanvas.InitialFocus(), dialogCanvas.TrappedFocusScope())
	}
	remote.mu.Lock()
	_, existsBeforeConfirm := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !existsBeforeConfirm {
		t.Fatal("one click deleted the file")
	}
	if result := state.Key(ui.KeyEvent{Key: ui.KeyEscape}); !result.Handled || view.dialog != sftpDialogNone {
		t.Fatalf("delete Escape = %+v dialog=%d", result, view.dialog)
	}
	remote.mu.Lock()
	_, existsAfterCancel := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !existsAfterCancel {
		t.Fatal("cancel deleted the file")
	}

	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "notes.txt") < 0
	})
	remote.mu.Lock()
	_, fileRemains := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if fileRemains {
		t.Fatal("confirmed file delete did not remove the file")
	}

	state.Click(hitSFTPNewDirectory)
	replaceSFTPDialogText(t, state, "empty")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "empty") >= 0
	})
	clickSFTPRow(t, state, view, "empty")
	state.Click(hitSFTPDelete)
	if !view.dialogEntry.IsDir() {
		t.Fatal("directory delete did not retain entry type")
	}
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "empty") < 0
	})
	remote.mu.Lock()
	_, directoryRemains := remote.entries["/home/test/empty"]
	remote.mu.Unlock()
	if directoryRemains {
		t.Fatal("confirmed empty-directory delete did not remove it")
	}
}

func TestSFTPWriteErrorsRemainVisible(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	state.Click(hitSFTPNewDirectory)
	replaceSFTPDialogText(t, state, "docs")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
	if view.errorText == "" {
		t.Fatalf("create error = %q", view.errorText)
	}

	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
	remote.mu.Lock()
	_, stillExists := remote.entries["/home/test/docs"]
	remote.mu.Unlock()
	if !stillExists || view.errorText == "" {
		t.Fatalf("nonempty delete: exists=%v error=%q", stillExists, view.errorText)
	}
}

func TestSFTPWorkerStartsOnlyWhenFilesPageOpens(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	state.Wake()
	fake.mu.Lock()
	opens := fake.sftpOpens
	fake.mu.Unlock()
	if opens != 0 {
		t.Fatalf("idle terminal opened %d SFTP subsystems", opens)
	}
}

func TestSFTPSaveCancelDoesNotTransfer(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.options.SaveFile = func(app.SaveFileOptions) (string, error) { return "", app.ErrCanceled }
	state.Click(hitSFTPEntryList.At(1))
	state.Click(hitSFTPDownload)
	if session.files.busy || session.files.status != state.messages.SFTPSaveCanceled {
		t.Fatalf("view after cancel = %+v", session.files)
	}
}

func TestLeavingSFTPStopsItsWorker(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	worker := session.files.worker
	state.Click(hitHosts)
	if session.files != nil {
		t.Fatal("SFTP page stayed attached")
	}
	select {
	case <-worker.Done():
	case <-time.After(time.Second):
		t.Fatal("SFTP worker did not stop")
	}
	remote.mu.Lock()
	closed := remote.closed
	remote.mu.Unlock()
	if !closed {
		t.Fatal("SFTP subsystem stayed open")
	}
}

func TestSFTPSteadyStatePaintDoesNotAllocate(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	clickSFTPRow(t, state, session.files, "notes.txt")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	allocations := testing.AllocsPerRun(100, func() {
		canvas.Reset()
		state.Paint(&canvas, size)
	})
	if allocations != 0 {
		t.Fatalf("SFTP steady-state paint allocations = %v", allocations)
	}
}

func BenchmarkPaintSFTPPage(b *testing.B) {
	state, session, _, _ := openTestSFTP(b)
	clickSFTPRow(b, state, session.files, "notes.txt")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

func BenchmarkPaintSFTPTextEditor(b *testing.B) {
	state, session, _, _ := openTestSFTP(b)
	openSFTPTextEditor(b, state, session, "notes.txt")
	var canvas ui.Canvas
	size := ui.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	b.ReportAllocs()
	b.ResetTimer()
	for b.Loop() {
		canvas.Reset()
		state.Paint(&canvas, size)
	}
}

// The list scrolls like everything in Viva: a positive delta reveals rows below.
// A wheel over the terminal beside the panel is the terminal's, not the list's.
func TestSFTPListScrollsDownWithPositiveDeltaOnlyOverThePanel(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	entries := make([]sftpclient.Entry, 100)
	for index := range entries {
		entries[index] = sftpclient.Entry{Name: fmt.Sprintf("file-%03d.txt", index), Mode: 0o644}
	}
	view.rows = prepareSFTPRows(view.rows, entries)
	view.first = 0
	size := ui.Size{Width: 1400, Height: 900}
	var canvas ui.Canvas
	state.Paint(&canvas, size)
	if view.visibleRows <= 0 || view.visibleRows >= len(view.rows) {
		t.Fatalf("visible rows = %d of %d", view.visibleRows, len(view.rows))
	}
	panel := state.contextRect
	if panel.Width == 0 {
		t.Fatal("the paint reported no files panel")
	}

	// Over empty space of the panel the wheel event names no control; over a
	// row or a button it names that control. Both must reach the list.
	var over ui.ID
	scroll := func(x, delta float32) {
		state.Scroll(ui.ScrollEvent{X: x, Y: panel.Y + 100, DeltaY: delta, Precise: true, Viewport: size, Target: over})
	}
	scroll(panel.X+panel.Width/2, 3*sftpRowHeight)
	if view.first != 3 {
		t.Fatalf("a positive wheel moved the first row to %d, want 3", view.first)
	}
	scroll(panel.X+panel.Width/2, -sftpRowHeight)
	if view.first != 2 {
		t.Fatalf("a negative wheel moved the first row to %d, want 2", view.first)
	}
	scroll(panel.X+panel.Width/2, sftpRowHeight/3) // less than a row
	if view.first != 2 {
		t.Fatalf("a third of a row moved the list to %d", view.first)
	}
	scroll(panel.X+panel.Width/2, 2*sftpRowHeight/3+1) // the rest of it
	if view.first != 3 {
		t.Fatalf("banked travel did not add up to a row: first = %d", view.first)
	}
	over = contextID(hitSFTPEntryList.At(0)) // over a row of the list
	scroll(panel.X+panel.Width/2, -1.1*sftpRowHeight)
	if view.first != 2 {
		t.Fatalf("a wheel over a row moved the first row to %d, want 2", view.first)
	}
	scroll(panel.X+panel.Width/2, 1.1*sftpRowHeight)
	over = ui.ID{}
	scroll(panel.X+panel.Width+200, 5*sftpRowHeight) // over the terminal
	if view.first != 3 {
		t.Fatalf("a wheel over the terminal moved the file list to %d", view.first)
	}
	scroll(panel.X+panel.Width/2, 1000*sftpRowHeight)
	if want := len(view.rows) - view.visibleRows; view.first != want {
		t.Fatalf("scrolling far down left the first row at %d, want the last page at %d", view.first, want)
	}
}

// A files panel that was open when the app last quit comes back with its list:
// the panel follows the session, so once that session is connected the list is
// opened for it without another click.
func TestOpenFilesPanelListsAConnectedSessionWithoutAClick(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	state.closeSFTP(session) // as after a restart: panel open, no list yet
	if session.files != nil {
		t.Fatal("the list survived closeSFTP")
	}
	state.syncActiveSession()
	if session.files == nil {
		t.Fatal("an open files panel did not start listing the connected session")
	}
	state.closeSFTP(session)
}

func TestInfoBarOffersSplitsBesideTheTools(t *testing.T) {
	state, _, _, _ := liveSession(t)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, id := range []ui.ID{hitPaneSplitHorizontal, hitPaneSplitVertical} {
		if _, ok := hitByID(&canvas, id); !ok {
			t.Fatalf("no split button %d in the bar", id)
		}
	}
	before := len(state.deck.Panes(state.deck.ActiveWorkspace(), nil))
	state.Click(hitPaneSplitHorizontal)
	if got := len(state.deck.Panes(state.deck.ActiveWorkspace(), nil)); got != before+1 {
		t.Fatalf("panes = %d, want %d after the split button", got, before+1)
	}
}

// Double-clicking a folder in the list enters it, and double-clicking a file
// opens it in the editor; a single click only selects.
func TestSFTPDoubleClickEntersFoldersAndOpensFiles(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	folder, file := -1, -1
	for index := range view.rows {
		if view.rows[index].entry.IsDir() {
			folder = index
		} else {
			file = index
		}
	}
	if folder < 0 || file < 0 {
		t.Fatalf("rows = %+v", view.rows)
	}
	press := func(index, clicks int) {
		state.Pointer(ui.PointerEvent{
			Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: clicks,
			Target: contextID(hitSFTPEntryList.At(index - view.first)),
		})
	}

	press(folder, 1)
	if view.path != "/home/test" || view.busy {
		t.Fatalf("a single click navigated: path=%q busy=%v", view.path, view.busy)
	}
	press(folder, 2)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })

	state.Click(hitSFTPUp)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
	press(file, 2)
	waitSFTP(t, state, session, func(view *sftpView) bool { return view.editPath != "" })
	if view.editName != "notes.txt" {
		t.Fatalf("opened %q for editing", view.editName)
	}
}

// Entering a folder costs a round trip to the server. The click that selects a
// folder starts reading it, so the double-click that follows finds the listing
// done or under way instead of starting it; a folder seen before comes back at
// once, and refresh reads again.
func TestSFTPEnteringAFolderUsesWhatTheFirstClickStarted(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.readDelay = 80 * time.Millisecond
	remote.mu.Unlock()
	docs := sftpRowIndex(t, view, "docs")
	target := contextID(hitSFTPEntryList.At(docs - view.first))
	press := func(clicks int) {
		// As the runtime delivers a click: the press, then the click on release.
		result := state.Pointer(ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: clicks, Target: target})
		if !result.Handled {
			state.Click(target)
		}
	}

	press(1)
	time.Sleep(100 * time.Millisecond) // the gap between the two presses of a double-click
	started := time.Now()
	press(2)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	if waited := time.Since(started); waited > 30*time.Millisecond {
		t.Fatalf("entering the folder took %v after the double-click; the first click should have started the read", waited)
	}
	if got := remote.readCount("/home/test/docs"); got != 1 {
		t.Fatalf("the folder was listed %d times, want once", got)
	}

	// Back up, and in again: the folder is remembered, so it shows without
	// waiting, and is read once more behind the scenes to stay true.
	state.Click(hitSFTPUp)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
	started = time.Now()
	press(2)
	if view.path != "/home/test/docs" || view.busy {
		t.Fatalf("a remembered folder did not open at once: path=%q busy=%v", view.path, view.busy)
	}
	if time.Since(started) > 20*time.Millisecond {
		t.Fatalf("a remembered folder took %v to show", time.Since(started))
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return remote.readCount("/home/test/docs") == 2 })

	// Refresh trusts nothing remembered.
	before := remote.readCount("/home/test/docs")
	state.Click(hitSFTPRefresh)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	if remote.readCount("/home/test/docs") != before+1 {
		t.Fatal("refresh did not list the folder again")
	}
}

// A folder that changed on the server shows its new contents once the quiet
// re-read arrives, without losing the selection.
func TestSFTPRememberedFolderIsCorrectedByItsReread(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	docs := sftpRowIndex(t, view, "docs")
	enter := func() {
		state.Pointer(ui.PointerEvent{
			Kind: ui.PointerDown, Button: ui.PointerButtonPrimary, ClickCount: 2,
			Target: contextID(hitSFTPEntryList.At(docs - view.first)),
		})
	}
	enter()
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test/docs" })
	state.Click(hitSFTPUp)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })

	remote.mu.Lock()
	remote.entries["/home/test/docs"] = append(remote.entries["/home/test/docs"],
		workbenchFileInfo{name: "new.md", size: 3, mode: 0o644, when: time.Unix(1_700_000_300, 0)})
	remote.mu.Unlock()

	enter()
	if len(view.rows) != 1 {
		t.Fatalf("the remembered folder shows %d rows, want the earlier 1", len(view.rows))
	}
	waitSFTP(t, state, session, func(view *sftpView) bool { return len(view.rows) == 2 })
	if sftpRowIndex(t, view, "new.md") < 0 {
		t.Fatal("the new file never appeared")
	}
}

func TestSFTPLinksFillInAfterTheListingIsShown(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	remote.mu.Lock()
	remote.linkDelay = 40 * time.Millisecond
	remote.mu.Unlock()
	if err := remote.Symlink("notes.txt", "/home/test/latest"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitSFTPRefresh)
	// The listing arrives first, with the link unresolved.
	waitSFTP(t, state, session, func(view *sftpView) bool {
		for _, row := range view.rows {
			if row.entry.Name == "latest" {
				return row.entry.LinkTarget == ""
			}
		}
		return false
	})
	// Its resolution follows and updates the row in place.
	waitSFTP(t, state, session, func(view *sftpView) bool {
		for _, row := range view.rows {
			if row.entry.Name == "latest" {
				return row.entry.LinkTarget == "notes.txt" && !row.entry.LinkDir
			}
		}
		return false
	})
}

func TestSFTPListingCacheHasAByteBudget(t *testing.T) {
	view := &sftpView{listingLimitBytes: 1000}
	listing := func(name string) []sftpclient.Entry {
		return []sftpclient.Entry{{Name: strings.Repeat(name, 300)}}
	}
	view.remember("/old", listing("a"))
	view.remember("/new", listing("b"))
	view.remember("/newest", listing("c"))
	if _, ok := view.listing("/old"); ok {
		t.Fatal("the oldest listing was not evicted")
	}
	if _, ok := view.listing("/new"); !ok {
		t.Fatal("the second listing was evicted before the budget required it")
	}
	if _, ok := view.listing("/newest"); !ok {
		t.Fatal("the newest listing is missing")
	}
	if view.listingBytes > view.listingLimitBytes {
		t.Fatalf("listing bytes = %d, limit %d", view.listingBytes, view.listingLimitBytes)
	}
	before := view.listingBytes
	view.remember("/huge", []sftpclient.Entry{{Name: strings.Repeat("x", 2000)}})
	if _, ok := view.listing("/huge"); ok {
		t.Fatal("a listing larger than the budget was cached")
	}
	if view.listingBytes != before {
		t.Fatalf("the refused listing changed the budget: %d -> %d", before, view.listingBytes)
	}
	view.forgetListings()
	if view.listingBytes != 0 || len(view.listings) != 0 {
		t.Fatalf("forget left %d listings and %d bytes", len(view.listings), view.listingBytes)
	}
}

func TestALargeRemoteListingIsPreparedInTheBackground(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	remote.mu.Lock()
	for index := 0; index < sftpPrepInlineEntries+88; index++ {
		remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{
			name: fmt.Sprintf("bulk-%05d.txt", index), size: 1, mode: 0o644, when: time.Unix(1_700_000_000, 0),
		})
	}
	remote.mu.Unlock()
	state.Click(hitSFTPRefresh)
	want := 2 + sftpPrepInlineEntries + 88
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && len(view.rows) == want })
	if got := viewRowNamed(t, session.files, "bulk-00000.txt"); got != "bulk-00000.txt" {
		t.Fatalf("row = %q", got)
	}
}

func viewRowNamed(t *testing.T, view *sftpView, name string) string {
	t.Helper()
	for _, row := range view.rows {
		if row.entry.Name == name {
			return row.entry.Name
		}
	}
	t.Fatalf("no row %q in %d rows", name, len(view.rows))
	return ""
}

func TestAnOvertakenRemoteListingIsDropped(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	remote.mu.Lock()
	for index := 0; index < sftpPrepInlineEntries+88; index++ {
		remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{
			name: fmt.Sprintf("bulk-%05d.txt", index), size: 1, mode: 0o644, when: time.Unix(1_700_000_000, 0),
		})
	}
	remote.mu.Unlock()
	view := session.files
	state.Click(hitSFTPRefresh)
	// Wait for the large listing's result to arrive, so the panel is no longer
	// busy and its docs row can be opened while the rows are still being
	// prepared in the background.
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy })
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPOpenEntry)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return view.path == "/home/test/docs" && len(view.rows) == 1
	})
	deadline := time.Now().Add(300 * time.Millisecond)
	for time.Now().Before(deadline) {
		state.Wake()
		time.Sleep(time.Millisecond)
		if view.path != "/home/test/docs" || len(view.rows) != 1 {
			t.Fatalf("the overtaken listing landed: %q with %d rows", view.path, len(view.rows))
		}
	}
}

func TestALargeLinkedListingResolvesAfterItIsShown(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	remote.mu.Lock()
	remote.linkDelay = 10 * time.Millisecond
	for index := 0; index < sftpPrepInlineEntries+88; index++ {
		remote.entries["/home/test"] = append(remote.entries["/home/test"], workbenchFileInfo{
			name: fmt.Sprintf("bulk-%05d.txt", index), size: 1, mode: 0o644, when: time.Unix(1_700_000_000, 0),
		})
	}
	remote.mu.Unlock()
	if err := remote.Symlink("notes.txt", "/home/test/latest"); err != nil {
		t.Fatal(err)
	}
	state.Click(hitSFTPRefresh)
	want := 3 + sftpPrepInlineEntries + 88
	waitSFTP(t, state, session, func(view *sftpView) bool {
		if len(view.rows) != want {
			return false
		}
		for _, row := range view.rows {
			if row.entry.Name == "latest" {
				return row.entry.LinkTarget == "notes.txt" && !row.entry.LinkDir
			}
		}
		return false
	})
}
