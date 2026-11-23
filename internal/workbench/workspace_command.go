package workbench

import (
	"strings"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
	"github.com/vivaui/viva/ui"
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

// commandField is the page field on the command editor of one host, made once
// and bound to the editor the dialog reads.
func (draft *workspaceDraft) commandField(id string) *ui.Field {
	if draft.commandFields == nil {
		draft.commandFields = make(map[string]*ui.Field)
	}
	field := draft.commandFields[id]
	if field == nil {
		field = new(ui.Field)
		field.Bind(draft.commandEditor(id))
		draft.commandFields[id] = field
	}
	return field
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

// declareWorkspaceCommandNotice declares the pending login command's strip in
// the terminal pane: its words and the two page buttons that run or drop it.
func (state *State) declareWorkspaceCommandNotice(c ui.C, session *Session) {
	if !workspaceCommandPending(session) {
		return
	}
	m := state.messages
	ui.Row(c).Height(inputNoticeHeight).FillWidth().Shrink(0).Background(state.pal.surface).
		PadSides(8, 4, 5, 4).Gap(6).Items(ui.Center).Children(func() {
		ui.Text(c, m.WorkspaceCommandPending).FontSize(12).Color(state.pal.muted).Grow(1).Ellipsis()
		ui.Button(c, m.WorkspaceCommandRun).XSmall().Outline().
			Hit(hitWorkspaceCommandRun.At(int(session.ID)), m.WorkspaceCommandRun)
		ui.Button(c, m.WorkspaceCommandCancel).XSmall().Secondary().
			Hit(hitWorkspaceCommandCancel.At(int(session.ID)), m.WorkspaceCommandCancel)
	})
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
