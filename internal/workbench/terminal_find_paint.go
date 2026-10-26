package workbench

import (
	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/ui"
	"motty/internal/term"
	"motty/internal/workbench/icons"
)

const terminalFindButtonSize = float32(24)

// findBarSlots are the places of the find bar's field and count, which
// declareFindBar lays out and paintFindBarSlots fills.
type findBarSlots struct {
	icon, field, count flex.Box
}

// declareFindBar declares the bar of a pane whose search is open: the magnifier,
// the query field taking what the buttons leave, the match count and the three
// buttons. The field keeps 120 px however narrow the pane.
func (state *State) declareFindBar(f *flex.Frame, session *Session) (slots findBarSlots) {
	find := &session.term.find
	disabled := len(find.matches) == 0
	f.Row().Fill().PadSides(0, termPadX, 0, termPadX).Items(flex.Center).Children(func() {
		slots.icon = f.Slot().Size(16, 16).MarginSides(0, 8, 0, 0)
		slots.field = f.Slot().Grow(1).MinWidth(120).Height(26).MarginSides(0, 10, 0, 0)
		slots.count = f.Slot().Size(92, 22).Shrink(0).MarginSides(0, 8, 0, 0)
		state.declareFindButton(f, hitTerminalFindPrevious, icons.ArrowUp, state.messages.TerminalFindPrevious, disabled).MarginSides(0, 2, 0, 0)
		state.declareFindButton(f, hitTerminalFindNext, icons.ArrowDown, state.messages.TerminalFindNext, disabled).MarginSides(0, 4, 0, 0)
		state.declareFindButton(f, hitTerminalFindClose, icons.X, state.messages.TerminalFindClose, false)
	})
	return slots
}

func (state *State) paintFindBarSlots(canvas *ui.Canvas, session *Session, slots findBarSlots) {
	find := &session.term.find
	canvas.DrawIcon(icons.Search, slots.icon.Rect(), state.pal.muted)
	value := find.editor.String()
	focused := canvas.Focused() == hitTerminalFindField
	components.PaintInput(canvas, state.Theme(), slots.field.Rect(), components.InputSnapshot{
		ID:              hitTerminalFindField,
		Label:           state.messages.TerminalFindLabel,
		Value:           value,
		Placeholder:     state.messages.TerminalFindPlaceholder,
		Focused:         focused,
		PlatformEditing: focused && value != "",
	})
	canvas.LabelCenter(find.count, slots.count.Rect(), ui.TextStyle{Size: metrics.FontSizeS, Weight: ui.WeightMedium, Color: state.pal.muted})
}

// declareFindButton declares one of the bar's icon buttons: quiet until the
// pointer is over it, dimmed when there is nothing to go to.
func (state *State) declareFindButton(f *flex.Frame, id ui.ID, icon ui.Icon, label string, disabled bool) flex.Box {
	foreground := state.pal.muted
	button := f.Row().Size(terminalFindButtonSize, terminalFindButtonSize).Shrink(0).Center().Radius(radius.item).
		HitWith(id, label, ui.HitOptions{Role: ui.RoleButton, Disabled: disabled, SkipFocus: true})
	if disabled {
		foreground.Alpha *= 0.45
	} else {
		button.HoverBackground(state.pal.surfaceHot)
		if f.Canvas().Hovered() == id {
			foreground = state.pal.text
		}
	}
	return button.Children(func() {
		f.Icon(icon).Size(14, 14).Color(foreground)
	})
}

func (state *State) paintTerminalFindMatches(canvas *ui.Canvas, session *Session, geometry terminalGeometry, row, line, first int) int {
	find := &session.term.find
	for first < len(find.matches) && find.matches[first].End.Line < line {
		first++
	}
	color := state.pal.warning
	color.Alpha = 0.28
	for index := first; index < len(find.matches) && find.matches[index].Start.Line <= line; index++ {
		if index == find.active || find.matches[index].End.Line < line {
			continue
		}
		state.paintTerminalFindMatch(canvas, geometry, row, line, find.matches[index], color)
	}
	if find.active >= 0 && find.active < len(find.matches) {
		active := find.matches[find.active]
		if active.Start.Line <= line && active.End.Line >= line {
			color.Alpha = 0.62
			state.paintTerminalFindMatch(canvas, geometry, row, line, active, color)
		}
	}
	return first
}

func (state *State) paintTerminalFindMatch(canvas *ui.Canvas, geometry terminalGeometry, row, line int, match term.SearchMatch, color ui.Color) {
	from, to := 0, geometry.cols
	if match.Start.Line == line {
		from = match.Start.Column
	}
	if match.End.Line == line {
		to = match.End.Column
	}
	from, to = min(max(from, 0), geometry.cols), min(max(to, 0), geometry.cols)
	if to <= from {
		return
	}
	left := snap(geometry.left + float32(from)*state.cellWidth)
	right := snap(geometry.left + float32(to)*state.cellWidth)
	y := geometry.top + float32(row)*state.cellHeight
	canvas.Fill(ui.Rect{X: left, Y: y, Width: right - left, Height: state.cellHeight}, color)
}
