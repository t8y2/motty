package workbench

import (
	"strconv"
	"strings"

	layoutcore "motty/internal/workspace"
)

// cleanSessionTitle cleans a name the user chose. It may repeat another tab's:
// an explicit rename is kept as written, and the tab's number or position
// tells equal ones apart. Only the @ name below has to stay distinct.
func cleanSessionTitle(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "session"
	}
	return base
}

// uniqueSessionTitle numbers an automatically generated title, so tabs opened
// from the same host stay distinguishable even when tab numbers are hidden. A
// name the user types goes through cleanSessionTitle instead and may repeat.
func (state *State) uniqueSessionTitle(base string) string {
	base = cleanSessionTitle(base)
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate += "-" + strconv.Itoa(suffix)
		}
		available := true
		slug := mentionSlug(candidate)
		for index := range state.sessions {
			session := &state.sessions[index]
			if strings.EqualFold(session.Title, candidate) ||
				slug != "" && strings.EqualFold(mentionSlug(session.Title), slug) {
				available = false
				break
			}
		}
		if available {
			return candidate
		}
	}
}

// Assign once, rather than numbering while painting: closing or reordering a
// tab must not change the names already used in questions and @ references.
// Since the display title may repeat, the @ name carries the numbering.
func (state *State) uniqueMentionName(title string, sessionID uint32) string {
	base := mentionSlug(title)
	if base == "" {
		base = "session-" + sessionHandle(sessionID)
	}
	for suffix := 1; ; suffix++ {
		candidate := base
		if suffix > 1 {
			candidate += "-" + strconv.Itoa(suffix)
		}
		available := true
		for index := range state.sessions {
			other := &state.sessions[index]
			if other.ID != sessionID && strings.EqualFold(sessionMentionName(other), candidate) {
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
// group title; its individual terminals still have their own names.
func (state *State) renameSession(session *Session, title string) {
	session.Title = cleanSessionTitle(title)
	session.Mention = state.uniqueMentionName(session.Title, session.ID)
	workspace, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if ok && len(state.deck.Panes(workspace, nil)) == 1 {
		_ = state.deck.Rename(workspace, session.Title)
	}
}
