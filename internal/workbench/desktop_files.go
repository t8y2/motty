package workbench

import (
	"errors"
	"fmt"
	"io"
	"os"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"

	"motty/internal/desktop"
)

// Files go from this computer to a remote desktop through the remote clipboard:
// the user copies files here (or drops them on the picture) and pastes them in
// the remote system, which then reads them from this computer (RDP's clipboard
// file transfer). The list is made off the goroutine that paints, because it
// walks folders, and what happened is shown when the result is picked up.

// desktopFiles is a desktop session's side of that transfer.
type desktopFiles struct {
	// remote is the session when its protocol can take files, nil for one that
	// cannot (VNC).
	remote desktop.FileClipboard
	// sentKey says what was last offered or refused, so the same copied files
	// are not offered again by every click; it is empty while the clipboard holds
	// no files, so copying them again offers them again.
	sentKey string
	// dropped says the remote clipboard holds files the user dropped on the
	// picture, which the local clipboard has not been changed since: a paste
	// shortcut pastes those. pasteAfter says a paste was asked for while the files
	// were still being offered, and goes to the remote once they are.
	dropped, pasteAfter bool

	// receiving is how far the files the remote copied have been saved, while
	// isReceiving; receiveText is its card's byte line.
	receiving   desktop.FileProgress
	isReceiving bool
	receiveText transferText

	// promised says the remote clipboard's files are on this computer's
	// clipboard as promises, promiseVersion names the remote list they read,
	// and promiseSeq names the promise last made. promiseReleased is the
	// platform's release callback, which may run on any goroutine: it holds the
	// seq of the promise the clipboard no longer has.
	promised        bool
	promiseVersion  uint64
	promiseSeq      uint64
	promiseReleased atomic.Uint64

	mu      sync.Mutex
	running bool
	queued  *fileOfferRequest // the latest request made while another was running
	results []fileOfferResult // finished, not yet shown
}

// fileOfferRequest is files to offer: the copied files the clipboard sync found
// (key is their filesKey), or files the user dropped on the picture (no key), whose
// outcome is told with a notice.
type fileOfferRequest struct {
	paths    []string
	key      string
	explicit bool
	dropped  bool
}

type fileOfferResult struct {
	request fileOfferRequest
	offer   desktop.FileOffer
	err     error
}

// busy reports whether an offer is being made or its outcome is waiting to be shown.
func (files *desktopFiles) busy() bool {
	files.mu.Lock()
	defer files.mu.Unlock()
	return files.running || len(files.results) > 0
}

// filesKey identifies a list of copied files by their paths and what the top
// level of it was last changed at, so that a file edited since it was offered
// is offered again: its old list would be refused when the remote pastes it.
func filesKey(paths []string) string {
	var key strings.Builder
	for _, path := range paths {
		key.WriteString(path)
		if info, err := os.Stat(path); err == nil {
			key.WriteByte('\x00')
			key.WriteString(strconv.FormatInt(info.Size(), 10))
			key.WriteByte('\x00')
			key.WriteString(strconv.FormatInt(info.ModTime().UnixNano(), 10))
		}
		key.WriteByte('\n')
	}
	return key.String()
}

// syncClipboardFiles offers the files the system clipboard holds to the remote
// and reports whether it holds any. Those files stand for the clipboard: a file
// manager also leaves the files' names there as text, which must not be sent
// instead of them.
func (state *State) syncClipboardFiles(view *desktopView) bool {
	paths, err := state.options.ClipboardFiles()
	if err != nil || len(paths) == 0 {
		view.files.sentKey = ""
		return false
	}
	key := filesKey(paths)
	if key == view.files.sentKey {
		return true
	}
	view.files.sentKey, view.files.dropped = key, false
	// The files replace what the remote clipboard held, so the next text or
	// picture is offered again even when it is the same as before the files.
	view.sentClip, view.sentImage = "", imageDigest{}
	state.offerDesktopFiles(view, fileOfferRequest{paths: paths, key: key})
	return true
}

// dropDesktopFiles offers files the user dropped on the picture. The local
// clipboard is taken as already seen, so the next click does not replace what
// was just dropped with a clipboard the user copied before: that changes the
// remote only once the user copies something new.
func (state *State) dropDesktopFiles(view *desktopView, paths []string) {
	if text, err := state.options.ClipboardText(); err == nil {
		view.sentClip = text
	}
	if copied, err := state.options.ClipboardFiles(); err == nil {
		view.files.sentKey = filesKey(copied)
	} else {
		view.files.sentKey = ""
	}
	// A picture the clipboard holds is taken as already seen too: the text path
	// above remembers the clipboard's words, but a clipboard of a picture and no
	// words has none to remember.
	if view.sentClip == "" {
		_, view.sentImage, _ = state.clipboardImage()
	}
	view.files.dropped = false
	// No key: what the clipboard sync last saw stays what it saw, so a click does
	// not offer the clipboard's own files over the dropped ones.
	state.offerDesktopFiles(view, fileOfferRequest{paths: paths, explicit: true, dropped: true})
}

// pasteFiles handles a paste shortcut when files are what should be pasted: the
// system clipboard holds files, or the files the user dropped are still the
// remote clipboard. The remote pastes them itself, so the shortcut becomes the
// remote's own: Ctrl+V, pressed once the files are on the remote clipboard. It
// reports false when the paste is a paste of text.
func (state *State) pasteFiles(view *desktopView) bool {
	files := &view.files
	if files.remote == nil || !view.clipboard || view.ended {
		return false
	}
	if paths, err := state.options.ClipboardFiles(); err == nil && len(paths) > 0 {
		if key := filesKey(paths); key != files.sentKey {
			files.sentKey, files.dropped = key, false
			state.offerDesktopFiles(view, fileOfferRequest{paths: paths, key: key})
		}
		return state.pressPasteWhenOffered(view)
	}
	if files.dropped {
		if text, _ := state.options.ClipboardText(); text == view.sentClip {
			return state.pressPasteWhenOffered(view)
		}
		files.dropped = false
	}
	return false
}

// pressPasteWhenOffered presses the remote's paste chord now, or after the offer
// that is still being made.
func (state *State) pressPasteWhenOffered(view *desktopView) bool {
	if view.files.busy() {
		view.files.pasteAfter = true
		return true
	}
	view.pasteChord()
	return true
}

// pasteChord presses Ctrl+V on the remote side and lets go of it.
func (view *desktopView) pasteChord() {
	view.remoteControlChord('v')
}

// offerDesktopFiles makes the list of files and gives it to the remote on a
// goroutine of its own. A request made while one is running replaces any waiting.
func (state *State) offerDesktopFiles(view *desktopView, request fileOfferRequest) {
	files := &view.files
	if files.remote == nil || view.ended || !view.clipboard {
		if request.explicit {
			message := state.messages.Tools.DesktopFilesUnsupported
			if files.remote != nil && !view.clipboard {
				message = state.messages.Tools.DesktopFilesClipboardOff
			}
			state.fileOpenNotice(nil, state.messages.Tools.DesktopFilesTitle, message, components.ToastWarning)
		}
		return
	}
	files.mu.Lock()
	if files.running {
		files.queued = &request
		files.mu.Unlock()
		return
	}
	files.running = true
	files.mu.Unlock()
	remote, invalidate := files.remote, state.options.Invalidate
	go func() {
		for {
			offer, err := remote.SetClipboardFiles(request.paths)
			files.mu.Lock()
			files.results = append(files.results, fileOfferResult{request: request, offer: offer, err: err})
			next := files.queued
			files.queued = nil
			if next == nil {
				files.running = false
			}
			files.mu.Unlock()
			if invalidate != nil {
				invalidate()
			}
			if next == nil {
				return
			}
			request = *next
		}
	}()
}

// finishFileOffers shows the outcome of the offers that have finished. A drop
// says what happened with a notice; copied files that the clipboard sync offered
// say it on the status line, and fail silently, since the user only copied.
func (state *State) finishFileOffers(view *desktopView) {
	files := &view.files
	files.mu.Lock()
	results := files.results
	files.results = nil
	files.mu.Unlock()
	pasted := false
	for _, result := range results {
		if result.request.key != "" && !errors.Is(result.err, desktop.ErrClipboardNotReady) {
			files.sentKey = result.request.key
		}
		if result.err == nil {
			files.dropped = result.request.dropped
			pasted = true
		} else if result.request.dropped {
			files.dropped = false
		}
		if errors.Is(result.err, desktop.ErrClipboardNotReady) && !result.request.explicit {
			files.sentKey = "" // the channel is still starting: offer again at the next sync
		}
		if result.err != nil {
			if result.request.explicit {
				state.fileOpenNotice(nil, state.messages.Tools.DesktopFilesTitle, state.desktopFilesError(result.err, result.offer), components.ToastError)
			}
			continue
		}
		text := state.desktopFilesOffered(result.offer)
		if result.request.explicit {
			state.fileOpenNotice(nil, state.messages.Tools.DesktopFilesTitle, text, components.ToastSuccess)
		} else {
			state.status = text
		}
	}
	if files.pasteAfter && len(results) > 0 {
		files.pasteAfter = false
		if pasted {
			view.pasteChord()
		}
	}
}

// receivingFiles reports whether the session is saving files its remote copied,
// which a progress card shows.
func (view *desktopView) receivingFiles() bool {
	return view != nil && !view.ended && view.files.isReceiving
}

// pumpReceivingFiles reads how far the files the remote copied have been saved.
func (state *State) pumpReceivingFiles(view *desktopView, receiver desktop.FileReceiver) {
	progress, ok := receiver.ReceivingFiles()
	files := &view.files
	if ok == files.isReceiving && progress == files.receiving {
		return
	}
	files.receiving, files.isReceiving = progress, ok
	state.reportTransferProgress()
}

// declareDesktopReceiveCard is the progress card of files the remote copied
// that are being saved here, before they are put on this computer's clipboard.
func (state *State) declareDesktopReceiveCard(c ui.C, session *Session, slot int) {
	files := &session.desk.files
	progress := files.receiving
	state.declareProgressCard(c, slot, progressCard{
		title: state.messages.Tools.TransferReceiveTitle, address: session.Address, name: progress.Name,
		line: files.receiveText.format(progress.Done, progress.Size), fraction: progressFraction(progress.Done, progress.Size),
		cancel: hitDesktopReceiveCancelFor.At(int(session.ID)),
	})
}

// clickDesktopReceiveCancel is the cancel of that card: the files are not saved
// and this computer's clipboard keeps what it held.
func (state *State) clickDesktopReceiveCancel(id gfx.ID) bool {
	n, ok := id.In(hitDesktopReceiveCancelFor)
	if !ok || n <= 0 {
		return false
	}
	session, ok := state.sessionByIDPointer(uint32(n))
	if !ok || !session.desk.receivingFiles() {
		return true
	}
	if receiver, ok := session.desk.session.(desktop.FileReceiver); ok {
		receiver.CancelReceivingFiles()
	}
	session.desk.files.isReceiving = false
	state.status = state.messages.Tools.TransferCancelled
	state.reportTransferProgress()
	return true
}

// receiveDesktopFiles puts files the remote copied on this computer's
// clipboard. The files stand for the clipboard from then on: the sync that
// offers the local clipboard to the remote must not send them back.
func (state *State) receiveDesktopFiles(view *desktopView, files desktop.ReceivedFiles) {
	m := state.messages.Tools
	if len(files.Paths) == 0 {
		if files.Err != nil {
			state.status = fmt.Sprintf(m.DesktopFilesReceiveFailedFmt, files.Err)
		}
		return
	}
	if state.options.SetClipboardFiles == nil {
		return
	}
	previousKey := view.files.sentKey
	view.files.sentKey = filesKey(files.Paths)
	if err := state.options.SetClipboardFiles(files.Paths); err != nil {
		view.files.sentKey = previousKey
		state.status = fmt.Sprintf(m.DesktopFilesReceiveFailedFmt, err)
		return
	}
	view.files.dropped = false
	// Remember what the system clipboard now reports, not only the paths that
	// were put there: a platform may normalize them (a resolved symlink, for
	// one), and the next sync compares against that same report.
	view.files.sentKey = filesKey(files.Paths)
	if copied, err := state.options.ClipboardFiles(); err == nil && len(copied) > 0 {
		view.files.sentKey = filesKey(copied)
	}
	if text, err := state.options.ClipboardText(); err == nil {
		view.sentClip = text
	}
	text := fmt.Sprintf(m.DesktopFilesReceivedFmt, files.Files+files.Folders, formatBytes(int64(files.Bytes)))
	if files.Skipped > 0 {
		text += " " + fmt.Sprintf(m.DesktopFilesReceivedSkippedFmt, files.Skipped)
	}
	state.status = text
}

// clipboardFilePromises reports whether the system clipboard can hold files
// whose content is read when the user pastes them.
func (state *State) clipboardFilePromises() bool {
	if state.options.ClipboardFilePromises != nil {
		return state.options.ClipboardFilePromises()
	}
	return app.Supports(app.CapabilityClipboardFilePromise)
}

// pumpDesktopFileList takes the file list the remote system put on its
// clipboard and promises it to this computer's clipboard, so copying files in
// the remote session transfers nothing until they are pasted here. When the
// clipboard no longer holds that promise, the server's data is let go.
func (state *State) pumpDesktopFileList(view *desktopView, lister desktop.FileLister) {
	files := &view.files
	if version := files.promiseReleased.Swap(0); version != 0 && files.promised && version == files.promiseSeq {
		files.promised = false
		lister.ReleaseRemoteFileList(files.promiseVersion)
	}
	list, ok := lister.RemoteFileList()
	if !ok {
		return
	}
	if list.Err != nil {
		state.status = fmt.Sprintf(state.messages.Tools.DesktopFilesReceiveFailedFmt, list.Err)
		lister.ReleaseRemoteFileList(list.Version)
		return
	}
	state.promiseDesktopFiles(view, lister, list)
}

// promiseDesktopFiles puts the remote list on the local clipboard as a promise.
// A tree the platform cannot describe (too many entries, a name the shell
// refuses) is downloaded instead, so the files still reach this computer.
func (state *State) promiseDesktopFiles(view *desktopView, lister desktop.FileLister, list desktop.ClipboardFileList) {
	promises, err := desktopFilePromises(lister, list.Version, list.Files)
	if err == nil {
		files := &view.files
		files.promiseSeq++
		seq := files.promiseSeq
		released := func() {
			files.promiseReleased.Store(seq)
			if state.options.Invalidate != nil {
				state.options.Invalidate()
			}
		}
		if setErr := state.options.SetClipboardFilePromises(promises, released); setErr == nil {
			files.promised, files.promiseVersion = true, list.Version
			return
		} else {
			err = setErr
		}
	}
	if downloadErr := lister.DownloadRemoteFileList(list.Version); downloadErr != nil {
		state.status = fmt.Sprintf(state.messages.Tools.DesktopFilesReceiveFailedFmt, downloadErr)
	}
}

// desktopFilePromises turns the remote's flat list into the promise tree the
// clipboard takes: folders with their children, and each file opening the
// server's bytes a range at a time when a paste reads it.
func desktopFilePromises(lister desktop.FileLister, version uint64, entries []desktop.FileListing) ([]app.FilePromise, error) {
	if len(entries) > app.MaxDragFiles {
		return nil, fmt.Errorf("%d files and folders", len(entries))
	}
	type node struct {
		name     string
		index    int
		file     bool
		size     int64
		children map[string]*node
		order    []string
	}
	root := &node{index: -1, children: make(map[string]*node)}
	for index, entry := range entries {
		parts, ok := promiseNameParts(entry.Name)
		if !ok {
			return nil, fmt.Errorf("the remote name %q cannot be one", entry.Name)
		}
		current := root
		for _, part := range parts[:len(parts)-1] {
			child := current.children[part]
			if child == nil {
				child = &node{name: part, index: -1, children: make(map[string]*node)}
				current.children[part] = child
				current.order = append(current.order, part)
			}
			if child.file {
				return nil, fmt.Errorf("the remote name %q is inside a file", entry.Name)
			}
			current = child
		}
		name := parts[len(parts)-1]
		child := current.children[name]
		if child == nil {
			child = &node{name: name, index: -1, children: make(map[string]*node)}
			current.children[name] = child
			current.order = append(current.order, name)
		}
		if entry.Dir {
			if child.file {
				return nil, fmt.Errorf("the remote list names %q as a file and a folder", entry.Name)
			}
			continue
		}
		if child.file || len(child.order) > 0 {
			return nil, fmt.Errorf("the remote list names %q twice", entry.Name)
		}
		child.file, child.index, child.size = true, index, entry.Size
	}
	var build func(order []string, children map[string]*node) []app.FilePromise
	build = func(order []string, children map[string]*node) []app.FilePromise {
		out := make([]app.FilePromise, 0, len(order))
		for _, name := range order {
			child := children[name]
			if child.file {
				out = append(out, app.FilePromise{
					Name: child.name, Size: child.size,
					Open: remoteFileOpener(lister, version, child.index, child.size),
				})
				continue
			}
			kids := build(child.order, child.children)
			out = append(out, app.FilePromise{Name: child.name, Directory: true, List: func() ([]app.FilePromise, error) {
				return kids, nil
			}})
		}
		return out
	}
	return build(root.order, root.children), nil
}

// promiseNameParts splits a descriptor's relative name into the one path
// component per level the clipboard's promise tree takes, refusing anything
// that could name a place outside it.
func promiseNameParts(name string) ([]string, bool) {
	name = strings.ReplaceAll(name, `\`, "/")
	if name == "" || strings.HasPrefix(name, "/") || strings.IndexByte(name, 0) >= 0 {
		return nil, false
	}
	parts := strings.Split(name, "/")
	if len(parts) > 32 {
		return nil, false
	}
	for _, part := range parts {
		if part == "" || part == "." || part == ".." {
			return nil, false
		}
	}
	return parts, true
}

// remoteFileOpener is a file promise's Open: it reads the server's bytes when a
// paste asks for them, not before.
func remoteFileOpener(lister desktop.FileLister, version uint64, index int, size int64) func() (io.ReadCloser, error) {
	return func() (io.ReadCloser, error) {
		return &remoteFileReader{lister: lister, version: version, index: index, size: size}, nil
	}
}

// remoteFileReader reads one promised file from the server a range at a time.
// The clipboard asks for it in buffers of its own size; each Read returns what
// one request carries, which a reader may do as long as it makes progress.
type remoteFileReader struct {
	lister  desktop.FileLister
	version uint64
	index   int
	size    int64
	offset  int64
}

// maxReadRange bounds one request: MS-RDPECLIP carries at most 64 KiB of a
// file per message, and Windows asks for no more.
const maxReadRange = 64 << 10

func (reader *remoteFileReader) Read(p []byte) (int, error) {
	if reader.offset >= reader.size {
		return 0, io.EOF
	}
	if len(p) == 0 {
		return 0, nil
	}
	length := int64(len(p))
	if length > maxReadRange {
		length = maxReadRange
	}
	if remaining := reader.size - reader.offset; length > remaining {
		length = remaining
	}
	data, err := reader.lister.ReadRemoteFile(reader.version, reader.index, reader.offset, int(length))
	if err != nil {
		return 0, err
	}
	if len(data) == 0 {
		return 0, io.ErrUnexpectedEOF
	}
	n := copy(p, data)
	reader.offset += int64(n)
	return n, nil
}

func (reader *remoteFileReader) Close() error { return nil }

// desktopFilesOffered tells what was put on the remote clipboard, and what was left out.
func (state *State) desktopFilesOffered(offer desktop.FileOffer) string {
	m := state.messages.Tools
	text := fmt.Sprintf(m.DesktopFilesOfferedFmt, offer.Files+offer.Folders, formatBytes(int64(offer.Bytes)))
	if offer.Skipped > 0 {
		text += " " + fmt.Sprintf(m.DesktopFilesSkippedFmt, offer.Skipped, offer.FirstSkipped)
	}
	return text
}

// desktopFilesError says why files could not be offered.
func (state *State) desktopFilesError(err error, offer desktop.FileOffer) string {
	m := state.messages.Tools
	switch {
	case errors.Is(err, desktop.ErrFilesUnsupported):
		return m.DesktopFilesUnsupported
	case errors.Is(err, desktop.ErrClipboardOff):
		return m.DesktopFilesClipboardOff
	case errors.Is(err, desktop.ErrClipboardNotReady):
		return m.DesktopFilesNotReady
	case errors.Is(err, desktop.ErrNothingToOffer):
		return m.DesktopFilesNothing
	case errors.Is(err, desktop.ErrTooManyFiles):
		return fmt.Sprintf(m.DesktopFilesTooManyFmt, desktop.MaxOfferFiles)
	case errors.Is(err, desktop.ErrPathTooLong):
		return fmt.Sprintf(m.DesktopFilesPathTooLongFmt, offer.FirstSkipped)
	}
	return fmt.Sprintf(m.DesktopFilesFailedFmt, err)
}

// desktopDropAt is the desktop a file dropped at a window point would be offered
// to, with the pane it fills: the picture of a session that is showing.
func (state *State) desktopDropAt(x, y float32) (*Session, gfx.Rect, bool) {
	session, bounds, ok := state.paneAt(x, y)
	if !ok || session.desk == nil || session.desk.ended {
		return nil, gfx.Rect{}, false
	}
	return session, bounds, true
}
