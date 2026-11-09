package workbench

import (
	"fmt"
	"math"
	"os"
	"path"
	"runtime"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"motty/internal/localshell"
	"motty/internal/sftpclient"
	"motty/internal/sshconn"
)

// paintSFTPDropOverlay is presentation only: it declares no hits, so the file
// row under the pointer remains the target of the eventual system drop. label
// says what the drop will do.
func (state *State) paintSFTPDropOverlay(canvas *gfx.Canvas, bounds gfx.Rect, label string) {
	if bounds.Width <= 0 || bounds.Height <= 0 {
		return
	}
	canvas.PushLayer(popupLayer)
	defer canvas.PopLayer()
	canvas.Clip(bounds)
	defer canvas.Unclip()

	gray := gfx.RGB(128, 128, 128)
	// Quiet the list below the hint without turning the drop area into a solid
	// slab. Both surfaces and the icon stay neutral in either theme.
	canvas.Fill(bounds, withAlpha(state.pal.workspace, 0.92))
	canvas.Fill(bounds, withAlpha(gray, 0.08))
	const stroke, dash, gap = float32(1.5), float32(8), float32(6)
	// End both edges with a complete dash so the four square corners meet.
	dashes := func(length float32, paint func(float32, float32)) {
		count := max(int(math.Round(float64((length+gap)/(dash+gap)))), 1)
		part := min(dash, length)
		step := float32(0)
		if count > 1 {
			step = (length - part) / float32(count-1)
		}
		for index := 0; index < count; index++ {
			paint(float32(index)*step, part)
		}
	}
	dashes(bounds.Width, func(offset, length float32) {
		canvas.Fill(gfx.Rect{X: bounds.X + offset, Y: bounds.Y, Width: length, Height: stroke}, gray)
		canvas.Fill(gfx.Rect{X: bounds.X + offset, Y: bounds.Y + bounds.Height - stroke, Width: length, Height: stroke}, gray)
	})
	dashes(bounds.Height, func(offset, length float32) {
		canvas.Fill(gfx.Rect{X: bounds.X, Y: bounds.Y + offset, Width: stroke, Height: length}, gray)
		canvas.Fill(gfx.Rect{X: bounds.X + bounds.Width - stroke, Y: bounds.Y + offset, Width: stroke, Height: length}, gray)
	})

	textStyle := gfx.TextStyle{Size: 15, Weight: gfx.WeightMedium, Color: state.pal.text}
	icon := gfx.Rect{Width: 36, Height: 36}
	text := gfx.Rect{X: bounds.X + 12, Width: max(bounds.Width-24, 0), Height: 22}
	if bounds.Height < 100 {
		// A folder row has room for a single centered icon-and-label line.
		textStyle.Size = 13
		icon.Width, icon.Height = 16, 16
		width := min(canvas.TextWidthWeight(label, textStyle.Size, textStyle.Weight), max(bounds.Width-44, 0))
		icon.X = bounds.X + (bounds.Width-icon.Width-8-width)/2
		icon.Y = bounds.Y + (bounds.Height-icon.Height)/2
		text = gfx.Rect{X: icon.X + icon.Width + 8, Y: bounds.Y + (bounds.Height-20)/2, Width: width, Height: 20}
	} else {
		const groupHeight = float32(72)
		icon.X = bounds.X + (bounds.Width-icon.Width)/2
		icon.Y = bounds.Y + (bounds.Height-groupHeight)/2
		text.Y = icon.Y + icon.Height + 14
		label = elide(canvas, label, textStyle.Size, textStyle.Weight, text.Width)
	}
	paintIcon(canvas, iconUpload, icon, state.pal.muted)
	canvas.LabelCenter(label, text, textStyle)
}

// Files dragged in from the system. Over the file panel they are uploaded: onto a
// folder row into that folder, anywhere else on the panel into the folder on
// show. Over the terminal of a server they are uploaded into the folder the
// shell is in. Over the terminal of a shell on this computer they are typed as
// paths, quoted, as other terminals do. Nowhere else takes them.

// pointInContext reports whether a window point is on the side panel.
func (state *State) pointInContext(x, y float32) bool {
	return state.contextRect.Width > 0 && pointInRect(x, y, state.contextRect)
}

// dropPanel is the file panel of the active session when it can take an upload
// at the point: it shows the server's files, is not busy with a dialog or the
// editor, and the point is on it.
func (state *State) dropPanel(x, y float32) (*Session, *sftpView) {
	if state.activeSection != sectionTerminal || state.modal != modalNone || !state.pointInContext(x, y) {
		return nil, nil
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok || session.files == nil || session.conn == nil {
		return nil, nil
	}
	view := session.files
	if view.local || view.dialog != sftpDialogNone || view.editPath != "" || view.path == "" {
		return nil, nil
	}
	return session, view
}

// dropFolderRow is the row of a folder under the pointer, or -1.
func (state *State) dropFolderRow(view *sftpView, target gfx.ID) int {
	local, ok := tree.Local(contextMount, target)
	if !ok || local.Scope() != hitSFTPEntryList {
		return -1
	}
	index := view.first + item(local)
	if view.searching() || index < 0 || index >= len(view.rows) || !view.rows[index].entry.IsDir() {
		return -1
	}
	return index
}

// terminalDrop is a terminal that files dragged over it would be uploaded
// from: the pane it fills and the folder on the server they would go into.
type terminalDrop struct {
	session   uint32
	bounds    gfx.Rect
	directory string
	// desktop marks a remote desktop's picture: what is dropped on it is offered
	// to the remote clipboard, not uploaded.
	desktop bool
}

// FileDrop receives files dragged from the system over the window.
func (state *State) FileDrop(event gfx.FileDropEvent) gfx.EventResult {
	if state.modal == modalFileProperties {
		return gfx.EventResult{Handled: true}
	}
	if event.Phase == gfx.FileDropLeave {
		repaint := state.clearDrop() || state.aiDropHover(false)
		if w := state.activeSFTPWindow(); w != nil && w.drag.active {
			w.cancelDrag()
			repaint = true
		}
		return gfx.EventResult{Repaint: repaint}
	}
	// The assistant panel is a place of its own, over the page beside it: files
	// dropped on it are attached to the next question.
	if state.aiDropsOnPanel(event.X, event.Y) {
		repaint := state.clearDrop()
		if event.Phase == gfx.FileDropOver {
			return gfx.EventResult{Handled: true, Repaint: state.aiDropHover(true) || repaint}
		}
		state.aiDropHover(false)
		state.aiAttachPaths(event.Paths)
		return gfx.EventResult{Handled: len(event.Paths) > 0, Repaint: true}
	}
	left := state.aiDropHover(false)
	if state.fileTabVerticalRailAt(event.X, event.Y) {
		repaint := left || state.clearDrop()
		if w := state.activeSFTPWindow(); w != nil && w.drag.active {
			w.cancelDrag()
			repaint = true
		}
		return gfx.EventResult{Handled: true, Repaint: repaint}
	}
	result := state.fileDropOnPage(event)
	result.Repaint = result.Repaint || left
	return result
}

// fileDropOnPage is FileDrop for files that are not over the assistant: the file
// panel and the terminals.
func (state *State) fileDropOnPage(event gfx.FileDropEvent) gfx.EventResult {
	if result, handled := state.sftpTabFileDrop(event); handled {
		return result
	}
	switch event.Phase {
	case gfx.FileDropOver:
		if _, view := state.dropPanel(event.X, event.Y); view != nil {
			row := state.dropFolderRow(view, event.Target)
			changed := !view.dropping || view.dropRow != row
			view.dropping, view.dropRow = true, row
			others := state.clearDropExcept(view)
			terminal := state.clearTerminalDrop()
			return gfx.EventResult{Handled: true, Repaint: changed || others || terminal}
		}
		repaint := state.clearDropPanels()
		if session, bounds, ok := state.desktopDropAt(event.X, event.Y); ok {
			target := terminalDrop{session: session.ID, bounds: bounds, desktop: true}
			changed := state.terminalDrop != target
			state.terminalDrop = target
			return gfx.EventResult{Handled: true, Repaint: repaint || changed}
		}
		if upload, ok := state.terminalUploadAt(event.X, event.Y); ok {
			changed := state.terminalDrop != upload
			state.terminalDrop = upload
			return gfx.EventResult{Handled: true, Repaint: repaint || changed}
		}
		if state.clearTerminalDrop() {
			repaint = true
		}
		return gfx.EventResult{Handled: state.terminalTakesPaths(event.X, event.Y), Repaint: repaint}
	}
	repaint := state.clearDrop()
	if session, view := state.dropPanel(event.X, event.Y); view != nil {
		remoteDir := view.path
		if row := state.dropFolderRow(view, event.Target); row >= 0 {
			remoteDir = path.Join(view.path, view.rows[row].entry.Name)
		}
		queued := 0
		for _, local := range event.Paths {
			info, err := os.Stat(local)
			if err != nil {
				continue
			}
			state.queueSFTPUploadTo(view, local, info.IsDir(), remoteDir)
			queued++
		}
		if queued > 0 {
			view.status = fmt.Sprintf(state.messages.Tools.DropUploadingFmt, queued, remoteDir)
		}
		_ = session
		return gfx.EventResult{Handled: queued > 0, Repaint: true}
	}
	if session, _, ok := state.desktopDropAt(event.X, event.Y); ok {
		paths := slices.Clone(event.Paths)
		state.dropDesktopFiles(session.desk, paths)
		return gfx.EventResult{Handled: len(paths) > 0, Repaint: true}
	}
	if upload, ok := state.terminalUploadAt(event.X, event.Y); ok {
		return gfx.EventResult{Handled: state.uploadToTerminalFolder(upload, event.Paths), Repaint: true}
	}
	if state.terminalTakesPaths(event.X, event.Y) {
		session, _ := state.sessionByIDPointer(state.activeSessionID)
		session.term.send([]byte(quotedPaths(event.Paths, session.wslDistro()) + " "))
		return gfx.EventResult{Handled: true, Repaint: true}
	}
	return gfx.EventResult{Repaint: repaint}
}

// paintTerminalDrop marks the terminal that files being dragged over it would
// be uploaded from, and where.
func (state *State) paintTerminalDrop(canvas *gfx.Canvas) {
	if drop := state.terminalDrop; drop.session != 0 {
		label := fmt.Sprintf(state.messages.Tools.DropTerminalUploadFmt, drop.directory)
		if drop.desktop {
			label = state.messages.Tools.DropDesktopFiles
		}
		state.paintSFTPDropOverlay(canvas, drop.bounds, label)
	}
}

// paneAt is the session under a window point, with the pane it fills.
func (state *State) paneAt(x, y float32) (*Session, gfx.Rect, bool) {
	if state.activeSection != sectionTerminal || state.modal != modalNone || state.pointInContext(x, y) {
		return nil, gfx.Rect{}, false
	}
	state.refreshWorkspaceViews()
	frame := state.workspacePixelBounds(state.viewport)
	for index := range state.panes {
		bounds := pixelBounds(frame, state.panes[index].Bounds)
		if !pointInRect(x, y, bounds) {
			continue
		}
		session, ok := state.sessionByIDPointer(state.panes[index].SessionID)
		if !ok {
			return nil, gfx.Rect{}, false
		}
		return session, bounds, true
	}
	return nil, gfx.Rect{}, false
}

// terminalPaneAt is the terminal under a window point, with the pane it fills.
func (state *State) terminalPaneAt(x, y float32) (*Session, gfx.Rect, bool) {
	session, bounds, ok := state.paneAt(x, y)
	if !ok || session.term == nil || session.term.shell == nil {
		return nil, gfx.Rect{}, false
	}
	return session, bounds, true
}

// terminalUploadAt reports the upload that files dropped at the point would
// make: the point is on the terminal of a connected server, and the folder its
// shell is in is known, from the shell's own report or else the one the file
// panel shows. A container's shell sees another file system than the one the
// upload reaches, so it is left out.
func (state *State) terminalUploadAt(x, y float32) (terminalDrop, bool) {
	session, bounds, ok := state.terminalPaneAt(x, y)
	if !ok || !session.hasServer() || session.conn == nil || session.phase != sshconn.PhaseConnected || session.containerCommand != "" {
		return terminalDrop{}, false
	}
	directory := ""
	if cwd, ok := session.term.screen.WorkingDirectory(); ok {
		directory = cwd.Path
	} else if session.files != nil {
		directory = session.files.path
	}
	if directory == "" {
		return terminalDrop{}, false
	}
	return terminalDrop{session: session.ID, bounds: bounds, directory: directory}, true
}

// uploadToTerminalFolder queues the dropped files into the folder the terminal's
// shell is in and reports whether any was queued. The transfers run on the
// session's file worker, made again if the panel was closed, and are followed in
// the transfer center like any other.
func (state *State) uploadToTerminalFolder(upload terminalDrop, paths []string) bool {
	session, ok := state.sessionByIDPointer(upload.session)
	if !ok {
		return false
	}
	if session.files == nil {
		state.openSFTP(session)
	}
	view := session.files
	if view == nil || view.worker == nil {
		state.status = state.messages.SFTPUnavailable
		return false
	}
	// The person is at the terminal and will type next, so the transfer center
	// stays as it was rather than opening over the shell and taking the keyboard.
	transfersOpen := state.transfers.open
	batch := terminalUploadBatch{view: view, title: session.Title, directory: upload.directory, pending: map[uint64]struct{}{}}
	for _, local := range paths {
		info, err := os.Stat(local)
		if err != nil {
			continue
		}
		if id := state.queueSFTPUploadTo(view, local, info.IsDir(), upload.directory); id != 0 {
			batch.pending[id] = struct{}{}
		}
	}
	state.transfers.open = transfersOpen
	batch.total = len(batch.pending)
	if batch.total == 0 {
		return false
	}
	state.terminalUploads = append(state.terminalUploads, batch)
	state.fileOpenNotice(nil, batch.title, fmt.Sprintf(state.messages.Tools.DropUploadingFmt, batch.total, batch.directory), components.ToastInfo)
	return true
}

// terminalUploadBatch is the files of one drop on a terminal, followed until
// every transfer has ended so that a single notice can say how it went: the
// transfer center stays shut, and nothing else would tell.
type terminalUploadBatch struct {
	view             *sftpView
	title, directory string
	pending          map[uint64]struct{}
	total, failed    int
	reason           string // why the first failed transfer failed
}

// settleTerminalUploads counts the transfers of view that have ended and, once a
// drop has none left running, tells how it went.
func (state *State) settleTerminalUploads(view *sftpView) {
	for index := 0; index < len(state.terminalUploads); {
		batch := &state.terminalUploads[index]
		if batch.view != view {
			index++
			continue
		}
		for id := range batch.pending {
			at := slices.IndexFunc(view.transfers, func(snapshot sftpclient.TransferSnapshot) bool { return snapshot.ID == id })
			if at < 0 {
				delete(batch.pending, id) // the worker has let it go; how it ended is not known
				continue
			}
			switch snapshot := view.transfers[at]; snapshot.State {
			case sftpclient.TransferCompleted:
				delete(batch.pending, id)
			case sftpclient.TransferFailed, sftpclient.TransferCanceled:
				delete(batch.pending, id)
				if batch.failed++; batch.reason == "" {
					batch.reason = snapshot.Error
				}
			}
		}
		if len(batch.pending) > 0 {
			index++
			continue
		}
		done := *batch
		state.terminalUploads = slices.Delete(state.terminalUploads, index, index+1)
		m := state.messages.Tools
		if done.failed == 0 {
			state.fileOpenNotice(nil, done.title, fmt.Sprintf(m.DropTerminalDoneFmt, done.total, done.directory), components.ToastSuccess)
			continue
		}
		state.fileOpenNotice(nil, done.title, fmt.Sprintf(m.DropTerminalFailedFmt, done.failed, done.total, done.directory, done.reason), components.ToastError)
	}
}

// forgetTerminalUploads stops following the drops of a file view that is going
// away.
func (state *State) forgetTerminalUploads(view *sftpView) {
	state.terminalUploads = slices.DeleteFunc(state.terminalUploads, func(batch terminalUploadBatch) bool { return batch.view == view })
}

// terminalTakesPaths reports whether the point is on the terminal of a live
// shell on this computer, which can be handed paths to type.
func (state *State) terminalTakesPaths(x, y float32) bool {
	if state.activeSection != sectionTerminal || state.modal != modalNone || state.pointInContext(x, y) {
		return false
	}
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	return ok && session.isLocal() && session.term != nil && session.term.shell != nil
}

// clearDrop ends a hover over the panel or a terminal and reports whether
// anything showed it.
func (state *State) clearDrop() bool {
	panels := state.clearDropPanels()
	terminal := state.clearTerminalDrop()
	return panels || terminal
}

// clearDropPanels ends a hover over the panel and reports whether anything
// showed it.
func (state *State) clearDropPanels() bool {
	changed := false
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil && view.dropping {
			view.dropping, view.dropRow = false, -1
			changed = true
		}
	}
	return changed
}

// clearTerminalDrop ends a hover over a terminal and reports whether anything
// showed it.
func (state *State) clearTerminalDrop() bool {
	changed := state.terminalDrop != terminalDrop{}
	state.terminalDrop = terminalDrop{}
	return changed
}

// clearDropExcept clears the hover of every panel but view's.
func (state *State) clearDropExcept(keep *sftpView) bool {
	changed := false
	for index := range state.sessions {
		if view := state.sessions[index].files; view != nil && view != keep && view.dropping {
			view.dropping, view.dropRow = false, -1
			changed = true
		}
	}
	return changed
}

// quotedPaths makes paths safe to type into the shell of this computer. A shell
// in a WSL distribution takes the paths as that distribution names them, quoted
// for a POSIX shell; one that cannot be named there is typed as it is.
func quotedPaths(paths []string, distro string) string {
	quoted := make([]string, len(paths))
	for index, p := range paths {
		switch {
		case distro != "":
			if linux, ok := localshell.WSLPath(p, distro); ok {
				p = linux
			}
			quoted[index] = shellQuote(p)
		case runtime.GOOS == "windows":
			quoted[index] = `"` + strings.ReplaceAll(p, `"`, `""`) + `"`
		default:
			quoted[index] = shellQuote(p)
		}
	}
	return strings.Join(quoted, " ")
}

// wslDistro is the WSL distribution a local session's shell runs in.
func (session *Session) wslDistro() string {
	if conn, ok := session.conn.(*localConn); ok {
		distro, _ := localshell.WSLDistro(conn.shell)
		return distro
	}
	return ""
}
