package workbench

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"motty/internal/cmdhistory"
	"motty/internal/completion"
)

// A suggestion is proof about one echoed line, not permission to type into a
// session slot later reused by another terminal. Both presentation modes use
// the same proof and a single drain before writing.
type suggestionSnapshot struct {
	terminalInputConfirmation
	line              string
	historyGeneration uint64
	catalogGeneration uint64
}

func (state *State) captureSuggestion(session *Session, line string) suggestionSnapshot {
	return suggestionSnapshot{
		terminalInputConfirmation: state.inputAsk(session),
		line:                      line,
		historyGeneration:         state.completion.generation,
		catalogGeneration:         state.completion.catalogGeneration,
	}
}

func (state *State) suggestionOwner(snapshot suggestionSnapshot) *Session {
	session := state.inputOwnerAfterDrain(snapshot.sessionID, snapshot.owner)
	if session == nil || state.broadcast != 0 || state.historyClearing() || state.history.suggestOff {
		return nil
	}
	t := session.term
	if t.folderToken != snapshot.token || t.inputGuard.epoch != snapshot.epoch || t.promptVersion != snapshot.version {
		return nil
	}
	if state.completion.generation != snapshot.historyGeneration || state.completion.catalogGeneration != snapshot.catalogGeneration {
		return nil
	}
	if len(t.rx) > 0 {
		return nil
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok || line != snapshot.line || cmdhistory.Secret(line) {
		return nil
	}
	return session
}

// sendSuggestion types one suffix into the session. Catalog-sourced text is
// checked once more here, immediately before it becomes input: the importer
// already refuses unsafe tokens and values, but a tampered or damaged cache
// decoded later must not be able to type characters that neither a command
// token nor a literal value can contain. Text learned from the user's own
// history is their own input and is not constrained further than the
// control-character guard below.
func (state *State) sendSuggestion(session *Session, suffix string, fromCatalog bool) {
	if suffix == "" || len(suffix) > cmdhistory.MaxCommand || !utf8.ValidString(suffix) {
		return
	}
	if strings.IndexFunc(suffix, unicode.IsControl) >= 0 {
		return
	}
	if fromCatalog && !completion.SafeInsert(suffix) {
		return
	}
	// Draining again here could invalidate the proof after checking it. The
	// owner was drained immediately above, and broadcasting is not permitted.
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	if !ok {
		return
	}
	if session.term.sendInput([]byte(suffix), false) {
		session.term.suggestionPending = &suggestionEcho{line: line + suffix, token: session.term.folderToken, epoch: session.term.inputGuard.epoch}
	}
}

// An accepted suffix must be echoed before the same screen can supply another
// suggestion. Otherwise two quick acceptance keys can append the suffix twice.
type suggestionEcho struct {
	line  string
	token string
	epoch uint64
}

func (state *State) reconcileSuggestionEcho(session *Session) {
	t := session.term
	pending := t.suggestionPending
	if pending == nil {
		return
	}
	if t.folderToken != pending.token || t.inputGuard.epoch != pending.epoch {
		t.suggestionPending = nil
		return
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok {
		return
	}
	if line == pending.line || t.inputGuard.active && t.inputGuard.known && line == t.inputGuard.prefix {
		t.suggestionPending = nil
	}
}
