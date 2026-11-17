package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
	"motty/internal/workbench/icons"
)

const (
	terminalFindButtonSize = float32(24)
	terminalFindMinWidth   = float32(260)
	terminalFindCountWidth = float32(64)
	// terminalFindControlsWidth is what the controls at the field's right end
	// take, with their gaps and the field's inset: the count, four buttons and
	// the 2 px between each. The field keeps its text clear of it.
	terminalFindControlsWidth = terminalFindCountWidth + 4*terminalFindButtonSize + 4*2 + 8
)

// findBarSlots are the places of the find field and its count, which
// declareFindBar lays out and paintFindBarSlots fills.
type findBarSlots struct {
	field, count, settings ui.Box
}

// declareFindBar declares the bar of a pane whose search is open: one field
// across the row, with the magnifier inside its start and the match count, the
// settings, previous, next and close buttons inside its end.
func (state *State) declareFindBar(c ui.C, session *Session) (slots findBarSlots) {
	find := &session.term.find
	disabled := len(find.matches) == 0
	// The last pixel row is the bar's divider, which would cover the
	// field's bottom border.
	ui.Row(c).Fill().PadSides(0, 0, 1, 0).Children(func() {
		ui.Stack(c).Grow(1).MinWidth(terminalFindMinWidth).FillHeight().Children(func() {
			slots.field = ui.Slot(c).Fill().Draw(session.term.findDraw)
			ui.Row(c).Absolute().Top(0).Bottom(0).Right(4).Items(ui.Center).Children(func() {
				slots.count = ui.Slot(c).Size(terminalFindCountWidth, 22).Shrink(0).MarginSides(0, 2, 0, 0)
				slots.settings = state.declareFindButton(c, hitTerminalFindSettings, icons.Settings, state.messages.FindSettings.Title, false).MarginSides(0, 2, 0, 0)
				state.declareFindButton(c, hitTerminalFindPrevious, icons.ArrowUp, state.messages.TerminalFindPrevious, disabled).MarginSides(0, 2, 0, 0)
				state.declareFindButton(c, hitTerminalFindNext, icons.ArrowDown, state.messages.TerminalFindNext, disabled).MarginSides(0, 2, 0, 0)
				state.declareFindButton(c, hitTerminalFindClose, icons.X, state.messages.TerminalFindClose, false)
			})
		})
	})
	return slots
}

// drawFindField paints the field in its place in the tree, so the controls
// declared after it sit over its surface; the count and the settings anchor
// follow in paintFindBarSlots.
func (state *State) drawFindField(d *ui.DrawContext, sessionID uint32) {
	session, ok := state.sessionByIDPointer(sessionID)
	if !ok || session.term == nil || !session.term.find.open {
		return
	}
	value := session.term.find.editor.String()
	focused := d.Canvas.Focused() == hitTerminalFindField
	// The field is square: it fills the bar, whose own edges are square.
	theme := state.Theme()
	theme.Metrics.RadiusControl = 0
	components.PaintInputGroup(d.Canvas, theme, d.Rect, components.InputGroupSnapshot{
		ID:            hitTerminalFindField,
		Label:         state.messages.TerminalFindLabel,
		Value:         value,
		Placeholder:   state.messages.TerminalFindPlaceholder,
		Focused:       focused,
		NativeEditing: focused && value != "",
		InlineStart:   components.InputGroupSlot{Kind: components.InputGroupSlotIcon, Icon: icons.Search},
		// An empty addon only reserves the room the declared controls sit in.
		InlineEnd: components.InputGroupSlot{Kind: components.InputGroupSlotText, Width: terminalFindControlsWidth},
	})
}

func (state *State) paintFindBarSlots(canvas *gfx.Canvas, session *Session, slots findBarSlots) {
	state.findSettingsUI.anchor = slots.settings.Rect()
	canvas.LabelCenter(session.term.find.count, slots.count.Rect(), gfx.TextStyle{Size: metrics.FontSizeS, Weight: gfx.WeightMedium, Color: state.pal.muted})
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
