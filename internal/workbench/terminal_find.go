package workbench

import (
	"fmt"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/i18n"
	"motty/internal/term"
)

// terminalFindHits is the place of the controls this file paints.
var terminalFindHits = tree.Feature("terminal-find")

// Terminal find. The field is shared by the active
// session; the query and matches themselves stay with each terminal.
var (
	hitTerminalFindField    = tree.ID(terminalFindHits, "terminal-find-field")
	hitTerminalFindPrevious = tree.ID(terminalFindHits, "terminal-find-previous")
	hitTerminalFindNext     = tree.ID(terminalFindHits, "terminal-find-next")
	hitTerminalFindClose    = tree.ID(terminalFindHits, "terminal-find-close")
)

const (
	terminalFindMaxQuery   = 256
	terminalFindMaxMatches = 1000
	terminalFindKeep       = 128
	// terminalFindRefreshInterval bounds how often a find bar open over moving
	// output rescans the retained buffer. A scan of 5000 lines is 1.6 ms, and
	// output can change the screen many times a second; 10 Hz keeps the count
	// close enough without spending the window thread on every chunk. A frame
	// requested by the find bar paint runs the next scan.
	terminalFindRefreshInterval = 100 * time.Millisecond
)

type terminalFind struct {
	editor      text.Editor
	input       app.TextInput
	open        bool
	matches     []term.SearchMatch
	active      int
	truncated   bool
	query       string
	count       string
	screenState uint64
	lastScan    time.Duration // when the last automatic scan ran
	pending     bool          // a version change is waiting for the next scan
}

func newTerminalFind() terminalFind {
	find := terminalFind{active: -1}
	find.editor.Restrict(terminalFindRune, terminalFindMaxQuery)
	return find
}

func terminalFindRune(value rune) bool {
	return value != '\r' && value != '\n'
}

func (find *terminalFind) refresh(screen *term.Terminal, messages i18n.Messages, reveal bool) {
	find.pending = false
	query := find.editor.String()
	oldQuery := find.query
	oldActive := find.active
	oldMatch := term.SearchMatch{}
	if oldActive >= 0 && oldActive < len(find.matches) {
		oldMatch = find.matches[oldActive]
	}

	find.matches, find.truncated = screen.Search(find.matches[:0], query, terminalFindMaxMatches)
	find.query = query
	switch {
	case len(find.matches) == 0:
		find.active = -1
	case query != oldQuery || oldActive < 0:
		find.active = 0
	default:
		find.active = min(oldActive, len(find.matches)-1)
		for index, match := range find.matches {
			if match == oldMatch {
				find.active = index
				break
			}
		}
	}
	if reveal {
		find.reveal(screen)
	}
	find.describe(messages)
	find.screenState = screen.Version()
}

// refreshIfChanged rescans when output changed the screen. now is the
// workbench clock; zero forces the scan (the configure and open paths, which
// must show the new screen at once). A change inside the interval marks the bar
// pending; paint asks for the frame that runs the trailing scan.
func (find *terminalFind) refreshIfChanged(screen *term.Terminal, messages i18n.Messages, now time.Duration) {
	if !find.open || find.screenState == screen.Version() {
		return
	}
	if now > 0 && find.lastScan > 0 && now-find.lastScan < terminalFindRefreshInterval {
		find.pending = true
		return
	}
	find.refresh(screen, messages, false)
	find.lastScan = now
	find.pending = false
}

func (find *terminalFind) describe(messages i18n.Messages) {
	switch {
	case find.query == "":
		find.count = ""
	case len(find.matches) == 0:
		find.count = messages.TerminalFindNoMatches
	case find.truncated:
		find.count = fmt.Sprintf(messages.TerminalFindTruncatedFmt, find.active+1, len(find.matches))
	default:
		find.count = fmt.Sprintf(messages.TerminalFindCountFmt, find.active+1, len(find.matches))
	}
}

func (find *terminalFind) reveal(screen *term.Terminal) bool {
	if find.active < 0 || find.active >= len(find.matches) {
		return false
	}
	return screen.RevealLine(find.matches[find.active].Start.Line)
}

func (find *terminalFind) move(screen *term.Terminal, messages i18n.Messages, delta int) bool {
	if len(find.matches) == 0 {
		return false
	}
	find.active = (find.active + delta + len(find.matches)) % len(find.matches)
	moved := find.reveal(screen)
	find.describe(messages)
	find.screenState = screen.Version()
	return moved
}

func (find *terminalFind) close() {
	find.open = false
	find.active = -1
	find.matches = find.matches[:0]
	if cap(find.matches) > terminalFindKeep {
		find.matches = nil
	}
	find.truncated = false
	find.query = ""
	find.count = ""
}

func (state *State) activeTerminalFind() (*Session, bool) {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	return session, ok && state.activeSection == sectionTerminal && state.modal == modalNone && session.term != nil
}

func (state *State) openTerminalFind(session *Session) ui.EventResult {
	find := &session.term.find
	find.open = true
	find.refresh(session.term.screen, state.messages, true)
	return ui.EventResult{Handled: true, Repaint: true, Focus: hitTerminalFindField, FocusVisible: true}
}

func (state *State) closeTerminalFind(session *Session) ui.EventResult {
	session.term.find.close()
	return ui.EventResult{Handled: true, Repaint: true, Focus: hitTerminalList.At(int(session.ID))}
}

func (state *State) terminalFindEditor(hit ui.ID) *text.Editor {
	if hit != hitTerminalFindField {
		return nil
	}
	session, ok := state.activeTerminalFind()
	if !ok || !session.term.find.open {
		return nil
	}
	return &session.term.find.editor
}

// TerminalFindInput resolves the native single-line editor used by the find
// bar. Its callback refreshes matches after the text service mutates the query.
func (state *State) TerminalFindInput(context app.TextInputContext) *app.TextInput {
	if _, local, ok := state.decodePaneHit(context.ID); ok {
		context.ID = local
	}
	if context.ID != hitTerminalFindField {
		return nil
	}
	session, ok := state.activeTerminalFind()
	if !ok || !session.term.find.open {
		return nil
	}
	find := &session.term.find
	find.input.Editor = nil
	find.input.State = app.TextStateOf(&find.editor)
	if find.input.OnEvent == nil {
		find.input.OnEvent = func(event app.TextEvent) (app.TextState, error) {
			return state.terminalFindText(session.ID, event), nil
		}
	}
	return &find.input
}

func (state *State) terminalFindText(sessionID uint32, event app.TextEvent) app.TextState {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil || !session.term.find.open {
		return app.TextState{}
	}
	find := &session.term.find
	event.Apply(&find.editor)
	find.refresh(session.term.screen, state.messages, true)
	return app.TextStateOf(&find.editor)
}

func (state *State) terminalFindKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if event.Target.Scope() != terminalFindHits {
		return ui.EventResult{}, false
	}
	session, ok := state.activeTerminalFind()
	if !ok || !session.term.find.open {
		return ui.EventResult{}, false
	}
	switch event.Key {
	case ui.KeyEscape:
		return state.closeTerminalFind(session), true
	case ui.KeyEnter:
		if event.Repeat {
			return ui.EventResult{Handled: true}, true
		}
		delta := 1
		if event.Modifiers&ui.ModifierShift != 0 {
			delta = -1
		}
		session.term.find.move(session.term.screen, state.messages, delta)
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	return ui.EventResult{}, false
}

func (state *State) clickTerminalFind(id ui.ID) bool {
	if id.Scope() != terminalFindHits {
		return false
	}
	session, ok := state.activeTerminalFind()
	if !ok || !session.term.find.open {
		return true
	}
	switch id {
	case hitTerminalFindPrevious:
		session.term.find.move(session.term.screen, state.messages, -1)
	case hitTerminalFindNext:
		session.term.find.move(session.term.screen, state.messages, 1)
	case hitTerminalFindClose:
		session.term.find.close()
	}
	return true
}

func (state *State) terminalFindPointer(event ui.PointerEvent) (ui.EventResult, bool) {
	if event.Target != hitTerminalFindClose || event.Kind != ui.PointerDown || event.Button != ui.PointerButtonPrimary {
		return ui.EventResult{}, false
	}
	session, ok := state.activeTerminalFind()
	if !ok || !session.term.find.open {
		return ui.EventResult{}, false
	}
	return state.closeTerminalFind(session), true
}

func (state *State) describeTerminalFinds() {
	for index := range state.sessions {
		if t := state.sessions[index].term; t != nil && t.find.open {
			t.find.describe(state.messages)
		}
	}
}
