package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabRename is the rename dialog's state: which tab, and the name being typed.
type tabRename struct {
	workspace layoutcore.WorkspaceID
	// session, when set, renames that one terminal of a split tab instead of the
	// tab. The name is what the assistant's target list and @ mentions show.
	session uint32
	editor  text.Editor
	// field is the slot the dialog's field is painted into, and draw the painter
	// kept so a repaint declares no closure.
	field ui.Box
	draw  func(*ui.DrawContext)
}

// hitPaneRename renames the terminal that has the keyboard, from the command
// palette. Double clicking a split pane's bar reaches the same dialog.
var hitPaneRename = tree.ID(shellHits, "pane-rename")

// openTerminalRename asks for a new name for one terminal of a split tab.
func (state *State) openTerminalRename(session *Session) {
	state.tabRename = tabRename{session: session.ID}
	state.tabRename.draw = state.drawTabRenameField
	state.tabRename.editor.Insert(session.Title)
	state.tabRename.editor.SetSelection(text.Range{End: state.tabRename.editor.RuneCount()})
	state.dialogError = ""
	state.modal = modalRenameTab
}

// renameActiveTerminal asks for a new name for the terminal that has the keyboard.
func (state *State) renameActiveTerminal() {
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		state.openTerminalRename(session)
	}
}

// openTabRename asks for a new name for a tab, starting from its current one.
func (state *State) openTabRename(workspace layoutcore.WorkspaceID) {
	index, ok := state.workspaceTab(workspace)
	if !ok {
		return
	}
	state.tabRename = tabRename{workspace: workspace}
	state.tabRename.draw = state.drawTabRenameField
	state.tabRename.editor.Insert(state.tabLabel(state.workspaceTabs[index]))
	state.tabRename.editor.SetSelection(text.Range{End: state.tabRename.editor.RuneCount()})
	state.dialogError = ""
	state.modal = modalRenameTab
}

func (state *State) clickTabRenameDialog(id gfx.ID) {
	switch id {
	case hitTabRenameClose, hitTabRenameCancel:
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
		if id := state.tabRename.session; id != 0 {
			if session, ok := state.sessionByIDPointer(id); ok {
				state.renameSession(session, name)
				state.rebuildTabOptions()
				state.save()
			}
			return
		}
		if state.deck.Rename(state.tabRename.workspace, name) == nil {
			_ = state.deck.SetNamed(state.tabRename.workspace, true)
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
func (state *State) tabRenameKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.modal != modalRenameTab || event.Key != gfx.KeyEnter || event.Repeat {
		return gfx.EventResult{}, false
	}
	if event.Target != hitTabRenameField && event.Target != hitTabRenameSubmit {
		return gfx.EventResult{}, false
	}
	state.clickTabRenameDialog(hitTabRenameSubmit)
	return gfx.EventResult{Handled: true, Repaint: true}, true
}

func (state *State) declareTabRenameDialog(c ui.C) {
	messages := state.messages
	// No scrim dismiss: a press beside the form must not discard its input.
	viewport := dialogViewport(c)
	height := dialogContentHeight(c.Canvas(), *c.Theme(), viewport, 460, "", 62)
	title := messages.RenameTabTitle
	if state.tabRename.session != 0 {
		title = messages.RenameTerminalTitle
	}
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: title, Width: 460, Height: height, Layer: dialogLayer,
		Scope: dialogScope, CloseID: hitTabRenameClose, InitialFocusID: hitTabRenameField,
	})
	dialog.Body(func() {
		column := ui.Column(c).Fill()
		column.Children(func() {
			state.tabRename.field = ui.Slot(c).Height(62)
		})
		if state.tabRename.draw == nil {
			state.tabRename.draw = state.drawTabRenameField
		}
		column.DrawOver(state.tabRename.draw)
	})
	dialog.Footer(func() {
		state.dialogActionFooter(c,
			components.ButtonAction{ID: hitTabRenameCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
			components.ButtonAction{ID: hitTabRenameSubmit, Label: messages.RenameTabSubmit},
		)
	})
}

// drawTabRenameField paints the rename dialog's field into the slot the body
// laid out.
func (state *State) drawTabRenameField(d *ui.DrawContext) {
	messages := state.messages
	placeholder := messages.RenameTabPlaceholder
	if state.tabRename.session != 0 {
		placeholder = messages.RenameTerminalPlaceholder
	}
	field := components.BeginField(d.Canvas, *d.Theme, state.tabRename.field.Rect())
	if state.dialogError != "" {
		field = field.Invalid()
	}
	content := field.Paint(messages.RenameTabField, "", state.dialogError)
	value := state.tabRename.editor.String()
	focused := d.Canvas.Focused() == hitTabRenameField
	content.Height = 32
	components.PaintInput(d.Canvas, *d.Theme, content, components.InputSnapshot{
		ID: hitTabRenameField, Label: messages.RenameTabField, Value: value,
		Placeholder: placeholder, Focused: focused, PlatformEditing: focused && value != "",
	})
}
