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
	// field is the slot the dialog's field is painted into, and draw the painter
	// kept so a repaint declares no closure.
	field ui.Box
	draw  func(*ui.DrawContext)
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
	ai.rename.draw = state.drawAIRenameField
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
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, "", 62)
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: m.RenameTitle, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitAIRenameClose, InitialFocusID: hitAIRenameField,
	})
	dialog.Body(func() {
		column := ui.Column(c).Fill()
		column.Children(func() {
			state.ai.rename.field = ui.Slot(c).Height(62)
		})
		if state.ai.rename.draw == nil {
			state.ai.rename.draw = state.drawAIRenameField
		}
		column.DrawOver(state.ai.rename.draw)
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitAIRenameCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitAIRenameSubmit, Label: m.RenameSubmit},
		)
	})
}

// drawAIRenameField paints the rename dialog's field into the slot the body
// laid out.
func (state *State) drawAIRenameField(d *ui.DrawContext) {
	m := state.messages.AIPanel
	field := components.BeginField(d.Canvas, *d.Theme, state.ai.rename.field.Rect())
	if state.dialogError != "" {
		field = field.Invalid()
	}
	content := field.Paint(m.RenameField, "", state.dialogError)
	value := state.ai.rename.editor.String()
	focused := d.Canvas.Focused() == hitAIRenameField
	content.Height = 32
	components.PaintInput(d.Canvas, *d.Theme, content, components.InputSnapshot{
		ID: hitAIRenameField, Label: m.RenameField, Value: value,
		Placeholder: m.RenamePlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
}
