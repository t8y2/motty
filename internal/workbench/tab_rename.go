package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabRename is the rename dialog's state: which tab, and the name being typed.
type tabRename struct {
	workspace layoutcore.WorkspaceID
	editor    text.Editor
}

// openTabRename asks for a new name for a tab, starting from its current one.
func (state *State) openTabRename(workspace layoutcore.WorkspaceID) {
	index, ok := state.workspaceTab(workspace)
	if !ok {
		return
	}
	state.tabRename = tabRename{workspace: workspace}
	state.tabRename.editor.Insert(state.workspaceTabs[index].Title)
	state.tabRename.editor.SetSelection(text.Range{End: state.tabRename.editor.RuneCount()})
	state.dialogError = ""
	state.modal = modalRenameTab
}

func (state *State) clickTabRenameDialog(id ui.ID) {
	switch id {
	case hitTabRenameDismiss, hitTabRenameClose, hitTabRenameCancel:
		state.modal = modalNone
		state.dialogError = ""
	case hitTabRenameSubmit:
		name := strings.TrimSpace(state.tabRename.editor.String())
		if name == "" {
			state.dialogError = state.messages.ErrTabNameRequired
			return
		}
		state.modal = modalNone
		state.dialogError = ""
		if state.deck.Rename(state.tabRename.workspace, name) == nil {
			panes := state.deck.Panes(state.tabRename.workspace, nil)
			if len(panes) == 1 {
				if session, ok := state.sessionByIDPointer(uint32(panes[0].SessionID)); ok {
					state.renameSession(session, name)
				}
			}
			state.rebuildTabOptions()
			state.save()
		}
	}
}

// tabRenameKey makes Enter in the field, or on the button, submit.
func (state *State) tabRenameKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalRenameTab || event.Key != ui.KeyEnter || event.Repeat {
		return ui.EventResult{}, false
	}
	if event.Target != hitTabRenameField && event.Target != hitTabRenameSubmit {
		return ui.EventResult{}, false
	}
	state.clickTabRenameDialog(hitTabRenameSubmit)
	return ui.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) paintTabRenameDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages := state.messages
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitTabRenameDismiss, CloseID: hitTabRenameClose, InitialFocusID: hitTabRenameField,
		ContentSize: ui.Size{Width: 460}, FitBody: true, BodyHeight: 62,
	})
	layout := dialog.Begin(messages.RenameTabTitle, "")
	var slot flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() { slot = f.Slot().Height(62) })
	f.End()
	field := components.BeginField(canvas, state.Theme(), slot.Rect())
	if state.dialogError != "" {
		field = field.Invalid()
	}
	content := field.Paint(messages.RenameTabField, "", state.dialogError)
	value := state.tabRename.editor.String()
	focused := canvas.Focused() == hitTabRenameField
	content.Height = 32
	components.PaintInput(canvas, state.Theme(), content, components.InputSnapshot{
		ID: hitTabRenameField, Label: messages.RenameTabField, Value: value,
		Placeholder: messages.RenameTabPlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitTabRenameCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitTabRenameSubmit, Label: messages.RenameTabSubmit},
	)
	dialog.End()
}
