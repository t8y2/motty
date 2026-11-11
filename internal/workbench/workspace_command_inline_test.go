package workbench

import (
	"reflect"
	"testing"

	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/text"
)

func TestWorkspaceInlineSaveRollbackAndIsolation(t *testing.T) {
	state := templateManagementState(t, 2)
	id := state.templates[0].HostIDs[0]
	original := WorkspaceCommand{Command: "cd /srv"}
	state.sessions = append(state.sessions, Session{workspaceCommand: original})
	state.templates[0].Commands = map[string]WorkspaceCommand{id: original}
	state.templates[1].Commands = map[string]WorkspaceCommand{id: {Command: "other"}}
	state.Click(hitWorkspaceInlineEdit.At(0))
	state.workspaceCommandEdit.editor = *text.NewEditor("tail -f app.log")
	previous := cloneWorkspaceTemplates(state.templates)
	config := state.config
	state.config = nil
	state.Click(hitWorkspaceInlineSave)
	state.config = config
	if !reflect.DeepEqual(previous, state.templates) || state.workspaceCommandEdit.error == "" || state.workspaceCommandEdit.editor.String() != "tail -f app.log" {
		t.Fatal("failed save lost input or mutated templates")
	}
	state.Click(hitWorkspaceInlineSave)
	if got := state.templates[0].Commands[id]; got.Command != "tail -f app.log" {
		t.Fatalf("saved %+v", got)
	}
	if state.sessions[len(state.sessions)-1].workspaceCommand != original {
		t.Fatal("changed running snapshot")
	}
	if state.templates[1].Commands[id].Command != "other" {
		t.Fatal("changed another template")
	}
	state.Click(hitWorkspaceInlineEdit.At(0))
	state.workspaceCommandEdit.editor = *text.NewEditor("one\ntwo")
	state.Click(hitWorkspaceInlineSave)
	if state.workspaceCommandEdit.error != state.messages.WorkspaceCommandInvalid {
		t.Fatal("invalid command accepted")
	}
	state.workspaceCommandEdit.editor = *text.NewEditor("")
	state.Click(hitWorkspaceInlineSave)
	if len(state.templates[0].Commands) != 0 {
		t.Fatal("clear did not remove command")
	}
}

func TestWorkspaceInlineMissingTargetAndOutsideClick(t *testing.T) {
	state := templateManagementState(t, 1)
	id := state.templates[0].HostIDs[0]
	state.templates[0].Commands = map[string]WorkspaceCommand{id: {Command: "ls"}}
	state.Click(hitWorkspaceInlineEdit.At(0))
	state.noteExit = &noteExitRequest{}
	if state.EditorFor(hitWorkspaceInlineField) != nil {
		t.Fatal("editor bypassed note exit confirmation")
	}
	state.noteExit = nil
	state.activeSection = sectionHosts
	if state.EditorFor(hitWorkspaceInlineField) != nil {
		t.Fatal("editor remained active outside workspaces")
	}
	state.activeSection = sectionWorkspaces
	state.Click(hitWorkspaceNew)
	if state.workspaceCommandEdit.template != "" || state.modal != modalWorkspaceDraft {
		t.Fatal("outside click did not cancel and execute original action")
	}
	state.closeModal()
	state.Click(hitWorkspaceInlineEdit.At(0))
	state.templates = nil
	state.Click(hitWorkspaceInlineSave)
	if state.workspaceCommandEdit.template != "" || len(state.templates) != 0 {
		t.Fatal("recreated missing target")
	}
}

func TestWorkspaceInlineHitDoesNotStartDrag(t *testing.T) {
	state := templateManagementState(t, 1)
	id := state.templates[0].HostIDs[0]
	state.templates[0].Commands = map[string]WorkspaceCommand{id: {Command: "ls"}}
	paintAt(state, gfx.Size{Width: 1280, Height: 800})
	state.startWorkspaceCardDrag(gfx.PointerEvent{Target: hitWorkspaceInlineEdit.At(0), Button: gfx.PointerButtonPrimary})
	if state.workspaceCardDrag.pressed {
		t.Fatal("command started drag")
	}
	state.startWorkspaceCardDrag(gfx.PointerEvent{Target: hitWorkspaceTemplateCards.At(0), Button: gfx.PointerButtonPrimary})
	if !state.workspaceCardDrag.pressed {
		t.Fatal("card no longer starts drag")
	}
}
