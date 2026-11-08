package workbench

// localStartPolicy is where a local shell starts when nothing more specific
// asks for a folder. A split always starts in its pane's folder, whichever is
// chosen; this is about tabs.
type localStartPolicy uint8

const (
	// localStartRemember is the zero value and the default: a local tab brought
	// back by reopening the app starts its shell in the folder it was last in,
	// the way it was left, and a new local terminal starts where the user is.
	localStartRemember localStartPolicy = iota
	// localStartHome starts every local tab in the home folder: a restored tab
	// forgets where it was (and still starts), and a new one inherits nothing.
	localStartHome
)

func (policy localStartPolicy) String() string {
	if policy == localStartHome {
		return "home"
	}
	return "remember"
}

func localStartPolicyFromString(value string) localStartPolicy {
	if value == localStartHome.String() {
		return localStartHome
	}
	return localStartRemember
}

// paintLocalStartSetting is the choice's row, beside the other local terminal
// settings.
func (state *State) paintLocalStartSetting(page *settingsPage) {
	options := state.settingsLocalStartOptions()
	selected := hitSettingsLocalStartRemember
	if state.localStart == localStartHome {
		selected = hitSettingsLocalStartHome
	}
	page.row(state.messages.SettingsLocalStart, state.messages.SettingsLocalStartDescription, options[:], selected)
}

func (state *State) setLocalStartPolicy(policy localStartPolicy) bool {
	if state.localStart == policy {
		return false
	}
	state.localStart = policy
	return true
}

// newLocalTerminalDirectory is where a new local terminal starts: with the
// folder remembered, the one the local tab in front is in, or else the one a
// local shell was last in, so a terminal opened from a remote tab or after a
// relaunch still starts where the user left off. Empty means the home folder.
func (state *State) newLocalTerminalDirectory() string {
	if state.localStart == localStartHome {
		return ""
	}
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.isLocal() && session.localDirectory != "" {
		return session.localDirectory
	}
	return state.lastLocalDirectory
}

// noteLocalDirectory remembers the folder a local shell is in as the last one,
// which outlives the tab: it is where the next local terminal starts.
func (state *State) noteLocalDirectory(directory string) {
	if directory == "" || directory == state.lastLocalDirectory {
		return
	}
	state.lastLocalDirectory = directory
	state.terminalDirectoryDirty = true
}

// noteActiveLocalDirectory makes the local tab in front the last place, so a
// window closed with several local tabs reopens new ones where the user was
// looking rather than wherever a background shell last printed a prompt.
func (state *State) noteActiveLocalDirectory() {
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok && session.isLocal() {
		state.noteLocalDirectory(session.localDirectory)
	}
}

// startRestoredLocalSessions starts the shells of the local tabs the restore
// brought back. Starting one dials nothing, so the restore policy, which bounds
// network connections, never holds it back. The folder choice only decides
// where the shell starts: the folder the tab was left in, or home (the reconnect
// forgets the saved folder).
func (state *State) startRestoredLocalSessions() {
	for index := range state.sessions {
		session := &state.sessions[index]
		if !session.isLocal() || !session.restored || !state.canReconnect(session) {
			continue
		}
		session.autoRestore = true
		state.reconnect(session)
	}
}

// hasRestoredLocalSession reports whether a restored local tab waits for its
// shell, which the first Wake starts.
func (state *State) hasRestoredLocalSession() bool {
	for index := range state.sessions {
		if session := &state.sessions[index]; session.isLocal() && session.restored {
			return true
		}
	}
	return false
}
