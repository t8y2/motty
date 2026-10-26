package workbench

import (
	"math"
	"unicode/utf8"

	"github.com/vivaui/viva/ui"
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
	color    ui.Color
}

// rowEntry is a terminal line prepared for painting: its text, the colored
// runs of it and the background spans. It stays valid while the line's
// identity, version, cursor and the palette stay the same, so an unchanged
// line costs nothing to paint again, and a line that only moved (the screen
// scrolled) is found again by its ID.
type rowEntry struct {
	id      uint64
	version uint32
	cursor  int // the column drawn as the cursor block, or -1
	epoch   uint32
	cols    int
	used    uint32 // paint generation that last used the entry
	text    string
	gutter  gutterEntry
	runs    []ui.TextRun
	spans   []bgSpan
}

// rowCache holds the prepared lines of one terminal.
type rowCache struct {
	entries    map[uint64]*rowEntry
	free       []*rowEntry
	generation uint32
	scratch    []byte
	builds     uint64 // lines prepared so far, to tell reuse from rework

	// Scratch for the highlight rules: the text of a line, the cell each of its
	// bytes came from, and the slot each byte was given.
	hlText  []byte
	hlCell  []int32
	hlMarks []highlight.Slot
	hlCells []highlight.Slot
}

// begin starts a paint pass.
func (cache *rowCache) begin() { cache.generation++ }

// end releases the entries the pass did not use, so the cache holds about one
// screenful however long the terminal has run.
func (cache *rowCache) end(visible int) {
	if len(cache.entries) <= visible*2+8 {
		return
	}
	for id, entry := range cache.entries {
		if entry.used != cache.generation {
			delete(cache.entries, id)
			cache.free = append(cache.free, entry)
		}
	}
}

// row returns the prepared form of a terminal line, building it when the line
// is new or changed. cursor is the column to draw as the cursor block, or -1.
func (cache *rowCache) row(line term.Row, cols, cursor int, pal *palette, inactive bool) *rowEntry {
	epoch := uint32(pal.epoch) << 3
	if pal.dark {
		epoch |= 1
	}
	if inactive {
		epoch |= 2
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
	if entry.version != line.Version || entry.cursor != cursor || entry.epoch != epoch || entry.cols != cols {
		cache.build(entry, line, cols, cursor, pal, inactive)
		entry.version, entry.cursor, entry.epoch, entry.cols = line.Version, cursor, epoch, cols
	}
	return entry
}

// cellStyle is what a cell looks like once its colors and attributes are
// resolved against the palette.
type cellStyle struct {
	fg        ui.Color
	bg        ui.Color
	hasBG     bool
	weight    float32
	italic    bool
	underline bool
	strike    bool
}

func resolveCell(cell term.Cell, pal *palette, inactive bool) cellStyle {
	style := cellStyle{fg: pal.termColor(cell.FG, pal.termFG)}
	if cell.BG != 0 {
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
	if cell.Attr&term.Bold != 0 {
		style.weight = ui.WeightBold
	}
	style.italic = cell.Attr&term.Italic != 0
	style.underline = cell.Attr&term.Underline != 0
	style.strike = cell.Attr&term.Strike != 0
	return style
}

// termColor resolves a terminal color; fallback stands in for the default.
func (pal *palette) termColor(color term.Color, fallback ui.Color) ui.Color {
	switch color.Kind() {
	case term.PaletteColor:
		red, green, blue := term.Resolve(color.Index(), &pal.ansi)
		return ui.RGB(red, green, blue)
	case term.TrueColor:
		return ui.RGB(color.Components())
	}
	return fallback
}

// build turns a terminal line into text, runs and background spans.
//
// Runs hold what is visible. Blank cells need no text: a run that ends where a
// blank begins swallows it, otherwise it is a gap between runs and costs
// nothing. Every character that is not plain ASCII is a run of its own, placed
// at its cell by the renderer, so a wide or fallback-font glyph can never push
// the text after it off its column.
func (cache *rowCache) build(entry *rowEntry, line term.Row, cols, cursor int, pal *palette, inactive bool) {
	cache.builds++
	entry.runs = entry.runs[:0]
	entry.spans = entry.spans[:0]
	text := cache.scratch[:0]
	cells := line.Cells
	count := min(len(cells), cols)

	if cursor >= 0 && cursor < count && cells[cursor].Attr&term.WideTail != 0 && cursor > 0 {
		cursor-- // the cursor sits on the head of a wide character
	}
	cursorStyle := cellStyle{fg: pal.terminal, bg: pal.cursor, hasBG: true}
	cellSlots := cache.markCells(line, count, pal.highlight)

	run := -1 // index into entry.runs of the run that may still grow
	runASCII := false
	marks := line.Marks
	var (
		spanFrom  = -1
		spanTo    int
		spanColor ui.Color
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
		style := resolveCell(cell, pal, inactive)
		if cellSlots != nil && cellSlots[x] != highlight.None && cell.FG.Kind() == term.DefaultColor && cell.Attr&term.Inverse == 0 {
			style.fg = pal.highlightColor(cellSlots[x], inactive)
		}
		if x == cursor {
			style = cursorStyle
		}

		switch {
		case !style.hasBG:
			closeSpan()
		case spanFrom >= 0 && spanColor == style.bg && spanTo == x:
			spanTo = x + width
		default:
			closeSpan()
			spanFrom, spanTo, spanColor = x, x+width, style.bg
		}

		character := cell.R
		if character < 0x20 || (character >= 0x7f && character < 0xa0) {
			character = 0
		}
		blank := character == 0 || character == ' ' || cell.Attr&term.Hidden != 0
		if blank && !style.underline && !style.strike {
			// An invisible cell: swallow it into a run that ends here.
			// A decorated run (underline, strike) draws across its cells, so
			// blanks must not stretch it.
			if run >= 0 && !entry.runs[run].Underline && !entry.runs[run].Strike &&
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
			sameRunInk(entry.runs[run], style) {
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
		entry.runs = append(entry.runs, ui.TextRun{
			Column: int32(x), Bytes: int32(len(text) - before), Cells: int32(width),
			Color: style.fg, Weight: style.weight, Italic: style.italic, Underline: style.underline, Strike: style.strike,
		})
		run = len(entry.runs) - 1
		runASCII = ascii
	}
	closeSpan()
	// The cursor may sit past the last stored cell of a short line.
	if cursor >= count && cursor >= 0 {
		entry.spans = append(entry.spans, bgSpan{from: cursor, to: cursor + 1, color: pal.cursor})
	}

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

// highlightColor is the color a slot has in the terminal's scheme.
func (pal *palette) highlightColor(slot highlight.Slot, inactive bool) ui.Color {
	ansi := pal.ansi[slot.ANSI()]
	color := ui.RGB(ansi[0], ansi[1], ansi[2])
	if inactive {
		color.Alpha *= 0.55
	}
	return color
}

// markCells runs the highlight rules over a line's text and returns the slot of
// each cell, or nil when no rule matched, which is nearly every line of a quiet
// terminal and costs the caller nothing.
func (cache *rowCache) markCells(line term.Row, count int, rules *highlight.Set) []highlight.Slot {
	if rules.Empty() {
		return nil
	}
	text, owner := cache.hlText[:0], cache.hlCell[:0]
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
	}
	cache.hlText, cache.hlCell = text, owner
	marks := slotBuffer(&cache.hlMarks, len(text))
	if !rules.Mark(text, marks) {
		return nil
	}
	cells := slotBuffer(&cache.hlCells, count)
	for index, slot := range marks {
		if slot != highlight.None {
			cells[owner[index]] = slot
		}
	}
	return cells
}

// slotBuffer returns a zeroed slice of n slots, reusing what the cache kept.
func slotBuffer(buffer *[]highlight.Slot, n int) []highlight.Slot {
	if cap(*buffer) < n {
		*buffer = make([]highlight.Slot, n)
	}
	*buffer = (*buffer)[:n]
	clear(*buffer)
	return *buffer
}

func sameRunInk(run ui.TextRun, style cellStyle) bool {
	return run.Color == style.fg && run.Weight == style.weight &&
		run.Italic == style.italic && run.Underline == style.underline && run.Strike == style.strike
}

// snap rounds a length to half a pixel, so cell edges land on physical pixels
// at 2x and neighbouring background spans meet without seams.
func snap(value float32) float32 { return float32(math.Round(float64(value)*2) / 2) }
