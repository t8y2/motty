package term

// printASCII writes a run of printable ASCII (0x20-0x7E) at the cursor. It is
// the hot path for ordinary output and does the same as print without the
// per-character width and charset work.
func (t *Terminal) printASCII(run []byte) {
	l := t.scr.lines[t.cy]
	t.stamp(l)
	if t.fillASCII(l, run) {
		return
	}
	for _, b := range run {
		if t.pend {
			t.wrap()
			l = t.scr.lines[t.cy]
			t.stamp(l)
		}
		x := t.cx
		if t.modes.Insert {
			t.insertCells(l, x, 1)
		}
		c := &l.span(x, 1)[0]
		if c.Attr&(WideHead|WideTail) != 0 {
			t.clearWideHalf(l, x)
		}
		if len(l.marks) != 0 {
			l.dropMarks(x, x+1)
		}
		*c = Cell{R: rune(b), FG: t.pen.fg, BG: t.pen.bg, Attr: t.pen.attr}
		l.setHyperlink(x, t.pen.hyperlink)
		l.version++
		if x+1 >= t.cols {
			t.pend = t.modes.Autowrap
		} else {
			t.cx = x + 1
		}
	}
	t.lastRune = rune(run[len(run)-1])
}

// fillASCII writes a whole run of printable ASCII in one pass when nothing in
// its way needs per-cell handling: no pending wrap, insert mode, marks,
// hyperlink or double-width halves. It returns false when the caller must take
// the general path.
func (t *Terminal) fillASCII(l *line, run []byte) bool {
	if len(run) == 0 || t.pend || t.modes.Insert || t.pen.hyperlink != 0 || len(l.marks) != 0 || l.hyperlinks != nil {
		return false
	}
	end := t.cx + len(run)
	if end > t.cols {
		return false
	}
	for _, cell := range l.cells[t.cx:end] {
		if cell.Attr&(WideHead|WideTail) != 0 {
			return false
		}
	}
	cell := Cell{FG: t.pen.fg, BG: t.pen.bg, Attr: t.pen.attr}
	cells := l.span(t.cx, len(run))
	for index, value := range run {
		cell.R = rune(value)
		cells[index] = cell
	}
	l.version++
	t.cx = end
	if t.cx >= t.cols {
		t.cx = t.cols - 1
		t.pend = t.modes.Autowrap
	}
	t.lastRune = rune(run[len(run)-1])
	return true
}

// print writes one character at the cursor, handling width, wrapping,
// combining marks and the active character set.
func (t *Terminal) print(r rune) {
	if t.charsets[t.gl] == charsetGraphics {
		r = graphicsRune(r)
	}
	w := Width(r)
	if w == 0 {
		t.combine(r)
		return
	}
	if w == 2 && t.cols < 2 {
		w = 1
	}
	if t.pend {
		t.wrap()
	}
	l := t.scr.lines[t.cy]
	if w == 2 && t.cx == t.cols-1 {
		if !t.modes.Autowrap {
			return
		}
		// No room for both halves: leave the last cell empty and wrap.
		t.eraseRange(l, t.cx, t.cx+1)
		t.wrap()
		l = t.scr.lines[t.cy]
	}
	t.stamp(l)
	x := t.cx
	if t.modes.Insert {
		t.insertCells(l, x, w)
	}
	cells := l.span(x, w)
	for i := range cells {
		if cells[i].Attr&(WideHead|WideTail) != 0 {
			t.clearWideHalf(l, x+i)
		}
	}
	l.dropMarks(x, x+w)
	attr := t.pen.attr
	if w == 2 {
		cells[0] = Cell{R: r, FG: t.pen.fg, BG: t.pen.bg, Attr: attr | WideHead}
		cells[1] = Cell{FG: t.pen.fg, BG: t.pen.bg, Attr: attr | WideTail}
	} else {
		cells[0] = Cell{R: r, FG: t.pen.fg, BG: t.pen.bg, Attr: attr}
	}
	l.setHyperlinkRange(x, x+w, t.pen.hyperlink)
	l.version++
	if x+w >= t.cols {
		t.cx = t.cols - 1
		t.pend = t.modes.Autowrap
	} else {
		t.cx = x + w
	}
	t.lastRune = r
}

// wrap moves to the start of the next line, marking the line it leaves as
// continued.
func (t *Terminal) wrap() {
	t.scr.lines[t.cy].wrapped = true
	t.cx, t.pend = 0, false
	t.index()
}

// clearWideHalf blanks the other half of the double-width character that has a
// half at x, which is about to be overwritten.
func (t *Terminal) clearWideHalf(l *line, x int) {
	switch c := l.cells[x]; {
	case c.Attr&WideTail != 0 && x > 0:
		l.orphan(x - 1)
	case c.Attr&WideHead != 0 && x+1 < len(l.cells):
		l.orphan(x + 1)
	}
}

// combine attaches a zero-width character to the character before the cursor.
func (t *Terminal) combine(r rune) {
	x := t.cx
	if !t.pend {
		x--
	}
	if x < 0 {
		return
	}
	l := t.scr.lines[t.cy]
	if l.cells[x].Attr&WideTail != 0 && x > 0 {
		x--
	}
	if l.cells[x].R == 0 || len(l.marks) >= maxMarks {
		return
	}
	// Keep marks ordered by column, in arrival order within a column.
	at := len(l.marks)
	for at > 0 && int(l.marks[at-1].Col) > x {
		at--
	}
	l.marks = append(l.marks, Mark{})
	copy(l.marks[at+1:], l.marks[at:])
	l.marks[at] = Mark{Col: uint16(x), R: r}
	l.version++
}

// graphicsRune maps the DEC special graphics set onto Unicode.
func graphicsRune(r rune) rune {
	if r < 0x5F || r > 0x7E {
		return r
	}
	return decGraphics[r-0x5F]
}

// decGraphics covers 0x5F-0x7E of the DEC special character and line drawing
// set.
var decGraphics = [...]rune{
	0x00A0, '◆', '▒', '␉', '␌', '␍', '␊', '°', '±', '␤', '␋', '┘', '┐', '┌', '└', '┼',
	'⎺', '⎻', '─', '⎼', '⎽', '├', '┤', '┴', '┬', '│', '≤', '≥', 'π', '≠', '£', '·',
}
