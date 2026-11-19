//go:build darwin || windows || linux

package workbench

import (
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
	"motty/internal/i18n"
)

// The group tree is a pane beside the host list, so the list and grid layouts
// keep working while it is open. Its choice is the same filter the chips set,
// and the open pane, its width and the layout survive a restart.
func TestHostTreePaneSelectsGroupsAndKeepsBothLayouts(t *testing.T) {
	state := nestedHostGroupState(t)
	state.setLanguage(i18n.English)
	view, err := headless.Open(app.Config{
		Width: 1400, Height: 900, Tree: Tree(), Paint: state.Paint,
		OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize,
	}, nil, headless.Options{Scale: 1.25})
	if err != nil {
		t.Fatal(err)
	}
	defer view.Close()
	must := func(err error) {
		t.Helper()
		if err != nil {
			t.Fatal(err)
		}
	}
	hit := func(id gfx.ID) gfx.Hit {
		t.Helper()
		h, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok {
			t.Fatalf("missing %s", tree.Text(id))
		}
		return h
	}
	click := func(id gfx.ID) {
		t.Helper()
		h := hit(id)
		must(view.Click(h.Bounds.X+h.Bounds.Width/2, h.Bounds.Y+h.Bounds.Height/2))
	}
	groupRow := func(name string) int {
		t.Helper()
		if !state.hostSide.visible {
			t.Fatal("the group tree is not shown")
		}
		for row, entry := range state.hostSideRows {
			if entry.Kind == hostSideRowGroup && state.hostGroups[entry.GroupIndex].Name == name {
				return row
			}
		}
		t.Fatalf("group %q is not a row of the tree", name)
		return -1
	}
	must(view.Rest())
	click(hitHostViewTree)
	must(view.Rest())
	if !state.hostSide.open || !state.hostSide.visible {
		t.Fatalf("tree pane: open %v, shown %v", state.hostSide.open, state.hostSide.visible)
	}
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostFilterAll); ok {
		t.Fatal("the tree pane still shows the group chips")
	}

	group := groupRow("Team")
	expanded := state.hostGroups[state.hostSideRows[group].GroupIndex].Expanded
	click(state.hostSide.tree.RowID(group))
	must(view.Rest())
	if state.hostGroups[state.hostSideRows[group].GroupIndex].Expanded == expanded {
		t.Fatal("choosing the group did not open or close it")
	}
	if state.hostFilterOn {
		t.Fatal("choosing the group also filtered the list")
	}
	state.setHostFilter(true, "Team")
	must(view.Rest())
	click(state.hostSide.tree.RowID(0))
	must(view.Rest())
	if state.hostFilterOn {
		t.Fatalf("choosing All left the filter on %q", state.hostFilter)
	}
	click(state.hostSide.tree.RowID(groupRow("Team")))
	must(view.Rest())

	click(hitHostViewGrid)
	must(view.Rest())
	if state.hostView != hostViewGrid || !state.hostSide.visible {
		t.Fatalf("grid with the tree: layout %d, tree shown %v", state.hostView, state.hostSide.visible)
	}
	click(hitHostViewList)
	must(view.Rest())
	if state.hostView != hostViewList || !state.hostSide.visible {
		t.Fatalf("list with the tree: layout %d, tree shown %v", state.hostView, state.hostSide.visible)
	}

	click(hitHostViewTree)
	must(view.Rest())
	if state.hostSide.open || state.hostSide.visible {
		t.Fatalf("closing the tree: open %v, shown %v", state.hostSide.open, state.hostSide.visible)
	}
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitHostFilterAll); !ok {
		t.Fatal("the chips do not return when the tree closes")
	}
	if state.hostFilterOn {
		t.Fatalf("closing the tree left the filter on %q", state.hostFilter)
	}

	state.hostSide.open, state.hostView, state.hostSide.width = true, hostViewGrid, 300
	if !state.saveResult() {
		t.Fatal("the hosts layout did not reach the store")
	}
	restored := newTestState(t, state.options)
	if restored.hostView != hostViewGrid || !restored.hostSide.open {
		t.Fatalf("saved layout = %d, tree open %v", restored.hostView, restored.hostSide.open)
	}
	if restored.hostSide.width != 300 {
		t.Fatalf("saved group tree width = %v, want 300", restored.hostSide.width)
	}
}
