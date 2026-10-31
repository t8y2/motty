package workbench

import (
	"strings"
	"unicode"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
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
// suggests it at once (#219).
func (state *State) noteGhostCommand(key, command string) {
	state.noteGhostEntry("", command)
	state.noteGhostEntry(key, command)
}

func (state *State) noteGhostEntry(key, command string) {
	entries, ok := state.history.ghost[key]
	if !ok {
		return
	}
	for index := range entries {
		if entries[index].Command == command {
			entry := entries[index]
			entry.Count++
			copy(entries[1:index+1], entries[:index])
			entries[0] = entry
			return
		}
	}
	state.history.ghost[key] = append([]cmdhistory.Entry{{Command: command, Count: 1}}, entries...)
}

// commandGhost is the rest of the command the line being typed in the session
// continues into, when one is to be offered. The host's history comes first;
// when it has no match, what the user typed on their other hosts is tried
// (#219), and a single catalog candidate is offered last, so a host with
// learning off still gets public completions.
func (state *State) commandGhost(session *Session) (string, bool) {
	h := &state.history
	if h.suggestOff || h.inlineOff || session == nil || session.term == nil || session.term.shell == nil {
		return "", false
	}
	// Accepting is checked against one terminal's prompt and never typed into
	// several at once, so a suggestion that could not be accepted is not shown.
	if state.broadcast != 0 {
		return "", false
	}
	t := session.term
	if t.suggestionPending != nil || t.preedit.Composing || !t.screen.Cursor().Visible {
		return "", false
	}
	if _, _, _, predicting := t.prediction.overlay(); predicting {
		return "", false
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok {
		return "", false
	}
	if h.on && state.historyAvailable(session) && !session.host.NoHistory {
		if rest, ok := cmdhistory.Suggest(state.commandGhostEntries(session), line); ok {
			return rest, true
		}
		if rest, ok := cmdhistory.Suggest(state.everyGhostEntries(), line); ok {
			return rest, true
		}
	}
	return state.staticGhost(session, line)
}

// acceptGhost types the offered text, or its first word, into the shell. It
// reports false when there is nothing on offer, leaving the key to the shell.
func (state *State) acceptGhost(session *Session, word bool) bool {
	rest, ok := state.commandGhost(session)
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
	if now, ok := state.commandGhost(current); !ok || now != rest {
		return true
	}
	if word {
		rest = firstWord(rest)
	}
	state.sendSuggestion(current, rest)
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
func (state *State) ghostKey(session *Session, event ui.KeyEvent) bool {
	if event.Modifiers&^(ui.ModifierAlt|ui.ModifierControl) != 0 {
		return false
	}
	switch {
	case event.Key == ui.KeyRight && event.Modifiers == 0, event.Key == ui.KeyEnd && event.Modifiers == 0:
		return state.acceptGhost(session, false)
	case event.Key == ui.KeyRight:
		return state.acceptGhost(session, true)
	}
	return false
}

// paintGhost draws the suggestion after the cursor, clipped before whatever
// the shell has drawn further along the row (a right-hand prompt) and the edge.
func (state *State) paintGhost(canvas *ui.Canvas, t *terminal, geometry terminalGeometry, cursor term.Cursor, rest string, cursorShown bool) {
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
	var runs []ui.TextRun
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
		runs = append(runs, ui.TextRun{Column: int32(used), Bytes: int32(utf8.RuneLen(r)), Cells: int32(width), Color: withAlpha(state.termPal.termFG, 0.42)})
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
	style := ui.TextStyle{Size: state.terminalFontSize, Color: state.termPal.termFG, MonoFace: state.terminalFontFace}
	x := geometry.left + float32(cursor.X)*state.cellWidth
	y := geometry.top + float32(cursor.Y)*state.cellHeight
	canvas.LabelRuns(shown.String(), ui.Rect{X: x, Y: y, Width: float32(used) * state.cellWidth, Height: state.cellHeight}, style, state.cellWidth, runs)
}
