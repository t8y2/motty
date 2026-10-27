package workbench

import (
	"strings"
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

func TestTabMenuOffersDuplicateForSessionTab(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	workspace := state.deck.ActiveWorkspace()
	rightClickTab(state, hitTabList.At(int(workspace)))

	var canvas ui.Canvas
	state.Paint(&canvas, ui.Size{Width: 1400, Height: 900})
	for _, hit := range canvas.Hits() {
		if hit.Label == "Duplicate Tab" && !hit.Disabled {
			return
		}
	}
	t.Fatal("a session tab does not offer Duplicate Tab")
}

func TestTabMenuDuplicateOpensAnIndependentSessionForTheSameHost(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	originalWorkspace := state.deck.ActiveWorkspace()
	firstPane := state.sessions[0]
	openHostInSplit(t, state, 2, layoutcore.Horizontal)
	const sourceOnly = "source-only-output"
	sourceTerminal := state.sessions[1].term
	sourceTerminal.screen.Write([]byte(sourceOnly))
	sourceShell := &fakeShell{done: make(chan struct{})}
	sourceTerminal.shell = sourceShell
	source := state.sessions[1]
	if state.activeSessionID != source.ID || source.HostID == firstPane.HostID {
		t.Fatalf("split setup active=%d source=%d hosts=%d/%d", state.activeSessionID, source.ID, firstPane.HostID, source.HostID)
	}
	rightClickTab(state, hitTabList.At(int(originalWorkspace)))

	state.Click(hitTabMenuDuplicate)

	workspaces := state.deck.Workspaces(nil)
	if len(workspaces) != 2 || len(state.sessions) != 3 {
		t.Fatalf("duplicate created workspaces=%d sessions=%d, want 2/3", len(workspaces), len(state.sessions))
	}
	duplicate := state.sessions[2]
	if duplicate.ID == source.ID || duplicate.HostID != source.HostID || duplicate.Address != source.Address ||
		duplicate.host.CatalogID != source.host.CatalogID || duplicate.host.Address != source.host.Address {
		t.Fatalf("duplicate = %+v, active source = %+v", duplicate, source)
	}
	if duplicate.term == nil || source.term == nil || duplicate.term == source.term || duplicate.term.screen == source.term.screen || duplicate.term.shell == sourceShell {
		t.Fatalf("duplicate terminal/screen/shell = %p/%p/%p, source = %p/%p/%p; want independent state",
			duplicate.term, duplicate.term.screen, duplicate.term.shell, source.term, source.term.screen, source.term.shell)
	}
	if text := duplicate.term.screen.Text(); strings.Contains(text, sourceOnly) {
		t.Fatalf("duplicate copied source terminal buffer: %q", text)
	}
	if state.deck.ActiveWorkspace() == originalWorkspace || state.activeSessionID != duplicate.ID {
		t.Fatalf("active workspace/session = %d/%d, want the duplicate %d", state.deck.ActiveWorkspace(), state.activeSessionID, duplicate.ID)
	}
}

func TestTabMenuDuplicateDoesNotConsumeAPendingEmptyPane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	openHost(state)
	sourceWorkspace := state.deck.ActiveWorkspace()
	openEmptyTab(state)
	pendingWorkspace := state.deck.ActiveWorkspace()
	pendingPane := state.deck.ActivePane(pendingWorkspace)
	state.Click(hitPaneChooseHost)
	rightClickTab(state, hitTabList.At(int(sourceWorkspace)))

	state.Click(hitTabMenuDuplicate)

	if workspaces := state.deck.Workspaces(nil); len(workspaces) != 3 {
		t.Fatalf("duplicate created %d workspaces, want a new third tab", len(workspaces))
	}
	pane, ok := state.deck.Pane(pendingWorkspace, pendingPane)
	if !ok || pane.SessionID != 0 {
		t.Fatalf("pending pane = %+v, found %v; want it left empty", pane, ok)
	}
	if state.pendingWorkspace != 0 || state.pendingPane != 0 {
		t.Fatalf("pending target survived duplicate: workspace=%d pane=%d", state.pendingWorkspace, state.pendingPane)
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
	openHost(first)
	first.save()

	state := newTestState(t, options)
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
