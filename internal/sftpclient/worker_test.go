package sftpclient

import (
	"bytes"
	"errors"
	"io"
	"os"
	"path"
	"path/filepath"
	"slices"
	"sync"
	"testing"
	"time"
)

type fakeInfo struct {
	name string
	size int64
	mode os.FileMode
}

func (info fakeInfo) Name() string       { return info.name }
func (info fakeInfo) Size() int64        { return info.size }
func (info fakeInfo) Mode() os.FileMode  { return info.mode }
func (info fakeInfo) ModTime() time.Time { return time.Unix(1_700_000_000, 0) }
func (info fakeInfo) IsDir() bool        { return info.mode.IsDir() }
func (info fakeInfo) Sys() any           { return nil }

type fakeRemote struct {
	cwd     string
	infos   []os.FileInfo
	files   map[string][]byte
	chunk   int
	listErr error
	closed  chan struct{}
	opened  chan struct{}
	block   bool
	once    sync.Once
}

func (remote *fakeRemote) Getwd() (string, error) { return remote.cwd, nil }
func (remote *fakeRemote) ReadDir(string) ([]os.FileInfo, error) {
	return remote.infos, remote.listErr
}
func (remote *fakeRemote) Open(path string) (io.ReadCloser, error) {
	content, ok := remote.files[path]
	if !ok {
		return nil, os.ErrNotExist
	}
	if remote.block {
		close(remote.opened)
		return blockingReadCloser{closed: remote.closed}, nil
	}
	return io.NopCloser(&chunkReader{content: content, chunk: remote.chunk}), nil
}
func (remote *fakeRemote) Close() error {
	remote.once.Do(func() { close(remote.closed) })
	return nil
}

type chunkReader struct {
	content []byte
	chunk   int
}

type blockingReadCloser struct {
	closed <-chan struct{}
}

func (reader blockingReadCloser) Read([]byte) (int, error) {
	<-reader.closed
	return 0, os.ErrClosed
}

func (blockingReadCloser) Close() error { return nil }

func (reader *chunkReader) Read(destination []byte) (int, error) {
	if len(reader.content) == 0 {
		return 0, io.EOF
	}
	count := min(len(destination), len(reader.content))
	if reader.chunk > 0 {
		count = min(count, reader.chunk)
	}
	copy(destination, reader.content[:count])
	reader.content = reader.content[count:]
	return count, nil
}

func waitResult(t *testing.T, worker *Worker) Result {
	t.Helper()
	select {
	case result := <-worker.Results():
		return result
	case <-time.After(time.Second):
		t.Fatal("worker did not complete")
		return Result{}
	}
}

func TestWorkerListsWorkingDirectoryWithDirectoriesFirst(t *testing.T) {
	remote := &fakeRemote{cwd: "/home/test", closed: make(chan struct{}), infos: []os.FileInfo{
		fakeInfo{name: "z.txt", size: 9},
		fakeInfo{name: "src", mode: os.ModeDir | 0o755},
		fakeInfo{name: "A.txt", size: 3},
	}}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	if !worker.List("") {
		t.Fatal("list was not queued")
	}
	result := waitResult(t, worker)
	if result.Err != nil || result.Path != "/home/test" {
		t.Fatalf("result = %+v", result)
	}
	want := []string{"src", "A.txt", "z.txt"}
	for index, entry := range result.Entries {
		if entry.Name != want[index] {
			t.Fatalf("entries = %+v", result.Entries)
		}
	}
}

func TestWorkerDownloadsEveryChunkBoundary(t *testing.T) {
	content := []byte("alpha\x00beta\ngamma")
	for _, chunk := range []int{1, 2, 3, 7, len(content)} {
		t.Run(string(rune('a'+chunk)), func(t *testing.T) {
			remote := &fakeRemote{closed: make(chan struct{}), files: map[string][]byte{"/remote/data.bin": content}, chunk: chunk}
			worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
			defer worker.Close()
			target := filepath.Join(t.TempDir(), "data.bin")
			if !worker.Download("/remote/data.bin", target) {
				t.Fatal("download was not queued")
			}
			result := waitResult(t, worker)
			if result.Err != nil || result.Bytes != int64(len(content)) {
				t.Fatalf("result = %+v", result)
			}
			got, err := os.ReadFile(target)
			if err != nil || !bytes.Equal(got, content) {
				t.Fatalf("download = %q, %v", got, err)
			}
		})
	}
}

func TestWorkerReportsOpenAndListErrors(t *testing.T) {
	want := errors.New("subsystem unavailable")
	worker := NewWorker(func() (Remote, error) { return nil, want }, nil)
	defer worker.Close()
	worker.List("/")
	if result := waitResult(t, worker); !errors.Is(result.Err, want) {
		t.Fatalf("open error = %v", result.Err)
	}

	remote := &fakeRemote{closed: make(chan struct{}), listErr: os.ErrPermission}
	worker.Close()
	<-worker.Done()
	worker = NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	worker.List("/root")
	if result := waitResult(t, worker); !errors.Is(result.Err, os.ErrPermission) {
		t.Fatalf("list error = %v", result.Err)
	}
}

func TestWorkerCancelInterruptsDownloadAndRemovesPartialFile(t *testing.T) {
	remote := &fakeRemote{
		closed: make(chan struct{}), opened: make(chan struct{}), block: true,
		files: map[string][]byte{"/remote/blocked.bin": []byte("unused")},
	}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()
	target := filepath.Join(t.TempDir(), "blocked.bin")
	if !worker.Download("/remote/blocked.bin", target) {
		t.Fatal("download was not queued")
	}
	select {
	case <-remote.opened:
	case <-time.After(time.Second):
		t.Fatal("download did not start")
	}
	worker.Cancel()
	if result := waitResult(t, worker); !errors.Is(result.Err, ErrCanceled) {
		t.Fatalf("cancel result = %+v", result)
	}
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatalf("partial target remains: %v", err)
	}
}

// treeRemote answers for a fixed tree, so a walk can be followed without a
// network: the folders listed and the paths removed come back in order.
type treeRemote struct {
	*fakeRemote
	dirs    map[string][]os.FileInfo
	links   map[string]string
	listed  []string
	removed []string
}

func (remote *treeRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	remote.listed = append(remote.listed, directory)
	entries, ok := remote.dirs[directory]
	if !ok {
		return nil, os.ErrNotExist
	}
	return entries, nil
}

func (remote *treeRemote) Lstat(name string) (os.FileInfo, error) {
	if _, ok := remote.links[name]; ok {
		return fakeInfo{name: path.Base(name), mode: os.ModeSymlink | 0o777}, nil
	}
	if _, ok := remote.dirs[name]; ok {
		return fakeInfo{name: path.Base(name), mode: os.ModeDir | 0o755}, nil
	}
	for _, entry := range remote.dirs[path.Dir(name)] {
		if entry.Name() == path.Base(name) {
			return entry, nil
		}
	}
	return nil, os.ErrNotExist
}

func (remote *treeRemote) Remove(name string) error {
	remote.removed = append(remote.removed, name)
	return nil
}

func (remote *treeRemote) RemoveDirectory(name string) error {
	delete(remote.dirs, name)
	remote.removed = append(remote.removed, name)
	return nil
}

func newTreeRemote(dirs map[string][]os.FileInfo, links map[string]string) *treeRemote {
	return &treeRemote{fakeRemote: &fakeRemote{closed: make(chan struct{})}, dirs: dirs, links: links}
}

func TestWorkerEmptiesAFolderFromTheBottomUpWithoutFollowingLinks(t *testing.T) {
	remote := newTreeRemote(map[string][]os.FileInfo{
		"/data": {
			fakeInfo{name: "a.txt", size: 1},
			fakeInfo{name: "sub", mode: os.ModeDir | 0o755},
			fakeInfo{name: "outside", mode: os.ModeSymlink | 0o777},
		},
		"/data/sub":  {fakeInfo{name: "b.txt", size: 1}},
		"/elsewhere": {fakeInfo{name: "kept.txt", size: 1}},
	}, map[string]string{"/data/outside": "/elsewhere"})
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	if !worker.RemoveDirectory("/data") {
		t.Fatal("the removal was not queued")
	}
	if result := waitResult(t, worker); result.Err != nil {
		t.Fatalf("remove directory result = %+v", result)
	}
	if slices.Contains(remote.listed, "/elsewhere") {
		t.Fatal("the link was followed: the folder it points at was emptied")
	}
	want := []string{"/data/a.txt", "/data/sub/b.txt", "/data/sub", "/data/outside", "/data"}
	if !slices.Equal(remote.removed, want) {
		t.Fatalf("removed %q, want %q", remote.removed, want)
	}
	if len(remote.dirs["/elsewhere"]) != 1 {
		t.Fatalf("what the link points at = %+v", remote.dirs["/elsewhere"])
	}
}

func TestWorkerRemovesALinkToAFolderAsTheLinkItIs(t *testing.T) {
	remote := newTreeRemote(map[string][]os.FileInfo{
		"/data":     {fakeInfo{name: "link", mode: os.ModeSymlink | 0o777}},
		"/data/sub": {fakeInfo{name: "kept.txt", size: 1}},
	}, map[string]string{"/data/link": "/data/sub"})
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	if !worker.RemoveDirectory("/data/link") {
		t.Fatal("the removal was not queued")
	}
	if result := waitResult(t, worker); result.Err != nil {
		t.Fatalf("remove link result = %+v", result)
	}
	if len(remote.listed) != 0 {
		t.Fatalf("the link was listed through: %q", remote.listed)
	}
	if !slices.Equal(remote.removed, []string{"/data/link"}) {
		t.Fatalf("removed %q, want only the link", remote.removed)
	}
	if len(remote.dirs["/data/sub"]) != 1 {
		t.Fatalf("the folder the link points at = %+v", remote.dirs["/data/sub"])
	}
}

// endlessRemote lists one more folder inside every folder it is asked about, as
// a looping mount or a hostile server would.
type endlessRemote struct{ *treeRemote }

func (remote endlessRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	remote.listed = append(remote.listed, directory)
	return []os.FileInfo{fakeInfo{name: "again", mode: os.ModeDir | 0o755}}, nil
}

func TestWorkerGivesUpOnATreeThatNeverEnds(t *testing.T) {
	remote := endlessRemote{newTreeRemote(map[string][]os.FileInfo{"/loop": nil}, nil)}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	if !worker.RemoveDirectory("/loop") {
		t.Fatal("the removal was not queued")
	}
	if result := waitResult(t, worker); result.Err == nil {
		t.Fatal("an endless tree was walked to its end")
	}
	if len(remote.listed) > maxRemoveDepth+2 {
		t.Fatalf("listed %d folders before giving up", len(remote.listed))
	}
	if len(remote.removed) != 0 {
		t.Fatalf("removed %q from a tree that was never finished", remote.removed)
	}
}

func TestWorkerRefusesToRemoveThroughAnEntryNamedLikeAPath(t *testing.T) {
	remote := newTreeRemote(map[string][]os.FileInfo{
		"/data": {fakeInfo{name: "../other", size: 1}},
	}, nil)
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	if !worker.RemoveDirectory("/data") {
		t.Fatal("the removal was not queued")
	}
	if result := waitResult(t, worker); !errors.Is(result.Err, ErrPathTraversal) {
		t.Fatalf("remove directory error = %v, want path traversal", result.Err)
	}
	if len(remote.removed) != 0 {
		t.Fatalf("removed %q", remote.removed)
	}
}

func BenchmarkEntriesFrom(b *testing.B) {
	infos := make([]os.FileInfo, 128)
	for index := range infos {
		mode := os.FileMode(0o644)
		if index%8 == 0 {
			mode = os.ModeDir | 0o755
		}
		infos[index] = fakeInfo{name: "entry-" + string(rune(index+32)), size: int64(index), mode: mode}
	}
	b.ReportAllocs()
	for b.Loop() {
		if entries := entriesFrom(infos); len(entries) != len(infos) {
			b.Fatal(len(entries))
		}
	}
}
