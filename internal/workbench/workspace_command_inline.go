package workbench

import (
	"maps"
	"slices"
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

var (
	hitWorkspaceInlineEdit   = tree.Sub(workspaceHits, tree.Name("inline-command-edit"))
	hitWorkspaceInlineField  = tree.ID(workspaceHits, "inline-command-field")
	hitWorkspaceInlineSave   = tree.ID(workspaceHits, "inline-command-save")
	hitWorkspaceInlineCancel = tree.ID(workspaceHits, "inline-command-cancel")
	hitWorkspaceInlineBody   = tree.ID(workspaceHits, "inline-command-body")
)

type workspaceCommandInline struct {
	template, host string
	editor         text.Editor
	error          string
}

func workspaceInlineOwns(id gfx.ID) bool {
	return id == hitWorkspaceInlineField || id == hitWorkspaceInlineSave || id == hitWorkspaceInlineCancel || id == hitWorkspaceInlineBody
}

func workspaceTemplateCardHeight(template WorkspaceTemplate) float32 {
	height := workspaceCardContentHeight(len(template.HostIDs), template.Layout)
	for _, id := range template.HostIDs {
		if template.Commands[id].Command != "" {
			return height * (workspaceCardHeight + 48) / workspaceCardHeight
		}
	}
	return height
}

func (state *State) workspaceCardCommand(templateID, hostID string) WorkspaceCommand {
	for _, template := range state.templates {
		if template.ID == templateID {
			return template.Commands[hostID]
		}
	}
	return WorkspaceCommand{}
}

func (state *State) paintWorkspaceInline(canvas *gfx.Canvas, region workspaceCardRegion, hostID string, command WorkspaceCommand, bounds gfx.Rect) {
	edit := &state.workspaceCommandEdit
	if edit.template == region.template && edit.host == hostID {
		canvas.HitWithOptions(hitWorkspaceInlineBody, bounds, state.messages.WorkspaceCommandField, gfx.HitOptions{SkipTab: true})
		buttons := min(float32(116), bounds.Width*.45)
		field := bounds
		field.Width = max(0, bounds.Width-buttons-8)
		field.Height = 32
		components.PaintInput(canvas, state.Theme(), field, components.InputSnapshot{ID: hitWorkspaceInlineField, Label: state.messages.WorkspaceCommandField, Value: edit.editor.String(), Focused: canvas.Focused() == hitWorkspaceInlineField, PlatformEditing: canvas.Focused() == hitWorkspaceInlineField && edit.editor.String() != "", Invalid: edit.error != ""})
		components.BeginButtonRow(canvas, state.Theme(), gfx.Rect{X: bounds.X + bounds.Width - buttons, Y: bounds.Y + 3, Width: buttons, Height: 26}).Size(components.ButtonSizeXSmall).Paint(
			components.ButtonAction{ID: hitWorkspaceInlineSave, Label: state.messages.WorkspaceCommandSave, Variant: components.ButtonOutline},
			components.ButtonAction{ID: hitWorkspaceInlineCancel, Label: state.messages.Cancel, Variant: components.ButtonSecondary},
		)
		if edit.error != "" {
			monitorLabel(canvas, elide(canvas, edit.error, 11, gfx.WeightRegular, bounds.Width), gfx.Rect{X: bounds.X, Y: bounds.Y + 36, Width: bounds.Width, Height: 18}, 11, state.pal.danger, gfx.WeightRegular)
		}
		return
	}
	index, _ := region.id.In(hitWorkspaceTemplateCards)
	id := hitWorkspaceInlineEdit.At(index)
	canvas.HitWithOptions(id, bounds, state.messages.WorkspaceCommandField, gfx.HitOptions{Role: gfx.RoleButton, Cursor: gfx.PointerCursorHand, Hint: command.Command, Description: command.Command})
	monitorLabel(canvas, elide(canvas, command.Command, 12, gfx.WeightRegular, max(0, bounds.Width-28)), gfx.Rect{X: bounds.X, Y: bounds.Y + 4, Width: max(0, bounds.Width-28), Height: 22}, 12, state.pal.text, gfx.WeightRegular)
	canvas.DrawIcon(lucideIcons[iconPencil], gfx.Rect{X: bounds.X + bounds.Width - 20, Y: bounds.Y + 6, Width: 16, Height: 16}, state.pal.muted)
}

func (state *State) clickWorkspaceInline(id gfx.ID) bool {
	if state.modal != modalNone || state.activeSection != sectionWorkspaces {
		return false
	}
	if index, ok := id.In(hitWorkspaceInlineEdit); ok {
		templateIndex, hostIndex := index/layoutcore.MaxPanes, index%layoutcore.MaxPanes
		if templateIndex >= len(state.templates) {
			return true
		}
		template := state.templates[templateIndex]
		if hostIndex >= len(template.HostIDs) {
			return true
		}
		hostID := template.HostIDs[hostIndex]
		host, found := state.workspaceHostByID(hostID)
		command := template.Commands[hostID]
		if !found || !workspaceCommandHost(host) || command.Command == "" {
			return true
		}
		state.workspaceCommandEdit = workspaceCommandInline{template: template.ID, host: hostID, editor: *text.NewEditor(command.Command)}
		state.workspaceCommandEdit.editor.SetSelection(text.Range{End: state.workspaceCommandEdit.editor.RuneCount()})
		state.workspaceTemplateFocusPending = hitWorkspaceInlineField
		return true
	}
	switch id {
	case hitWorkspaceInlineSave:
		state.saveWorkspaceInline()
		return true
	case hitWorkspaceInlineCancel:
		state.workspaceCommandEdit = workspaceCommandInline{}
		return true
	}
	return false
}

func (state *State) saveWorkspaceInline() {
	edit := &state.workspaceCommandEdit
	if edit.template == "" {
		return
	}
	index := slices.IndexFunc(state.templates, func(t WorkspaceTemplate) bool { return t.ID == edit.template })
	host, ok := state.workspaceHostByID(edit.host)
	if index < 0 || !ok || !workspaceCommandHost(host) || !slices.Contains(state.templates[index].HostIDs, edit.host) {
		state.workspaceCommandEdit = workspaceCommandInline{}
		state.showActionWarning(state.messages.WorkspaceCommandField, state.messages.WorkspaceTemplateEditMissing)
		return
	}
	value := strings.TrimSpace(edit.editor.String())
	if !validLoginCommand(value) {
		edit.error = state.messages.WorkspaceCommandInvalid
		return
	}
	previous := state.templates
	updated := slices.Clone(previous)
	updated[index].Commands = maps.Clone(previous[index].Commands)
	command := updated[index].Commands[edit.host]
	command.Command = value
	if value == "" {
		delete(updated[index].Commands, edit.host)
	} else {
		if updated[index].Commands == nil {
			updated[index].Commands = make(map[string]WorkspaceCommand)
		}
		updated[index].Commands[edit.host] = command
	}
	if len(updated[index].Commands) == 0 {
		updated[index].Commands = nil
	}
	state.templates = updated
	if !state.saveResult() {
		state.templates = previous
		edit.error = state.messages.WorkspaceSaveFailed
		return
	}
	state.workspaceCommandEdit = workspaceCommandInline{}
}

func (state *State) workspaceInlineKey(event gfx.KeyEvent) (gfx.EventResult, bool) {
	if state.workspaceCommandEdit.template == "" || state.modal != modalNone || state.activeSection != sectionWorkspaces || event.Release || event.Repeat {
		return gfx.EventResult{}, false
	}
	switch event.Key {
	case gfx.KeyEscape:
		state.workspaceCommandEdit = workspaceCommandInline{}
	case gfx.KeyEnter:
		if !workspaceInlineOwns(event.Target) {
			return gfx.EventResult{}, false
		}
		if _, composing := state.workspaceCommandEdit.editor.Composition(); composing {
			return gfx.EventResult{}, false
		}
		if event.Target == hitWorkspaceInlineCancel {
			state.workspaceCommandEdit = workspaceCommandInline{}
		} else {
			state.saveWorkspaceInline()
		}
	default:
		return gfx.EventResult{}, false
	}
	return gfx.EventResult{Handled: true, Repaint: true}, true
}
