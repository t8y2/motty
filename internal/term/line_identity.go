package term

// A line has two identities. ID names one physical row and changes when the row
// is recycled for new text. Logical names the logical line, the text between
// hard newlines: the physical rows a soft wrap produces from one line share it,
// a hard newline starts a new one, and a reflow keeps it while it re-splits the
// text into different rows. Logical values only grow, so a live identity is
// never reused.
//
// The identity is additive: nothing in the emulator reads it, so physical row
// ids, row text, the cursor and prompt handling behave exactly as before. Only
// an owner that asks for a logical line observes it.

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

// LogicalAt returns the logical line of the retained line numbered line, in the
// numbering LineNumber and RetainedFrom use. ok is false for a line number the
// retained buffer no longer holds.
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

// CurrentLogical is the logical line under the live cursor, for an owner that
// ends its range where the shell's output currently is. ok is false on the
// alternate screen and while a wrap is pending, where the cursor has no
// settled buffer point.
func (t *Terminal) CurrentLogical() (uint64, bool) {
	if t.onAlt || t.pend {
		return 0, false
	}
	return t.LogicalAt(t.scrolled + uint64(t.cy) + 1)
}

// ReflowEpoch counts the resizes that changed the grid size. It lets an owner
// cache row positions across writes and rebuild them when a reflow may have
// moved rows, which does not change logical identities but does change the
// physical rows they occupy.
func (t *Terminal) ReflowEpoch() uint64 { return t.reflowEpoch }
