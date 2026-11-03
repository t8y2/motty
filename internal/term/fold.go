package term

import "sort"

// A command block is what one command of the shell's integration owns, as the
// OSC 133 markers delimit it: it opens on the row its prompt started on (A), the
// command is typed on the rows up to the one its output starts on (C), and the
// output runs to the row before the next prompt starts, or to the row before the
// cursor while the command is still the last. Folding a block hides its output,
// never the command line, from the view alone: the buffer, the scrollback, a
// selection and its copy, a search and the text handed to the assistant keep
// every row, so unfolding restores the screen exactly.
//
// The markers are kept on the lines themselves, as the OR of what fell on the
// physical rows of a logical line, on its first row. A block therefore follows
// its lines through scrolling, the history, a resize, an erased display and the
// history dropping its oldest lines, with no table to keep in step with them,
// and the reader's fold choice lives on the block's first row too. A shell
// without the integration sends no markers and has no blocks.
//
// The integration sends A, B and C but not D, the end of a command. A block
// ends where the next prompt starts, which is where a standard terminal would
// also end it for a shell that never reports the exit.

// lineMark is what the shell's markers recorded on a line.
type lineMark uint8

const (
	// markPrompt is set on the row a prompt started on, OSC 133 A. It is the
	// anchor of its block, the row the fold box sits on.
	markPrompt lineMark = 1 << iota
	// markOutput is set on the row a command's output starts on, OSC 133 C.
	markOutput
	// markFolded is set on an anchor whose output the reader folded shut.
	markFolded
)

// FoldInfo is what the view knows about the command block around a view row.
type FoldInfo struct {
	// Anchor marks the row a block opens on when the block has output to hide.
	Anchor bool
	// Folded marks an anchor whose output is folded shut.
	Folded bool
	// Inside marks a row the block owns while it is open: the rest of the
	// command and its output.
	Inside bool
}

// viewRow is one row the view shows: its row in the window of the buffer, as
// Row and lineAt count it, and the block structure around it. Folded rows are
// not in the list, so the rows after them move up.
type viewRow struct {
	index  int
	anchor bool
	folded bool
	inside bool
}

// block is a command block found around some row.
type block struct {
	anchor int // the row the prompt started on
	// out is the first row of the output, or -1 while the row asked about lies
	// in the part of the block that is the prompt and the typed command.
	out int
	end int // the last row of the output, when out is set
}

// SetFolding makes the main screen's view fold the command blocks the reader
// folded shut. Off, every block shows in full whatever was folded before; the
// choices stay on their lines and come back when it is turned on again, since
// the gutter that offers them is a display preference.
func (t *Terminal) SetFolding(on bool) {
	if t.folding == on {
		return
	}
	t.folding = on
	t.version++
}

// markPromptStart records OSC 133 A on the row under the cursor. A full-screen
// program owns every row of the alternate screen and has no prompt to mark.
func (t *Terminal) markPromptStart() {
	if t.onAlt {
		return
	}
	t.sawMarks = true
	if l := t.scr.lines[t.cy]; l.mark&markPrompt == 0 {
		l.mark |= markPrompt
		t.version++
	}
}

// markOutputStart records OSC 133 C. The output starts on the row under the
// cursor, so a cursor still inside the typed command marks nothing: hiding that
// row would hide the command.
func (t *Terminal) markOutputStart() {
	if t.onAlt || t.cx != 0 {
		return
	}
	t.sawMarks = true
	if l := t.scr.lines[t.cy]; l.mark&markOutput == 0 {
		l.mark |= markOutput
		t.version++
	}
}

func (t *Terminal) markAt(index int) lineMark { return t.bufferLine(index).mark }

// cursorIndex is the buffer row the cursor is on, which ends the last block.
func (t *Terminal) cursorIndex() int { return t.histCount + t.cy }

// prevMarked is the last row at or before index that carries a marker, or -1.
func (t *Terminal) prevMarked(index int) int {
	for ; index >= 0; index-- {
		if t.markAt(index)&(markPrompt|markOutput) != 0 {
			return index
		}
	}
	return -1
}

// outputEnd is the last row of the output that holds row out: the row before
// the next prompt, or before the cursor while no prompt follows.
func (t *Terminal) outputEnd(out int) int {
	limit := min(t.bufferLineCount(), t.cursorIndex())
	for k := out + 1; k < limit; k++ {
		if t.markAt(k)&markPrompt != 0 {
			return k - 1
		}
	}
	return limit - 1
}

// blockAt is the block row index belongs to, found from the nearest marker at or
// above it. A C with no prompt before it, such as the one the integration sends
// when it starts, belongs to no block, and neither does a row at or below the
// cursor that no output has reached yet.
func (t *Terminal) blockAt(index int) (block, bool) {
	if index < 0 || index >= t.bufferLineCount() {
		return block{}, false
	}
	j := t.prevMarked(index)
	if j < 0 {
		return block{}, false
	}
	if t.markAt(j)&markPrompt != 0 {
		return block{anchor: j, out: -1}, true
	}
	a := t.prevMarked(j - 1)
	if a < 0 || t.markAt(a)&markPrompt == 0 {
		return block{}, false
	}
	end := t.outputEnd(j)
	if index > end {
		return block{}, false
	}
	return block{anchor: a, out: j, end: end}, true
}

// outputOf is the row the output of the block whose prompt starts on anchor
// starts on: a C on a later row that the cursor has passed, before the next
// prompt starts. A C on the anchor's own row is the previous block's, which that
// prompt closed with no output.
func (t *Terminal) outputOf(anchor int) (int, bool) {
	limit := min(t.bufferLineCount(), t.cursorIndex())
	for k := anchor + 1; k < limit; k++ {
		m := t.markAt(k)
		if m&markPrompt != 0 {
			return 0, false
		}
		if m&markOutput != 0 {
			return k, true
		}
	}
	return 0, false
}

// blocks reports whether the view can have command blocks: the main screen of a
// terminal that folds, once the shell has sent a marker. Until then a session
// pays nothing for them, and a terminal without the integration never does.
func (t *Terminal) blocks() bool { return t.folding && t.sawMarks && !t.onAlt }

// identity reports whether view rows are the window's rows: the alternate
// screen, a terminal that does not fold, one whose shell sent no markers, and a
// view in which no block is folded.
func (t *Terminal) identity() bool {
	if !t.blocks() || !t.anyFolded {
		return true
	}
	t.viewRows()
	return t.shownIdentity
}

// windowIndex is the row in the window of the buffer that view row i shows.
func (t *Terminal) windowIndex(i int) (int, bool) {
	if i < 0 || i >= t.rows {
		return 0, false
	}
	if !t.blocks() || !t.anyFolded {
		return i, true
	}
	rows := t.viewRows()
	if i >= len(rows) {
		return 0, false
	}
	return rows[i].index, true
}

// viewRows is the rows the main screen's view shows, built once per change of
// the buffer or the scroll. A terminal that does not fold shows the window as it
// is, and so does the alternate screen; neither builds the list.
func (t *Terminal) viewRows() []viewRow {
	if t.shownOK && t.shownAt == t.version && t.shownFrom == t.view {
		return t.shown
	}
	t.shownOK, t.shownAt, t.shownFrom = true, t.version, t.view
	t.shownIdentity = true
	rows := t.shown[:0]
	start := t.histCount - t.view
	count := t.bufferLineCount()
	cursor := t.cursorIndex()

	const (
		outside = iota
		command
		output
	)
	// What the rows above the window leave open: the window may start in the
	// middle of a block, even in the middle of a folded one.
	var (
		phase  = outside
		anchor int
		slot   = -1 // where the anchor sits in rows, when the window holds it
		folded bool
	)
	if b, ok := t.blockAt(start - 1); ok {
		anchor = b.anchor
		switch {
		case b.out < 0:
			phase = command
		case b.end >= start:
			phase, folded = output, t.markAt(b.anchor)&markFolded != 0
		}
	}
	for index := start; index < count && len(rows) < t.rows; {
		m := t.markAt(index)
		if m&markOutput != 0 && phase == command {
			if m&markPrompt == 0 && index < cursor {
				phase, folded = output, t.markAt(anchor)&markFolded != 0
				if slot >= 0 {
					rows[slot].anchor, rows[slot].folded = true, folded
					if !folded {
						// The command's own continuation rows belong to the block.
						for k := slot + 1; k < len(rows); k++ {
							rows[k].inside = true
						}
					}
				}
			} else {
				phase = outside
			}
		}
		if m&markPrompt != 0 {
			phase, anchor, slot = command, index, len(rows)
		}
		if phase == output && index >= cursor {
			phase = outside
		}
		if phase == output && folded {
			index = t.outputEnd(index) + 1
			phase = outside
			t.shownIdentity = false
			continue
		}
		rows = append(rows, viewRow{index: index - start, inside: phase == output})
		index++
	}
	t.shown = rows
	return rows
}

// FoldAt is what the view knows about the command block around view row.
func (t *Terminal) FoldAt(row int) FoldInfo {
	if !t.blocks() {
		return FoldInfo{}
	}
	rows := t.viewRows()
	if row < 0 || row >= len(rows) {
		return FoldInfo{}
	}
	return FoldInfo{Anchor: rows[row].anchor, Folded: rows[row].folded, Inside: rows[row].inside}
}

// displayRow is where the window's row index is shown, or -1 when a fold hides
// it or the view does not reach it.
func (t *Terminal) displayRow(index int) int {
	if !t.blocks() || !t.anyFolded {
		if index >= 0 && index < t.rows {
			return index
		}
		return -1
	}
	rows := t.viewRows()
	i := sort.Search(len(rows), func(i int) bool { return rows[i].index >= index })
	if i < len(rows) && rows[i].index == index {
		return i
	}
	return -1
}

// flip folds the block anchored on buffer row anchor shut, or opens it again.
func (t *Terminal) flip(anchor int) {
	l := t.bufferLine(anchor)
	l.mark ^= markFolded
	t.anyFolded = t.anyFolded || l.mark&markFolded != 0
	t.version++
}

// ToggleFold folds the block whose box is on view row shut, or opens it again.
// It reports whether anything changed: a row that opens no block with output
// does nothing.
func (t *Terminal) ToggleFold(row int) bool {
	if !t.blocks() {
		return false
	}
	rows := t.viewRows()
	if row < 0 || row >= len(rows) || !rows[row].anchor {
		return false
	}
	t.flip(t.histCount - t.view + rows[row].index)
	return true
}

// ToggleLastFold folds or opens the output of the latest command that has any,
// the one nearest above the cursor.
func (t *Terminal) ToggleLastFold() bool {
	if !t.blocks() {
		return false
	}
	for j := t.prevMarked(t.cursorIndex() - 1); j >= 0; j = t.prevMarked(j - 1) {
		m := t.markAt(j)
		if m&markOutput == 0 || m&markPrompt != 0 {
			continue
		}
		if a := t.prevMarked(j - 1); a >= 0 && t.markAt(a)&markPrompt != 0 {
			t.flip(a)
			return true
		}
	}
	return false
}

// UnfoldAll opens every folded block and reports whether there was one.
func (t *Terminal) UnfoldAll() bool {
	if t.onAlt {
		return false
	}
	t.anyFolded = false
	changed := false
	for index, count := 0, t.bufferLineCount(); index < count; index++ {
		if l := t.bufferLine(index); l.mark&markFolded != 0 {
			l.mark &^= markFolded
			changed = true
		}
	}
	if changed {
		t.version++
	}
	return changed
}

// unfoldAround opens the folded block that hides buffer row index, if any, and
// reports whether it did.
func (t *Terminal) unfoldAround(index int) bool {
	if !t.blocks() || !t.anyFolded {
		return false
	}
	b, ok := t.blockAt(index)
	if !ok || b.out < 0 || t.markAt(b.anchor)&markFolded == 0 {
		return false
	}
	t.flip(b.anchor)
	return true
}

// snapView moves a view that scrolling would leave inside a folded block's
// hidden rows out of them: to the block's first row when it was heading back
// into the history, past its last row when it was heading to the live screen.
// Without it the wheel would spend one notch on every hidden row.
func (t *Terminal) snapView(view int, back bool) int {
	if !t.blocks() || !t.anyFolded {
		return view
	}
	start := t.histCount - view
	if b, ok := t.blockAt(start); ok && b.out >= 0 && b.out <= start && t.markAt(b.anchor)&markFolded != 0 {
		if back {
			start = b.anchor
		} else {
			start = b.end + 1
		}
	}
	return clampInt(t.histCount-start, 0, t.histCount)
}

// ScreenRow is where a view row sits on the screen the application draws on. It
// reports false when the application has no such row: the view shows the
// history, or the row is not the application's in a view with folded rows.
func (t *Terminal) ScreenRow(row int) (int, bool) {
	if t.identity() {
		return row, true
	}
	index, ok := t.windowIndex(row)
	if !ok {
		return 0, false
	}
	line := t.histCount - t.view + index
	if line < t.histCount {
		return 0, false
	}
	return line - t.histCount, true
}
