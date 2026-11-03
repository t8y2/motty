package workbench

import (
	"errors"
	"fmt"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/flex"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

var (
	hitWorkspaceSaveDismiss = tree.ID(workspaceHits, "save-dismiss")
	hitWorkspaceSaveClose   = tree.ID(workspaceHits, "save-close")
	hitWorkspaceSaveCancel  = tree.ID(workspaceHits, "save-cancel")
	hitWorkspaceSaveSubmit  = tree.ID(workspaceHits, "save-submit")
	hitWorkspaceSaveName    = tree.ID(workspaceHits, "save-name")
)

// workspaceSave is the small dialog that turns one running temporary workspace
// into a reusable template. The workspace remains open after saving.
type workspaceSave struct {
	workspace layoutcore.WorkspaceID
	columns   uint8
	name      text.Editor
	error     string
}

func (state *State) openWorkspaceSave(workspace layoutcore.WorkspaceID) {
	columns, temporary := state.temporaryWorkspaceColumns[workspace]
	if !temporary {
		return
	}
	index, ok := state.workspaceTab(workspace)
	if !ok {
		return
	}
	state.workspaceSave = workspaceSave{workspace: workspace, columns: columns}
	state.workspaceSave.name.Insert(state.tabLabel(state.workspaceTabs[index]))
	state.workspaceSave.name.SetSelection(text.Range{End: state.workspaceSave.name.RuneCount()})
	state.modal = modalWorkspaceSave
}

func (state *State) clickWorkspaceSave(id ui.ID) {
	switch id {
	case hitWorkspaceSaveDismiss, hitWorkspaceSaveClose, hitWorkspaceSaveCancel:
		state.closeModal()
	case hitWorkspaceSaveSubmit:
		state.submitWorkspaceSave()
	}
}

// submitWorkspaceSave records only terminal references, their current visual
// order, and the temporary workspace's launch columns.
func (state *State) submitWorkspaceSave() {
	save := &state.workspaceSave
	hostIDs, err := state.workspaceSaveHostCatalogIDs(save.workspace)
	if err != nil {
		save.error = state.workspaceSaveCompositionError(err)
		return
	}
	template, err := NewWorkspaceTemplate(strings.TrimSpace(save.name.String()), hostIDs, save.columns)
	if err != nil {
		save.error = state.workspaceSaveTemplateError(err)
		return
	}
	previousTemplates := state.templates
	previousSelected := state.selectedWorkspaceTemplateID
	previousExpanded := state.workspaceLibraryExpanded
	state.templates = append(state.templates, template)
	state.selectedWorkspaceTemplateID = template.ID
	state.workspaceLibraryExpanded = true
	delete(state.temporaryWorkspaceColumns, save.workspace)
	previousBroadcast := state.broadcast
	previousStatus := state.status
	state.clearBroadcast()
	state.status = state.messages.Tools.StatusBroadcastOff
	previousSection := state.activeSection
	state.activeSection = sectionWorkspaces
	if !state.saveResult() {
		state.templates = previousTemplates
		state.selectedWorkspaceTemplateID = previousSelected
		state.workspaceLibraryExpanded = previousExpanded
		state.temporaryWorkspaceColumns[save.workspace] = save.columns
		state.broadcast = previousBroadcast
		state.status = previousStatus
		state.activeSection = previousSection
		save.error = state.messages.WorkspaceSaveFailed
		return
	}
	if session, ok := state.sessionByIDPointer(state.activeSessionID); ok {
		state.releaseIdleSFTP(session)
	}
	state.closeModal()
	state.showActionInfo(
		state.messages.WorkspaceLibraryTitle,
		fmt.Sprintf(state.messages.WorkspaceDraftSavedFmt, template.Name),
	)
}

func (state *State) workspaceSaveCompositionError(err error) string {
	switch {
	case errors.Is(err, ErrWorkspaceSaveEmptyPane):
		return state.messages.WorkspaceDraftEmptyPane
	case errors.Is(err, ErrWorkspaceSaveMissingSession):
		return state.messages.WorkspaceDraftMissingSession
	case errors.Is(err, ErrWorkspaceSaveMissingHost):
		return state.messages.WorkspaceDraftMissingHost
	case errors.Is(err, ErrWorkspaceSaveDuplicateHost):
		return state.messages.WorkspaceDraftDuplicateHost
	default:
		return state.messages.WorkspaceSaveFailed
	}
}

func (state *State) workspaceSaveTemplateError(err error) string {
	switch {
	case errors.Is(err, ErrWorkspaceTemplateName):
		return state.messages.WorkspaceDraftNameRequired
	case errors.Is(err, ErrWorkspaceTemplateDupID):
		return state.messages.WorkspaceDraftDuplicateHost
	case errors.Is(err, ErrWorkspaceTemplateHosts), errors.Is(err, ErrWorkspaceTemplateHostID):
		return state.messages.WorkspaceDraftHostsRequired
	default:
		return state.messages.WorkspaceSaveFailed
	}
}

func (state *State) workspaceSaveHostCatalogIDs(workspace layoutcore.WorkspaceID) ([]string, error) {
	hostIDs := make([]string, 0, maxWorkspaceTemplateHosts)
	seen := make(map[string]struct{}, maxWorkspaceTemplateHosts)
	for _, pane := range state.deck.Panes(workspace, nil) {
		if pane.SessionID == 0 {
			return nil, ErrWorkspaceSaveEmptyPane
		}
		session, ok := state.sessionByIDPointer(uint32(pane.SessionID))
		if !ok {
			return nil, ErrWorkspaceSaveMissingSession
		}
		hostID := workspaceHostReference(session.host)
		if hostID == "" {
			return nil, ErrWorkspaceSaveMissingHost
		}
		if _, duplicate := seen[hostID]; duplicate {
			return nil, ErrWorkspaceSaveDuplicateHost
		}
		seen[hostID] = struct{}{}
		hostIDs = append(hostIDs, hostID)
	}
	if len(hostIDs) == 0 {
		return nil, ErrWorkspaceTemplateHosts
	}
	if len(hostIDs) > maxWorkspaceTemplateHosts {
		return nil, ErrWorkspaceTemplateHosts
	}
	return hostIDs, nil
}

func (state *State) paintWorkspaceSaveDialog(canvas *ui.Canvas, viewport ui.Rect) {
	messages := state.messages
	save := &state.workspaceSave
	dialog := components.BeginDialog(canvas, state.Theme(), viewport, components.DialogOptions{
		Layer: dialogLayer, Scope: dialogScope,
		DismissID: hitWorkspaceSaveDismiss, CloseID: hitWorkspaceSaveClose, InitialFocusID: hitWorkspaceSaveName,
		ContentSize: ui.Size{Width: 500}, FitBody: true, BodyHeight: 92,
	})
	description := ""
	if index, ok := state.workspaceTab(save.workspace); ok {
		description = fmt.Sprintf(messages.WorkspaceSaveDescriptionFmt, state.tabLabel(state.workspaceTabs[index]),
			len(state.deck.Panes(save.workspace, nil)))
	}
	layout := dialog.Begin(messages.WorkspaceSaveTitle, description)
	var field flex.Box
	f := state.chrome.dialog.Begin(canvas, state.Theme(), layout.Body)
	f.Column().Fill().Children(func() { field = f.Slot().Height(62) })
	f.End()
	beginField := components.BeginField(canvas, state.Theme(), field.Rect())
	if save.error != "" {
		beginField = beginField.Invalid()
	}
	content := beginField.Paint(messages.WorkspaceDraftName, "", save.error)
	value := save.name.String()
	focused := canvas.Focused() == hitWorkspaceSaveName
	content.Height = 32
	components.PaintInput(canvas, state.Theme(), content, components.InputSnapshot{
		ID: hitWorkspaceSaveName, Label: messages.WorkspaceDraftName, Value: value,
		Placeholder: messages.WorkspaceDraftNamePlaceholder, Focused: focused, PlatformEditing: focused && value != "",
	})
	dialog.Actions(layout,
		components.ButtonAction{ID: hitWorkspaceSaveCancel, Label: messages.Cancel, Variant: components.ButtonSecondary},
		components.ButtonAction{ID: hitWorkspaceSaveSubmit, Label: messages.WorkspaceDraftSave},
	)
	dialog.End()
}

// workspaceSaveKey keeps the save dialog's keyboard behavior local to it:
// Escape closes, and Enter submits from either the name field or the button.
func (state *State) workspaceSaveKey(event ui.KeyEvent) (ui.EventResult, bool) {
	if state.modal != modalWorkspaceSave {
		return ui.EventResult{}, false
	}
	if event.Key == ui.KeyEscape && !event.Repeat {
		state.closeModal()
		return ui.EventResult{Handled: true, Repaint: true}, true
	}
	if event.Key != ui.KeyEnter || event.Repeat ||
		(event.Target != hitWorkspaceSaveName && event.Target != hitWorkspaceSaveSubmit) {
		return ui.EventResult{}, false
	}
	if editor := state.EditorFor(event.Target); editor != nil {
		if _, composing := editor.Composition(); composing {
			return ui.EventResult{}, false
		}
	}
	state.submitWorkspaceSave()
	return ui.EventResult{Handled: true, Repaint: true}, true
}
