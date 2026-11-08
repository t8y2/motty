package workbench

import (
	"path"
	"path/filepath"
	"strings"

	"motty/internal/sshconn"
	"motty/internal/term"
)

// Reports may arrive before the panel opens or while a listing/editor owns it.
// Consume only the latest one, once, rather than losing it or snapping back on
// every frame after the user has deliberately browsed somewhere else.
func (state *State) terminalDirectory(sessionID uint32, cwd term.Cwd) {
	state.recordTerminalDirectory(sessionID, cwd, true)
}

// Explicit panel opening replays the latest directory, whereas prompt reports
// are deduplicated so they do not undo manual browsing in an open panel.
func (state *State) recordTerminalDirectory(sessionID uint32, cwd term.Cwd, replay bool) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil || session.containerCommand != "" {
		return
	}
	if session.term.folderIntegrationSeen && !session.term.promptStarted {
		return // a running program or nested login is not this shell's prompt
	}
	if session.isLocal() {
		// A nested SSH login's OSC 7 must not replace the local startup path.
		if !localDirectoryHost(cwd.Host) || !validLocalDirectory(cwd.Path) {
			return
		}
		if directory := filepath.Clean(cwd.Path); session.term.folderIntegrationSeen && session.term.promptStarted {
			if directory != session.localDirectory {
				session.localDirectory = directory
				state.terminalDirectoryDirty = true
			}
			state.noteLocalDirectory(directory)
		}
	} else {
		if !validSyncDirectory(cwd.Path) {
			// Native Windows OSC 9;9 reports are useful locally, but are not
			// POSIX SFTP paths. Keep remote following's existing path contract.
			return
		}
		// Only the shell integration's prompt proves this is the shell's own
		// directory, rather than a program or nested login reporting one.
		if session.hasServer() && session.term.folderIntegrationSeen && session.term.promptStarted {
			if directory := path.Clean(cwd.Path); directory != session.startDirectory {
				session.startDirectory = directory
				state.terminalDirectoryDirty = true
			}
		}
	}
	// Deduplicate accepted reports, rather than the terminal's raw reports. A
	// repeated prompt must not undo manual browsing, but its first trusted
	// report still counts after a running program reported the same path.
	if !replay && cwd == session.term.reportedDirectory {
		return
	}
	session.term.reportedDirectory = cwd
	if !session.canBrowseFiles() || state.sftpFollowOff {
		return
	}
	session.term.followPath = cwd.Path
}

func (state *State) navigateSFTPDirectory(view *sftpView, directory string) {
	state.navigateSFTPDirectoryNavigation(view, directory, sftpNavigationManual, 0)
}

func (state *State) navigateSFTPDirectoryNavigation(view *sftpView, directory string, kind sftpNavigationKind, index int) {
	directory = path.Clean(directory)
	nav := state.beginSFTPNavigation(view, false, directory, kind, index)
	state.requestTerminalDirectory(view, directory)
	if view.terminalPath != "" {
		// A cached listing is optimistic UI, not proof that this directory
		// still exists. Reverse commands require a fresh successful read.
		state.queueSFTPListing(view, directory, nav)
		if !view.busy {
			view.terminalPath = ""
		}
		return
	}
	state.openSFTPDirectoryNavigation(view, directory, nav)
}

func validSyncDirectory(directory string) bool {
	return path.IsAbs(directory) && strings.IndexFunc(directory, func(r rune) bool { return r < 0x20 || r == 0x7f }) < 0
}

// Only an explicit navigation owns a reverse request. Listing refreshes and
// shell reports use openSFTPDirectory directly and cannot form a feedback loop.
func (state *State) requestTerminalDirectory(view *sftpView, directory string) {
	view.terminalPath, view.terminalSyncSkipped = "", false
	session, ok := state.sftpSession(view)
	if !ok || view.local || view.detached != nil || session.term == nil || session.containerCommand != "" || !session.hasServer() {
		return
	}
	t := session.term
	t.followPath = ""
	if !state.sftpTerminalFollow {
		return
	}
	if t.takeOutput != nil {
		t.takeOutput()
	}
	if !validSyncDirectory(directory) || !state.terminalDirectoryReady(session) {
		view.terminalSyncSkipped = true
		return
	}
	view.terminalPath, view.terminalPromptVersion = path.Clean(directory), t.promptVersion
}

func (state *State) terminalDirectoryReady(session *Session) bool {
	t := session.term
	return session.hasServer() && session.containerCommand == "" && session.phase == sshconn.PhaseConnected &&
		t != nil && t.shell != nil && !t.ended && t.promptReady && !t.promptDirty && t.exec == nil &&
		!t.screen.Modes().AltScreen && !(state.transfer.phase != transferNone && state.transfer.sessionID == session.ID)
}

// The successful listing is the commit point. A failed/obsolete listing must
// never cause a cd, and typing or a new prompt while I/O ran invalidates it.
func (state *State) syncTerminalDirectory(view *sftpView) {
	if view.terminalSyncSkipped {
		view.terminalSyncSkipped = false
		if state.sftpTerminalFollow {
			view.status = state.terminalFollowSkippedText(view)
		}
	}
	if view.terminalPath == "" || path.Clean(view.path) != view.terminalPath {
		return
	}
	directory, version := view.terminalPath, view.terminalPromptVersion
	view.terminalPath = ""
	if !state.sftpTerminalFollow || view.local || view.detached != nil {
		return
	}
	session, ok := state.sftpSession(view)
	if ok && session.term != nil && session.term.takeOutput != nil {
		session.term.takeOutput()
	}
	if !ok || !state.terminalDirectoryReady(session) || session.term.promptVersion != version ||
		!session.term.send([]byte("builtin cd -- "+shellQuote(directory)+"\r")) {
		view.status = state.terminalFollowSkippedText(view)
	}
}

// terminalFollowSkippedText says why the terminal was left where it was. A shell
// that reports its prompt but never the start of a command (its DEBUG trap was
// replaced, say) can never be proven idle, so asking the user to finish a command would send
// them looking for something that is not there.
func (state *State) terminalFollowSkippedText(view *sftpView) string {
	if session, ok := state.sftpSession(view); ok && session.term != nil && session.term.folderIntegrationSeen && !session.term.commandReporting {
		return state.messages.Tools.FilesTerminalFollowUnsupported
	}
	return state.messages.Tools.FilesTerminalFollowSkipped
}

func (state *State) followTerminalDirectory(session *Session) {
	t := session.term
	if t == nil || t.followPath == "" {
		return
	}
	if state.sftpFollowOff || !session.canBrowseFiles() || session.containerCommand != "" {
		t.followPath = ""
		return
	}
	view := session.files
	if session.isLocal() {
		state.followLocalTerminalDirectory(session, view)
		return
	}
	if view == nil || view.worker == nil || view.local || view.detached != nil || view.busy ||
		view.dialog != sftpDialogNone || view.editPath != "" || view.pathEditing || view.dropping {
		return
	}
	if state.sftpPathInput && view.pathField.path == view.path && view.pathField.editor.String() != view.path {
		return
	}
	directory := t.followPath
	t.followPath = ""
	if path.Clean(view.path) != path.Clean(directory) {
		state.openSFTPDirectory(view, directory)
	}
}

// followLocalTerminalDirectory moves a local terminal's panel to the folder its
// shell reported, once, unless the panel is in the middle of something else.
func (state *State) followLocalTerminalDirectory(session *Session, view *sftpView) {
	t := session.term
	if view == nil || !view.localOnly || view.detached != nil || view.busy ||
		view.dialog != sftpDialogNone || view.editPath != "" || view.pathEditing || view.dropping {
		return
	}
	if state.sftpPathInput && view.pathField.path == view.lfolder.path && view.pathField.editor.String() != view.lfolder.path {
		return
	}
	directory := t.followPath
	t.followPath = ""
	if filepath.Clean(view.lfolder.path) != filepath.Clean(directory) {
		_ = state.listLocal(view, directory)
	}
}
