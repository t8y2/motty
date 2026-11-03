package term

import (
	"strings"
	"unicode"
)

// Position identifies one visible terminal cell.
type Position struct {
	Row, Column int
}

// Point is one cell of the retained buffer. Line counts as LineNumber does, so
// a Point keeps naming the same text while the view scrolls and while the
// history drops its oldest lines. Selections use it to stay put under scrolling.
type Point struct {
	Line   uint64
	Column int
}

// PointAt is the buffer point under a view position.
func (t *Terminal) PointAt(position Position) Point {
	return Point{Line: t.LineNumber(position.Row), Column: position.Column}
}

// SelectedText returns the cells between two inclusive visible positions.
// Soft-wrapped rows are joined; hard line endings remain newlines.
func (t *Terminal) SelectedText(anchor, focus Position) string {
	return t.SelectedRange(t.PointAt(t.clampPosition(anchor)), t.PointAt(t.clampPosition(focus)))
}

// SelectedRange returns the cells between two inclusive buffer points, which
// may lie outside the view. Points beyond the retained lines clamp to them.
// Soft-wrapped rows are joined; hard line endings remain newlines.
func (t *Terminal) SelectedRange(anchor, focus Point) string {
	base, count := t.lineBase(), uint64(t.bufferLineCount())
	if count == 0 {
		return ""
	}
	anchor, focus = t.clampPoint(anchor, base, count), t.clampPoint(focus, base, count)
	if pointLess(focus, anchor) {
		anchor, focus = focus, anchor
	}

	var result strings.Builder
	result.Grow(int(min(focus.Line-anchor.Line+1, 1<<16)) * (t.cols + 1))
	for line := anchor.Line; line <= focus.Line; line++ {
		from, to := 0, t.cols-1
		if line == anchor.Line {
			from = anchor.Column
		}
		if line == focus.Line {
			to = focus.Column
		}
		row := t.rowOf(t.bufferLine(int(line - base)))
		writeSelectedCells(&result, row, from, to)
		if line < focus.Line && !row.Wrapped {
			result.WriteByte('\n')
		}
	}
	return result.String()
}

// lineBase is the line number of retained-buffer index 0.
func (t *Terminal) lineBase() uint64 {
	if t.onAlt {
		return t.scrolled + 1
	}
	return t.scrolled - uint64(t.histCount) + 1
}

func (t *Terminal) clampPoint(point Point, base, count uint64) Point {
	point.Line = min(max(point.Line, base), base+count-1)
	point.Column = clampInt(point.Column, 0, t.cols-1)
	return point
}

func pointLess(left, right Point) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Column < right.Column
}

// WordBounds returns the inclusive run of like cells around position. Letters,
// numbers, and underscores form words; spaces and punctuation form their own runs.
func (t *Terminal) WordBounds(position Position) (Position, Position) {
	position = t.clampPosition(position)
	row := t.Row(position.Row)
	column := position.Column
	if column < len(row.Cells) && row.Cells[column].Attr&WideTail != 0 && column > 0 {
		column--
	}
	kind := selectedCellKind(row.Cells, column)
	start := column
	for start > 0 {
		previous := start - 1
		if previous < len(row.Cells) && row.Cells[previous].Attr&WideTail != 0 && previous > 0 {
			previous--
		}
		if selectedCellKind(row.Cells, previous) != kind {
			break
		}
		start = previous
	}
	end := column
	for {
		next := end + selectedCellWidth(row.Cells, end)
		if next >= t.cols || selectedCellKind(row.Cells, next) != kind {
			break
		}
		end = next
	}
	end += selectedCellWidth(row.Cells, end) - 1
	return Position{Row: position.Row, Column: start}, Position{Row: position.Row, Column: min(end, t.cols-1)}
}

func (t *Terminal) clampPosition(position Position) Position {
	position.Row = clampInt(position.Row, 0, t.rows-1)
	position.Column = clampInt(position.Column, 0, t.cols-1)
	return position
}

func positionLess(left, right Position) bool {
	return left.Row < right.Row || left.Row == right.Row && left.Column < right.Column
}

func writeSelectedCells(result *strings.Builder, row Row, from, to int) {
	to = selectedRowEnd(row, from, to)
	mark := 0
	for column := from; column <= to; column++ {
		for mark < len(row.Marks) && int(row.Marks[mark].Col) < column {
			mark++
		}
		if column >= len(row.Cells) {
			result.WriteByte(' ')
			continue
		}
		cell := row.Cells[column]
		if cell.Attr&WideTail != 0 {
			continue
		}
		character := cell.R
		if character == 0 {
			character = ' '
		}
		result.WriteRune(character)
		for mark < len(row.Marks) && int(row.Marks[mark].Col) == column {
			result.WriteRune(row.Marks[mark].R)
			mark++
		}
	}
}

func selectedRowEnd(row Row, from, to int) int {
	for to >= from {
		if to >= len(row.Cells) {
			to--
			continue
		}
		cell := row.Cells[to]
		if cell.Attr&WideTail != 0 {
			if to > from && selectedCellContent(row, to-1) {
				return to
			}
			to--
			continue
		}
		if selectedCellContent(row, to) {
			return to
		}
		to--
	}
	return from - 1
}

func selectedCellContent(row Row, column int) bool {
	if column >= 0 && column < len(row.Cells) {
		character := row.Cells[column].R
		if character != 0 && character != ' ' {
			return true
		}
	}
	for _, mark := range row.Marks {
		if int(mark.Col) == column {
			return true
		}
		if int(mark.Col) > column {
			break
		}
	}
	return false
}

type cellKind uint8

const (
	cellBlank cellKind = iota
	cellWord
	cellPunctuation
)

func selectedCellKind(cells []Cell, column int) cellKind {
	if column < 0 || column >= len(cells) || cells[column].R == 0 || unicode.IsSpace(cells[column].R) {
		return cellBlank
	}
	if cells[column].R == '_' || unicode.IsLetter(cells[column].R) || unicode.IsNumber(cells[column].R) {
		return cellWord
	}
	return cellPunctuation
}

func selectedCellWidth(cells []Cell, column int) int {
	if column >= 0 && column < len(cells) && cells[column].Attr&WideHead != 0 {
		return 2
	}
	return 1
}

// TokenAt returns the run of non-blank text around a visible cell, without the
// brackets, quotes and sentence punctuation that usually wrap it: what a person
// would call "that address" or "that path" when pointing at it. It is empty on a
// blank cell. The token stays on its row, so a soft-wrapped path is cut where the
// wrap fell.
func (t *Terminal) TokenAt(position Position) string {
	position = t.clampPosition(position)
	row := t.Row(position.Row)
	blank := func(column int) bool {
		if column >= len(row.Cells) {
			return true
		}
		cell := row.Cells[column]
		return cell.Attr&WideTail == 0 && (cell.R == 0 || unicode.IsSpace(cell.R))
	}
	if blank(position.Column) {
		return ""
	}
	start, end := position.Column, position.Column
	for start > 0 && !blank(start-1) && position.Column-start < 512 {
		start--
	}
	for end+1 < t.cols && !blank(end+1) && end-position.Column < 512 {
		end++
	}
	text := t.SelectedText(Position{Row: position.Row, Column: start}, Position{Row: position.Row, Column: end})
	text = strings.TrimLeft(text, "([{<\"'`")
	return strings.TrimRight(text, ")]}>\"'`,;:!?.")
}

// maxUnderlinedRun bounds how many cells UnderlinedTextAt walks either way.
const maxUnderlinedRun = 1024

// UnderlinedTextAt returns the run of underlined text in one colour around a
// visible cell: how a program marks a link it could not make a real one, as a
// coding agent that thinks the terminal has no hyperlinks still underlines the
// file name it would have linked. Unlike TokenAt the run may hold spaces, and it
// follows soft wraps onto the rows before and after. It is empty when the cell
// is not underlined.
func (t *Terminal) UnderlinedTextAt(position Position) string {
	position = t.clampPosition(position)
	rows := make(map[int][]Cell, 3)
	cells := func(row int) []Cell {
		found, ok := rows[row]
		if !ok {
			found = t.Row(row).Cells
			rows[row] = found
		}
		return found
	}
	start := cells(position.Row)
	if position.Column >= len(start) {
		return ""
	}
	mark := start[position.Column]
	if mark.Attr&WideTail != 0 && position.Column > 0 {
		mark = start[position.Column-1]
	}
	if mark.Attr&Underline == 0 || mark.R == 0 {
		return ""
	}
	// A wide character's tail holds no character of its own and goes with it.
	same := func(at Position) bool {
		row := cells(at.Row)
		if at.Column < 0 || at.Column >= len(row) {
			return false
		}
		cell := row[at.Column]
		return cell.Attr&WideTail != 0 || cell.R != 0 && cell.Attr&Underline != 0 && cell.FG == mark.FG
	}
	// A row that wraps early, before a wide character that did not fit, ends
	// in blank cells; the run goes on after its last character.
	lastCell := func(row int) int {
		line := cells(row)
		for column := min(len(line), t.cols) - 1; column >= 0; column-- {
			if line[column].R != 0 || line[column].Attr&WideTail != 0 {
				return column
			}
		}
		return -1
	}
	wraps := func(row int) bool { return t.Row(row).Wrapped }
	first, last := position, position
	for steps := 0; steps < maxUnderlinedRun; steps++ {
		previous := Position{Row: first.Row, Column: first.Column - 1}
		if previous.Column < 0 {
			if previous.Row == 0 || !wraps(previous.Row-1) {
				break
			}
			previous = Position{Row: previous.Row - 1, Column: lastCell(previous.Row - 1)}
		}
		if !same(previous) {
			break
		}
		first = previous
	}
	for steps := 0; steps < maxUnderlinedRun; steps++ {
		next := Position{Row: last.Row, Column: last.Column + 1}
		if next.Column >= t.cols || next.Column > lastCell(last.Row) && wraps(last.Row) {
			if next.Row+1 >= t.rows || !wraps(last.Row) {
				break
			}
			next = Position{Row: next.Row + 1, Column: 0}
		}
		if !same(next) {
			break
		}
		last = next
	}
	// The text is read from the cells rather than as a selection would copy it,
	// which drops the blanks at the end of a wrapped row: a space where the row
	// wrapped is still part of a name.
	var text strings.Builder
	for row := first.Row; row <= last.Row; row++ {
		from, to := 0, lastCell(row)
		if row == first.Row {
			from = first.Column
		}
		if row == last.Row {
			to = last.Column
		}
		marks := t.Row(row).Marks
		for column, line := from, cells(row); column <= to && column < len(line); column++ {
			if cell := line[column]; cell.Attr&WideTail == 0 && cell.R != 0 {
				text.WriteRune(cell.R)
			}
			for _, mark := range marks {
				if int(mark.Col) == column {
					text.WriteRune(mark.R)
				}
			}
		}
	}
	return strings.TrimSpace(text.String())
}
