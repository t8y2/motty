package workbench

import (
	"github.com/vivaui/viva/gfx"
	layoutcore "motty/internal/workspace"
	"testing"
)

func TestPaneMaximizeRestoredAndEmptyControls(t *testing.T) {
	_, options := newWorld(t)
	original := newTestState(t, options)
	openHost(original)
	openHostInSplit(t, original, 2, layoutcore.Horizontal)
	original.save()
	restored := newTestState(t, options)
	size := gfx.Size{Width: 1400, Height: 900}
	restored.Resize(size)
	driver := paneDriver(t, restored, size)
	for _, pane := range restored.Panes(nil) {
		hitOf(t, driver, hitPaneMaximizeList.At(int(pane.ID)))
	}
	target := restored.Panes(nil)[0].ID
	hit := hitOf(t, driver, hitPaneMaximizeList.At(int(target)))
	if err := driver.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2); err != nil {
		t.Fatal(err)
	}
	hitOf(t, driver, hitPaneMaximizeList.At(int(target)))
	if !restored.deck.PaneMaximized(restored.deck.ActiveWorkspace()) {
		t.Fatal("restored page did not maximize")
	}
	empty := newTestState(t, testOptions(t))
	openEmptyTab(empty)
	if !empty.splitActivePane(layoutcore.Horizontal) {
		t.Fatal("empty split failed")
	}
	empty.Resize(size)
	emptyDriver := paneDriver(t, empty, size)
	for _, pane := range empty.Panes(nil) {
		hitOf(t, emptyDriver, hitPaneMaximizeList.At(int(pane.ID)))
	}
}

func TestPaneMaximizeBottomTabsRevealSelectedPane(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	state.Resize(gfx.Size{Width: 400, Height: 500})
	panes := state.deck.Panes(workspace, nil)
	state.setPaneMaximized(workspace, panes[len(panes)-1].ID, true)
	driver := paneDriver(t, state, state.viewport)
	selected := hitOf(t, driver, hitWorkspaceFocusTab.At(int(panes[len(panes)-1].ID)))
	strip := state.workspaceFocusTabsBounds(workspace, state.viewport)
	if selected.Bounds.X < strip.X || selected.Bounds.X+selected.Bounds.Width > strip.X+strip.Width+.5 {
		t.Fatalf("selected tab outside viewport: %+v / %+v", selected.Bounds, strip)
	}
}

func TestPaneMaximizeReopenedStripRevealsSelection(t *testing.T) {
	state := newTestState(t, testOptions(t))
	workspace := launchTemporaryWorkspace(t, state, 0, 1, 2)
	size := gfx.Size{Width: 400, Height: 500}
	state.Resize(size)
	panes := state.deck.Panes(workspace, nil)
	selected := panes[len(panes)-1].ID
	state.setPaneMaximized(workspace, selected, true)
	var canvas gfx.Canvas
	paint := func() { canvas.Reset(); state.Paint(&canvas, size) }
	visible := func() {
		t.Helper()
		hit, ok := hitByID(&canvas, hitWorkspaceFocusTab.At(int(selected)))
		frame := state.workspaceFocusTabsBounds(workspace, size)
		if !ok || hit.Bounds.X < frame.X || hit.Bounds.X+hit.Bounds.Width > frame.X+frame.Width+.5 {
			t.Fatalf("selected tab not revealed: %+v within %+v", hit.Bounds, frame)
		}
	}
	paint()
	visible()
	state.togglePaneMaximized(0)
	paint()
	state.togglePaneMaximized(0)
	paint()
	visible()
	state.Click(hitSettings)
	paint()
	state.activateWorkspace(workspace)
	paint()
	visible()
}
