package workbench

import (
	"sort"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// A collapsed block is a presentation-only view of the same buffer: the
// emulator keeps every line, and the display map decides which retained lines
// fill the screen. Building the map from the live bottom keeps the prompt and
// cursor where they were, while folded output above them disappears.
//
// The map is rebuilt every paint from the blocks, never stored as terminal
// state, so a reflow, a prune or an unfold cannot leave stale rows behind. With
// no folded block the map is the identity: display row i is view row i, which is
// what the terminal did before blocks existed.

type displayEntry struct {
	line   uint64
	index  int
	fold   int
	hidden uint64
}

// blockFoldLines is how many output lines a finished block can fold. Every
// output line is folded, including a single one: the summary replaces the first
// output row, and the line where the next prompt begins belongs to that prompt.
func blockFoldLines(block *commandBlock) uint64 {
	if block.status == blockRunning || !block.exec.ok {
		return 0
	}
	if block.span.ok {
		if block.span.endLogical > block.span.execLogical {
			return block.span.endLogical - block.span.execLogical
		}
		if block.span.endLogical == block.span.execLogical && block.end.ok && block.end.column > block.span.execCell {
			return 1
		}
		return 0
	}
	end := block.endLine()
	if end < block.exec.line {
		return 0
	}
	lines := end - block.exec.line
	if block.end.ok && block.end.column > 0 {
		// Output without a trailing newline ends on the line D's cursor is on.
		lines++
	}
	return lines
}

// blockFoldRows is how many rows the display map removes while collapsed. The
// summary occupies the first output row, so a block with one output line keeps
// its row count and only changes that row's content.
func blockFoldRows(block *commandBlock) uint64 {
	if !block.collapsed {
		return 0
	}
	lines := blockFoldLines(block)
	if lines <= 1 {
		return 0
	}
	rows := lines - 1
	if block.end.ok && block.end.column > 0 {
		// The last line carries the next prompt, so it stays visible.
		rows--
	}
	return rows
}

// foldsActive says whether the display map may hide lines. A synchronized update
// paints a held screen, the alternate screen has no blocks, find must see every
// match, and a program that owns the mouse must get the real grid.
func (t *terminal) foldsActive() bool {
	return t.sync == nil && !t.presentedAltScreen() && !t.find.open &&
		t.screen.Modes().Mouse == term.MouseOff
}

type foldRange struct {
	block      int
	summary    uint64
	end        uint64
	lines      uint64
	startIndex int
	endIndex   int
}

func (t *terminal) markFoldsDirty() {
	t.foldsValid = false
	t.folds = t.folds[:0]
	t.foldRows = 0
}

// ensureFolds rebuilds the collapsed range index only after a block or fold
// change. Blocks are in line order, so the index is already sorted.
func (t *terminal) ensureFolds() {
	base, _ := t.screen.RetainedFrom()
	epoch := t.screen.ReflowEpoch()
	if t.foldsValid && t.foldsBase == base && t.foldsEpoch == epoch {
		return
	}
	t.folds = t.folds[:0]
	t.foldRows = 0
	count := t.screen.RetainedLines()
	for index := range t.blocks {
		block := &t.blocks[index]
		if !block.collapsed {
			continue
		}
		lines := blockFoldLines(block)
		if lines == 0 {
			continue
		}
		var fold foldRange
		if block.span.ok {
			summaryIndex, endIndex := t.blockFoldBounds(block, base, count)
			if endIndex < summaryIndex+1 {
				endIndex = min(summaryIndex+1, count)
			}
			summary := base + uint64(summaryIndex)
			fold = foldRange{
				block: index, summary: summary, end: base + uint64(endIndex), lines: lines,
				startIndex: summaryIndex + 1,
				endIndex:   endIndex,
			}
		} else {
			end := t.blockFoldEnd(index)
			if end > block.exec.line {
				lines = min(lines, end-block.exec.line)
			}
			if lines == 0 {
				continue
			}
			summary := blockFoldSummaryLine(block, base)
			fold = foldRange{
				block: index, summary: summary, end: end, lines: lines,
				startIndex: int(summary+1) - int(base),
				endIndex:   int(end) - int(base),
			}
			fold.startIndex = max(fold.startIndex, 0)
			fold.endIndex = min(fold.endIndex, count)
			if fold.endIndex < fold.startIndex {
				fold.endIndex = fold.startIndex
			}
		}
		t.folds = append(t.folds, fold)
		t.foldRows += fold.endIndex - fold.startIndex
	}
	t.foldsBase = base
	t.foldsEpoch = epoch
	t.foldsValid = true
}

// blockFoldBounds maps a span to the current retained rows. summaryIndex is the
// first row of execLogical (or the oldest retained row when the head was
// trimmed); endIndex is the first row at or after endLogical.
func (t *terminal) blockFoldBounds(block *commandBlock, base uint64, count int) (summaryIndex, endIndex int) {
	summaryIndex = 0
	if line, ok := t.screen.FirstLineOfLogical(block.span.execLogical); ok && line >= base {
		summaryIndex = int(line - base)
	}
	endIndex = count
	if line, ok := t.screen.FirstLineAtOrAfterLogical(block.span.endLogical); ok && line >= base {
		endIndex = int(line - base)
	}
	endIndex = min(max(endIndex, summaryIndex), count)
	return summaryIndex, endIndex
}

// blockFoldEnd is the retained line where a fold stops. For a logical span this
// is the first row at or after endLogical; the legacy physical fallback exists
// only for hand-built tests and old fixtures.
func (t *terminal) blockFoldEnd(index int) uint64 {
	block := &t.blocks[index]
	if block.span.ok {
		if line, ok := t.screen.FirstLineAtOrAfterLogical(block.span.endLogical); ok {
			return line
		}
		if base, ok := t.screen.RetainedFrom(); ok {
			return base + uint64(t.screen.RetainedLines())
		}
		return block.endLine()
	}
	return t.legacyBlockFoldEnd(index)
}

func (t *terminal) legacyBlockFoldEnd(index int) uint64 {
	block := &t.blocks[index]
	end := block.endLine()
	if index+1 < len(t.blocks) {
		if next := t.blocks[index+1].startLine(); next > block.exec.line {
			end = next
		}
	} else if block.status != blockRunning {
		if !block.endApprox && !block.stale && t.blockPendingCommand && t.blockCommand.ok && t.blockCommand.line > block.exec.line {
			end = t.blockCommand.line
		} else if point, ok := t.screen.CursorPoint(); ok && point.Line > block.exec.line {
			end = point.Line
		}
	}
	if !block.endApprox && !block.stale {
		end = max(end, block.endLine())
	}
	return end
}

// blockFoldSummaryLine is where the fold summary is painted. When the command
// or output head left the scrollback, the summary takes the oldest retained
// line so an unrelated newer history line is not left where the command was.
func blockFoldSummaryLine(block *commandBlock, base uint64) uint64 {
	if block.headTrimmed || !block.command.ok || block.command.line < base || block.exec.line < base {
		return base
	}
	return block.exec.line
}

// blockFoldSummaryLine maps a logical span's first output row, falling back to
// the retained base when the head was trimmed.
func (t *terminal) blockFoldSummaryLine(block *commandBlock, base uint64) uint64 {
	if !block.span.ok {
		return blockFoldSummaryLine(block, base)
	}
	if block.headTrimmed {
		return base
	}
	if line, ok := t.screen.FirstLineOfLogical(block.span.execLogical); ok && line >= base {
		return line
	}
	return base
}

func (t *terminal) foldAt(line uint64) (int, uint64) {
	t.ensureFolds()
	index := sort.Search(len(t.folds), func(index int) bool {
		return t.folds[index].summary > line
	}) - 1
	if index < 0 {
		return -1, 0
	}
	fold := &t.folds[index]
	if line == fold.summary || line > fold.summary && line < fold.end {
		return fold.block, fold.lines
	}
	return -1, 0
}

// blockDisplayStart is where the block's chrome starts on screen. A span's
// logical start wins; physical anchors are only for hand-built tests and
// legacy blocks. A trimmed head falls back to the oldest retained row.
func (t *terminal) blockDisplayStart(block *commandBlock) uint64 {
	if block.span.ok {
		if base, ok := t.screen.RetainedFrom(); ok {
			if line, found := t.screen.FirstLineOfLogical(block.span.startLogical); found && line >= base {
				return line
			}
			if block.headTrimmed {
				return base
			}
		}
	}
	start := block.startLine()
	if base, ok := t.screen.RetainedFrom(); ok && (block.headTrimmed || !block.command.ok) && base < start {
		return base
	}
	return start
}

// blockDisplayStartAt uses the same effective start as selection, including the
// previous block's boundary.
func (t *terminal) blockDisplayStartAt(index int) uint64 {
	if index < 0 || index >= len(t.blocks) {
		return 0
	}
	block := &t.blocks[index]
	if block.span.ok {
		if base, ok := t.screen.RetainedFrom(); ok {
			if line, found := t.screen.FirstLineOfLogical(t.blockSpanStart(index, block)); found && line >= base {
				return line
			}
			if block.headTrimmed {
				return base
			}
		}
	}
	return t.blockDisplayStart(block)
}

func (t *terminal) hasFolds() bool {
	t.ensureFolds()
	return len(t.folds) > 0
}

func (t *terminal) foldContaining(index int) int {
	t.ensureFolds()
	found := sort.Search(len(t.folds), func(i int) bool {
		return t.folds[i].endIndex > index
	})
	if found < len(t.folds) && t.folds[found].startIndex <= index {
		return found
	}
	return -1
}

func (t *terminal) hiddenAtOrBelow(index int) int {
	t.ensureFolds()
	hidden := 0
	for _, fold := range t.folds {
		if index < fold.startIndex {
			break
		}
		if fold.endIndex <= fold.startIndex {
			continue
		}
		hidden += min(index, fold.endIndex-1) - fold.startIndex + 1
	}
	return hidden
}

func (t *terminal) visibleAtOrBelow(index, count int) int {
	hiddenBelow := 0
	if index > 0 {
		hiddenBelow = t.hiddenAtOrBelow(index - 1)
	}
	return (count - index) - (t.foldRows - hiddenBelow)
}

// rawIndexForDisplayOffset is the retained-buffer index of the bottom line at a
// display offset. Visible lines are counted without walking every hidden row, so
// a scrolled folded view costs O(log rows + folds), not O(buffer).
func (t *terminal) rawIndexForDisplayOffset(offset int) int {
	count := t.screen.RetainedLines()
	if count == 0 {
		return 0
	}
	t.ensureFolds()
	need := offset + 1
	low, high := 0, count
	for low < high {
		mid := (low + high) / 2
		if t.visibleAtOrBelow(mid, count) >= need {
			low = mid + 1
		} else {
			high = mid
		}
	}
	index := max(min(low-1, count-1), 0)
	for {
		fold := t.foldContaining(index)
		if fold < 0 {
			break
		}
		index = t.folds[fold].endIndex
		if index >= count {
			index = count - 1
			break
		}
	}
	return index
}

func (t *terminal) buildDisplayMap(rows int) {
	t.display = t.display[:0]
	if rows <= 0 {
		return
	}
	if t.sync != nil || !t.foldsActive() || !t.hasFolds() {
		t.displayOffset = t.screen.ViewOffset()
		return
	}
	t.clampDisplayOffset(rows)
	base, ok := t.screen.RetainedFrom()
	total := t.screen.RetainedLines()
	if !ok || total == 0 {
		for row := 0; row < rows; row++ {
			t.display = append(t.display, displayEntry{index: -1, fold: -1})
		}
		return
	}
	bottom := t.rawIndexForDisplayOffset(t.displayOffset)
	for index := bottom; index >= 0 && len(t.display) < rows; index-- {
		line := base + uint64(index)
		fold, hidden := t.foldAt(line)
		if fold >= 0 && hidden > 0 {
			summary := t.blockFoldSummaryLine(&t.blocks[fold], base)
			if line > summary {
				// Jump over the whole hidden range to its summary line, which
				// may be the oldest retained line when the head was trimmed.
				index = int(summary-base) + 1
				continue
			}
		}
		entry := displayEntry{line: line, index: index, fold: -1}
		if fold >= 0 && hidden > 0 {
			entry.fold, entry.hidden = fold, hidden
		}
		t.display = append(t.display, entry)
	}
	for left, right := 0, len(t.display)-1; left < right; left, right = left+1, right-1 {
		t.display[left], t.display[right] = t.display[right], t.display[left]
	}
	for len(t.display) < rows {
		t.display = append(t.display, displayEntry{index: -1, fold: -1})
	}
}

// hiddenRows is how many rows the collapsed blocks remove from the display.
func (t *terminal) hiddenRows() int {
	if !t.foldsActive() {
		return 0
	}
	t.ensureFolds()
	return t.foldRows
}

// displayCapacity is the display total and the largest display offset that can
// scroll back. The offset is in visible lines, never raw hidden rows.
func (t *terminal) displayCapacity(rows int) (total, maxOffset int) {
	count := t.screen.RetainedLines()
	if t.sync != nil || !t.foldsActive() || !t.hasFolds() {
		return count, max(count-rows, 0)
	}
	total = count - t.hiddenRows()
	return total, max(total-rows, 0)
}

func (t *terminal) clampDisplayOffset(rows int) bool {
	_, maxOffset := t.displayCapacity(rows)
	if t.displayOffset > maxOffset {
		t.displayOffset = maxOffset
		return true
	}
	if t.displayOffset < 0 {
		t.displayOffset = 0
		return true
	}
	return false
}

// syncRawView keeps the terminal's raw view near the display position for the
// features that still read it (find, prediction, autofill).
func (t *terminal) syncRawView(rows int) {
	count := t.screen.RetainedLines()
	if _, ok := t.screen.RetainedFrom(); !ok || count == 0 {
		return
	}
	bottom := t.rawIndexForDisplayOffset(t.displayOffset)
	target := count - 1 - bottom
	if target < 0 {
		target = 0
	}
	target = min(target, t.screen.Scrollback())
	if delta := target - t.screen.ViewOffset(); delta != 0 {
		t.screen.Scroll(delta)
	}
}

func (t *terminal) scrollToBottomView() bool {
	changed := t.displayOffset != 0
	t.displayOffset = 0
	if t.screen.ScrollToBottom() {
		changed = true
	}
	return changed
}

// displayRowOfLine finds a buffer line's display row. The inverse of
// displayLine, for placing chrome: subtracting line numbers is wrong while a
// fold above the block has removed rows.
func (t *terminal) displayRowOfLine(line uint64) (int, bool) {
	for row := range t.display {
		if entry := t.display[row]; entry.index >= 0 && entry.line == line {
			return row, true
		}
	}
	return 0, false
}

// displayRowAtOrAfter finds the first displayed line at or after line, which is
// where a block starts when its first line scrolled above the viewport.
func (t *terminal) displayRowAtOrAfter(line uint64) (int, bool) {
	for row := range t.display {
		if entry := t.display[row]; entry.index >= 0 && entry.line >= line {
			return row, true
		}
	}
	return 0, false
}

func (t *terminal) displayEntryAt(row int) (displayEntry, bool) {
	if row < 0 || row >= len(t.display) {
		return displayEntry{fold: -1}, false
	}
	entry := t.display[row]
	if entry.index < 0 {
		return entry, false
	}
	return entry, true
}

func (t *terminal) displayRow(row int) term.Row {
	if t.sync != nil || len(t.display) == 0 {
		return t.presentedRow(row)
	}
	entry, ok := t.displayEntryAt(row)
	if !ok {
		return term.Row{}
	}
	line, _ := t.screen.RetainedRow(entry.index)
	return line
}

func (t *terminal) displayLine(row int) uint64 {
	if t.sync != nil || len(t.display) == 0 {
		return t.screen.LineNumber(row)
	}
	if entry, ok := t.displayEntryAt(row); ok {
		return entry.line
	}
	return 0
}

func (t *terminal) displayIndex(row int) int {
	if t.sync != nil || len(t.display) == 0 {
		return t.screen.ViewLine(row)
	}
	if entry, ok := t.displayEntryAt(row); ok {
		return entry.index
	}
	return -1
}

func (t *terminal) displayFold(row int) (int, uint64, bool) {
	if t.sync != nil || len(t.display) == 0 {
		return -1, 0, false
	}
	if entry, ok := t.displayEntryAt(row); ok && entry.fold >= 0 {
		return entry.fold, entry.hidden, true
	}
	return -1, 0, false
}

func (t *terminal) displayPoint(position term.Position) term.Point {
	return term.Point{Line: t.displayLine(position.Row), Column: position.Column}
}

func (t *terminal) displayCursorRow(cursor term.Cursor) int {
	if !t.foldsActive() {
		return cursor.Y
	}
	point, ok := t.screen.CursorPoint()
	if !ok {
		return cursor.Y
	}
	for row := range t.display {
		if t.display[row].line == point.Line {
			return row
		}
	}
	return cursor.Y
}

// displayScroll is the scroll area's total and offset in display lines. It is
// the same formula the terminal always used when nothing is folded: the offset
// counts the complete lines above the viewport.
func (t *terminal) displayScroll(rows int) (total, offset int) {
	count := t.screen.RetainedLines()
	if _, ok := t.screen.RetainedFrom(); !ok || count == 0 {
		return 0, 0
	}
	if t.sync != nil || !t.foldsActive() || !t.hasFolds() {
		t.displayOffset = t.screen.ViewOffset()
		return count, max(count-rows-t.screen.ViewOffset(), 0)
	}
	total = count - t.hiddenRows()
	t.clampDisplayOffset(rows)
	offset = max(total-rows-t.displayOffset, 0)
	return total, offset
}

// toggleHoveredBlockFold opens or closes the block under the pointer. The
// toolbar already knows the block, but a wheel or keyboard scroll can have moved
// it since the toolbar was painted, so the pointer position is the fallback.
func (state *State) toggleHoveredBlockFold(session *Session) bool {
	terminal := session.term
	index := terminal.blockSelected
	if index < 0 || index >= len(terminal.blocks) {
		position := state.terminalPositionAt(session, state.terminalMenu.pointer.X, state.terminalMenu.pointer.Y)
		index = terminal.blockAtLine(terminal.displayLine(position.Row))
	}
	if index < 0 {
		return false
	}
	return state.toggleBlock(session, &terminal.blocks[index])
}

func (state *State) toggleTerminalMenuBlock(session *Session) {
	block := state.terminalMenuBlock(session)
	if block != nil {
		state.toggleBlock(session, block)
	}
}

func (state *State) toggleBlock(session *Session, block *commandBlock) bool {
	return state.setBlockCollapsed(session, block, !block.collapsed)
}

// setBlockCollapsed changes one fold and keeps the summary row where it was on
// screen. Without that anchor a fold in scrollback shrinks the display under a
// fixed raw bottom, so the commands below the fold stay outside the viewport
// and only a wheel scroll brings them back.
func (state *State) setBlockCollapsed(session *Session, block *commandBlock, collapsed bool) bool {
	terminal := session.term
	if block.collapsed == collapsed || !terminal.blocksEnabled || blockFoldLines(block) == 0 {
		return false
	}
	if !terminal.hasFolds() {
		// No fold existed yet: the raw view is the display view.
		terminal.displayOffset = terminal.screen.ViewOffset()
	}
	block.collapsed = collapsed
	terminal.markFoldsDirty()
	terminal.selection.clear()
	_, rows := terminal.screen.Size()
	if total, _ := terminal.displayScroll(rows); total <= rows {
		terminal.scrollToBottomView()
	} else {
		terminal.clampDisplayOffset(rows)
		terminal.syncRawView(rows)
	}
	return true
}

// terminalFoldSummaryPointer expands a collapsed block when its summary row is
// pressed. The row is painted chrome, so the whole row is the target, not only
// the chevron; a press elsewhere in the terminal is untouched.
func (state *State) terminalFoldSummaryPointer(session *Session, index int, event gfx.PointerEvent) gfx.EventResult {
	terminal := session.term
	if terminal == nil || !terminal.blocksEnabled || index < 0 || index >= len(terminal.blocks) {
		return gfx.EventResult{}
	}
	block := &terminal.blocks[index]
	if !block.collapsed {
		return gfx.EventResult{}
	}
	switch event.Kind {
	case gfx.PointerDown:
		changed := terminal.blockSelected != index
		terminal.blockSelected = index
		return gfx.EventResult{Handled: true, Repaint: changed}
	case gfx.PointerUp:
		if event.Button == gfx.PointerButtonPrimary || event.Button == gfx.PointerButtonNone {
			return gfx.EventResult{Handled: true, Repaint: state.setBlockCollapsed(session, block, false)}
		}
	}
	return gfx.EventResult{Handled: true}
}

// scrollDisplay moves the view in visible lines. Hidden folded rows are never
// part of the scroll range, so scrolling at either end cannot enter them.
func (t *terminal) scrollDisplay(lines int) bool {
	if lines == 0 {
		return false
	}
	_, rows := t.screen.Size()
	if t.sync != nil || !t.foldsActive() || !t.hasFolds() {
		changed := t.screen.Scroll(lines)
		t.displayOffset = t.screen.ViewOffset()
		return changed
	}
	total, _ := t.displayScroll(rows)
	maxOffset := max(total-rows, 0)
	next := min(max(t.displayOffset+lines, 0), maxOffset)
	if next == t.displayOffset {
		return false
	}
	t.displayOffset = next
	t.syncRawView(rows)
	return true
}
