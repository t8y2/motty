//go:build windows || darwin || linux

package workbench

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"testing"
	"time"

	"github.com/pkg/sftp"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

func TestSFTPBrowseCreatesMissingLocalCounterpartOnlyAfterConfirmation(t *testing.T) {
	state, w, _, host, root := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.toggleBrowseSync()
	roots := w.browse.roots
	w.navigate(false, "/home/test/docs")
	dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
	target := filepath.Join(root, "docs")
	if _, err := os.Stat(target); !errors.Is(err, os.ErrNotExist) {
		t.Fatal("directory was created without confirmation")
	}
	if !w.browse.enabled || !w.navigationBusy() || w.view.errorText != "" || len(w.notices.items) != 0 {
		t.Fatal("asking to create lost the pair or showed an error")
	}
	if err := view.Rest(); err != nil {
		t.Fatal(err)
	}
	if _, ok := view.Hit(state.messages.SFTPWindow.BrowseCreateConfirm); !ok {
		t.Fatal("missing create action")
	}
	dualClickID(t, view, hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	info, err := os.Stat(target)
	if err != nil || !info.IsDir() || !w.browse.enabled || w.browse.roots != roots || w.view.lfolder.path != target || w.view.path != "/home/test/docs" {
		t.Fatalf("creation did not complete the pair: %v", err)
	}
	if len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("creation queued a file transfer")
	}
	w.navigate(true, root)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if w.view.path != "/home/test" || !w.browse.enabled {
		t.Fatal("subsequent navigation lost synchronization")
	}
}

func TestSFTPBrowseCreateDialogFitsAndInitiallyFocusesPause(t *testing.T) {
	for _, scale := range []float64{1, 1.5, 2} {
		t.Run(fmt.Sprint(scale), func(t *testing.T) {
			state, w, _, host, root := dualSFTPFixture(t, scale)
			view := host.windows[0].view
			w.toggleBrowseSync()
			w.navigate(false, "/home/test/docs")
			dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
			if err := view.Resize(960, 720); err != nil {
				t.Fatal(err)
			}
			if err := view.Rest(); err != nil {
				t.Fatal(err)
			}
			for _, id := range []gfx.ID{hitSFTPDialogSubmit, hitSFTPDialogCancel, hitSFTPDialogClose} {
				bounds := dualHit(t, view, id)
				if bounds.X < 0 || bounds.Y < 0 || bounds.X+bounds.Width > 960 || bounds.Y+bounds.Height > 720 {
					t.Fatal("confirmation action escaped the window")
				}
			}
			if view.Snapshot().Focused != hitSFTPDialogCancel {
				t.Fatal("directory creation should require explicit confirmation")
			}
			if err := view.Driver().Key(gfx.KeyEnter, 0); err != nil {
				t.Fatal(err)
			}
			if w.navigationBusy() || w.browse.enabled || w.view.dialog != sftpDialogNone {
				t.Fatal("the initially focused pause action did not dismiss cleanly")
			}
			if _, err := os.Stat(filepath.Join(root, "docs")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("entering the dialog accidentally created the directory")
			}
		})
	}
}

func TestSFTPBrowseCreatesNestedRemoteCounterpartAndContinues(t *testing.T) {
	state, w, remote, host, root := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.view.worker.Close()
	<-w.view.worker.Done()
	backend := &browseMkdirRemote{workbenchSFTPRemote: remote}
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return backend, nil }, func() { w.window.Invalidate() })
	target := filepath.Join(root, "new parent", "中文 child")
	if err := os.MkdirAll(target, 0o755); err != nil {
		t.Fatal(err)
	}
	w.toggleBrowseSync()
	roots := w.browse.roots
	w.navigate(true, target)
	dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
	remoteTarget := "/home/test/new parent/中文 child"
	remote.mu.Lock()
	_, exists := remote.entries[remoteTarget]
	remote.mu.Unlock()
	if exists {
		t.Fatal("remote directory was created before confirmation")
	}
	dualClickID(t, view, hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	if !w.browse.enabled || w.view.path != remoteTarget || w.view.lfolder.path != target || w.browse.roots != roots {
		t.Fatal("remote creation did not keep the mapped pair")
	}
	remote.mu.Lock()
	_, parent := remote.entries[path.Dir(remoteTarget)]
	_, child := remote.entries[remoteTarget]
	files := len(remote.files)
	remote.mu.Unlock()
	if !parent || !child || files != 2 || len(w.view.worker.Transfers(nil)) != 0 {
		t.Fatal("creation changed files or omitted a parent")
	}
}

func TestSFTPBrowsePendingPairDoesNotOpenAnotherFileDialog(t *testing.T) {
	state, w, remote, host, root := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	if err := os.Mkdir(filepath.Join(root, "docs"), 0o700); err != nil {
		t.Fatal(err)
	}
	remote.mu.Lock()
	remote.readDelay = 80 * time.Millisecond
	remote.mu.Unlock()
	w.toggleBrowseSync()
	w.navigate(true, filepath.Join(root, "docs"))
	if w.browse.pending == nil {
		t.Fatal("paired navigation did not start")
	}
	w.fileAction(true, hitSFTPNewDirectory)
	for _, id := range []gfx.ID{sftpWindowNewFolder[0], sftpWindowNewFolder[1], hitSFTPLocationProfiles} {
		w.click(id)
		if w.view.dialog != sftpDialogNone || w.bookmark.open {
			t.Fatalf("%v opened a competing dialog during paired navigation", id)
		}
	}
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
}

func TestSFTPBrowseTreatsSFTPNoSuchPathAsMissingCounterpart(t *testing.T) {
	state, w, remote, host, root := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.view.worker.Close()
	<-w.view.worker.Done()
	backend := pathErrorRemote{
		workbenchSFTPRemote: remote,
		err:                 &sftp.StatusError{Code: 10},
	}
	w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return backend, nil }, func() { w.window.Invalidate() })
	target := filepath.Join(root, "missing")
	if err := os.Mkdir(target, 0o700); err != nil {
		t.Fatal(err)
	}
	w.toggleBrowseSync()
	w.navigate(true, target)
	dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
	if w.browse.pending == nil || w.browse.pending.paths[1] != "/home/test/missing" || !w.browse.enabled {
		t.Fatal("NO_SUCH_PATH did not offer to create the matching directory")
	}
	dualClickID(t, view, hitSFTPDialogCancel)
	if w.navigationBusy() || w.browse.enabled || w.view.path != "/home/test" {
		t.Fatal("pausing did not keep the existing server directory")
	}
}

func TestSFTPBrowseCreationCanBePausedByButtonEscapeAndClose(t *testing.T) {
	for _, cancel := range []string{"button", "escape", "close"} {
		t.Run(cancel, func(t *testing.T) {
			state, w, _, host, root := dualSFTPFixture(t, 1)
			view := host.windows[0].view
			w.toggleBrowseSync()
			w.navigate(false, "/home/test/docs")
			dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
			if cancel == "escape" {
				if err := view.Driver().Key(gfx.KeyEscape, 0); err != nil {
					t.Fatal(err)
				}
			} else if cancel == "close" {
				dualClickID(t, view, hitSFTPDialogClose)
			} else {
				dualClickID(t, view, hitSFTPDialogCancel)
			}
			if w.navigationBusy() || w.browse.enabled || w.view.dialog != sftpDialogNone || w.view.lfolder.path != root || w.view.path != "/home/test/docs" {
				t.Fatal("pausing did not release the request and keep both visible directories")
			}
			if _, err := os.Stat(filepath.Join(root, "docs")); !errors.Is(err, os.ErrNotExist) {
				t.Fatal("pause created a directory")
			}
		})
	}
}

func TestSFTPBrowseCreationCannotReplaceALocalFile(t *testing.T) {
	state, w, _, host, root := dualSFTPFixture(t, 1)
	view := host.windows[0].view
	w.toggleBrowseSync()
	w.navigate(false, "/home/test/docs")
	dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
	target := filepath.Join(root, "docs")
	if err := os.WriteFile(target, []byte("keep"), 0o600); err != nil {
		t.Fatal(err)
	}
	w.click(hitSFTPDialogSubmit)
	dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
	got, err := os.ReadFile(target)
	if err != nil || string(got) != "keep" || w.browse.enabled || w.navigationBusy() || w.view.errorText != "" || len(w.notices.items) != 1 || w.notices.items[0].kind != components.ToastError {
		t.Fatal("creation overwrote a file or failed to use a global error")
	}
}

type browseMkdirRemote struct {
	*workbenchSFTPRemote
	failure error
	started chan struct{}
	gate    chan struct{}
}

func (r *browseMkdirRemote) ReadDir(directory string) ([]os.FileInfo, error) {
	r.mu.Lock()
	_, exists := r.entries[directory]
	_, file := r.files[directory]
	r.mu.Unlock()
	if !exists {
		if file {
			return nil, os.ErrInvalid
		}
		return nil, os.ErrNotExist
	}
	return r.workbenchSFTPRemote.ReadDir(directory)
}

func (r *browseMkdirRemote) Mkdir(directory string) error {
	if r.started != nil {
		r.started <- struct{}{}
		<-r.gate
	}
	if r.failure != nil {
		return r.failure
	}
	return r.workbenchSFTPRemote.Mkdir(directory)
}

func TestSFTPBrowseRemoteCreationFailureIsGlobalAndLateCreationDoesNotNavigate(t *testing.T) {
	for _, late := range []bool{false, true} {
		t.Run(map[bool]string{false: "permission", true: "late"}[late], func(t *testing.T) {
			state, w, remote, host, root := dualSFTPFixture(t, 1)
			view := host.windows[0].view
			w.view.worker.Close()
			<-w.view.worker.Done()
			backend := &browseMkdirRemote{workbenchSFTPRemote: remote, failure: os.ErrPermission}
			if late {
				backend.failure = nil
				backend.started = make(chan struct{}, 1)
				backend.gate = make(chan struct{})
				defer close(backend.gate)
			}
			w.view.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return backend, nil }, func() { w.window.Invalidate() })
			target := filepath.Join(root, "missing")
			if err := os.Mkdir(target, 0o700); err != nil {
				t.Fatal(err)
			}
			w.toggleBrowseSync()
			w.navigate(true, target)
			dualWait(t, state, w, view, func() bool { return w.view.dialog == sftpDialogBrowseCreateDirectory })
			w.click(hitSFTPDialogSubmit)
			if late {
				select {
				case <-backend.started:
				case <-time.After(4 * time.Second):
					t.Fatal("mkdir did not start")
				}
				w.toggleBrowseSync()
				backend.gate <- struct{}{}
			}
			dualWait(t, state, w, view, func() bool { return !w.navigationBusy() })
			if w.browse.enabled || w.view.path != "/home/test" || w.view.errorText != "" {
				t.Fatal("creation restored a retired pair or moved the server")
			}
			if !late && (len(w.notices.items) != 1 || w.notices.items[0].kind != components.ToastError) {
				t.Fatal("creation failure did not use a global toast")
			}
		})
	}
}
