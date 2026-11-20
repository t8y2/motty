package workbench

import (
	"fmt"
	"sort"
	"strings"
	"time"
	"unicode/utf8"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// Command blocks are a view of the shell's own OSC 133 markers, not a second
// terminal: the emulator keeps the only buffer, and a block is a pair of buffer
// ranges over it. The shell already tells us where the prompt ends (B), where a
// command starts (C) and where it finished with what status (D), so the block
// adds nothing to the execution path and fails soft when a shell has no
// integration.
//
// Anchors are kept by line identity as well as by number. Line numbers survive
// scrolling and history trimming, but a width reflow renumbers lines without
// changing BufferGeneration; the identity lets remapBlocks follow a block across
// that reflow, and degrade only the anchors a merge removed.

const (
	// maxCommandBlocks bounds one terminal's block history. The oldest blocks
	// leave first; the running block never does.
	maxCommandBlocks = 256
	// maxCommandText bounds the command line copied for a block. A pasted
	// command can be long, and the text is only needed for display and copy.
	maxCommandText = 64 << 10
)

type blockStatus uint8

const (
	blockRunning blockStatus = iota
	blockSucceeded
	blockFailed
	blockInterrupted
	blockUnknown
)

// blockAnchor is a buffer point plus the identity of its line, which is what
// survives a reflow.
type blockAnchor struct {
	line   uint64
	column int
	id     uint64
	ok     bool
}

// blockSpan is the reflow-invariant range a command owns. execLogical/execCell
// are recorded at OSC 133 C, endLogical (exclusive) at D. It never changes.
type blockSpan struct {
	ok           bool
	startLogical uint64 // OSC 133 A/B: the prompt/input logical line
	execLogical  uint64
	execCell     int
	endLogical   uint64
}

type commandBlock struct {
	id          uint64
	prompt      blockAnchor // OSC 133 A: the prompt's first cell
	command     blockAnchor // OSC 133 B: the first cell the user may type at
	exec        blockAnchor // OSC 133 C: the first output cell
	end         blockAnchor // OSC 133 D: where the next prompt begins
	text        string      // the command line as the screen echoed it
	started     time.Duration
	finished    time.Duration
	exitCode    int
	status      blockStatus
	span        blockSpan // logical range recorded at C/D; see blockSpan
	collapsed   bool      // the output is folded to its summary row
	headTrimmed bool      // the command/output head left the scrollback
	endApprox   bool      // the end anchor was approximated after a reflow
	stale       bool      // an anchor was lost to a reflow; metadata stays, ranges may not
}

// endLine is the last line the block covers. A running block reaches the live
// screen, which no stored point can name yet.
func (block commandBlock) endLine() uint64 {
	if block.status == blockRunning {
		return ^uint64(0)
	}
	if block.end.ok && block.end.line >= block.exec.line {
		return block.end.line
	}
	return block.exec.line
}

// startLine is the first line the block covers, prompt included.
func (block commandBlock) startLine() uint64 {
	if block.prompt.ok {
		return block.prompt.line
	}
	if block.command.ok {
		return block.command.line
	}
	return block.exec.line
}

// visualEndLine is the last line block chrome may cover. D's cursor normally
// sits at column zero of the line where the next prompt begins, so that line
// belongs to the next command's input, not to this block. Output without a
// trailing newline ends mid-line and keeps that line, because the prompt shares
// it with the output.
func (block commandBlock) visualEndLine() uint64 {
	end := block.endLine()
	if block.status == blockRunning || !block.end.ok || block.end.column != 0 || end <= block.startLine() {
		return end
	}
	return end - 1
}

func (block commandBlock) duration() time.Duration {
	if block.status == blockRunning || block.finished < block.started {
		return 0
	}
	return block.finished - block.started
}

// blockCapturePoint is the live cursor as an anchor. The emulator's CursorPoint
// reports the cursor's screen row, which stays correct while the user reads
// scrollback.
func blockCapturePoint(screen *term.Terminal) blockAnchor {
	point, ok := screen.CursorPoint()
	if !ok {
		return blockAnchor{}
	}
	return newBlockAnchor(screen, point)
}

func newBlockAnchor(screen *term.Terminal, point term.Point) blockAnchor {
	anchor := blockAnchor{line: point.Line, column: point.Column, ok: true}
	anchor.id, _ = screen.LineIDAt(point.Line)
	return anchor
}

func blockPointBefore(left, right term.Point) bool {
	return left.Line < right.Line || left.Line == right.Line && left.Column < right.Column
}

// blockPromptStart records OSC 133 A. A prompt that arrives while a block is
// still running means the shell never sent D; the block ends at the prompt with
// an unknown status rather than being left running forever.
func (state *State) blockPromptStart(session *Session) {
	t := session.term
	if t == nil || !t.blocksEnabled || t.folderToken == "" {
		return
	}
	if t.blockActive >= 0 {
		state.finishOpenBlock(t, blockUnknown, 0)
	}
	prompt := blockCapturePoint(t.screen)
	// A prompt redraw can emit A again without B (for example after a resize).
	// Keep the input anchor when A lands on the line B already named, otherwise
	// the next C would open no block at all.
	redraw := t.blockPendingCommand && t.blockCommand.ok && prompt.ok && t.blockCommand.line == prompt.line
	t.blockPrompt = prompt
	t.blockPendingPrompt = t.blockPrompt.ok
	if !redraw {
		t.blockPendingCommand = false
		t.blockCommand = blockAnchor{}
	}
	t.pruneBlocks()
}

// blockPromptReady records OSC 133 B as the start of the editable line.
func (state *State) blockPromptReady(session *Session) {
	t := session.term
	if t == nil || !t.blocksEnabled || !t.blockPendingPrompt {
		return
	}
	screen := t.screen
	if point, ok := screen.InputStart(); ok {
		t.blockCommand = newBlockAnchor(screen, point)
	} else {
		t.blockCommand = blockCapturePoint(screen)
	}
	t.blockPendingCommand = t.blockCommand.ok
}

// blockCommandStart records OSC 133 C and opens a block. A C without a preceding
// B is the integration script's install-time marker (or a stray sequence), so it
// opens nothing. The command text is captured now, while the echoed line is
// still on screen; output stays lazy and is copied from the buffer on demand.
func (state *State) blockCommandStart(session *Session) {
	t := session.term
	if t == nil || !t.blocksEnabled {
		return
	}
	if !t.blockPendingCommand {
		// A redraw may have emitted A without B. C still names a real command;
		// use the prompt line if it is the only anchor left.
		if !t.blockPendingPrompt || !t.blockPrompt.ok {
			return
		}
		t.blockCommand = t.blockPrompt
	}
	command := t.blockCommand
	prompt := t.blockPrompt
	t.blockPendingPrompt, t.blockPendingCommand = false, false
	if !prompt.ok || prompt.line > command.line {
		prompt = command
	}
	exec := blockCapturePoint(t.screen)
	if !exec.ok {
		return
	}
	if t.blockActive >= 0 {
		state.finishOpenBlock(t, blockUnknown, 0)
	}
	span := blockSpan{}
	if logical, ok := t.screen.LogicalAt(exec.line); ok {
		span = blockSpan{ok: true, execLogical: logical, execCell: exec.column, startLogical: logical}
		// The command/prompt row is the row just before the first output row. It
		// is safe to include only when it belongs after the previous block; the
		// shell's A/B anchors are not used because a resize redraw can leave them
		// in the previous block.
		if exec.column == 0 && exec.line > 0 {
			if prev, ok := t.screen.LogicalAt(exec.line - 1); ok && prev < logical {
				last := len(t.blocks) - 1
				if last < 0 || !t.blocks[last].span.ok || prev >= t.blocks[last].span.endLogical {
					span.startLogical = prev
				}
			}
		}
		if last := len(t.blocks) - 1; last >= 0 && t.blocks[last].span.ok && span.startLogical < t.blocks[last].span.endLogical {
			span.startLogical = span.execLogical
		}
	}
	block := commandBlock{
		id:      t.nextBlockID + 1,
		prompt:  prompt,
		command: command,
		exec:    exec,
		started: state.now(),
		status:  blockRunning,
		span:    span,
	}
	block.text = captureBlockCommand(t.screen, command, exec)
	t.nextBlockID++
	if last := len(t.blocks) - 1; last >= 0 && block.startLine() < t.blocks[last].startLine() {
		// The screen was cleared or reset under the block history; the old
		// ranges no longer describe this buffer.
		t.blocks = t.blocks[:0]
		t.blockActive = -1
		t.blockSelected = -1
	}
	t.blocks = append(t.blocks, block)
	t.blockActive = len(t.blocks) - 1
	t.markFoldsDirty()
	t.trimBlockTexts()
	t.pruneBlocks()
}

// blockCommandFinished records OSC 133 D. A D with no open block is an empty
// line at a prompt and must not become a block.
func (state *State) blockCommandFinished(session *Session, code int) {
	t := session.term
	if t == nil || !t.blocksEnabled || t.blockActive < 0 {
		return
	}
	status := blockFailed
	switch {
	case code == 0:
		status = blockSucceeded
	case code == 130:
		status = blockInterrupted
	}
	state.finishOpenBlock(t, status, code)
	t.pruneBlocks()
}

func (state *State) finishOpenBlock(t *terminal, status blockStatus, code int) {
	block := &t.blocks[t.blockActive]
	end := blockCapturePoint(t.screen)
	if end.ok && end.line >= block.exec.line {
		block.end = end
	} else if block.exec.ok {
		block.end = block.exec
	}
	if block.span.ok {
		if logical, ok := t.screen.LogicalAt(end.line); ok && logical >= block.span.execLogical {
			block.span.endLogical = logical
		} else {
			block.span.endLogical = block.span.execLogical
		}
	}
	block.status = status
	block.exitCode = code
	block.finished = state.now()
	t.blockActive = -1
	t.markFoldsDirty()
}

func captureBlockCommand(screen *term.Terminal, command, exec blockAnchor) string {
	if !command.ok || !exec.ok || blockPointBefore(term.Point{Line: exec.line, Column: exec.column}, term.Point{Line: command.line, Column: command.column}) {
		return ""
	}
	text := selectedRangeBefore(screen,
		term.Point{Line: command.line, Column: command.column},
		term.Point{Line: exec.line, Column: exec.column},
	)
	text = strings.TrimRight(text, " \t\r\n")
	if len(text) > maxCommandText {
		cut := maxCommandText
		for cut > 0 && !utf8.RuneStart(text[cut]) {
			cut--
		}
		text = text[:cut]
	}
	return text
}

// remapBlocks follows every anchor across a reflow, in which line numbers change
// but line identities survive where a wrapped row could. An anchor whose row was
// merged away is dropped; the block keeps its text and the anchors that remain.
func (state *State) remapBlocks(session *Session) {
	t := session.term
	if t == nil || len(t.blocks) == 0 && !t.blockPendingPrompt && !t.blockPendingCommand {
		return
	}
	screen := t.screen
	count := screen.RetainedLines()
	if t.lineIndex == nil {
		t.lineIndex = make(map[uint64]uint64, count)
	}
	lines := t.lineIndex
	clear(lines)
	for index := 0; index < count; index++ {
		row := screen.RetainedRow(index)
		if row.ID != 0 {
			lines[row.ID] = screen.RetainedLine(index)
		}
	}
	first, hasFirst := screen.RetainedFrom()
	last := uint64(0)
	if hasFirst && count > 0 {
		last = first + uint64(count) - 1
	}
	remap := func(anchor blockAnchor) (blockAnchor, bool, bool) {
		if !anchor.ok {
			return anchor, true, false
		}
		if anchor.id != 0 {
			if line, ok := lines[anchor.id]; ok {
				anchor.line = line
				return anchor, true, false
			}
		}
		if hasFirst && anchor.line < first {
			// The line was trimmed at the head of the buffer. Keep it on the
			// oldest retained line; its output is still partially copyable.
			anchor.line = first
			if row := screen.RetainedRow(0); row.ID != 0 {
				anchor.id = row.ID
			}
			return anchor, false, true
		}
		if hasFirst {
			// A reflow can merge a row's identity away, or compress the buffer
			// so the old line is past the last retained one. Clamp it and adopt
			// the current line's identity so later resizes can still follow it.
			anchor.line = min(max(anchor.line, first), last)
			if row := screen.RetainedRow(int(anchor.line - first)); row.ID != 0 {
				anchor.id = row.ID
			}
			return anchor, false, false
		}
		return blockAnchor{}, false, false
	}
	remapAnchor := func(anchor blockAnchor) blockAnchor {
		next, _, _ := remap(anchor)
		return next
	}
	t.blockPrompt = remapAnchor(t.blockPrompt)
	t.blockCommand = remapAnchor(t.blockCommand)
	t.blockPendingPrompt = t.blockPendingPrompt && t.blockPrompt.ok
	t.blockPendingCommand = t.blockPendingCommand && t.blockCommand.ok
	for index := range t.blocks {
		block := &t.blocks[index]
		if hasFirst {
			if block.command.ok && block.command.line < first || block.exec.ok && block.exec.line < first {
				block.headTrimmed = true
			}
		}
		var promptExact, promptTrimmed bool
		block.prompt, promptExact, promptTrimmed = remap(block.prompt)
		if !promptExact {
			block.prompt = blockAnchor{}
			block.headTrimmed = block.headTrimmed || promptTrimmed
		}
		var commandExact, commandTrimmed bool
		block.command, commandExact, commandTrimmed = remap(block.command)
		if !commandExact {
			// The old line number may name a different history row after a
			// reflow; do not paint an unrelated line as this command.
			block.command = blockAnchor{}
			block.headTrimmed = block.headTrimmed || commandTrimmed
		}
		var _, execTrimmed bool
		block.exec, _, execTrimmed = remap(block.exec)
		if execTrimmed {
			block.headTrimmed = true
		}
		var endExact bool
		block.end, endExact, _ = remap(block.end)
		if !endExact && block.end.ok {
			block.endApprox = true
		}
		if !block.exec.ok || !block.end.ok && block.status != blockRunning {
			block.stale = true
		}
		if !block.prompt.ok {
			block.prompt = block.command
		}
	}
	t.markFoldsDirty()
}

// pruneBlocks drops blocks whose last line left the retained buffer and enforces
// the per-terminal cap. The running block is never dropped.
func (t *terminal) pruneBlocks() {
	if first, ok := t.screen.RetainedFrom(); ok {
		cut := 0
		for cut < len(t.blocks) {
			block := &t.blocks[cut]
			if block.span.ok && block.status != blockRunning {
				baseLogical := t.screen.RetainedLogical(0)
				if block.span.endLogical < baseLogical {
					cut++
					continue
				}
				if _, ok := t.screen.FirstLineAtOrAfterLogical(block.span.endLogical); !ok {
					cut++
					continue
				}
				break
			}
			if block.endLine() >= first {
				break
			}
			cut++
		}
		if cut > 0 {
			t.blocks = append(t.blocks[:0], t.blocks[cut:]...)
			t.blockActive -= cut
			if t.blockActive < 0 {
				t.blockActive = -1
			}
			t.blockSelected -= cut
			if t.blockSelected < 0 {
				t.blockSelected = -1
			}
			t.markFoldsDirty()
		}
	}
	for len(t.blocks) > maxCommandBlocks {
		t.blocks = append(t.blocks[:0], t.blocks[1:]...)
		t.blockActive--
		if t.blockActive < 0 {
			t.blockActive = -1
		}
		t.blockSelected--
		if t.blockSelected < 0 {
			t.blockSelected = -1
		}
		t.markFoldsDirty()
	}
}

// blockSpanRange maps a block span to current buffer points. A trimmed head
// starts at the oldest retained row; the end is the first row at/after
// endLogical, so the prompt row itself stays outside the copied range.
func (t *terminal) blockSpanRange(block *commandBlock) (start, end term.Point, ok bool) {
	if !block.span.ok || !block.exec.ok {
		return term.Point{}, term.Point{}, false
	}
	base, hasBase := t.screen.RetainedFrom()
	if block.status != blockRunning && block.span.endLogical < t.screen.RetainedLogical(0) {
		return term.Point{}, term.Point{}, false
	}
	startLine, found := t.screen.FirstLineOfLogical(block.span.execLogical)
	if found {
		start = term.Point{Line: startLine, Column: block.span.execCell}
	} else if hasBase {
		start = term.Point{Line: base}
	} else {
		return term.Point{}, term.Point{}, false
	}
	if block.status == blockRunning {
		point, ok := t.screen.CursorPoint()
		return start, point, ok
	}
	endLine, found := t.screen.FirstLineAtOrAfterLogical(block.span.endLogical)
	if !found {
		return term.Point{}, term.Point{}, false
	}
	end = term.Point{Line: endLine}
	if block.span.endLogical == block.span.execLogical {
		end.Column = block.end.column
	}
	return start, end, true
}

// blockCommandBounds is the command/prompt part of a block: from its logical
// start to the row before the first output row. Physical anchors are only a
// fallback for hand-built blocks.
func (t *terminal) blockCommandBounds(block *commandBlock) (start, end uint64) {
	start = t.blockDisplayStart(block)
	if !block.span.ok {
		end = block.exec.line
		if end > block.startLine() {
			end--
		}
		return start, end
	}
	if exec, ok := t.screen.FirstLineOfLogical(block.span.execLogical); ok {
		end = exec
		if end > start {
			end--
		}
		return start, end
	}
	return start, start
}

// blockSpanStart is the first logical line the block may claim. A stale prompt
// anchor can point into the previous block; the previous end is still the
// boundary, so never start before it.
func (t *terminal) blockSpanStart(index int, block *commandBlock) uint64 {
	start := block.span.startLogical
	if start == 0 {
		start = block.span.execLogical
	}
	if index > 0 && t.blocks[index-1].span.ok && start < t.blocks[index-1].span.endLogical {
		start = block.span.execLogical
	}
	return start
}

// blockAtLine finds the block covering an absolute line. The logical span is
// the source of truth; the physical range is only a fallback for hand-built
// fixtures and chrome rows.
func (t *terminal) blockAtLine(line uint64) int {
	if logical, ok := t.screen.LogicalAt(line); ok {
		for index := len(t.blocks) - 1; index >= 0; index-- {
			block := &t.blocks[index]
			if !block.span.ok {
				continue
			}
			end := block.span.endLogical
			if block.status == blockRunning {
				end = ^uint64(0)
			}
			start := t.blockSpanStart(index, block)
			if logical >= start && logical < end {
				return index
			}
		}
	}
	index := sort.Search(len(t.blocks), func(index int) bool {
		return t.blockDisplayStart(&t.blocks[index]) > line
	}) - 1
	if index >= 0 && line >= t.blockDisplayStart(&t.blocks[index]) && line <= t.blocks[index].visualEndLine() {
		// A span block is authoritative: an old physical anchor must never make
		// its chrome or selection claim a row outside the logical range.
		if t.blocks[index].span.ok {
			return -1
		}
		return index
	}
	return -1
}

type blockAction uint8

const (
	blockCopyCommand blockAction = iota
	blockCopyOutput
	blockCopyAll
	blockToggleFold
	blockCopyNone
)

// blockOutputAvailable says whether a copy of the output would be possible,
// without building the text. The context menu uses it so an open menu over a
// long block does not copy the whole output every frame.
func (t *terminal) blockOutputAvailable(block *commandBlock) bool {
	if block.span.ok {
		start, end, ok := t.blockSpanRange(block)
		return ok && (end.Line > start.Line || end.Line == start.Line && end.Column > start.Column)
	}
	if block.stale || !block.exec.ok {
		return false
	}
	first, ok := t.screen.RetainedFrom()
	if !ok {
		return false
	}
	startLine, startColumn := block.exec.line, block.exec.column
	if startLine < first {
		startLine, startColumn = first, 0
	}
	if block.status == blockRunning {
		point, ok := t.screen.CursorPoint()
		return ok && (point.Line > startLine || point.Line == startLine && point.Column > startColumn)
	}
	if !block.end.ok || block.end.line < startLine {
		return false
	}
	return block.end.line > startLine || block.end.line == startLine && block.end.column > startColumn
}

func (t *terminal) blockOutputNonEmpty(block *commandBlock) bool {
	return t.blockOutputAvailable(block)
}

func (t *terminal) blockTextAvailable(block *commandBlock, action blockAction) bool {
	switch action {
	case blockCopyCommand:
		return block.text != ""
	case blockCopyOutput:
		return t.blockOutputAvailable(block)
	case blockCopyNone:
		return false
	default:
		return t.blockOutputAvailable(block) && (block.text != "" || t.blockOutputNonEmpty(block))
	}
}

// blockText is what a copy action takes from a block. Output is read back from
// the emulator's buffer, so it is never duplicated while a command runs. A block
// whose output left the retained buffer refuses the copy instead of copying a
// clamped, unrelated range; its command text is still available.
func (t *terminal) blockText(block *commandBlock, action blockAction) (string, bool) {
	switch action {
	case blockCopyCommand:
		return block.text, block.text != ""
	case blockCopyOutput:
		return t.blockOutputText(block)
	case blockCopyNone:
		return "", false
	default:
		output, ok := t.blockOutputText(block)
		if !ok {
			return "", false
		}
		switch {
		case block.text == "":
			return output, output != ""
		case output == "":
			return block.text, true
		default:
			return block.text + "\n" + output, true
		}
	}
}

func (t *terminal) blockOutputText(block *commandBlock) (string, bool) {
	if block.span.ok {
		start, end, ok := t.blockSpanRange(block)
		if !ok {
			return "", false
		}
		if end.Line < start.Line || end.Line == start.Line && end.Column <= start.Column {
			return "", true
		}
		return strings.TrimRight(selectedRangeBefore(t.screen, start, end), " \t\r\n"), true
	}
	if block.stale || !block.exec.ok {
		return "", false
	}
	first, ok := t.screen.RetainedFrom()
	if !ok {
		return "", false
	}
	start := term.Point{Line: block.exec.line, Column: block.exec.column}
	if start.Line < first {
		// Only the tail survived the scrollback limit; copy what is left.
		start = term.Point{Line: first}
	}
	end, ok := block.end, block.end.ok
	if block.status == blockRunning {
		end = blockCapturePoint(t.screen)
		ok = end.ok
	}
	if !ok || end.line < start.Line {
		return "", false
	}
	if end.line == start.Line && end.column <= start.Column {
		return "", true
	}
	text := selectedRangeBefore(t.screen, start, term.Point{Line: end.line, Column: end.column})
	return strings.TrimRight(text, " \t\r\n"), true
}

func blockStatusIcon(status blockStatus) uint8 {
	switch status {
	case blockRunning:
		return iconActivity
	case blockSucceeded:
		return iconCircleCheck
	case blockFailed:
		return iconAlert
	case blockInterrupted:
		return iconStop
	}
	return iconHistory
}

// blockStatusText is the one-line status the menu shows for a block. A running
// block shows no duration: a live timer would ask for a frame per second.
func (state *State) blockStatusText(block *commandBlock) string {
	messages := state.messages.Blocks
	label := messages.Finished
	switch block.status {
	case blockRunning:
		return messages.Running
	case blockSucceeded:
		label = messages.Succeeded
	case blockFailed:
		label = messages.Failed
	case blockInterrupted:
		label = messages.Interrupted
	}
	if duration := block.duration(); duration > 0 {
		label += " · " + formatBlockDuration(duration)
	}
	if block.status != blockUnknown {
		label += " · " + fmt.Sprintf(messages.ExitCodeFmt, block.exitCode)
	}
	return label
}

func formatBlockDuration(duration time.Duration) string {
	switch {
	case duration < time.Second:
		return fmt.Sprintf("%dms", duration.Milliseconds())
	case duration < time.Minute:
		return fmt.Sprintf("%.2fs", duration.Seconds())
	default:
		minutes := int(duration / time.Minute)
		return fmt.Sprintf("%dm %02ds", minutes, int(duration/time.Second)%60)
	}
}

func (state *State) copyTerminalBlock(session *Session, id gfx.ID) {
	block := state.terminalMenuBlock(session)
	if block == nil {
		return
	}
	action := blockCopyAll
	switch id {
	case hitTerminalMenuBlockCommand:
		action = blockCopyCommand
	case hitTerminalMenuBlockOutput:
		action = blockCopyOutput
	}
	text, ok := session.term.blockText(block, action)
	if !ok {
		state.status = state.messages.Blocks.OutputUnavailable
		return
	}
	if err := state.options.SetClipboardText(text); err != nil {
		state.status = err.Error()
		return
	}
	state.status = state.messages.Blocks.Copied
}

// clearCommandBlocks drops every block when the display is erased. The rows keep
// their line numbers, so leaving the ranges behind would paint blocks over
// content that no longer exists.
func (state *State) clearCommandBlocks(sessionID uint32) {
	current, ok := state.sessionByIDPointer(sessionID)
	if !ok || current.term == nil {
		return
	}
	clearTerminalCommandBlocks(current.term)
}

// pruneCommandBlocks keeps the blocks that still name a line after a history
// erase. A block on the screen is untouched; one that lived in the dropped
// scrollback leaves with it.
func (state *State) pruneCommandBlocks(sessionID uint32) {
	current, ok := state.sessionByIDPointer(sessionID)
	if !ok || current.term == nil {
		return
	}
	current.term.pruneBlocks()
	current.term.selection.trim(current.term.screen)
}

// setCommandBlocks turns the command-block view on or off. Turning it off also
// turns the context-menu switch off and drops every block, because a block that
// is no longer shown must not answer a menu or a hover.
func (state *State) setCommandBlocks(on bool) bool {
	if state.terminalCommandBlocks == on {
		return false
	}
	state.terminalCommandBlocks = on
	// configureTerminal snapshots this for each new terminal; open terminals
	// keep the mode they started with, in both directions.
	return true
}

// setTerminalBlockMenuActions shows or hides the block rows in the terminal's
// context menu. Turning them on turns the block view on too; the setting cannot
// outlive the feature it belongs to.
func (state *State) setTerminalBlockMenuActions(on bool) bool {
	if state.terminalBlockMenuActions == on {
		return false
	}
	state.terminalBlockMenuActions = on
	return true
}

func clearTerminalCommandBlocks(t *terminal) {
	t.markFoldsDirty()
	t.lineIndex = nil
	t.blocks = nil
	t.blockActive = -1
	t.blockPrompt, t.blockCommand = blockAnchor{}, blockAnchor{}
	t.blockPendingPrompt, t.blockPendingCommand = false, false
	t.blockSelected = -1
	t.blockCopied = blockCopyNone
	t.display = t.display[:0]
}

const maxCommandTextTotal = 1 << 20

// trimBlockTexts keeps the newest command texts within one terminal's budget.
// Output stays in the emulator buffer; only the copied command text is capped.
func (t *terminal) trimBlockTexts() {
	total := 0
	for index := len(t.blocks) - 1; index >= 0; index-- {
		text := t.blocks[index].text
		if total+len(text) <= maxCommandTextTotal {
			total += len(text)
			continue
		}
		t.blocks[index].text = ""
	}
}

// selectedRangeBefore is SelectedRange over [start, end): the end point is where
// the next prompt begins, not a cell the block owns. Without this, copying a
// block whose command ended with a newline includes the prompt's first cell.
func selectedRangeBefore(screen *term.Terminal, start, end term.Point) string {
	if end.Line < start.Line || end.Line == start.Line && end.Column <= start.Column {
		return ""
	}
	if end.Column == 0 {
		if end.Line == 0 {
			return ""
		}
		cols, _ := screen.Size()
		end.Line--
		end.Column = cols - 1
	} else {
		end.Column--
	}
	return screen.SelectedRange(start, end)
}
