package workbench

import (
	"testing"

	"github.com/vivaui/viva/ui"
	layoutcore "motty/internal/workspace"
)

func rightClickTab(state *State, target ui.ID) ui.EventResult {
	return pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Target: target, X: 300, Y: 14, Viewport: state.viewport})
}

func tabsOf(state *State) []layoutcore.WorkspaceID {
	var ids []layoutcore.WorkspaceID
	for _, tab := range state.deck.Workspaces(nil) {
		ids = append(ids, tab.ID)
	}
	return ids
}

func fourTabs(t *testing.T) (*State, []layoutcore.WorkspaceID) {
	t.Helper()
	state := newTestState(t, testOptions(t))
	for range 4 {
		openEmptyTab(state)
	}
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	return state, tabsOf(state)
}

func TestRightClickOnATabOpensItsMenu(t *testing.T) {
	state, tabs := fourTabs(t)
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTabMenuClose); ok {
		t.Fatal("the menu is up before any click")
	}
	if result := rightClickTab(state, hitTabList.At(int(tabs[1]))); !result.Handled || !result.Repaint {
		t.Fatalf("right click = %+v", result)
	}
	canvas = ui.Canvas{}
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, id := range []ui.ID{hitTabMenuClose, hitTabMenuCloseOthers, hitTabMenuCloseRight} {
		hit, ok := hitByID(&canvas, id)
		if !ok || hit.Disabled {
			t.Errorf("menu item %d: %+v, found %v", id, hit, ok)
		}
	}
	if state.deck.ActiveWorkspace() != tabs[3] {
		t.Fatal("right-clicking a tab must not switch to it")
	}
}

func TestRightClickOnACloseButtonOpensTheMenuToo(t *testing.T) {
	state, tabs := fourTabs(t)
	rightClickTab(state, hitTabCloseList.At(int(tabs[0])))
	if !state.tabMenu.open || state.tabMenu.workspace != tabs[0] {
		t.Fatalf("menu = %+v", state.tabMenu)
	}
}

func TestTabMenuDisablesWhatCannotApply(t *testing.T) {
	state, tabs := fourTabs(t)
	rightClickTab(state, hitTabList.At(int(tabs[3])))
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if hit, _ := hitByID(&canvas, hitTabMenuCloseRight); !hit.Disabled {
		t.Fatal("the last tab has nothing to its right to close")
	}
	state.Click(hitTabMenuDismiss)
	for _, id := range tabs[1:] {
		state.closeWorkspace(id)
	}
	rightClickTab(state, hitTabList.At(int(tabs[0])))
	canvas = ui.Canvas{}
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if hit, _ := hitByID(&canvas, hitTabMenuCloseOthers); !hit.Disabled {
		t.Fatal("a lone tab has no others to close")
	}
}

func TestTabMenuCloseAndCloseOthersAndCloseRight(t *testing.T) {
	state, tabs := fourTabs(t)
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	state.Click(hitTabMenuClose)
	if got := tabsOf(state); len(got) != 3 || got[0] != tabs[0] || got[1] != tabs[2] {
		t.Fatalf("after Close: %v", got)
	}

	state, tabs = fourTabs(t)
	// Only one tab to the right of the third: no question for a single tab.
	rightClickTab(state, hitTabList.At(int(tabs[2])))
	state.Click(hitTabMenuCloseRight)
	if state.modal != modalNone || len(tabsOf(state)) != 3 {
		t.Fatalf("closing one tab asked first or did not close it: modal=%v tabs=%d", state.modal, len(tabsOf(state)))
	}

	state, tabs = fourTabs(t)
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	state.Click(hitTabMenuCloseRight)
	if state.modal != modalCloseTabs || len(tabsOf(state)) != 4 {
		t.Fatalf("closing two tabs did not ask first: modal=%v tabs=%d", state.modal, len(tabsOf(state)))
	}
	state.Click(hitTabsCloseConfirm)
	if got := tabsOf(state); len(got) != 2 || got[1] != tabs[1] {
		t.Fatalf("after Close to the Right: %v", got)
	}
	if state.deck.ActiveWorkspace() != tabs[1] {
		t.Fatalf("active = %d, want the tab the menu was opened on (%d)", state.deck.ActiveWorkspace(), tabs[1])
	}

	state, tabs = fourTabs(t)
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	state.Click(hitTabMenuCloseOthers)
	state.Click(hitTabsCloseCancel)
	if state.modal != modalNone || len(tabsOf(state)) != 4 {
		t.Fatalf("cancel closed tabs: modal=%v tabs=%d", state.modal, len(tabsOf(state)))
	}
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	state.Click(hitTabMenuCloseOthers)
	state.Key(ui.KeyEvent{Key: ui.KeyEnter, Target: hitTabsCloseConfirm})
	if got := tabsOf(state); len(got) != 1 || got[0] != tabs[1] {
		t.Fatalf("after Close Others: %v", got)
	}
	if state.deck.ActiveWorkspace() != tabs[1] || state.activeSection != sectionTerminal {
		t.Fatalf("active = %d section %v", state.deck.ActiveWorkspace(), state.activeSection)
	}
}

func TestTabMenuClosesWithoutActingOnAnOutsidePressOrEscape(t *testing.T) {
	state, tabs := fourTabs(t)
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if hit, ok := hitByID(&canvas, hitTabMenuDismiss); !ok || hit.Bounds.Width != 1400 || hit.Bounds.Height != 900 {
		t.Fatalf("no window-wide backdrop: %+v, %v", hit, ok)
	}
	if result := pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Target: hitTabMenuDismiss}); !result.Handled || state.tabMenu.open {
		t.Fatalf("a secondary press outside: %+v open=%v", result, state.tabMenu.open)
	}
	rightClickTab(state, hitTabList.At(int(tabs[1])))
	state.Click(hitTabMenuDismiss) // Escape and a primary press outside both arrive as this
	if state.tabMenu.open || len(tabsOf(state)) != 4 {
		t.Fatalf("open=%v tabs=%d", state.tabMenu.open, len(tabsOf(state)))
	}
}

func TestTabMenuOffersReconnectOnlyForATabThatNeedsIt(t *testing.T) {
	_, options := newWorld(t)
	first := newTestState(t, options)
	first.restoreConnect = restoreConnectOff
	openHost(first)
	first.save()

	state := newTestState(t, options)
	state.restoreConnect = restoreConnectOff
	state.Paint(&ui.Canvas{}, ui.Size{Width: 1400, Height: 900})
	tab := state.deck.ActiveWorkspace()
	session, _ := state.sessionByIDPointer(state.activeSessionID)
	if !session.restored {
		t.Fatal("setup: not restored")
	}
	rightClickTab(state, hitTabList.At(int(tab)))
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTabMenuReconnect); !ok {
		t.Fatal("a restored tab offers no Reconnect")
	}
	state.Click(hitTabMenuReconnect)
	if session.conn == nil {
		t.Fatal("Reconnect did not dial")
	}
	rightClickTab(state, hitTabList.At(int(tab)))
	canvas = ui.Canvas{}
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	if _, ok := hitByID(&canvas, hitTabMenuReconnect); ok {
		t.Fatal("a tab that is connecting still offers Reconnect")
	}
}

func TestTabMenuStaysInsideTheWindow(t *testing.T) {
	state, tabs := fourTabs(t)
	pointer(state, ui.PointerEvent{Kind: ui.PointerDown, Button: ui.PointerButtonSecondary, Target: hitTabList.At(int(tabs[0])), X: 1399, Y: 895, Viewport: state.viewport})
	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	hit, ok := hitByID(&canvas, hitTabMenuCloseRight)
	if !ok || hit.Bounds.X+hit.Bounds.Width > 1400 || hit.Bounds.Y+hit.Bounds.Height > 900 {
		t.Fatalf("the menu runs off the window: %+v %v", hit.Bounds, ok)
	}
}
