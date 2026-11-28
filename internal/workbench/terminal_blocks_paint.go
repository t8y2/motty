package workbench

import (
	"fmt"
	"strings"
	"time"

	"github.com/vivaui/viva/gfx"
	"motty/internal/term"
)

// Command block chrome: a faint command-row tint, a hairline at the block's
// first row, and a status strip in the left padding. All of it is painted under
// the cells, so text, links and images keep the terminal's own rendering. The
// alternate screen has no blocks, and a synchronized update paints the held
// screen, so both keep the grid untouched.

const (
	blockStatusWidth  = 3
	blockCopiedLinger = 1200 * time.Millisecond
)

func (state *State) blockStatusColor(status blockStatus) gfx.Color {
	switch status {
	case blockRunning:
		return state.termPal.accent
	case blockSucceeded:
		return state.termPal.success
	case blockFailed:
		return state.termPal.danger
	case blockInterrupted:
		return state.termPal.warning
	}
	return state.termPal.muted
}

func (state *State) paintBlockRow(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, line uint64, row int, cellHeight, cellWidth float32) {
	terminal := session.term
	index := terminal.blockAtLine(line)
	if index < 0 {
		return
	}
	block := &terminal.blocks[index]
	pal := &state.termPal
	y := geometry.top + float32(row)*cellHeight
	left := geometry.left
	right := geometry.left + float32(geometry.cols)*cellWidth
	start := terminal.blockDisplayStartAt(index)
	if line == start && row > 0 {
		canvas.Fill(gfx.Rect{X: left, Y: y, Width: right - left, Height: 1}, withAlpha(pal.termFG, 0.12))
	}
	if index == terminal.blockSelected {
		// Hover is an interaction state, not the resting command row: it may tint
		// the whole block, while the input itself gets a bar rather than a fill.
		canvas.Fill(gfx.Rect{X: left, Y: y, Width: right - left, Height: cellHeight}, withAlpha(pal.accent, 0.055))
	}
	commandStart, commandLast := terminal.blockCommandBounds(block)
	if line >= commandStart && line <= commandLast {
		canvas.Fill(gfx.Rect{
			X: geometry.area.X + 4, Y: y, Width: blockStatusWidth, Height: cellHeight,
		}, withAlpha(state.blockStatusColor(block.status), 0.85))
	}
}

// selectBlockAt is the click selection: the block under the press becomes the
// one whose toolbar and tint show, and a press outside every block clears it.
func (state *State) selectBlockAt(session *Session, position term.Position) bool {
	terminal := session.term
	selected := -1
	if terminal.blocksEnabled {
		if index := terminal.blockAtLine(terminal.displayLine(position.Row)); index >= 0 {
			selected = index
		}
	}
	if selected == terminal.blockSelected {
		return false
	}
	terminal.blockSelected = selected
	return true
}

// blockTruncated reports a folded block whose head no longer names the whole
// output: the command row or the output head left the retained buffer.
func (t *terminal) blockTruncated(block *commandBlock) bool {
	if block == nil || !block.collapsed {
		return false
	}
	first, ok := t.screen.RetainedFrom()
	if !ok {
		return false
	}
	return block.headTrimmed || !block.command.ok ||
		block.command.ok && block.command.line < first ||
		block.exec.ok && block.exec.line < first
}

func blockToolbarStatus(state *State, block *commandBlock) string {
	messages := state.messages.Blocks
	if block.status == blockRunning {
		return messages.Running
	}
	if block.status == blockUnknown {
		return messages.Finished
	}
	if block.duration() <= 0 {
		return fmt.Sprintf(messages.ExitCodeFmt, block.exitCode)
	}
	return formatBlockDuration(block.duration()) + " · " + fmt.Sprintf(messages.ExitCodeFmt, block.exitCode)
}

func blockCopyIcon(action blockAction) uint8 {
	switch action {
	case blockCopyCommand:
		return iconTerminal
	case blockCopyOutput:
		return iconCopy
	}
	return iconSelectAll
}

func blockCopyLabel(state *State, action blockAction) string {
	switch action {
	case blockCopyCommand:
		return state.messages.Blocks.CopyCommand
	case blockCopyOutput:
		return state.messages.Blocks.CopyOutput
	}
	return state.messages.Blocks.CopyBlock
}

func blockCopyPart(action blockAction) int {
	switch action {
	case blockCopyCommand:
		return terminalPartBlockCopyCommand
	case blockCopyOutput:
		return terminalPartBlockCopyOutput
	case blockToggleFold:
		return terminalPartBlockToggleFold
	}
	return terminalPartBlockCopyAll
}

func blockActionLabel(state *State, action blockAction, block *commandBlock) string {
	if action == blockToggleFold {
		if block.collapsed {
			return state.messages.Blocks.Expand
		}
		return state.messages.Blocks.Collapse
	}
	return blockCopyLabel(state, action)
}

// paintBlockToolbar draws the hover toolbar at the block's first visible row.
// The buttons are control parts, declared after the grid, so the page's own
// routing makes them real buttons for hover, press and accessibility while the
// grid keeps the rest of the pointer.
func blockToolbarActions(block *commandBlock) []blockAction {
	actions := []blockAction{blockCopyCommand, blockCopyOutput, blockCopyAll}
	if blockFoldLines(block) > 0 {
		actions = append(actions, blockToggleFold)
	}
	return actions
}

// blockToolbarVisible is the paint's own gate, reused by the keyboard path so a
// focused button cannot outlive the toolbar.
func (t *terminal) blockToolbarVisible() bool {
	index := t.blockSelected
	return t.blocksEnabled && index >= 0 && index < len(t.blocks) && !t.selection.dragging &&
		t.screen.Modes().Mouse == term.MouseOff
}

func (state *State) paintBlockToolbar(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, rows int, cellHeight float32) {
	terminal := session.term
	index := terminal.blockSelected
	if !terminal.blocksEnabled || index < 0 || index >= len(terminal.blocks) ||
		terminal.selection.dragging || terminal.screen.Modes().Mouse != term.MouseOff {
		return
	}
	block := &terminal.blocks[index]
	startLine := terminal.blockDisplayStartAt(index)
	var firstRow int64
	if len(terminal.display) == 0 {
		firstRow = int64(startLine) - int64(terminal.screen.LineNumber(0))
	} else {
		row, ok := terminal.displayRowOfLine(startLine)
		if !ok {
			row, ok = terminal.displayRowAtOrAfter(startLine)
		}
		if !ok {
			return
		}
		firstRow = int64(row)
	}
	if firstRow < 0 {
		firstRow = 0
	}
	if firstRow >= int64(rows) {
		return
	}
	if state.cellWidth <= 0 {
		return
	}
	height := min(cellHeight, 22)
	if height < 14 {
		return
	}
	size := min(float32(11), height-6)
	pal := &state.termPal
	label := blockToolbarStatus(state, block)
	labelWidth := float32(0)
	if label != "" {
		labelWidth = canvas.TextWidthWeight(label, size, gfx.WeightMedium)
	}
	actions := blockToolbarActions(block)
	warning := terminal.blockTruncated(block)
	gap, pad, dot := float32(3), float32(5), float32(10)
	width := pad*2 + labelWidth + dot + gap + float32(len(actions))*height + float32(len(actions)-1)*gap
	if warning {
		width += height + gap
	}
	right := geometry.left + float32(geometry.cols)*state.cellWidth - 8
	if width > right-geometry.left {
		return
	}
	x := right - width
	y := geometry.top + float32(firstRow)*cellHeight + (cellHeight-height)/2
	box := gfx.Rect{X: x, Y: y, Width: width, Height: height}
	canvas.Rounded(box, 5, withAlpha(pal.shell, 0.97))
	canvas.RoundedBorder(box, 5, 1, withAlpha(pal.termFG, 0.18), gfx.Color{})

	dotX := x + pad
	dotY := y + (height-6)/2
	canvas.Rounded(gfx.Rect{X: dotX, Y: dotY, Width: 6, Height: 6}, 3, state.blockStatusColor(block.status))
	labelX := dotX + dot + 2
	if label != "" {
		color := pal.muted
		switch block.status {
		case blockRunning:
			color = pal.accent
		case blockSucceeded:
			color = pal.success
		case blockFailed:
			color = pal.danger
		case blockInterrupted:
			color = pal.warning
		}
		canvas.Label(label, gfx.Rect{X: labelX, Y: y, Width: labelWidth + 1, Height: height}, gfx.TextStyle{
			Size: size, Weight: gfx.WeightMedium, Color: color,
		})
	}

	buttonX := labelX + labelWidth + gap
	if warning {
		button := gfx.Rect{X: buttonX, Y: y, Width: height, Height: height}
		part := terminal.parts.At(terminalPartBlockTruncatedHint)
		if canvas.Hovered() == part {
			canvas.Rounded(button, 4, withAlpha(pal.termFG, 0.10))
		}
		iconSize := min(height-8, 14)
		canvas.DrawIcon(lucideIcons[iconAlert], gfx.Rect{
			X: button.X + (button.Width-iconSize)/2, Y: button.Y + (button.Height-iconSize)/2,
			Width: iconSize, Height: iconSize,
		}, pal.warning)
		hint := state.messages.Blocks.TruncatedHint
		canvas.HitWithOptions(part, button, hint, gfx.HitOptions{
			Role: gfx.RoleButton, SkipTab: true, Cursor: gfx.PointerCursorHand, Hint: hint,
		})
		buttonX += height + gap
	}
	for _, action := range actions {
		button := gfx.Rect{X: buttonX, Y: y, Width: height, Height: height}
		part := terminal.parts.At(blockCopyPart(action))
		hovered := canvas.Hovered() == part
		if hovered {
			canvas.Rounded(button, 4, withAlpha(pal.termFG, 0.10))
		}
		iconColor := pal.muted
		if hovered {
			iconColor = pal.text
		}
		glyph := blockCopyIcon(action)
		if action == blockToggleFold {
			glyph = iconChevronRight
			if block.collapsed {
				glyph = iconChevronDown
			}
		}
		if terminal.blockCopied == action && action != blockCopyNone {
			remaining := blockCopiedLinger - (state.now() - terminal.blockCopiedAt)
			if remaining > 0 {
				glyph = iconCheck
				iconColor = pal.accent
				canvas.RequestFrameIn(remaining)
			}
		}
		iconSize := min(height-8, 14)
		canvas.DrawIcon(lucideIcons[glyph], gfx.Rect{
			X: button.X + (button.Width-iconSize)/2, Y: button.Y + (button.Height-iconSize)/2,
			Width: iconSize, Height: iconSize,
		}, iconColor)
		label := blockActionLabel(state, action, block)
		canvas.HitWithOptions(part, button, label, gfx.HitOptions{
			Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand,
			Hint: label,
		})
		buttonX += height + gap
	}
}

// terminalBlockToolbarKey gives the floating toolbar a real keyboard path. Tab
// enters it while a block is selected, Tab/Shift+Tab move between its buttons,
// Enter or Space activates one, and Escape returns to the terminal grid.
func (state *State) terminalBlockToolbarKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if event.Release || state.modal != modalNone {
		return gfx.EventResult{}, false
	}
	session := state.terminalPageSession(event.Target)
	if session == nil || session.term == nil {
		return gfx.EventResult{}, false
	}
	terminal := session.term
	if !terminal.blockToolbarVisible() {
		return gfx.EventResult{}, false
	}
	block := &terminal.blocks[terminal.blockSelected]
	actions := blockToolbarActions(block)
	if len(actions) == 0 {
		return gfx.EventResult{}, false
	}
	parts := make([]gfx.ID, len(actions))
	for index, action := range actions {
		parts[index] = terminal.parts.At(blockCopyPart(action))
	}
	focus := func(index int) gfx.EventResult {
		return gfx.EventResult{Focus: parts[index], FocusVisible: true, Handled: true, Repaint: true}
	}
	leave := func() gfx.EventResult {
		terminal.blockSelected = -1
		return gfx.EventResult{Focus: terminal.controlID, FocusVisible: true, Handled: true, Repaint: true}
	}

	if part, ok := event.Target.In(terminal.parts); ok {
		current := -1
		for index, action := range actions {
			if blockCopyPart(action) == part {
				current = index
				break
			}
		}
		if current < 0 {
			return gfx.EventResult{}, false
		}
		switch event.Key {
		case gfx.KeyEnter, gfx.KeySpace:
			repaint := false
			if actions[current] == blockToggleFold {
				repaint = state.toggleHoveredBlockFold(session)
			} else {
				repaint = state.copyHoveredBlock(session, actions[current])
			}
			return gfx.EventResult{Handled: true, Repaint: repaint}, true
		case gfx.KeyTab:
			next := current + 1
			if event.Modifiers&gfx.ModifierShift != 0 {
				next = current - 1
			}
			if next < 0 || next >= len(parts) {
				return leave(), true
			}
			return focus(next), true
		case gfx.KeyEscape:
			return leave(), true
		}
		return gfx.EventResult{}, false
	}

	// Tab from the terminal grid enters the toolbar only while a block is
	// selected; ordinary shell completion keeps its Tab when it is not.
	if event.Target == terminal.controlID && event.Key == gfx.KeyTab {
		if event.Modifiers&gfx.ModifierShift != 0 {
			return focus(len(parts) - 1), true
		}
		return focus(0), true
	}
	return gfx.EventResult{}, false
}

func (state *State) terminalBlockPointer(session *Session, action blockAction, event gfx.PointerEvent) gfx.EventResult {
	switch event.Kind {
	case gfx.PointerDown:
		if event.Button == gfx.PointerButtonSecondary {
			return state.terminalPointer(session, event)
		}
		return gfx.EventResult{Handled: true}
	case gfx.PointerUp:
		if event.Button != gfx.PointerButtonPrimary && event.Button != gfx.PointerButtonNone {
			return gfx.EventResult{Handled: true}
		}
		if action == blockToggleFold {
			return gfx.EventResult{Handled: true, Repaint: state.toggleHoveredBlockFold(session)}
		}
		return gfx.EventResult{Handled: true, Repaint: state.copyHoveredBlock(session, action)}
	}
	return gfx.EventResult{}
}

func (state *State) copyHoveredBlock(session *Session, action blockAction) bool {
	terminal := session.term
	index := terminal.blockSelected
	if index < 0 || index >= len(terminal.blocks) {
		position := state.terminalPositionAt(session, state.terminalMenu.pointer.X, state.terminalMenu.pointer.Y)
		index = terminal.blockAtLine(terminal.displayLine(position.Row))
	}
	if index < 0 {
		return false
	}
	text, ok := terminal.blockText(&terminal.blocks[index], action)
	if !ok {
		state.status = state.messages.Blocks.OutputUnavailable
		return true
	}
	if err := state.options.SetClipboardText(text); err != nil {
		state.status = err.Error()
		return true
	}
	state.status = state.messages.Blocks.Copied
	terminal.blockSelected = index
	terminal.blockCopied, terminal.blockCopiedAt = action, state.now()
	return true
}

// paintFoldSummary replaces a collapsed block's first output row with a summary
// strip. The row still carries the buffer line's identity, so gutter, images and
// hit testing keep their coordinates; only the cells are painted over.
func (state *State) paintFoldSummary(canvas *gfx.Canvas, session *Session, geometry terminalGeometry, fold int, block *commandBlock, hidden uint64, y float32) {
	pal := &state.termPal
	left := geometry.left
	right := geometry.left + float32(geometry.cols)*state.cellWidth
	height := state.cellHeight
	if right <= left || height <= 0 {
		return
	}
	canvas.Fill(gfx.Rect{X: left, Y: y, Width: right - left, Height: height}, withAlpha(pal.termFG, 0.055))
	iconSize := min(height-6, 12)
	textLeft := left + 8
	if iconSize > 4 {
		canvas.DrawIcon(lucideIcons[iconChevronRight], gfx.Rect{
			X: left + 4, Y: y + (height-iconSize)/2, Width: iconSize, Height: iconSize,
		}, state.blockStatusColor(block.status))
		textLeft = left + 4 + iconSize + 6
	}
	size := min(state.terminalFontSize, height-4)
	label := state.messages.Blocks.HiddenOneLine
	if hidden != 1 {
		label = fmt.Sprintf(state.messages.Blocks.HiddenLinesFmt, hidden)
	}
	first, hasFirst := session.term.screen.RetainedFrom()
	if block.text != "" && (block.headTrimmed || !block.command.ok || hasFirst && block.command.line < first) {
		command := strings.Join(strings.Fields(block.text), " ")
		label = command + "  ·  " + label
	}
	label = elide(canvas, label, size, gfx.WeightMedium, right-textLeft-4)
	if label != "" {
		canvas.Label(label, gfx.Rect{X: textLeft, Y: y, Width: right - textLeft - 4, Height: height}, gfx.TextStyle{
			Size: size, Weight: gfx.WeightMedium, Color: pal.muted,
		})
	}
	part := session.term.parts.At(terminalPartFoldSummaryBase + fold)
	canvas.HitWithOptions(part, gfx.Rect{X: left, Y: y, Width: right - left, Height: height}, state.messages.Blocks.Expand, gfx.HitOptions{
		Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Hint: state.messages.Blocks.Expand,
		Expandable: true,
	})
}
