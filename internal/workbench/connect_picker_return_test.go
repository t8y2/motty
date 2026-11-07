package workbench

import (
	"testing"

	"github.com/vivaui/viva/text"
	layoutcore "motty/internal/workspace"
)

// Closing the host editor that the new-tab connect picker opened brings the
// picker back, so editing a host does not drop the user out of choosing where
// to connect.
func TestEditingAHostFromTheConnectPickerReturnsToIt(t *testing.T) {
	state, _, _, _ := liveSession(t)
	state.openConnectPicker()
	if state.modal != modalCommandPalette || !state.commandPalette.connect {
		t.Fatalf("the connect picker did not open: modal=%d connect=%v", state.modal, state.commandPalette.connect)
	}
	state.openEditHostID(state.hosts[0].ID)
	if state.modal != modalNewHost {
		t.Fatalf("the host editor did not take over: modal=%d", state.modal)
	}
	state.Click(hitDialogCancel)
	if state.modal != modalCommandPalette || !state.commandPalette.connect {
		t.Fatalf("cancelling dropped the picker: modal=%d connect=%v", state.modal, state.commandPalette.connect)
	}
}

// An empty pane is what paints the New-Tab page, so it must stay the active
// pane once the editor that was opened over it closes. This is the pane the
// connect picker was on.
func TestAnEmptyPaneStaysActiveAfterItsEditorCloses(t *testing.T) {
	state, _, _, _ := liveSession(t)
	empty := state.messages.LabelNewTab
	workspace, _, err := state.deck.Create(empty)
	if err != nil {
		t.Fatal(err)
	}
	state.deck.ActivateWorkspace(workspace)
	state.activeSessionID = 0
	state.activeSection = sectionTerminal
	state.refreshWorkspaceViews()

	state.openHostDialog()
	state.editor.label = *text.NewEditor("A second host")
	state.editor.address = *text.NewEditor("second.example.com")
	state.editor.user = *text.NewEditor("ops")
	state.Click(hitDialogSubmit)
	if state.modal == modalNewHost {
		t.Fatalf("the form did not submit: %q", state.dialogError)
	}
	if state.activeSessionID != 0 {
		t.Fatalf("an empty pane opened elsewhere: active session = %d", state.activeSessionID)
	}
	active := state.deck.ActivePane(state.deck.ActiveWorkspace())
	for _, pane := range state.deck.Panes(state.deck.ActiveWorkspace(), nil) {
		if pane.ID == active && pane.SessionID != 0 {
			t.Fatalf("the active pane is not the empty one: %+v", pane)
		}
	}
}

// Editing a host from the host list must not move the keyboard off the live
// terminal the user was in, even when the same workspace also holds an empty
// pane for a new tab.
func TestEditingAHostFromTheHostListKeepsTheLiveTerminalActive(t *testing.T) {
	state, _, _, _ := liveSession(t)
	live := state.activeSessionID
	if live == 0 {
		t.Fatal("the fixture has no live session")
	}
	if !state.splitActivePane(layoutcore.Horizontal) {
		t.Fatal("could not split the pane")
	}
	// The split leaves the new pane empty and active; the user goes back to the
	// terminal, which is where they are when they open the host list.
	if !state.ActivatePane(live) {
		t.Fatal("could not return to the live terminal")
	}
	if state.activeSessionID != live {
		t.Fatalf("active session = %d, want %d", state.activeSessionID, live)
	}

	state.activeSection = sectionHosts
	state.openEditHostID(state.hosts[0].ID)
	state.Click(hitDialogCancel)

	if state.activeSessionID != live {
		t.Fatalf("closing the host editor moved the keyboard to session %d, want %d", state.activeSessionID, live)
	}
}
