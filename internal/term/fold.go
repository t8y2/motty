package term

// A command block is the run of rows one command produced: it opens on the row
// the command was typed on and runs to the row before the next command, or to
// the row before the cursor while it is the last. Folding a block hides what it
// printed from the view alone: the buffer, the scrollback, the selection and the
// copy text keep every row, so unfolding restores the screen exactly.
//
// A block is identified by the line the command was typed on, which keeps its
// identity through scrolling, resizing and a reflow of the history.
type FoldInfo struct {
	// Anchor marks a row a block opens on that has rows to hide.
	Anchor bool
	// Folded marks a block the reader has folded shut.
	Folded bool
	// Inside marks a row the anchor's block owns.
	Inside bool
}

// viewRow is one row the view shows: which view row of the buffer it comes from
// and the block structure around it. Folded rows are not in the list, so the
// rows after them move up.
type viewRow struct {
	index  int // the row in view coordinates, as Row and lineAt take it
	anchor bool
	folded bool
	inside bool
}

// MarkCommand records that a command starts on the row under the cursor, so the
// rows it prints can be folded away. A full-screen program owns every row, so
// the alternate screen records nothing.
func (t *Terminal) MarkCommand() {
	if t.onAlt {
		return
	}
	l := t.scr.lines[t.cy]
	if l.start {
		return
	}
	l.start = true
	t.version++
}

// FoldAt is what the view knows about the command block around row.
func (t *Terminal) FoldAt(row int) FoldInfo {
	rows := t.viewRows()
	if row < 0 || row >= len(rows) {
		return FoldInfo{}
	}
	return FoldInfo{Anchor: rows[row].anchor, Folded: rows[row].folded, Inside: rows[row].inside}
}

// ToggleFold folds the block that opens on row shut, or opens it again. It
// reports whether anything changed: a row that begins no block does nothing.
func (t *Terminal) ToggleFold(row int) bool {
	if t.onAlt {
		return false
	}
	rows := t.viewRows()
	if row < 0 || row >= len(rows) || !rows[row].anchor {
		return false
	}
	if t.folds == nil {
		t.folds = make(map[uint64]bool)
	}
	id := t.lineAt(rows[row].index).id
	if t.folds[id] {
		delete(t.folds, id)
	} else {
		t.folds[id] = true
	}
	t.version++
	return true
}

// viewRows is the rows the view shows, built once per change of the buffer, the
// scroll and the folds. Without folds it is the identity.
func (t *Terminal) viewRows() []viewRow {
	if t.shownOK && t.shownAt == t.version && t.shownFrom == t.view && t.shownFor == len(t.folds) {
		return t.shown
	}
	t.shownOK, t.shownAt, t.shownFrom, t.shownFor = true, t.version, t.view, len(t.folds)
	rows := t.shown[:0]
	start := t.histCount - t.view
	end := t.bufferLineCount()
	cursor := t.cursorIndex()
	for index := start; index < end && len(rows) < t.rows; {
		l := t.bufferLine(index)
		if !l.start || index >= cursor {
			rows = append(rows, viewRow{index: index - start})
			index++
			continue
		}
		last := t.blockEnd(index)
		if last <= index {
			rows = append(rows, viewRow{index: index - start})
			index++
			continue
		}
		folded := t.folds[l.id]
		rows = append(rows, viewRow{index: index - start, anchor: true, folded: folded})
		if !folded {
			for k := index + 1; k <= last && len(rows) < t.rows; k++ {
				rows = append(rows, viewRow{index: k - start, inside: true})
			}
		}
		index = last + 1
	}
	t.shown = rows
	return rows
}

// blockEnd is the last row of the block that opens at index: the row before the
// next command, or before the cursor when no command follows.
func (t *Terminal) blockEnd(index int) int {
	last := min(t.bufferLineCount()-1, t.cursorIndex()-1)
	for k := index + 1; k <= last; k++ {
		if t.bufferLine(k).start {
			return k - 1
		}
	}
	return last
}

// cursorIndex is the buffer row the cursor is on, which ends the last block. The
// alternate screen has no cursor row to speak of.
func (t *Terminal) cursorIndex() int {
	if t.onAlt {
		return 0
	}
	return t.histCount + t.cy
}

// displayRow is where view row index of the buffer is shown, or -1 when a fold
// hides it.
func (t *Terminal) displayRow(index int) int {
	rows := t.viewRows()
	for i := range rows {
		if rows[i].index == index {
			return i
		}
	}
	return -1
}

// ScreenRow is where a view row sits on the screen the application draws on. It
// reports false when the application has no such row: a fold hid it, or the view
// is scrolled back into the history.
func (t *Terminal) ScreenRow(row int) (int, bool) {
	line := t.ViewLine(row)
	if line < 0 {
		return 0, false
	}
	if t.onAlt {
		return line, line < t.rows
	}
	screen := line - t.histCount
	if screen < 0 || screen >= len(t.main.lines) {
		return 0, false
	}
	return screen, true
}
