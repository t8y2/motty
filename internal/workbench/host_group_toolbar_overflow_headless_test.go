//go:build darwin || windows || linux

package workbench

import (
	"fmt"
	"os"
	"path/filepath"
	"testing"

	"github.com/vivaui/viva/app"
	"github.com/vivaui/viva/gfx"
	"github.com/vivaui/viva/headless"
)

func TestHostGroupToolbarNativeOverflowAndKeyboard(t *testing.T) {
	state := manyHostsState(t, 16)
	state.hostView = hostViewList
	state.setAppearance(appearanceDark)
	for index := range state.hosts {
		state.hosts[index].Group = fmt.Sprintf("Region %02d", index)
	}
	state.hostDataChanged()
	view, err := headless.Open(app.Config{Width: 960, Height: 600, Tree: Tree(), Paint: state.Paint, OnPointer: state.Pointer, OnKey: state.Key, OnScroll: state.Scroll, OnResize: state.Resize}, nil, headless.Options{Scale: 1.25})
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
	must(view.Rest())
	click := func(id gfx.ID) {
		t.Helper()
		hit, ok := hitByIDSnapshot(view.Snapshot().Hits, id)
		if !ok || hit.Disabled {
			t.Fatalf("unavailable %s", tree.Text(id))
		}
		must(view.Click(hit.Bounds.X+hit.Bounds.Width/2, hit.Bounds.Y+hit.Bounds.Height/2))
	}
	focus := func(id gfx.ID) {
		t.Helper()
		for step := 0; step < 64 && view.Snapshot().Focused != id; step++ {
			must(view.Keys("Shift+Tab"))
		}
		if view.Snapshot().Focused != id {
			t.Fatalf("keyboard could not reach %s", tree.Text(id))
		}
	}
	click(hitHostFilterAll)
	focus(hitHostGroupsCollapseAll)
	must(view.Keys("Enter"))
	for _, group := range state.hostGroups {
		if group.Expanded {
			t.Fatal("keyboard collapse left an expanded group")
		}
	}
	focus(hitHostGroupsExpandAll)
	must(view.Keys("Enter"))
	for _, group := range state.hostGroups {
		if !group.Expanded {
			t.Fatal("keyboard expand left a collapsed group")
		}
	}
	click(hitHostCheckList.At(int(state.hosts[0].ID)))
	click(hitHostCheckList.At(int(state.hosts[1].ID)))
	if state.hostFilterHidden >= len(state.hostGroups) {
		t.Fatal("fixture did not force filter overflow")
	}
	if len(state.hostSelections) != 2 {
		t.Fatal("fixture did not check two hosts")
	}
	if dir := os.Getenv("MOTTY_GROUP_TOOLBAR_SCREENSHOTS"); dir != "" {
		must(view.Rest())
		must(os.MkdirAll(dir, 0700))
		must(view.Screenshot(filepath.Join(dir, "433-compact-overflow-selected.png")))
	}
	click(hitHostFilterMore)
	must(view.Keys("End"))
	must(view.Keys("Enter"))
	if !state.hostFilterOn || state.hostFilter != state.hostGroups[len(state.hostGroups)-1].Name || len(state.hostSelections) != 0 {
		t.Fatal("More keyboard selection did not switch groups and clear checks")
	}
	if _, ok := hitByIDSnapshot(view.Snapshot().Hits, hitDeleteHost); ok {
		t.Fatal("batch toolbar remained after filter selection cleared checks")
	}
}
