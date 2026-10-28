package workbench

import (
	"strconv"
	"strings"

	layoutcore "motty/internal/workspace"
)

// Assign once, rather than numbering while painting: closing or reordering a
// tab must not change the names already used in questions and @ references.
func (state *State) uniqueSessionTitle(base string, except uint32) string {
	base = strings.TrimSpace(base)
	if base == "" {
		base = "session"
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate += "-" + strconv.Itoa(suffix)
		}
		available := true
		slug := mentionSlug(candidate)
		for _, session := range state.sessions {
			if session.ID != except && (strings.EqualFold(session.Title, candidate) ||
				slug != "" && strings.EqualFold(mentionSlug(session.Title), slug)) {
				available = false
				break
			}
		}
		if available {
			return candidate
		}
	}
}

// A one-terminal tab and its session share a name. A split workspace keeps its
// group title; its individual terminals still have their own unique names.
func (state *State) renameSession(session *Session, title string) {
	session.Title = state.uniqueSessionTitle(title, session.ID)
	workspace, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if ok && len(state.deck.Panes(workspace, nil)) == 1 {
		_ = state.deck.Rename(workspace, session.Title)
	}
}
