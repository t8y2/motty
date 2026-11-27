package workbench

import (
	"math"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/highlight"
	"motty/internal/term"
)

// The terminal's fixed-pitch font size and the margins around its grid.
const (
	termFontSize  = float32(13)
	termPadX      = float32(14)
	termPadTop    = float32(8)
	termPadBottom = float32(8)
)

// bgSpan is a stretch of cells painted with a background color.
type bgSpan struct {
	from, to int // cell columns, to exclusive
	color    gfx.Color
}

// cellSpan is a stretch of cells of one line, to exclusive. The zero value is
// empty.
type cellSpan struct {
	from, to int
}

func (span cellSpan) holds(column int) bool { return column >= span.from && column < span.to }

// rowEntry is a terminal line prepared for painting: its text, the colored
// runs of it and the background spans. It stays valid while the line's
// identity, version, cursor, covered cells and the palette stay the same, so
// an unchanged line costs nothing to paint again, and a line that only moved
// (the screen scrolled) is found again by its ID.
type rowEntry struct {
	id             uint64
	version        uint32
	logicalVersion uint64
	logicalID      uint64
	cursor         int // the column drawn as the cursor block, or -1
	covered        cellSpan
	epoch          uint32
	cols           int
	usedCols       int
	used           uint32 // paint generation that last used the entry
	text           string
	gutter         gutterEntry
	runs           []gfx.TextRun
	spans          []bgSpan
}

// rowCache holds the prepared lines of one terminal.
type rowCache struct {
	entries    map[uint64]*rowEntry
	free       []*rowEntry
	generation uint32
	scratch    []byte
	builds     uint64 // lines prepared so far, to tell reuse from rework

	// Scratch for the highlight rules: the text of a line, the cell each of its
	// bytes came from, and the color each byte was given.
	hlText  []byte
	hlCell  []int32
	hlMarks []highlight.Color
	hlCells []highlight.Color

	// Scratch for logical-line highlight rules across wrapped rows.
	hlLogicalText  []byte
	hlOwnerRow     []int32
	hlOwnerCol     []int32
	hlLogicalMarks []highlight.Color
	logical        map[uint64]*logicalHighlight
	logicalFree    []*logicalHighlight
	logicalSerial  uint64
}

type logicalRowKey struct {
	id      uint64
	version uint32
	used    int
	length  int
	wrapped bool
}

type logicalHighlight struct {
	rows      []logicalRowKey
	positions map[uint64]int
	cells     []highlight.Color
	epoch     uint32
	cols      int
	used      uint32
	serial    uint64
	rules     *highlight.Set
}

// begin starts a paint pass.
func (cache *rowCache) begin() { cache.generation++ }

// end releases the entries the pass did not use, so the cache holds about one
// screenful however long the terminal has run.
func (cache *rowCache) end(visible int) {
	if len(cache.entries) > visible*2+8 {
		for id, entry := range cache.entries {
			if entry.used != cache.generation {
				delete(cache.entries, id)
				cache.free = append(cache.free, entry)
			}
		}
	}
	if len(cache.logical) > visible*2+8 {
		for id, entry := range cache.logical {
			if entry.used != cache.generation {
				delete(cache.logical, id)
				cache.logicalFree = append(cache.logicalFree, entry)
			}
		}
	}
}

// row returns the prepared form of a terminal line, building it when the line
// is new or changed. cursor is the column to draw as the cursor block, or -1.
// covered is the stretch of cells something else is drawn over, an input
// method's composition: they are left out, text and background, because a
// rectangle painted over a cell does not hide the text already in it.
func (cache *rowCache) row(line term.Row, cols, cursor int, covered cellSpan, pal *palette, inactive bool, logical ...term.Row) *rowEntry {
	return cache.rowWithLogical(line, cols, cursor, covered, pal, inactive, logical)
}

// rowWithLogical is row with the soft-wrapped line around it: the rows of
// logical, which holds line, are matched as one text so a match may cross a
// wrap.
func (cache *rowCache) rowWithLogical(line term.Row, cols, cursor int, covered cellSpan, pal *palette, inactive bool, logical []term.Row) *rowEntry {
	epoch := uint32(pal.epoch) << 3
	if pal.dark {
		epoch |= 1
	}
	if inactive {
		epoch |= 2
	}
	if pal.hideApplicationBackground {
		epoch |= 4
	}
	entry := cache.entries[line.ID]
	if entry == nil {
		if cache.entries == nil {
			cache.entries = make(map[uint64]*rowEntry)
		}
		if last := len(cache.free) - 1; last >= 0 {
			entry, cache.free = cache.free[last], cache.free[:last]
		} else {
			entry = &rowEntry{}
		}
		cache.entries[line.ID] = entry
		entry.id = line.ID
		entry.version = ^uint32(0) // never matches: build below
	}
	entry.used = cache.generation
	var group *logicalHighlight
	var logicalID, logicalVersion uint64
	if len(logical) > 1 {
		group = cache.logicalLine(logical, cols, epoch, pal.highlight)
		logicalID, logicalVersion = logical[0].ID, group.serial
	}
	if entry.version != line.Version || entry.logicalID != logicalID || entry.logicalVersion != logicalVersion || entry.cursor != cursor || entry.covered != covered || entry.epoch != epoch {
		cache.build(entry, line, cols, cursor, covered, pal, inactive, group)
		entry.version, entry.logicalID, entry.logicalVersion, entry.cursor, entry.covered, entry.epoch, entry.cols = line.Version, logicalID, logicalVersion, cursor, covered, epoch, cols
	} else if entry.cols != cols {
		rebuild := entry.usedCols > min(entry.cols, cols) || pal.highlight.PaddingSensitive()
		if !rebuild && cols > entry.cols {
			// Row clips history to the viewport. Its hidden background cells
			// survive a resize without changing the line's version, so inspect
			// the newly exposed columns before reusing the narrower rendering.
			for _, cell := range line.Cells[min(entry.cols, len(line.Cells)):min(cols, len(line.Cells))] {
				if cell != (term.Cell{}) {
					rebuild = true
					break
				}
			}
		}
		if rebuild {
			cache.build(entry, line, cols, cursor, covered, pal, inactive, group)
			entry.logicalVersion = logicalVersion
		}
		entry.cols = cols
	}
	return entry
}

// cellStyle is what a cell looks like once its colors and attributes are
// resolved against the palette.
type cellStyle struct {
	fg        gfx.Color
	bg        gfx.Color
	hasBG     bool
	weight    float32
	italic    bool
	underline bool
	strike    bool
}

// cellKey is the part of a cell that resolveCell reads, so two cells with the
// same key resolve to the same style.
type cellKey struct {
	fg   term.Color
	bg   term.Color
	attr term.Attr
}

func resolveCell(cell term.Cell, pal *palette, inactive bool) cellStyle {
	bold := cell.Attr&term.Bold != 0
	fg := cell.FG
	if bold && pal.intense.brightens() {
		fg = brighten(fg)
	}
	style := cellStyle{fg: pal.termColor(fg, pal.termFG)}
	if cell.BG != 0 && !pal.hideApplicationBackground {
		style.bg, style.hasBG = pal.termColor(cell.BG, pal.terminal), true
	}
	if cell.Attr&term.Inverse != 0 {
		inkSource := pal.termColor(cell.BG, pal.terminal) // the background becomes the ink
		style.bg, style.hasBG = pal.termColor(cell.FG, pal.termFG), true
		style.fg = inkSource
	}
	if cell.Attr&term.Dim != 0 {
		style.fg.Alpha *= 0.6
	}
	if inactive {
		style.fg.Alpha *= 0.55
	}
	if bold && pal.intense.embolden() {
		style.weight = gfx.WeightBold
	}
	style.italic = cell.Attr&term.Italic != 0
	style.underline = cell.Attr&term.Underline != 0
	style.strike = cell.Attr&term.Strike != 0
	return style
}

// termColor resolves a terminal color; fallback stands in for the default.
func (pal *palette) termColor(color term.Color, fallback gfx.Color) gfx.Color {
	switch color.Kind() {
	case term.PaletteColor:
		red, green, blue := term.Resolve(color.Index(), &pal.ansi)
		return gfx.RGB(red, green, blue)
	case term.TrueColor:
		return gfx.RGB(color.Components())
	}
	return fallback
}

// build turns a terminal line into text, runs and background spans.
//
// Runs hold what is visible. Blank cells need no text: an ASCII run that ends
// where a blank begins swallows it; otherwise it is a gap between runs and
// costs nothing. Every non-ASCII character is a run of its own, placed at its
// cell by the renderer, so a wide or fallback-font glyph can never push the
// text after it off its column.
func (cache *rowCache) build(entry *rowEntry, line term.Row, cols, cursor int, covered cellSpan, pal *palette, inactive bool, group *logicalHighlight) {
	cache.builds++
	entry.runs = entry.runs[:0]
	entry.spans = entry.spans[:0]
	text := cache.scratch[:0]
	cells := line.Cells
	count := min(len(cells), cols)

	if cursor >= 0 && cursor < count && cells[cursor].Attr&term.WideTail != 0 && cursor > 0 {
		cursor-- // the cursor sits on the head of a wide character
	}
	if covered.from > 0 && covered.from < count && cells[covered.from].Attr&term.WideTail != 0 {
		covered.from-- // the cover starts on the tail of a wide character: take its head too
	}
	cursorStyle := cellStyle{fg: pal.terminal, bg: pal.cursor, hasBG: true}
	var cellSlots []highlight.Color
	if group != nil {
		if index, ok := group.positions[line.ID]; ok && len(group.cells) >= (index+1)*cols {
			cellSlots = group.cells[index*cols : index*cols+count]
		}
	} else {
		cellSlots = cache.markCells(line, count, pal.highlight)
	}

	run := -1 // index into entry.runs of the run that may still grow
	runASCII := false
	usedCols := 0
	marks := line.Marks
	// Resolving a cell is the expensive half of a row: it maps the cell's own
	// colors through the palette. A row is long stretches of one style, so the
	// style is resolved where a stretch starts, not for every cell of it. The
	// key is the part of a cell resolveCell reads, so the comparison stays one
	// comparison however the cell changes.
	var (
		key    cellKey
		style  cellStyle
		styled bool
	)
	var (
		spanFrom  = -1
		spanTo    int
		spanColor gfx.Color
	)
	closeSpan := func() {
		if spanFrom >= 0 {
			entry.spans = append(entry.spans, bgSpan{from: spanFrom, to: spanTo, color: spanColor})
			spanFrom = -1
		}
	}

	for x := 0; x < count; x++ {
		cell := cells[x]
		if cell.Attr&term.WideTail != 0 {
			continue
		}
		width := 1
		if cell.Attr&term.WideHead != 0 {
			width = 2
		}
		if cell != (term.Cell{}) {
			usedCols = x + width
		}
		if covered.holds(x) {
			closeSpan()
			continue
		}
		if k := (cellKey{fg: cell.FG, bg: cell.BG, attr: cell.Attr}); !styled || k != key {
			key, style, styled = k, resolveCell(cell, pal, inactive), true
		}
		ink := style
		if cellSlots != nil && cellSlots[x] != highlight.None && cell.FG.Kind() == term.DefaultColor && cell.Attr&term.Inverse == 0 {
			ink.fg = pal.highlightColor(cellSlots[x], inactive)
		}
		if x == cursor {
			ink = cursorStyle
		}

		switch {
		case !ink.hasBG:
			closeSpan()
		case spanFrom >= 0 && spanColor == ink.bg && spanTo == x:
			spanTo = x + width
		default:
			closeSpan()
			spanFrom, spanTo, spanColor = x, x+width, ink.bg
		}

		character := cell.R
		if character < 0x20 || (character >= 0x7f && character < 0xa0) {
			character = 0
		}
		blank := character == 0 || character == ' ' || cell.Attr&term.Hidden != 0
		if blank && !ink.underline && !ink.strike {
			// An invisible cell: swallow it into an ASCII run that ends here.
			// A non-ASCII character must stay on its own so the renderer keeps
			// centering it within its cells, even when blanks follow it.
			// A decorated run (underline, strike) draws across its cells, so
			// blanks must not stretch it.
			if run >= 0 && runASCII && !entry.runs[run].Underline && !entry.runs[run].Strike &&
				int(entry.runs[run].Column+entry.runs[run].Cells) == x {
				entry.runs[run].Bytes++
				entry.runs[run].Cells += int32(width)
				text = append(text, ' ')
			}
			continue
		}
		if blank {
			character = ' '
		}

		for len(marks) > 0 && int(marks[0].Col) < x {
			marks = marks[1:]
		}
		// A character with combining marks is shaped on its own, like any
		// non-ASCII one.
		ascii := character < 0x7f && !(len(marks) > 0 && int(marks[0].Col) == x)
		if run >= 0 && ascii && runASCII && int(entry.runs[run].Column+entry.runs[run].Cells) == x &&
			sameRunInk(entry.runs[run], ink) {
			entry.runs[run].Bytes++
			entry.runs[run].Cells++
			text = append(text, byte(character))
			continue
		}
		before := len(text)
		text = utf8.AppendRune(text, character)
		for len(marks) > 0 && int(marks[0].Col) == x {
			text = utf8.AppendRune(text, marks[0].R)
			marks = marks[1:]
		}
		entry.runs = append(entry.runs, gfx.TextRun{
			Column: int32(x), Bytes: int32(len(text) - before), Cells: int32(width),
			Color: ink.fg, Weight: ink.weight, Italic: ink.italic, Underline: ink.underline, Strike: ink.strike,
		})
		run = len(entry.runs) - 1
		runASCII = ascii
	}
	closeSpan()
	for x := count; x < len(cells); x++ {
		if cells[x] != (term.Cell{}) {
			usedCols = x + 1
		}
	}
	// The cursor may sit past the last stored cell of a short line.
	if cursor >= count && cursor >= 0 {
		entry.spans = append(entry.spans, bgSpan{from: cursor, to: cursor + 1, color: pal.cursor})
	}
	if cursor >= 0 && cursor+1 > usedCols {
		usedCols = cursor + 1
	}
	if covered.to > usedCols {
		usedCols = covered.to
	}
	entry.usedCols = usedCols

	// Blanks at the end of the last run are not worth drawing.
	for run = len(entry.runs) - 1; run >= 0; run = len(entry.runs) - 1 {
		last := &entry.runs[run]
		if len(text) == 0 || text[len(text)-1] != ' ' || last.Underline || last.Strike {
			break
		}
		text = text[:len(text)-1]
		last.Bytes--
		last.Cells--
		if last.Bytes == 0 {
			entry.runs = entry.runs[:run]
		}
	}
	cache.scratch = text
	entry.text = string(text)
	// A run of plain ASCII with room for a substitution is shaped as one line
	// by Viva, so a programming font's ligatures appear. Single cells and
	// non-ASCII runs keep the one-glyph-per-cell path.
	offset := 0
	for index := range entry.runs {
		run := &entry.runs[index]
		end := offset + int(run.Bytes)
		run.Ligatures = run.Cells >= 2 && asciiOnly(entry.text[offset:end])
		offset = end
	}
}

// asciiOnly reports whether value is printable ASCII, the only text a
// fixed-pitch ligature run may hold.
func asciiOnly(value string) bool {
	for index := 0; index < len(value); index++ {
		if value[index] < 0x20 || value[index] >= 0x7f {
			return false
		}
	}
	return true
}

// highlightColor is the color a rule gives its text: a named color's in the
// terminal's scheme, or the explicit one as the user chose it.
func (pal *palette) highlightColor(ink highlight.Color, inactive bool) gfx.Color {
	color := gfx.Color{}
	if ink.IsCustom() {
		red, green, blue := ink.Channels()
		color = gfx.RGB(red, green, blue)
	} else {
		ansi := pal.ansi[ink.ANSI()]
		color = gfx.RGB(ansi[0], ansi[1], ansi[2])
	}
	if inactive {
		color.Alpha *= 0.55
	}
	return color
}

// markCells runs the highlight rules over a line's text and returns the color of
// each cell, or nil when no rule matched, which is nearly every line of a quiet
// terminal and costs the caller nothing.
func (cache *rowCache) markCells(line term.Row, count int, rules *highlight.Set) []highlight.Color {
	if rules.Empty() {
		return nil
	}
	text, owner := cache.hlText[:0], cache.hlCell[:0]
	markIndex := 0
	for x := 0; x < count; x++ {
		cell := line.Cells[x]
		if cell.Attr&term.WideTail != 0 {
			continue
		}
		character := cell.R
		if character < 0x20 || (character >= 0x7f && character < 0xa0) || cell.Attr&term.Hidden != 0 {
			character = ' '
		}
		before := len(text)
		text = utf8.AppendRune(text, character)
		for range len(text) - before {
			owner = append(owner, int32(x))
		}
		for markIndex < len(line.Marks) && int(line.Marks[markIndex].Col) < x {
			markIndex++
		}
		for markIndex < len(line.Marks) && int(line.Marks[markIndex].Col) == x {
			if cell.Attr&term.Hidden == 0 {
				before = len(text)
				text = utf8.AppendRune(text, line.Marks[markIndex].R)
				for range len(text) - before {
					owner = append(owner, int32(x))
				}
			}
			markIndex++
		}
	}
	cache.hlText, cache.hlCell = text, owner
	marks := markBuffer(&cache.hlMarks, len(text))
	if !rules.Mark(text, marks) {
		return nil
	}
	cells := markBuffer(&cache.hlCells, count)
	for index, color := range marks {
		if color != highlight.None {
			cells[owner[index]] = color
		}
	}
	return cells
}

func (cache *rowCache) logicalLine(logical []term.Row, cols int, epoch uint32, rules *highlight.Set) *logicalHighlight {
	if cache.logical == nil {
		cache.logical = make(map[uint64]*logicalHighlight)
	}
	id := logical[0].ID
	group := cache.logical[id]
	if group == nil {
		if last := len(cache.logicalFree) - 1; last >= 0 {
			group, cache.logicalFree = cache.logicalFree[last], cache.logicalFree[:last]
			group.serial = 0
		} else {
			group = &logicalHighlight{}
		}
		cache.logical[id] = group
	}
	if group.serial != 0 && group.used == cache.generation {
		return group
	}
	group.used = cache.generation
	changed := group.serial == 0 || group.cols != cols || group.epoch != epoch || group.rules != rules || len(group.rows) != len(logical)
	if !changed {
		for index, row := range logical {
			key := logicalRowKey{id: row.ID, version: row.Version, used: row.Used, length: len(row.Cells), wrapped: row.Wrapped}
			if group.rows[index] != key {
				changed = true
				break
			}
		}
	}
	if !changed {
		return group
	}
	cache.logicalSerial++
	group.serial, group.cols, group.epoch, group.rules = cache.logicalSerial, cols, epoch, rules
	group.rows = group.rows[:0]
	if group.positions == nil {
		group.positions = make(map[uint64]int)
	} else {
		clear(group.positions)
	}
	for index, row := range logical {
		group.rows = append(group.rows, logicalRowKey{id: row.ID, version: row.Version, used: row.Used, length: len(row.Cells), wrapped: row.Wrapped})
		group.positions[row.ID] = index
	}
	cache.markLogicalCells(group, logical, cols, rules)
	return group
}

func (cache *rowCache) markLogicalCells(group *logicalHighlight, logical []term.Row, cols int, rules *highlight.Set) {
	group.cells = group.cells[:0]
	if rules.Empty() {
		return
	}
	text := cache.hlLogicalText[:0]
	ownerRow := cache.hlOwnerRow[:0]
	ownerCol := cache.hlOwnerCol[:0]

	for rowIndex, row := range logical {
		rowCells := row.Cells
		rowCount := min(len(rowCells), cols)
		if rowIndex < len(logical)-1 {
			rowCount = min(rowCount, row.Used)
			marked := 0
			if len(row.Marks) > 0 {
				marked = int(row.Marks[len(row.Marks)-1].Col) + 1
			}
			for rowCount > marked && rowCells[rowCount-1].R == 0 && rowCells[rowCount-1].Attr&term.WideTail == 0 {
				rowCount--
			}
		}
		markIndex := 0
		for x := 0; x < rowCount; x++ {
			cell := rowCells[x]
			if cell.Attr&term.WideTail != 0 {
				continue
			}
			character := cell.R
			if character < 0x20 || (character >= 0x7f && character < 0xa0) || cell.Attr&term.Hidden != 0 {
				character = ' '
			}
			before := len(text)
			text = utf8.AppendRune(text, character)
			for range len(text) - before {
				ownerRow = append(ownerRow, int32(rowIndex))
				ownerCol = append(ownerCol, int32(x))
			}
			for markIndex < len(row.Marks) && int(row.Marks[markIndex].Col) < x {
				markIndex++
			}
			for markIndex < len(row.Marks) && int(row.Marks[markIndex].Col) == x {
				if cell.Attr&term.Hidden == 0 {
					before = len(text)
					text = utf8.AppendRune(text, row.Marks[markIndex].R)
					for range len(text) - before {
						ownerRow = append(ownerRow, int32(rowIndex))
						ownerCol = append(ownerCol, int32(x))
					}
				}
				markIndex++
			}
		}
	}

	cache.hlLogicalText = text
	cache.hlOwnerRow = ownerRow
	cache.hlOwnerCol = ownerCol

	marks := markBuffer(&cache.hlLogicalMarks, len(text))
	if !rules.Mark(text, marks) {
		return
	}

	cells := markBuffer(&group.cells, len(logical)*cols)
	for index, color := range marks {
		if color != highlight.None {
			cells[int(ownerRow[index])*cols+int(ownerCol[index])] = color
		}
	}
}

// markBuffer returns a zeroed slice of n marks, reusing what the cache kept.
func markBuffer(buffer *[]highlight.Color, n int) []highlight.Color {
	if cap(*buffer) < n {
		*buffer = make([]highlight.Color, n)
	}
	*buffer = (*buffer)[:n]
	clear(*buffer)
	return *buffer
}

func sameRunInk(run gfx.TextRun, style cellStyle) bool {
	return run.Color == style.fg && run.Weight == style.weight &&
		run.Italic == style.italic && run.Underline == style.underline && run.Strike == style.strike
}

// snap rounds a length to half a pixel, so cell edges land on physical pixels
// at 2x and neighbouring background spans meet without seams.
func snap(value float32) float32 { return float32(math.Round(float64(value)*2) / 2) }
