package workbench

import (
	"os"
	"strings"

	"motty/internal/localshell"
	layoutcore "motty/internal/workspace"
)

type tabTitleMode string

const (
	tabTitleConnection tabTitleMode = "connection"
	tabTitleTerminal   tabTitleMode = "terminal"
	tabTitleDirectory  tabTitleMode = "directory"
)

// tabLabel follows the active pane without changing the saved workspace name.
// A local tab whose pane runs a coding agent takes the agent's name while the
// setting asks for the connection name: the mark beside it already says which
// program is in front, and the connection returns when the agent exits.
func (state *State) tabLabel(workspace layoutcore.WorkspaceInfo) string {
	if name := state.agentTabName(workspace); name != "" {
		return name
	}
	return state.tabBaseLabel(workspace)
}

// agentTabName is the coding agent a local tab shows in place of its connection
// name, or "" when the tab's own name stands: another tab-name source is
// selected, the user named the tab, or the pane in front is not a local agent.
func (state *State) agentTabName(workspace layoutcore.WorkspaceInfo) string {
	if (state.tabTitleMode != tabTitleConnection && state.tabTitleMode != "") || workspace.Named {
		return ""
	}
	session := state.tabSession(workspace)
	if session == nil || !session.isLocal() || session.agent == agentNone {
		return ""
	}
	return agentName(session.agent)
}

// tabBaseLabel is the name the tab-name setting gives the tab, without an agent
// passing through it. Renaming and saving start from this, so a transient agent
// never becomes the tab's own name.
func (state *State) tabBaseLabel(workspace layoutcore.WorkspaceInfo) string {
	if state.tabTitleMode == tabTitleConnection || state.tabTitleMode == "" || workspace.Named {
		return workspace.Title
	}
	session := state.tabSession(workspace)
	if session == nil || session.term == nil || session.term.screen == nil {
		return workspace.Title
	}
	if state.tabTitleMode == tabTitleDirectory {
		if directory := session.term.reportedDirectory.Path; directory != "" {
			home := session.term.reportedHome
			if home == "" && session.isLocal() {
				// Inspect the running connection, not a preference that may
				// have changed since this shell started.
				conn, local := session.conn.(*localConn)
				wsl := false
				if local {
					_, wsl = localshell.WSLDistro(conn.shell)
				}
				if !wsl {
					home, _ = os.UserHomeDir()
				}
			}
			return abbreviateTabDirectory(directory, home)
		}
		return workspace.Title
	}
	if reported := strings.TrimSpace(session.term.screen.Title()); reported != "" {
		return reported
	}
	return workspace.Title
}

// tabDirectory is the unabridged hover path, only when the label follows cwd.
func (state *State) tabDirectory(workspace layoutcore.WorkspaceInfo) string {
	if state.tabTitleMode != tabTitleDirectory || workspace.Named {
		return ""
	}
	if session := state.tabSession(workspace); session != nil && session.term != nil {
		return session.term.reportedDirectory.Path
	}
	return ""
}

func abbreviateTabDirectory(directory, home string) string {
	if home == "" {
		return directory
	}
	separator := "/"
	if strings.HasPrefix(home, `\\`) || len(home) >= 3 && home[1] == ':' && home[2] == '\\' {
		separator = `\`
	}
	if directory == home {
		return "~"
	}
	home = strings.TrimRight(home, separator)
	// Preserve roots; a root home still has a directory boundary.
	if home == "" {
		home = separator
	}
	if directory == home {
		return "~"
	}
	prefix := strings.TrimRight(home, separator) + separator
	if strings.HasPrefix(directory, prefix) {
		return "~" + separator + strings.TrimPrefix(directory, prefix)
	}
	return directory
}

// elideTabDirectory keeps whole trailing folders when possible, then the end
// of an oversized leaf name. It never changes the tab's identity or hover path.
func elideTabDirectory(directory string, fits func(string) bool) string {
	if fits(directory) {
		return directory
	}
	separator := '/'
	if strings.HasPrefix(directory, `\\`) || len(directory) >= 3 && directory[1] == ':' && directory[2] == '\\' || strings.HasPrefix(directory, `~\`) {
		separator = '\\'
	}
	leaf := 0
	for index, r := range directory {
		if r != separator {
			continue
		}
		leaf = index + 1
		if leaf < len(directory) {
			if candidate := "…" + directory[index:]; fits(candidate) {
				return candidate
			}
		}
	}
	if !fits("…") {
		return ""
	}
	runes := []rune(directory[leaf:])
	keep := 0
	for low, high := 1, len(runes); low <= high; {
		middle := (low + high) / 2
		if fits("…" + string(runes[len(runes)-middle:])) {
			keep, low = middle, middle+1
		} else {
			high = middle - 1
		}
	}
	return "…" + string(runes[len(runes)-keep:])
}

// tabSession returns the session behind the pane the tab snapshot currently
// represents. Tab labels and badges share this lookup so a pane activation
// updates both together.
func (state *State) tabSession(workspace layoutcore.WorkspaceInfo) *Session {
	pane, ok := state.deck.Pane(workspace.ID, workspace.ActivePane)
	if !ok || pane.SessionID == 0 {
		return nil
	}
	session, _ := state.sessionByIDPointer(uint32(pane.SessionID))
	return session
}
