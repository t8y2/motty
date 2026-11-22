package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
	"reflect"
	"testing"
)

func TestPaneMaximizeButtonSwitchAndRestore(t *testing.T) {
	state, firstShell, secondShell := twoLivePanes(t)
	size := gfx.Size{Width: 1400, Height: 900}
	state.Resize(size)
	driver := paneDriver(t, state, size)
	workspace := state.deck.ActiveWorkspace()
	panes := state.deck.Panes(workspace, nil)
	layout, _ := state.deck.Layout(workspace)
	original := layout.DTO()
	click := func(id gfx.ID) {
		t.Helper()
		hit := hitOf(t, driver, id)
		if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
			t.Fatal(err)
		}
	}
	click(hitPaneMaximizeList.At(int(panes[0].ID)))
	if !state.deck.PaneMaximized(workspace) || state.activeSessionID != uint32(panes[0].SessionID) {
		t.Fatal("button maximized the wrong pane")
	}
	for _, pane := range state.Panes(nil) {
		if (pane.Bounds.Width > 0) != pane.Active {
			t.Fatal("hidden pane still occupies the display")
		}
		if _, visible := state.paneWindowBounds(pane.SessionID, size); visible != pane.Active {
			t.Fatal("hidden pane remains hittable")
		}
	}
	if len(state.PaneSplits(nil)) != 0 {
		t.Fatal("maximized pane retains dividers")
	}
	for _, pane := range panes {
		hitOf(t, driver, hitWorkspaceFocusTab.At(int(pane.ID)))
	}
	click(hitWorkspaceFocusTab.At(int(panes[1].ID)))
	if state.activeSessionID != uint32(panes[1].SessionID) || !state.deck.PaneMaximized(workspace) {
		t.Fatal("tab did not select the maximized pane")
	}
	if driver.Snapshot().Focused != state.activeTerminalFocusHit() {
		t.Fatal("mouse selection kept focus outside terminal")
	}
	if err := driver.Type("pane-two"); err != nil {
		t.Fatal(err)
	}
	if firstShell.typed() != "" || secondShell.typed() != "pane-two" {
		t.Fatal("input was sent to the wrong pane")
	}
	click(hitPaneMaximizeList.At(int(panes[1].ID)))
	if state.deck.PaneMaximized(workspace) || !reflect.DeepEqual(original, layout.DTO()) {
		t.Fatal("restore changed original split tree")
	}
	if state.activeSessionID != uint32(panes[1].SessionID) {
		t.Fatal("restore changed input target")
	}
}

func TestPaneMaximizeGeometryAndSplit(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	state.Resize(gfx.Size{Width: 1600, Height: 1000})
	workspace := state.deck.ActiveWorkspace()
	tiled := state.workspacePixelBounds(state.viewport)
	state.togglePaneMaximized(0)
	frame := state.workspacePixelBounds(state.viewport)
	strip := state.workspaceFocusTabsBounds(workspace, state.viewport)
	if frame.Height+strip.Height != tiled.Height || strip.Y != frame.Y+frame.Height {
		t.Fatal("bottom strip overlaps terminal geometry")
	}
	for _, pane := range state.Panes(nil) {
		session, _ := state.sessionByIDPointer(pane.SessionID)
		geometry := state.layoutForSession(pane.SessionID)
		cols, rows := session.term.screen.Size()
		if cols != geometry.cols || rows != geometry.rows || cols <= 0 || rows <= 0 {
			t.Fatal("PTY geometry differs from maximized view")
		}
	}
	if !state.splitActivePane(layoutcore.Vertical) {
		t.Fatal("split failed")
	}
	if state.deck.PaneMaximized(workspace) || len(state.Panes(nil)) != 3 {
		t.Fatal("split did not restore the tiled view")
	}
}

func TestPaneMaximizeTabsKeyboardKeepsSelection(t *testing.T) {
	state, _, _ := twoLivePanes(t)
	state.Resize(gfx.Size{Width: 1400, Height: 900})
	state.togglePaneMaximized(0)
	panes := state.Panes(nil)
	first := hitWorkspaceFocusTab.At(int(panes[0].ID))
	result := state.Key(gfx.KeyEvent{Key: gfx.KeyRight, Target: first})
	if !result.Handled || result.Focus != hitWorkspaceFocusTab.At(int(panes[1].ID)) {
		t.Fatal("arrow did not retain focus in strip")
	}
	result = state.Key(gfx.KeyEvent{Key: gfx.KeyEnter, Target: result.Focus})
	if !result.Handled || result.Focus != state.activeTerminalFocusHit() {
		t.Fatal("Enter did not return focus to terminal")
	}
}
