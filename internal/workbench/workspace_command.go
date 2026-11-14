package workbench

import (
	"strings"

	"github.com/vivaui/viva/components"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

var (
	hitWorkspaceCommandEdit   = tree.Sub(workspaceHits, tree.Name("command-edit"))
	hitWorkspaceCommandField  = tree.Sub(workspaceHits, tree.Name("command-field"))
	hitWorkspaceCommandRun    = tree.Sub(workspaceHits, tree.Name("command-run"))
	hitWorkspaceCommandCancel = tree.Sub(workspaceHits, tree.Name("command-cancel"))
)

// Local terminals also use the zero-valued SSH protocol; exclude them explicitly.
func workspaceCommandHost(host Host) bool {
	return host.ID != localHostID && host.isSSH() && !host.mosh()
}

func (draft *workspaceDraft) commandEditor(id string) *text.Editor {
	if draft.commandEditors == nil {
		draft.commandEditors = make(map[string]*text.Editor)
	}
	if draft.commandEditors[id] == nil {
		draft.commandEditors[id] = text.NewEditor("")
	}
	return draft.commandEditors[id]
}

func (state *State) workspaceDraftCommands() (map[string]WorkspaceCommand, bool) {
	draft := &state.workspaceDraft
	var commands map[string]WorkspaceCommand
	for id, editor := range draft.commandEditors {
		if !draft.selected[id] || id == workspaceLocalTerminalRef {
			continue
		}
		if host, found := state.workspaceHostByID(id); found && !workspaceCommandHost(host) {
			continue
		}
		command := strings.TrimSpace(editor.String())
		if !validLoginCommand(command) {
			return nil, false
		}
		if command != "" {
			if commands == nil {
				commands = make(map[string]WorkspaceCommand)
			}
			commands[id] = WorkspaceCommand{Command: command}
		}
	}
	return commands, true
}

func (state *State) paintWorkspaceCommandFields(canvas *gfx.Canvas, host Host, bounds gfx.Rect) {
	draft, m := &state.workspaceDraft, state.messages
	editor := draft.commandEditor(host.CatalogID)
	id := hitWorkspaceCommandField.At(int(host.ID))
	monitorLabel(canvas, elide(canvas, m.WorkspaceCommandOrderHint, 12, gfx.WeightRegular, bounds.Width), gfx.Rect{X: bounds.X, Y: bounds.Y, Width: bounds.Width, Height: 22}, 12, state.pal.text, gfx.WeightRegular)
	control := gfx.Rect{X: bounds.X, Y: bounds.Y + 26, Width: bounds.Width, Height: 32}
	components.PaintInput(canvas, state.Theme(), control, components.InputSnapshot{ID: id, Label: m.WorkspaceCommandField, Value: editor.String(), Placeholder: m.Tools.LoginCommandPlaceholder, Focused: canvas.Focused() == id, PlatformEditing: canvas.Focused() == id && editor.String() != ""})
}

func workspaceCommandPending(session *Session) bool {
	return session != nil && session.term != nil && session.term.shell != nil && !session.term.ended && session.term.login.workspace != ""
}

// Keep equal chrome in a split, so switching focus does not resize a PTY.
func (state *State) workspaceCommandNoticeInWorkspace(session *Session) bool {
	if workspaceCommandPending(session) {
		return true
	}
	owner, _, ok := state.deck.PaneForSession(layoutcore.SessionID(session.ID))
	if !ok {
		return false
	}
	for i := range state.sessions {
		other := &state.sessions[i]
		workspace, _, found := state.deck.PaneForSession(layoutcore.SessionID(other.ID))
		if found && workspace == owner && workspaceCommandPending(other) {
			return true
		}
	}
	return false
}

func (state *State) paintWorkspaceCommandNotice(canvas *gfx.Canvas, session *Session, bounds gfx.Rect) {
	if !workspaceCommandPending(session) {
		return
	}
	m := state.messages
	canvas.Fill(bounds, state.pal.surface)
	buttons := min(float32(144), bounds.Width*.55)
	label := gfx.Rect{X: bounds.X + 8, Y: bounds.Y + 8, Width: max(0, bounds.Width-buttons-20), Height: 22}
	monitorLabel(canvas, elide(canvas, m.WorkspaceCommandPending, 12, gfx.WeightRegular, label.Width), label, 12, state.pal.muted, gfx.WeightRegular)
	components.BeginButtonRow(canvas, state.Theme(), gfx.Rect{X: bounds.X + bounds.Width - buttons - 4, Y: bounds.Y + 5, Width: buttons, Height: 26}).Size(components.ButtonSizeXSmall).Paint(
		components.ButtonAction{ID: hitWorkspaceCommandRun.At(int(session.ID)), Label: m.WorkspaceCommandRun, Variant: components.ButtonOutline},
		components.ButtonAction{ID: hitWorkspaceCommandCancel.At(int(session.ID)), Label: m.WorkspaceCommandCancel, Variant: components.ButtonSecondary},
	)
}

func (state *State) clickWorkspaceCommand(id gfx.ID) bool {
	run, ok := id.In(hitWorkspaceCommandRun)
	if !ok {
		run, ok = id.In(hitWorkspaceCommandCancel)
	}
	if !ok {
		return false
	}
	session, found := state.sessionByIDPointer(uint32(run))
	if !found || !workspaceCommandPending(session) {
		return true
	}
	if id.Scope() == hitWorkspaceCommandCancel {
		session.term.login = loginCommand{}
	} else {
		// Drain output first: it may have ended this shell or started a reconnect.
		t := session.term
		token := t.folderToken
		if t.takeOutput != nil {
			t.takeOutput()
		}
		if session.term == t && t.folderToken == token && workspaceCommandPending(session) {
			state.sendInitializationCommand(t)
		}
	}
	state.fitTerminals()
	return true
}
