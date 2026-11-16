package workbench

import (
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"runtime"
	"strings"

	"github.com/vivaui/viva/components"
	"motty/internal/sftpclient"
)

// Only explicit navigation starts a request. Publishing or refreshing a listing
// can complete that request, but never starts another one in the other direction.
type sftpBrowseSync struct {
	enabled                        bool
	roots                          [2]string // local, remote
	pending                        *sftpBrowseRequest
	creating                       *sftpBrowseRequest
	descriptionRoots               [2]string
	descriptionFormat, description string
}

type sftpBrowseRequest struct {
	paths              [2]string
	side               int
	second             bool
	rebase             bool
	enable             bool
	confirm, attempted bool
}

func (w *sftpWindow) navigationBusy() bool {
	return w.view.busy || w.pendingLocal != "" || w.browse.pending != nil
}

func (w *sftpWindow) toggleBrowseSync() {
	if w.browse.enabled {
		w.stopBrowseCounterpart()
		w.browse.enabled = false
		w.browse.pending = nil
		return
	}
	if !w.connected() || w.navigationBusy() || w.view.path == "" || w.view.lfolder.path == "" {
		return
	}
	// Keep an explicitly opened profile's roots when the panels still show
	// matching children of that pair. Toggling must not turn a child into a root.
	if counterpart, ok := browseCounterpart(true, w.browse.roots, w.view.lfolder.path); !ok || counterpart != path.Clean(w.view.path) {
		w.browse.roots = [2]string{filepath.Clean(w.view.lfolder.path), path.Clean(w.view.path)}
	}
	w.browse.enabled = true
}

// Map paths by components, never by a string prefix ("web2" is not in "web").
// Local paths retain the host OS's volume, UNC and case rules; remote paths
// always retain SFTP's slash-separated, case-sensitive spelling.
func browseCounterpart(local bool, roots [2]string, destination string) (string, bool) {
	var relative string
	if local {
		var err error
		relative, err = filepath.Rel(roots[0], destination)
		if err != nil || relative != "." && !filepath.IsLocal(relative) {
			return "", false
		}
		relative = filepath.ToSlash(relative)
		return path.Join(roots[1], relative), true
	}
	root := path.Clean(roots[1])
	destination = path.Clean(destination)
	switch {
	case destination == root:
		relative = "."
	case root == "/" && path.IsAbs(destination):
		relative = strings.TrimPrefix(destination, "/")
	case strings.HasPrefix(destination, root+"/"):
		relative = strings.TrimPrefix(destination, root+"/")
	default:
		return "", false
	}
	converted := filepath.FromSlash(relative)
	if runtime.GOOS == "windows" && relative != "." {
		for _, component := range strings.Split(relative, "/") {
			if strings.ContainsAny(component, `\:*?"<>|`) || strings.HasSuffix(component, ".") || strings.HasSuffix(component, " ") {
				return "", false
			}
		}
	}
	if relative != "." && (!filepath.IsLocal(converted) || filepath.ToSlash(converted) != relative) {
		return "", false
	}
	return filepath.Join(roots[0], converted), true
}

// Turning synchronization off must not apply a late counterpart response.
// Keep an explicit source navigation, and never cancel the transfer worker:
// uploads and downloads belong to their own independent task lifecycle.
func (w *sftpWindow) stopBrowseCounterpart() {
	request := w.browse.pending
	if request == nil || !request.second || request.confirm || request == w.browse.creating {
		return
	}
	if request.side == 0 {
		w.owner.cancelSFTPLocalNavigation(w.view)
		return
	}
	directory := request.paths[1]
	preparing := w.view.preparingPath == directory
	w.owner.cancelSFTPNavigation(w.view)
	if !preparing {
		w.view.abandonedPath, w.view.busy = directory, true
	}
}

func (w *sftpWindow) startBrowseNavigation(local bool, destination string) {
	w.startBrowseNavigationHistory(local, destination, sftpNavigationManual, 0)
}

func (w *sftpWindow) startBrowseNavigationHistory(local bool, destination string, kind sftpNavigationKind, index int) {
	if w.browse.enabled {
		counterpart, ok := browseCounterpart(local, w.browse.roots, destination)
		if !ok {
			w.browse.enabled = false
			w.notices.show(w.owner.messages.SFTPWindow.BrowseSync, w.owner.messages.SFTPWindow.BrowseOutside, components.ToastInfo)
		} else {
			request := &sftpBrowseRequest{side: sideIndex(local)}
			request.paths[request.side], request.paths[1-request.side] = destination, counterpart
			w.browse.pending = request
		}
	}
	w.readBrowseDirectoryHistory(local, destination, kind, index)
}

func (w *sftpWindow) readBrowseDirectory(local bool, destination string) {
	w.readBrowseDirectoryHistory(local, destination, sftpNavigationManual, 0)
}

func (w *sftpWindow) readBrowseDirectoryHistory(local bool, destination string, kind sftpNavigationKind, index int) {
	if local {
		w.pendingLocal = destination
		if err := w.owner.listLocalNavigation(w.view, destination, kind, index); err != nil {
			w.pendingLocal = ""
			if !w.browseListingFailed(true, destination, err) {
				w.view.errorText = ""
				w.owner.showSFTPPathErrorAt(w.view, destination, err)
			}
		}
		return
	}
	fresh := w.browse.pending != nil || w.view.pathRequest == destination
	w.view.pathRequest = destination
	nav := w.owner.beginSFTPNavigation(w.view, false, destination, kind, index)
	if fresh {
		// A cached directory is optimistic UI, not proof that a paired folder
		// still exists. Confirm both ends before enabling a saved pair.
		w.owner.queueSFTPListing(w.view, destination, nav)
	} else {
		w.owner.openSFTPDirectoryNavigation(w.view, destination, nav)
	}
	if !w.view.busy {
		w.view.pathRequest = ""
		if fresh || w.view.path != destination {
			err := errors.New(w.owner.messages.SFTPUnavailable)
			if !w.browseListingFailed(false, destination, err) {
				w.owner.showSFTPPathErrorAt(w.view, destination, err)
			}
		}
	}
}

func (w *sftpWindow) browseListingCommitted(local bool, directory string) {
	request := w.browse.pending
	if w.closed || request == nil || request.side != sideIndex(local) || request.paths[request.side] != directory {
		return
	}
	if !request.second {
		request.side, request.second = 1-request.side, true
		w.readBrowseDirectory(request.side == 0, request.paths[request.side])
		return
	}
	w.browse.pending = nil
	if request.rebase {
		w.browse.roots, w.browse.enabled = request.paths, request.enable
	}
}

func (w *sftpWindow) browseListingFailed(local bool, directory string, err error) bool {
	request := w.browse.pending
	if request == nil || request.side != sideIndex(local) || request.paths[request.side] != directory {
		return false
	}
	if request.second && !request.attempted && (w.browse.enabled || request.rebase && request.enable) && sftpclient.IsPathNotFound(err) {
		request.confirm = true
		w.view.pathRequest, w.view.errorText = "", ""
		if !local {
			w.view.navigation = sftpNavigation{}
		}
		w.view.setItemCountStatus(w.owner.messages.SFTPItemsFmt, len(w.view.active().rows))
		w.view.dialog = sftpDialogBrowseCreateDirectory
		return true
	}
	w.browse.pending, w.browse.enabled = nil, false
	w.view.pathRequest, w.view.errorText = "", ""
	w.view.setItemCountStatus(w.owner.messages.SFTPItemsFmt, len(w.view.active().rows))
	m := w.owner.messages.SFTPWindow
	w.notices.showPathReason(m.BrowseSync, m.BrowseFailedFmt, directory, err.Error(), components.ToastError)
	return true
}

func (w *sftpWindow) cancelBrowseDirectoryPrompt() {
	if request := w.browse.pending; request != nil && request.confirm {
		w.browse.pending, w.browse.enabled = nil, false
		w.view.pathRequest, w.view.errorText = "", ""
	}
}

func (w *sftpWindow) createBrowseDirectory() {
	request := w.browse.pending
	if request == nil || !request.confirm || w.closed || !w.connected() {
		w.owner.closeSFTPDialog(w.view)
		return
	}
	request.confirm, request.attempted = false, true
	w.owner.closeSFTPDialog(w.view)
	directory := request.paths[request.side]
	w.browse.creating = request
	if request.side == 0 {
		w.pendingLocal = directory
		go func() {
			err := os.MkdirAll(directory, 0o755)
			w.owner.aiPost(func(*State) { w.finishBrowseDirectoryCreation(request, err) })
		}()
		return
	}
	if !w.view.worker.CreateDirectories(directory) {
		w.browse.creating = nil
		w.browseDirectoryFailed(directory, errors.New(w.owner.messages.SFTPUnavailable))
		return
	}
	w.view.busy, w.view.operation = true, sftpclient.OperationCreateDirectory
	w.view.status = fmt.Sprintf(w.owner.messages.SFTPCreatingDirectoryFmt, directory)
}

func (w *sftpWindow) browseDirectoryFailed(directory string, err error) {
	w.browse.pending, w.browse.enabled = nil, false
	w.view.pathRequest, w.view.errorText = "", ""
	w.view.setItemCountStatus(w.owner.messages.SFTPItemsFmt, len(w.view.active().rows))
	m := w.owner.messages.SFTPWindow
	w.notices.showPathReason(m.BrowseSync, m.BrowseCreateFailedFmt, directory, err.Error(), components.ToastError)
}

func (w *sftpWindow) browseDirectoryCreated(result sftpclient.Result) bool {
	request := w.browse.creating
	if request == nil || request.side != 1 || result.Operation != sftpclient.OperationCreateDirectory || result.Path != request.paths[1] {
		return false
	}
	w.finishBrowseDirectoryCreation(request, result.Err)
	return true
}

func (w *sftpWindow) finishBrowseDirectoryCreation(request *sftpBrowseRequest, err error) {
	if w.browse.creating != request {
		return
	}
	w.browse.creating = nil
	if request.side == 0 {
		w.pendingLocal = ""
	} else {
		w.view.busy = false
	}
	if w.closed || w.browse.pending != request || !w.connected() {
		return // a completed mkdir must not revive a retired navigation
	}
	directory := request.paths[request.side]
	if err != nil {
		w.browseDirectoryFailed(directory, err)
		return
	}
	if request.side == 1 {
		w.view.forgetListings()
	}
	w.readBrowseDirectory(request.side == 0, directory)
}

func (w *sftpWindow) cancelBrowseNavigation(local bool, directory string) {
	request := w.browse.pending
	if request == nil || request.side != sideIndex(local) || request.paths[request.side] != directory {
		return
	}
	// The retired listing cannot finish the pair; its two locations no longer
	// confirm synchronization, even when opening a profile with sync disabled.
	w.browse.pending, w.browse.enabled = nil, false
}

func (w *sftpWindow) openPairedBookmark(bookmark sftpBookmark) {
	if w.navigationBusy() || !w.connected() {
		return
	}
	if bookmark.local == "" {
		// A remote-only profile cannot establish a synchronized pair. Clear a
		// previous pair before opening it so the toolbar reflects the profile.
		w.browse.enabled = false
		w.browse.pending = nil
		w.navigate(false, bookmark.path)
		return
	}
	if !filepath.IsAbs(bookmark.local) {
		w.notices.show(w.owner.messages.SFTPWindow.BookmarkTitle, w.owner.messages.SFTPWindow.BookmarkInvalid, components.ToastError)
		return
	}
	w.cancelDrag()
	enabled := bookmark.sync
	w.browse.enabled = false
	w.browse.pending = &sftpBrowseRequest{paths: [2]string{filepath.Clean(bookmark.local), path.Clean(bookmark.path)}, side: 1, rebase: true, enable: enabled}
	w.readBrowseDirectory(false, bookmark.path)
}

func (w *sftpWindow) browseDescription() string {
	m := w.owner.messages.SFTPWindow
	if w.browse.enabled {
		if w.browse.descriptionRoots != w.browse.roots || w.browse.descriptionFormat != m.BrowseRootsFmt {
			w.browse.descriptionRoots, w.browse.descriptionFormat = w.browse.roots, m.BrowseRootsFmt
			w.browse.description = fmt.Sprintf(m.BrowseRootsFmt, w.browse.roots[0], w.browse.roots[1])
		}
		return w.browse.description
	}
	return m.BrowseHint
}
