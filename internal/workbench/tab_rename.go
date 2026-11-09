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
}

// hitPaneRename renames the terminal that has the keyboard, from the command
// palette. Double clicking a split pane's bar reaches the same dialog.
var hitPaneRename = tree.ID(shellHits, "pane-rename")

// openTerminalRename asks for a new name for one terminal of a split tab.
func (state *State) openTerminalRename(session *Session) {
	state.tabRename = tabRename{session: session.ID}
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

func (state *State) paintTabRenameDialog(canvas *gfx.Canvas, viewport gfx.Rect) {
	messages := state.messages
	// No scrim dismiss: a press beside the form must not discard its input.
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		CloseID: hitTabRenameClose, InitialFocusID: hitTabRenameField,
		ContentSize: gfx.Size{Width: 460}, FitBody: true, BodyHeight: 62,
	})
	title, placeholder := messages.RenameTabTitle, messages.RenameTabPlaceholder
	if state.tabRename.session != 0 {
		title, placeholder = messages.RenameTerminalTitle, messages.RenameTerminalPlaceholder
	}
	layout := dialog.Begin(title, "")
	var slot ui.Box
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
		Placeholder: placeholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitTabRenameCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitTabRenameSubmit, Label: messages.RenameTabSubmit},
	)
	dialog.End()
}
