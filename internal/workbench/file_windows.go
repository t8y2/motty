package workbench

import (
	"crypto/sha256"
	"errors"
	"fmt"
	"io"
	"os"
	"path"
	"path/filepath"
	"strings"
	"sync"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/text"
	"motty/internal/filecontent"
	"motty/internal/sftpclient"
)

type fileWindowManager struct {
	mu        sync.Mutex
	closed    bool
	events    []fileDocumentEvent
	documents map[string]*fileDocument // UI thread only
}
type fileDocumentEvent struct {
	document           *fileDocument
	data               []byte
	image              *filecontent.Image
	decoded            filecontent.Text
	err                error
	saved, reinterpret bool
}
type fileDocument struct {
	owner                            *State
	cacheKey, location, name, source string
	local                            bool
	worker                           *sftpclient.Worker
	window                           *app.ContentWindow
	raw                              []byte
	revision                         sftpclient.Revision
	format                           filecontent.Format
	editor                           text.Editor
	original, pending                string
	image                            *filecontent.Image
	ready, busy, closed              bool
	status, problem                  string
	encodingOpen                     bool
	confirm                          string
	allowClose, afterClosed          func()
	closeAfter                       bool
	notices                          fileNotices
	ui                               fileDocumentUI
}

func (state *State) postFileEvent(event fileDocumentEvent) {
	m := &state.fileWindows
	m.mu.Lock()
	if m.closed {
		m.mu.Unlock()
		return
	}
	m.events = append(m.events, event)
	m.mu.Unlock()
	state.options.Invalidate()
}

func (state *State) openFileWindow(view *sftpView, location string, local bool) {
	if state.options.OpenContentWindow == nil {
		return
	}
	var session *Session
	for i := range state.sessions {
		if state.sessions[i].files == view {
			session = &state.sessions[i]
			break
		}
	}
	if session == nil {
		return
	}
	if entry, ok := state.selectedSFTPEntry(view); ok && entry.Size > sftpclient.MaxPreviewFileBytes {
		state.fileNotices.show(state.messages.FileWindow.CannotOpen, state.messages.FileWindow.TooLarge, components.ToastWarning)
		return
	}
	key := fmt.Sprintf("%d:%t:%s", session.ID, local, location)
	if existing := state.fileWindows.documents[key]; existing != nil {
		if existing.window != nil {
			existing.window.Show()
		}
		return
	}
	if len(state.fileWindows.documents) >= 8 {
		state.fileNotices.show(state.messages.FileWindow.CannotOpen, state.messages.FileWindow.TooMany, components.ToastWarning)
		return
	}
	if state.fileWindows.documents == nil {
		state.fileWindows.documents = make(map[string]*fileDocument)
	}
	name := path.Base(location)
	source := session.Title + " · " + session.Address
	if local {
		name = filepath.Base(location)
		source = state.messages.FileWindow.Local
	}
	d := &fileDocument{owner: state, cacheKey: key, location: location, name: name, source: source, local: local, status: state.messages.FileWindow.Loading}
	state.fileWindows.documents[key] = d
	if local {
		go func() {
			data, err := readLocalDocument(location)
			event := decodeFileEvent(d, data, err)
			state.postFileEvent(event)
		}()
		return
	}
	conn, mode := session.conn, session.host.Transfer
	if conn == nil {
		delete(state.fileWindows.documents, key)
		state.fileNotices.show(state.messages.FileWindow.CannotOpen, state.messages.SFTPUnavailable, components.ToastError)
		return
	}
	d.worker = sftpclient.NewWorker(func() (sftpclient.Remote, error) { return openRemoteFiles(conn, mode) }, func() {})
	go func() {
		for result := range d.worker.Results() {
			if result.Operation == sftpclient.OperationOpenDocument {
				state.postFileEvent(decodeFileEvent(d, result.Data, result.Err))
			}
			if result.Operation == sftpclient.OperationSaveDocument {
				state.postFileEvent(fileDocumentEvent{document: d, data: result.Data, err: result.Err, saved: true})
			}
		}
	}()
	if !d.worker.OpenDocument(location) {
		delete(state.fileWindows.documents, key)
		d.worker.Close()
		state.fileNotices.show(state.messages.FileWindow.CannotOpen, state.messages.SFTPUnavailable, components.ToastError)
	}
}

func decodeFileEvent(document *fileDocument, data []byte, err error) fileDocumentEvent {
	event := fileDocumentEvent{document: document, data: data, err: err}
	if err != nil {
		return event
	}
	event.image, event.err = filecontent.DecodeImage(data, document.name)
	if event.err != nil || event.image != nil {
		return event
	}
	if filecontent.KnownBinary(data) {
		event.err = filecontent.ErrBinary
		return event
	}
	switch strings.ToLower(filepath.Ext(document.name)) {
	case ".bin", ".exe", ".dll", ".zip", ".gz", ".7z", ".pdf", ".db", ".sqlite", ".tar", ".rar", ".so", ".dylib", ".jar", ".mp3", ".mp4":
		event.err = filecontent.ErrBinary
		return event
	}
	event.decoded, event.err = filecontent.Decode(data, "")
	return event
}

func (state *State) pumpFileWindows() {
	m := &state.fileWindows
	m.mu.Lock()
	events := m.events
	m.events = nil
	m.mu.Unlock()
	for _, event := range events {
		d := event.document
		if state.closed || d.closed {
			continue
		}
		if event.saved || event.reinterpret {
			d.apply(event)
			if d.window != nil {
				d.window.Invalidate()
			}
			continue
		}
		// Reload keeps an explicitly selected BOM-less encoding. A server-side
		// BOM is authoritative; undecodable replacement bytes require a new choice.
		if d.window != nil && d.image == nil && event.image == nil && d.format.Encoding != "" && !event.decoded.Format.BOM && (event.err == nil || errors.Is(event.err, filecontent.ErrChooseEncoding)) {
			event.decoded, event.err = filecontent.Decode(event.data, d.format.Encoding)
			if errors.Is(event.err, filecontent.ErrEncoding) {
				event.err = filecontent.ErrChooseEncoding
			}
		}
		if event.err != nil && !errors.Is(event.err, filecontent.ErrChooseEncoding) {
			if d.window != nil {
				d.busy = false
				d.closeAfter = false
				d.afterClosed = nil
				d.problem = state.fileProblem(event.err)
				d.notices.show(d.name, d.problem, components.ToastError)
				d.window.Invalidate()
				continue
			}
			state.fileNotices.show(state.messages.FileWindow.CannotOpen, d.name+": "+state.fileProblem(event.err), components.ToastWarning)
			delete(m.documents, d.cacheKey)
			d.closed = true
			if d.worker != nil {
				d.worker.Close()
			}
			continue
		}
		d.raw, d.revision, d.image = event.data, sftpclient.Revision(sha256.Sum256(event.data)), event.image
		d.setText(event.decoded, event.err)
		if d.window == nil {
			window, err := state.options.OpenContentWindow(d.config(), nil)
			if err != nil {
				state.fileNotices.show(state.messages.FileWindow.CannotOpen, err.Error(), components.ToastError)
				delete(m.documents, d.cacheKey)
				d.closed = true
				if d.worker != nil {
					d.worker.Close()
				}
				continue
			}
			d.window = window
		} else {
			d.window.Invalidate()
		}
		if d.closeAfter {
			d.closeAfter = false
			allow := d.allowClose
			d.allowClose = nil
			if allow == nil {
				allow = d.window.Close
			}
			d.requestClose(allow)
		}
	}
}

func (state *State) fileProblem(err error) string {
	m := state.messages.FileWindow
	switch {
	case errors.Is(err, sftpclient.ErrPreviewFileTooLarge), errors.Is(err, filecontent.ErrTooLarge):
		return m.TooLarge
	case errors.Is(err, filecontent.ErrImageTooLarge):
		return m.ImageTooLarge
	case errors.Is(err, filecontent.ErrBinary), errors.Is(err, filecontent.ErrUnsupportedImage), errors.Is(err, sftpclient.ErrNotRegularFile):
		return m.Unsupported
	case errors.Is(err, filecontent.ErrChooseEncoding):
		return m.ChooseEncoding
	default:
		return err.Error()
	}
}

func (d *fileDocument) setText(decoded filecontent.Text, err error) {
	d.busy = false
	d.problem = ""
	d.ready = err == nil
	if errors.Is(err, filecontent.ErrChooseEncoding) {
		d.format = filecontent.Format{}
		d.original = ""
		d.editor = text.Editor{}
		d.status = d.owner.messages.FileWindow.ChooseEncoding
		return
	}
	if err != nil {
		d.problem = d.owner.fileProblem(err)
		return
	}
	d.format, d.original = decoded.Format, decoded.Value
	d.editor = text.Editor{}
	d.editor.Insert(decoded.Value)
	d.ui.code.Layout = components.TextareaLayout{}
	d.status = d.owner.messages.FileWindow.Ready
	if d.image != nil {
		d.status = d.owner.messages.FileWindow.ReadOnly
	}
}

func (d *fileDocument) dirty() bool {
	return d.image == nil && d.ready && d.editor.String() != d.original
}

func (d *fileDocument) save(closeAfter bool) {
	if d.busy || !d.ready || d.image != nil {
		return
	}
	d.encodingOpen = false
	if !d.dirty() {
		if closeAfter {
			d.closeAllowed()
		}
		return
	}
	data, err := filecontent.Encode(d.editor.String(), d.format)
	if err != nil {
		d.afterClosed = nil
		d.problem = fmt.Sprintf(d.owner.messages.FileWindow.EncodingLoss, d.format.Encoding)
		if !errors.Is(err, filecontent.ErrEncoding) {
			d.problem = d.owner.fileProblem(err)
		}
		d.notices.show(d.name, d.problem, components.ToastError)
		return
	}
	d.pending = d.editor.String()
	d.busy = true
	d.closeAfter = closeAfter
	d.status = d.owner.messages.FileWindow.Saving
	d.problem = ""
	d.confirm = ""
	if d.local {
		revision, location := d.revision, d.location
		go func() {
			err := saveLocalDocument(location, data, revision)
			d.owner.postFileEvent(fileDocumentEvent{document: d, data: data, err: err, saved: true})
		}()
	} else if !d.worker.SaveDocument(d.location, data, d.revision) {
		d.busy = false
		d.closeAfter = false
		d.afterClosed = nil
		d.problem = d.owner.messages.SFTPUnavailable
		d.notices.show(d.name, d.problem, components.ToastError)
	}
}

func (d *fileDocument) apply(event fileDocumentEvent) {
	d.busy = false
	if event.err != nil {
		d.closeAfter = false
		d.afterClosed = nil
		d.problem = d.owner.fileProblem(event.err)
		d.notices.show(d.name, d.problem, components.ToastError)
		return
	}
	if event.reinterpret {
		d.setText(event.decoded, nil)
		if d.closeAfter {
			d.closeAfter = false
			d.closeAllowed()
		}
		return
	}
	d.raw, d.revision = event.data, sftpclient.Revision(sha256.Sum256(event.data))
	decoded, err := filecontent.Decode(d.raw, d.format.Encoding)
	if err != nil {
		d.problem = d.owner.fileProblem(err)
		return
	}
	d.format, d.original = decoded.Format, d.pending
	d.status = d.owner.messages.FileWindow.Saved
	d.problem = ""
	d.notices.show(d.name, d.status, components.ToastSuccess)
	if d.closeAfter {
		d.closeAllowed()
	}
}

func (d *fileDocument) reload() {
	if d.busy {
		return
	}
	d.encodingOpen = false
	d.confirm = ""
	d.busy = true
	d.status = d.owner.messages.FileWindow.Loading
	d.problem = ""
	if d.local {
		go func() {
			data, err := readLocalDocument(d.location)
			d.owner.postFileEvent(decodeFileEvent(d, data, err))
		}()
	} else if !d.worker.OpenDocument(d.location) {
		d.busy = false
		d.closeAfter = false
		d.afterClosed = nil
		d.problem = d.owner.messages.SFTPUnavailable
		d.notices.show(d.name, d.problem, components.ToastError)
	}
}

func (d *fileDocument) chooseEncoding(name string) {
	d.encodingOpen = false
	if d.busy {
		return
	}
	if d.dirty() {
		d.problem = d.owner.messages.FileWindow.EncodingChanged
		d.notices.show(d.name, d.problem, components.ToastWarning)
		return
	}
	d.busy = true
	raw := d.raw
	go func() {
		decoded, err := filecontent.Decode(raw, name)
		d.owner.postFileEvent(fileDocumentEvent{document: d, decoded: decoded, err: err, reinterpret: true})
	}()
}

func (d *fileDocument) requestClose(allow func()) {
	d.encodingOpen = false
	if d.busy {
		d.allowClose = allow
		d.closeAfter = true
		return
	}
	if d.dirty() {
		d.confirm = "close"
		d.allowClose = allow
		if d.window != nil {
			d.window.Invalidate()
		}
		return
	}
	allow()
}
func (d *fileDocument) closeAllowed() {
	if d.allowClose != nil {
		allow := d.allowClose
		d.allowClose = nil
		allow()
	} else if d.window != nil {
		d.window.Close()
	}
}

func (d *fileDocument) onClosed() {
	if d.closed {
		return
	}
	d.closed = true
	if d.worker != nil {
		d.worker.Close()
	}
	if d.owner.fileWindows.documents[d.cacheKey] == d {
		delete(d.owner.fileWindows.documents, d.cacheKey)
	}
	if d.afterClosed != nil {
		next := d.afterClosed
		d.afterClosed = nil
		next()
	}
}

func (state *State) closeFileWindows() {
	m := &state.fileWindows
	m.mu.Lock()
	m.closed = true
	m.events = nil
	m.mu.Unlock()
	for _, d := range m.documents {
		d.afterClosed = nil
		if d.worker != nil {
			d.worker.Close()
		}
		if d.window != nil {
			d.window.Close()
		}
		d.closed = true
	}
	m.documents = nil
}

// ConfirmDocumentClose serializes dirty-document confirmation before closing a
// source window or quitting. Cancel leaves the application and edits alive.
func (state *State) ConfirmDocumentClose(allow func()) bool {
	for _, d := range state.fileWindows.documents {
		if d.dirty() || d.busy {
			d.afterClosed = func() {
				if !state.ConfirmDocumentClose(allow) {
					allow()
				}
			}
			d.requestClose(func() {
				if d.window != nil {
					d.window.Close()
				}
			})
			if d.window != nil {
				d.window.Show()
			}
			return true
		}
	}
	return false
}

func readLocalDocument(location string) ([]byte, error) {
	source, err := os.Open(location)
	if err != nil {
		return nil, err
	}
	defer source.Close()
	info, err := source.Stat()
	if err != nil {
		return nil, err
	}
	if !info.Mode().IsRegular() {
		return nil, filecontent.ErrBinary
	}
	if info.Size() > sftpclient.MaxPreviewFileBytes {
		return nil, sftpclient.ErrPreviewFileTooLarge
	}
	data, err := io.ReadAll(io.LimitReader(source, sftpclient.MaxPreviewFileBytes+1))
	if len(data) > sftpclient.MaxPreviewFileBytes {
		return nil, sftpclient.ErrPreviewFileTooLarge
	}
	return data, err
}

func saveLocalDocument(location string, data []byte, revision sftpclient.Revision) error {
	info, err := os.Lstat(location)
	if err != nil {
		return err
	}
	if !info.Mode().IsRegular() {
		return filecontent.ErrBinary
	}
	current, err := readLocalDocument(location)
	if err != nil {
		return err
	}
	if sftpclient.Revision(sha256.Sum256(current)) != revision {
		return sftpclient.ErrRemoteChanged
	}
	temporary, err := os.CreateTemp(filepath.Dir(location), ".motty-edit-*")
	if err != nil {
		return err
	}
	name := temporary.Name()
	defer os.Remove(name)
	if err = temporary.Chmod(info.Mode().Perm()); err == nil {
		_, err = temporary.Write(data)
	}
	if err == nil {
		err = temporary.Sync()
	}
	closeErr := temporary.Close()
	if err != nil {
		return err
	}
	if closeErr != nil {
		return closeErr
	}
	current, err = readLocalDocument(location)
	if err != nil {
		return err
	}
	if sftpclient.Revision(sha256.Sum256(current)) != revision {
		return sftpclient.ErrRemoteChanged
	}
	return os.Rename(name, location)
}

func (d *fileDocument) config() app.Config {
	return app.Config{Title: d.name + " — " + d.source, Icon: AppIcon(), Width: 1000, Height: 760, Tree: Tree(), Paint: d.paint, TextInput: d.textInput, OnPointer: d.pointer, OnKey: d.key, OnShortcut: d.shortcut, OnScroll: d.scroll, OnCloseRequest: d.requestClose, OnClosed: d.onClosed}
}
