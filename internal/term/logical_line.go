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

// LogicalLineRange returns the retained-buffer bounds of the logical line at a
// view row, clipped to the retained lines from through to. The walk stops at
// the clip, so a line soft-wrapped across the whole history costs only the rows
// the caller asked about.
func (t *Terminal) LogicalLineRange(row, from, to int) (first, last int, ok bool) {
	if t == nil {
		return 0, 0, false
	}
	index := t.ViewLine(row)
	from, to = max(from, 0), min(to, t.bufferLineCount()-1)
	if index < from || index > to {
		return 0, 0, false
	}
	first, last = index, index
	for first > from && t.bufferLine(first-1).wrapped {
		first--
	}
	for last < to && t.bufferLine(last).wrapped {
		last++
	}
	return first, last, true
}

// logicalNumberCache is the logical line number of one buffer row, kept from one
// gutter frame to the next so LogicalLineNumbers counts only the rows the view
// moved over instead of walking the whole scrollback. A write cannot move it:
// the rows before the first visible row are history rows, which do not change,
// so only a drop from the oldest end, a reflow or a clear can invalidate it;
// those paths adjust or clear it. See LogicalLineNumbers.
type logicalNumberCache struct {
	row    int    // the buffer row the number is for
	number uint64 // the logical line number of that row
	id     uint64 // the row's identity, so a buffer shift invalidates the cache
	valid  bool
}

// dropOldest moves the cache down one row after a line left the oldest end of the buffer,
// or drops it when its own row did. The rows that stay keep their numbers.
func (c *logicalNumberCache) dropOldest() {
	if !c.valid {
		return
	}
	if c.row == 0 {
		c.valid = false
		return
	}
	c.row--
}

// lineHead reports whether buffer row i begins a logical line:
// row 0 when the last dropped line did not wrap into it, every other row whose predecessor did not wrap.
func (t *Terminal) lineHead(i int) bool {
	return i == 0 && !t.droppedWrapped || i > 0 && !t.bufferLine(i-1).wrapped
}

// logicalLineNumber returns the number of the logical line buffer row i belongs to,
// counting every line the buffer has dropped and every line head up to i.
// It moves the cache to i, so the next frame counts only the rows the view moved
// over and the cost of a frame does not grow with the scrollback.
func (t *Terminal) logicalLineNumber(i int) uint64 {
	cache := &t.logicalNumbers
	if !cache.valid || cache.row >= t.bufferLineCount() || (cache.row >= 0 && t.bufferLine(cache.row).id != cache.id) {
		cache.row, cache.number = -1, t.scrolled-uint64(t.histCount)-t.droppedContinuations
	}
	for cache.row < i {
		cache.row++
		if t.lineHead(cache.row) {
			cache.number++
		}
	}
	for cache.row > i {
		if t.lineHead(cache.row) {
			cache.number--
		}
		cache.row--
	}
	cache.id, cache.valid = t.bufferLine(cache.row).id, true
	return cache.number
}

// LogicalLineNumbers fills numbers and heads for every view row: numbers[row]
// is the 1-based number of the row's logical line among every line the main
// buffer has produced, counting a soft-wrapped run as one line, and heads[row]
// says whether the row begins that line. A continuation row carries its line's
// number and shows nothing of its own, so rewrapping a line into more or fewer
// rows moves no number: the rows around it keep what they had. Numbers count
// past dropped history the way LineNumber does. It returns the slices, grown
// to the row count. A call costs the rows on screen plus the rows the view
// moved over since the last call, not the scrollback. On the alternate screen,
// where the numbers are not meaningful, every row is numbered as LineNumber
// numbers it.
func (t *Terminal) LogicalLineNumbers(numbers []uint64, heads []bool) ([]uint64, []bool) {
	if len(numbers) < t.rows {
		numbers = make([]uint64, t.rows)
	}
	if len(heads) < t.rows {
		heads = make([]bool, t.rows)
	}
	numbers, heads = numbers[:t.rows], heads[:t.rows]
	if t.onAlt {
		for row := range numbers {
			numbers[row], heads[row] = t.LineNumber(row), true
		}
		return numbers, heads
	}
	top := t.histCount - t.view
	count := t.logicalLineNumber(top)
	for i := top; i < top+t.rows; i++ {
		head := t.lineHead(i)
		if i > top && head {
			count++
		}
		numbers[i-top], heads[i-top] = count, head
	}
	return numbers, heads
}

// RetainedRow returns a read-only row in the retained buffer.
func (t *Terminal) RetainedRow(index int) (Row, bool) {
	if t == nil || index < 0 || index >= t.bufferLineCount() {
		return Row{}, false
	}
	return t.rowOf(t.bufferLine(index)), true
}
