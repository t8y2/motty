package workbench

import "github.com/vivaui/viva/ui"

// declareMenus declares the context menus that are open, in the window's page, over
// the dialogs. A menu is declared only while its state says it is open.
func (state *State) declareMenus(c ui.C) {
	state.declareHostMenu(c)
	state.declareHostGroupActions(c)
	state.declareHostBlankMenu(c)
	state.declareTerminalMenu(c)
	state.declareGhostMenu(c)
	state.declareSFTPMenu(c)
	state.declareTabMenu(c)
	state.declareWorkspacePaneTools(c)
	state.declareToolbarDockMenu(c)
	state.declareSnippetMenu(c)
	state.declareLocalShellPicker(c)
	state.declareWorkspaceTemplateMenu(c)
}
