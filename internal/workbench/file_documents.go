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
	"time"

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
	document                     *fileDocument
	data                         []byte
	image                        *filecontent.Image
	decoded                      filecontent.Text
	diff                         components.CodeDiffResult
	diffRight                    string
	diffGeneration               uint64
	err                          error
	saved, reinterpret, compared bool
	diffTick, diffReady          bool
}

type fileComparison struct {
	active, loading, pending  bool
	conflict, saving, confirm bool
	oversized                 bool
	baseline                  string
	revision                  sftpclient.Revision
	result                    components.CodeDiffResult
	computedValue             string
	computedSet               bool
	generation                uint64
	timer                     *time.Timer
	window                    *app.ContentWindow
	ui                        fileDocumentUI
}

const (
	fileDiffDebounce = 250 * time.Millisecond
	fileDiffMaxBytes = 512 << 10
	fileDiffMaxLines = 20_000
)

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
	confirmCloseAfter                bool
	notices                          fileNotices
	origin                           *sftpWindow
	view                             *sftpView
	ui                               fileDocumentUI
	comparison                       fileComparison
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
	if view.detached != nil {
		session = &view.detached.session
	}
	if session == nil {
		return
	}
	if entry, ok := state.selectedSFTPEntry(view); ok && entry.Size > sftpclient.MaxPreviewFileBytes {
		state.fileOpenNotice(view.detached, state.messages.FileWindow.CannotOpen, state.messages.FileWindow.TooLarge, components.ToastWarning)
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
		state.fileOpenNotice(view.detached, state.messages.FileWindow.CannotOpen, state.messages.FileWindow.TooMany, components.ToastWarning)
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
	d := &fileDocument{owner: state, cacheKey: key, location: location, name: name, source: source, local: local, view: view, status: state.messages.FileWindow.Loading}
	d.origin = view.detached
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
		state.fileOpenNotice(view.detached, state.messages.FileWindow.CannotOpen, state.messages.SFTPUnavailable, components.ToastError)
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
			if result.Operation == sftpclient.OperationReadDocument {
				state.postFileEvent(fileDocumentEvent{document: d, data: result.Data, err: result.Err, compared: true})
			}
		}
	}()
	if !d.worker.OpenDocument(location) {
		delete(state.fileWindows.documents, key)
		d.worker.Close()
		state.fileOpenNotice(view.detached, state.messages.FileWindow.CannotOpen, state.messages.SFTPUnavailable, components.ToastError)
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
	defer state.releaseClosedSFTPConnections()
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
		if event.saved || event.reinterpret || event.compared || event.diffTick || event.diffReady {
			d.apply(event)
			d.invalidateWindows()
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
				d.invalidateWindows()
				continue
			}
			state.fileOpenNotice(d.origin, state.messages.FileWindow.CannotOpen, d.name+": "+state.fileProblem(event.err), components.ToastWarning)
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
				state.fileOpenNotice(d.origin, state.messages.FileWindow.CannotOpen, err.Error(), components.ToastError)
				delete(m.documents, d.cacheKey)
				d.closed = true
				if d.worker != nil {
					d.worker.Close()
				}
				continue
			}
			d.window = window
		} else {
			d.invalidateWindows()
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

func (d *fileDocument) requestSave(closeAfter bool) {
	if d.busy || !d.ready || d.image != nil {
		return
	}
	d.encodingOpen = false
	d.ui.saveMenuOpen = false
	if d.comparison.active {
		if closeAfter {
			d.closeAfter = true
		}
		if d.comparison.window != nil {
			d.comparison.window.Show()
		}
		return
	}
	if !d.dirty() {
		if closeAfter {
			d.closeAllowed()
		}
		return
	}
	if d.local {
		d.saveDirect(closeAfter)
		return
	}
	d.confirm = "save"
	d.confirmCloseAfter = closeAfter
	if d.window != nil {
		d.window.Invalidate()
	}
}

func (d *fileDocument) saveDirect(closeAfter bool) {
	if d.busy || !d.ready || d.image != nil {
		return
	}
	if d.comparison.active {
		if closeAfter {
			d.closeAfter = true
		}
		if d.comparison.window != nil {
			d.comparison.window.Show()
		}
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
	d.confirmCloseAfter = false
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
	if event.compared {
		d.applyComparisonRead(event)
		return
	}
	if event.diffTick {
		d.startDiff(event.diffGeneration)
		return
	}
	if event.diffReady {
		d.finishDiff(event)
		return
	}
	d.busy = false
	if event.err != nil {
		d.closeAfter = false
		d.afterClosed = nil
		d.problem = d.owner.fileProblem(event.err)
		d.notices.show(d.name, d.problem, components.ToastError)
		if d.comparison.saving && errors.Is(event.err, sftpclient.ErrRemoteChanged) {
			d.comparison.saving = false
			d.comparison.conflict = true
			d.status = d.owner.messages.FileWindow.CompareConflict
			d.problem = d.status
		} else if d.comparison.saving {
			d.comparison.saving = false
			d.status = d.owner.messages.FileWindow.CompareReady
		}
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
	d.stopComparison()
	if !d.local {
		d.owner.refreshSFTPAfterRemoteSave(d.view, d.location)
	}
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
		d.confirmCloseAfter = false
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
	d.stopComparison()
	if d.worker != nil {
		d.worker.Close()
	}
	if d.owner.fileWindows.documents[d.cacheKey] == d {
		delete(d.owner.fileWindows.documents, d.cacheKey)
	}
	if d.origin != nil {
		d.origin.releaseConnection()
	}
	if d.afterClosed != nil {
		next := d.afterClosed
		d.afterClosed = nil
		next()
	}
}

func (d *fileDocument) beginComparison() {
	if d.comparison.active {
		if d.comparison.window != nil {
			d.comparison.window.Show()
		}
		return
	}
	if d.busy || !d.ready || d.image != nil || d.local {
		return
	}
	if !fileDiffWithinLimit(d.editor.String()) {
		d.rejectComparisonLimit()
		return
	}
	if d.worker == nil {
		return
	}
	d.encodingOpen = false
	d.ui.saveMenuOpen = false
	d.stopComparison()
	d.comparison.ui = fileDocumentUI{}
	d.comparison.loading = true
	d.comparison.pending = false
	d.comparison.conflict = false
	d.comparison.saving = false
	d.comparison.confirm = false
	d.comparison.generation++
	d.busy = true
	d.problem = ""
	d.status = d.owner.messages.FileWindow.Comparing
	if !d.worker.ReadDocument(d.location) {
		d.busy = false
		d.comparison.loading = false
		d.status = d.owner.messages.SFTPUnavailable
		d.problem = d.status
		d.notices.show(d.name, d.problem, components.ToastError)
		d.invalidateWindows()
		d.resumePendingClose()
	}
}

func (d *fileDocument) applyComparisonRead(event fileDocumentEvent) {
	d.busy = false
	c := &d.comparison
	c.loading = false
	if c.generation == 0 {
		return
	}
	if event.err != nil {
		d.problem = d.owner.fileProblem(event.err)
		d.notices.show(d.name, d.problem, components.ToastError)
		if c.active {
			c.conflict = true
			d.status = d.owner.messages.FileWindow.CompareConflict
		} else {
			d.status = d.owner.messages.FileWindow.Ready
		}
		d.invalidateWindows()
		d.resumePendingClose()
		return
	}
	decoded, err := filecontent.Decode(event.data, "")
	if d.format.Encoding != "" && !decoded.Format.BOM && (err == nil || errors.Is(err, filecontent.ErrChooseEncoding)) {
		decoded, err = filecontent.Decode(event.data, d.format.Encoding)
		if errors.Is(err, filecontent.ErrEncoding) {
			err = filecontent.ErrChooseEncoding
		}
	}
	if err != nil {
		d.problem = d.owner.fileProblem(err)
		d.notices.show(d.name, d.problem, components.ToastError)
		if c.active {
			c.conflict = true
			d.status = d.owner.messages.FileWindow.CompareConflict
		} else {
			d.status = d.owner.messages.FileWindow.Ready
		}
		d.invalidateWindows()
		d.resumePendingClose()
		return
	}
	if !fileDiffWithinLimit(decoded.Value) {
		d.rejectComparisonLimit()
		d.resumePendingClose()
		return
	}
	c.active = true
	c.pending = true
	c.conflict = false
	c.oversized = false
	c.baseline = decoded.Value
	c.revision = sftpclient.Revision(sha256.Sum256(event.data))
	c.result = components.CodeDiffResult{Left: strings.Split(c.baseline, "\n")}
	c.computedValue = ""
	c.computedSet = false
	d.problem = ""
	d.status = d.owner.messages.FileWindow.Comparing
	if err := d.openComparisonWindow(); err != nil {
		d.resetComparison()
		d.status = d.owner.messages.FileWindow.Ready
		d.problem = err.Error()
		d.notices.show(d.name, d.problem, components.ToastError)
		d.invalidateWindows()
		d.resumePendingClose()
		return
	}
	d.scheduleDiff()
	d.invalidateWindows()
	d.resumePendingClose()
}

func fileDiffWithinLimit(value string) bool {
	lines := strings.Count(value, "\n")
	if lines == 0 || !strings.HasSuffix(value, "\n") {
		lines++
	}
	return len(value) <= fileDiffMaxBytes && lines <= fileDiffMaxLines
}

func (d *fileDocument) rejectComparisonLimit() {
	d.status = d.owner.messages.FileWindow.Ready
	d.problem = d.owner.messages.FileWindow.CompareLimit
	d.notices.show(d.name, d.problem, components.ToastWarning)
	d.invalidateWindows()
}

func (d *fileDocument) openComparisonWindow() error {
	if d.comparison.window != nil {
		d.comparison.window.Show()
		return nil
	}
	window, err := d.owner.options.OpenContentWindow(d.comparisonConfig(), nil)
	if err != nil {
		return err
	}
	d.comparison.window = window
	return nil
}

func (d *fileDocument) comparisonConfig() app.Config {
	return app.Config{
		Title: fmt.Sprintf(d.owner.messages.FileWindow.CompareWindowTitle, d.name),
		Icon:  AppIcon(), Width: 1120, Height: 720, MinWidth: fileWindowMinWidth, MinHeight: fileWindowMinHeight, Tree: Tree(),
		Paint: d.paintComparison, TextInput: d.comparisonTextInput, OnPointer: d.comparisonPointer,
		OnKey: d.comparisonKey, OnShortcut: d.comparisonShortcut, OnScroll: d.comparisonScroll,
		OnCloseRequest: d.requestComparisonClose, OnClosed: d.onComparisonClosed,
	}
}

func (d *fileDocument) requestComparisonClose(allow func()) {
	if d.comparison.confirm {
		d.comparison.confirm = false
		d.comparison.ui.tooltip = components.HoverTooltip{}
		d.invalidateWindows()
		return
	}
	if d.busy || d.comparison.saving {
		return
	}
	d.resetComparison()
	d.status = d.owner.messages.FileWindow.Ready
	d.problem = ""
	d.closeAfter = false
	d.allowClose = nil
	d.invalidateWindows()
	allow()
}

func (d *fileDocument) onComparisonClosed() {
	if d.comparison.window == nil {
		return
	}
	d.resetComparison()
	d.closeAfter = false
	d.allowClose = nil
	d.invalidateWindows()
}

func (d *fileDocument) invalidateWindows() {
	if d.window != nil {
		d.window.Invalidate()
	}
	if d.comparison.window != nil {
		d.comparison.window.Invalidate()
	}
}

func (d *fileDocument) scheduleDiff() {
	c := &d.comparison
	if !c.active || c.loading || c.conflict {
		return
	}
	value := d.editor.String()
	if !fileDiffWithinLimit(value) {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.generation++
		c.pending = false
		if !c.oversized {
			d.problem = d.owner.messages.FileWindow.CompareLimit
			d.notices.show(d.name, d.problem, components.ToastWarning)
		}
		c.oversized = true
		d.invalidateWindows()
		return
	}
	if c.oversized {
		c.oversized = false
		if d.problem == d.owner.messages.FileWindow.CompareLimit {
			d.problem = ""
		}
	}
	if c.computedSet && value == c.computedValue {
		if c.timer != nil {
			c.timer.Stop()
			c.timer = nil
		}
		c.generation++
		c.pending = false
		d.invalidateWindows()
		return
	}
	if c.timer != nil {
		c.timer.Stop()
	}
	c.generation++
	generation := c.generation
	c.pending = true
	c.timer = time.AfterFunc(fileDiffDebounce, func() {
		d.owner.postFileEvent(fileDocumentEvent{document: d, diffTick: true, diffGeneration: generation})
	})
	d.invalidateWindows()
}

func (d *fileDocument) startDiff(generation uint64) {
	c := &d.comparison
	if !c.active || c.loading || c.conflict || generation != c.generation {
		return
	}
	c.timer = nil
	left, right := c.baseline, d.editor.String()
	if !fileDiffWithinLimit(right) {
		c.oversized = true
		c.pending = false
		d.invalidateWindows()
		return
	}
	go func() {
		result := components.DiffCodeLines(left, right)
		d.owner.postFileEvent(fileDocumentEvent{
			document: d, diff: result, diffRight: right, diffReady: true, diffGeneration: generation,
		})
	}()
}

func (d *fileDocument) finishDiff(event fileDocumentEvent) {
	c := &d.comparison
	if !c.active || c.loading || c.conflict || event.diffGeneration != c.generation {
		return
	}
	c.result = event.diff
	c.computedValue = event.diffRight
	c.computedSet = true
	c.pending = false
	c.timer = nil
	d.status = d.owner.messages.FileWindow.CompareReady
	d.invalidateWindows()
}

func (d *fileDocument) stopComparison() {
	window := d.comparison.window
	d.resetComparison()
	if window != nil {
		window.Close()
	}
}

func (d *fileDocument) resetComparison() {
	c := &d.comparison
	if c.timer != nil {
		c.timer.Stop()
		c.timer = nil
	}
	c.generation++
	c.active = false
	c.loading = false
	c.pending = false
	c.conflict = false
	c.saving = false
	c.confirm = false
	c.oversized = false
	c.baseline = ""
	c.revision = sftpclient.Revision{}
	c.result = components.CodeDiffResult{}
	c.computedValue = ""
	c.computedSet = false
	c.window = nil
}

func (d *fileDocument) resumePendingClose() {
	if !d.closeAfter || d.busy || d.comparison.saving {
		return
	}
	d.closeAfter = false
	allow := d.allowClose
	d.allowClose = nil
	if allow == nil && d.window != nil {
		allow = d.window.Close
	}
	if allow != nil {
		d.requestClose(allow)
	}
}

func (d *fileDocument) cancelComparison() {
	if d.busy || d.comparison.confirm {
		return
	}
	d.stopComparison()
	d.problem = ""
	d.status = d.owner.messages.FileWindow.Ready
	d.closeAfter = false
	d.allowClose = nil
}

func (d *fileDocument) confirmComparisonUpload() {
	if !d.canUploadComparison() {
		return
	}
	d.comparison.confirm = true
	d.invalidateWindows()
}

func (d *fileDocument) canUploadComparison() bool {
	c := d.comparison
	return c.active && !c.loading && !c.pending && !c.conflict && !c.oversized && !c.saving &&
		c.result.Stats.Added+c.result.Stats.Removed > 0
}

func (d *fileDocument) uploadComparison() {
	if d.busy || !d.canUploadComparison() {
		return
	}
	data, err := filecontent.Encode(d.editor.String(), d.format)
	if err != nil {
		d.problem = fmt.Sprintf(d.owner.messages.FileWindow.EncodingLoss, d.format.Encoding)
		if !errors.Is(err, filecontent.ErrEncoding) {
			d.problem = d.owner.fileProblem(err)
		}
		d.notices.show(d.name, d.problem, components.ToastError)
		return
	}
	d.comparison.confirm = false
	d.pending = d.editor.String()
	d.busy = true
	d.comparison.saving = true
	d.status = d.owner.messages.FileWindow.Uploading
	d.problem = ""
	if !d.worker.SaveDocument(d.location, data, d.comparison.revision) {
		d.busy = false
		d.comparison.saving = false
		d.problem = d.owner.messages.SFTPUnavailable
		d.notices.show(d.name, d.problem, components.ToastError)
	}
	d.invalidateWindows()
}

func (state *State) closeFileWindows() {
	m := &state.fileWindows
	m.mu.Lock()
	m.closed = true
	m.events = nil
	m.mu.Unlock()
	for _, d := range m.documents {
		d.afterClosed = nil
		d.stopComparison()
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
	return state.confirmSFTPWindows(func() {
		if !state.ConfirmDocumentClose(allow) {
			allow()
		}
	})
}

func (state *State) fileOpenNotice(origin *sftpWindow, title, detail string, kind components.ToastType) {
	if origin != nil && !origin.closed && origin.window != nil {
		origin.notices.show(title, detail, kind)
		origin.window.Invalidate()
		return
	}
	state.fileNotices.show(title, detail, kind)
	state.options.Invalidate()
}

func (state *State) fileOpenPathReason(origin *sftpWindow, title, format, location, reason string, kind components.ToastType) {
	if origin != nil && !origin.closed && origin.window != nil {
		origin.notices.showPathReason(title, format, location, reason, kind)
		origin.window.Invalidate()
		return
	}
	state.fileNotices.showPathReason(title, format, location, reason, kind)
	state.options.Invalidate()
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
	return app.Config{Title: d.name + " — " + d.source, Icon: AppIcon(), Width: 1000, Height: 760, MinWidth: fileWindowMinWidth, MinHeight: fileWindowMinHeight, Tree: Tree(), Paint: d.paint, TextInput: d.textInput, OnPointer: d.pointer, OnKey: d.key, OnShortcut: d.shortcut, OnScroll: d.scroll, OnCloseRequest: d.requestClose, OnClosed: d.onClosed}
}
