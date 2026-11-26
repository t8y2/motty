package sftpclient

import (
	"errors"
	"os"
	"path"
	"reflect"
	"strings"
	"testing"
)

type chmodTestCall struct {
	path string
	mode os.FileMode
}

// chmodTestRemote is a small directory tree that records every permission
// change and can refuse a particular path.
type chmodTestRemote struct {
	fakeRemote
	entries      map[string][]os.FileInfo
	chmodErr     map[string]error
	calls        []chmodTestCall
	readDirs     []string
	restrictRead bool
	modes        map[string]os.FileMode
}

func (remote *chmodTestRemote) Lstat(name string) (os.FileInfo, error) {
	for _, info := range remote.entries[path.Dir(name)] {
		if info.Name() == path.Base(name) {
			return info, nil
		}
	}
	return nil, os.ErrNotExist
}

func (remote *chmodTestRemote) ReadDir(name string) ([]os.FileInfo, error) {
	remote.readDirs = append(remote.readDirs, name)
	if mode, changed := remote.modes[name]; remote.restrictRead && changed && mode.Perm()&0o500 != 0o500 {
		return nil, os.ErrPermission
	}
	return remote.entries[name], nil
}

func (remote *chmodTestRemote) Chmod(name string, mode os.FileMode) error {
	remote.calls = append(remote.calls, chmodTestCall{path: name, mode: mode.Perm()})
	if remote.restrictRead {
		if remote.modes == nil {
			remote.modes = map[string]os.FileMode{}
		}
		remote.modes[name] = mode
	}
	return remote.chmodErr[name]
}

func newChmodTestWorker(t testing.TB, remote *chmodTestRemote) *Worker {
	t.Helper()
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	t.Cleanup(worker.Close)
	return worker
}

func TestWorkerChmodSelectionAppliesToEveryPathAndContinuesAfterFailure(t *testing.T) {
	refused := errors.New("permission denied")
	remote := &chmodTestRemote{
		fakeRemote: fakeRemote{closed: make(chan struct{})},
		entries: map[string][]os.FileInfo{
			"/": {
				fakeInfo{name: "one", mode: 0o644},
				fakeInfo{name: "two", mode: 0o644},
			},
		},
		chmodErr: map[string]error{"/two": refused},
	}
	worker := newChmodTestWorker(t, remote)

	result := requireQueuedResult(t, worker.ChmodSelection([]string{"/one", "/two"}, 0o600, false), worker, OperationChmodSelection)
	if result.Files != 1 || result.Failures != 1 {
		t.Fatalf("changed=%d failed=%d, want 1 and 1", result.Files, result.Failures)
	}
	if !errors.Is(result.Err, refused) {
		t.Fatalf("server refusal was lost: %v", result.Err)
	}
	var partial *PartialChmodError
	if !errors.As(result.Err, &partial) || partial.Failed != 1 {
		t.Fatalf("partial result = %+v", result.Err)
	}
	want := []chmodTestCall{{path: "/one", mode: 0o600}, {path: "/two", mode: 0o600}}
	if !reflect.DeepEqual(remote.calls, want) {
		t.Fatalf("chmod calls = %+v, want %+v", remote.calls, want)
	}
}

func TestWorkerChmodSelectionWalksFoldersAndLeavesLinksAlone(t *testing.T) {
	remote := &chmodTestRemote{
		fakeRemote: fakeRemote{closed: make(chan struct{})},
		entries: map[string][]os.FileInfo{
			"/": {
				fakeInfo{name: "tree", mode: os.ModeDir | 0o755},
				fakeInfo{name: "link", mode: os.ModeSymlink | 0o777},
			},
			"/tree": {
				fakeInfo{name: "a", mode: 0o644},
				fakeInfo{name: "sub", mode: os.ModeDir | 0o755},
				fakeInfo{name: "link", mode: os.ModeSymlink | 0o777},
			},
			"/tree/sub": {
				fakeInfo{name: "b", mode: 0o600},
			},
		},
	}
	worker := newChmodTestWorker(t, remote)

	result := requireQueuedResult(t, worker.ChmodSelection([]string{"/tree", "/link"}, 0o750, true), worker, OperationChmodSelection)
	if result.Err != nil || result.Files != 4 || result.Failures != 0 {
		t.Fatalf("recursive chmod = %+v", result)
	}
	want := []chmodTestCall{
		{path: "/tree/a", mode: 0o750},
		{path: "/tree/sub/b", mode: 0o750},
		{path: "/tree/sub", mode: 0o750},
		{path: "/tree", mode: 0o750},
	}
	if !reflect.DeepEqual(remote.calls, want) {
		t.Fatalf("chmod calls = %+v, want %+v", remote.calls, want)
	}
	if !reflect.DeepEqual(remote.readDirs, []string{"/tree", "/tree/sub"}) {
		t.Fatalf("walk entered unexpected directories: %v", remote.readDirs)
	}
}

func TestWorkerChmodSelectionWalksBeforeRestrictingFolderPermissions(t *testing.T) {
	remote := &chmodTestRemote{
		fakeRemote:   fakeRemote{closed: make(chan struct{})},
		restrictRead: true,
		entries: map[string][]os.FileInfo{
			"/": {
				fakeInfo{name: "tree", mode: os.ModeDir | 0o755},
			},
			"/tree": {
				fakeInfo{name: "a", mode: 0o644},
			},
		},
	}
	worker := newChmodTestWorker(t, remote)

	result := requireQueuedResult(t, worker.ChmodSelection([]string{"/tree"}, 0o600, true), worker, OperationChmodSelection)
	if result.Err != nil || result.Files != 2 {
		t.Fatalf("recursive chmod could not finish before access was restricted: %+v", result)
	}
	want := []chmodTestCall{{path: "/tree/a", mode: 0o600}, {path: "/tree", mode: 0o600}}
	if !reflect.DeepEqual(remote.calls, want) {
		t.Fatalf("chmod calls = %+v, want %+v", remote.calls, want)
	}
}

func TestWorkerChmodSelectionRejectsAPathThatEscapesTheTree(t *testing.T) {
	remote := &chmodTestRemote{
		fakeRemote: fakeRemote{closed: make(chan struct{})},
		entries: map[string][]os.FileInfo{
			"/": {
				fakeInfo{name: "tree", mode: os.ModeDir | 0o755},
			},
			"/tree": {
				fakeInfo{name: "../outside", mode: 0o644},
			},
		},
	}
	worker := newChmodTestWorker(t, remote)

	result := requireQueuedResult(t, worker.ChmodSelection([]string{"/tree"}, 0o600, true), worker, OperationChmodSelection)
	if !errors.Is(result.Err, ErrPathTraversal) {
		t.Fatalf("path traversal was not refused: %v", result.Err)
	}
	if len(remote.calls) != 0 {
		t.Fatalf("walk changed the tree before refusing an unsafe entry: %+v", remote.calls)
	}
}

func TestWorkerChmodSelectionRefusesRecursionWithoutLstat(t *testing.T) {
	remote := &fakeRemote{closed: make(chan struct{})}
	worker := NewWorker(func() (Remote, error) { return remote, nil }, nil)
	defer worker.Close()

	result := requireQueuedResult(t, worker.ChmodSelection([]string{"/tree"}, 0o600, true), worker, OperationChmodSelection)
	if result.Files != 0 || result.Failures != 1 || !strings.Contains(result.Err.Error(), "recursive permission change is not supported") {
		t.Fatalf("unsupported recursive chmod = %+v", result)
	}
}
