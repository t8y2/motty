package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

func (state *State) paintHistory(canvas *gfx.Canvas, viewport gfx.Rect) {
	state.rankHistory()
	h := &state.history
	m := state.messages.History
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitHistoryDismiss, CloseID: hitHistoryClose, InitialFocusID: hitHistorySearch,
		ContentSize: gfx.Size{Width: 680, Height: 480},
	})
	layout := dialog.Begin(m.Title, "")
	value := h.search.String()
	focused := canvas.Focused() == hitHistorySearch
	count := min(len(h.ranked)-h.top, historyVisibleRows)
	count = max(count, 0)
	// The pointer on a row makes it the current one, so the row the keyboard
	// chose does not stay lit beside it.
	current := -1
	for slot := 0; slot < count; slot++ {
		if canvas.Hovered() == hitHistoryItems.At(slot) {
			current = h.top + slot
		}
	}
	empty := m.Empty
	switch {
	case !h.on && len(h.entries) == 0:
		empty = m.EmptyOff
	case len(h.entries) == 0:
		empty = m.EmptyNone
	}

	var search, scope ui.Box
	f := state.historyLayout.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() {
		f.Row().Height(36).Gap(8).Shrink(0).Children(func() {
			search = f.Slot().Grow(1).Height(36)
			scope = f.Slot().Width(210).Height(36).Self(ui.Center)
		})
		defer func() {
			f.Text(m.Hint).FontSize(metrics.FontSizeS).Color(state.pal.faint).LineHeight(18).Ellipsis().Shrink(0)
		}()
		if count == 0 {
			f.Text(empty).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(20).TextCenter().MarginSides(36, 0, 0, 0).Grow(1)
			return
		}
		f.Column().Gap(2).MarginSides(10, 0, 0, 0).Grow(1).Children(func() {
			for slot := 0; slot < count; slot++ {
				index := h.top + slot
				entry := h.entries[h.ranked[index]]
				id := hitHistoryItems.At(slot)
				selected := current == index || current < 0 && h.selected == index
				color, detailColor := state.pal.text, state.pal.faint
				if selected {
					color, detailColor = state.pal.onAccent, state.pal.onAccent
				}
				row := f.Row().Height(30).PadX(10).Gap(12).Items(ui.Center).Shrink(0)
				if selected {
					row.Background(state.pal.accent).Radius(radius.item)
				}
				// The rows touch through the gap between them, so crossing it never
				// takes the pointer off the list and flashes the keyboard's row.
				row.HitOutset(1, 0, 1, 0).HitWith(id, entry.Command, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {
					f.Text(entry.Command).FontSize(metrics.FontSizeM).Color(color).Ellipsis().Grow(1)
					if entry.Count > 1 {
						f.Text(fmt.Sprintf(m.UsedFmt, entry.Count)).FontSize(metrics.FontSizeS).Color(detailColor).Shrink(0)
					}
				})
			}
		})
	})
	f.End()
	components.PaintInput(canvas, state.Theme(), search.Rect(), components.InputSnapshot{
		ID: hitHistorySearch, Label: m.Title, Value: value,
		Placeholder: m.Placeholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	options := [...]components.SegmentOption{
		{ID: hitHistoryScopeHost, Label: m.ScopeHost},
		{ID: hitHistoryScopeAll, Label: m.ScopeAll},
	}
	selectedScope := options[0].ID
	if h.all {
		selectedScope = options[1].ID
	}
	rect := scope.Rect()
	rect.Height = 30
	components.BeginSegmentedControl(canvas, state.Theme(), rect, options[:], selectedScope).Paint()
	dialog.End()
}
