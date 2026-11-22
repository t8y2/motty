package workbench

import (
	"path/filepath"

	"github.com/vivaui/viva/gfx"
)

func (state *State) openTerminalFolder(target gfx.ID) gfx.EventResult {
	return state.openSessionFolder(state.focusedShortcutTerminal(target))
}

// localSessionDirectory returns the best known local folder for a local
// terminal session: the last reported directory, the remembered/restored
// directory, the shell's startup directory, or the user's home directory for
// non-WSL local terminals when none has been reported yet.
func localSessionDirectory(session *Session) string {
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
	if session.wslDistro() == "" {
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
