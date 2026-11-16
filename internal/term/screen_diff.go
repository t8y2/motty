package term

import (
	"sort"
	"strconv"
	"unicode/utf8"
)

// ScreenDiff returns the bytes that turn a terminal showing from into one
// showing to, as Mosh's display diff does: only changed cells are written, a
// screen that moved up is advanced with newlines so the lines that left the top
// reach the terminal's scrollback, and the cursor, its shape, the title and the
// modes end in the state to describes.
//
// from may be nil for the first frame, which repaints the whole screen and
// clears whatever the owner drew before (connection progress, for example).
// Sizes that differ also repaint in full, because the row diff no longer
// describes the same cells. Bells and clipboard writes are not emulator state
// and are not part of the result.
func ScreenDiff(from, to *Terminal) []byte {
	if to == nil {
		return nil
	}
	cols, rows := to.cols, to.rows
	var out []byte
	full := from == nil || from.cols != cols || from.rows != rows || from.onAlt != to.onAlt
	var previous []diffRow
	frameRow := 0
	pen := diffPen{}
	if full {
		// Reset the scrolling region and the pen so the clear paints default
		// blank cells, then start from the top-left corner.
		out = append(out, "\x1b[r\x1b[0m\x1b[H\x1b[2J"...)
	} else {
		// The owner's pen is whatever the last frame left, and a local write may
		// have changed it. The first run sets its own, so nothing depends on it;
		// a state that changes no cells is delivered as no bytes at all.
		previous = make([]diffRow, rows)
		for y := range previous {
			previous[y] = liveRow(from, y)
		}
		scrolled, height := screenScroll(to, previous, cols, rows)
		if scrolled > 0 {
			out = screenScrollOutput(out, scrolled, height, rows, from.cy)
			// The rows the terminal now shows moved up; compare against them.
			bottom := scrolled + height - 1
			for i := 0; i <= bottom; i++ {
				if i+scrolled <= bottom {
					previous[i] = previous[i+scrolled]
				} else {
					previous[i] = diffRow{}
				}
			}
		}
		frameRow = height
	}
	if from == nil {
		if to.title != "" {
			out = appendOSC(out, "0", ";"+sanitizeTitle(to.title))
		}
	} else if from.title != to.title {
		out = appendOSC(out, "0", ";"+sanitizeTitle(to.title))
	}
	wrote := full
	for y := frameRow; y < rows; y++ {
		var old diffRow
		if !full {
			old = previous[y]
		}
		var changed bool
		out, changed = appendChangedCells(out, y, liveRow(to, y), old, cols, full, &pen)
		wrote = wrote || changed
	}
	if !full && frameRow > 0 {
		wrote = true // the scroll moved the cursor on the real terminal
	}
	out = appendCursorState(out, from, to, wrote)
	return out
}

// diffRow is one visible row of a screen while a diff is built.
type diffRow struct {
	cells []Cell
	marks []Mark
}

// liveRow reads screen row y without the scrollback view offset: a diff moves
// the live screen, not the view.
func liveRow(t *Terminal, y int) diffRow {
	if t.scr == nil || y < 0 || y >= len(t.scr.lines) {
		return diffRow{}
	}
	row := t.rowOf(t.scr.lines[y])
	return diffRow{cells: row.Cells, marks: row.Marks}
}

func diffCell(row diffRow, x int) Cell {
	if x < 0 || x >= len(row.cells) {
		return Cell{}
	}
	return row.cells[x]
}

func marksAt(row diffRow, x int) []Mark {
	i := sort.Search(len(row.marks), func(i int) bool { return int(row.marks[i].Col) >= x })
	j := i
	for j < len(row.marks) && int(row.marks[j].Col) == x {
		j++
	}
	return row.marks[i:j]
}

// unitWidth is the number of cells the character starting at x occupies. A
// wide character covers its head and the tail cell after it.
func unitWidth(row diffRow, x, cols int) int {
	if diffCell(row, x).Attr&WideHead != 0 && x+1 < cols {
		return 2
	}
	return 1
}

// unitBlank reports whether the character at x is a default blank, which a full
// repaint may leave to the screen clear.
func unitBlank(row diffRow, x, width int) bool {
	if diffCell(row, x) != (Cell{}) || len(marksAt(row, x)) != 0 {
		return false
	}
	return width == 1 || diffCell(row, x+1) == (Cell{})
}

// rowsEqual reports whether two visible rows hold the same cells and the same
// combining marks across the width.
func rowsEqual(a, b diffRow, cols int) bool {
	if len(a.marks) != len(b.marks) {
		return false
	}
	for i := range a.marks {
		if a.marks[i] != b.marks[i] {
			return false
		}
	}
	for x := 0; x < cols; x++ {
		if diffCell(a, x) != diffCell(b, x) {
			return false
		}
	}
	return true
}

// unitEqual reports whether the character at x is unchanged.
func unitEqual(to, from diffRow, x, width int) bool {
	if diffCell(to, x) != diffCell(from, x) || !equalMarks(marksAt(to, x), marksAt(from, x)) {
		return false
	}
	return width == 1 || diffCell(to, x+1) == diffCell(from, x+1)
}

func equalMarks(a, b []Mark) bool {
	if len(a) != len(b) {
		return false
	}
	for i := range a {
		if a[i] != b[i] {
			return false
		}
	}
	return true
}

// screenScroll finds a screen that moved up by whole rows: the new top row also
// appears at row k of the old screen, and the rows after it keep matching for a
// region of height rows. scrolled is how many rows left the top, height is how
// tall the matching region is, and the caller starts the row diff at height.
func screenScroll(to *Terminal, previous []diffRow, cols, rows int) (scrolled, height int) {
	top := liveRow(to, 0)
	for row := 0; row < rows; row++ {
		if !rowsEqual(top, previous[row], cols) {
			continue
		}
		if row == 0 {
			return 0, 0
		}
		scrolled, height = row, 1
		for region := 1; scrolled+region < rows; region++ {
			if rowsEqual(liveRow(to, region), previous[scrolled+region], cols) {
				height = region + 1
			} else {
				break
			}
		}
		return scrolled, height
	}
	return 0, 0
}

// screenScrollOutput advances the terminal by scrolled rows through a region at
// least that tall, so the rows leaving the top enter scrollback.
func screenScrollOutput(out []byte, scrolled, height, rows, cursorY int) []byte {
	// Blank scrolling lines take the default background, not whatever the last
	// cell wrote.
	out = append(out, "\x1b[0m"...)
	bottom := scrolled + height - 1
	if scrolled+height == rows && cursorY == rows-1 {
		// The whole screen and the cursor on its last row: a carriage return
		// and one line feed per row is what a pager or a scrolling shell does.
		out = append(out, '\r')
		for i := 0; i < scrolled; i++ {
			out = append(out, '\n')
		}
		return out
	}
	out = append(out, "\x1b[1;"...)
	out = strconv.AppendInt(out, int64(bottom+1), 10)
	out = append(out, 'r')
	out = append(out, "\x1b["...)
	out = strconv.AppendInt(out, int64(bottom+1), 10)
	out = append(out, ";1H"...)
	for i := 0; i < scrolled; i++ {
		out = append(out, '\n')
	}
	out = append(out, "\x1b[r"...)
	return out
}

// appendChangedCells writes the parts of target row y that differ from old.
// full says the screen was just cleared: default blank cells need no write and
// split the runs, while in a partial update a changed blank must be erased.
func appendChangedCells(out []byte, y int, target, old diffRow, cols int, full bool, pen *diffPen) ([]byte, bool) {
	wrote := false
	changed := func(x, width int) bool {
		if full {
			return !unitBlank(target, x, width)
		}
		return !unitEqual(target, old, x, width)
	}
	for x := 0; x < cols; {
		width := unitWidth(target, x, cols)
		if !changed(x, width) {
			x += width
			continue
		}
		end := x + width
		for end < cols {
			w := unitWidth(target, end, cols)
			if !changed(end, w) {
				break
			}
			end += w
		}
		wrote = true
		for pos := x; pos < end; {
			if isBlankUnit(target, pos) {
				// Erase instead of writing spaces: the terminal's own blank
				// cell is what the target holds, and the clear keeps a run of
				// them cheap.
				start, count := pos, 0
				for pos < end && isBlankUnit(target, pos) {
					pos++
					count++
				}
				if !full {
					out = appendMove(out, y+1, start+1)
					if !pen.isDefault() {
						out = append(out, "\x1b[0m"...)
						*pen = diffPen{set: true}
					}
					out = append(out, "\x1b["...)
					out = strconv.AppendInt(out, int64(count), 10)
					out = append(out, 'X')
				}
				continue
			}
			out = appendMove(out, y+1, pos+1)
			for pos < end && !isBlankUnit(target, pos) {
				c := diffCell(target, pos)
				w := unitWidth(target, pos, cols)
				if c.Attr&WideTail != 0 {
					pos++ // the head before it already wrote this cell
					continue
				}
				out = appendCellSGR(out, pen, c)
				r := c.R
				if r < 0x20 || r == 0x7f {
					r = ' '
				}
				out = utf8.AppendRune(out, r)
				for _, mark := range marksAt(target, pos) {
					out = utf8.AppendRune(out, mark.R)
				}
				pos += w
			}
		}
		x = end
	}
	return out, wrote
}

// diffPen is the rendition the terminal's pen currently holds, so that a run of
// equally styled cells is written as one run instead of one SGR per cell.
type diffPen struct {
	fg, bg Color
	attr   Attr
	set    bool
}

func isBlankUnit(row diffRow, x int) bool {
	return diffCell(row, x) == (Cell{}) && len(marksAt(row, x)) == 0
}

func (pen *diffPen) isDefault() bool {
	return pen.set && pen.fg == 0 && pen.bg == 0 && pen.attr == 0
}

func appendCellSGR(out []byte, pen *diffPen, cell Cell) []byte {
	attr := cell.Attr &^ (WideHead | WideTail)
	if pen.set && pen.fg == cell.FG && pen.bg == cell.BG && pen.attr == attr {
		return out
	}
	*pen = diffPen{fg: cell.FG, bg: cell.BG, attr: attr, set: true}
	return appendSGR(out, cell)
}

// appendSGR selects the rendition of one cell: a reset followed by the
// attributes and colors that are not the default.
func appendSGR(out []byte, cell Cell) []byte {
	out = append(out, "\x1b[0"...)
	attr := cell.Attr &^ (WideHead | WideTail)
	for _, bit := range [...]struct {
		flag Attr
		code byte
	}{
		{Bold, '1'}, {Dim, '2'}, {Italic, '3'}, {Underline, '4'},
		{Blink, '5'}, {Inverse, '7'}, {Hidden, '8'}, {Strike, '9'},
	} {
		if attr&bit.flag != 0 {
			out = append(out, ';', bit.code)
		}
	}
	out = appendColor(out, cell.FG, false)
	out = appendColor(out, cell.BG, true)
	return append(out, 'm')
}

func appendColor(out []byte, color Color, background bool) []byte {
	base := 38
	if background {
		base = 48
	}
	switch color.Kind() {
	case PaletteColor:
		out = append(out, ';')
		out = strconv.AppendInt(out, int64(base), 10)
		out = append(out, ";5;"...)
		out = strconv.AppendInt(out, int64(color.Index()), 10)
	case TrueColor:
		red, green, blue := color.Components()
		out = append(out, ';')
		out = strconv.AppendInt(out, int64(base), 10)
		out = append(out, ";2;"...)
		out = strconv.AppendInt(out, int64(red), 10)
		out = append(out, ';')
		out = strconv.AppendInt(out, int64(green), 10)
		out = append(out, ';')
		out = strconv.AppendInt(out, int64(blue), 10)
	}
	return out
}

func appendMove(out []byte, row, col int) []byte {
	out = append(out, "\x1b["...)
	out = strconv.AppendInt(out, int64(row), 10)
	out = append(out, ';')
	out = strconv.AppendInt(out, int64(col), 10)
	return append(out, 'H')
}

// appendCursorState writes the state that follows the grid: cursor position and
// shape, visibility and the modes the mirror tracks.
func appendCursorState(out []byte, from, to *Terminal, moved bool) []byte {
	if from == nil || moved || from.cx != to.cx || from.cy != to.cy {
		out = appendMove(out, to.cy+1, to.cx+1)
	}
	before := Modes{CursorVisible: true, Autowrap: true, AlternateScroll: true}
	beforeCursor, beforeStyled, beforeSteady := CursorBlock, false, false
	if from != nil {
		before = from.modes
		beforeCursor, beforeStyled, beforeSteady = from.cursor, from.cursorStyled, from.cursorSteady
	}
	out = appendBoolMode(out, to.modes.CursorVisible != before.CursorVisible, to.modes.CursorVisible, "\x1b[?25")
	out = appendBoolMode(out, to.modes.AppCursorKeys != before.AppCursorKeys, to.modes.AppCursorKeys, "\x1b[?1")
	out = appendBoolMode(out, to.modes.BracketedPaste != before.BracketedPaste, to.modes.BracketedPaste, "\x1b[?2004")
	out = appendBoolMode(out, to.modes.ReverseVideo != before.ReverseVideo, to.modes.ReverseVideo, "\x1b[?5")
	out = appendBoolMode(out, to.modes.FocusReporting != before.FocusReporting, to.modes.FocusReporting, "\x1b[?1004")
	out = appendBoolMode(out, to.modes.Win32Input != before.Win32Input, to.modes.Win32Input, "\x1b[?9001")
	out = appendMouseMode(out, before.Mouse, to.modes.Mouse)
	out = appendMouseEncoding(out, before.MouseEncoding, to.modes.MouseEncoding)
	switch {
	case to.cursorStyled && (!beforeStyled || beforeCursor != to.cursor || beforeSteady != to.cursorSteady):
		out = appendCursorStyle(out, to.cursor, to.cursorSteady)
	case !to.cursorStyled && beforeStyled:
		// The program gave the cursor back; zero names the terminal's own.
		out = append(out, "\x1b[0 q"...)
	}
	return out
}

func appendBoolMode(out []byte, changed, state bool, prefix string) []byte {
	if !changed {
		return out
	}
	out = append(out, prefix...)
	if state {
		return append(out, 'h')
	}
	return append(out, 'l')
}

func appendMouseMode(out []byte, from, to MouseMode) []byte {
	if from == to {
		return out
	}
	// Clearing every reporting mode first keeps a change from leaving the old
	// level armed alongside the new one, as the C client does.
	if from != MouseOff {
		out = append(out, "\x1b[?1003l\x1b[?1002l\x1b[?1000l\x1b[?9l"...)
	}
	switch to {
	case MouseX10:
		out = append(out, "\x1b[?9h"...)
	case MouseButton:
		out = append(out, "\x1b[?1000h"...)
	case MouseDrag:
		out = append(out, "\x1b[?1002h"...)
	case MouseAny:
		out = append(out, "\x1b[?1003h"...)
	}
	return out
}

func appendMouseEncoding(out []byte, from, to MouseEncoding) []byte {
	if from == to {
		return out
	}
	if from != MouseEncodingX10 {
		out = append(out, "\x1b[?1015l\x1b[?1006l\x1b[?1005l"...)
	}
	switch to {
	case MouseEncodingUTF8:
		out = append(out, "\x1b[?1005h"...)
	case MouseEncodingSGR:
		out = append(out, "\x1b[?1006h"...)
	case MouseEncodingURXVT:
		out = append(out, "\x1b[?1015h"...)
	}
	return out
}

func appendCursorStyle(out []byte, style CursorStyle, steady bool) []byte {
	code := 1
	switch style {
	case CursorUnderline:
		code = 3
	case CursorBar:
		code = 5
	}
	if steady {
		code++
	}
	out = append(out, "\x1b["...)
	out = strconv.AppendInt(out, int64(code), 10)
	return append(out, " q"...)
}

// appendOSC writes an operating system command with a BEL terminator.
func appendOSC(out []byte, command, payload string) []byte {
	out = append(out, "\x1b]"...)
	out = append(out, command...)
	out = append(out, payload...)
	return append(out, '\a')
}

// sanitizeTitle drops the bytes that would end or split the OSC string it is
// written into. The title came from the remote program and is not trusted to be
// free of control characters.
func sanitizeTitle(title string) string {
	clean := make([]byte, 0, len(title))
	for i := 0; i < len(title); i++ {
		if b := title[i]; b >= 0x20 && b != 0x7f {
			clean = append(clean, b)
		}
	}
	return string(clean)
}
