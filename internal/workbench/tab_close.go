package workbench

import (
	"fmt"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabsClose is a bulk close waiting for the user's answer: the tab that stays,
// and which of the others go, as the tab menu chose them.
type tabsClose struct {
	keep  layoutcore.WorkspaceID
	pick  func(position int) bool
	count int
}

// askCloseTabs closes the tabs pick selects. Closing one tab is as small as
// pressing its own close button, so it just happens; two or more ask first,
// because it ends several sessions at once.
func (state *State) askCloseTabs(keep layoutcore.WorkspaceID, pick func(position int) bool) {
	count := 0
	for position := range state.workspaceTabs {
		if pick(position) {
			count++
		}
	}
	if count < 2 {
		state.closeTabs(keep, pick)
		return
	}
	state.tabsClose = tabsClose{keep: keep, pick: pick, count: count}
	state.modal = modalCloseTabs
}

func (state *State) clickCloseTabsDialog(id ui.ID) {
	switch id {
	case hitTabsCloseDismiss, hitTabsCloseClose, hitTabsCloseCancel:
		state.modal = modalNone
		state.tabsClose = tabsClose{}
	case hitTabsCloseConfirm:
		pending := state.tabsClose
		state.modal = modalNone
		state.tabsClose = tabsClose{}
		state.closeTabs(pending.keep, pending.pick)
	}
}

// closeTabsKey makes Enter on the confirm button confirm.
func (state *State) closeTabsKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalCloseTabs || event.Key != ui.KeyEnter || event.Target != hitTabsCloseConfirm || event.Repeat {
		return ui.EventResult{}, false
	}
	state.clickCloseTabsDialog(hitTabsCloseConfirm)
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) paintCloseTabsDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages := state.messages
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitTabsCloseDismiss, CloseID: hitTabsCloseClose, InitialFocusID: hitTabsCloseCancel,
		ContentSize: ui.Size{Width: 520}, FitBody: true,
	})
	layout := dialog.Begin(fmt.Sprintf(messages.CloseTabsTitleFmt, state.tabsClose.count), messages.CloseTabsDescription)
	dialog.Actions(layout,
		components.ButtonAction{ID: hitTabsCloseCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitTabsCloseConfirm, Label: messages.CloseTabsConfirm, Variant: components.ButtonDestructive},
	)
	dialog.End()
}
