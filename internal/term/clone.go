package term

import "maps"

// Clone returns an independent copy of the terminal's state: the grid of both
// screens, its lines and their cells and marks, the cursor, the modes, the
// title, tabs, charsets and any escape sequence the parser is in the middle of.
//
// It exists for state replication, where one owner keeps several screens that
// each continue from a different point of the same byte stream (Mosh's received
// states). The owner's callbacks are not copied — the clone has none — and
// neither are inline images, pending notifications or the history ring's spare
// line, which a replicated stream does not carry.
func (t *Terminal) Clone() *Terminal {
	clone := *t
	clone.main.lines = cloneLines(t.main.lines)
	clone.alt.lines = cloneLines(t.alt.lines)
	clone.main.kittyStack = append([]uint32(nil), t.main.kittyStack...)
	clone.alt.kittyStack = append([]uint32(nil), t.alt.kittyStack...)
	if t.scr == &t.main {
		clone.scr = &clone.main
	} else {
		clone.scr = &clone.alt
	}
	if t.hist != nil {
		clone.hist = cloneLines(t.hist)
	}
	clone.histSpare = nil
	clone.tabs = append([]bool(nil), t.tabs...)
	clone.hyperlinks = append([]hyperlinkRecord(nil), t.hyperlinks...)
	clone.hyperlinkByTarget = maps.Clone(t.hyperlinkByTarget)
	clone.kittyNotes = append([]kittyPending(nil), t.kittyNotes...)
	clone.runBuf = nil
	clone.reflow = reflow{}
	// The owner's hooks belong to the terminal they were installed on; a copy
	// that fired them would report one input twice.
	clone.replyFn = nil
	clone.clipboardFn = nil
	clone.notifyFn = nil
	clone.bellFn = nil
	clone.cwdFn = nil
	clone.cwdReportFn = nil
	clone.promptReadyFn = nil
	clone.promptStartFn = nil
	clone.inputEnvironmentFn = nil
	clone.homeDirectoryFn = nil
	clone.commandStartFn = nil
	clone.commandFinishedFn = nil
	clone.syncFn = nil
	clone.clock = nil
	clone.images = nil
	clone.kitty = nil
	clone.imageOps = nil
	clone.imageBytes = 0
	clone.imageWake = nil
	clone.parser.osc = append([]byte(nil), t.parser.osc...)
	clone.parser.dcs = append([]byte(nil), t.parser.dcs...)
	clone.parser.apc = append([]byte(nil), t.parser.apc...)
	clone.parser.scratch = nil
	return &clone
}

// cloneLines copies a grid's line pointers and the lines they name: a clone
// must not share a row, or a write to either would change both.
func cloneLines(lines []*line) []*line {
	if lines == nil {
		return nil
	}
	cloned := make([]*line, len(lines))
	for i, l := range lines {
		if l == nil { // an unused slot of the history ring
			continue
		}
		c := *l
		c.cells = append([]Cell(nil), l.cells...)
		c.marks = append([]Mark(nil), l.marks...)
		if l.hyperlinks != nil {
			links := append([]uint32(nil), (*l.hyperlinks)...)
			c.hyperlinks = &links
		}
		cloned[i] = &c
	}
	return cloned
}
