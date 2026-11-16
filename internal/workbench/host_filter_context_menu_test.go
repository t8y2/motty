package workbench

import (
	"fmt"
	"testing"

	"github.com/vivaui/viva/gfx"
)

func TestHostFilterContextMenuTargetsThePointedGroup(t *testing.T) {
	for _, mode := range []hostViewMode{hostViewList, hostViewGrid} {
		for _, target := range []string{"Servers", "Network"} {
			t.Run(fmt.Sprintf("view=%d/target=%s", mode, target), func(t *testing.T) {
				state := newTestState(t, testOptions(t))
				state.hostView = mode
				state.setHostFilter(true, "Servers")
				typeSearch(state, "Production")
				selected := state.selectedHostID
				index := state.hostGroupIndex(target)
				id := hitHostFilterList.At(index)
				canvas, _ := paintedTexts(state)
				hit, ok := hitByID(canvas, id)
				if !ok {
					t.Fatal("group chip is not visible")
				}
				at := gfx.Point{X: hit.Bounds.X + hit.Bounds.Width/2, Y: hit.Bounds.Y + hit.Bounds.Height/2}
				pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: id, X: at.X, Y: at.Y})
				if !state.groupActions.open || state.groupActions.path != target || state.groupActions.at != at {
					t.Fatalf("right-click did not open the pointed group's menu: %+v", state.groupActions)
				}
				if !state.hostFilterOn || state.hostFilter != "Servers" || state.search.String() != "Production" || state.selectedHostID != selected {
					t.Fatal("right-click changed the active filter, search or selected host")
				}
				state.Click(hitGroupNewHost)
				if state.modal != modalNewHost || state.editor.value(fieldGroup) != target {
					t.Fatal("New Host did not prefill the right-clicked group")
				}
			})
		}
	}
}

func TestHostFilterContextMenuDoesNotGiveAllAGroupTarget(t *testing.T) {
	state := newTestState(t, testOptions(t))
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: hitHostFilterAll, X: 250, Y: 110})
	if state.groupActions.open {
		t.Fatal("All incorrectly opened a group actions menu")
	}
	state.Click(hitHostFilterList.At(state.hostGroupIndex("Network")))
	if !state.hostFilterOn || state.hostFilter != "Network" {
		t.Fatal("left-click no longer selects a group")
	}
}

func TestHostFilterOverflowContextMenuReplacesThePicker(t *testing.T) {
	state := manyHostsState(t, 18)
	for index := range state.hosts {
		state.hosts[index].Group = fmt.Sprintf("Group %02d", index)
	}
	state.hostDataChanged()
	paintAt(state, gfx.Size{Width: 960, Height: 600})
	if state.hostFilterHidden >= len(state.hostGroups) {
		t.Fatal("fixture did not overflow")
	}
	state.Click(hitHostFilterMore)
	id := hitHostFilterMenuList.At(state.hostFilterHidden)
	pointer(state, gfx.PointerEvent{Kind: gfx.PointerDown, Button: gfx.PointerButtonSecondary, Target: id, X: 500, Y: 150, Viewport: gfx.Size{Width: 960, Height: 600}})
	want := state.hostGroups[state.hostFilterHidden].Name
	if !state.groupActions.open || state.groupActions.path != want || state.hostFilterMenu || state.hostFilterOn {
		t.Fatalf("overflow right-click did not replace the picker without selecting: menu=%+v picker=%v", state.groupActions, state.hostFilterMenu)
	}
	state.Key(gfx.KeyEvent{Key: gfx.KeyEscape})
	if state.groupActions.open {
		t.Fatal("Escape did not close the group menu")
	}
}
