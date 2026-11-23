package workbench

import (
	"github.com/vivaui/viva/gfx"
	"reflect"
	"testing"
)

func TestWorkspaceFocusTabDragAfterScrolling(t *testing.T) {
	state, _, _, _ := liveSession(t)
	for len(state.sessions) < 12 {
		state.duplicateTerminal(terminalTestHitByID(state, state.activeSessionID))
	}
	workspace := state.deck.ActiveWorkspace()
	state.togglePaneMaximized(0)
	driver := paneDriver(t, state, gfx.Size{Width: 1000, Height: 700})
	if state.focusTabs.scroll.OffsetX <= 0 {
		t.Fatal("active last tab was not scrolled into view")
	}
	original := state.deck.Panes(workspace, nil)
	from := hitOf(t, driver, hitWorkspaceFocusTab.At(int(original[11].ID))).Bounds
	to := hitOf(t, driver, hitWorkspaceFocusTab.At(int(original[10].ID))).Bounds
	if err := driver.Press(from.X+from.Width/2, from.Y+10, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(to.X+3, to.Y+10); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(to.X+3, to.Y+10, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	want := append(original[:10:10], original[11], original[10])
	if got := state.deck.Panes(workspace, nil); !reflect.DeepEqual(got, want) {
		t.Fatalf("scrolled order: %+v", got)
	}
	// 按住拖到左边缘后持续滚动；Escape 取消此次拖动。
	active := hitOf(t, driver, hitWorkspaceFocusTab.At(int(original[11].ID))).Bounds
	frame := state.workspaceFocusTabsBounds(workspace, state.viewport)
	offset := state.focusTabs.scroll.OffsetX
	if err := driver.Press(active.X+active.Width/2, active.Y+10, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if err := driver.Move(frame.X+2, active.Y+10); err != nil {
		t.Fatal(err)
	}
	for range 3 {
		if err := driver.Paint(); err != nil {
			t.Fatal(err)
		}
	}
	if state.focusTabs.scroll.OffsetX >= offset {
		t.Fatalf("edge drag did not scroll: before=%v after=%v drag=%+v frame=%+v active=%+v valid=%v", offset, state.focusTabs.scroll.OffsetX, state.focusTabs.drag, frame, active, state.workspaceFocusDragValid())
	}
	if err := driver.Key(gfx.KeyEscape, 0); err != nil {
		t.Fatal(err)
	}
	if err := driver.Release(frame.X+2, active.Y+10, gfx.PointerButtonPrimary); err != nil {
		t.Fatal(err)
	}
	if got := state.deck.Panes(workspace, nil); !reflect.DeepEqual(got, want) {
		t.Fatal("cancelled drag changed order")
	}
}
