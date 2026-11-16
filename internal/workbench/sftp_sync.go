package workbench

import (
	"path"
	"path/filepath"
	"regexp"
	"strings"

	"motty/internal/shellkind"
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

// Only explicit navigation or the locate action owns a reverse request.
// Refreshes and shell reports cannot form a feedback loop.
func (state *State) requestTerminalDirectory(view *sftpView, directory string) {
	state.prepareTerminalDirectory(view, directory, false)
}

func (state *State) prepareTerminalDirectory(view *sftpView, directory string, locate bool) {
	view.terminalPath, view.terminalSyncSkipped, view.terminalLocate = "", false, false
	session, ok := state.sftpSession(view)
	if !ok || view.local || view.detached != nil || session.term == nil || session.containerCommand != "" || !session.hasServer() {
		return
	}
	t := session.term
	t.followPath = ""
	if !state.sftpTerminalFollow && !locate {
		return
	}
	if t.takeOutput != nil {
		t.takeOutput()
	}
	ready := state.terminalDirectoryReady(session)
	if locate {
		ready = state.terminalLocateReady(session)
	}
	if !validSyncDirectory(directory) || !ready {
		view.terminalSyncSkipped = true
		return
	}
	view.terminalPath, view.terminalPromptVersion = path.Clean(directory), t.promptVersion
	view.terminalLocate = locate
	view.terminalScreenVersion = t.screen.Version()
}

// A manual click can also target an ordinary POSIX shell, including a host
// confined to its interactive session. Unlike automatic sync this is the
// user's explicit command. Accept only a familiar empty shell prompt; never
// use this fallback after this session's integration has taken ownership.
//
// Bash-family prompts end in `$` or `#`, optionally behind a known user/host
// shape. zsh's default prompt ends in `%` after the host name (`#` for root),
// so those bare-word forms accept only `%` or `#`; a word followed by `$` is
// still not a prompt. The terminator is required and trailing spaces are
// anchored, so an echoed command line cannot match.
var locateShellPrompt = regexp.MustCompile(
	`^(?:(?:[^\s@]+@[^\s:]+:[^\r\n#$]*|\[[^\r\n]*@[^\r\n]*\]|(?:ba|z|da|k)?sh(?:-[0-9.]+)?)?[#$]|[A-Za-z][A-Za-z0-9._-]*[%#])[ \t]*$`)

func (state *State) terminalLocateReady(session *Session) bool {
	if state.terminalDirectoryReady(session) {
		return true
	}
	t := session.term
	if !session.hasServer() || session.containerCommand != "" || session.phase != sshconn.PhaseConnected ||
		t == nil || t.shell == nil || t.ended || t.folderIntegrationSeen || t.commandReporting || t.promptDirty ||
		t.exec != nil || t.transferHoldsInput || session.transferBusy() || t.injection.holding() || len(t.rx) != 0 ||
		t.screen.Modes().AltScreen || t.screen.ViewOffset() != 0 || t.screen.KeyboardFlags() != 0 || t.screen.Modes().Win32Input ||
		session.host.System == sshconn.SystemWindows {
		return false
	}
	if kind := t.shellKind(); kind != shellkind.Unknown && kind != shellkind.POSIX {
		return false
	}
	// Input without a subsequent remote echo must not reuse the old prompt.
	if t.screen.Version() <= t.inputScreenVersion {
		return false
	}
	cursor := t.screen.Cursor()
	prompt, ok := t.screen.AppendCellText(nil, cursor.Y, 0, cursor.X)
	return ok && strings.TrimSpace(t.screen.RowText(cursor.Y)) == strings.TrimSpace(string(prompt)) &&
		locateShellPrompt.Match(prompt)
}

// The toolbar targets the displayed remote folder, not a selected child or an
// unfinished path draft. A fresh read validates even a previously cached path.
func (state *State) canLocateSFTPTerminal(view *sftpView) bool {
	if view == nil || view.local || view.localOnly || view.detached != nil || view.worker == nil || view.busy || !validSyncDirectory(view.path) {
		return false
	}
	session, ok := state.sftpSession(view)
	return ok && session.hasServer() && session.host.System != sshconn.SystemWindows &&
		session.containerCommand == "" && session.phase == sshconn.PhaseConnected &&
		session.term != nil && session.term.shell != nil && !session.term.ended
}

func (state *State) locateSFTPTerminal(view *sftpView) {
	if !state.canLocateSFTPTerminal(view) {
		return
	}
	state.prepareTerminalDirectory(view, view.path, true)
	if view.terminalPath == "" {
		view.terminalSyncSkipped = false
		view.status = state.messages.Tools.FilesLocateTerminalSkipped
		return
	}
	nav := state.beginSFTPNavigation(view, false, view.terminalPath, sftpNavigationPassive, 0)
	state.queueSFTPListing(view, view.terminalPath, nav)
	if !view.busy {
		view.terminalPath, view.terminalLocate = "", false
		view.status = state.messages.Tools.FilesLocateTerminalSkipped
	}
}

func (state *State) terminalDirectoryReady(session *Session) bool {
	t := session.term
	return session.hasServer() && session.containerCommand == "" && session.phase == sshconn.PhaseConnected &&
		t != nil && t.shell != nil && !t.ended && t.promptReady && !t.promptDirty && t.exec == nil &&
		!t.screen.Modes().AltScreen && !session.transferBusy()
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
	directory, version, locate := view.terminalPath, view.terminalPromptVersion, view.terminalLocate
	view.terminalPath, view.terminalLocate = "", false
	if !state.sftpTerminalFollow && !locate || view.local || view.detached != nil {
		return
	}
	session, ok := state.sftpSession(view)
	if ok && session.term != nil && session.term.takeOutput != nil {
		session.term.takeOutput()
	}
	ready, command := false, "builtin cd -- "+shellQuote(directory)+"\r"
	if ok && session.term != nil {
		ready = state.terminalDirectoryReady(session)
		if locate {
			ready = state.terminalLocateReady(session)
			if !session.term.folderIntegrationSeen {
				// POSIX sh/dash do not have Bash's `builtin` command.
				command = "command cd -- " + shellQuote(directory) + "\r"
				ready = ready && session.term.screen.Version() == view.terminalScreenVersion
			}
		}
	}
	// Output was drained above; a second drain after validating the snapshot
	// could change the prompt between checking and sending the command.
	if !ready || session.term.promptVersion != version || session.term.transferHoldsInput ||
		!session.term.sendInput([]byte(command), false) {
		view.status = state.terminalFollowSkippedText(view)
		if locate {
			view.status = state.messages.Tools.FilesLocateTerminalSkipped
		}
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
