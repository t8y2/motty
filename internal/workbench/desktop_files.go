package workbench

import (
	"errors"
	"fmt"
	"os"
	"strconv"
	"strings"
	"sync"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"

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
	for _, event := range [...]desktop.KeyEvent{
		{Key: desktop.KeyControl, Down: true}, {Rune: 'v', Down: true}, {Rune: 'v'}, {Key: desktop.KeyControl},
	} {
		_ = view.session.SendKey(event)
	}
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
