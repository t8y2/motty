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
// read from the history file the first time and kept in step with it after.
func (state *State) commandGhostEntries(session *Session) []cmdhistory.Entry {
	h := &state.history
	key := historyKey(session)
	if entries, ok := h.ghost[key]; ok {
		return entries
	}
	store := state.historyStore()
	if store == nil {
		return nil
	}
	entries, err := store.List(key)
	if err != nil {
		return nil
	}
	if h.ghost == nil {
		h.ghost = map[string][]cmdhistory.Entry{}
	}
	h.ghost[key] = entries
	return entries
}

// noteGhostCommand puts a command just remembered at the front of the cache.
func (state *State) noteGhostCommand(key, command string) {
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

// forgetGhostEntries drops the cache after commands were deleted.
func (state *State) forgetGhostEntries() { state.history.ghost = nil }

// commandGhost is the rest of the command the line being typed in the session
// continues into, when one is to be offered.
func (state *State) commandGhost(session *Session) (string, bool) {
	h := &state.history
	if !h.on || h.suggestOff || session == nil || session.term == nil || session.term.shell == nil {
		return "", false
	}
	t := session.term
	if !state.historyAvailable(session) || session.host.NoHistory || t.preedit.Composing || !t.screen.Cursor().Visible {
		return "", false
	}
	if _, _, _, predicting := t.prediction.overlay(); predicting {
		return "", false
	}
	line, ok := t.recorder.Line(cmdhistory.TermScreen{Term: t.screen})
	if !ok {
		return "", false
	}
	return cmdhistory.Suggest(state.commandGhostEntries(session), line)
}

// acceptGhost types the offered text, or its first word, into the shell. It
// reports false when there is nothing on offer, leaving the key to the shell.
func (state *State) acceptGhost(session *Session, word bool) bool {
	rest, ok := state.commandGhost(session)
	if !ok {
		return false
	}
	if word {
		rest = firstWord(rest)
	}
	state.typed(session, []byte(rest))
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
