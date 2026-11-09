package workbench

import (
	"strings"
	"time"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/cmdhistory"
	"motty/internal/term"
)

// Inline command suggestions: the rest of a command that the line being typed
// most likely continues into, drawn dim after the cursor, taken with Right or
// End (a word at a time with Alt or Ctrl+Right).
//
// Ownership: the line comes from the emulator's screen and the candidates from
// the command history, both application state, and what is accepted is typed
// into the shell as the user's own keys. Nothing here is reusable desktop
// machinery, so nothing moved into Viva.
//
// A suggestion exists only where the recorder would also remember the line (a
// shell prompt that does not ask for a secret, a line that does not start with
// a blank), only while the history is on, and never beside a suggestion the
// shell draws itself.

// commandGhostEntries is the candidates for a session's host, newest first,
// prepared in the background and kept coherent with history changes.
func (state *State) commandGhostEntries(session *Session) []cmdhistory.Entry {
	if session == nil || state.historyClearing() {
		return nil
	}
	return state.history.ghost[historyKey(session)]
}

// everyGhostEntries is the candidates merged across every host, prepared in
// the background under the empty key (adoptCompletion asks for it beside the
// host's own). A host whose own history has no match for the line still
// suggests what the user typed elsewhere: the same machines often share one
// job (#219). The merged list holds this host's lines too, so a fallback hit
// may come from this host while its own cache is still loading; painting only
// reads what has landed and never starts that loading.
func (state *State) everyGhostEntries() []cmdhistory.Entry {
	if state.historyClearing() {
		return nil
	}
	return state.history.ghost[""]
}

// noteGhostCommand puts a command just remembered at the front of the cache,
// the host's own and the merged one, so the next host of the same job
// suggests it at once (#219). used is the moment it was recorded, so the list
// keeps showing the most recent use rather than the time of an older run.
func (state *State) noteGhostCommand(key, command string, used time.Time) {
	state.noteGhostEntry("", command, used)
	state.noteGhostEntry(key, command, used)
}

func (state *State) noteGhostEntry(key, command string, used time.Time) {
	entries, ok := state.history.ghost[key]
	if !ok {
		return
	}
	for index := range entries {
		if entries[index].Command == command {
			entry := entries[index]
			entry.Count++
			entry.Used = used
			copy(entries[1:index+1], entries[:index])
			entries[0] = entry
			return
		}
	}
	state.history.ghost[key] = append([]cmdhistory.Entry{{Command: command, Count: 1, Used: used}}, entries...)
}

// commandGhost is the rest of the command the line being typed in the session
// continues into, when one is to be offered. The host's history comes first;
// when it has no match, what the user typed on their other hosts is tried
// (#219), and a single catalog candidate is offered last, so a host with
// learning off still gets public completions.
func (state *State) commandGhost(session *Session) (string, bool) {
	rest, _, ok := state.commandGhostSource(session)
	return rest, ok
}

// commandGhostSource also reports whether the suggestion came from the
// catalog. Only that text is revalidated before it is typed; history is the
// user's own input.
func (state *State) commandGhostSource(session *Session) (rest string, fromCatalog, ok bool) {
	h := &state.history
	if h.suggestOff || h.inlineOff || session == nil || session.term == nil || session.term.shell == nil {
		return "", false, false
	}
	// Accepting is checked against one terminal's prompt and never typed into
	// several at once, so a suggestion that could not be accepted is not shown.
	if state.broadcast != 0 {
		return "", false, false
	}
	t := session.term
	if t.suggestionPending != nil || t.preedit.Composing || !t.screen.Cursor().Visible {
		return "", false, false
	}
	if _, _, _, predicting := t.prediction.overlay(state.now()); predicting {
		return "", false, false
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok {
		return "", false, false
	}
	if h.on && state.historyAvailable(session) && !session.host.NoHistory {
		if rest, ok := cmdhistory.Suggest(state.commandGhostEntries(session), line); ok {
			return rest, false, true
		}
		if rest, ok := cmdhistory.Suggest(state.everyGhostEntries(), line); ok {
			return rest, false, true
		}
	}
	rest, ok = state.staticGhost(session, line)
	return rest, ok, ok
}

// acceptGhost types the offered text, or its first word, into the shell. It
// reports false when there is nothing on offer, leaving the key to the shell.
func (state *State) acceptGhost(session *Session, word bool) bool {
	rest, fromCatalog, ok := state.commandGhostSource(session)
	if !ok {
		return false
	}
	line, ok := session.term.recorder.Line(cmdhistory.TermScreen{Term: session.term.screen})
	if !ok {
		return true
	}
	snapshot := state.captureSuggestion(session, line)
	current := state.suggestionOwner(snapshot)
	if current == nil {
		return true
	}
	// Unlike the initial paint, this lookup sees all output already queued.
	// A rejected acceptance is consumed; it must not become a program key.
	now, nowFromCatalog, ok := state.commandGhostSource(current)
	if !ok || now != rest {
		return true
	}
	fromCatalog = nowFromCatalog
	if word {
		rest = firstWord(rest)
	}
	state.sendSuggestion(current, rest, fromCatalog)
	return true
}

// firstWord is the leading blanks of text and the word after them.
func firstWord(text string) string {
	end := 0
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if !unicode.IsSpace(r) {
			break
		}
		end += size
	}
	for end < len(text) {
		r, size := utf8.DecodeRuneInString(text[end:])
		if unicode.IsSpace(r) {
			break
		}
		end += size
	}
	return text[:end]
}

// ghostKey takes the keys that accept a suggestion.
func (state *State) ghostKey(session *Session, event gfx.KeyEvent) bool {
	if event.Modifiers&^(gfx.ModifierAlt|gfx.ModifierControl) != 0 {
		return false
	}
	switch {
	case event.Key == gfx.KeyRight && event.Modifiers == 0, event.Key == gfx.KeyEnd && event.Modifiers == 0:
		return state.acceptGhost(session, false)
	case event.Key == gfx.KeyRight:
		return state.acceptGhost(session, true)
	}
	return false
}

// paintGhost draws the suggestion after the cursor, clipped before whatever
// the shell has drawn further along the row (a right-hand prompt) and the edge.
func (state *State) paintGhost(canvas *gfx.Canvas, t *terminal, geometry terminalGeometry, cursor term.Cursor, rest string, cursorShown bool) {
	cols, _ := t.screen.Size()
	limit := cols
	cells := t.screen.Row(cursor.Y).Cells
	for column := cursor.X; column < len(cells) && column < cols; column++ {
		if r := cells[column].R; r != 0 && r != ' ' {
			limit = column - 1
			break
		}
	}
	available := limit - cursor.X
	if available <= 0 {
		return
	}
	var runs []gfx.TextRun
	var shown strings.Builder
	used := 0
	for _, r := range rest {
		width := term.Width(r)
		if width <= 0 || r < 0x20 || r == 0x7f {
			break
		}
		if used+width > available {
			break
		}
		runs = append(runs, gfx.TextRun{Column: int32(used), Bytes: int32(utf8.RuneLen(r)), Cells: int32(width), Color: withAlpha(state.termPal.termFG, 0.42)})
		shown.WriteRune(r)
		used += width
	}
	if used == 0 {
		return
	}
	if cursorShown {
		// A block cursor covers the first cell: its character takes the cursor's
		// own contrast.
		runs[0].Color = state.termPal.terminal
	}
	style := gfx.TextStyle{Size: state.terminalFontSize, Color: state.termPal.termFG, MonoFace: state.terminalFontFace}
	x := geometry.left + float32(cursor.X)*state.cellWidth
	y := geometry.top + float32(cursor.Y)*state.cellHeight
	canvas.LabelRuns(shown.String(), gfx.Rect{X: x, Y: y, Width: float32(used) * state.cellWidth, Height: state.cellHeight}, style, state.cellWidth, runs)
}
