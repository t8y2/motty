package term

import (
	"slices"
	"unicode"
)

// BufferPosition is one cell position in the retained terminal buffer. Line 0
// is the oldest scrollback line; screen lines follow the history.
type BufferPosition struct {
	Line   int
	Column int
}

// SearchMatch is one case-insensitive literal match. End is exclusive. A match
// may cross rows only where the terminal soft-wrapped them.
type SearchMatch struct {
	Start BufferPosition
	End   BufferPosition
}

// Search finds query in retained terminal text, reusing dst. At most limit
// matches are retained; truncated reports that more existed. Empty queries,
// nonpositive limits and queries containing a line break return no matches.
func (t *Terminal) Search(dst []SearchMatch, query string, limit int) ([]SearchMatch, bool) {
	dst = dst[:0]
	if limit <= 0 {
		return dst, false
	}
	truncated := false
	t.search(query, func(match SearchMatch) {
		if len(dst) < limit {
			dst = append(dst, match)
		} else {
			truncated = true
		}
	})
	return dst, truncated
}

// SearchFrom retains up to limit matches in search order from line, wrapping
// once. Results are returned in buffer order, with the first match's index.
// A match that spans the starting line is eligible in either direction.
func (t *Terminal) SearchFrom(dst []SearchMatch, query string, limit, line int, backward bool) ([]SearchMatch, int, bool) {
	dst = dst[:0]
	if limit <= 0 {
		return dst, -1, false
	}

	var before, after searchWindow
	before.matches = dst
	total := 0
	t.search(query, func(match SearchMatch) {
		total++
		earlier := match.End.Line < line
		if backward {
			earlier = match.Start.Line <= line
		}
		if earlier {
			before.add(match, limit, backward)
		} else {
			after.add(match, limit, backward)
		}
	})
	lo, hi := before.ordered(), after.ordered()
	if total == 0 {
		return lo, -1, false
	}
	if backward {
		hi = hi[max(0, len(hi)-(limit-len(lo))):]
		first := len(lo) - 1
		if first < 0 {
			first = len(hi) - 1
		}
		return append(lo, hi...), first, total > limit
	}
	lo = lo[:min(len(lo), limit-len(hi))]
	first := 0
	if len(hi) != 0 {
		first = len(lo)
	}
	return append(lo, hi...), first, total > limit
}

type searchWindow struct {
	matches []SearchMatch
	next    int
}

func (w *searchWindow) add(match SearchMatch, limit int, keepLast bool) {
	if len(w.matches) < limit {
		w.matches = append(w.matches, match)
	} else if keepLast {
		w.matches[w.next] = match
		w.next = (w.next + 1) % limit
	}
}

func (w searchWindow) ordered() []SearchMatch {
	if w.next == 0 {
		return w.matches
	}
	slices.Reverse(w.matches[:w.next])
	slices.Reverse(w.matches[w.next:])
	slices.Reverse(w.matches)
	return w.matches
}

func (t *Terminal) search(query string, add func(SearchMatch)) {
	if query == "" {
		return
	}
	needle := []rune(query)
	for index, value := range needle {
		if value == '\r' || value == '\n' {
			return
		}
		needle[index] = unicode.ToLower(value)
	}
	prefix := searchPrefix(needle)
	starts := make([]BufferPosition, len(needle))
	matched, emitted := 0, 0

	emit := func(value rune, start, end BufferPosition) {
		value = unicode.ToLower(value)
		for matched > 0 && needle[matched] != value {
			matched = prefix[matched-1]
		}
		if needle[matched] == value {
			matched++
		}
		starts[emitted%len(starts)] = start
		emitted++
		if matched != len(needle) {
			return
		}
		add(SearchMatch{Start: starts[(emitted-len(needle))%len(starts)], End: end})
		matched = prefix[matched-1]
	}

	for lineIndex := 0; lineIndex < t.bufferLineCount(); lineIndex++ {
		line := t.bufferLine(lineIndex)
		used := retainedLineUsedCells(line)
		mark := 0
		for column := 0; column < used; column++ {
			cell := line.cells[column]
			if cell.Attr&WideTail != 0 {
				continue
			}
			width := 1
			if cell.Attr&WideHead != 0 {
				width = 2
			}
			value := cell.R
			if value == 0 || value < 0x20 || value >= 0x7f && value < 0xa0 || cell.Attr&Hidden != 0 {
				value = ' '
			}
			position := BufferPosition{Line: lineIndex, Column: column}
			end := BufferPosition{Line: lineIndex, Column: min(column+width, t.cols)}
			emit(value, position, end)
			for mark < len(line.marks) && int(line.marks[mark].Col) < column {
				mark++
			}
			for mark < len(line.marks) && int(line.marks[mark].Col) == column {
				emit(line.marks[mark].R, position, end)
				mark++
			}
		}
		if !line.wrapped {
			matched, emitted = 0, 0
		}
	}
}

func searchPrefix(needle []rune) []int {
	prefix := make([]int, len(needle))
	for index, matched := 1, 0; index < len(needle); index++ {
		for matched > 0 && needle[index] != needle[matched] {
			matched = prefix[matched-1]
		}
		if needle[index] == needle[matched] {
			matched++
		}
		prefix[index] = matched
	}
	return prefix
}

func retainedLineUsedCells(line *line) int {
	used := len(line.cells)
	lastMark := -1
	if len(line.marks) > 0 {
		lastMark = int(line.marks[len(line.marks)-1].Col)
	}
	for used > lastMark+1 {
		cell := line.cells[used-1]
		if cell.R != 0 && cell.R != ' ' || cell.Attr&WideTail != 0 {
			break
		}
		used--
	}
	return used
}

func (t *Terminal) bufferLineCount() int {
	if t.onAlt {
		return len(t.alt.lines)
	}
	return t.histCount + len(t.main.lines)
}

func (t *Terminal) bufferLine(index int) *line {
	if t.onAlt {
		return t.alt.lines[index]
	}
	if index < t.histCount {
		return t.histAt(index)
	}
	return t.main.lines[index-t.histCount]
}

// ViewLine returns the retained-buffer line displayed at view row, or -1 when
// row lies outside the screen.
func (t *Terminal) ViewLine(row int) int {
	if row < 0 || row >= t.rows {
		return -1
	}
	if t.onAlt {
		return row
	}
	return t.histCount - t.view + row
}

// RevealLine scrolls the main-screen view to include line, centered when the
// retained history permits it. The alternate screen cannot scroll.
func (t *Terminal) RevealLine(line int) bool {
	if line < 0 || line >= t.bufferLineCount() || t.onAlt {
		return false
	}
	top := clampInt(line-t.rows/2, 0, t.histCount)
	view := t.histCount - top
	if view == t.view {
		return false
	}
	t.view = view
	t.version++
	return true
}
