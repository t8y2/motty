package term

import "unicode/utf8"

// LogicalLineText reads the logical line containing view row, joining only
// soft-wrapped rows, including those in scrollback. id names its first row.
// Empty padding at a wide-character wrap is omitted; spaces actually written
// before a wrap are preserved. ok is false for an invalid row or a line longer
// than maxBytes, so consumers of untrusted output can bound their work.
func (t *Terminal) LogicalLineText(row, maxBytes int) (text string, id uint64, ok bool) {
	index := t.ViewLine(row)
	if index < 0 || maxBytes <= 0 {
		return "", 0, false
	}
	first, last := index, index
	for first > 0 && t.bufferLine(first-1).wrapped {
		first--
		if last-first >= maxBytes {
			return "", 0, false
		}
	}
	for last+1 < t.bufferLineCount() && t.bufferLine(last).wrapped {
		last++
		if last-first >= maxBytes {
			return "", 0, false
		}
	}
	var scratch [256]byte
	data := scratch[:0]
	for index := first; index <= last; index++ {
		line := t.bufferLine(index)
		end := min(line.used, len(line.cells), t.cols)
		marked := 0
		if len(line.marks) > 0 {
			marked = int(line.marks[len(line.marks)-1].Col) + 1
		}
		for end > marked {
			cell := line.cells[end-1]
			if cell.Attr&WideTail != 0 || cell.R != 0 && (index != last || cell.R != ' ') {
				break
			}
			end--
		}
		mark := 0
		for x := 0; x < end; x++ {
			cell := line.cells[x]
			if cell.Attr&WideTail != 0 {
				continue
			}
			r := cell.R
			if r == 0 {
				r = ' '
			}
			data = utf8.AppendRune(data, r)
			if len(data) > maxBytes {
				return "", 0, false
			}
			for mark < len(line.marks) && int(line.marks[mark].Col) < x {
				mark++
			}
			for mark < len(line.marks) && int(line.marks[mark].Col) == x {
				data = utf8.AppendRune(data, line.marks[mark].R)
				if len(data) > maxBytes {
					return "", 0, false
				}
				mark++
			}
		}
	}
	return string(data), t.bufferLine(first).id, true
}

// RowIsWrapped reports whether view row is part of a soft-wrapped line
// (either it continues on the next row, or the row before it wrapped into it).
func (t *Terminal) RowIsWrapped(row int) bool {
	if t == nil {
		return false
	}
	index := t.ViewLine(row)
	if index < 0 || index >= t.bufferLineCount() {
		return false
	}
	if t.bufferLine(index).wrapped {
		return true
	}
	return index > 0 && t.bufferLine(index-1).wrapped
}

// LogicalLineRows returns the slice of visible or retained Rows that form the
// soft-wrapped logical line containing view row, including any rows in scrollback.
func (t *Terminal) LogicalLineRows(row int) []Row {
	if t == nil {
		return nil
	}
	index := t.ViewLine(row)
	if index < 0 || index >= t.bufferLineCount() {
		return nil
	}
	first, last := index, index
	for first > 0 && t.bufferLine(first-1).wrapped {
		first--
	}
	for last+1 < t.bufferLineCount() && t.bufferLine(last).wrapped {
		last++
	}
	count := last - first + 1
	if count <= 1 {
		return []Row{t.Row(row)}
	}
	rows := make([]Row, count)
	for i := 0; i < count; i++ {
		rows[i] = t.rowOf(t.bufferLine(first + i))
	}
	return rows
}

// LogicalLineRange returns the retained-buffer bounds of the logical line at a view row.
func (t *Terminal) LogicalLineRange(row int) (first, last int, ok bool) {
	if t == nil {
		return 0, 0, false
	}
	index := t.ViewLine(row)
	if index < 0 || index >= t.bufferLineCount() {
		return 0, 0, false
	}
	first, last = index, index
	for first > 0 && t.bufferLine(first-1).wrapped {
		first--
	}
	for last+1 < t.bufferLineCount() && t.bufferLine(last).wrapped {
		last++
	}
	return first, last, true
}

// RetainedRow returns a read-only row in the retained buffer.
func (t *Terminal) RetainedRow(index int) (Row, bool) {
	if t == nil || index < 0 || index >= t.bufferLineCount() {
		return Row{}, false
	}
	return t.rowOf(t.bufferLine(index)), true
}
