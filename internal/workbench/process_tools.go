package workbench

// openToolsWindow opens the containers-and-processes window on its first tab,
// containers. The process list is one tab away, and the resource monitor points
// at it from its own page.
func (state *State) openToolsWindow(session *Session) {
	state.openRemoteToolsTab(session, remoteContainers, true)
}
