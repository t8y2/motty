package workbench

import (
	"testing"

	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
)

func lockTab(t *testing.T, state *State, id layoutcore.WorkspaceID) {
	t.Helper()
	rightClickTab(state, hitTabList.At(int(id)))
	state.Click(hitTabMenuLock)
	if !state.deck.Locked(id) {
		t.Fatal("the menu did not lock the tab")
	}
}

func TestLockedTabCloseRoutesRequireConfirmation(t *testing.T) {
	for _, route := range []string{"button", "menu", "shortcut", "pane", "connection", "terminal-menu"} {
		t.Run(route, func(t *testing.T) {
			state := newTestState(t, testOptions(t))
			openHost(state)
			id := state.deck.ActiveWorkspace()
			lockTab(t, state, id)
			closeTab := func() {
				switch route {
				case "button":
					state.Click(hitTabCloseList.At(int(id)))
				case "menu":
					rightClickTab(state, hitTabList.At(int(id)))
					state.Click(hitTabMenuClose)
				case "shortcut":
					commandW(state)
				case "pane":
					state.closeActivePane()
				case "connection":
					state.clickConnection(hitConnClose)
				case "terminal-menu":
					state.closeSession(state.activeSessionID)
				}
			}
			closeTab()
			if state.modal != modalCloseTabs || len(state.sessions) != 1 || len(tabsOf(state)) != 1 {
				t.Fatal("a locked tab closed without confirmation")
			}
			state.Click(hitTabsCloseCancel)
			if !state.deck.Locked(id) || len(state.sessions) != 1 {
				t.Fatal("cancel removed or unlocked the tab")
			}
			closeTab()
			state.Click(hitTabsCloseConfirm)
			if state.modal != modalNone || len(state.sessions) != 0 || len(tabsOf(state)) != 0 {
				t.Fatal("confirm did not release the tab and its sessions")
			}
		})
	}
}

func TestLockedTabStateRestoresAndUnlockAllowsImmediateClose(t *testing.T) {
	options := testOptions(t)
	state := newTestState(t, options)
	openHost(state)
	id := state.deck.ActiveWorkspace()
	lockTab(t, state, id)
	state = newTestState(t, options)
	id = state.deck.ActiveWorkspace()
	if !state.deck.Locked(id) || !state.workspaceTabs[0].Locked {
		t.Fatal("lock did not survive session restoration")
	}
	rightClickTab(state, hitTabList.At(int(id)))
	var canvas gfx.Canvas
	state.Paint(&canvas, gfx.Size{Width: 1100, Height: 700})
	hit, ok := hitByID(&canvas, hitTabMenuLock)
	if !ok || hit.Label != state.messages.MenuUnlockTab {
		t.Fatalf("unlock menu = %+v", hit)
	}
	state.Click(hitTabMenuLock)
	state.closeWorkspace(id)
	if state.modal != modalNone || len(tabsOf(state)) != 0 {
		t.Fatal("an unlocked tab still requires confirmation")
	}
}

func TestLockedTabBulkCloseConfirmsOneLockedTabAndKeepsStableTargets(t *testing.T) {
	state, tabs := fourTabs(t)
	lockTab(t, state, tabs[3])
	rightClickTab(state, hitTabList.At(int(tabs[2])))
	state.Click(hitTabMenuCloseRight)
	if state.modal != modalCloseTabs || state.tabsClose.locked != 1 || len(tabsOf(state)) != 4 {
		t.Fatal("closing a single locked neighbour bypassed confirmation")
	}
	state.Click(hitTabsCloseCancel)
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	state.Click(hitTabMenuCloseRight)
	// A background/new-window operation may change the deck while the dialog
	// is open. The confirmation must still apply only to the displayed targets.
	newID, _, err := state.deck.Create("new tab")
	if err != nil {
		t.Fatal(err)
	}
	state.rebuildTabOptions()
	state.Click(hitTabsCloseConfirm)
	if got := tabsOf(state); len(got) != 3 || got[0] != tabs[0] || got[1] != tabs[1] || got[2] != newID {
		t.Fatalf("confirmation closed different tabs: %v", got)
	}
}

func TestLockedSplitTabProtectsLastPaneButAllowsClosingOneSplit(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	id := state.deck.ActiveWorkspace()
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	lockTab(t, state, id)
	state.closeActivePane()
	if state.modal != modalNone || len(state.deck.Panes(id, nil)) != 1 || !state.deck.Locked(id) {
		t.Fatal("closing one split changed tab protection")
	}
	state.closeActivePane()
	if state.modal != modalCloseTabs || len(state.sessions) != 1 {
		t.Fatal("closing the last split bypassed protection")
	}
}

// A crowded strip shrinks every tab to barTabMin; a locked tab keeps 18 more so
// its title never collides with the lock icon. This guards the reserve itself,
// which a screenshot cannot.
func TestLockedTabKeepsItsTitleRoomInACrowdedStrip(t *testing.T) {
	measure := func(locked bool) float32 {
		t.Helper()
		state := newTestState(t, testOptions(t))
		for range 20 {
			openEmptyTab(state)
		}
		if locked {
			if err := state.deck.SetLocked(tabsOf(state)[0], true); err != nil {
				t.Fatal(err)
			}
			state.rebuildTabOptions()
		}
		state.Paint(&gfx.Canvas{}, gfx.Size{Width: 900, Height: 600})
		return state.chrome.geometry.tabWidth
	}
	if got := measure(false); got != barTabMin {
		t.Fatalf("an unlocked crowded strip uses %v wide tabs, want %v", got, barTabMin)
	}
	if got := measure(true); got != barTabMin+18 {
		t.Fatalf("a locked crowded strip uses %v wide tabs, want %v", got, barTabMin+18)
	}
}

// The lock is the tab's only visible sign of protection; assistive technology
// has to hear it from the tab's own hint.
func TestLockedTabAnnouncesItsProtection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	id := state.deck.ActiveWorkspace()
	hit := func() gfx.Hit {
		t.Helper()
		canvas := &gfx.Canvas{}
		state.Paint(canvas, gfx.Size{Width: 1400, Height: 900})
		found, ok := hitByID(canvas, hitTabList.At(int(id)))
		if !ok {
			t.Fatal("the tab is not painted")
		}
		return found
	}
	if tab := hit(); tab.Hint != tab.Label {
		t.Fatalf("an unlocked tab announces %q, want its full label %q", tab.Hint, tab.Label)
	}
	lockTab(t, state, id)
	if hint := hit().Hint; hint != state.messages.TabLockedHint {
		t.Fatalf("a locked tab announces %q, want %q", hint, state.messages.TabLockedHint)
	}
}
