package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// tabRename is the terminal-rename dialog's state: which session of a split
// tab, and the name being typed. A tab itself is renamed in place on its tab;
// see tab_inline.go.
type tabRename struct {
	// session names the one terminal of a split tab whose name is edited. The
	// name is what the assistant's target list and @ mentions show.
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

// applyTabRename gives a tab a name its user chose: the deck remembers it, a
// tab with a single pane carries the name onto its session, and the choice is
// saved. A nil return leaves the tab unchanged, as callers already expect of
// deck.Rename.
func (state *State) applyTabRename(workspace layoutcore.WorkspaceID, name string) {
	if state.deck.Rename(workspace, name) == nil {
		_ = state.deck.SetNamed(workspace, true)
		panes := state.deck.Panes(workspace, nil)
		if len(panes) == 1 {
			if session, ok := state.sessionByIDPointer(uint32(panes[0].SessionID)); ok {
				state.renameSession(session, name)
			}
		}
		state.rebuildTabOptions()
		state.save()
	}
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
		if session, ok := state.sessionByIDPointer(state.tabRename.session); ok {
			state.renameSession(session, name)
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
	dialog := ui.Dialog(c, ui.DialogSpec{
		Title: messages.RenameTerminalTitle, Width: 460, Height: height, Layer: dialogLayer,
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
		Placeholder: messages.RenameTerminalPlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
}
