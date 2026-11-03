package workbench

// openProcessTools uses the same independent tools window as containers. The
// process icon opens directly on its tab, while the window keeps every remote
// resource available through the existing tabs.
func (state *State) openProcessTools(session *Session) {
	state.openRemoteToolsTab(session, remoteProcesses, true)
}
