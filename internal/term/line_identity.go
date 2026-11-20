package term

// Line identity and view-independent cursor points.
//
// A command block is a range of buffer lines, not of view rows: the user may
// scroll the view while a command runs, and the block must keep naming the same
// text. Point already counts lines the way LineNumber does, but deriving it from
// Cursor through PointAt is only correct while the view is at the bottom:
// LineNumber subtracts the view offset, while the cursor always sits on the live
// screen. CursorPoint and InputStart therefore use the emulator's own live
// position, and the line identity lookup lets an owner follow a block across a
// reflow, which changes line numbers without changing the buffer generation.
//
// A line is identified two ways. ID names one physical row and changes when the
// row is recycled for new text. Logical names the logical line, the text between
// hard newlines: the physical rows a soft wrap produces from one line share it,
// a hard newline starts a new one, and a reflow keeps it while it re-splits the
// text into different rows. Logical values only grow, and never change across a
// reflow, so a block can record a logical line and find its current rows later.

// CursorPoint is the buffer point under the live cursor. It takes the cursor's
// screen row directly instead of a view row: PointAt expects a view row, and the
// two differ while the user reads scrollback, so a block started or finished
// then still gets the line the cursor is really on. ok is false on the alternate
// screen and while a wrap is pending, where the cursor has no settled buffer
// point.
func (t *Terminal) CursorPoint() (Point, bool) {
	if t.onAlt || t.pend {
		return Point{}, false
	}
	return Point{Line: t.scrolled + uint64(t.cy) + 1, Column: t.cx}, true
}

// InputStart is the start of the shell's editable line as OSC 133 B recorded it.
// It is the point the command text begins at, independent of the current view.
func (t *Terminal) InputStart() (Point, bool) {
	if !t.inputStartSet || t.onAlt || t.pend {
		return Point{}, false
	}
	return t.inputStart, true
}

// LineIDAt returns the identity of the retained line at line number line. The
// identity survives scrolling, history trimming and, where a reflow can keep the
// row, a width change; a recycled row gets a new identity.
func (t *Terminal) LineIDAt(line uint64) (uint64, bool) {
	base, count := t.lineBase(), uint64(t.bufferLineCount())
	if count == 0 || line < base || line >= base+count {
		return 0, false
	}
	l := t.bufferLine(int(line - base))
	if l == nil || l.id == 0 {
		return 0, false
	}
	return l.id, true
}

// LineOfID returns the current line number of the retained line with id. It is
// the inverse of LineIDAt, for following a block across a reflow. ok is false
// once the line left the retained buffer or a reflow merged the row away.
func (t *Terminal) LineOfID(id uint64) (uint64, bool) {
	if id == 0 {
		return 0, false
	}
	base, count := t.lineBase(), t.bufferLineCount()
	for index := 0; index < count; index++ {
		if l := t.bufferLine(index); l != nil && l.id == id {
			return base + uint64(index), true
		}
	}
	return 0, false
}

// RetainedLogical returns the logical line of retained-buffer index, 0 being
// the oldest retained line. It returns 0 outside the retained buffer.
func (t *Terminal) RetainedLogical(index int) uint64 {
	if index < 0 || index >= t.bufferLineCount() {
		return 0
	}
	if l := t.bufferLine(index); l != nil {
		return l.logical
	}
	return 0
}

// LogicalAt returns the logical line of the retained line numbered line. It is
// the logical counterpart of LineIDAt and accepts the same range.
func (t *Terminal) LogicalAt(line uint64) (uint64, bool) {
	base, count := t.lineBase(), uint64(t.bufferLineCount())
	if count == 0 || line < base || line >= base+count {
		return 0, false
	}
	l := t.bufferLine(int(line - base))
	if l == nil || l.logical == 0 {
		return 0, false
	}
	return l.logical, true
}

// FirstLineOfLogical returns the first retained line whose logical line is
// logical. ok is false once every row of that line has been trimmed away, or
// when no retained row has the identity.
func (t *Terminal) FirstLineOfLogical(logical uint64) (uint64, bool) {
	if logical == 0 {
		return 0, false
	}
	base, count := t.lineBase(), t.bufferLineCount()
	for index := 0; index < count; index++ {
		if l := t.bufferLine(index); l != nil && l.logical == logical {
			return base + uint64(index), true
		}
	}
	return 0, false
}

// FirstLineAtOrAfterLogical returns the first retained line whose logical line
// is at least logical. A row inserted above older rows can carry a newer
// logical identity, so this scans rather than relying on buffer-order
// monotonicity. ok is false when every retained row is older.
func (t *Terminal) FirstLineAtOrAfterLogical(logical uint64) (uint64, bool) {
	base, count := t.lineBase(), t.bufferLineCount()
	for index := 0; index < count; index++ {
		if l := t.bufferLine(index); l != nil && l.logical >= logical {
			return base + uint64(index), true
		}
	}
	return 0, false
}

// CurrentLogical is the logical line under the live cursor, for a block that
// ends where the shell's output currently is. ok is false wherever CursorPoint
// is false: on the alternate screen and while a wrap is pending.
func (t *Terminal) CurrentLogical() (uint64, bool) {
	point, ok := t.CursorPoint()
	if !ok {
		return 0, false
	}
	return t.LogicalAt(point.Line)
}

// ReflowEpoch counts the resizes that changed the grid size. It lets an owner
// cache row positions across writes and rebuild them when a reflow may have
// moved rows, which does not change Logical identities but does change the
// physical rows they occupy.
func (t *Terminal) ReflowEpoch() uint64 { return t.reflowEpoch }

// RetainedLines is the number of lines the retained buffer holds, history
// included. It is the range RetainedRow accepts.
func (t *Terminal) RetainedLines() int { return t.bufferLineCount() }

// RetainedRow returns retained-buffer line index, 0 being the oldest line the
// buffer still holds. A view can show any of these lines, not only the screen's
// rows, which is what lets an owner draw a collapsed view of the buffer.
func (t *Terminal) RetainedRow(index int) Row {
	if index < 0 || index >= t.bufferLineCount() {
		return Row{}
	}
	return t.rowOf(t.bufferLine(index))
}

// RetainedLine is the absolute line number of retained-buffer index.
func (t *Terminal) RetainedLine(index int) uint64 {
	return t.lineBase() + uint64(index)
}
