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
	// form and input are the dialog's field: its labelled row and its text,
	// bound to editor when first declared.
	form  ui.FormFieldState
	input ui.Field
	bound bool
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

func (state *State) declareAIRenameDialog(c ui.C) {
	m := state.messages.AIPanel
	// No scrim dismiss: a press beside the form must not discard its input.
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, "", dialogRowHeight(c.Theme()))
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.RenameTitle, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitAIRenameClose, InitialFocusID: hitAIRenameField,
	})
	dialog.Body(func() {
		rename := &state.ai.rename
		if !rename.bound {
			rename.input.Bind(&rename.editor)
			rename.bound = true
		}
		rename.form.For, rename.form.Error, rename.form.Invalid = hitAIRenameField, state.dialogError, state.dialogError != ""
		ui.FormField(c, &rename.form, m.RenameField).FillWidth().Height(dialogRowHeight(c.Theme())).Children(func() {
			ui.TextFieldID(c, hitAIRenameField, &rename.input, m.RenameField, m.RenamePlaceholder)
		})
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitAIRenameCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitAIRenameSubmit, Label: m.RenameSubmit},
		)
	})
}
