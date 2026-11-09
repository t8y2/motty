package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
)

// aiRenameDialog is the assistant's conversation-rename dialog: which saved
// conversation, and the title being typed.
type aiRenameDialog struct {
	id     int64
	editor text.Editor
}

// openAIRename asks for a new title for a saved conversation, starting from
// its current one.
func (state *State) openAIRename(index int) {
	ai := &state.ai
	if index < 0 || index >= len(ai.conversations) {
		return
	}
	conversation := ai.conversations[index]
	ai.rename = aiRenameDialog{id: conversation.ID}
	ai.rename.editor.Insert(conversation.Title)
	ai.rename.editor.SetSelection(text.Range{End: ai.rename.editor.RuneCount()})
	state.dialogError = ""
	state.modal = modalAIRename
}

func (state *State) clickAIRenameDialog(id gfx.ID) {
	switch id {
	case hitAIRenameClose, hitAIRenameCancel:
		state.modal = modalNone
		state.dialogError = ""
	case hitAIRenameSubmit:
		title := strings.TrimSpace(state.ai.rename.editor.String())
		if title == "" {
			state.dialogError = state.messages.AIPanel.RenameRequired
			return
		}
		st, err := state.aiStore()
		if err != nil {
			state.dialogError = err.Error()
			return
		}
		if err := st.RenameConversation(state.ai.rename.id, title); err != nil {
			state.dialogError = err.Error()
			return
		}
		state.modal = modalNone
		state.dialogError = ""
		state.aiPanelLoadHistory()
	}
}

// aiRenameKey makes Enter in the field, or on the button, submit.
func (state *State) aiRenameKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalAIRename || event.Key != gfx.KeyEnter || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Target != hitAIRenameField && event.Target != hitAIRenameSubmit {
		return gfx.EventResult{}, false
	}
	state.clickAIRenameDialog(hitAIRenameSubmit)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) paintAIRenameDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	m := state.messages.AIPanel
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitAIRenameClose, InitialFocusID: hitAIRenameField,
		ContentSize: gfx.Size{Width: 460}, FitBody: true, BodyHeight: 62,
	})
	layout := dialog.Begin(m.RenameTitle, "")
	var slot ui.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() { slot = f.Slot().Height(62) })
	f.End()
	field := components.BeginField(canvas, state.Theme(), slot.Rect())
	if state.dialogError != "" {
		field = field.Invalid()
	}
	content := field.Paint(m.RenameField, "", state.dialogError)
	value := state.ai.rename.editor.String()
	focused := canvas.Focused() == hitAIRenameField
	content.Height = 32
	components.PaintInput(canvas, state.Theme(), content, components.InputSnapshot{
		ID: hitAIRenameField, Label: m.RenameField, Value: value,
		Placeholder: m.RenamePlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitAIRenameCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitAIRenameSubmit, Label: m.RenameSubmit},
	)
	dialog.End()
}
