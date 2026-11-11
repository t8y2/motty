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
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
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
	removeErr    error             // refuses every removal while set
	removeErrAt  map[string]error  // refuses a particular path
	unreadable   map[string]bool   // folders whose listing is refused
	onRead       func(dir string)  // runs before a folder is listed, outside the lock
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
	delay, onRead := remote.readDelay, remote.onRead
	remote.mu.Unlock()
	if onRead != nil {
		onRead(path)
	}
	if delay > 0 {
		time.Sleep(delay) // a network round trip
	}
	remote.mu.Lock()
	defer remote.mu.Unlock()
	if remote.unreadable[path] {
		return nil, os.ErrPermission
	}
	// A listing is a snapshot: the caller may delete from the folder while it
	// reads what the folder held, as it does when removing a tree.
	return append([]os.FileInfo(nil), remote.entries[path]...), nil
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
	if err := remote.removeErrAt[remotePath]; err != nil {
		return err
	}
	if remote.removeErr != nil {
		return remote.removeErr
	}
	if _, ok := remote.links[remotePath]; ok {
		// A link is unlinked; whatever it points at is left alone.
		delete(remote.links, remotePath)
		remote.removeEntryLocked(path.Dir(remotePath), path.Base(remotePath))
		return nil
	}
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
	if err := remote.removeErrAt[remotePath]; err != nil {
		return err
	}
	if remote.removeErr != nil {
		return remote.removeErr
	}
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
	deadline := time.Now().Add(waitBudget())
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
	t.Fatalf("SFTP state did not settle: path=%q rows=%d busy=%t operation=%v status=%q error=%q waiting=%q prefetching=%d",
		session.files.path, len(session.files.rows), session.files.busy, session.files.operation,
		session.files.status, session.files.errorText, session.files.waiting, len(session.files.prefetching))
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

func clickSFTPRow(t testing.TB, state *State, view *sftpView, name string) gfx.ID {
	t.Helper()
	index := sftpRowIndex(t, view, name)
	id := hitSFTPEntryList.At(index - view.first)
	state.Click(id)
	return id
}

// sftpListRowID is the window identity of a painted row of the folder's Viva
// List. The List names its rows itself, so a test that finds one in a painted
// snapshot asks the last paint for it.
func sftpListRowID(t testing.TB, folder *sftpFolder, index int) gfx.ID {
	t.Helper()
	if folder.listRowsScope.IsZero() {
		t.Fatal("the file list has not been painted")
	}
	return contextID(folder.listRowsScope.At(index))
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

// sftpPageButtonHit finds the page control with this accessible label.
func sftpPageButtonHit(canvas *gfx.Canvas, label string) (gfx.Hit, bool) {
	for _, hit := range canvas.Hits() {
		if hit.Label == label && hit.Role == gfx.RoleButton {
			return hit, true
		}
	}
	return gfx.Hit{}, false
}

// clickSFTPPageButton paints the window and delivers a click's event pass to
// the files page control with this label, which acts while it is declared.
func clickSFTPPageButton(t testing.TB, state *State, label string) {
	t.Helper()
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	hit, ok := sftpPageButtonHit(&canvas, label)
	if !ok {
		t.Fatalf("no files control labeled %q", label)
	}
	if hit.Disabled || hit.Block {
		t.Fatalf("files control %q is not actionable (disabled=%v blocked=%v)", label, hit.Disabled, hit.Block)
	}
	var pass gfx.Canvas
	pass.BeginEventPass(hit.ID, gfx.ClickInfo{})
	state.Paint(&pass, size)
	pass.EndEventPass()
}

// clickSFTPWindowButton is clickSFTPPageButton for a file window's own canvas.
func clickSFTPWindowButton(t testing.TB, w *sftpWindow, label string) {
	t.Helper()
	size := gfx.Size{Width: 640, Height: 400}
	var canvas gfx.Canvas
	w.paint(&canvas, size)
	hit, ok := sftpPageButtonHit(&canvas, label)
	if !ok {
		t.Fatalf("no window control labeled %q", label)
	}
	var pass gfx.Canvas
	pass.BeginEventPass(hit.ID, gfx.ClickInfo{})
	w.paint(&pass, size)
	pass.EndEventPass()
}

func TestSFTPBrowseAndDownload(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	state.sftpDownloadDirectory = "" // exercise the ask-each-time picker path
	if view.path != "/home/test" || view.rows[0].entry.Name != "docs" || view.rows[1].entry.Name != "notes.txt" {
		t.Fatalf("initial listing = %q %+v", view.path, view.rows)
	}

	rowID := clickSFTPRow(t, state, view, "docs")
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: rowID}); !result.Handled || !result.Repaint {
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
	canvas, _ := paintAt(state, gfx.Size{Width: 1400, Height: 900})
	box := panelHit(t, canvas, contextID(hitSFTPEditor))
	rows := 0
	for _, text := range canvas.Texts() {
		if text.Bounds.Y > box.Bounds.Y && text.Bounds.Y < box.Bounds.Y+box.Bounds.Height && text.Bounds.X >= box.Bounds.X && text.Bounds.X < box.Bounds.X+box.Bounds.Width {
			rows++
			if got := canvas.TextWidthWeight(text.Value, 13, gfx.WeightRegular); got > box.Bounds.Width {
				t.Errorf("row %q is %v wide in an editor %v wide", text.Value, got, box.Bounds.Width)
			}
		}
	}
	if rows < 3 {
		t.Fatalf("the file's long line took %d rows", rows)
	}
	end := view.editEditor.Caret()
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyUp, Target: contextID(hitSFTPEditor)}); !result.Handled || view.editEditor.Caret() >= end {
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
	if !sftpEditorDirty(view) {
		t.Fatal("typing did not make the editor dirty")
	}
	clickSFTPPageButton(t, state, state.messages.SFTPEditSave)
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

	clickSFTPPageButton(t, state, state.messages.SFTPEditBack)
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

	clickSFTPPageButton(t, state, state.messages.SFTPEditSave)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.dialog == sftpDialogRemoteChanged })
	if view.editEditor.String() != "my local edits\n" || view.errorText != state.messages.SFTPEditConflict {
		t.Fatalf("conflict editor = %q error = %q", view.editEditor.String(), view.errorText)
	}
	state.Click(hitSFTPDialogCancel)
	if view.dialog != sftpDialogNone || view.editEditor.String() != "my local edits\n" {
		t.Fatalf("keeping edits changed state: %+v", view)
	}

	clickSFTPPageButton(t, state, state.messages.SFTPEditReload)
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
	clickSFTPPageButton(t, state, state.messages.SFTPEditBack)
	if view.dialog != sftpDialogDiscardEdit || view.editPath == "" {
		t.Fatalf("discard confirmation = %+v", view)
	}
	state.Click(hitSFTPDialogCancel)
	if view.editEditor.String() != "unsaved" {
		t.Fatalf("cancel discard lost edits: %q", view.editEditor.String())
	}
	clickSFTPPageButton(t, state, state.messages.SFTPEditBack)
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
	state.Paint(&canvas, size)
	if _, ok := canvas.NextFrame(); ok {
		t.Fatal("idle SFTP editor requested another frame")
	}
	if _, ok := hitByID(&canvas, contextID(hitSFTPEditor)); !ok {
		t.Fatal("missing editor hit")
	}
	for _, label := range []string{state.messages.SFTPEditSave, state.messages.SFTPEditBack, state.messages.SFTPEditReload} {
		if _, ok := sftpPageButtonHit(&canvas, label); !ok {
			t.Fatalf("missing editor control %q", label)
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
	state.sftpDownloadDirectory = "" // exercise the ask-each-time picker path
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
	state.sftpDownloadDirectory = "" // exercise the ask-each-time picker path
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
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: hitSFTPDialogField}); !result.Handled {
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
	if view.dialog != sftpDialogRename || view.dialogEditor.String() != "notes.txt" {
		t.Fatalf("rename dialog = %+v", view)
	}
	replaceSFTPDialogText(t, state, "docs/renamed.txt")
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogRename || view.dialogError != state.messages.SFTPErrRenameInvalid {
		t.Fatalf("rename accepted a path: dialog=%d error=%q", view.dialog, view.dialogError)
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

	// The permission grid mirrors the octal field in both directions
	// (issue #392): nine boxes, a tap flips one bit, and a typed value moves
	// the boxes.
	clickSFTPRow(t, state, view, "renamed.txt")
	state.Click(hitSFTPChmod)
	if view.dialogEditor.String() != "600" {
		t.Fatalf("chmod reopen value = %q", view.dialogEditor.String())
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	var boxes [9]gfx.Rect
	for index := 0; index < 9; index++ {
		hit, ok := hitByID(&canvas, hitSFTPChmodBits.At(index))
		if !ok {
			t.Fatalf("permission checkbox %d is not painted", index)
		}
		// 600 checks owner read and write only: the boxes are drawn from the
		// field, not from a state of their own.
		if want := index < 2; hit.Checked != want {
			t.Fatalf("permission checkbox %d checked = %v, want %v for 600", index, hit.Checked, want)
		}
		boxes[index] = hit.Bounds
	}
	// Tapping the gap between two boxes must not reach a neighbour.
	for first := 0; first < 9; first++ {
		for second := first + 1; second < 9; second++ {
			a, b := boxes[first], boxes[second]
			if a.X < b.X+b.Width && b.X < a.X+a.Width && a.Y < b.Y+b.Height && b.Y < a.Y+a.Height {
				t.Fatalf("permission checkboxes %d and %d overlap: %+v %+v", first, second, a, b)
			}
		}
	}
	// An empty field is not a mode: every box stands down instead of showing
	// stale bits.
	replaceSFTPDialogText(t, state, "")
	canvas = gfx.Canvas{}
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for index := 0; index < 9; index++ {
		hit, ok := hitByID(&canvas, hitSFTPChmodBits.At(index))
		if !ok || !hit.Disabled {
			t.Fatalf("permission checkbox %d enabled with an empty field: %+v %t", index, hit, ok)
		}
	}
	replaceSFTPDialogText(t, state, "600")
	// 600: owner rw. Tapping group write (index 4) sets that one bit: 620.
	state.Click(hitSFTPChmodBits.At(4))
	if view.dialogEditor.String() != "620" {
		t.Fatalf("after group-write tap = %q, want 620", view.dialogEditor.String())
	}
	// Tapping it again takes the bit back.
	state.Click(hitSFTPChmodBits.At(4))
	if view.dialogEditor.String() != "600" {
		t.Fatalf("after second tap = %q, want 600", view.dialogEditor.String())
	}
	// Typing in the field moves the boxes: 700 has owner execute; tapping it
	// clears the bit back to 600.
	replaceSFTPDialogText(t, state, "700")
	state.Click(hitSFTPChmodBits.At(2))
	if view.dialogEditor.String() != "600" {
		t.Fatalf("after owner-exec tap on 700 = %q, want 600", view.dialogEditor.String())
	}
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		index := sftpRowIndexOrNegative(view, "renamed.txt")
		return !view.busy && index >= 0 && view.rows[index].entry.Mode.Perm() == 0o600
	})
	clickSFTPRow(t, state, view, "renamed.txt")
	state.Click(hitSFTPMove)
	if view.dialog != sftpDialogMove || view.dialogEditor.String() != "/home/test" {
		t.Fatalf("move dialog = dialog %d, value %q", view.dialog, view.dialogEditor.String())
	}
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogMove || view.dialogError != state.messages.SFTPErrMoveSame {
		t.Fatalf("move to the current folder was accepted: dialog=%d error=%q", view.dialog, view.dialogError)
	}
	replaceSFTPDialogText(t, state, "docs")
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "renamed.txt") < 0
	})
	remote.mu.Lock()
	_, moved := remote.files["/home/test/docs/renamed.txt"]
	_, oldLocation := remote.files["/home/test/renamed.txt"]
	remote.mu.Unlock()
	if !moved || oldLocation {
		t.Fatalf("move state: moved=%v old=%v", moved, oldLocation)
	}
}

func TestRemoteRenameAcceptsOnlyBaseNames(t *testing.T) {
	for _, value := range []string{"renamed.txt", "配置.yaml", ".env", "with space"} {
		if !validRemoteBaseName(value) {
			t.Errorf("valid base name %q was rejected", value)
		}
	}
	for _, value := range []string{"", ".", "..", "folder/file", `folder\file`, "nul\x00name"} {
		if validRemoteBaseName(value) {
			t.Errorf("path-like name %q was accepted", value)
		}
	}
}

func TestRemoteMoveRejectsItsOwnDirectory(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPMove)
	replaceSFTPDialogText(t, state, "docs")
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogMove || view.dialogError != state.messages.SFTPErrMoveInside {
		t.Fatalf("moving a folder into itself = dialog %d, error %q", view.dialog, view.dialogError)
	}
	replaceSFTPDialogText(t, state, "/home/test/docs/deep")
	state.Click(hitSFTPDialogSubmit)
	if view.dialog != sftpDialogMove || view.dialogError != state.messages.SFTPErrMoveInside {
		t.Fatalf("moving a folder into its descendant = dialog %d, error %q", view.dialog, view.dialogError)
	}
}

func TestSFTPMultiDeleteConfirmsSelectionAndRemovesFilesAndFolders(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")
	clickSFTPRow(t, state, view, "docs")
	if !sftpMenuRowIDs(state.sftpMenuRows(session, view))[hitSFTPMenuDelete] {
		t.Fatal("multi-selection has no delete action")
	}
	state.Click(hitSFTPDelete)
	if view.dialog != sftpDialogDelete || len(view.dialogEntries) != 2 {
		t.Fatalf("delete confirmation lost the selection: dialog=%v entries=%v", view.dialog, view.dialogEntries)
	}
	state.Click(hitSFTPDialogCancel)
	remote.mu.Lock()
	_, untouched := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !untouched || view.dialog != sftpDialogNone {
		t.Fatal("canceling the multi-delete changed files")
	}
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "notes.txt") < 0 && sftpRowIndexOrNegative(view, "docs") < 0
	})
	remote.mu.Lock()
	_, fileLeft := remote.files["/home/test/notes.txt"]
	_, childLeft := remote.files["/home/test/docs/guide.md"]
	remote.mu.Unlock()
	if fileLeft || childLeft {
		t.Fatalf("confirmed selection remains: file=%v child=%v", fileLeft, childLeft)
	}
}

func TestSFTPMultiDeleteStopsAfterFailureAndRefreshes(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	remote.mu.Lock()
	remote.removeErrAt = map[string]error{"/home/test/notes.txt": os.ErrPermission}
	remote.mu.Unlock()
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && view.dialog == sftpDialogNone && len(view.deleteRemaining) == 0 && sftpRowIndexOrNegative(view, "notes.txt") >= 0
	})
	remote.mu.Lock()
	_, untouched := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !untouched || sftpRowIndexOrNegative(view, "docs") >= 0 || len(state.fileNotices.items) == 0 ||
		!strings.Contains(state.fileNotices.items[0].detail, "/home/test/notes.txt") {
		t.Fatal("failure deleted another selected item or lacked partial-failure notice")
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
	var dialogCanvas gfx.Canvas
	state.Paint(&dialogCanvas, gfx.Size{Width: 1400, Height: 900})
	if dialogCanvas.InitialFocus() != hitSFTPDialogCancel || dialogCanvas.TrappedFocusScope() != dialogScope {
		t.Fatalf("delete dialog focus=%d scope=%d", dialogCanvas.InitialFocus(), dialogCanvas.TrappedFocusScope())
	}
	remote.mu.Lock()
	_, existsBeforeConfirm := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if !existsBeforeConfirm {
		t.Fatal("one click deleted the file")
	}
	if result := state.Key(gfx.KeyEvent{Key: gfx.KeyEscape}); !result.Handled || view.dialog != sftpDialogNone {
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
	if !strings.Contains(view.errorText, state.messages.SFTPErrDirectoryExists) {
		t.Fatalf("create error = %q, want containing %q", view.errorText, state.messages.SFTPErrDirectoryExists)
	}

	// A removal the server refuses stays visible instead of passing for done.
	remote.mu.Lock()
	remote.removeErr = os.ErrPermission
	remote.mu.Unlock()
	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.errorText != "" })
	remote.mu.Lock()
	_, stillExists := remote.entries["/home/test/docs"]
	remote.mu.Unlock()
	if !stillExists || view.errorText == "" {
		t.Fatalf("refused delete: exists=%v error=%q", stillExists, view.errorText)
	}
	// The walk may have deleted part of the folder before it was refused: say so
	// in a notice that is not cut off, and drop what was remembered of it.
	if len(state.fileNotices.items) != 1 || state.fileNotices.items[0].kind != components.ToastError ||
		!strings.Contains(state.fileNotices.items[0].detail, state.messages.SFTPDeletePartialHint) || len(view.listings) != 0 {
		t.Fatalf("refused delete: notices=%+v listings=%d", state.fileNotices.items, len(view.listings))
	}
}

// A folder is deleted with everything in it: SFTP's own remove refuses a folder
// that is not empty, so the panel walks the tree. What a link inside points at is
// left alone.
func TestSFTPDeletingAFolderTakesItsContents(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	remote.mu.Lock()
	remote.entries["/home/test/docs"] = append(remote.entries["/home/test/docs"],
		workbenchFileInfo{name: "deep", mode: os.ModeDir | 0o755, when: time.Unix(1_700_000_400, 0)},
		workbenchFileInfo{name: "outside", mode: os.ModeSymlink | 0o777, when: time.Unix(1_700_000_600, 0)},
	)
	remote.entries["/home/test/docs/deep"] = []os.FileInfo{
		workbenchFileInfo{name: "inner.txt", size: 3, mode: 0o644, when: time.Unix(1_700_000_500, 0)},
	}
	remote.files["/home/test/docs/deep/inner.txt"] = []byte("in\n")
	if remote.links == nil {
		remote.links = map[string]string{}
	}
	remote.links["/home/test/docs/outside"] = "/home/test/notes.txt"
	remote.mu.Unlock()

	clickSFTPRow(t, state, view, "docs")
	state.Click(hitSFTPDelete)
	if view.dialog != sftpDialogDelete || !view.dialogEntry.IsDir() {
		t.Fatalf("folder delete dialog = %+v", view)
	}
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	// The description wraps, so the painted lines are read back together.
	want := fmt.Sprintf(state.messages.SFTPDeleteFolderContentsFmt, "docs")
	var said strings.Builder
	for _, painted := range canvas.Texts() {
		said.WriteString(painted.Value)
		said.WriteString(" ")
	}
	if !strings.Contains(said.String(), want) {
		t.Fatalf("the confirmation does not say the contents go too: %q in %q", want, said.String())
	}

	state.Click(hitSFTPDialogSubmit)
	waitSFTP(t, state, session, func(view *sftpView) bool {
		return !view.busy && sftpRowIndexOrNegative(view, "docs") < 0
	})
	remote.mu.Lock()
	_, folder := remote.entries["/home/test/docs"]
	_, nested := remote.entries["/home/test/docs/deep"]
	_, inner := remote.files["/home/test/docs/deep/inner.txt"]
	_, link := remote.links["/home/test/docs/outside"]
	notes := remote.files["/home/test/notes.txt"]
	remote.mu.Unlock()
	if folder || nested || inner || link {
		t.Fatalf("the folder's contents remain: folder=%v nested=%v file=%v link=%v", folder, nested, inner, link)
	}
	if len(notes) == 0 {
		t.Fatal("the link's target was deleted with the folder")
	}
}

func TestSFTPWorkerStartsOnlyWhenFilesPageOpens(t *testing.T) {
	state, _, fake, _ := liveSession(t)
	state.Paint(&gfx.Canvas{}, gfx.Size{Width: 1400, Height: 900})
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
	state.sftpDownloadDirectory = "" // exercise the ask-each-time picker path
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	var canvas gfx.Canvas
	size := gfx.Size{Width: 1400, Height: 900}
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
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	if view.visibleRows <= 0 || view.visibleRows >= len(view.rows) {
		t.Fatalf("visible rows = %d of %d", view.visibleRows, len(view.rows))
	}
	panel := state.contextRect
	if panel.Width == 0 {
		t.Fatal("the paint reported no files panel")
	}

	// Over empty space of the panel the wheel event names no control; over a
	// row or a button it names that control. Both must reach the list. The List
	// scrolls by pixels, so a sub-row delta moves the view and the first row is
	// the row the offset falls in.
	var over gfx.ID
	scroll := func(x, delta float32) {
		state.Scroll(gfx.ScrollEvent{X: x, Y: panel.Y + 100, DeltaY: delta, Precise: true, Viewport: size, Target: over})
	}
	scroll(panel.X+panel.Width/2, 3*sftpRowHeight)
	if view.first != 3 {
		t.Fatalf("a positive wheel moved the first row to %d, want 3", view.first)
	}
	scroll(panel.X+panel.Width/2, -sftpRowHeight)
	if view.first != 2 {
		t.Fatalf("a negative wheel moved the first row to %d, want 2", view.first)
	}
	before := view.scrollOffset
	scroll(panel.X+panel.Width/2, sftpRowHeight/3)
	if view.first != 2 || view.scrollOffset != before+sftpRowHeight/3 {
		t.Fatalf("a third of a row did not move the pixel offset: %f -> %f", before, view.scrollOffset)
	}
	scroll(panel.X+panel.Width/2, 2*sftpRowHeight/3+1)
	if view.first != 3 {
		t.Fatalf("the rest of a row did not reach row 3: first = %d", view.first)
	}
	over = contextID(hitSFTPEntryList.At(0)) // over a row of the list
	before = view.scrollOffset
	scroll(panel.X+panel.Width/2, -sftpRowHeight)
	if view.first != 2 || view.scrollOffset != before-sftpRowHeight {
		t.Fatalf("a wheel over a row moved the list to first=%d offset=%f", view.first, view.scrollOffset)
	}
	over = gfx.ID{}
	before = view.scrollOffset
	scroll(panel.X+panel.Width+200, 5*sftpRowHeight) // over the terminal
	if view.scrollOffset != before {
		t.Fatalf("a wheel over the terminal moved the file list: %f -> %f", before, view.scrollOffset)
	}
	scroll(panel.X+panel.Width/2, 1000*sftpRowHeight)
	state.Paint(&canvas, size)
	if view.first != int(view.scrollOffset/sftpRowHeight) || view.scrollOffset != view.scroll.MaxOffsetY {
		t.Fatalf("scrolling far down left offset %f first %d, max %f", view.scrollOffset, view.first, view.scroll.MaxOffsetY)
	}

	// Scrolling far down and painting anchors the list at the bottom. Further
	// downward wheels must not repaint or jitter the offset.
	state.Paint(&canvas, size)
	bottomOffset := view.scrollOffset
	bottomFirst := view.first
	if bottomOffset == float32(bottomFirst)*sftpRowHeight {
		// The jitter lives in the remainder between the pixel offset and the row
		// grid; a row-aligned fixture could not fail, so it must not pass silently.
		t.Fatalf("the fixture's bottom offset %f is row aligned", bottomOffset)
	}
	for i := 0; i < 10; i++ {
		res := state.Scroll(gfx.ScrollEvent{X: panel.X + panel.Width/2, Y: panel.Y + 100, DeltaY: sftpRowHeight, Viewport: size})
		if res.Repaint {
			t.Fatalf("scroll at bottom requested repaint on iteration %d", i)
		}
		state.Paint(&canvas, size)
		if view.scrollOffset != bottomOffset || view.first != bottomFirst {
			t.Fatalf("scroll at bottom jittered: offset %f -> %f, first %d -> %d", bottomOffset, view.scrollOffset, bottomFirst, view.first)
		}
	}

	// Scrolling upward from the bottom must move and repaint smoothly.
	upRes := state.Scroll(gfx.ScrollEvent{X: panel.X + panel.Width/2, Y: panel.Y + 100, DeltaY: -sftpRowHeight, Viewport: size})
	if !upRes.Repaint {
		t.Fatal("scrolling upward from bottom did not request repaint")
	}
	if view.first >= bottomFirst && view.scrollOffset >= bottomOffset {
		t.Fatalf("scrolling upward from bottom did not decrease position: offset=%f first=%d", view.scrollOffset, view.first)
	}
}

// A drag released inside a row keeps the offset it left, and scrolling up from
// there returns the list to its top instead of stalling on whole rows.
func TestSFTPDragReleaseKeepsASubRowOffsetAndWheelUpReturnsToTheTop(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	entries := make([]sftpclient.Entry, 100)
	for index := range entries {
		entries[index] = sftpclient.Entry{Name: fmt.Sprintf("file-%03d.txt", index), Mode: 0o644}
	}
	view.rows = prepareSFTPRows(view.rows, entries)
	view.first = 0
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	panel := state.contextRect
	if panel.Width == 0 {
		t.Fatal("the paint reported no files panel")
	}
	folder := view.active()

	// As a scrollbar drag would leave it: released 17 pixels into the first row,
	// past the half-row mark that used to round the row up and snap back.
	folder.listScroll.ScrollTo(17)
	folder.scrollOffset = 17
	view.scrollDrag = true
	if result, handled := state.sftpListPointer(gfx.PointerEvent{Kind: gfx.PointerUp, Button: gfx.PointerButtonPrimary, Captured: true}); !handled || !result.Repaint {
		t.Fatal("releasing the scrollbar drag was not handled")
	}
	if folder.first != 0 {
		t.Fatalf("release 17 pixels into the first row put the first row at %d, want 0", folder.first)
	}
	state.Paint(&canvas, size)
	if folder.scrollOffset != 17 {
		t.Fatalf("release over a partial row snapped the offset to %f, want 17", folder.scrollOffset)
	}

	// Scrolling up from that offset must show the top, not stall on row 0.
	up := state.Scroll(gfx.ScrollEvent{X: panel.X + panel.Width/2, Y: panel.Y + 100, DeltaY: -sftpRowHeight, Viewport: size})
	if !up.Repaint {
		t.Fatal("scrolling up from a sub-row offset did not request repaint")
	}
	state.Paint(&canvas, size)
	if folder.scrollOffset != 0 || folder.first != 0 {
		t.Fatalf("scrolling up left the list at offset %f, first %d, want 0/0", folder.scrollOffset, folder.first)
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
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1400, Height: 900})
	for _, id := range []gfx.ID{hitPaneSplitHorizontal, hitPaneSplitVertical} {
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
		state.Pointer(gfx.PointerEvent{
			Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: clicks,
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
	for _, completed := range []bool{false, true} {
		name := "prefetch_in_flight"
		if completed {
			name = "prefetch_completed"
		}
		t.Run(name, func(t *testing.T) {
			const directory = "/home/test/docs"
			state, session, _, remote := openTestSFTP(t)
			view := session.files
			reads := make(chan chan struct{}, 1)
			stop := make(chan struct{})
			// Registered after openTestSFTP: release every blocked read before
			// its cleanup closes the worker, including when an assertion fails.
			t.Cleanup(func() { close(stop) })
			remote.mu.Lock()
			remote.onRead = func(dir string) {
				if dir != directory {
					return
				}
				release := make(chan struct{})
				select {
				case reads <- release:
				case <-stop:
					return
				}
				select {
				case <-release:
				case <-stop:
				}
			}
			remote.mu.Unlock()
			nextRead := func() chan struct{} {
				t.Helper()
				var release chan struct{}
				waitSFTP(t, state, session, func(*sftpView) bool {
					select {
					case release = <-reads:
						return true
					default:
						return false
					}
				})
				return release
			}
			assertShown := func() {
				t.Helper()
				if view.path != directory || view.busy || view.waiting != "" {
					t.Fatalf("folder not shown: path=%q busy=%v waiting=%q", view.path, view.busy, view.waiting)
				}
				if len(view.rows) != 1 || view.rows[0].entry.Name != "guide.md" {
					t.Fatalf("folder rows = %+v, want guide.md", view.rows)
				}
			}
			waitAccepted := func(id uint64) {
				t.Helper()
				waitSFTP(t, state, session, func(view *sftpView) bool {
					_, inFlight := view.prefetching[directory]
					return !inFlight && view.acceptedListID == id && !view.busy && view.path == directory
				})
				assertShown()
			}
			docs := sftpRowIndex(t, view, "docs")
			target := contextID(hitSFTPEntryList.At(docs - view.first))
			press := func(clicks int) {
				// As the runtime delivers a click: the press, then the click on release.
				result := state.Pointer(gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: clicks, Target: target})
				if !result.Handled {
					state.Click(target)
				}
			}

			press(1)
			firstRead := nextRead()
			firstID := view.prefetching[directory]
			if firstID == 0 || view.path != "/home/test" || view.busy || remote.readCount(directory) != 1 {
				t.Fatalf("single click did not prefetch without entering: id=%d path=%q busy=%v reads=%d", firstID, view.path, view.busy, remote.readCount(directory))
			}
			if completed {
				close(firstRead)
				waitSFTP(t, state, session, func(view *sftpView) bool {
					_, inFlight := view.prefetching[directory]
					_, remembered := view.listing(directory)
					return !inFlight && remembered
				})
				press(2)
				assertShown() // The completed prefetch is shown before the new read returns.
				reread := nextRead()
				id := view.prefetching[directory]
				assertShown()
				if remote.readCount(directory) != 2 {
					t.Fatal("entering the prefetched folder did not start exactly one background reread")
				}
				close(reread)
				waitAccepted(id)
			} else {
				press(2)
				if !view.busy || view.waiting != directory || view.waitingID != firstID || view.prefetching[directory] != firstID {
					t.Fatalf("double-click did not join the first read: busy=%v waiting=%q id=%d, want %d", view.busy, view.waiting, view.waitingID, firstID)
				}
				close(firstRead)
				waitAccepted(firstID)
				if remote.readCount(directory) != 1 {
					t.Fatal("double-click listed the folder again instead of joining the first read")
				}
			}

			// A remembered folder is shown while its background reread is held.
			state.Click(hitSFTPUp)
			waitSFTP(t, state, session, func(view *sftpView) bool { return !view.busy && view.path == "/home/test" })
			before := remote.readCount(directory)
			press(2)
			assertShown()
			reread := nextRead()
			rereadID := view.prefetching[directory]
			assertShown()
			if remote.readCount(directory) != before+1 {
				t.Fatal("entering a remembered folder did not start exactly one background reread")
			}
			close(reread)
			waitAccepted(rereadID)

			// Refresh waits for a new server result instead of using the cache.
			before = remote.readCount(directory)
			state.Click(hitSFTPRefresh)
			if !view.busy {
				t.Fatal("refresh did not wait for a new listing")
			}
			refreshID := view.navigation.id
			refresh := nextRead()
			if !view.busy || remote.readCount(directory) != before+1 {
				t.Fatal("refresh did not start exactly one new listing")
			}
			close(refresh)
			waitAccepted(refreshID)
			if remote.readCount(directory) != before+1 {
				t.Fatal("refresh listed the folder more than once")
			}
		})
	}
}

// A folder that changed on the server shows its new contents once the quiet
// re-read arrives, without losing the selection.
func TestSFTPRememberedFolderIsCorrectedByItsReread(t *testing.T) {
	state, session, _, remote := openTestSFTP(t)
	view := session.files
	docs := sftpRowIndex(t, view, "docs")
	enter := func() {
		state.Pointer(gfx.PointerEvent{
			Kind: gfx.PointerDown, Button: gfx.PointerButtonPrimary, ClickCount: 2,
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

// The terminal pane is painted shifted by the files panel, so a wheel over its
// left part lands on window coordinates that are also inside the panel's
// width. That wheel is the terminal's: the panel only owns what is over it.
func TestTheWheelOverTheTerminalBesideTheFilesPanelScrollsTheTerminal(t *testing.T) {
	state, session, fake, _ := openTestSFTP(t)
	shell := fake.shell(t)
	var output strings.Builder
	for line := 1; line <= 200; line++ {
		output.WriteString("line\r\n")
	}
	shell.say(output.String())
	state.Wake()
	size := gfx.Size{Width: 1400, Height: 900}
	var canvas gfx.Canvas
	state.Paint(&canvas, size)
	panel := state.contextRect
	if panel.Width == 0 {
		t.Fatal("the paint reported no files panel")
	}
	rows := len(session.files.rows)
	first := session.files.first
	over := pagedIdentity(state, session, terminalTestHit(state, session))
	x, y := panel.X+panel.Width+panel.Width/2, panel.Y+200
	result := state.Scroll(gfx.ScrollEvent{X: x, Y: y, DeltaY: -3 * state.cellHeight, Precise: true, Viewport: size, Target: over})
	if got := session.term.screen.ViewOffset(); got != 3 || !result.Repaint {
		t.Fatalf("a wheel over the terminal beside the panel: offset %d, %+v", got, result)
	}
	if session.files.first != first || len(session.files.rows) != rows {
		t.Fatal("the wheel over the terminal moved the file list")
	}
}

// TestSFTPChmodGridTransitionsThroughTheEventPass drives the permission grid
// through the driver's real pointer event pass, not the test-only Click: the
// dialog body is a page now, and its hand-painted checkbox hits must still
// reach the central dispatcher and flip one bit of the octal field.
func TestSFTPChmodGridTransitionsThroughTheEventPass(t *testing.T) {
	state, session, _, _ := openTestSFTP(t)
	view := session.files
	clickSFTPRow(t, state, view, "notes.txt")
	state.Click(hitSFTPChmod)
	if view.dialog != sftpDialogChmod || view.dialogEditor.String() != "644" {
		t.Fatalf("chmod dialog = %d, value %q", view.dialog, view.dialogEditor.String())
	}
	driver := modalEventDriver(t, state)
	if _, ok := hitByIDInSnapshot(driver.Snapshot(), hitSFTPChmodBits.At(4)); !ok {
		t.Fatal("the permission grid is not painted")
	}
	// Group write on 644: 664. The click goes through the event pass to the
	// central dispatcher, which flips the bit of the field's value.
	clickPaintedHit(t, driver, hitSFTPChmodBits.At(4))
	if got := view.dialogEditor.String(); got != "664" {
		t.Fatalf("after group-write click = %q, want 664", got)
	}
	if hit, ok := hitByIDInSnapshot(driver.Snapshot(), hitSFTPChmodBits.At(4)); !ok || !hit.Checked {
		t.Fatalf("group-write box after the click = %+v, painted %v", hit, ok)
	}
	clickPaintedHit(t, driver, hitSFTPChmodBits.At(4))
	if got := view.dialogEditor.String(); got != "644" {
		t.Fatalf("after second click = %q, want 644", got)
	}
	// The field still moves the boxes: 700 has owner execute, and a click on
	// that box clears it back to 600.
	replaceSFTPDialogText(t, state, "700")
	clickPaintedHit(t, driver, hitSFTPChmodBits.At(2))
	if got := view.dialogEditor.String(); got != "600" {
		t.Fatalf("after owner-exec click on 700 = %q, want 600", got)
	}
}
