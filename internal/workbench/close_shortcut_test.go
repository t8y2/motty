package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

// commandW presses the close shortcut with the modifier this platform's defaults
// use: Command on macOS, Control elsewhere.
func commandW(state *State) ui.EventResult {
	return state.Shortcut(ui.KeyEvent{Modifiers: defaultShortcutModifier(), Text: "w", Key: ui.Key('w')})
}

func TestCommandWClosesAConnectingTab(t *testing.T) {
	world, options := newWorld(t)
	state := newTestState(t, options)
	connect(t, state, world)
	if state.deck.ActiveWorkspace() == 0 {
		t.Fatal("connecting opened no tab")
	}

	result := commandW(state)
	if !result.Handled {
		t.Fatalf("Command+W was not handled: %+v", result)
	}
	if len(state.sessions) != 0 {
		t.Fatalf("sessions = %d, want the connecting session closed", len(state.sessions))
	}
	if tabs := state.deck.Workspaces(nil); len(tabs) != 0 {
		t.Fatalf("tabs = %+v, want the tab itself gone", tabs)
	}
	if state.activeSection != sectionHosts {
		t.Fatalf("section = %v, want the host list", state.activeSection)
	}
}

func TestCommandWClosesOnlyTheActivePaneOfASplitWorkspace(t *testing.T) {
	state := threeVisiblePaneState(t)
	tabs := len(state.deck.Workspaces(nil))
	panes := len(state.Panes(nil))
	sessions := len(state.sessions)
	closing := state.activeSessionID

	commandW(state)
	if got := len(state.Panes(nil)); got != panes-1 {
		t.Fatalf("panes = %d, want %d", got, panes-1)
	}
	if len(state.sessions) != sessions-1 {
		t.Fatalf("sessions = %d, want %d", len(state.sessions), sessions-1)
	}
	if _, ok := state.sessionByID(closing); ok {
		t.Fatal("the active pane's session survived")
	}
	if got := len(state.deck.Workspaces(nil)); got != tabs {
		t.Fatalf("tabs = %d, want the workspace kept (%d)", got, tabs)
	}
	if _, _, ok := state.deck.PaneForSession(layoutcore.SessionID(state.activeSessionID)); !ok {
		t.Fatal("the new active session has no pane")
	}
}

// An empty pane has no session behind it, which is exactly what Command+W has to
// still close: first the pane, and then, as the last one, the tab.
func TestCommandWClosesAnEmptyPane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openEmptyTab(state) // a tab of one empty pane
	workspace := state.deck.ActiveWorkspace()
	state.Click(hitPaneSplitHorizontal)
	if panes := state.deck.Panes(workspace, nil); len(panes) != 2 || panes[0].SessionID != 0 {
		t.Fatalf("setup panes = %+v", panes)
	}
	if result := commandW(state); !result.Handled {
		t.Fatalf("Command+W on an empty pane: %+v", result)
	}
	if panes := state.deck.Panes(workspace, nil); len(panes) != 1 {
		t.Fatalf("panes = %+v, want the empty pane closed", panes)
	}
	commandW(state)
	if _, ok := state.deck.Layout(workspace); ok {
		t.Fatal("the last empty pane did not close its tab")
	}
}

// "+" opens a tab holding one empty pane: Command+W closes that tab too.
func TestCommandWClosesANewEmptyTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	openEmptyTab(state)
	workspace := state.deck.ActiveWorkspace()
	if state.activeSessionID != 0 && state.deck.ActivePane(workspace) == 0 {
		t.Fatalf("setup: active session %d", state.activeSessionID)
	}
	tabs := len(state.deck.Workspaces(nil))
	commandW(state)
	if got := len(state.deck.Workspaces(nil)); got != tabs-1 {
		t.Fatalf("tabs = %d, want %d: Command+W left the empty tab", got, tabs-1)
	}
}

// With no tab open the host list is all there is, and Command+W closes the
// window, as it does in any macOS app.
func TestCommandWOnTheHostListClosesTheWindow(t *testing.T) {
	closed := 0
	options := testOptions(t)
	options.CloseWindow = func() { closed++ }
	state := New(options)
	t.Cleanup(state.Close)

	if result := commandW(state); !result.Handled {
		t.Fatalf("Command+W on the host list: %+v", result)
	}
	if closed != 1 {
		t.Fatalf("window close requests = %d, want 1", closed)
	}
}

// Tabs kept open behind the host list are not what Command+W is aimed at: the
// page on show is, so the tab survives and the window is asked to close.
func TestCommandWOnTheHostListLeavesTabsOpenBehindIt(t *testing.T) {
	closed := 0
	world, options := newWorld(t)
	options.CloseWindow = func() { closed++ }
	state := New(options)
	t.Cleanup(state.Close)
	connect(t, state, world)
	state.activeSection = sectionHosts
	tabs := len(state.deck.Workspaces(nil))

	commandW(state)
	if closed != 1 {
		t.Fatalf("window close requests = %d, want 1", closed)
	}
	if got := len(state.deck.Workspaces(nil)); got != tabs {
		t.Fatalf("tabs = %d, want %d: Command+W closed a tab that was not on show", got, tabs)
	}
}
