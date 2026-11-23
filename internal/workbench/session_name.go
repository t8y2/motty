package workbench

import (
	"strconv"
	"strings"

	layoutcore "motty/internal/workspace"
)

// cleanSessionTitle cleans a name, chosen by the user or derived from the
// connection. It may repeat another tab's: tabs are told apart by their place
// in the tab strip, and an explicit rename is kept as the user wrote it. Only
// the @ name below has to stay distinct.
func cleanSessionTitle(base string) string {
	base = strings.TrimSpace(base)
	if base == "" {
		return "session"
	}
	return base
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

// legacyCustomSessionTitle 兼容未保存名称来源的旧记录。
// 默认标题仅为主机名或主机名加自动编号，其余名称按自定义保留。
func legacyCustomSessionTitle(title, base string) bool {
	base = cleanSessionTitle(base)
	if title == base {
		return false
	}
	suffix, numbered := strings.CutPrefix(title, base+"-")
	if numbered {
		number, err := strconv.Atoi(suffix)
		if err == nil && number >= 2 && strconv.Itoa(number) == suffix {
			return false
		}
	}
	return true
}

// 保存显式 false，以区别旧版本完全没有来源标记的记录。
func savedCustomSessionTitle(custom bool) *bool { return &custom }

func restoredCustomSessionTitle(custom *bool, title, base string) bool {
	if custom != nil {
		return *custom
	}
	return legacyCustomSessionTitle(title, base)
}
