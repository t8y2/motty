package workbench

import "github.com/vivaui/viva/gfx"

func (state *State) openTerminalFolder(target gfx.ID) gfx.EventResult {
	return state.openSessionFolder(state.focusedShortcutTerminal(target))
}

// canOpenSessionFolder 供快捷键和菜单共用，确保可用状态与实际操作一致。
func canOpenSessionFolder(session *Session) bool {
	return session != nil && session.isLocal() && validLocalDirectory(session.splitDirectory())
}

// openSessionFolder 使用指定终端，右键菜单不依赖键盘焦点所在终端。
func (state *State) openSessionFolder(session *Session) gfx.EventResult {
	if !canOpenSessionFolder(session) {
		return gfx.EventResult{}
	}
	directory := session.splitDirectory()
	m := state.messages.Transfers
	state.launchFileManager(directory, state.messages.ShortcutTerminalFolder,
		m.ShowInFolderFailed, m.ShowInFolderMissing, state.options.OpenFolder)
	return gfx.EventResult{Handled: true, Repaint: true}
}
