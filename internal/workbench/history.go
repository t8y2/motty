package workbench

import (
	"fmt"
	"path/filepath"
	"sort"
	"strings"
	"time"

	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	"motty/internal/cmdhistory"
	"motty/internal/fuzzy"
)

// Command history: the lines typed in terminals, kept to search and reuse.
//
// Ownership: what a terminal shows and what keys are sent are the application's
// (the emulator and the recorder read them); the list, its search field and the
// dialog are Viva components used as the command palette uses them. Nothing here
// is reusable desktop machinery, so nothing moved into Viva.
//
// The history lives in its own file, history.db next to the state file. It is
// not part of a backup or of cloud sync, and it is off until the user turns it
// on: the filters in package cmdhistory are heuristics, and a heuristic that
// guards secrets should be opted into.

// historyHits is the place of the controls this file paints.
var historyHits = tree.Feature("command-history")

var (
	hitHistoryOpen           = tree.ID(historyHits, "history-open")
	hitHistoryDismiss        = tree.ID(historyHits, "history-dismiss")
	hitHistoryClose          = tree.ID(historyHits, "history-close")
	hitHistorySearch         = tree.ID(historyHits, "history-search")
	hitHistoryScopeHost      = tree.ID(historyHits, "history-scope-host")
	hitHistoryScopeAll       = tree.ID(historyHits, "history-scope-all")
	hitHistoryItems          = tree.Sub(historyHits, tree.Name("history-items"))
	hitSettingsHistoryToggle = tree.ID(historyHits, "settings-history")
	hitSettingsSuggestToggle = tree.ID(historyHits, "settings-history-suggest")
	hitSettingsHistoryClear  = tree.ID(historyHits, "settings-history-clear")
)

const (
	// historyVisibleRows is how many results the picker lists at once.
	historyVisibleRows = 10
	// historyMaxResults bounds the ranked list; the picker scrolls through it.
	historyMaxResults = 500
)

// historyUI is the command history's state: the switch, the file, and the
// picker.
type historyUI struct {
	on bool

	// suggestOff turns off the inline suggestions the history offers while a
	// command is typed; they are on by default but need the history itself.
	suggestOff bool
	// ghost is the candidates of each host for those suggestions, newest first.
	ghost map[string][]cmdhistory.Entry

	store *cmdhistory.Queue
	tried bool
	err   error

	// The picker.
	sessionID uint32
	host      string // history key of the session it was opened for
	all       bool   // every host, not only this one
	search    text.Editor
	entries   []cmdhistory.Entry
	ranked    []int // indexes into entries, best first
	query     string
	selected  int
	top       int // first listed result
}

// historyKey is the key under which a session's commands are kept.
func historyKey(session *Session) string {
	if session.isLocal() {
		return "local"
	}
	if session.host.CatalogID != "" {
		return session.host.CatalogID
	}
	return session.host.Address
}

// historyAvailable reports whether the session is one whose lines can be read
// back: a shell that echoes what is typed, which SSH and a local shell do. A
// serial line or a Telnet session may or may not, and the client cannot tell,
// so those are never recorded.
func (state *State) historyAvailable(session *Session) bool {
	return session != nil && session.term != nil && (session.isLocal() || session.host.isSSH())
}

// attachHistory connects a new terminal to the history.
func (state *State) attachHistory(session *Session) {
	t := session.term
	if t == nil {
		return
	}
	sessionID := session.ID
	t.historyOn = func() bool {
		if !state.history.on {
			return false
		}
		session, ok := state.sessionByIDPointer(sessionID)
		return ok && state.historyAvailable(session) && !session.host.NoHistory
	}
	t.onCommand = func(command string) { state.rememberCommand(sessionID, command) }
	t.takeOutput = func() {
		if session, ok := state.sessionByIDPointer(sessionID); ok {
			state.pump(session)
		}
	}
}

func (state *State) historyPath() string {
	if state.options.HistoryPath != "" {
		return state.options.HistoryPath
	}
	return filepath.Join(filepath.Dir(state.options.StatePath), "history.db")
}

// historyStore opens the file the first time it is needed. A failure is
// remembered and shown once, not retried at every command.
func (state *State) historyStore() *cmdhistory.Queue {
	h := &state.history
	if h.store != nil || h.tried {
		return h.store
	}
	h.tried = true
	store, err := cmdhistory.Open(state.historyPath())
	if err != nil {
		h.err = err
		state.status = fmt.Sprintf(state.messages.History.StatusUnavailableFmt, err)
		return nil
	}
	h.store = cmdhistory.NewQueue(store)
	return h.store
}

func (state *State) closeHistory() {
	if state.history.store != nil {
		_ = state.history.store.Close()
		state.history.store = nil
	}
}

// rememberCommand stores a line the recorder judged safe.
func (state *State) rememberCommand(sessionID uint32, command string) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || !state.history.on || !state.historyAvailable(session) || session.host.NoHistory {
		return
	}
	if store := state.historyStore(); store != nil {
		key := historyKey(session)
		if err := store.Record(key, command, time.Now()); err != nil {
			state.status = fmt.Sprintf(state.messages.History.StatusUnavailableFmt, err)
		} else {
			state.noteGhostCommand(key, command)
			if err := store.Error(); err != nil {
				state.status = fmt.Sprintf(state.messages.History.StatusUnavailableFmt, err)
			}
		}
	}
}

// toggleHistory flips the Remember commands switch.
func (state *State) toggleHistory() {
	state.history.on = !state.history.on
	for index := range state.sessions {
		if t := state.sessions[index].term; t != nil {
			t.recorder.Reset() // a line half seen is not finished under the new setting
		}
	}
	state.save()
}

// toggleSuggestions flips the inline suggestions switch.
func (state *State) toggleSuggestions() {
	state.history.suggestOff = !state.history.suggestOff
	state.save()
}

// askClearHistory confirms, then forgets every remembered command.
func (state *State) askClearHistory() {
	m := state.messages.History
	state.openForm(formDialog{
		title: m.ClearTitle, description: m.ClearDescription, submit: m.ClearConfirm, danger: true,
		run: func(state *State, _ *formDialog) string {
			store := state.historyStore()
			if store == nil {
				return fmt.Sprintf(m.StatusUnavailableFmt, state.history.err)
			}
			if err := store.Clear(); err != nil {
				return fmt.Sprintf(m.StatusClearFailedFmt, err)
			}
			state.forgetGhostEntries()
			state.status = m.StatusCleared
			return ""
		},
	})
}

// ---------------------------------------------------------------------------
// The picker

// openHistoryActive opens the picker for the terminal in front.
func (state *State) openHistoryActive() {
	session, ok := state.sessionByIDPointer(state.activeSessionID)
	if !ok {
		state.status = state.messages.History.StatusNoTerminal
		return
	}
	state.openHistory(session)
}

func (state *State) openHistory(session *Session) {
	h := &state.history
	if session == nil || session.term == nil {
		state.status = state.messages.History.StatusNoTerminal
		return
	}
	h.sessionID, h.host = session.ID, historyKey(session)
	h.all = !state.historyAvailable(session) || session.host.NoHistory
	h.search = text.Editor{}
	h.query = "\x00" // never equal to a typed query, so the first refresh ranks
	state.modal = modalHistory
	state.loadHistory()
	state.rankHistory()
}

func (state *State) closeHistoryPicker() {
	h := &state.history
	h.entries, h.ranked = nil, nil
	h.search = text.Editor{}
	h.query = ""
	h.selected, h.top = 0, 0
	state.modal = modalNone
}

// loadHistory reads the entries of the scope the picker shows.
func (state *State) loadHistory() {
	h := &state.history
	h.entries, h.ranked = nil, nil
	store := state.historyStore()
	if store == nil {
		return
	}
	scope := h.host
	if h.all {
		scope = ""
	}
	entries, err := store.List(scope)
	if err != nil {
		state.status = fmt.Sprintf(state.messages.History.StatusUnavailableFmt, err)
		return
	}
	h.entries = entries
	h.query = "\x00"
}

// rankHistory orders the entries for the query in the search field: the better
// the match the higher; ties go to the more recent, then the more used. An
// empty query lists everything, most recent first.
func (state *State) rankHistory() {
	h := &state.history
	query := strings.TrimSpace(h.search.String())
	if query == h.query && h.ranked != nil {
		return
	}
	h.query = query
	compiled := fuzzy.Compile(query)
	type result struct{ index, score int }
	found := make([]result, 0, min(len(h.entries), historyMaxResults*2))
	for index, entry := range h.entries {
		if score, ok := compiled.Score(entry.Command); ok {
			found = append(found, result{index, score})
		}
	}
	sort.SliceStable(found, func(i, j int) bool {
		a, b := found[i], found[j]
		if a.score != b.score {
			return a.score > b.score
		}
		ea, eb := h.entries[a.index], h.entries[b.index]
		if !ea.Used.Equal(eb.Used) {
			return ea.Used.After(eb.Used)
		}
		return ea.Count > eb.Count
	})
	h.ranked = make([]int, 0, min(len(found), historyMaxResults))
	for _, result := range found {
		if len(h.ranked) == historyMaxResults {
			break
		}
		h.ranked = append(h.ranked, result.index)
	}
	h.selected, h.top = 0, 0
}

// historySelection is the command the picker has selected.
func (state *State) historySelection() (cmdhistory.Entry, bool) {
	h := &state.history
	if h.selected < 0 || h.selected >= len(h.ranked) {
		return cmdhistory.Entry{}, false
	}
	return h.entries[h.ranked[h.selected]], true
}

func (state *State) moveHistorySelection(delta int) {
	h := &state.history
	if len(h.ranked) == 0 {
		return
	}
	h.selected = min(max(h.selected+delta, 0), len(h.ranked)-1)
	if h.selected < h.top {
		h.top = h.selected
	}
	if h.selected >= h.top+historyVisibleRows {
		h.top = h.selected - historyVisibleRows + 1
	}
}

// chooseHistory types the selected command into the terminal. It does not press
// Enter unless run is set: the user can edit the line first. The text goes in as
// typed bytes, so the shell's own line editor sees it as the user's.
func (state *State) chooseHistory(run bool) {
	h := &state.history
	m := state.messages.History
	entry, ok := state.historySelection()
	if !ok {
		return
	}
	session, found := state.sessionByIDPointer(h.sessionID)
	key := h.host
	state.closeHistoryPicker()
	if !found || session.term == nil {
		state.status = m.StatusNoTerminal
		return
	}
	input := make([]byte, 0, len(entry.Command)+1)
	input = append(input, entry.Command...)
	if run {
		input = append(input, '\r')
	}
	if !state.typed(session, input) {
		state.status = m.StatusNotConnected
		return
	}
	if run && state.history.on && !session.host.NoHistory {
		// The recorder cannot see an echo that has not come back yet, so a line
		// run from the picker is counted here. It came from the history, which
		// only holds lines that were already cleared.
		if store := state.historyStore(); store != nil {
			_ = store.Record(key, entry.Command, time.Now())
			state.noteGhostCommand(key, entry.Command)
		}
	}
}

// deleteHistorySelection forgets the selected command, on every host when the
// picker shows all of them.
func (state *State) deleteHistorySelection() {
	h := &state.history
	m := state.messages.History
	entry, ok := state.historySelection()
	if !ok {
		return
	}
	store := state.historyStore()
	if store == nil {
		return
	}
	scope := h.host
	if h.all {
		scope = ""
	}
	if err := store.Delete(scope, entry.Command); err != nil {
		state.status = fmt.Sprintf(m.StatusRemoveFailedFmt, err)
		return
	}
	state.forgetGhostEntries()
	selected, top := h.selected, h.top
	state.loadHistory()
	state.rankHistory()
	if len(h.ranked) > 0 {
		h.selected = min(selected, len(h.ranked)-1)
		h.top = min(top, h.selected)
	}
	state.status = fmt.Sprintf(m.StatusRemovedFmt, entry.Command)
}

func (state *State) clickHistory(id ui.ID) {
	h := &state.history
	switch id {
	case hitHistoryDismiss, hitHistoryClose:
		state.closeHistoryPicker()
	case hitHistoryScopeHost:
		if h.all {
			h.all = false
			state.loadHistory()
			state.rankHistory()
		}
	case hitHistoryScopeAll:
		if !h.all {
			h.all = true
			state.loadHistory()
			state.rankHistory()
		}
	default:
		if slot, ok := id.In(hitHistoryItems); ok && h.top+slot < len(h.ranked) {
			h.selected = h.top + slot
			state.chooseHistory(false)
		}
	}
}

func (state *State) historyKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalHistory {
		return ui.EventResult{}, false
	}
	state.rankHistory()
	h := &state.history
	handled := ui.EventResult{Handled: true, Repaint: true, Focus: hitHistorySearch}
	switch event.Key {
	case ui.KeyEscape:
		state.closeHistoryPicker()
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
	case ui.KeyUp:
		state.moveHistorySelection(-1)
		return handled, true
	case ui.KeyDown:
		state.moveHistorySelection(1)
		return handled, true
	case ui.KeyPageUp:
		state.moveHistorySelection(-historyVisibleRows)
		return handled, true
	case ui.KeyPageDown:
		state.moveHistorySelection(historyVisibleRows)
		return handled, true
	case ui.KeyEnter:
		if event.Repeat {
			return handled, true
		}
		state.chooseHistory(event.Modifiers&ui.ModifierShift != 0)
		return ui.EventResult{Handled: true, Repaint: true, Focus: state.activeTerminalFocusHit()}, true
	case ui.KeyDelete:
		// In the search field Delete is the text's own key; only an empty field
		// has nothing for it to erase.
		if _, inItems := event.Target.In(hitHistoryItems); inItems || h.search.String() == "" {
			state.deleteHistorySelection()
			return handled, true
		}
	}
	return ui.EventResult{}, false
}
