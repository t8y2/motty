package workbench

import (
	"reflect"
	"testing"
	"time"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func workspaceCommandSession(t *testing.T, login string, command WorkspaceCommand) (*State, *Session, *fakeShell, *time.Duration) {
	t.Helper()
	state, shell, now := loginCommandSession(t, login)
	session := &state.sessions[0]
	session.workspaceCommand = command
	session.term.armInitialization(session)
	return state, session, shell, now
}

func workspaceCommandPrompt(state *State, session *Session, shell *fakeShell) {
	token := session.term.folderToken
	shell.say("\x1b]133;C;motty=" + token + "\x07\x1b]133;A;motty=" + token + "\x07$ \x1b]133;B;motty=" + token + "\x07")
	state.Wake()
}

func TestWorkspaceCommandRequiresNewPromptAfterLogin(t *testing.T) {
	state, session, shell, now := workspaceCommandSession(t, "sudo -i", WorkspaceCommand{Command: "cd /srv"})
	workspaceCommandPrompt(state, session, shell)
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("first phase: %q", shell.typed())
	}
	shell.say("Password: ")
	state.Wake()
	*now += time.Minute
	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatalf("quiet fallback leaked command: %q", shell.typed())
	}
	// The original prompt, even marked ready, cannot authorize phase two.
	session.term.promptReady = true
	state.Wake()
	if shell.typed() != "sudo -i\r" {
		t.Fatal("old prompt authorized phase two")
	}
	workspaceCommandPrompt(state, session, shell)
	state.Wake()
	if shell.typed() != "sudo -i\rcd /srv\r" || workspaceCommandPending(session) {
		t.Fatalf("second phase: %q", shell.typed())
	}
}

func TestWorkspaceCommandPauseManualContinueAndCancel(t *testing.T) {
	for _, cancel := range []bool{false, true} {
		t.Run(map[bool]string{true: "cancel", false: "continue"}[cancel], func(t *testing.T) {
			state, session, shell, _ := workspaceCommandSession(t, "sudo -i", WorkspaceCommand{Command: "cd /srv"})
			workspaceCommandPrompt(state, session, shell)
			state.terminalText(session.ID, app.TextEvent{Kind: app.TextInsert, Text: "secret"})
			session.term.send([]byte("\r"))
			workspaceCommandPrompt(state, session, shell)
			if shell.typed() != "sudo -i\rsecret\r" {
				t.Fatalf("did not pause: %q", shell.typed())
			}
			action := hitWorkspaceCommandRun.At(int(session.ID))
			if cancel {
				action = hitWorkspaceCommandCancel.At(int(session.ID))
			}
			state.Click(action)
			state.Click(action)
			state.Wake()
			want := "sudo -i\rsecret\r"
			if !cancel {
				want += "cd /srv\r"
			}
			if shell.typed() != want || workspaceCommandPending(session) {
				t.Fatalf("manual action: %q", shell.typed())
			}
			if session.workspaceCommand.Command != "cd /srv" {
				t.Fatal("action modified configuration")
			}
		})
	}
}

func TestWorkspaceCommandWithoutLoginAndReset(t *testing.T) {
	state, session, shell, _ := workspaceCommandSession(t, "", WorkspaceCommand{Command: "cd /srv"})
	workspaceCommandPrompt(state, session, shell)
	if shell.typed() != "cd /srv\r" {
		t.Fatalf("workspace command: %q", shell.typed())
	}
	session.term.resetFolderSync()
	if session.term.login.line != "" {
		t.Fatal("reset kept pending command")
	}
	session.term.armInitialization(session)
	workspaceCommandPrompt(state, session, shell)
	if shell.typed() != "cd /srv\rcd /srv\r" {
		t.Fatalf("new shell: %q", shell.typed())
	}
}

func TestWorkspaceCommandWriteFailureDoesNotAdvance(t *testing.T) {
	state, session, shell, _ := workspaceCommandSession(t, "sudo -i", WorkspaceCommand{Command: "cd /srv"})
	shell.mu.Lock()
	shell.refuse = true
	shell.mu.Unlock()
	workspaceCommandPrompt(state, session, shell)
	if session.term.login.line != "sudo -i" || !session.term.login.paused || shell.typed() != "" {
		t.Fatal("failed write advanced initialization")
	}
	shell.mu.Lock()
	shell.refuse = false
	shell.mu.Unlock()
	state.Wake()
	if shell.typed() != "" {
		t.Fatal("failed write automatically retried")
	}
	state.Click(hitWorkspaceCommandRun.At(int(session.ID)))
	if shell.typed() != "sudo -i\r" {
		t.Fatal("manual retry failed")
	}
	workspaceCommandPrompt(state, session, shell)
	if shell.typed() != "sudo -i\r" {
		t.Fatal("manual retry resumed paused automation")
	}
}

func TestWorkspaceCommandDraftPersistenceAndSessionSnapshot(t *testing.T) {
	_, options := newWorld(t)
	state := newTestState(t, options)
	if workspaceCommandHost(localHost("Local")) {
		t.Fatal("local terminal supports SSH initialization")
	}
	state.openWorkspaceDraft(workspaceDraftFixed)
	state.workspaceDraft.name.Insert("Logs")
	state.Click(workspaceDraftHostID(0))
	host := state.hosts[0]
	state.Click(hitWorkspaceCommandEdit.At(int(host.ID)))
	editor := state.EditorFor(hitWorkspaceCommandField.At(int(host.ID)))
	if editor == nil {
		t.Fatal("no command editor")
	}
	editor.Insert("cd /srv && tail -f app.log")
	canvas, _ := paintAt(state, gfx.Size{Width: 1280, Height: 800})
	panelHit(t, canvas, hitWorkspaceCommandField.At(int(host.ID)))
	state.submitWorkspaceDraft()
	if len(state.templates) != 1 {
		t.Fatalf("save failed: %s", state.workspaceDraft.error)
	}
	command := WorkspaceCommand{Command: "cd /srv && tail -f app.log"}
	template := state.templates[0]
	if template.Commands[host.CatalogID] != command {
		t.Fatalf("saved %+v", template.Commands)
	}
	cloned := cloneWorkspaceTemplates(state.templates)
	cloned[0].Commands[host.CatalogID] = WorkspaceCommand{Command: "other"}
	if template.Commands[host.CatalogID] != command {
		t.Fatal("clone aliased command map")
	}
	loaded, _, err := loadState(options.StatePath)
	if err != nil || !reflect.DeepEqual(loaded.WorkspaceTemplates[0], template) {
		t.Fatalf("round trip: %+v %v", loaded.WorkspaceTemplates, err)
	}
	state.applySelectedWorkspaceTemplate()
	if state.sessions[0].workspaceCommand != command {
		t.Fatal("launch lost snapshot")
	}
	// Editing the template cannot replace an existing session's configuration.
	state.templates[0].Commands[host.CatalogID] = WorkspaceCommand{Command: "other"}
	if state.sessions[0].workspaceCommand != command {
		t.Fatal("template edit changed live session")
	}
	saved := state.snapshot()
	restored := newTestState(t, testOptions(t))
	restored.restoreSessions(saved.Sessions, saved.ActiveSession)
	if restored.sessions[0].workspaceCommand != command {
		t.Fatal("session restore lost snapshot")
	}
	workspace := state.deck.ActiveWorkspace()
	state.temporaryWorkspaceLayouts[workspace] = layoutcore.GridSpec(1)
	state.openWorkspaceSave(workspace)
	state.submitWorkspaceSave()
	if len(state.templates) != 2 || state.templates[1].Commands[host.CatalogID] != command {
		t.Fatal("save current workspace lost snapshot")
	}
}

func TestWorkspaceCommandNormalizationAndRemoval(t *testing.T) {
	state := newTestState(t, testOptions(t))
	host := state.hosts[0]
	template := mustWorkspaceTemplate(t, "Logs", []string{host.CatalogID}, 1)
	template.Commands = map[string]WorkspaceCommand{host.CatalogID: {Command: " cd /srv "}, "host_missing": {Command: "invalid"}, workspaceLocalTerminalRef: {Command: "invalid"}}
	normalized, err := normalizeWorkspaceTemplate(template)
	if err != nil || len(normalized.Commands) != 1 || normalized.Commands[host.CatalogID].Command != "cd /srv" {
		t.Fatalf("normalization: %+v %v", normalized, err)
	}
	// A missing catalog host can still be retained in an edited template.
	normalized.HostIDs = append(normalized.HostIDs, "host_missing")
	normalized.Commands["host_missing"] = WorkspaceCommand{Command: "tail -f app.log"}
	state.templates = []WorkspaceTemplate{normalized}
	state.openWorkspaceDraftEdit(template.ID)
	commands, valid := state.workspaceDraftCommands()
	if !valid || commands["host_missing"].Command != "tail -f app.log" {
		t.Fatal("draft lost missing host command")
	}

	state.workspaceDraftRemoveItem(0)
	if _, exists := state.workspaceDraft.commandEditors[host.CatalogID]; exists {
		t.Fatal("removed host kept configuration")
	}
	// Invalid lines cannot be submitted through the form.
	state.openWorkspaceDraft(workspaceDraftFixed)
	state.workspaceDraft.selected[host.CatalogID] = true
	state.workspaceDraft.commandEditor(host.CatalogID).Insert("one\ntwo")
	if _, valid := state.workspaceDraftCommands(); valid {
		t.Fatal("accepted multiline command")
	}
}

func TestWorkspaceCommandDefaultUsesLoginAddedLater(t *testing.T) {
	state, session, shell, now := workspaceCommandSession(t, "", WorkspaceCommand{Command: "cd /srv"})
	shell.say("$ ")
	state.Wake()
	*now += loginCommandGrace
	state.Wake()
	if shell.typed() != "cd /srv\r" {
		t.Fatalf("initial quiet fallback: %q", shell.typed())
	}
	session.term.resetFolderSync()
	session.host.LoginCommand = "cd /srv" // Even identical commands are two distinct steps.
	session.term.armInitialization(session)
	workspaceCommandPrompt(state, session, shell)
	if shell.typed() != "cd /srv\rcd /srv\r" {
		t.Fatalf("new login not executed first: %q", shell.typed())
	}
	workspaceCommandPrompt(state, session, shell)
	if shell.typed() != "cd /srv\rcd /srv\rcd /srv\r" {
		t.Fatalf("workspace step lost: %q", shell.typed())
	}
}

func TestWorkspaceCommandManualActionDoesNotCrossReconnect(t *testing.T) {
	state, session, shell, _ := workspaceCommandSession(t, "", WorkspaceCommand{Command: "cd /srv"})
	session.term.takeOutput = func() {
		session.term.resetFolderSync()
		session.term.armInitialization(session)
	}
	state.Click(hitWorkspaceCommandRun.At(int(session.ID)))
	if shell.typed() != "" {
		t.Fatal("stale manual action reached replacement shell")
	}
}
