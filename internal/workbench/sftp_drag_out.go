package workbench

import (
	"io"
	"os"
	"path"
	"path/filepath"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/sftpclient"
)

// Dragging a row out of the application hands the files to the desktop: the
// file manager writes them where the user lets go. A remote row does not exist
// on this computer yet, so its content is promised and produced only when the
// target copies it; a local row's paths are offered as they are. Either way a
// drag out copies and never deletes what it came from.
//
// The drag starts when the pointer leaves every Motty window. Until then the
// gesture belongs to the panes, which move, copy and download inside the
// application.

// pointOutsideViewport reports whether a pointer event is outside the content
// area of the window it reached. A captured press keeps reporting moves out
// there, which is how a drag sees that it left the application.
func pointOutsideViewport(event gfx.PointerEvent) bool {
	return event.X < 0 || event.Y < 0 || event.X >= event.Viewport.Width || event.Y >= event.Viewport.Height
}

// ownWindowAt reports whether a screen point is over one of this process's
// windows, other than exclude. A drag that is merely over another Motty window
// must not become the system's drag: the panes handle their own targets.
func (state *State) ownWindowAt(at gfx.Point, exclude *sftpWindow) bool {
	for _, member := range state.dropStates() {
		if member == nil || member.closed {
			continue
		}
		if member.windowVisible != nil && !member.windowVisible() {
			continue
		}
		if member.frame != nil {
			if frame, ok := member.frame(); ok && pointInRect(at.X, at.Y, frame) {
				return true
			}
		}
		for _, window := range member.sftpWindows {
			if window == nil || window == exclude || window.closed || window.window == nil || window.frame == nil {
				continue
			}
			if window.visible != nil && !window.visible() {
				continue
			}
			if frame, ok := window.frame(); ok && pointInRect(at.X, at.Y, frame) {
				return true
			}
		}
	}
	return false
}

// dragFilesOut hands entries from the folder at directory to the desktop. The
// window is the detached file-manager window when the gesture started there,
// nil for the docked panel.
func (state *State) dragFilesOut(window *sftpWindow, view *sftpView, entries []sftpclient.Entry, directory string, fromLocal bool) {
	if view == nil || len(entries) == 0 || !app.Supports(app.CapabilityFileDrag) {
		return
	}
	if fromLocal {
		paths := make([]string, 0, len(entries))
		for _, entry := range entries {
			if entry.LinkBroken {
				continue
			}
			paths = append(paths, filepath.Join(directory, entry.Name))
		}
		if len(paths) == 0 {
			return
		}
		if err := app.StartFileDrag(paths, app.FileDragCopy); err != nil {
			state.dragOutFailed(window, err)
		}
		return
	}
	promises := remotePromises(view.openFiles, directory, entries, state.dragOutFailedFor(window))
	if len(promises) == 0 {
		return
	}
	if err := app.StartFilePromiseDrag(promises); err != nil {
		state.dragOutFailed(window, err)
	}
}

func (state *State) dragOutFailedFor(window *sftpWindow) func(error) {
	return func(err error) {
		state.aiPost(func(state *State) {
			state.dragOutFailed(window, err)
		})
	}
}

// dragOutFailed tells the user why the drag never reached the desktop. The
// target's own copy failure is the platform's to show; this is the case where
// the application could not prepare what it promised.
func (state *State) dragOutFailed(window *sftpWindow, err error) {
	if err == nil {
		return
	}
	messages := state.messages
	state.fileOpenNotice(window, messages.SFTPWindow.DragOutFailed, err.Error(), components.ToastError)
}

// remotePromises maps the entries of a remote folder to the promises a drag
// offers. Names follow the download's rules so dragging a file out and
// downloading it give the same local name; symbolic links are left out, as
// Download refuses them.
func remotePromises(openFiles func() (sftpclient.Remote, error), directory string, entries []sftpclient.Entry, failed func(error)) []app.FilePromise {
	if openFiles == nil {
		return nil
	}
	promises := make([]app.FilePromise, 0, len(entries))
	for _, entry := range entries {
		if entry.LinkBroken || entry.IsLink() {
			continue
		}
		name := safeLocalName(entry.Name)
		remotePath := path.Join(directory, entry.Name)
		if entry.IsDir() {
			promises = append(promises, remoteDirectoryPromise(openFiles, name, remotePath, failed))
			continue
		}
		promises = append(promises, remoteFilePromise(openFiles, name, remotePath, entry.Size, failed))
	}
	return promises
}

func remoteFilePromise(openFiles func() (sftpclient.Remote, error), name, remotePath string, size int64, failed func(error)) app.FilePromise {
	return app.FilePromise{Name: name, Size: size, Open: func() (io.ReadCloser, error) {
		remote, err := openFiles()
		if err != nil {
			failed(err)
			return nil, err
		}
		reader, err := remote.Open(remotePath)
		if err != nil {
			remote.Close()
			failed(err)
			return nil, err
		}
		return &promiseReader{ReadCloser: reader, remote: remote}, nil
	}}
}

func remoteDirectoryPromise(openFiles func() (sftpclient.Remote, error), name, remotePath string, failed func(error)) app.FilePromise {
	return app.FilePromise{Name: name, Directory: true, List: func() ([]app.FilePromise, error) {
		remote, err := openFiles()
		if err != nil {
			return nil, err
		}
		infos, err := remote.ReadDir(remotePath)
		remote.Close()
		if err != nil {
			return nil, err
		}
		children := make([]app.FilePromise, 0, len(infos))
		for _, info := range infos {
			if info.Mode()&os.ModeSymlink != 0 {
				continue
			}
			name := safeLocalName(info.Name())
			childPath := path.Join(remotePath, info.Name())
			if info.IsDir() {
				children = append(children, remoteDirectoryPromise(openFiles, name, childPath, failed))
				continue
			}
			children = append(children, remoteFilePromise(openFiles, name, childPath, info.Size(), failed))
		}
		return children, nil
	}}
}

// promiseReader closes the file channel with the file it served.
type promiseReader struct {
	io.ReadCloser
	remote sftpclient.Remote
}

func (reader *promiseReader) Close() error {
	err := reader.ReadCloser.Close()
	if reader.remote != nil {
		reader.remote.Close()
		reader.remote = nil
	}
	return err
}
