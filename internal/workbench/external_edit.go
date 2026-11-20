package workbench

import (
	"bytes"
	"crypto/sha256"
	"errors"
	"fmt"
	"os"
	"path"
	"path/filepath"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/components"
	"motty/internal/sftpclient"
)

// Editing a remote text file in the system's editor. The file is read through
// the panel's SFTP worker (the same bounded UTF-8 read the built-in editor
// uses), written to a private temporary folder and opened with the system. While
// the edit is open a once-a-second look at that one file finds saves; each is
// sent back with the content-identity check the built-in editor uses, so a file
// that changed on the server in the meantime is never replaced unasked. The look
// runs only while an edit is open, and the temporary folder goes with the edit.

var (
	hitSFTPEditExternal = tree.ID(sftpHits, "sftp-edit-external")
)

const (
	defaultEditPollInterval = time.Second
)

// externalEdit is one remote file open in the system's editor.
type externalEdit struct {
	origin  *sftpWindow
	id      uint32
	session uint32
	remote  string
	name    string
	dir     string
	local   string

	// revision and bom are what the server's copy was when last read or sent.
	revision sftpclient.Revision
	bom      bool
	// synced is the content of the local file the server has.
	synced [sha256.Size]byte

	// seen is the file's size and time as last looked at; dirty says it differs
	// from synced and is sent once it stops changing.
	seenSize int64
	seenTime time.Time
	dirty    bool
	// saving says a send is on its way; worker is the panel worker it was given
	// to, so a connection that ends mid-send cannot leave saving set forever and
	// every later save skipped.
	saving bool
	worker *sftpclient.Worker
	// sending is the content being sent, which becomes synced when it lands.
	sending [sha256.Size]byte
	// conflicted says the server's copy changed under a send; asking says the
	// user is being asked what to do about it.
	conflicted, asking bool
}

type externalEdits struct {
	list []externalEdit
	next uint32
	// opening is the read that starts an edit, or, with replace set, the one that
	// refreshes the revision of an edit the user chose to send over a conflict.
	// worker is what it was asked of, so a read whose connection ended cannot
	// keep every later edit of the run waiting for an answer.
	opening struct {
		origin  *sftpWindow
		active  bool
		session uint32
		remote  string
		replace uint32
		worker  *sftpclient.Worker
	}
	watching bool
	stop     chan struct{}
}

// safeLocalName keeps what a remote name may be as a local file name; see
// sftpclient.SafeLocalName, which the transfer worker also uses for the
// children of a downloaded folder.
func safeLocalName(remote string) string {
	return sftpclient.SafeLocalName(remote)
}

func (state *State) editByID(id uint32) *externalEdit {
	for index := range state.edits.list {
		if state.edits.list[index].id == id {
			return &state.edits.list[index]
		}
	}
	return nil
}

func (state *State) editForRemote(session uint32, remote string) *externalEdit {
	for index := range state.edits.list {
		if edit := &state.edits.list[index]; edit.session == session && edit.remote == remote {
			return edit
		}
	}
	return nil
}

// startExternalEdit reads the selected remote file for the system's editor.
func (state *State) startExternalEdit(session *Session, view *sftpView) {
	entry, ok := state.selectedSFTPEntry(view)
	if !ok || entry.IsDir() || view.local || view.worker == nil {
		return
	}
	if !state.externalEditReady() || view.busy {
		return
	}
	remote := path.Join(view.path, entry.Name)
	if existing := state.editForRemote(session.ID, remote); existing != nil {
		// Already open: bring the editor back to it instead of making a copy.
		// When a save from that copy never reached the server, say so: what the
		// editor shows is then not what the server has.
		if externalEditHeldLocally(existing) {
			message := fmt.Sprintf(state.messages.Tools.ExtEditUnsentFmt, existing.name)
			view.errorText = message
			state.fileOpenNotice(view.detached, existing.name, message, components.ToastWarning)
		}
		if err := state.openWithConfiguredEditor(existing.local); err != nil {
			view.errorText = fmt.Sprintf(state.messages.Tools.ExtEditOpenFailedFmt, entry.Name, err)
		}
		return
	}
	if !view.worker.OpenText(remote) {
		view.errorText = state.messages.SFTPUnavailable
		return
	}
	state.edits.opening.active, state.edits.opening.session, state.edits.opening.remote, state.edits.opening.replace = true, session.ID, remote, 0
	state.edits.opening.origin = view.detached
	state.edits.opening.worker = view.worker
	view.status = fmt.Sprintf(state.messages.SFTPEditOpeningFmt, entry.Name)
	view.errorText = ""
}

// externalEditReady reports whether a new edit may start. A read whose worker
// ended without an answer is dropped here: its result is published nowhere, so
// it would otherwise block every later edit for the rest of the app run.
func (state *State) externalEditReady() bool {
	opening := &state.edits.opening
	if !opening.active {
		return true
	}
	if opening.worker == nil {
		return false
	}
	select {
	case <-opening.worker.Done():
		opening.active, opening.worker = false, nil
		return true
	default:
		return false
	}
}

// externalEditResult takes the worker results that belong to an external edit.
// It reports whether it did.
func (state *State) externalEditResult(session *Session, view *sftpView, result sftpclient.Result) bool {
	m := state.messages.Tools
	opening := &state.edits.opening
	switch result.Operation {
	case sftpclient.OperationOpenText:
		// The session and the path are the identity of the read; the window it
		// was asked from only decides where the answer is shown, so a tab moving
		// between windows while the file is read cannot lose the answer.
		if !opening.active || opening.session != session.ID || opening.remote != result.Path {
			return false
		}
		replace := opening.replace
		opening.active, opening.replace, opening.worker = false, 0, nil
		if result.Err != nil {
			view.errorText = fmt.Sprintf(state.messages.SFTPErrorFmt, result.Err)
			return true
		}
		if replace != 0 {
			if edit := state.editByID(replace); edit != nil {
				edit.revision, edit.bom, edit.asking = result.Document.Revision, result.Document.UTF8BOM, false
				state.sendExternalEdit(edit)
			}
			return true
		}
		state.openInSystemEditor(session, view, result)
		return true
	case sftpclient.OperationSaveText:
		edit := state.editForRemote(session.ID, result.Path)
		if edit == nil || !edit.saving {
			return false
		}
		edit.saving, edit.worker = false, nil
		switch {
		case result.Err == nil:
			edit.revision, edit.bom, edit.synced = result.Document.Revision, result.Document.UTF8BOM, edit.sending
			if edit.origin != nil {
				view.refreshAfterTransfers = true
			}
			view.status = fmt.Sprintf(m.ExtEditSentFmt, edit.name)
			view.errorText = ""
			state.fileOpenNotice(view.detached, edit.name, view.status, components.ToastSuccess)
			state.refreshSFTPAfterRemoteSave(view, result.Path)
		case errors.Is(result.Err, sftpclient.ErrRemoteChanged):
			edit.conflicted, edit.dirty = true, false
		default:
			view.errorText = fmt.Sprintf(m.ExtEditSendFailedFmt, edit.name, result.Err)
			state.fileOpenNotice(view.detached, edit.name, view.errorText, components.ToastError)
		}
		return true
	}
	return false
}

func (state *State) openInSystemEditor(session *Session, view *sftpView, result sftpclient.Result) {
	m := state.messages.Tools
	name := safeLocalName(result.Path)
	dir, err := os.MkdirTemp("", "motty-edit-")
	if err != nil {
		view.errorText = fmt.Sprintf(m.ExtEditOpenFailedFmt, name, err)
		return
	}
	content := make([]byte, 0, len(result.Document.Text)+3)
	if result.Document.UTF8BOM {
		content = append(content, 0xef, 0xbb, 0xbf)
	}
	content = append(content, result.Document.Text...)
	local := filepath.Join(dir, name)
	if err := os.WriteFile(local, content, 0o600); err != nil {
		_ = os.RemoveAll(dir)
		view.errorText = fmt.Sprintf(m.ExtEditOpenFailedFmt, name, err)
		return
	}
	info, err := os.Stat(local)
	if err == nil {
		err = state.openWithConfiguredEditor(local)
	}
	if err != nil {
		_ = os.RemoveAll(dir)
		if isExecutablePathError(err) {
			view.errorText = m.ExtEditRefused
		} else {
			view.errorText = fmt.Sprintf(m.ExtEditOpenFailedFmt, name, err)
		}
		return
	}
	state.edits.next++
	state.edits.list = append(state.edits.list, externalEdit{
		origin: view.detached,
		id:     state.edits.next, session: session.ID, remote: result.Path, name: name, dir: dir, local: local,
		revision: result.Document.Revision, bom: result.Document.UTF8BOM, synced: sha256.Sum256(content),
		seenSize: info.Size(), seenTime: info.ModTime(),
	})
	view.status = fmt.Sprintf(m.ExtEditOpenedFmt, name)
	view.errorText = ""
	state.watchExternalEdits()
}

// watchExternalEdits starts the once-a-second look, which runs while there is an
// edit to look at and ends with the last one.
func (state *State) watchExternalEdits() {
	edits := &state.edits
	if edits.watching || len(edits.list) == 0 {
		return
	}
	edits.watching, edits.stop = true, make(chan struct{})
	stop := edits.stop
	interval := state.options.EditPollInterval
	go func() {
		ticker := time.NewTicker(interval)
		defer ticker.Stop()
		for {
			select {
			case <-stop:
				return
			case <-ticker.C:
				if !state.aiPost(func(state *State) { state.pollExternalEdits() }) {
					return
				}
			}
		}
	}()
}

func (state *State) stopWatchingExternalEdits() {
	if state.edits.watching {
		close(state.edits.stop)
		state.edits.watching, state.edits.stop = false, nil
	}
}

// pollExternalEdits looks at each open edit's file once.
func (state *State) pollExternalEdits() {
	for index := range state.edits.list {
		edit := &state.edits.list[index]
		if edit.saving {
			state.settleExternalSend(edit)
		}
		info, err := os.Stat(edit.local)
		if err != nil {
			continue // the editor may be replacing the file; the next look sees it
		}
		changed := info.Size() != edit.seenSize || !info.ModTime().Equal(edit.seenTime)
		switch {
		case changed:
			edit.seenSize, edit.seenTime = info.Size(), info.ModTime()
			edit.dirty = true
		case edit.dirty && !edit.saving:
			// It has not changed since the last look: the save is done.
			edit.dirty = false
			state.sendExternalEdit(edit)
		}
	}
	state.askExternalConflict()
}

// settleExternalSend gives up on a save whose worker ended before answering.
// The worker publishes nothing once it is closed, so saving would otherwise stay
// set and the edit would never be sent again; the file is looked at once more
// instead, which sends it on the same pass.
func (state *State) settleExternalSend(edit *externalEdit) {
	if edit.worker == nil {
		return
	}
	select {
	case <-edit.worker.Done():
		edit.saving, edit.worker = false, nil
		edit.dirty = true
		if info, err := os.Stat(edit.local); err == nil {
			edit.seenSize, edit.seenTime = info.Size(), info.ModTime()
		}
	default:
	}
}

// externalEditHeldLocally reports whether the local copy holds content the
// server does not have and nothing is on its way to it. Comparing the bytes is
// what makes this true after a save the server refused to take, and after a
// save the once-a-second look never recognised.
func externalEditHeldLocally(edit *externalEdit) bool {
	if edit.dirty || edit.saving {
		return false // a save is still on its way
	}
	data, err := os.ReadFile(edit.local)
	if err != nil {
		return false // the editor may be replacing the file; nothing to warn about
	}
	return sha256.Sum256(data) != edit.synced
}

// sendExternalEdit sends the file's current content back, when it is new text.
// Every way of not sending is reported: a copy the server never received must
// not look like a save.
func (state *State) sendExternalEdit(edit *externalEdit) {
	m := state.messages.Tools
	session, ok := state.sessionByIDPointer(edit.session)
	if edit.origin != nil {
		session, ok = &edit.origin.session, !edit.origin.closed
	}
	if !ok {
		state.reportExternalEdit(edit, fmt.Sprintf(m.ExtEditGoneFmt, edit.name))
		return
	}
	data, err := os.ReadFile(edit.local)
	if err != nil {
		state.reportExternalEdit(edit, fmt.Sprintf(m.ExtEditSendFailedFmt, edit.name, err))
		return
	}
	sum := sha256.Sum256(data)
	if sum == edit.synced {
		return
	}
	bom := bytes.HasPrefix(data, []byte{0xef, 0xbb, 0xbf})
	body := data
	if bom {
		body = data[3:]
	}
	if len(data) > sftpclient.MaxEditFileBytes || !utf8.Valid(body) || bytes.IndexByte(body, 0) >= 0 {
		state.reportExternalEdit(edit, fmt.Sprintf(m.ExtEditBadFmt, edit.name))
		return
	}
	if session.files == nil {
		state.openSFTP(session) // the panel was closed; the worker is made again
	}
	view := session.files
	if view == nil || view.worker == nil || !view.worker.SaveText(edit.remote, string(body), edit.revision, bom) {
		state.reportExternalEdit(edit, fmt.Sprintf(m.ExtEditSendFailedFmt, edit.name, state.messages.SFTPUnavailable))
		return
	}
	edit.saving, edit.sending, edit.worker = true, sum, view.worker
}

// reportExternalEdit says a save did not reach the server, where the user can
// see it, and leaves the editor and the local copy alone: the user decides
// whether to fix the cause and save again.
func (state *State) reportExternalEdit(edit *externalEdit, detail string) {
	if session, ok := state.sessionByIDPointer(edit.session); ok && session.files != nil {
		session.files.errorText = detail
		session.files.invalidateDetached()
	}
	state.fileOpenNotice(edit.origin, edit.name, detail, components.ToastError)
}

// askExternalConflict asks about one edit whose send found the server's copy
// changed. Replacing reads the server's file again to take its identity and then
// sends; keeping leaves the local file as it is until the next save.
func (state *State) askExternalConflict() {
	if state.modal != modalNone {
		return
	}
	for index := range state.edits.list {
		edit := &state.edits.list[index]
		if !edit.conflicted || edit.asking {
			continue
		}
		edit.asking = true
		id, m := edit.id, state.messages.Tools
		if edit.origin != nil {
			edit.origin.externalConflict = id
			edit.origin.reveal()
			return
		}
		state.openForm(formDialog{
			title: m.ExtEditConflictTitle, description: fmt.Sprintf(m.ExtEditConflictDescriptionFmt, edit.name),
			submit: m.ExtEditReplace, danger: true,
			run: func(state *State, _ *formDialog) string {
				state.replaceOverConflict(id)
				return ""
			},
			cancel: func(state *State) { state.keepAfterConflict(id) },
		})
		return
	}
}

// keepAfterConflict leaves the local file alone; the next save asks again.
func (state *State) keepAfterConflict(id uint32) {
	if edit := state.editByID(id); edit != nil {
		edit.asking, edit.conflicted = false, false
		if info, err := os.Stat(edit.local); err == nil {
			edit.seenSize, edit.seenTime = info.Size(), info.ModTime()
		}
	}
}

func (state *State) replaceOverConflict(id uint32) {
	edit := state.editByID(id)
	if edit == nil {
		return
	}
	session, ok := state.sessionByIDPointer(edit.session)
	if edit.origin != nil {
		session, ok = &edit.origin.session, !edit.origin.closed
	}
	if !ok || session.files == nil || session.files.worker == nil || !state.externalEditReady() || !session.files.worker.OpenText(edit.remote) {
		state.keepAfterConflict(id)
		return
	}
	state.edits.opening.active, state.edits.opening.session, state.edits.opening.remote, state.edits.opening.replace = true, edit.session, edit.remote, id
	state.edits.opening.origin = edit.origin
	state.edits.opening.worker = session.files.worker
	edit.asking, edit.conflicted = false, false
	if info, err := os.Stat(edit.local); err == nil {
		edit.seenSize, edit.seenTime = info.Size(), info.ModTime()
	}
}

// dropExternalEdits ends the edits of a session (all of them for 0) and removes
// their temporary files.
func (state *State) dropExternalEdits(session uint32) {
	kept := state.edits.list[:0]
	for _, edit := range state.edits.list {
		if session == 0 || edit.session == session {
			_ = os.RemoveAll(edit.dir)
			continue
		}
		kept = append(kept, edit)
	}
	clear(state.edits.list[len(kept):])
	state.edits.list = kept
	if state.edits.opening.active && (session == 0 || state.edits.opening.session == session) {
		state.edits.opening.active, state.edits.opening.worker = false, nil
	}
	if len(state.edits.list) == 0 {
		state.stopWatchingExternalEdits()
	}
}

func isExecutablePathError(err error) bool { return errors.Is(err, app.ErrExecutablePath) }

func (state *State) dropWindowExternalEdits(window *sftpWindow) {
	kept := state.edits.list[:0]
	for _, edit := range state.edits.list {
		if edit.origin == window {
			_ = os.RemoveAll(edit.dir)
			continue
		}
		kept = append(kept, edit)
	}
	clear(state.edits.list[len(kept):])
	state.edits.list = kept
	if state.edits.opening.origin == window {
		state.edits.opening.active, state.edits.opening.worker = false, nil
		state.edits.opening.origin = nil
	}
	if len(kept) == 0 {
		state.stopWatchingExternalEdits()
	}
}
