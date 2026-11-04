package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func activeTab(state *State) layoutcore.WorkspaceInfo {
	for _, tab := range state.deck.Workspaces(nil) {
		if tab.ID == state.deck.ActiveWorkspace() {
			return tab
		}
	}
	return layoutcore.WorkspaceInfo{}
}

func TestTabFollowsTheTitleTheTerminalReports(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	hostName := activeTab(state).Title
	state.sessions[0].term.screen.Write([]byte("\x1b]2;work:1:bash\a"))

	if got := state.tabLabel(activeTab(state)); got != hostName {
		t.Fatalf("with the setting off the tab reads %q; want the host name %q", got, hostName)
	}

	state.terminalTabTitle = true
	if got := state.tabLabel(activeTab(state)); got != "work:1:bash" {
		t.Fatalf("tab = %q; want the reported title", got)
	}
	if got := activeTab(state).Title; got != hostName {
		t.Fatalf("the stored title became %q; the reported title must not be saved", got)
	}

	state.sessions[0].term.screen.Write([]byte("\x1b]2;\a"))
	if got := state.tabLabel(activeTab(state)); got != hostName {
		t.Fatalf("after the title was cleared the tab reads %q; want %q", got, hostName)
	}
}

func TestRenamedTabKeepsItsNameWhenTheTerminalReportsATitle(t *testing.T) {
	state := newTestState(t, testOptions(t))
	state.terminalTabTitle = true
	openHost(state)
	workspace := state.deck.ActiveWorkspace()
	state.openTabRename(workspace)
	state.tabRename.editor.Insert("production")
	state.clickTabRenameDialog(hitTabRenameSubmit)

	state.sessions[0].term.screen.Write([]byte("\x1b]2;work:1:bash\a"))
	if got := state.tabLabel(activeTab(state)); got != "production" {
		t.Fatalf("tab = %q; a name the user chose must win over the terminal's title", got)
	}

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTabList.At(int(workspace))); !ok {
		t.Fatal("the renamed tab is not painted")
	}
}

func TestActivatingPaneRefreshesCachedTabIdentityAndKeepsRenamedTitle(t *testing.T) {
	state := threeVisiblePaneState(t)
	state.terminalTabTitle = true

	workspaceID := state.deck.ActiveWorkspace()
	workspaceIndex, ok := state.workspaceTab(workspaceID)
	if !ok {
		t.Fatal("the active workspace is missing from the tab cache")
	}
	firstSession := &state.sessions[0]
	secondSession := &state.sessions[1]
	_, firstPane, ok := state.deck.PaneForSession(layoutcore.SessionID(firstSession.ID))
	if !ok {
		t.Fatal("the first session has no pane")
	}
	_, secondPane, ok := state.deck.PaneForSession(layoutcore.SessionID(secondSession.ID))
	if !ok {
		t.Fatal("the second session has no pane")
	}

	firstSession.term.screen.Write([]byte("\x1b]2;first-shell\a"))
	secondSession.term.screen.Write([]byte("\x1b]2;second-shell\a"))
	state.rebuildTabOptions()

	state.ActivatePane(firstSession.ID)
	cached := state.workspaceTabs[workspaceIndex]
	if cached.ActivePane != firstPane || state.tabSession(cached) != firstSession || state.tabLabel(cached) != "first-shell" {
		t.Fatalf("after first activation: cached pane=%d session=%p label=%q", cached.ActivePane, state.tabSession(cached), state.tabLabel(cached))
	}

	state.ActivatePane(secondSession.ID)
	cached = state.workspaceTabs[workspaceIndex]
	if cached.ActivePane != secondPane || state.tabSession(cached) != secondSession || state.tabLabel(cached) != "second-shell" {
		t.Fatalf("after second activation: cached pane=%d session=%p label=%q", cached.ActivePane, state.tabSession(cached), state.tabLabel(cached))
	}

	state.openTabRename(workspaceID)
	state.tabRename.editor.Insert("production-main")
	state.clickTabRenameDialog(hitTabRenameSubmit)
	state.ActivatePane(firstSession.ID)
	cached = state.workspaceTabs[workspaceIndex]
	if !cached.Named || cached.ActivePane != firstPane || state.tabSession(cached) != firstSession {
		t.Fatalf("after renamed tab activation: named=%t cached pane=%d session=%p", cached.Named, cached.ActivePane, state.tabSession(cached))
	}
	if got := state.tabLabel(cached); got != "production-main" {
		t.Fatalf("renamed tab changed to %q after activating another pane", got)
	}
}

func TestTabTitleSettingSurvivesRestart(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	state.terminalTabTitle = true
	state.save()
	if again := newTestState(t, options); !again.terminalTabTitle {
		t.Fatal("the setting was lost after a restart")
	}
}
