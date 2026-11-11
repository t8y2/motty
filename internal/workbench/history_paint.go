package workbench

import (
	"fmt"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/ui"
)

// declareHistory declares the command-history picker in the workbench page: its
// search field, its host/all switch and its rows are page elements, so the page
// owns their pointer and keyboard. The dialog is an overlay of the same page, as
// the command palette is.
func (state *State) declareHistory(c ui.C) {
	if state.modal != modalHistory {
		return
	}
	state.rankHistory()
	m := state.messages.History
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.Title, Width: 680, Height: 480,
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitHistoryDismiss, CloseID: hitHistoryClose,
		InitialFocusID: hitHistorySearch,
	})
	dialog.Body(func() { state.declareHistoryBody(c) })
	if dialog.Dismissed() {
		state.closeHistoryPicker()
	}
}

// declareHistoryBody declares the picker: the search field and the host/all
// switch over the visible results, with the hint under them.
func (state *State) declareHistoryBody(c ui.C) {
	h := &state.history
	m := state.messages.History
	ui.Column(c).Fill().Children(func() {
		ui.Row(c).Height(36).Gap(8).Shrink(0).Children(func() {
			ui.TextFieldID(c, hitHistorySearch, &h.field, m.Title, m.Placeholder).
				Grow(1).Height(36).NavigationKeys()
			ui.Slot(c).Width(210).Height(36).Self(ui.Center).Children(func() {
				if ui.Choice(c, &h.all, historyScopeChoices[:], state.historyScopeLabel).Fill().Changed() {
					// The switch reloads the scope as it is declared, so read the
					// rows after it, not before.
					state.loadHistory()
					state.rankHistory()
				}
			})
		})
		count := min(len(h.ranked)-h.top, historyVisibleRows)
		count = max(count, 0)
		// The pointer on a row makes it the current one, so the row the keyboard
		// chose does not stay lit beside it.
		current := -1
		if canvas := c.Canvas(); canvas != nil {
			for slot := 0; slot < count; slot++ {
				if canvas.Hovered() == hitHistoryItems.At(slot) {
					current = h.top + slot
				}
			}
		}
		empty := m.Empty
		switch {
		case !h.on && len(h.entries) == 0:
			empty = m.EmptyOff
		case len(h.entries) == 0:
			empty = m.EmptyNone
		}
		// The hint sits under the results, where it does not push them down.
		defer func() {
			ui.Text(c, m.Hint).FontSize(metrics.FontSizeS).Color(state.pal.faint).LineHeight(18).Ellipsis().Shrink(0)
		}()
		if count == 0 {
			ui.Text(c, empty).FontSize(metrics.FontSizeM).Color(state.pal.muted).LineHeight(20).TextCenter().MarginSides(36, 0, 0, 0).Grow(1)
			return
		}
		ui.Column(c).Gap(2).MarginSides(10, 0, 0, 0).Grow(1).Children(func() {
			for slot := 0; slot < count; slot++ {
				index := h.top + slot
				entry := h.entries[h.ranked[index]]
				id := hitHistoryItems.At(slot)
				selected := current == index || current < 0 && h.selected == index
				color, detailColor := state.pal.text, state.pal.faint
				if selected {
					color, detailColor = state.pal.onAccent, state.pal.onAccent
				}
				row := ui.Row(c).Height(30).PadX(10).Gap(12).Items(ui.Center).Shrink(0)
				if selected {
					row.Background(state.pal.accent).Radius(radius.item)
				}
				// The rows touch through the gap between them, so crossing it never
				// takes the pointer off the list and flashes the keyboard's row.
				row.HitOutset(1, 0, 1, 0).HitWith(id, entry.Command, gfx.HitOptions{Role: gfx.RoleButton}).Children(func() {
					ui.Text(c, entry.Command).FontSize(metrics.FontSizeM).Color(color).Ellipsis().Grow(1)
					if entry.Count > 1 {
						ui.Text(c, fmt.Sprintf(m.UsedFmt, entry.Count)).FontSize(metrics.FontSizeS).Color(detailColor).Shrink(0)
					}
				})
			}
		})
	})
}

// historyScopeChoices are the picker's switch values: this host, or every host.
var historyScopeChoices = [...]bool{false, true}

func (state *State) historyScopeLabel(all bool) string {
	if all {
		return state.messages.History.ScopeAll
	}
	return state.messages.History.ScopeHost
}
