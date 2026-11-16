package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
	"motty/internal/workbench/icons"
)

const terminalFindButtonSize = float32(24)

// findBarSlots are the places of the find bar's field and count, which
// declareFindBar lays out and paintFindBarSlots fills.
type findBarSlots struct {
	icon, field, count ui.Box
}

// declareFindBar declares the bar of a pane whose search is open: the magnifier,
// the query field taking what the buttons leave, the match count and the three
// buttons. The field keeps 120 px however narrow the pane.
func (state *State) declareFindBar(c ui.C, session *Session) (slots findBarSlots) {
	find := &session.term.find
	disabled := len(find.matches) == 0
	ui.Row(c).Fill().PadSides(0, termPadX, 0, termPadX).Items(ui.Center).Children(func() {
		slots.icon = ui.Slot(c).Size(16, 16).MarginSides(0, 8, 0, 0)
		slots.field = ui.Slot(c).Grow(1).MinWidth(120).Height(state.Theme().Metrics.ControlHeight).MarginSides(0, 10, 0, 0)
		slots.count = ui.Slot(c).Size(92, 22).Shrink(0).MarginSides(0, 8, 0, 0)
		state.declareFindButton(c, hitTerminalFindPrevious, icons.ArrowUp, state.messages.TerminalFindPrevious, disabled).MarginSides(0, 2, 0, 0)
		state.declareFindButton(c, hitTerminalFindNext, icons.ArrowDown, state.messages.TerminalFindNext, disabled).MarginSides(0, 4, 0, 0)
		state.declareFindButton(c, hitTerminalFindClose, icons.X, state.messages.TerminalFindClose, false)
	})
	return slots
}

func (state *State) paintFindBarSlots(canvas *gfx.Canvas, session *Session, slots findBarSlots) {
	find := &session.term.find
	canvas.DrawIcon(icons.Search, slots.icon.Rect(), state.pal.muted)
	value := find.editor.String()
	focused := canvas.Focused() == hitTerminalFindField
	layout := components.PaintInputGroup(canvas, state.Theme(), slots.field.Rect(), components.InputGroupSnapshot{
		ID:            hitTerminalFindField,
		Label:         state.messages.TerminalFindLabel,
		Value:         value,
		Placeholder:   state.messages.TerminalFindPlaceholder,
		Focused:       focused,
		NativeEditing: focused && value != "",
		InlineEnd: components.InputGroupSlot{Kind: components.InputGroupSlotButton, ID: hitTerminalFindSettings, Icon: icons.Settings,
			Label: state.messages.FindSettings.Title, ButtonSize: components.InputGroupButtonIconXS},
	})
	state.findSettingsUI.anchor = layout.InlineEnd
	canvas.LabelCenter(find.count, slots.count.Rect(), gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.muted})
}

// declareFindButton declares one of the bar's icon buttons: quiet until the
// pointer is over it, dimmed when there is nothing to go to.
func (state *State) declareFindButton(c ui.C, id gfx.ID, icon gfx.Icon, label string, disabled bool) ui.Box {
	foreground := state.pal.muted
	button := ui.Row(c).Size(terminalFindButtonSize, terminalFindButtonSize).Shrink(0).Center().Radius(radius.item).
		HitWith(id, label, gfx.HitOptions{Role: gfx.RoleButton, Disabled: disabled, SkipFocus: true})
	if disabled {
		foreground.Alpha *= 0.45
	} else {
		button.Hover(ui.Look{}.Background(state.pal.surfaceHot))
	}
	return button.Children(func() {
		icon := ui.Icon(c, icon).Size(14, 14).Color(foreground)
		if !disabled {
			icon.StateFrom(button).Hover(ui.Look{}.Color(state.pal.text))
		}
	})
}

// terminalFindRow walks the matches intersecting a row in buffer order.
func terminalFindRow(find *terminalFind, line, first int) (start, end int) {
	for first < len(find.matches) && find.matches[first].End.Line < line {
		first++
	}
	end = first
	for end < len(find.matches) && find.matches[end].Start.Line <= line {
		end++
	}
	return first, end
}

func terminalFindSpan(match term.SearchMatch, line, cols int) cellSpan {
	from, to := 0, cols
	if match.Start.Line == line {
		from = match.Start.Column
	}
	if match.End.Line == line {
		to = match.End.Column
	}
	return cellSpan{from: min(max(from, 0), cols), to: min(max(to, 0), cols)}
}

func (state *State) terminalFindBounds(geometry terminalGeometry, row int, span cellSpan) gfx.Rect {
	left := snap(geometry.left + float32(span.from)*state.cellWidth)
	right := snap(geometry.left + float32(span.to)*state.cellWidth)
	return gfx.Rect{X: left, Y: geometry.top + float32(row)*state.cellHeight, Width: right - left, Height: state.cellHeight}
}

func (state *State) paintTerminalFindMatches(canvas *gfx.Canvas, find *terminalFind, geometry terminalGeometry, row, line, first int, ink, background gfx.Color) int {
	start, end := terminalFindRow(find, line, first)
	var fill, active cellSpan
	for index := start; index < end; index++ {
		match := find.matches[index]
		if match.End.Line < line {
			continue
		}
		span := terminalFindSpan(match, line, geometry.cols)
		if span.to <= span.from {
			continue
		}
		if index == find.active {
			active = span
		}
		// Overlapping hits share one fill so their opacity does not stack.
		switch {
		case fill.to <= fill.from:
			fill = span
		case span.from < fill.to:
			fill.to = max(fill.to, span.to)
		default:
			canvas.Fill(state.terminalFindBounds(geometry, row, fill), background)
			fill = span
		}
	}
	if fill.to > fill.from {
		canvas.Fill(state.terminalFindBounds(geometry, row, fill), background)
	}
	if active.to > active.from {
		outline := ink
		if background.Alpha < 0.5 {
			outline = state.termPal.termFG
		}
		canvas.RoundedOutline(state.terminalFindBounds(geometry, row, active), 0, 1, outline)
	}
	return start
}

// terminalFindRuns splits only ASCII runs, where each byte is one cell. Wide
// glyphs and combining marks are already standalone runs and stay intact.
func terminalFindRuns(dst []gfx.TextRun, entry *rowEntry, find *terminalFind, line, cols, first int, ink gfx.Color) ([]gfx.TextRun, int) {
	start, end := terminalFindRow(find, line, first)
	if start == end {
		return dst[:0], start
	}
	dst = dst[:0]
	matchIndex := start
	for _, run := range entry.runs {
		from, to := int(run.Column), int(run.Column+run.Cells)
		for from < to {
			for matchIndex < end && terminalFindSpan(find.matches[matchIndex], line, cols).to <= from {
				matchIndex++
			}
			cut, color := to, run.Color
			if matchIndex < end {
				span := terminalFindSpan(find.matches[matchIndex], line, cols)
				if span.from > from {
					cut = min(cut, span.from)
				} else {
					cut, color = min(cut, span.to), ink
				}
			}
			part := run
			if from != int(run.Column) || cut != to {
				part.Column, part.Cells = int32(from), int32(cut-from)
				part.Bytes = part.Cells
				part.Ligatures = part.Ligatures && part.Cells > 1
			}
			part.Color = color
			dst = append(dst, part)
			from = cut
		}
	}
	return dst, start
}
