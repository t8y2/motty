package workbench

import (
	"path/filepath"

	"github.com/vivaui/viva/gfx"
)

func (state *State) openTerminalFolder(target gfx.ID) gfx.EventResult {
	return state.openSessionFolder(state.focusedShortcutTerminal(target))
}

// localShellDirectory returns the folder a local terminal session is known to
// be in: the last reported directory, the remembered or restored one, or the
// directory its shell started in. It is empty when none is known, so a name
// resolved against it is never guessed from somewhere the shell is not.
func localShellDirectory(session *Session) string {
	if session == nil || !session.isLocal() {
		return ""
	}
	if session.term != nil && validLocalDirectory(session.term.reportedDirectory.Path) {
		return filepath.Clean(session.term.reportedDirectory.Path)
	}
	if directory := session.splitDirectory(); validLocalDirectory(directory) {
		return filepath.Clean(directory)
	}
	if conn, ok := session.conn.(*localConn); ok && validLocalDirectory(conn.dir) {
		return filepath.Clean(conn.dir)
	}
	return ""
}

// localSessionDirectory is the folder the open-folder action shows: where the
// shell is when that is known, else the user's home directory for a non-WSL
// local terminal that has not reported one yet.
func localSessionDirectory(session *Session) string {
	if directory := localShellDirectory(session); directory != "" {
		return directory
	}
	if session != nil && session.isLocal() && session.wslDistro() == "" {
		if home := localHome(); validLocalDirectory(home) {
			return filepath.Clean(home)
		}
	}
	return ""
}

// canOpenSessionFolder 供快捷键和菜单共用，确保可用状态与实际操作一致。
func canOpenSessionFolder(session *Session) bool {
	return localSessionDirectory(session) != ""
}

// openSessionFolder 使用指定终端，右键菜单不依赖键盘焦点所在终端。
func (state *State) openSessionFolder(session *Session) gfx.EventResult {
	directory := localSessionDirectory(session)
	if directory == "" {
		return gfx.EventResult{}
	}
	state.openLocalFolder(directory)
	return gfx.EventResult{Handled: true, Repaint: true}
}

func (state *State) openLocalFolder(directory string) {
	m := state.messages.Transfers
	state.launchFileManager(directory, state.messages.ShortcutTerminalFolder,
		m.ShowInFolderFailed, m.ShowInFolderMissing, state.options.OpenFolder)
}
